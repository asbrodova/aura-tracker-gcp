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
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-version" {
			fmt.Println(version)
			return nil
		}
	}

	modulesFlag := flag.String("modules", "",
		"Comma-separated tool modules to enable: "+strings.Join(mcpserver.AllModules, ",")+".\n"+
			"Use 'none' for zero tools (resources and non-module prompts remain available). Default: all modules.")
	flag.Parse()

	enabledModules, err := parseModulesFlag(*modulesFlag)
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	userCfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load user config: %w", err)
	}

	environmentRegistry, err := loadEnvironmentRegistry(userCfg, os.Getenv)
	if err != nil {
		return fmt.Errorf("environment config: %w", err)
	}
	projectID := environmentRegistry.Default().ProjectID

	ctx := context.Background()
	securityCfg := securityAuditConfig(userCfg.SecurityAudit)
	if err := securityaudit.ValidateConfig(securityCfg); err != nil {
		return fmt.Errorf("security audit config: %w", err)
	}

	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return errors.New(`no GCP credentials found.

Run:  gcloud auth application-default login

For automation, use an attached service account, Workload Identity, or service-account impersonation`)
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
	recommenderEnabled, err := readBoolEnv("RECOMMENDER_ENABLED", true, os.Getenv)
	if err != nil {
		return err
	}
	if recommenderEnabled {
		adapterOpts = append(adapterOpts, gcpadapter.WithRecommender())
		log.Info("recommender integration enabled (set RECOMMENDER_ENABLED=false to disable)")
	}

	if err := applyCostReasoningEnv(&userCfg.CostReasoning); err != nil {
		return fmt.Errorf("cost reasoning config: %w", err)
	}
	costModuleEnabled := userCfg.CostReasoning.Enabled && (enabledModules == nil || enabledModules[mcpserver.ModuleCost])
	if costModuleEnabled {
		if strings.TrimSpace(userCfg.CostReasoning.Dataset) == "" {
			return errors.New("cost_reasoning.dataset or BILLING_EXPORT_DATASET is required when cost reasoning is enabled")
		}
		if userCfg.CostReasoning.Timezone != "" {
			if _, err := time.LoadLocation(userCfg.CostReasoning.Timezone); err != nil {
				return fmt.Errorf("invalid cost reasoning timezone %q: %w", userCfg.CostReasoning.Timezone, err)
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
	if strings.TrimSpace(os.Getenv("RECOMMENDER_BQ_EXPORT_ENABLED")) != "" {
		enabled, err := readBoolEnv("RECOMMENDER_BQ_EXPORT_ENABLED", false, os.Getenv)
		if err != nil {
			return err
		}
		userCfg.RecommenderExport.Enabled = enabled
	}
	if v := os.Getenv("RECOMMENDER_BQ_EXPORT_DATASET"); v != "" {
		userCfg.RecommenderExport.Dataset = v
	}
	if tb := os.Getenv("TRACE_BACKEND"); tb != "" {
		tb = strings.ToLower(strings.TrimSpace(tb))
		if tb != "trace" && tb != "monitoring" {
			return errors.New("TRACE_BACKEND must be 'trace' or 'monitoring'")
		}
		adapterOpts = append(adapterOpts, gcpadapter.WithTraceBackend(tb))
	}
	if gts := os.Getenv("GRAPH_TIMEOUT_SECONDS"); gts != "" {
		secs, err := strconv.Atoi(strings.TrimSpace(gts))
		if err != nil || secs < 1 || secs > 3600 {
			return errors.New("GRAPH_TIMEOUT_SECONDS must be an integer between 1 and 3600")
		}
		adapterOpts = append(adapterOpts, gcpadapter.WithGraphTimeout(time.Duration(secs)*time.Second))
	}

	svc, err := gcpadapter.New(ctx, projectID, adapterOpts...)
	if err != nil {
		return fmt.Errorf("init gcp adapter: %w", err)
	}
	defer func() {
		if err := svc.Close(); err != nil {
			log.Error("closing gcp adapter", "err", err)
		}
	}()

	anonCfg, err := anonymize.LoadConfig()
	if err != nil {
		return fmt.Errorf("load anonymize config: %w", err)
	}

	anon, anonClose, err := buildAnonymizer(ctx, anonCfg, log, projectID)
	if err != nil {
		return err
	}
	defer anonClose()
	if anonCfg.Enabled {
		log.Info("anonymization enabled", "mode", anonCfg.Mode, "audit_only", anonCfg.AuditOnly)
	}

	var gcpSvc ports.GCPService = svc
	safetyEnabled, err := readBoolEnv("SAFETY_ENABLED", true, os.Getenv)
	if err != nil {
		return err
	}
	if !safetyEnabled && strings.EqualFold(strings.TrimSpace(os.Getenv("MCP_TRANSPORT")), "sse") {
		return errors.New("SAFETY_ENABLED=false is not allowed with MCP_TRANSPORT=sse")
	}
	if safetyEnabled {
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
	maskProjectID, err := readBoolEnv("ANONYMIZE_PROJECT_ID", false, os.Getenv)
	if err != nil {
		return err
	}
	if maskProjectID {
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
		mcpOpts = append(mcpOpts, mcpserver.WithRecommenderExport(userCfg.RecommenderExport.Dataset))
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
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return errors.New("PORT must be an integer between 1 and 65535")
		}
		baseURL := os.Getenv("MCP_BASE_URL")
		if baseURL == "" {
			baseURL = fmt.Sprintf("http://localhost:%s", port)
			log.Warn("MCP_BASE_URL not set; using localhost — set to public Cloud Run URL in production")
		}
		authCfg, err := loadSSEAuthConfig(baseURL, os.Getenv)
		if err != nil {
			return fmt.Errorf("SSE authentication config: %w", err)
		}
		listenAddr := ":" + port
		if authCfg.Mode == authModeDisabled {
			listenAddr = "127.0.0.1:" + port
			log.Warn("SSE authentication disabled for loopback development")
		}
		httpServer := &http.Server{
			Addr:              listenAddr,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			IdleTimeout:       2 * time.Minute,
			MaxHeaderBytes:    64 << 10,
		}
		sseServer := server.NewSSEServer(s,
			server.WithBaseURL(baseURL),
			server.WithHTTPServer(httpServer),
		)
		httpServer.Handler = http.MaxBytesHandler(authenticatedMCPHandler(sseServer, googleIdentityTokenValidator{}, authCfg, log), 2<<20)
		log.Info("aura-tracker-gcp starting", "transport", "sse", "version", version, "addr", listenAddr, "base_url", baseURL, "auth_mode", authCfg.Mode)

		sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()

		serverErr := make(chan error, 1)
		go func() { serverErr <- sseServer.Start(listenAddr) }()

		select {
		case <-sigCtx.Done():
			log.Info("shutdown signal received; draining connections")
		case err := <-serverErr:
			if err == nil {
				return errors.New("SSE server stopped unexpectedly")
			}
			if !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("server error: %w", err)
			}
			return nil
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := sseServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}

	case "", "stdio":
		log.Info("aura-tracker-gcp starting", "transport", "stdio", "version", version)
		if err := server.ServeStdio(s); err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	default:
		return fmt.Errorf("MCP_TRANSPORT must be %q or %q", "stdio", "sse")
	}
	return nil
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
	case anonymize.ModeLocal:
		scrubber, err := anonymize.NewLocalScrubber(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("init local scrubber: %w", err)
		}
		return scrubber, func() {}, nil

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

	default:
		return nil, nil, fmt.Errorf("unsupported anonymization mode %q", cfg.Mode)
	}
}

