package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/oauth2/google"

	gcpadapter "github.com/asbrodova/aura-tracker-gcp/internal/gcp"
	mcpserver "github.com/asbrodova/aura-tracker-gcp/internal/mcp"
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

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		fmt.Fprintln(os.Stderr, "aura-tracker-gcp: GCP_PROJECT_ID environment variable is required")
		os.Exit(1)
	}

	ctx := context.Background()

	if _, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform"); err != nil {
		fmt.Fprintln(os.Stderr, `aura-tracker-gcp: no GCP credentials found.

Run:  gcloud auth application-default login

Or set GOOGLE_APPLICATION_CREDENTIALS to a service account key file.`)
		os.Exit(1)
	}

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

	s := mcpserver.New(svc, log, version)

	log.Info("aura-tracker-gcp starting", "transport", "stdio", "version", version)
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "aura-tracker-gcp: server error: %v\n", err)
		os.Exit(1)
	}
}
