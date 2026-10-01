package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func TestTodo_TCLOCK002_TimeMigrationIsExplicitAndIsolated(t *testing.T) {
	cfg := ServeConfig{DatabaseURL: "postgres://core:secret@localhost/core", Migrate: true}
	cfg.ClockRuntime = validClockRuntimeInput()
	cfg.ClockRuntime.CoreDatabaseURL = "postgres://spoof:secret@localhost/core"
	called := false
	wantErr := errors.New("migration refused")
	options := Options{}.Apply(WithTimeMigrator(func(_ context.Context, timeURL, coreURL, schema string, _ bootstrap.Logger) error {
		called = true
		if coreURL != cfg.DatabaseURL || timeURL != cfg.ClockRuntime.TimeDatabaseURL || schema != cfg.ClockRuntime.TimeSchema {
			t.Fatalf("migration received wrong isolated configuration")
		}
		return wantErr
	}))
	if err := applyClockMigrations(context.Background(), cfg, options, nil); !errors.Is(err, wantErr) || !called {
		t.Fatalf("migration result: called=%v err=%v", called, err)
	}
	if err := applyClockMigrations(context.Background(), cfg, Options{}, nil); err == nil {
		t.Fatal("enabled migration silently accepted missing adapter")
	}
	called = false
	cfg.Migrate = false
	if err := applyClockMigrations(context.Background(), cfg, options, nil); err != nil || called {
		t.Fatalf("disabled migration invoked adapter: %v", err)
	}
}
