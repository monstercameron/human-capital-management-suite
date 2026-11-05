package taskpool

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func strField(path string) workflow.Field {
	return workflow.Field{Path: path, Type: workflow.ValueType{Kind: workflow.KindString}}
}

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	catalog := NewCatalog()
	for _, entry := range []CatalogEntry{
		{
			Kind: "interview", Version: 1,
			OutputSchema: workflow.SchemaRef{SchemaID: "taskpool.interview.output/v1", Version: 1, ProtobufFullName: "hcmnext.taskpool.v1.InterviewOutput"},
			Outputs:      []workflow.Field{strField("notes_ref"), strField("summary")},
			EffectClass:  capability.EffectReadOnly,
		},
		{
			Kind: "evidence-request", Version: 2,
			OutputSchema: workflow.SchemaRef{SchemaID: "taskpool.evidence.output/v1", Version: 1, ProtobufFullName: "hcmnext.taskpool.v1.EvidenceOutput"},
			Outputs:      []workflow.Field{strField("artifact_ref")},
			EffectClass:  capability.EffectPure,
		},
	} {
		if err := catalog.Register(entry); err != nil {
			t.Fatalf("Register %s: %v", entry.Kind, err)
		}
	}
	return catalog
}

func testPool(t *testing.T, catalog *Catalog) *Pool {
	t.Helper()
	pool, err := NewPool(PoolDeclaration{
		Tenant: "acme", InstanceID: "instance-1", NodeID: "case_tasks",
		Allow:    []KindRef{{Kind: "interview", Version: 1}, {Kind: "evidence-request", Version: 2}},
		MaxTasks: 2,
	}, catalog)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	return pool
}

