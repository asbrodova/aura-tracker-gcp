package anonymize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// compiledPattern is a ready-to-use scrubbing rule.
type compiledPattern struct {
	name  string
	re    *regexp.Regexp
	label string // upper-case prefix for the indexed token, e.g. "EMAIL" → [EMAIL_1]
	// staticRepl, when non-empty, replaces every match with this fixed string
	// instead of an indexed token. Used for secrets that need no context.
	staticRepl      string
	indexedTemplate string
}

// builtinPatterns are always applied, in order, before any custom patterns.
// internal_ip must precede public_ip so private ranges are labelled correctly.
var builtinPatterns = []compiledPattern{
	{
		name:  "internal_ip",
		re:    regexp.MustCompile(`\b(10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b`),
		label: "INTERNAL_IP",
	},
	{
		name:  "public_ip",
		re:    regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`),
		label: "PUBLIC_IP",
	},
	{
		name:  "email",
		re:    regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
		label: "EMAIL",
	},
	{
		name:  "service_account",
		re:    regexp.MustCompile(`[a-z][a-z0-9\-]{4,28}[a-z0-9]@[a-z][a-z0-9\-]{4,28}[a-z0-9]\.iam\.gserviceaccount\.com`),
		label: "SERVICE_ACCOUNT",
	},
	{
		name:  "gcp_api_key",
		re:    regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`),
		label: "GCP_API_KEY",
	},
}

// tokenRegistry maps (label, rawValue) → stable index within one Scrub call.
// The same raw value always gets the same token, allowing the LLM to correlate
// occurrences while identity stays hidden.
type tokenRegistry struct {
	indexes map[string]map[string]int // label → rawValue → assigned index
	next    map[string]int            // label → next counter
}

func newTokenRegistry() *tokenRegistry {
	return &tokenRegistry{
		indexes: make(map[string]map[string]int),
		next:    make(map[string]int),
	}
}

func (r *tokenRegistry) tokenFor(label, rawValue string) string {
	return fmt.Sprintf("[%s_%d]", label, r.indexFor(label, rawValue))
}

func (r *tokenRegistry) indexFor(label, rawValue string) int {
	if r.indexes[label] == nil {
		r.indexes[label] = make(map[string]int)
	}
	if idx, ok := r.indexes[label][rawValue]; ok {
		return idx
	}
	r.next[label]++
	idx := r.next[label]
	r.indexes[label][rawValue] = idx
	return idx
}

// LocalScrubber is a fast, regex-based Anonymizer. All methods are goroutine-safe
// because the mutable per-call state (tokenRegistry) is stack-allocated in Scrub.
type LocalScrubber struct {
	patterns  []compiledPattern
	whitelist map[string]struct{} // JSON key names whose values are never masked
	auditOnly bool
}

// NewLocalScrubber compiles the built-in patterns plus any custom patterns from cfg.
func NewLocalScrubber(cfg Config) (*LocalScrubber, error) {
	patterns := make([]compiledPattern, len(builtinPatterns))
	copy(patterns, builtinPatterns)

	for _, pc := range cfg.Patterns {
		re, err := regexp.Compile(pc.Regex)
		if err != nil {
			return nil, fmt.Errorf("anonymize: compile pattern %q: %w", pc.Name, err)
		}
		label := strings.ToUpper(strings.ReplaceAll(pc.Name, " ", "_"))
		cp := compiledPattern{name: pc.Name, re: re, label: label}
		if tmpl := pc.ReplacementTemplate; tmpl != "" {
			if strings.Contains(tmpl, "${INDEX}") {
				cp.indexedTemplate = tmpl
			} else {
				cp.staticRepl = tmpl
			}
		}
		patterns = append(patterns, cp)
	}

	whitelist := make(map[string]struct{}, len(cfg.JSONKeyWhitelist))
	for _, k := range cfg.JSONKeyWhitelist {
		whitelist[k] = struct{}{}
	}

	return &LocalScrubber{
		patterns:  patterns,
		whitelist: whitelist,
		auditOnly: cfg.AuditOnly,
	}, nil
}

