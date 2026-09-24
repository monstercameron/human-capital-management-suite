package tenant_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

type revoker struct{ calls []string }

func (r *revoker) Revoke(_ context.Context, id session.ID, _ string) (session.Record, error) {
	r.calls = append(r.calls, string(id))
	return session.Record{}, nil
}

type errorRevoker struct{ err error }

func (r errorRevoker) Revoke(context.Context, session.ID, string) (session.Record, error) {
	return session.Record{}, r.err
}

var lifecycleAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func TestTodo_TENANT_003(t *testing.T) {
	life, err := tenant.NewLifecycle("acme")
	if err != nil {
		t.Fatal(err)
	}
	r := &revoker{}
	suspended, err := life.Suspend(context.Background(), tenant.SuspendRequest{
		Reason: tenant.SuspensionSecurity, RequestedBy: "security-operator", IdempotencyKey: "s-1", At: lifecycleAt,
		Sessions:    []tenant.SessionRef{{ID: "session-b"}, {ID: "session-legal", RequiredForAccess: true}},
		PendingWork: []tenant.PendingWorkItem{{ID: "work-1"}, {ID: "legal-1", RequiredForAccess: true}}, Revoker: r,
	})
	if err != nil {
		t.Fatal(err)
	}
	if suspended.Status != tenant.TenantSuspended || len(r.calls) != 1 || r.calls[0] != "session-b" {
		t.Fatalf("suspension = %#v, revocations = %#v", suspended, r.calls)
	}
	if len(suspended.Capabilities) != len(tenant.AllCapabilities) || suspended.Pending.Action != tenant.PendingWorkFreeze || len(suspended.Pending.PreservedIDs) != 1 {
		t.Fatalf("incomplete suspension semantics: %#v", suspended)
	}
	replay, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionSecurity, RequestedBy: "security-operator", IdempotencyKey: "s-1", At: lifecycleAt, Revoker: r})
	if err != nil || replay.Decision != "NOOP" || len(r.calls) != 1 {
		t.Fatalf("idempotent retry = %#v, err=%v, calls=%v", replay, err, r.calls)
	}
	resumed, err := life.Resume(context.Background(), tenant.ResumeRequest{RequestedBy: "security-operator", IdempotencyKey: "r-1", At: lifecycleAt.Add(time.Hour)})
	if err != nil || resumed.Status != tenant.TenantActive {
		t.Fatalf("resume = %#v, err=%v", resumed, err)
	}
	closed, err := life.Close(context.Background(), tenant.CloseRequest{RequestedBy: "owner", IdempotencyKey: "c-1", At: lifecycleAt.Add(2 * time.Hour), Sessions: []tenant.SessionRef{{ID: "session-legal", RequiredForAccess: true}}, Revoker: r})
	if err != nil || closed.Status != tenant.TenantClosed || life.Status() != tenant.TenantClosed {
		t.Fatalf("close = %#v, err=%v", closed, err)
	}
	if len(r.calls) != 2 || r.calls[1] != "session-legal" {
		t.Fatalf("close did not revoke every session: %v", r.calls)
	}
}

func TestTodo_TENANT_003_Race(t *testing.T) {
	life, err := tenant.NewLifecycle("race")
	if err != nil {
		t.Fatal(err)
	}
	const workers = 12
	type outcome struct {
		result tenant.TransitionResult
		err    error
	}
	results := make(chan outcome, workers)
	request := tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "same-request", At: lifecycleAt}
	for i := 0; i < workers; i++ {
		go func() {
			result, err := life.Suspend(context.Background(), request)
			results <- outcome{result: result, err: err}
		}()
	}
	var eventDigest string
	for i := 0; i < workers; i++ {
		got := <-results
		if got.err != nil {
			t.Fatalf("concurrent suspend: %v", got.err)
		}
		if got.result.Status != tenant.TenantSuspended || got.result.Event.Digest == "" {
			t.Fatalf("concurrent transition=%+v", got.result)
		}
		if eventDigest != "" && got.result.Event.Digest != eventDigest {
			t.Fatalf("replay changed event digest: %q != %q", got.result.Event.Digest, eventDigest)
		}
		eventDigest = got.result.Event.Digest
	}
	if got := life.Status(); got != tenant.TenantSuspended {
		t.Fatalf("status = %s", got)
	}
	if events := life.Events(); len(events) != 1 || events[0].Digest != eventDigest {
		t.Fatalf("events=%+v, want one stable suspension event", events)
	}
}

func TestTodo_TENANT_003_Integration(t *testing.T) {
	life, err := tenant.NewLifecycle("integration")
	if err != nil {
		t.Fatal(err)
	}
	result, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionLegal, RequestedBy: "legal", IdempotencyKey: "legal-1", At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	if result.Event.Digest == "" || len(life.Events()) != 1 {
		t.Fatalf("event evidence = %#v", result.Event)
	}
}

