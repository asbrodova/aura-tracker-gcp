package gcp

import (
	"errors"
	"fmt"
	"testing"
)

func TestTestPermissionsInBatchesSplitsAtAPILimit(t *testing.T) {
	t.Parallel()
	permissions := make([]string, 230)
	for i := range permissions {
		permissions[i] = fmt.Sprintf("svc.resource.perm%03d", i)
	}
	var batchSizes []int
	allowed, err := testPermissionsInBatches(permissions, func(batch []string) ([]string, error) {
		batchSizes = append(batchSizes, len(batch))
		return []string{batch[0]}, nil // grant the first permission of each batch
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{100, 100, 30}; fmt.Sprint(batchSizes) != fmt.Sprint(want) {
		t.Fatalf("batch sizes = %v, want %v", batchSizes, want)
	}
	for _, granted := range []string{"svc.resource.perm000", "svc.resource.perm100", "svc.resource.perm200"} {
		if !allowed[granted] {
			t.Errorf("%s not reported as allowed", granted)
		}
	}
	if len(allowed) != 3 {
		t.Fatalf("allowed = %v, want exactly the 3 granted permissions", allowed)
	}
}

func TestTestPermissionsInBatchesPropagatesErrorsAndSkipsEmptyInput(t *testing.T) {
	t.Parallel()
	calls := 0
	if _, err := testPermissionsInBatches(nil, func([]string) ([]string, error) {
		calls++
		return nil, nil
	}); err != nil || calls != 0 {
		t.Fatalf("empty input: err=%v calls=%d, want no API call", err, calls)
	}

	wantErr := errors.New("permission denied")
	if _, err := testPermissionsInBatches([]string{"a.b.c"}, func([]string) ([]string, error) {
		return nil, wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
