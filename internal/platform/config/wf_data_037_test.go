package config

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var wfData037StringType = workflow.ValueType{Kind: workflow.KindString}

var wfData037Consumer = Consumer{Kind: ConsumerWorkflow, ID: "workflow.payroll"}

func wfData037Snapshot(t *testing.T, required bool) Snapshot {
	t.Helper()
	definition := ParameterDefinition{
		Key: "payroll.currency", Type: wfData037StringType,
		Classification: "TENANT_OPERATIONAL", Owner: "payroll-platform", Required: required,
		AllowedConsumers: []Consumer{wfData037Consumer},
	}
	snapshot, err := NewSnapshotWithDefinitions("tenant-config", "v7", nil, []ParameterDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestTodo_WF_DATA_037(t *testing.T) {
	store := NewParameterValueStore()
	snapshot := wfData037Snapshot(t, true)
	path := ParameterScopePath{{Kind: ScopeTenant, ID: "tenant-1"}, {Kind: ScopeCompany, ID: "company-1"}}
	root := ParameterValueChange{
		Key: "payroll.currency", Scope: path[0], Environment: EnvironmentProduction,
		Value: "USD", ValueType: wfData037StringType, Author: "admin@example.test", Reason: "Initial tenant setup",
		RecordedAt: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC), Path: path[:1],
	}
	first, err := store.Append(snapshot, root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || first.Author != root.Author || first.Reason != root.Reason || first.DefinitionVersion != "v7" {
		t.Fatalf("first authored revision metadata = %+v", first)
	}
	company := ParameterValueChange{
		Key: root.Key, Scope: path[1], Environment: root.Environment,
		Value: "CAD", ValueType: wfData037StringType, ExpectedRevision: 0,
		Author: "company-admin@example.test", Reason: "Company payroll currency",
		RecordedAt: root.RecordedAt.Add(time.Minute), Path: path,
	}
	second, err := store.Append(snapshot, company)
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 1 {
		t.Fatalf("first company revision = %d, want 1", second.Revision)
	}
	resolved, err := store.Resolve(snapshot, root.Key, path, root.Environment, wfData037Consumer, wfData037StringType)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Found || resolved.Value != "CAD" || resolved.Source != path[1] || resolved.Revision != 1 || len(resolved.Overridden) != 1 || resolved.Overridden[0] != path[0] {
		t.Fatalf("resolved override = %+v", resolved)
	}
	root.ExpectedRevision = 1
	root.Value = "EUR"
	root.RecordedAt = company.RecordedAt.Add(time.Minute)
	if _, err := store.Append(snapshot, root); err != nil {
		t.Fatalf("second tenant revision: %v", err)
	}
	history := store.Revisions(root.Key, path[0], root.Environment)
	if len(history) != 2 || history[0].Revision != 1 || history[1].Revision != 2 || history[1].Author != root.Author || history[1].Reason != root.Reason {
		t.Fatalf("tenant revision history = %+v", history)
	}
}

func TestTodo_WF_DATA_037_Property(t *testing.T) {
	snapshot := wfData037Snapshot(t, true)
	root := ParameterScope{Kind: ScopeTenant, ID: "tenant-1"}
	company := ParameterScope{Kind: ScopeCompany, ID: "company-1"}
	entity := ParameterScope{Kind: ScopeLegalEntity, ID: "entity-1"}
	paths := []ParameterScopePath{
		{root}, {root, company}, {root, company, entity},
	}
	for i, path := range paths {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			store := NewParameterValueStore()
			for j, scope := range path {
				change := ParameterValueChange{
					Key: "payroll.currency", Scope: scope, Environment: EnvironmentProduction,
					Value: []string{"USD", "CAD", "EUR"}[j], ValueType: wfData037StringType,
					Author: "admin@example.test", Reason: "Path override",
					RecordedAt: time.Date(2026, 3, 10, 12, j, 0, 0, time.UTC), Path: path[:j+1],
				}
				if _, err := store.Append(snapshot, change); err != nil {
					t.Fatal(err)
				}
			}
			resolved, err := store.Resolve(snapshot, "payroll.currency", path, EnvironmentProduction, wfData037Consumer, wfData037StringType)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"USD", "CAD", "EUR"}[len(path)-1]
			if resolved.Value != want || resolved.Source != path[len(path)-1] || len(resolved.Overridden) != len(path)-1 {
				t.Fatalf("resolution = %+v, want deepest value %q", resolved, want)
			}
		})
	}
	store := NewParameterValueStore()
	locked := ParameterValueChange{
		Key: "payroll.currency", Scope: root, Environment: EnvironmentProduction,
		Value: "USD", ValueType: wfData037StringType, Author: "admin@example.test", Reason: "Central policy",
		RecordedAt: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC), Locked: true, Path: ParameterScopePath{root},
	}
	if _, err := store.Append(snapshot, locked); err != nil {
		t.Fatal(err)
	}
	child := ParameterValueChange{
		Key: locked.Key, Scope: company, Environment: locked.Environment,
		Value: "CAD", ValueType: wfData037StringType, Author: locked.Author, Reason: "Attempted override",
		RecordedAt: locked.RecordedAt.Add(time.Minute), Path: ParameterScopePath{root, company},
	}
	if _, err := store.Append(snapshot, child); !errors.Is(err, ErrParameterScopeLocked) {
		t.Fatalf("append below locked ancestor error = %v, want ErrParameterScopeLocked", err)
	}
	resolved, err := store.Resolve(snapshot, locked.Key, ParameterScopePath{root, company}, locked.Environment, wfData037Consumer, wfData037StringType)
	if err != nil || resolved.Value != "USD" || resolved.Source != root {
		t.Fatalf("resolve below locked ancestor = %+v, %v; want inherited USD", resolved, err)
	}
	child.Scope = company
	child.ValueType = workflow.ValueType{Kind: workflow.KindBool}
	child.RecordedAt = locked.RecordedAt.Add(2 * time.Minute)
	if _, err := NewParameterValueStore().Append(snapshot, child); !errors.Is(err, ErrParameterValueTypeMismatch) {
		t.Fatalf("wrong declared value type error = %v, want ErrParameterValueTypeMismatch", err)
	}
}

