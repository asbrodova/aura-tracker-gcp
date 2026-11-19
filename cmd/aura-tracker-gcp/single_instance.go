package main

import "log/slog"

// sseSingleInstanceMessage is logged when the SSE transport starts. Live SSE
// streams, approval plans, rate limiters, quota gates and caches are all held
// in this process, so a deployment must run exactly one instance and send
// every message of a session to it.
const sseSingleInstanceMessage = "SSE state (live sessions, approval plans, rate limits, quota gates, caches) is held in this process: run exactly one instance and route every session to it"

// sseCloudRunFlags are the Cloud Run settings the single-instance deployment
// recipe requires.
const sseCloudRunFlags = "--max-instances=1 --session-affinity --timeout=3600"

// sseStateNotice returns the startup notice for the SSE transport. On Cloud
// Run (K_SERVICE is set) it is a warning that names the required flags,
// because scaling out or routing a session to another instance breaks it.
func sseStateNotice(getenv func(string) string) (slog.Level, string, []any) {
	service := getenv("K_SERVICE")
	if service == "" {
		return slog.LevelInfo, sseSingleInstanceMessage, nil
	}
	return slog.LevelWarn, sseSingleInstanceMessage, []any{
		"cloud_run_service", service,
		"cloud_run_revision", getenv("K_REVISION"),
		"required_flags", sseCloudRunFlags,
	}
}
