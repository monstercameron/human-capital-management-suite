package application

import (
	"context"
	"strings"
	"testing"
)

func TestTodo_TCLOCK002_ServeClockDisabledAndInvalidFailBeforeOpening(t *testing.T) {
	if runtime, err := composeConfiguredClock(context.Background(), ServeConfig{}, nil, Options{}); err != nil || runtime != nil {
		t.Fatalf("disabled clock runtime=%v error=%v", runtime, err)
	}
	_, err := composeConfiguredClock(context.Background(), ServeConfig{ClockRuntime: ClockRuntimeConfigInput{TimeDatabaseURL: "invalid"}}, nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "registry key") {
		t.Fatalf("invalid clock configuration did not fail before database opening: %v", err)
	}
}