// logCredentialSource logs which broad ADC source was detected without exposing
// local credential paths or credential contents.
func logCredentialSource(log *slog.Logger, creds *google.Credentials) {
	switch {
	case os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "":
		log.Info("adc: explicit credential file configured")
	case len(creds.JSON) > 0:
		log.Info("adc: user credentials (gcloud application-default login)")
	default:
		log.Info("adc: workload identity / GCE metadata server")
	}
}

// parseModulesFlag converts the --modules flag value to a validated module set.
func parseModulesFlag(val string) (map[string]bool, error) {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil, nil
	}
	if strings.EqualFold(val, "none") {
		return map[string]bool{}, nil
	}
	known := make(map[string]bool, len(mcpserver.AllModules))
	for _, module := range mcpserver.AllModules {
		known[module] = true
	}
	known[mcpserver.ModuleRecommenderExport] = true
	out := make(map[string]bool)
	for _, m := range strings.Split(val, ",") {
		m = strings.TrimSpace(strings.ToLower(m))
		if m == "" {
			return nil, errors.New("--modules contains an empty module name")
		}
		if !known[m] {
			return nil, fmt.Errorf("unknown module %q; valid modules: %s", m, strings.Join(mcpserver.AllModules, ","))
		}
		out[m] = true
	}
	return out, nil
}

func readBoolEnv(name string, defaultValue bool, getenv func(string) string) (bool, error) {
	value := strings.ToLower(strings.TrimSpace(getenv(name)))
	if value == "" {
		return defaultValue, nil
	}
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be 'true' or 'false'", name)
	}
}
