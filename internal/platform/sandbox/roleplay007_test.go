// WF-TEST-007 RED: simulation records approvals as would-await with no
// one able to act, and SIGNAL is refused.
package sandbox

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

var roleplay007Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// TestTodo_WF_TEST_007 is the PRIMARY test: in a test run the author acts
// as the routed approver or task owner and injects correlated signals or
// vendor callbacks with chosen payloads.
func TestTodo_WF_TEST_007(t *testing.T) {
	open := func(t *testing.T) *TestRun {
		t.Helper()
		run, err := OpenTestRun("test-run-007", "u-author")
		if err != nil {
			t.Fatalf("OpenTestRun: %v", err)
		}
		return run
	}

	t.Run("author acts as the routed approver", func(t *testing.T) {
		run := open(t)
		cred, err := run.AssumeRole("u-cfo", ScopeApproval)
		if err != nil {
			t.Fatalf("AssumeRole: %v", err)
		}
		err = run.ApproveAs(cred, ApprovalStep{
			WorkItemID: "wi-approve-1",
			RoutedTo:   "u-cfo",
			Decision:   workitem.ApprovalDecisionApprove,
		}, roleplay007Now)
		if err != nil {
			t.Fatalf("ApproveAs: %v", err)
		}
		journal := run.Journal()
		if len(journal) != 1 || journal[0].Kind != "approval" || journal[0].As != "u-cfo" {
			t.Fatalf("journal: %+v", journal)
		}
	})

	t.Run("author acts as the task owner", func(t *testing.T) {
		run := open(t)
		cred, err := run.AssumeRole("u-recruiter", ScopeTask)
		if err != nil {
			t.Fatalf("AssumeRole: %v", err)
		}
		err = run.CompleteTaskAs(cred, TaskStep{
			WorkItemID: "wi-task-2",
			RoutedTo:   "u-recruiter",
			OutputRef:  "artifact:screening-pack",
		}, roleplay007Now)
		if err != nil {
			t.Fatalf("CompleteTaskAs: %v", err)
		}
		if got := run.Journal(); len(got) != 1 || got[0].Kind != "task" {
			t.Fatalf("journal: %+v", got)
		}
	})

	t.Run("author injects a correlated signal with a chosen payload", func(t *testing.T) {
		run := open(t)
		cred, err := run.AssumeRole("u-author", ScopeSignal)
		if err != nil {
			t.Fatalf("AssumeRole: %v", err)
		}
		injected, err := run.InjectSignal(cred, SignalInjection{
			CorrelationKey:   "case_id",
			CorrelationValue: "case-9",
			EventType:        "vendor.screening.complete",
			Payload:          []byte(`{"score":"clear"}`),
			IdempotencyKey:   "idem-007-1",
		}, roleplay007Now)
		if err != nil {
			t.Fatalf("InjectSignal: %v", err)
		}
		// The injected record carries exactly what signal.Accept
		// correlates on: key, value, payload and idempotency identity.
		sig := signal.Signal{
			Tenant:           values.TenantId("t-acme"),
			Source:           "test-roleplay",
			EventType:        injected.EventType,
			CorrelationKey:   injected.CorrelationKey,
			CorrelationValue: injected.CorrelationValue,
			IdempotencyKey:   injected.IdempotencyKey,
			Payload:          injected.Payload,
		}
		if sig.CorrelationKey != "case_id" || sig.CorrelationValue != "case-9" {
			t.Fatal("correlation lost")
		}
		if string(sig.Payload) != `{"score":"clear"}` || sig.IdempotencyKey != "idem-007-1" {
			t.Fatal("payload or idempotency lost")
		}
		if got := run.Journal(); len(got) != 1 || got[0].Kind != "signal" {
			t.Fatalf("journal: %+v", got)
		}
	})

	t.Run("author injects a vendor callback with a chosen payload", func(t *testing.T) {
		run := open(t)
		cred, err := run.AssumeRole("u-author", ScopeCallback)
		if err != nil {
			t.Fatalf("AssumeRole: %v", err)
		}
		cb, err := run.InjectCallback(cred, CallbackInjection{
			Vendor:     "screening-vendor",
			CallbackID: "cb-123",
			Payload:    []byte(`{"status":"complete"}`),
		}, roleplay007Now)
		if err != nil {
			t.Fatalf("InjectCallback: %v", err)
		}
		if cb.Vendor != "screening-vendor" || cb.CallbackID != "cb-123" || string(cb.Payload) != `{"status":"complete"}` {
			t.Fatalf("callback record: %+v", cb)
		}
	})

	t.Run("without role-play nobody can act on the approval", func(t *testing.T) {
		run := open(t)
		cred, err := run.AssumeRole("u-cfo", ScopeSignal)
		if err != nil {
			t.Fatalf("AssumeRole: %v", err)
		}
		err = run.ApproveAs(cred, ApprovalStep{WorkItemID: "wi-1", RoutedTo: "u-cfo", Decision: workitem.ApprovalDecisionApprove}, roleplay007Now)
		if !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("out-of-scope approve err = %v, want ErrScopeDenied", err)
		}
		if len(run.Journal()) != 0 {
			t.Fatal("refused action was journaled")
		}
	})
}