// Scrub applies all patterns to the result's text content and structured content.
// If AuditOnly is true, the result content is replaced with an AuditReport.
func (s *LocalScrubber) Scrub(ctx context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	out, findings, err := s.scrub(ctx, result)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, nil
	}
	if s.auditOnly {
		return buildAuditResult(findings)
	}
	return out, nil
}

func (s *LocalScrubber) scrub(_ context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, []Finding, error) {
	if result == nil {
		return nil, nil, nil
	}

	reg := newTokenRegistry()
	var allFindings []Finding

	// Deep-copy so the original result is never mutated.
	out := *result
	meta, findings, err := s.scrubMeta(result.Meta, reg)
	if err != nil {
		return nil, nil, err
	}
	out.Meta = meta
	allFindings = append(allFindings, findings...)
	out.Content = make([]mcp.Content, len(result.Content))

	for i, content := range result.Content {
		scrubbed, findings, scrubErr := s.scrubContent(content, reg, i)
		if scrubErr != nil {
			return nil, nil, scrubErr
		}
		out.Content[i] = scrubbed
		allFindings = append(allFindings, findings...)
	}

	// StructuredContent is any — marshal → walk → unmarshal.
	if out.StructuredContent != nil {
		b, err := json.Marshal(out.StructuredContent)
		if err != nil {
			return nil, nil, fmt.Errorf("anonymize: marshal structured content: %w", err)
		}
		scrubbedJSON, scFindings, err := s.scrubJSON(string(b), reg, -1)
		if err != nil {
			return nil, nil, fmt.Errorf("anonymize: scrub structured content: %w", err)
		}
		allFindings = append(allFindings, scFindings...)
		var sc any
		if err := json.Unmarshal([]byte(scrubbedJSON), &sc); err != nil {
			return nil, nil, fmt.Errorf("anonymize: unmarshal scrubbed structured content: %w", err)
		}
		out.StructuredContent = sc
	}
	return &out, allFindings, nil
}

// scrubContent masks every serialized, string-bearing field in an MCP content
// object. Text payloads are handled separately so JSON key whitelisting keeps
// its documented behavior; the remaining envelope is round-tripped through the
// protocol's content decoder to cover metadata, annotations, links, MIME types,
// and embedded resource fields without maintaining a fragile field allowlist.
func (s *LocalScrubber) scrubContent(content mcp.Content, reg *tokenRegistry, contentIdx int) (mcp.Content, []Finding, error) {
	if content == nil {
		return nil, nil, errors.New("anonymize: nil MCP content withheld")
	}
	var err error
	content, err = prepareScrubbableContent(content)
	if err != nil {
		return nil, nil, err
	}

	var payload string
	var hasPayload bool
	switch typed := content.(type) {
	case mcp.TextContent:
		payload = typed.Text
		hasPayload = true
		typed.Text = ""
		content = typed
	case mcp.EmbeddedResource:
		if resource, ok := typed.Resource.(mcp.TextResourceContents); ok {
			payload = resource.Text
			hasPayload = true
			resource.Text = "__ANONYMIZE_TEXT_PAYLOAD__"
			typed.Resource = resource
			content = typed
		}
	}

	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, nil, fmt.Errorf("anonymize: marshal MCP content: %w", err)
	}
	scrubbedJSON, findings, err := s.scrubJSON(string(encoded), reg, contentIdx)
	if err != nil {
		return nil, nil, fmt.Errorf("anonymize: scrub MCP content: %w", err)
	}
	scrubbed, err := parseScrubbedContent([]byte(scrubbedJSON))
	if err != nil {
		return nil, nil, fmt.Errorf("anonymize: decode scrubbed MCP content: %w", err)
	}

	if !hasPayload {
		return scrubbed, findings, nil
	}
	var payloadFindings []Finding
	if json.Valid([]byte(payload)) {
		payload, payloadFindings, err = s.scrubJSON(payload, reg, contentIdx)
	} else {
		payload, payloadFindings = s.scrubText(payload, reg, contentIdx, "content.payload")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("anonymize: scrub MCP payload: %w", err)
	}
	findings = append(findings, payloadFindings...)

	switch typed := scrubbed.(type) {
	case mcp.TextContent:
		typed.Text = payload
		return typed, findings, nil
	case mcp.EmbeddedResource:
		resource, ok := typed.Resource.(mcp.TextResourceContents)
		if !ok {
			return nil, nil, errors.New("anonymize: text resource changed type while scrubbing")
		}
		resource.Text = payload
		typed.Resource = resource
		return typed, findings, nil
	default:
		return nil, nil, errors.New("anonymize: content changed type while scrubbing")
	}
}

