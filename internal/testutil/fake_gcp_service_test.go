package testutil_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// Compile-time assertion: FakeGCPService satisfies ports.GCPService.
var _ ports.GCPService = (*testutil.FakeGCPService)(nil)

// TestFakeGCPServiceOverridesEveryPortMethod guards the drift the compile-time
// assertion cannot see: a new port method whose fake has no <Method>Func
// field, or a fake method that ignores its field and always returns zero
// values (ExportRecommendationsToBQ did).
func TestFakeGCPServiceOverridesEveryPortMethod(t *testing.T) {
	port := reflect.TypeOf((*ports.GCPService)(nil)).Elem()
	for i := range port.NumMethod() {
		method := port.Method(i)
		t.Run(method.Name, func(t *testing.T) {
			fake := &testutil.FakeGCPService{}
			field := reflect.ValueOf(fake).Elem().FieldByName(method.Name + "Func")
			if !field.IsValid() {
				t.Fatalf("FakeGCPService has no %sFunc field", method.Name)
			}
			if field.Type() != method.Type {
				t.Fatalf("FakeGCPService.%sFunc has type %s, want %s", method.Name, field.Type(), method.Type)
			}
			called := false
			field.Set(reflect.MakeFunc(method.Type, func([]reflect.Value) []reflect.Value {
				called = true
				return zeroResults(method.Type)
			}))
			args := []reflect.Value{reflect.ValueOf(context.Background()), reflect.New(method.Type.In(1)).Elem()}
			reflect.ValueOf(fake).MethodByName(method.Name).Call(args)
			if !called {
				t.Fatalf("FakeGCPService.%s does not call %sFunc", method.Name, method.Name)
			}
		})
	}
}

func zeroResults(fn reflect.Type) []reflect.Value {
	out := make([]reflect.Value, fn.NumOut())
	for i := range out {
		out[i] = reflect.Zero(fn.Out(i))
	}
	return out
}