func TestTodo_TENANT_003_Security(t *testing.T) {
	life, err := tenant.NewLifecycle("security")
	if err != nil {
		t.Fatal(err)
	}
	_, err = life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionSecurity, RequestedBy: "security", IdempotencyKey: "bad", At: lifecycleAt, Sessions: []tenant.SessionRef{{ID: "s"}}})
	if !errors.Is(err, tenant.ErrInvalidLifecycle) {
		t.Fatalf("missing revoker error = %v", err)
	}
	if life.Status() != tenant.TenantActive {
		t.Fatalf("failed transition changed status to %s", life.Status())
	}
}

func TestTodo_TENANT_003_Mutation(t *testing.T) {
	life, err := tenant.NewLifecycle("mutation")
	if err != nil {
		t.Fatal(err)
	}
	r := &revoker{}
	result, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "m-1", At: lifecycleAt, Revoker: r})
	if err != nil {
		t.Fatal(err)
	}
	result.Capabilities[0].Allowed = true
	result.Event.Capabilities[0].Allowed = true
	if life.Events()[0].Capabilities[0].Allowed {
		t.Fatal("returned lifecycle evidence aliases internal state")
	}
}

func TestLifecycle_ConstructorsAccessorsAndExplain(t *testing.T) {
	if _, err := tenant.NewLifecycle(" "); !errors.Is(err, tenant.ErrInvalidLifecycle) {
		t.Fatalf("NewLifecycle blank error = %v, want ErrInvalidLifecycle", err)
	}
	manager, err := tenant.NewManager("managed")
	if err != nil {
		t.Fatal(err)
	}
	if manager.Tenant() != "managed" || manager.Status() != tenant.TenantActive {
		t.Fatalf("manager identity/state = %q/%q", manager.Tenant(), manager.Status())
	}
	if manager.Explain() != "tenant managed is ACTIVE" {
		t.Fatalf("Explain = %q, want stable active summary", manager.Explain())
	}
	if events := manager.Events(); len(events) != 0 {
		t.Fatalf("fresh Events = %#v, want empty", events)
	}
	var nilLifecycle *tenant.Lifecycle
	if nilLifecycle.Tenant() != "" || nilLifecycle.Status() != "" || nilLifecycle.Events() != nil {
		t.Fatalf("nil lifecycle accessors were not safe: tenant=%q status=%q events=%#v", nilLifecycle.Tenant(), nilLifecycle.Status(), nilLifecycle.Events())
	}
}