// TestTodo_WF_TEST_007_Security proves role-play exists only inside its
// test run and never grants authority outside it.
func TestTodo_WF_TEST_007_Security(t *testing.T) {
	mustOpen := func(t *testing.T, id string) *TestRun {
		t.Helper()
		run, err := OpenTestRun(id, "u-author")
		if err != nil {
			t.Fatalf("OpenTestRun: %v", err)
		}
		return run
	}

	t.Run("credential is bound to its own run", func(t *testing.T) {
		a := mustOpen(t, "run-a")
		b := mustOpen(t, "run-b")
		cred, err := a.AssumeRole("u-cfo", ScopeApproval)
		if err != nil {
			t.Fatalf("AssumeRole: %v", err)
		}
		err = b.ApproveAs(cred, ApprovalStep{WorkItemID: "wi-1", RoutedTo: "u-cfo", Decision: workitem.ApprovalDecisionApprove}, roleplay007Now)
		if !errors.Is(err, ErrForeignRun) {
			t.Fatalf("cross-run err = %v, want ErrForeignRun", err)
		}
		if _, err := b.InjectSignal(cred, SignalInjection{CorrelationKey: "k", CorrelationValue: "v", EventType: "e"}, roleplay007Now); !errors.Is(err, ErrForeignRun) {
			t.Fatalf("cross-run signal err = %v, want ErrForeignRun", err)
		}
	})

	t.Run("closing the run ends every credential", func(t *testing.T) {
		run := mustOpen(t, "run-close")
		cred, _ := run.AssumeRole("u-cfo", ScopeApproval, ScopeSignal)
		run.Close()
		if _, err := run.AssumeRole("u-cfo", ScopeApproval); !errors.Is(err, ErrRunClosed) {
			t.Fatalf("AssumeRole after close err = %v, want ErrRunClosed", err)
		}
		err := run.ApproveAs(cred, ApprovalStep{WorkItemID: "wi-1", RoutedTo: "u-cfo", Decision: workitem.ApprovalDecisionApprove}, roleplay007Now)
		if !errors.Is(err, ErrRunClosed) {
			t.Fatalf("ApproveAs after close err = %v, want ErrRunClosed", err)
		}
		if _, err := run.InjectSignal(cred, SignalInjection{CorrelationKey: "k", CorrelationValue: "v", EventType: "e"}, roleplay007Now); !errors.Is(err, ErrRunClosed) {
			t.Fatalf("InjectSignal after close err = %v, want ErrRunClosed", err)
		}
	})

	t.Run("role-play never authorizes production use", func(t *testing.T) {
		run := mustOpen(t, "run-prod")
		cred, _ := run.AssumeRole("u-cfo", ScopeApproval)
		if err := AuthorizeProduction(cred); !errors.Is(err, ErrTestOnly) {
			t.Fatalf("AuthorizeProduction err = %v, want ErrTestOnly", err)
		}
		// A credential with the test marker stripped is refused inside
		// the run as well: the marker is checked, not decorative.
		stripped := cred
		stripped.TestOnly = false
		err := run.ApproveAs(stripped, ApprovalStep{WorkItemID: "wi-1", RoutedTo: "u-cfo", Decision: workitem.ApprovalDecisionApprove}, roleplay007Now)
		if !errors.Is(err, ErrTestOnly) {
			t.Fatalf("stripped credential err = %v, want ErrTestOnly", err)
		}
	})

	t.Run("actor must be the routed identity", func(t *testing.T) {
		run := mustOpen(t, "run-route")
		cred, _ := run.AssumeRole("u-cfo", ScopeApproval)
		err := run.ApproveAs(cred, ApprovalStep{WorkItemID: "wi-1", RoutedTo: "u-ceo", Decision: workitem.ApprovalDecisionApprove}, roleplay007Now)
		if !errors.Is(err, ErrRoutedMismatch) {
			t.Fatalf("routed-mismatch err = %v, want ErrRoutedMismatch", err)
		}
	})

	t.Run("journal is test-only and records nothing refused", func(t *testing.T) {
		run := mustOpen(t, "run-journal")
		cred, _ := run.AssumeRole("u-cfo", ScopeApproval)
		_ = run.ApproveAs(cred, ApprovalStep{WorkItemID: "wi-1", RoutedTo: "u-cfo", Decision: "NOPE"}, roleplay007Now)
		if len(run.Journal()) != 0 {
			t.Fatal("invalid decision was journaled")
		}
		if err := run.ApproveAs(cred, ApprovalStep{WorkItemID: "wi-1", RoutedTo: "u-cfo", Decision: workitem.ApprovalDecisionReject}, roleplay007Now); err != nil {
			t.Fatalf("ApproveAs: %v", err)
		}
		for _, e := range run.Journal() {
			if !e.TestOnly || e.RunID != "run-journal" || e.Actor != "u-author" {
				t.Fatalf("journal entry leaks scope: %+v", e)
			}
		}
	})
}
