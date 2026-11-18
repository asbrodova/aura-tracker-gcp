package safety

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// TestSafetyDecoratorCoversEveryPortMethod checks all ~84 hand-written
// methods against the port, by reflection, so a new method cannot slip in
// unguarded:
//   - a method whose request has a DryRun bool is a mutation: with neither
//     dry_run nor a valid confirm_plan_id it must return
//     ConfirmationRequiredError without calling the inner service;
//   - every other method must pass straight through to the inner method of
//     the same name, exactly once.
func TestSafetyDecoratorCoversEveryPortMethod(t *testing.T) {
	port := reflect.TypeOf((*ports.GCPService)(nil)).Elem()
	mutations := 0
	for i := range port.NumMethod() {
		method := port.Method(i)
		reqType := method.Type.In(1)
		dryRun, hasDryRun := reqType.FieldByName("DryRun")
		isMutation := hasDryRun && dryRun.Type.Kind() == reflect.Bool
		if isMutation {
			mutations++
		}
		t.Run(method.Name, func(t *testing.T) {
			inner, calls := recordingFakeService()
			decorator := reflect.ValueOf(NewSafetyDecorator(inner, slog.New(slog.DiscardHandler))).MethodByName(method.Name)
			call := func(req reflect.Value) error {
				out := decorator.Call([]reflect.Value{reflect.ValueOf(context.Background()), req})
				err, _ := out[len(out)-1].Interface().(error)
				return err
			}
			req := reflect.New(reqType).Elem()

			if !isMutation {
				if err := call(req); err != nil {
					t.Fatalf("pass-through returned %v", err)
				}
				if got := calls(); !slices.Equal(got, []string{method.Name}) {
					t.Fatalf("SafetyDecorator.%s called inner %v, want [%s]", method.Name, got, method.Name)
				}
				return
			}

			assertRefused := func(when string) {
				t.Helper()
				var confirm *ports.ConfirmationRequiredError
				if err := call(req); !errors.As(err, &confirm) {
					t.Fatalf("%s: error = %v, want ConfirmationRequiredError", when, err)
				}
				if got := calls(); len(got) != 0 {
					t.Fatalf("%s: inner service was called: %v", when, got)
				}
			}
			assertRefused("without dry_run or confirm_plan_id")
			confirmPlanID := req.FieldByName("ConfirmPlanID")
			if !confirmPlanID.IsValid() || confirmPlanID.Kind() != reflect.String {
				t.Fatalf("mutation request %s has DryRun but no ConfirmPlanID string field", reqType)
			}
			confirmPlanID.SetString("unknown-plan")
			assertRefused("with an unknown confirm_plan_id")
		})
	}
	// ScaleDeployment, UpdateTraffic, ExportRecommendationsToBQ. If this
	// changes, update the mutation list in CLAUDE.md, CONTRIBUTING.md and the
	// wiki's Security-and-Safety page.
	if mutations != 3 {
		t.Errorf("found %d mutation methods (request has DryRun), want 3", mutations)
	}
}

// recordingFakeService returns a FakeGCPService whose every Func field
// records the method name and returns zero values.
func recordingFakeService() (*testutil.FakeGCPService, func() []string) {
	var (
		mu    sync.Mutex
		calls []string
	)
	fake := &testutil.FakeGCPService{}
	fields := reflect.ValueOf(fake).Elem()
	for i := range fields.NumField() {
		field := fields.Field(i)
		fieldType := fields.Type().Field(i)
		if field.Kind() != reflect.Func || !strings.HasSuffix(fieldType.Name, "Func") {
			continue
		}
		name := strings.TrimSuffix(fieldType.Name, "Func")
		fn := field.Type()
		field.Set(reflect.MakeFunc(fn, func([]reflect.Value) []reflect.Value {
			mu.Lock()
			calls = append(calls, name)
			mu.Unlock()
			out := make([]reflect.Value, fn.NumOut())
			for j := range out {
				out[j] = reflect.Zero(fn.Out(j))
			}
			return out
		}))
	}
	return fake, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), calls...)
	}
}
