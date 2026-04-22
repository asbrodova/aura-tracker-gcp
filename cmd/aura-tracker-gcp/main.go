package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/server"

	gcpadapter "github.com/asbrodova/aura-tracker-gcp/internal/gcp"
	mcpserver "github.com/asbrodova/aura-tracker-gcp/internal/mcp"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		fmt.Fprintln(os.Stderr, "aura-tracker-gcp: GCP_PROJECT_ID environment variable is required")
		os.Exit(1)
	}

	ctx := context.Background()

	svc, err := gcpadapter.New(ctx, projectID,
		gcpadapter.WithRateLimit(10, 20),
		gcpadapter.WithCallTimeout(30*time.Second),
		gcpadapter.WithLogger(log),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: init gcp adapter: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := svc.Close(); err != nil {
			log.Error("closing gcp adapter", "err", err)
		}
	}()

	s := mcpserver.New(svc, log)

	log.Info("aura-tracker-gcp starting", "transport", "stdio", "version", "0.1.0")
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: server error: %v\n", err)
		os.Exit(1)
	}
}
