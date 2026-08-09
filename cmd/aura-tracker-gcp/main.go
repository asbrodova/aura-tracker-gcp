package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/oauth2/google"

	"github.com/asbrodova/aura-tracker-gcp/internal/anonymize"
	"github.com/asbrodova/aura-tracker-gcp/internal/config"
	"github.com/asbrodova/aura-tracker-gcp/internal/costreasoning"
	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
	gcpadapter "github.com/asbrodova/aura-tracker-gcp/internal/gcp"
	mcpserver "github.com/asbrodova/aura-tracker-gcp/internal/mcp"
	"github.com/asbrodova/aura-tracker-gcp/internal/safety"
	"github.com/asbrodova/aura-tracker-gcp/internal/securityaudit"
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
		"Comma-separated tool modules to enable: "+strings.Join(mcpserver.AllModules, ",")+".\n"+
			"Use 'none' for zero tools (resources and non-module prompts remain available). Default: all modules.")
	flag.Parse()

	enabledModules := parseModulesFlag(*modulesFlag)

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	userCfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: load user config: %v\n", err)
		os.Exit(1)
	}

	environmentRegistry, err := loadEnvironmentRegistry(userCfg, os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: environment config: %v\n", err)
		os.Exit(1)
	}
	projectID := environmentRegistry.Default().ProjectID

	ctx := context.Background()
	securityCfg := securityAuditConfig(userCfg.SecurityAudit)
	if err := securityaudit.ValidateConfig(securityCfg); err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: security audit config: %v\n", err)
		os.Exit(1)
	}

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
		gcpadapter.WithSecurityAudit(gcpadapter.SecurityAdapterConfig{
			KubernetesAccess: securityCfg.KubernetesAccess, FleetProjectID: securityCfg.FleetProjectID,
			ClusterConcurrency:  securityCfg.ClusterConcurrency,
			PerClusterTimeout:   time.Duration(securityCfg.PerClusterTimeoutSeconds) * time.Second,
			MaxResourcesPerKind: securityCfg.MaxResourcesPerKind,
		}),
	}
	if os.Getenv("RECOMMENDER_ENABLED") != "false" {
		adapterOpts = append(adapterOpts, gcpadapter.WithRecommender())
		log.Info("recommender integration enabled (set RECOMMENDER_ENABLED=false to disable)")
	}

	if err := applyCostReasoningEnv(&userCfg.CostReasoning); err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: cost reasoning config: %v\n", err)
		os.Exit(1)
	}
	costModuleEnabled := userCfg.CostReasoning.Enabled && (enabledModules == nil || enabledModules[mcpserver.ModuleCost])
	if costModuleEnabled {
		if strings.TrimSpace(userCfg.CostReasoning.Dataset) == "" {
			fmt.Fprintln(os.Stderr, "aura-tracker-gcp: cost_reasoning.dataset or BILLING_EXPORT_DATASET is required when cost reasoning is enabled")
			os.Exit(1)
		}
		if userCfg.CostReasoning.Timezone != "" {
			if _, err := time.LoadLocation(userCfg.CostReasoning.Timezone); err != nil {
				fmt.Fprintf(os.Stderr, "aura-tracker-gcp: invalid cost reasoning timezone %q: %v\n", userCfg.CostReasoning.Timezone, err)
				os.Exit(1)
			}
		}
		adapterOpts = append(adapterOpts, gcpadapter.WithCostReasoning(gcpadapter.CostAdapterConfig{
			QueryProjectID: userCfg.CostReasoning.QueryProjectID, ExportProjectID: userCfg.CostReasoning.ExportProjectID,
			Dataset: userCfg.CostReasoning.Dataset, Table: userCfg.CostReasoning.Table,
			MaxBytesBilled: userCfg.CostReasoning.MaxBytesBilled,
		}))
		log.Info("cost reasoning enabled", "export_project", userCfg.CostReasoning.ExportProjectID, "dataset", userCfg.CostReasoning.Dataset)
	}

	// RECOMMENDER_BQ_EXPORT_ENABLED/DATASET override yaml config.
	if v := os.Getenv("RECOMMENDER_BQ_EXPORT_ENABLED"); v == "true" {
		userCfg.RecommenderExport.Enabled = true
	}
	if v := os.Getenv("RECOMMENDER_BQ_EXPORT_DATASET"); v != "" {
		userCfg.RecommenderExport.Dataset = v
	}
	if tb := os.Getenv("TRACE_BACKEND"); tb != "" {
		adapterOpts = append(adapterOpts, gcpadapter.WithTraceBackend(tb))
	}
	if gts := os.Getenv("GRAPH_TIMEOUT_SECONDS"); gts != "" {
		var secs int
		if _, err := fmt.Sscanf(gts, "%d", &secs); err == nil && secs > 0 {
			adapterOpts = append(adapterOpts, gcpadapter.WithGraphTimeout(time.Duration(secs)*time.Second))
		}
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

	anon, anonClose, err := buildAnonymizer(ctx, anonCfg, log, projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: %v\n", err)
		os.Exit(1)
	}
	defer anonClose()
	if anonCfg.Enabled {
		log.Info("anonymization enabled", "mode", anonCfg.Mode, "audit_only", anonCfg.AuditOnly)
	}

	var gcpSvc ports.GCPService = svc
	if os.Getenv("SAFETY_ENABLED") != "false" {
		gcpSvc = safety.NewSafetyDecorator(svc, log)
		log.Info("safety enforcement enabled", "plan_ttl", safety.PlanTTL)
	} else {
		log.Warn("safety enforcement DISABLED via SAFETY_ENABLED=false")
	}

	mcpOpts := []mcpserver.Option{
		mcpserver.WithAnonymizer(anon),
		mcpserver.WithModules(enabledModules),
		mcpserver.WithEnvironments(environmentRegistry),
	}
	if os.Getenv("ANONYMIZE_PROJECT_ID") == "true" {
		replacements := make(map[string]string)
		for _, environment := range environmentRegistry.Environments() {
			if environment.Alias == "" {
				replacements[environment.ProjectID] = "[GCP_PROJECT_ID]"
			}
		}
		mcpOpts = append(mcpOpts, mcpserver.WithProjectIDReplacements(replacements))
		if len(replacements) > 0 {
			mcpOpts = append(mcpOpts, mcpserver.WithProjectIDPlaceholder("your-project"))
		}
		log.Info("project ID masking enabled")
	}
	if userCfg.RecommenderExport.Enabled {
		mcpOpts = append(mcpOpts, mcpserver.WithRecommenderExport())
		log.Info("recommender BigQuery export enabled", "dataset", userCfg.RecommenderExport.Dataset)
	}
	if costModuleEnabled {
		mcpOpts = append(mcpOpts, mcpserver.WithCostReasoning(costreasoning.Config{
			Timezone: userCfg.CostReasoning.Timezone, HistoryDays: userCfg.CostReasoning.HistoryDays,
		}))
	}
	mcpOpts = append(mcpOpts, mcpserver.WithSecurityAuditConfig(securityCfg))
	s := mcpserver.New(gcpSvc, log, version, mcpOpts...)

	switch os.Getenv("MCP_TRANSPORT") {
	case "sse":
		port := os.Getenv("PORT") // Cloud Run sets PORT automatically.
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

		sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()

		go func() {
			if err := sseServer.Start(":" + port); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(os.Stderr, "aura-tracker-gcp: server error: %v\n", err)
				stop() // unblock the wait below so the process exits cleanly.
			}
		}()

		<-sigCtx.Done()
		log.Info("shutdown signal received; draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := sseServer.Shutdown(shutdownCtx); err != nil {
			log.Error("server shutdown", "err", err)
		}

	default: // "stdio" or unset — existing behavior preserved.
		log.Info("aura-tracker-gcp starting", "transport", "stdio", "version", version)
		if err := server.ServeStdio(s); err != nil {
			fmt.Fprintf(os.Stderr, "aura-tracker-gcp: server error: %v\n", err)
			os.Exit(1)
		}
	}
}

