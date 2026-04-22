package gcp

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/logging/logadmin"
	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) QueryRecentLogs(ctx context.Context, req models.QueryRecentLogsRequest) (models.QueryRecentLogsResponse, error) {
	if err := a.rateWait(ctx, "logging.QueryRecentLogs"); err != nil {
		return models.QueryRecentLogsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	if req.LookbackMinutes <= 0 {
		req.LookbackMinutes = 60
	}
	if req.MaxEntries <= 0 {
		req.MaxEntries = 50
	}

	since := time.Now().Add(-time.Duration(req.LookbackMinutes) * time.Minute).UTC()
	filter := fmt.Sprintf(
		`resource.type="%s" AND resource.labels.cluster_name="%s" AND severity>="%s" AND timestamp>="%s"`,
		req.ResourceType,
		req.ResourceName,
		req.MinSeverity,
		since.Format(time.RFC3339),
	)

	it := a.logAdmin.Entries(ctx,
		logadmin.Filter(filter),
		logadmin.NewestFirst(),
		logadmin.PageSize(int32(req.MaxEntries+1)), // fetch one extra to detect truncation
	)

	var entries []models.LogEntry
	truncated := false
	for {
		entry, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return models.QueryRecentLogsResponse{}, wrapGCPError("logging.QueryRecentLogs", err)
		}

		if len(entries) >= req.MaxEntries {
			truncated = true
			break
		}

		msg := fmt.Sprintf("%v", entry.Payload)
		severity := entry.Severity.String()
		ts := ""
		if !entry.Timestamp.IsZero() {
			ts = entry.Timestamp.UTC().Format(time.RFC3339)
		}

		entries = append(entries, models.LogEntry{
			Timestamp: ts,
			Severity:  severity,
			Message:   msg,
			Labels:    entry.Labels,
		})
	}
	if entries == nil {
		entries = []models.LogEntry{}
	}
	return models.QueryRecentLogsResponse{
		Entries:      entries,
		TotalFetched: len(entries),
		Truncated:    truncated,
	}, nil
}
