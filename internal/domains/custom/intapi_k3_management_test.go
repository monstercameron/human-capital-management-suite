package custom

import (
	"context"
	"errors"
	"testing"
)

func intapiK3Definition(version uint64) CustomObjectDefinition {
	return CustomObjectDefinition{Kind: "Asset", Namespace: "tenant", Version: version, Fields: map[string]FieldDefinition{"label": {Type: "string", Classification: FieldClassification{AuthZDomain: "custom.read", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}}}}
}

func TestTodo_INTAPI_012_CustomDefinitions(t *testing.T) {
	manager := NewDefinitionManager(nil)
	if _, err := manager.Create(context.Background(), "tenant-a", intapiK3Definition(1)); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(context.Background(), "tenant-a", intapiK3Definition(2), 1)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update = %+v, err=%v", updated, err)
	}
	if err := manager.Retire("tenant-a", "Asset", "tenant", 2); err != nil || !manager.IsRetired("tenant-a", "Asset", "tenant") {
		t.Fatalf("retire err=%v", err)
	}
}

func TestTodo_INTAPI_012_CustomDefinitions_Security(t *testing.T) {
	manager := NewDefinitionManager(nil)
	if _, err := manager.Create(context.Background(), "tenant-a", intapiK3Definition(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(context.Background(), "tenant-a", intapiK3Definition(2), 0); !errors.Is(err, ErrManagementRevision) {
		t.Fatalf("missing If-Match = %v", err)
	}
	if _, err := manager.Get("tenant-b", "Asset", "tenant", 1); err == nil {
		t.Fatal("cross-tenant definition was visible")
	}
}

func TestTodo_INTAPI_012_CustomDefinitions_Property(t *testing.T) {
	manager := NewDefinitionManager(nil)
	if _, err := manager.Create(context.Background(), "tenant-a", intapiK3Definition(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(context.Background(), "tenant-a", intapiK3Definition(2), 9); !errors.Is(err, ErrManagementRevision) {
		t.Fatalf("stale revision = %v", err)
	}
	if got := len(manager.List("tenant-a", "Asset", "tenant")); got != 1 {
		t.Fatalf("history changed after stale update: %d", got)
	}
}
