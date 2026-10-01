package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaBackgroundWorkloadTestLogger struct{ entries []string }

func (l *personaBackgroundWorkloadTestLogger) Error(message string, attributes ...any) {
	l.entries = append(l.entries, message+fmt.Sprint(attributes...))
}

func TestTodo_AGENTP_008_Recovery_BackgroundServedWorkloadScopesAndStops(t *testing.T) {
	if workload := personaInvocationBackgroundWorkload(nil, []string{"tenant-a"}, nil); workload != nil {
		t.Fatal("missing durable runtime produced workload")
	}
	if workload := personaInvocationBackgroundWorkload(&PersonaInvocationProductionRuntime{}, []string{"tenant-a"}, nil); workload != nil {
		t.Fatal("missing recovery produced workload")
	}
	bound := &PersonaInvocationProductionRuntime{Background: &PersonaBackgroundDispatcher{}}
	for _, tenants := range [][]string{nil, {""}, {" tenant-a"}} {
		if workload := personaInvocationBackgroundWorkload(bound, tenants, nil); workload != nil {
			t.Fatal("unbound tenant produced workload")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := &personaBackgroundWorkloadTestLogger{}
	var calls []string
	err := runPersonaInvocationBackgroundWorkload(ctx, []string{"tenant-a", "tenant-b"}, logger, func(received context.Context, tenant string, limit int) error {
		if received != ctx || limit != 16 {
			t.Fatal("workload changed context or tenant bound")
		}
		if _, ok := trust.FromContext(received); ok {
			t.Fatal("workload manufactured principal")
		}
		calls = append(calls, tenant)
		if tenant == "tenant-a" {
			return errors.New("private prompt and provider payload must never be logged")
		}
		cancel()
		return nil
	})
	if err != nil || !reflect.DeepEqual(calls, []string{"tenant-a", "tenant-b"}) {
		t.Fatalf("calls=%v err=%v", calls, err)
	}
	if len(logger.entries) != 1 || !strings.Contains(logger.entries[0], "tenant-a") || strings.Contains(logger.entries[0], "private prompt") || !strings.Contains(logger.entries[0], "error_type") {
		t.Fatalf("unsafe failure log: %v", logger.entries)
	}
	if err := runPersonaInvocationBackgroundWorkload(ctx, []string{"tenant-a"}, logger, func(context.Context, string, int) error { t.Fatal("canceled worker dispatched"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := runPersonaInvocationBackgroundWorkload(nil, nil, nil, nil); !errors.Is(err, errPersonaInvocationProductionComposition) {
		t.Fatal(err)
	}
}
