package agenttriggerstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_DURABLE_002 is the PRIMARY test for the queue-model record: one
// durable work queue in the databases the product already runs, compared
// against an external broker and a durable-execution engine, with operating
// cost, guarantees, cross-database implications, the workflow engine as a
// candidate for long agent processes, and a recommended single queue
// contract the existing outboxes can adopt without a rewrite.
func TestTodo_DURABLE_002(t *testing.T) {
	record := DurableQueueModel()

	if len(record.Options) != 3 {
		t.Fatalf("comparison covers %d options, want postgres, broker and durable-execution", len(record.Options))
	}
	wantOptions := []string{"postgres", "broker", "durable-execution"}
	for i, want := range wantOptions {
		option := record.Options[i]
		if option.ID != want {
			t.Fatalf("option %d = %q, want %q", i, option.ID, want)
		}
		for field, value := range map[string]string{
			"guarantees": option.Guarantees, "cost": option.Cost, "verdict": option.Verdict,
		} {
			if strings.TrimSpace(value) == "" {
				t.Fatalf("option %q leaves %s unanswered", want, field)
			}
		}
	}
	if record.Recommended != "postgres" {
		t.Fatalf("recommended = %q, want the postgres queue beside the data it acts on", record.Recommended)
	}

	wantOps := []string{"enqueue", "claim", "heartbeat", "complete", "fail", "dead-letter", "replay"}
	if len(record.Contract) != len(wantOps) {
		t.Fatalf("contract covers %d ops, want %v", len(record.Contract), wantOps)
	}
	for i, want := range wantOps {
		op := record.Contract[i]
		if op.Name != want || strings.TrimSpace(op.Rule) == "" {
			t.Fatalf("contract op %d = %+v, want %q with a rule", i, op, want)
		}
	}

	if len(record.Adoption) < 9 {
		t.Fatalf("adoption maps %d existing outboxes, want at least the nine RED names", len(record.Adoption))
	}
	for _, row := range record.Adoption {
		if row.Outbox == "" || row.Mapping == "" {
			t.Fatalf("adoption row without outbox or mapping: %+v", row)
		}
	}

	if strings.TrimSpace(record.CrossDatabase) == "" {
		t.Fatal("record leaves the cross-database implications unanswered")
	}
	if strings.TrimSpace(record.WorkflowEngine) == "" {
		t.Fatal("record leaves the workflow-engine candidacy unanswered")
	}

	// The record cross-checks the queue vocabulary this package enforces:
	// construction refuses a nil runner, and the no-rows sentinel the
	// claim paths compare against keeps existing.
	if _, err := New(nil, uuid.New(), "tenant-a"); err == nil {
		t.Fatal("Store.New accepted a nil runner")
	} else if !strings.Contains(err.Error(), scheduled.ErrInvalidSchedule.Error()) {
		t.Fatalf("Store.New(nil) = %v, want the invalid-schedule refusal", err)
	}
	_ = dbport.ErrNoRows
	_ = context.Background

	text := RenderDurableQueueModel()
	for _, want := range []string{
		"SKIP LOCKED", "visibility", "heartbeat", "fencing", "dead-letter",
		"at-least-once", "exactly-once", "RLS", "ordering", "tenant",
		"broker", "durable-execution", "replay", "DURABLE-001", "DURABLE-003",
		"DURABLE-004", "backoff", "fairness",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered record omits %q", want)
		}
	}
}

// TestTodo_DURABLE_002_Golden pins the rendered record bytes.
func TestTodo_DURABLE_002_Golden(t *testing.T) {
	const golden = "testdata/durable002_queue.golden"
	want, err := os.ReadFile(filepath.Clean(golden))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got := RenderDurableQueueModel(); got != string(want) {
		t.Fatalf("rendered record differs from %s (%d bytes vs %d)", golden, len(got), len(want))
	}
}
