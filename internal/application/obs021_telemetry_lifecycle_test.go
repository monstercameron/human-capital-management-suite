package application

import (
	"testing"
	"time"

	telemetrylifecycle "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/lifecycle"
)

func TestTodo_OBS_021_Served(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())

	component, ok := composed.Graph().Component(ComponentTelemetryLifecycle)
	if !ok {
		t.Fatalf("served graph has no %q component", ComponentTelemetryLifecycle)
	}
	if component.Kind != KindAdapter || component.Impl != "*lifecycle.Store" {
		t.Fatalf("telemetry lifecycle component = %+v, want *lifecycle.Store adapter", component)
	}
	if got := composed.TelemetryLifecycle(); got == nil {
		t.Fatal("served composition exposed no telemetry lifecycle store")
	}

	store := composed.TelemetryLifecycle()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	for _, copy := range []telemetrylifecycle.Copy{
		{ID: "tenant-a-log", TenantToken: "tenant-a", Kind: telemetrylifecycle.KindLogs, Role: telemetrylifecycle.RolePrimary, Backend: "logs", Region: "us-east", Digest: "digest-a", RetainUntil: now.Add(time.Hour), DeleteCapable: true, LastVerified: now},
		{ID: "tenant-b-log", TenantToken: "tenant-b", Kind: telemetrylifecycle.KindLogs, Role: telemetrylifecycle.RolePrimary, Backend: "logs", Region: "us-east", Digest: "digest-b", RetainUntil: now.Add(time.Hour), DeleteCapable: true, LastVerified: now},
	} {
		if err := store.Register(copy); err != nil {
			t.Fatalf("Register(%s): %v", copy.ID, err)
		}
	}
	got, err := store.Query(telemetrylifecycle.Query{TenantToken: "tenant-a", Purpose: "incident", Kind: telemetrylifecycle.KindLogs, Region: "us-east", AsOf: now})
	if err != nil || len(got) != 1 || got[0].ID != "tenant-a-log" {
		t.Fatalf("served tenant-scoped query = %+v, %v", got, err)
	}
	expired, err := store.Query(telemetrylifecycle.Query{TenantToken: "tenant-a", Purpose: "incident", Kind: telemetrylifecycle.KindLogs, Region: "us-east", AsOf: now.Add(2 * time.Hour)})
	if err != nil || len(expired) != 0 {
		t.Fatalf("served expired query = %+v, %v", expired, err)
	}
}