func securityAuditConfig(cfg config.SecurityAuditConfig) securityaudit.Config {
	out := securityaudit.Config{
		KubernetesAccess: cfg.KubernetesAccess, FleetProjectID: cfg.FleetProjectID,
		ClusterConcurrency: cfg.ClusterConcurrency, PerClusterTimeoutSeconds: cfg.PerClusterTimeoutSeconds,
		MaxResourcesPerKind: cfg.MaxResourcesPerKind,
		Suppressions:        make([]securityaudit.Suppression, 0, len(cfg.Suppressions)),
	}
	for _, suppression := range cfg.Suppressions {
		out.Suppressions = append(out.Suppressions, securityaudit.Suppression{
			RuleID: suppression.RuleID, Resource: suppression.Resource, Reason: suppression.Reason,
			Owner: suppression.Owner, ExpiresAt: suppression.ExpiresAt,
		})
	}
	return out
}

func loadEnvironmentRegistry(userCfg config.Config, getenv func(string) string) (*environments.Registry, error) {
	legacyEnvProject := strings.TrimSpace(getenv("GCP_PROJECT_ID"))
	rawEnvironments := strings.TrimSpace(getenv("GCP_ENVIRONMENTS_JSON"))

	var configured []config.EnvironmentConfig
	switch {
	case rawEnvironments != "":
		if legacyEnvProject != "" || userCfg.ProjectID != "" || len(userCfg.Environments) > 0 {
			return nil, errors.New("GCP_ENVIRONMENTS_JSON cannot be combined with GCP_PROJECT_ID, project_id, or environments in ~/.aura-tracker.yaml")
		}
		if err := json.Unmarshal([]byte(rawEnvironments), &configured); err != nil {
			return nil, fmt.Errorf("parse GCP_ENVIRONMENTS_JSON: %w", err)
		}
	case len(userCfg.Environments) > 0:
		if legacyEnvProject != "" || strings.TrimSpace(userCfg.ProjectID) != "" {
			return nil, errors.New("environments cannot be combined with GCP_PROJECT_ID or project_id")
		}
		configured = userCfg.Environments
	default:
		projectID := legacyEnvProject
		if projectID == "" {
			projectID = strings.TrimSpace(userCfg.ProjectID)
		}
		if projectID == "" {
			return nil, errors.New("GCP_PROJECT_ID, project_id, environments, or GCP_ENVIRONMENTS_JSON is required")
		}
		configured = []config.EnvironmentConfig{{ProjectID: projectID, Default: true}}
	}

	items := make([]environments.Environment, 0, len(configured))
	for _, environment := range configured {
		items = append(items, environments.Environment{
			ProjectID: environment.ProjectID,
			Alias:     environment.Alias,
			Default:   environment.Default,
		})
	}
	return environments.NewRegistry(items)
}