func TestTodo_WF_DATA_037_Security(t *testing.T) {
	store := NewParameterValueStore()
	snapshot := wfData037Snapshot(t, true)
	scope := ParameterScope{Kind: ScopeTenant, ID: "tenant-1"}
	path := ParameterScopePath{scope}
	production := ParameterValueChange{
		Key: "payroll.currency", Scope: scope, Environment: EnvironmentProduction,
		Value: "USD", ValueType: wfData037StringType, Author: "admin@example.test", Reason: "Production value",
		RecordedAt: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC), Path: path,
	}
	if _, err := store.Append(snapshot, production); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(snapshot, production.Key, path, EnvironmentSandbox, wfData037Consumer, wfData037StringType); !errors.Is(err, ErrParameterValueMissing) {
		t.Fatalf("sandbox read error = %v, want ErrParameterValueMissing", err)
	}
	sandbox := production
	sandbox.Environment = EnvironmentSandbox
	sandbox.Value = "TEST"
	sandbox.Author = "sandbox-admin@example.test"
	sandbox.Reason = "Sandbox-only test value"
	sandbox.RecordedAt = production.RecordedAt.Add(time.Minute)
	if _, err := store.Append(snapshot, sandbox); err != nil {
		t.Fatal(err)
	}
	for env, want := range map[ParameterEnvironment]string{EnvironmentProduction: "USD", EnvironmentSandbox: "TEST"} {
		resolved, err := store.Resolve(snapshot, production.Key, path, env, wfData037Consumer, wfData037StringType)
		if err != nil {
			t.Fatalf("resolve %s: %v", env, err)
		}
		if resolved.Value != want || resolved.Environment != env {
			t.Errorf("resolve %s = %+v, want isolated value %q", env, resolved, want)
		}
	}
	if _, err := store.Resolve(snapshot, production.Key, path, EnvironmentProduction, Consumer{Kind: ConsumerWorkflow, ID: "workflow.unlisted"}, wfData037StringType); !errors.Is(err, ErrParameterConsumerDenied) {
		t.Fatalf("unauthorized consumer error = %v, want ErrParameterConsumerDenied", err)
	}
}
