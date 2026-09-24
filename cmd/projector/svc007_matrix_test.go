package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func TestTodo_SVC_007_Mutation(t *testing.T) {
	noEnv := func(string) (string, bool) { return "", false }
	fields := projectorConfigFields()
	for _, args := range [][]string{{"-database-url=postgres://x", "-rebuild=false"}, {"-database-url=postgres://x", "-rebuild=true"}} {
		v, err := parseProjectorValues(args, noEnv, fields)
		if err != nil {
			t.Fatal(err)
		}
		rebuild, err := v.Bool("rebuild")
		if err != nil {
			t.Fatal(err)
		}
		if rebuild != (args[1] == "-rebuild=true") {
			t.Errorf("rebuild config for %v = %v", args, rebuild)
		}
	}
}

func parseProjectorValues(args []string, env func(string) (string, bool), fields []bootstrap.Field) (*bootstrap.Values, error) {
	return bootstrap.ParseConfig(args, env, fields)
}

func TestTodo_SVC_007_Race(t *testing.T) {
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fields := projectorConfigFields()
			if len(fields) != 4 {
				t.Errorf("config fields count = %d", len(fields))
			}
			for _, f := range fields {
				if f.Name == "database-url" && !f.Secret {
					t.Error("database URL lost secret marking")
				}
			}
		}()
	}
	wg.Wait()
}

type recoveryLister struct {
	calls  atomic.Int32
	cancel context.CancelFunc
	target projection.StreamProjection
}

func (l *recoveryLister) Due(context.Context) ([]projection.StreamProjection, error) {
	if l.calls.Add(1) == 1 {
		return nil, errors.New("temporary read failure")
	}
	return []projection.StreamProjection{l.target}, nil
}

type recoveryReconciler struct{ l *recoveryLister }

func (r recoveryReconciler) ReconcileOne(context.Context, projection.StreamProjection) (int, error) {
	r.l.cancel()
	return 1, nil
}

func TestTodo_SVC_007_Recovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	l := &recoveryLister{cancel: cancel, target: projection.StreamProjection{Tenant: uuid.New(), ProjectionName: "people", StreamKey: "tenant"}}
	err := runReconcileLoop(ctx, discardLogger(), l, recoveryReconciler{l}, time.Millisecond)
	if err != nil {
		t.Fatalf("reconcile loop after recovery: %v", err)
	}
	if got := l.calls.Load(); got != 2 {
		t.Fatalf("list calls = %d, want transient failure then recovered sweep", got)
	}
}

func TestTodo_SVC_007_Security(t *testing.T) {
	fields := projectorConfigFields()
	v, err := bootstrap.ParseConfig([]string{"-database-url=postgres://alice:secret@db/app"}, func(string) (string, bool) { return "", false }, fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(v); err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(v.LogAttrs()); i += 2 {
		attrs := v.LogAttrs()
		if attrs[i] == "database-url" && attrs[i+1] == bootstrap.RedactedValue {
			return
		}
	}
	t.Fatal("projector database credential appeared unredacted in log attributes")
}
