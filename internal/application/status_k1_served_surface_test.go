package application

import (
	"testing"
	"time"

	statuscontract "github.com/monstercameron/human-capital-management-suite/internal/experience/status"
)

func TestTodo_STATUS_001_Served(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	surface := NewServedStatusSurface()
	got, err := surface.Present(
		statuscontract.Request{TenantID: "tenant-a", Authorization: statuscontract.AuthorizationCurrent, Now: now, MaxAge: 5 * time.Minute},
		[]statuscontract.Service{
			{ID: "api", TenantID: "tenant-a", State: statuscontract.Degraded, UpdatedAt: now},
			{ID: "private-api", TenantID: "tenant-b", State: statuscontract.Outage, UpdatedAt: now},
		},
		[]statuscontract.Incident{
			{ID: "incident-a", TenantID: "tenant-a", ServiceID: "api", State: statuscontract.Degraded, UpdatedAt: now},
			{ID: "incident-b", TenantID: "tenant-b", ServiceID: "private-api", State: statuscontract.Outage, UpdatedAt: now},
		},
		[]statuscontract.Advisory{
			{ID: "advisory-a", TenantID: "tenant-a", IncidentID: "incident-a", ServiceID: "api", UpdatedAt: now},
			{ID: "advisory-b", TenantID: "tenant-b", IncidentID: "incident-b", ServiceID: "private-api", UpdatedAt: now},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tenant-a" || got.Freshness != statuscontract.FreshnessCurrent || len(got.Services) != 1 || len(got.Incidents) != 1 || len(got.Advisories) != 1 {
		t.Fatalf("served status projection=%+v", got)
	}
	if got.Incidents[0].ID != "incident-a" || got.Advisories[0].IncidentID != "incident-a" {
		t.Fatalf("served incident/advisory projection=%+v", got)
	}

	if entry, ok := surface.Lookup(string(statuscontract.Degraded)); !ok || entry.Label != "Degraded" {
		t.Fatalf("served status registry lookup=%+v ok=%v", entry, ok)
	}
	if len(surface.RegistryMatrix()) != 5 {
		t.Fatalf("served status registry length=%d", len(surface.RegistryMatrix()))
	}
}

func TestTodo_STATUS_001_ServedNilApp(t *testing.T) {
	if got := (*App)(nil).Status(); got.Present != nil || got.Lookup != nil || got.RegistryMatrix != nil {
		t.Fatalf("nil app exposed status capability=%+v", got)
	}
	if got := (&App{}).Status(); got.Present == nil || got.Lookup == nil || got.RegistryMatrix == nil {
		t.Fatalf("served app omitted status capability=%+v", got)
	}
}
