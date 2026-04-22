package gcp

import (
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// isIteratorDone reports whether err signals end-of-iteration from a GCP iterator.
func isIteratorDone(err error) bool {
	return err == iterator.Done
}

// isGRPCNotFound reports whether err is a gRPC NotFound status error.
func isGRPCNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}
