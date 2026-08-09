package gcp

import (
	"sort"

	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// isIteratorDone reports whether err signals end-of-iteration from a GCP iterator.
func isIteratorDone(err error) bool {
	return err == iterator.Done
}

func sortToolErrors(errors []models.ToolError) {
	sort.Slice(errors, func(i, j int) bool {
		if errors[i].FailingAPI != errors[j].FailingAPI {
			return errors[i].FailingAPI < errors[j].FailingAPI
		}
		return errors[i].Message < errors[j].Message
	})
}

// isGRPCNotFound reports whether err is a gRPC NotFound status error.
func isGRPCNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}
