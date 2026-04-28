package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/oauth2/google"

	"github.com/asbrodova/aura-tracker-gcp/internal/anonymize"
	gcpadapter "github.com/asbrodova/aura-tracker-gcp/internal/gcp"
	mcpserver "github.com/asbrodova/aura-tracker-gcp/internal/mcp"
	"github.com/asbrodova/aura-tracker-gcp/internal/safety"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// version is overwritten at build time by GoReleaser:
//
//	-ldflags="-X main.version={{.Version}}"
var version = "dev"

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-version" {
			fmt.Println(version)
			os.Exit(0)
		}
	}

	modulesFlag := flag.String("modules", "",
		"Comma-separated tool modules to enable: gke,cloudrun,pubsub,logging,monitoring,iam,topology,aura.\n"+
			"Use 'none' for zero tools (resources and prompts remain available). Default: all modules.")
	flag.Parse()

	enabledModules := parseModulesFlag(*modulesFlag)

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		fmt.Fprintln(os.Stderr, "aura-tracker-gcp: GCP_PROJECT_ID environment variable is required")
		os.Exit(1)
	}

	ctx := context.Background()

	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		fmt.Fprintln(os.Stderr, `aura-tracker-gcp: no GCP credentials found.

Run:  gcloud auth application-default login

Or set GOOGLE_APPLICATION_CREDENTIALS to a service account key file.`)
		os.Exit(1)
	}
	logCredentialSource(log, creds)

	adapterOpts := []gcpadapter.Option{
		gcpadapter.WithRateLimit(10, 20),
		gcpadapter.WithCallTimeout(30 * time.Second),
		gcpadapter.WithLogger(log),
		gcpadapter.WithModules(enabledModules),
	}
	if os.Getenv("RECOMMENDER_ENABLED") == "true" {
		adapterOpts = append(adapterOpts, gcpadapter.WithRecommender())
		log.Info("recommender integration enabled")
	}

	svc, err := gcpadapter.New(ctx, projectID, adapterOpts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: init gcp adapter: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := svc.Close(); err != nil {
			log.Error("closing gcp adapter", "err", err)
		}
	}()

	anonCfg, err := anonymize.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: load anonymize config: %v\n", err)
		os.Exit(1)
	}

	var anon anonymize.Anonymizer = anonymize.NoopAnonymizer{}
	if anonCfg.Enabled {
		switch anonCfg.Mode {
		case anonymize.ModeDLP:
			dlpSvc, err := gcpadapter.NewDLPAdapter(ctx, log)
			if err != nil {
				fmt.Fprintf(os.Stderr, "aura-tracker-gcp: init dlp adapter: %v\n", err)
				os.Exit(1)
			}
			defer func() {
				if err := dlpSvc.Close(); err != nil {
					log.Error("closing dlp adapter", "err", err)
				}
			}()
			anon = anonymize.NewDLPAnonymizer(dlpSvc, anonCfg, projectID)

		case anonymize.ModeBoth:
			dlpSvc, err := gcpadapter.NewDLPAdapter(ctx, log)
			if err != nil {
				fmt.Fprintf(os.Stderr, "aura-tracker-gcp: init dlp adapter: %v\n", err)
				os.Exit(1)
			}
			defer func() {
				if err := dlpSvc.Close(); err != nil {
					log.Error("closing dlp adapter", "err", err)
				}
			}()
			// Local always masks (auditOnly=false) so its output feeds DLP cleanly.
			localCfg := anonCfg
			localCfg.AuditOnly = false
			local, err := anonymize.NewLocalScrubber(localCfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "aura-tracker-gcp: init local scrubber: %v\n", err)
				os.Exit(1)
			}
			dlpAnon := anonymize.NewDLPAnonymizer(dlpSvc, anonCfg, projectID)
			anon = anonymize.NewChainedAnonymizer(local, dlpAnon)

		default: // ModeLocal
			scrubber, err := anonymize.NewLocalScrubber(anonCfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "aura-tracker-gcp: init scrubber: %v\n", err)
				os.Exit(1)
			}
			anon = scrubber
		}
		log.Info("anonymization enabled", "mode", anonCfg.Mode, "audit_only", anonCfg.AuditOnly)
	}

	var gcpSvc ports.GCPService = svc
	if os.Getenv("SAFETY_ENABLED") != "false" {
		gcpSvc = safety.NewSafetyDecorator(svc, log)
		log.Info("safety enforcement enabled", "plan_ttl", safety.PlanTTL)
	} else {
		log.Warn("safety enforcement DISABLED via SAFETY_ENABLED=false")
	}

	s := mcpserver.New(gcpSvc, log, version, mcpserver.WithAnonymizer(anon), mcpserver.WithModules(enabledModules))

	switch os.Getenv("MCP_TRANSPORT") {
	case "sse":
		port := os.Getenv("PORT") // Cloud Run sets PORT automatically
		if port == "" {
			port = "8080"
		}
		baseURL := os.Getenv("MCP_BASE_URL")
		if baseURL == "" {
			baseURL = fmt.Sprintf("http://localhost:%s", port)
			log.Warn("MCP_BASE_URL not set; using localhost — set to public Cloud Run URL in production")
		}
		sseServer := server.NewSSEServer(s,
			server.WithBaseURL(baseURL),
			server.WithSSEContextFunc(bearerAuthMiddleware(log)),
		)
		log.Info("aura-tracker-gcp starting", "transport", "sse", "version", version, "addr", ":"+port, "base_url", baseURL)
		if err := sseServer.Start(":" + port); err != nil {
			fmt.Fprintf(os.Stderr, "aura-tracker-gcp: server error: %v\n", err)
			os.Exit(1)
		}
	default: // "stdio" or unset — existing behavior preserved
		log.Info("aura-tracker-gcp starting", "transport", "stdio", "version", version)
		if err := server.ServeStdio(s); err != nil {
			fmt.Fprintf(os.Stderr, "aura-tracker-gcp: server error: %v\n", err)
			os.Exit(1)
		}
	}
}

