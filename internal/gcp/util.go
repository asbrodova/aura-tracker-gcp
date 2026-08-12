package gcp

import (
	"errors"
	"fmt"
	"sort"

	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	defaultInventoryPageSize = 250
	maxInventoryPageSize     = 1000
	maxUnpagedInventoryItems = 1000
)

var errInventoryLimitReached = errors.New("inventory result limit reached")

// appendInventoryBounded appends an item when capacity remains. A false return
// means the item was not appended, proving that at least one result was omitted.
// This distinction avoids reporting an exactly-full result as truncated.
func appendInventoryBounded[T any](values *[]T, value T) bool {
	if len(*values) >= maxUnpagedInventoryItems {
		return false
	}
	*values = append(*values, value)
	return true
}

func inventoryLimitResult(err error) (truncated bool, remaining error) {
	if errors.Is(err, errInventoryLimitReached) {
		return true, nil
	}
	return false, err
}

func regionalInventoryLimit(regionCount int) int {
	if regionCount <= 1 {
		return maxUnpagedInventoryItems
	}
	limit := maxUnpagedInventoryItems / regionCount
	if limit < 1 {
		return 1
	}
	return limit
}

func inventoryPageSize(value int) (int, error) {
	if value == 0 {
		return defaultInventoryPageSize, nil
	}
	if value < 1 || value > maxInventoryPageSize {
		return 0, fmt.Errorf("page_size must be between 1 and %d", maxInventoryPageSize)
	}
	return value, nil
}

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
