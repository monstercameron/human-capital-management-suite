package workflow_test

// PRIMARY and GOLDEN for WF-HIRE-001: the New employee hire happy path
// through the real EXECUTE driver, and a pinned record of what that run
// produces. The drive itself reuses hire_execute_test.go's fixture and
// helpers verbatim; nothing here changes the driver or runtime.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_WF_HIRE_001 drives New employee hire end to end through the
// same execute.Driver that drives Promotion: approval, background-check
// signal, adverse-result review skipped on CLEAR, candidate forms, three
// provisioning tasks, the start-date wait, and one authoritative commit
// to exactly one ledger fact.
func TestTodo_WF_HIRE_001(t *testing.T) {
	at := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	f := newHireFixture(t, "hire-primary-1", hireexec.RouteBackgroundCheckClear, at)

	_, parkedWait := hireDriveToWait(t, f)
	completed := hireFireStartDateTimerAndComplete(t, f, parkedWait.Start.InstanceID, parkedWait, f.startAt)
	if completed.Status != execute.StatusComplete {
		t.Fatalf("final result = %+v, want COMPLETE", completed)
	}
	last := completed.Advances[len(completed.Advances)-1]
	if !last.Complete || last.TerminalCode != hireexec.TerminalHired {
		t.Fatalf("terminal advancement = %+v, want %s", last, hireexec.TerminalHired)
	}
	instance := loadHireInstance(t, f, parkedWait.Start.InstanceID)
	if instance.RuntimeStatus != runtime.InstanceCompleted {
		t.Fatalf("instance status = %s, want COMPLETED", instance.RuntimeStatus)
	}
	if n := countRows(t, f.db, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID); n != 1 {
		t.Fatalf("ledger events = %d, want exactly 1", n)
	}
	if n := countRows(t, f.db, `SELECT count(*) FROM work_item WHERE tenant_id = $1 AND workflow_instance_id = $2`,
		f.tenantID, parkedWait.Start.InstanceID); n != 5 {
		t.Fatalf("work items = %d, want exactly 5 (one per human step)", n)
	}
}

// TestTodo_WF_HIRE_001_Golden pins the happy-path run record: the
// terminal code, the advancement count, the work-item count, and the one
// ledger fact's identity.
func TestTodo_WF_HIRE_001_Golden(t *testing.T) {
	at := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	f := newHireFixture(t, "hire-golden-1", hireexec.RouteBackgroundCheckClear, at)

	_, parkedWait := hireDriveToWait(t, f)
	completed := hireFireStartDateTimerAndComplete(t, f, parkedWait.Start.InstanceID, parkedWait, f.startAt)
	if completed.Status != execute.StatusComplete {
		t.Fatalf("final result = %+v, want COMPLETE", completed)
	}
	last := completed.Advances[len(completed.Advances)-1]
	var fact struct {
		AssertionClass string `json:"assertion_class"`
		SchemaRef      string `json:"schema_ref"`
		SourceRef      string `json:"source_ref"`
		Sequence       int64  `json:"sequence"`
	}
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT assertion_class, schema_ref, source_ref, sequence FROM ledger_event WHERE tenant_id = $1`, f.tenantID).
			Scan(&fact.AssertionClass, &fact.SchemaRef, &fact.SourceRef, &fact.Sequence)
	})
	record := struct {
		TerminalCode string `json:"terminal_code"`
		Advances     int    `json:"advances"`
		WorkItems    int    `json:"work_items"`
		Fact         any    `json:"ledger_fact"`
	}{
		TerminalCode: last.TerminalCode,
		Advances:     len(completed.Advances),
		WorkItems: countRows(t, f.db, `SELECT count(*) FROM work_item WHERE tenant_id = $1 AND workflow_instance_id = $2`,
			f.tenantID, parkedWait.Start.InstanceID),
		Fact: fact,
	}
	got, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "hire_run.golden")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("hire run record differs from golden %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