func TestLifecycle_RequestValidationAndConflictBranches(t *testing.T) {
	tests := []struct {
		name string
		req  tenant.SuspendRequest
	}{
		{"unknown reason", tenant.SuspendRequest{Reason: "UNKNOWN", RequestedBy: "operator", IdempotencyKey: "key", At: lifecycleAt}},
		{"missing requester", tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, IdempotencyKey: "key", At: lifecycleAt}},
		{"missing key", tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, RequestedBy: "operator", At: lifecycleAt}},
		{"missing time", tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "key"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			life, err := tenant.NewLifecycle("validation-" + tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := life.Suspend(context.Background(), tt.req); !errors.Is(err, tenant.ErrInvalidLifecycle) {
				t.Fatalf("Suspend error = %v, want ErrInvalidLifecycle", err)
			}
			if life.Status() != tenant.TenantActive || len(life.Events()) != 0 {
				t.Fatalf("invalid request changed lifecycle: status=%s events=%d", life.Status(), len(life.Events()))
			}
		})
	}

	life, err := tenant.NewLifecycle("conflicts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := life.Suspend(context.Background(), tenant.SuspendRequest{
		Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "first", At: lifecycleAt,
		Sessions: []tenant.SessionRef{{ID: ""}}, Revoker: &revoker{},
	}); !errors.Is(err, tenant.ErrInvalidLifecycle) {
		t.Fatalf("blank session error = %v, want ErrInvalidLifecycle", err)
	}
	if _, err := life.Suspend(context.Background(), tenant.SuspendRequest{
		Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "first", At: lifecycleAt,
		Sessions: []tenant.SessionRef{{ID: "session"}}, Revoker: errorRevoker{err: errors.New("trust unavailable")},
	}); err == nil || life.Status() != tenant.TenantActive || len(life.Events()) != 0 {
		t.Fatalf("revoker failure did not leave active state: err=%v status=%s events=%d", err, life.Status(), len(life.Events()))
	}
	if _, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "first", At: lifecycleAt}); err != nil {
		t.Fatal(err)
	}
	if _, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "second", At: lifecycleAt}); !errors.Is(err, tenant.ErrLifecycleConflict) {
		t.Fatalf("second suspension error = %v, want ErrLifecycleConflict", err)
	}
	if _, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionCommercial, RequestedBy: "operator", IdempotencyKey: "first", At: lifecycleAt.Add(time.Minute)}); !errors.Is(err, tenant.ErrLifecycleConflict) {
		t.Fatalf("reused key with changed evidence error = %v, want ErrLifecycleConflict", err)
	}
	if _, err := life.Resume(context.Background(), tenant.ResumeRequest{RequestedBy: "operator", IdempotencyKey: "resume", At: lifecycleAt.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := life.Resume(context.Background(), tenant.ResumeRequest{RequestedBy: "operator", IdempotencyKey: "resume-again", At: lifecycleAt.Add(2 * time.Hour)}); !errors.Is(err, tenant.ErrLifecycleConflict) {
		t.Fatalf("resume while active error = %v, want ErrLifecycleConflict", err)
	}
	if _, err := life.Close(context.Background(), tenant.CloseRequest{RequestedBy: "owner", IdempotencyKey: "close", At: lifecycleAt.Add(3 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := life.Suspend(context.Background(), tenant.SuspendRequest{Reason: tenant.SuspensionSecurity, RequestedBy: "operator", IdempotencyKey: "after-close", At: lifecycleAt.Add(4 * time.Hour)}); !errors.Is(err, tenant.ErrTenantClosed) {
		t.Fatalf("Suspend after close error = %v, want ErrTenantClosed", err)
	}
	if _, err := life.Resume(context.Background(), tenant.ResumeRequest{RequestedBy: "operator", IdempotencyKey: "after-close-resume", At: lifecycleAt.Add(4 * time.Hour)}); !errors.Is(err, tenant.ErrTenantClosed) {
		t.Fatalf("Resume after close error = %v, want ErrTenantClosed", err)
	}
	if _, err := life.Close(context.Background(), tenant.CloseRequest{RequestedBy: "owner", IdempotencyKey: "second-close", At: lifecycleAt.Add(4 * time.Hour)}); !errors.Is(err, tenant.ErrLifecycleConflict) {
		t.Fatalf("Close after close error = %v, want ErrLifecycleConflict", err)
	}
}

func TestLifecycle_SecurityDecisionsPendingAndRevocationOrdering(t *testing.T) {
	life, err := tenant.NewLifecycle("security-details")
	if err != nil {
		t.Fatal(err)
	}
	r := &revoker{}
	result, err := life.Suspend(context.Background(), tenant.SuspendRequest{
		Reason: tenant.SuspensionSecurity, RequestedBy: "security", IdempotencyKey: "security-1", At: lifecycleAt,
		Sessions:    []tenant.SessionRef{{ID: "z-session"}, {ID: "required", RequiredForAccess: true}, {ID: "a-session"}},
		PendingWork: []tenant.PendingWorkItem{{ID: "z-work", RequiredForAccess: true}, {ID: "ignored", RequiredForAccess: true}, {ID: "a-work", RequiredForAccess: true}},
		Revoker:     r,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 || r.calls[0] != "a-session" || r.calls[1] != "z-session" {
		t.Fatalf("revocations = %v, want sorted non-required sessions", r.calls)
	}
	if len(result.Revocations) != 2 || result.Revocations[0].Reason != "TENANT_SECURITY" || !result.Revocations[0].At.Equal(lifecycleAt.UTC()) {
		t.Fatalf("revocation receipts = %#v", result.Revocations)
	}
	if got, want := result.Pending.PreservedIDs, []string{"a-work", "ignored", "z-work"}; !equalStrings(got, want) {
		t.Fatalf("preserved pending IDs = %v, want %v", got, want)
	}
	decisions := make(map[tenant.Capability]tenant.CapabilityDecision, len(result.Capabilities))
	for _, decision := range result.Capabilities {
		decisions[decision.Capability] = decision
	}
	if decisions[tenant.CapabilityInteractive].Allowed || decisions[tenant.CapabilityConnector].Allowed || !decisions[tenant.CapabilityPayroll].Allowed || !decisions[tenant.CapabilityLegal].Allowed || !decisions[tenant.CapabilityAudit].Allowed {
		t.Fatalf("security capability decisions = %#v", decisions)
	}
	if decisions[tenant.CapabilityInteractive].Reason != "security suspension revokes interactive access" {
		t.Fatalf("interactive denial reason = %q", decisions[tenant.CapabilityInteractive].Reason)
	}

	closeLife, err := tenant.NewLifecycle("close-details")
	if err != nil {
		t.Fatal(err)
	}
	closeRevoker := &revoker{}
	closed, err := closeLife.Close(context.Background(), tenant.CloseRequest{
		RequestedBy: "owner", IdempotencyKey: "close-1", At: lifecycleAt,
		Sessions: []tenant.SessionRef{{ID: "required", RequiredForAccess: true}}, Revoker: closeRevoker,
		PendingWork: []tenant.PendingWorkItem{{ID: "legal", RequiredForAccess: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(closeRevoker.calls) != 1 || closeRevoker.calls[0] != "required" || closed.Pending.Action != tenant.PendingWorkRetain || closed.Pending.Count != 1 {
		t.Fatalf("close disposition = %#v, revocations = %v", closed.Pending, closeRevoker.calls)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
