package config

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_INTAPI_012(t *testing.T) {
	store := NewConfigurationStore(func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	created, err := store.Create(ConfigurationMutation{TenantID: "tenant-a", ID: "workflow-1", Kind: ConfigWorkflow, Value: []byte("v1")})
	if err != nil || created.Revision != 1 || created.Status != ConfigActive {
		t.Fatalf("create = %+v, err=%v", created, err)
	}
	updated, err := store.Update(ConfigurationMutation{TenantID: "tenant-a", ID: "workflow-1", Kind: ConfigWorkflow, Value: []byte("v2"), IfMatch: 1, HighImpact: true})
	if err != nil || updated.Revision != 2 || updated.Status != ConfigPendingApproval {
		t.Fatalf("update = %+v, err=%v", updated, err)
	}
	retired, err := store.Retire("tenant-a", "workflow-1", 2)
	if err != nil || retired.Status != ConfigRetired || retired.Revision != 3 {
		t.Fatalf("retire = %+v, err=%v", retired, err)
	}
}

func TestTodo_INTAPI_012_Security(t *testing.T) {
	store := NewConfigurationStore(nil)
	created, err := store.Create(ConfigurationMutation{TenantID: "tenant-a", ID: "secret-1", Kind: ConfigSecret, Value: []byte("top-secret"), Secret: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Value) != 0 {
		t.Fatal("secret was readable after create")
	}
	if _, err := store.Update(ConfigurationMutation{TenantID: "tenant-a", ID: "secret-1", Kind: ConfigSecret, Value: []byte("next"), Secret: true, IfMatch: 0}); !errors.Is(err, ErrConfigRevision) {
		t.Fatalf("missing If-Match = %v", err)
	}
	if _, err := store.Get("tenant-b", "secret-1"); !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("cross-tenant get = %v", err)
	}
}

func TestTodo_INTAPI_012_Integration(t *testing.T) {
	store := NewConfigurationStore(nil)
	for _, kind := range []ConfigurationKind{ConfigCustomObjectType, ConfigCustomObject, ConfigTenantParameter, ConfigSecret, ConfigWorkflow, ConfigRole, ConfigRoleBinding, ConfigConnector, ConfigConnection, ConfigJobArchitecture, ConfigReportDefinition} {
		if _, err := store.Create(ConfigurationMutation{TenantID: "tenant-a", ID: string(kind), Kind: kind, Value: []byte("definition")}); err != nil {
			t.Fatalf("create %s: %v", kind, err)
		}
	}
	if got := len(store.List("tenant-a", "")); got != 11 {
		t.Fatalf("list count = %d, want 11", got)
	}
}

func TestTodo_INTAPI_012_Property(t *testing.T) {
	store := NewConfigurationStore(nil)
	created, err := store.Create(ConfigurationMutation{TenantID: "tenant-a", ID: "parameter-1", Kind: ConfigTenantParameter, Value: []byte("one")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(ConfigurationMutation{TenantID: "tenant-a", ID: created.ID, Kind: created.Kind, Value: []byte("stale"), IfMatch: created.Revision - 1}); !errors.Is(err, ErrConfigRevision) {
		t.Fatalf("stale update = %v", err)
	}
	current, err := store.Get("tenant-a", created.ID)
	if err != nil || string(current.Value) != "one" || current.Revision != 1 {
		t.Fatalf("stale update changed history: %+v, %v", current, err)
	}
}
