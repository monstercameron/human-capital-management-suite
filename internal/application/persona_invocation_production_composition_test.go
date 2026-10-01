package application

import (
	"errors"
	"testing"
)

func TestNewDatabasePersonaInvocationProductionRuntime_FailsClosedWithoutServedDependencies(t *testing.T) {
	if _, err := NewDatabasePersonaInvocationProductionRuntime(PersonaInvocationProductionConfig{}); !errors.Is(err, errPersonaInvocationProductionComposition) {
		t.Fatalf("error=%v; want missing production composition error", err)
	}
}
