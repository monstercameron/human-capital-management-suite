package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
)

type rev00903Beginner struct{ tx *rev00903Tx }

func (b rev00903Beginner) Begin(context.Context) (dbport.Tx, error) { return b.tx, nil }

type rev00903Tx struct{ execs, reads, rowReads int }

func (tx *rev00903Tx) Exec(context.Context, string, ...any) (int64, error) {
	tx.execs++
	return 1, nil
}
func (tx *rev00903Tx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	tx.reads++
	return nil, errors.New("unexpected query")
}
func (tx *rev00903Tx) QueryRow(context.Context, string, ...any) dbport.Row {
	tx.rowReads++
	return rev00903Row{}
}
func (*rev00903Tx) Commit(context.Context) error   { return nil }
func (*rev00903Tx) Rollback(context.Context) error { return nil }

type rev00903Row struct{}

func (rev00903Row) Scan(...any) error { return errors.New("unexpected query row") }

func TestTodo_REV_009_03(t *testing.T) {
	reader := NewWorkflowInstanceInspector(nil, nil, nil)
	if _, err := reader.InspectWorkflowInstance(t.Context(), "tenant", uuid.New(), inspect.AllowAll("policy.v1", "purpose", "operator"), inspect.WorkItemAuthorization{Disclosed: true}); !errors.Is(err, ErrWorkflowInstanceInspectorUnconfigured) {
		t.Fatalf("unconfigured inspector error = %v, want ErrWorkflowInstanceInspectorUnconfigured", err)
	}
	want := []inspect.RecordFamily{
		{Family: inspect.FamilyTimer, Section: inspect.SectionNode, State: inspect.RecordLoaded, Count: 2},
		{Family: inspect.FamilyLease, Section: inspect.SectionInstance, State: inspect.RecordNotRecorded},
		{Family: inspect.FamilyCheckpoint, Section: inspect.SectionInstance, State: inspect.RecordIntegrityFailed, Reason: "CHECKPOINT_DIGEST_MISMATCH"},
		{Family: inspect.FamilyOutbox, Section: inspect.SectionConnector, State: inspect.RecordRedacted, Reason: "NO_CONNECTOR_SCOPE"},
		{Family: inspect.FamilyReconciliation, Section: inspect.SectionObservation, State: inspect.RecordUnavailable, Reason: "NO_RECONCILIATION_STORE"},
	}
	got := journeyRecordFamilies(want)
	if len(got) != len(want) {
		t.Fatalf("journey durable manifest has %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Family != want[i].Family || got[i].Section != string(want[i].Section) ||
			got[i].State != string(want[i].State) || got[i].Count != int32(want[i].Count) || got[i].Reason != want[i].Reason {
			t.Errorf("journey durable manifest[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTodo_REV_009_03_Security(t *testing.T) {
	tx := &rev00903Tx{}
	reader := NewWorkflowInstanceInspector(rev00903Beginner{tx: tx}, func(values.TenantId) uuid.UUID {
		return uuid.New()
	}, nil)
	decision := inspect.AllowAll("policy.v1", "journey.inspection", "viewer")
	decision.InstanceDisclosable = false
	decision.DenialReason = "INSTANCE_NOT_DISCLOSABLE"
	_, err := reader.InspectWorkflowInstance(t.Context(), "tenant", uuid.New(), decision,
		inspect.WorkItemAuthorization{Disclosed: true})
	if !errors.Is(err, inspect.ErrNotDisclosable) {
		t.Fatalf("non-disclosable instance error = %v, want inspect.ErrNotDisclosable", err)
	}
	if tx.reads != 0 || tx.rowReads != 0 {
		t.Fatalf("non-disclosable instance performed %d query and %d query-row reads", tx.reads, tx.rowReads)
	}
	if tx.execs != 1 {
		t.Fatalf("tenant scoping statements = %d, want exactly the tenant context setup before inspect.Load refuses", tx.execs)
	}
}