func testAt() values.Instant {
	return values.NewInstant(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
}

// TestTodo_WF_EXT_025 proves the PRIMARY matrix of WF-EXT-025: a node
// declares a task pool that admits runtime-added tasks from an allow-listed
// catalog up to a declared maximum, each completing with a typed output and
// carrying its audit.
func TestTodo_WF_EXT_025(t *testing.T) {
	catalog := testCatalog(t)
	pool := testPool(t, catalog)

	first, err := pool.Add(AddRequest{Tenant: "acme", InstanceID: "instance-1", Kind: "interview", Version: 1, AddedBy: "investigator-7", At: testAt()})
	if err != nil {
		t.Fatalf("Add interview: %v", err)
	}
	if first.TaskID == "" || first.State != TaskOpen {
		t.Fatalf("admitted task = %+v, want an open task with identity", first)
	}
	second, err := pool.Add(AddRequest{Tenant: "acme", InstanceID: "instance-1", Kind: "evidence-request", Version: 2, AddedBy: "investigator-7", At: testAt()})
	if err != nil {
		t.Fatalf("Add evidence-request: %v", err)
	}
	if second.TaskID == first.TaskID {
		t.Fatal("admitted tasks share an identity")
	}

	if err := pool.Complete(CompleteRequest{
		Tenant: "acme", InstanceID: "instance-1", TaskID: first.TaskID,
		CompletedBy: "investigator-7", At: testAt(),
		Outputs: map[string]workflow.TypedValue{
			"notes_ref": {Type: workflow.ValueType{Kind: workflow.KindString}, Text: "notes:1"},
			"summary":   {Type: workflow.ValueType{Kind: workflow.KindString}, Text: "witness confirms timeline"},
		},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	tasks := pool.Tasks()
	if len(tasks) != 2 || tasks[0].State != TaskCompleted || tasks[1].State != TaskOpen {
		t.Fatalf("pool tasks = %+v, want one completed and one open", tasks)
	}
	if len(tasks[0].Audit) != 2 || tasks[0].Audit[0].Action != AuditAdded || tasks[0].Audit[1].Action != AuditCompleted {
		t.Fatalf("task audit = %+v, want added then completed", tasks[0].Audit)
	}
	if tasks[0].Outputs["summary"].Text != "witness confirms timeline" {
		t.Fatalf("typed outputs = %+v", tasks[0].Outputs)
	}
	if pool.Digest() == "" {
		t.Fatal("pool carries no digest")
	}

	// A mistyped output never completes the task.
	if err := pool.Complete(CompleteRequest{
		Tenant: "acme", InstanceID: "instance-1", TaskID: second.TaskID,
		CompletedBy: "investigator-7", At: testAt(),
		Outputs: map[string]workflow.TypedValue{
			"artifact_ref": {Type: workflow.ValueType{Kind: workflow.KindBool}, Text: "true"},
		},
	}); err == nil {
		t.Fatal("mistyped output completed the task")
	}
}

// TestTodo_WF_EXT_025_Security proves an unlisted task kind, an over-cap
// addition, a cross-tenant addition and any effect-carrying kind are refused,
// so the pool cannot add effects or escape its node.
func TestTodo_WF_EXT_025_Security(t *testing.T) {
	catalog := testCatalog(t)

	writer := CatalogEntry{
		Kind: "record-update", Version: 1,
		OutputSchema: workflow.SchemaRef{SchemaID: "taskpool.update.output/v1", Version: 1, ProtobufFullName: "hcmnext.taskpool.v1.UpdateOutput"},
		Outputs:      []workflow.Field{strField("ok")},
		EffectClass:  capability.EffectInternalMutation,
	}
	if err := catalog.Register(writer); !errors.Is(err, ErrEffectRefused) {
		t.Fatalf("write-effect catalog Register = %v, want ErrEffectRefused", err)
	}

	pool := testPool(t, catalog)
	if _, err := pool.Add(AddRequest{Tenant: "acme", InstanceID: "instance-1", Kind: "surveillance", Version: 1, AddedBy: "investigator-7", At: testAt()}); !errors.Is(err, ErrKindNotAllowed) {
		t.Fatalf("unlisted kind = %v, want ErrKindNotAllowed", err)
	}
	if _, err := pool.Add(AddRequest{Tenant: "acme", InstanceID: "instance-1", Kind: "interview", Version: 9, AddedBy: "investigator-7", At: testAt()}); !errors.Is(err, ErrKindNotAllowed) {
		t.Fatalf("unlisted kind version = %v, want ErrKindNotAllowed", err)
	}
	if _, err := pool.Add(AddRequest{Tenant: "other", InstanceID: "instance-1", Kind: "interview", Version: 1, AddedBy: "investigator-7", At: testAt()}); !errors.Is(err, ErrPoolIdentity) {
		t.Fatalf("cross-tenant add = %v, want ErrPoolIdentity", err)
	}
	if _, err := pool.Add(AddRequest{Tenant: "acme", InstanceID: "instance-1", Kind: "interview", Version: 1, AddedBy: "investigator-7", At: testAt()}); err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	if _, err := pool.Add(AddRequest{Tenant: "acme", InstanceID: "instance-1", Kind: "interview", Version: 1, AddedBy: "investigator-7", At: testAt()}); err != nil {
		t.Fatalf("Add 2: %v", err)
	}
	if _, err := pool.Add(AddRequest{Tenant: "acme", InstanceID: "instance-1", Kind: "interview", Version: 1, AddedBy: "investigator-7", At: testAt()}); !errors.Is(err, ErrPoolFull) {
		t.Fatalf("over-cap add = %v, want ErrPoolFull", err)
	}

	// A pool declaration cannot smuggle an effect even past a registry that
	// predates the gate: declaration re-checks the kind's effect class.
	legacy := NewCatalogLegacyForTest(CatalogEntry{
		Kind: "legacy-write", Version: 1,
		OutputSchema: workflow.SchemaRef{SchemaID: "taskpool.legacy.output/v1", Version: 1, ProtobufFullName: "hcmnext.taskpool.v1.LegacyOutput"},
		Outputs:      []workflow.Field{strField("ok")},
		EffectClass:  capability.EffectExternalMutation,
	})
	if _, err := NewPool(PoolDeclaration{
		Tenant: "acme", InstanceID: "instance-1", NodeID: "case_tasks",
		Allow:    []KindRef{{Kind: "legacy-write", Version: 1}},
		MaxTasks: 1,
	}, legacy); !errors.Is(err, ErrEffectRefused) {
		t.Fatalf("effect-carrying pool declaration = %v, want ErrEffectRefused", err)
	}
}