// logCredentialSource logs which ADC credential source was detected at startup.
// creds.JSON is non-nil only when credentials were loaded from a file (SA key or
// gcloud well-known file); it is nil when coming from the GCE metadata server.
func logCredentialSource(log *slog.Logger, creds *google.Credentials) {
	switch {
	case os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "":
		log.Info("adc: service account key file", "path", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	case len(creds.JSON) > 0:
		log.Info("adc: user credentials (gcloud application-default login)")
	default:
		log.Info("adc: workload identity / GCE metadata server")
	}
}

// parseModulesFlag converts the --modules flag value to a map[string]bool.
// Returns nil (all modules) when val is empty.
// Returns an empty map (zero tools) when val is "none".
func parseModulesFlag(val string) map[string]bool {
	if val == "" {
		return nil
	}
	if strings.EqualFold(val, "none") {
		return map[string]bool{}
	}
	out := make(map[string]bool)
	for _, m := range strings.Split(val, ",") {
		if m = strings.TrimSpace(strings.ToLower(m)); m != "" {
			out[m] = true
		}
	}
	return out
}

// contextKeyCallerEmail is the context key for the authenticated caller's email,
// injected by bearerAuthMiddleware for audit logging in SSE mode.
type contextKeyCallerEmail struct{}

// bearerAuthMiddleware validates an optional Authorization: Bearer token on each
// MCP request. If valid, it injects the caller's email into the context for audit
// logging. GCP API calls always use the server's Workload Identity SA regardless.
func bearerAuthMiddleware(log *slog.Logger) server.SSEContextFunc {
	return func(ctx context.Context, r *http.Request) context.Context {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			return ctx
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		resp, err := http.Get("https://oauth2.googleapis.com/tokeninfo?access_token=" + token) //nolint:noctx
		if err != nil {
			log.Warn("bearer token validation failed", "err", err)
			return ctx
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Warn("bearer token rejected", "status", resp.StatusCode)
			return ctx
		}
		var info struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&info); err == nil && info.Email != "" {
			log.Info("authenticated mcp request", "user", info.Email)
			return context.WithValue(ctx, contextKeyCallerEmail{}, info.Email)
		}
		return ctx
	}
}
