package anonymize

import (
	"context"
	"encoding/json"
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
	staticRepl string
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
	if r.indexes[label] == nil {
		r.indexes[label] = make(map[string]int)
	}
	if idx, ok := r.indexes[label][rawValue]; ok {
		return fmt.Sprintf("[%s_%d]", label, idx)
	}
	r.next[label]++
	idx := r.next[label]
	r.indexes[label][rawValue] = idx
	return fmt.Sprintf("[%s_%d]", label, idx)
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
		if tmpl := pc.ReplacementTemplate; tmpl != "" && !strings.Contains(tmpl, "${INDEX}") {
			cp.staticRepl = tmpl
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
func (s *LocalScrubber) Scrub(_ context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	if result == nil {
		return nil, nil
	}

	reg := newTokenRegistry()
	var allFindings []Finding

	// Deep-copy so the original result is never mutated.
	out := *result
	out.Content = make([]mcp.Content, len(result.Content))
	copy(out.Content, result.Content)

	for i, c := range out.Content {
		tc, ok := c.(mcp.TextContent)
		if !ok {
			continue
		}
		var findings []Finding
		if json.Valid([]byte(tc.Text)) {
			tc.Text, findings = s.scrubJSON(tc.Text, reg, i)
		} else {
			tc.Text, findings = s.scrubText(tc.Text, reg, i, "")
		}
		out.Content[i] = tc
		allFindings = append(allFindings, findings...)
	}

	// StructuredContent is any — marshal → walk → unmarshal.
	if out.StructuredContent != nil {
		b, err := json.Marshal(out.StructuredContent)
		if err == nil {
			scrubbedJSON, scFindings := s.scrubJSON(string(b), reg, -1)
			allFindings = append(allFindings, scFindings...)
			var sc any
			if err := json.Unmarshal([]byte(scrubbedJSON), &sc); err == nil {
				out.StructuredContent = sc
			}
		}
	}

	if s.auditOnly {
		return buildAuditResult(allFindings)
	}
	return &out, nil
}

// scrubJSON parses jsonStr, walks the tree, masks strings, and re-serialises.
func (s *LocalScrubber) scrubJSON(jsonStr string, reg *tokenRegistry, contentIdx int) (string, []Finding) {
	var root any
	if err := json.Unmarshal([]byte(jsonStr), &root); err != nil {
		// Fallback: treat as plain text.
		masked, f := s.scrubText(jsonStr, reg, contentIdx, "")
		return masked, f
	}
	var findings []Finding
	root = s.walkNode(root, "", reg, contentIdx, &findings)
	b, err := json.Marshal(root)
	if err != nil {
		return jsonStr, findings
	}
	return string(b), findings
}

func (s *LocalScrubber) walkNode(node any, path string, reg *tokenRegistry, contentIdx int, findings *[]Finding) any {
	switch v := node.(type) {
	case map[string]any:
		for k, val := range v {
			if _, skip := s.whitelist[k]; skip {
				continue
			}
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			v[k] = s.walkNode(val, childPath, reg, contentIdx, findings)
		}
		return v
	case []any:
		for i, el := range v {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			v[i] = s.walkNode(el, childPath, reg, contentIdx, findings)
		}
		return v
	case string:
		masked, f := s.scrubText(v, reg, contentIdx, path)
		*findings = append(*findings, f...)
		return masked
	default:
		return node
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