func applyCostReasoningEnv(cfg *config.CostReasoningConfig) error {
	if cfg == nil {
		return errors.New("configuration is nil")
	}
	if value := os.Getenv("COST_REASONING_ENABLED"); value != "" {
		switch value {
		case "true":
			cfg.Enabled = true
		case "false":
			cfg.Enabled = false
		default:
			return fmt.Errorf("COST_REASONING_ENABLED must be 'true' or 'false'")
		}
	}
	stringOverrides := []struct {
		name   string
		target *string
	}{
		{"COST_QUERY_PROJECT_ID", &cfg.QueryProjectID},
		{"BILLING_EXPORT_PROJECT_ID", &cfg.ExportProjectID},
		{"BILLING_EXPORT_DATASET", &cfg.Dataset},
		{"BILLING_EXPORT_TABLE", &cfg.Table},
		{"COST_REASONING_TIMEZONE", &cfg.Timezone},
	}
	for _, override := range stringOverrides {
		if value := os.Getenv(override.name); value != "" {
			*override.target = value
		}
	}
	if value := os.Getenv("COST_QUERY_MAX_BYTES"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("COST_QUERY_MAX_BYTES must be a positive integer")
		}
		cfg.MaxBytesBilled = parsed
	}
	if value := os.Getenv("COST_REASONING_HISTORY_DAYS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 14 || parsed > 366 {
			return fmt.Errorf("COST_REASONING_HISTORY_DAYS must be between 14 and 366")
		}
		cfg.HistoryDays = parsed
	}
	if cfg.HistoryDays != 0 && (cfg.HistoryDays < 14 || cfg.HistoryDays > 366) {
		return fmt.Errorf("history_days must be between 14 and 366")
	}
	if cfg.MaxBytesBilled < 0 {
		return fmt.Errorf("max_bytes_billed must be positive")
	}
	return nil
}

// buildAnonymizer constructs the configured Anonymizer and returns a cleanup function.
// The cleanup function must always be called (defer it immediately after a nil-error check).
func buildAnonymizer(ctx context.Context, cfg anonymize.Config, log *slog.Logger, projectID string) (anonymize.Anonymizer, func(), error) {
	if !cfg.Enabled {
		return anonymize.NoopAnonymizer{}, func() {}, nil
	}

	switch cfg.Mode {
	case anonymize.ModeDLP:
		dlp, err := gcpadapter.NewDLPAdapter(ctx, log)
		if err != nil {
			return nil, nil, fmt.Errorf("init dlp adapter: %w", err)
		}
		return anonymize.NewDLPAnonymizer(dlp, cfg, projectID),
			func() {
				if err := dlp.Close(); err != nil {
					log.Error("closing dlp adapter", "err", err)
				}
			}, nil

	case anonymize.ModeBoth:
		dlp, err := gcpadapter.NewDLPAdapter(ctx, log)
		if err != nil {
			return nil, nil, fmt.Errorf("init dlp adapter: %w", err)
		}
		// Local always masks (auditOnly=false) so its output feeds DLP cleanly.
		localCfg := cfg
		localCfg.AuditOnly = false
		local, err := anonymize.NewLocalScrubber(localCfg)
		if err != nil {
			_ = dlp.Close()
			return nil, nil, fmt.Errorf("init local scrubber: %w", err)
		}
		chained := anonymize.NewChainedAnonymizer(local, anonymize.NewDLPAnonymizer(dlp, cfg, projectID))
		return chained,
			func() {
				if err := dlp.Close(); err != nil {
					log.Error("closing dlp adapter", "err", err)
				}
			}, nil

	default: // ModeLocal
		scrubber, err := anonymize.NewLocalScrubber(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("init local scrubber: %w", err)
		}
		return scrubber, func() {}, nil
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

// tokenInfoClient is a dedicated HTTP client for Google tokeninfo validation.
// The 5-second timeout prevents a slow tokeninfo endpoint from hanging an MCP connection.
var tokenInfoClient = &http.Client{Timeout: 5 * time.Second}

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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			"https://oauth2.googleapis.com/tokeninfo?access_token="+token, nil)
		if err != nil {
			log.Warn("bearer token validation: build request", "err", err)
			return ctx
		}
		resp, err := tokenInfoClient.Do(req)
		if err != nil {
			log.Warn("bearer token validation failed", "err", err)
			return ctx
		}
		defer resp.Body.Close() //nolint:errcheck
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