func (s *LocalScrubber) scrubMeta(meta *mcp.Meta, reg *tokenRegistry) (*mcp.Meta, []Finding, error) {
	if meta == nil {
		return nil, nil, nil
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return nil, nil, fmt.Errorf("anonymize: marshal result metadata: %w", err)
	}
	scrubbedJSON, findings, err := s.scrubJSON(string(encoded), reg, -1)
	if err != nil {
		return nil, nil, fmt.Errorf("anonymize: scrub result metadata: %w", err)
	}
	var out mcp.Meta
	if err := json.Unmarshal([]byte(scrubbedJSON), &out); err != nil {
		return nil, nil, fmt.Errorf("anonymize: decode result metadata: %w", err)
	}
	return &out, findings, nil
}

// scrubJSON parses jsonStr, walks the tree, masks strings, and re-serialises.
func (s *LocalScrubber) scrubJSON(jsonStr string, reg *tokenRegistry, contentIdx int) (string, []Finding, error) {
	var root any
	if err := json.Unmarshal([]byte(jsonStr), &root); err != nil {
		// Fallback: treat as plain text.
		masked, f := s.scrubText(jsonStr, reg, contentIdx, "")
		return masked, f, nil
	}
	var findings []Finding
	root, err := s.walkNode(root, "", reg, contentIdx, &findings)
	if err != nil {
		return "", findings, err
	}
	b, err := json.Marshal(root)
	if err != nil {
		return "", findings, err
	}
	return string(b), findings, nil
}

func (s *LocalScrubber) walkNode(node any, path string, reg *tokenRegistry, contentIdx int, findings *[]Finding) (any, error) {
	switch v := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			maskedKey, keyFindings := s.scrubText(k, reg, contentIdx, childPath+"{key}")
			*findings = append(*findings, keyFindings...)
			if _, collision := out[maskedKey]; collision {
				return nil, fmt.Errorf("anonymize: JSON key collision after scrubbing at %q", childPath)
			}
			if _, skip := s.whitelist[k]; skip {
				out[maskedKey] = val
				continue
			}
			scrubbed, err := s.walkNode(val, childPath, reg, contentIdx, findings)
			if err != nil {
				return nil, err
			}
			out[maskedKey] = scrubbed
		}
		return out, nil
	case []any:
		for i, el := range v {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			scrubbed, err := s.walkNode(el, childPath, reg, contentIdx, findings)
			if err != nil {
				return nil, err
			}
			v[i] = scrubbed
		}
		return v, nil
	case string:
		masked, f := s.scrubText(v, reg, contentIdx, path)
		*findings = append(*findings, f...)
		return masked, nil
	default:
		return node, nil
	}
}

// scrubText applies all compiled patterns to a plain string.
func (s *LocalScrubber) scrubText(text string, reg *tokenRegistry, contentIdx int, jsonPath string) (string, []Finding) {
	var findings []Finding
	result := text
	for _, p := range s.patterns {
		count := 0
		result = p.re.ReplaceAllStringFunc(result, func(match string) string {
			count++
			if p.staticRepl != "" {
				return p.staticRepl
			}
			if p.indexedTemplate != "" {
				return strings.ReplaceAll(p.indexedTemplate, "${INDEX}", fmt.Sprintf("%d", reg.indexFor(p.label, match)))
			}
			return reg.tokenFor(p.label, match)
		})
		if count > 0 {
			findings = append(findings, Finding{
				PatternName:  p.name,
				JSONPath:     jsonPath,
				ContentIndex: contentIdx,
				MatchCount:   count,
			})
		}
	}
	return result, findings
}
