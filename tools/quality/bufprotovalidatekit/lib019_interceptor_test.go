package bufprotovalidatekit_test

import (
	"reflect"
	"testing"

	kit "github.com/monstercameron/human-capital-management-suite/tools/quality/bufprotovalidatekit"
)

func TestTodo_LIB_019_InterceptorBindsImmutableMethodValidators(t *testing.T) {
	validator, descriptor := fixtureValidator(t)
	bindings := map[string]*kit.Validator{"/qualification.v1.Request/Validate": validator}
	interceptor, err := kit.NewInterceptor(bindings)
	if err != nil {
		t.Fatalf("NewInterceptor: %v", err)
	}
	bindings["/qualification.v1.Request/Other"] = validator

	if got, want := interceptor.Methods(), []string{"/qualification.v1.Request/Validate"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("methods=%v want=%v", got, want)
	}
	invalid := requestMessage(descriptor, "", "123456789", "type.googleapis.com/unregistered.Payload", []byte{1})
	first := interceptor.Validate("/qualification.v1.Request/Validate", invalid)
	second := interceptor.Validate("/qualification.v1.Request/Validate", invalid)
	if !reflect.DeepEqual(first, second) || len(first) == 0 {
		t.Fatalf("interceptor results are not stable: first=%+v second=%+v", first, second)
	}
	if got := interceptor.Validate("/qualification.v1.Request/Other", invalid); got != nil {
		t.Fatalf("mutated caller bindings changed published interceptor: %+v", got)
	}
}

func TestTodo_LIB_019_InterceptorRejectsInvalidBindings(t *testing.T) {
	if _, err := kit.NewInterceptor(map[string]*kit.Validator{" ": nil}); err == nil {
		t.Fatal("invalid method and validator binding was accepted")
	}
}
