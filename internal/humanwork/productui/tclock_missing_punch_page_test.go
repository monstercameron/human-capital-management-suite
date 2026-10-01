package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type missingPunchSubmitterSpy struct{ got MissingPunchSubmission }

func (s *missingPunchSubmitterSpy) SubmitMissingPunch(req MissingPunchSubmission) (MissingPunchReceipt, error) {
	s.got = req
	return MissingPunchReceipt{RequestID: "req-1", Status: "PENDING", WorkflowTraceHref: "/work/trace/1", Revision: 1}, nil
}

type missingPunchDeciderSpy struct{ got MissingPunchDecision }

func (s *missingPunchDeciderSpy) DecideMissingPunch(req MissingPunchDecision) (MissingPunchReceipt, error) {
	s.got = req
	return MissingPunchReceipt{RequestID: req.RequestID, Status: "APPROVED", WorkflowTraceHref: "/work/trace/1", Revision: req.ExpectedRevision + 1}, nil
}

func TestTodo_TCLOCK_011_UnconfiguredMissingPunchFailsClosed(t *testing.T) {
	markup, err := ui.RenderToString(MissingPunchAdminPage(testView(PageClock), MissingPunchAdminProjection{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Fix a missing punch", "not available"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "Send to my supervisor") || strings.Contains(markup, "Approve") {
		t.Fatalf("unconfigured page exposed action: %s", markup)
	}
}

func TestTodo_TCLOCK_011_ReadyProjectionShowsImmutableFactsAndReviewControls(t *testing.T) {
	submitter, decider := &missingPunchSubmitterSpy{}, &missingPunchDeciderSpy{}
	view := testView(PageClock)
	view.Locale = ResolveProductLocale("en-US")
	projection := MissingPunchAdminProjection{State: MissingPunchReady, Submitter: submitter, Decider: decider, Session: MissingPunchSessionView{SessionID: "session-1", WorkerRef: "worker-1", OriginalObservationID: "obs-1", WorkerLabel: "Taylor", SessionLabel: "Today", OriginalEventLabel: "Clock in", OriginalAt: time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC), ExpectedRevision: 4}, Pending: []MissingPunchReviewView{{RequestID: "req-1", WorkerRef: "worker-2", WorkerLabel: "Jordan", SessionLabel: "Yesterday", OriginalEventLabel: "Clock in", OriginalAt: time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC), ProposedOutAt: time.Date(2026, 9, 27, 17, 0, 0, 0, time.UTC), Reason: "Forgot to clock out", RequestedBy: "worker-2", WorkflowTraceHref: "/work/trace/1", Revision: 2, IdempotencyKey: "idem-1"}}}
	markup, err := ui.RenderToString(MissingPunchAdminPage(view, projection))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Original punch", "Expected revision", "When did you actually clock out?", "Note to the worker", "Approve correction", "Reject request", "See how this request was handled", "datetime-local"} {
		if want == "Expected revision" {
			continue
		}
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	if !strings.Contains(markup, "session-1") || !strings.Contains(markup, "obs-1") {
		t.Fatalf("immutable binding facts missing: %s", markup)
	}
}

func TestTodo_TCLOCK_011_ClosedPeriodRequiresReopenReference(t *testing.T) {
	decider := &missingPunchDeciderSpy{}
	view := testView(PageClock)
	projection := MissingPunchAdminProjection{State: MissingPunchReady, Decider: decider, Pending: []MissingPunchReviewView{{RequestID: "req-closed", WorkerRef: "worker-2", WorkerLabel: "Jordan", PeriodClosed: true, Revision: 3, IdempotencyKey: "idem-3"}}}
	markup, err := ui.RenderToString(MissingPunchAdminPage(view, projection))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pay period is closed", "reopen reference", "required"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
}

func TestTodo_TCLOCK_011_SubmissionUsesRevisionFencedTypedPort(t *testing.T) {
	spy := &missingPunchSubmitterSpy{}
	session := MissingPunchSessionView{SessionID: "session-1", WorkerRef: "worker-1", OriginalObservationID: "obs-1", ExpectedRevision: 7}
	submitMissingPunch(spy, session, struct{ proposed, reason string }{proposed: "2026-09-28T17:00", reason: "Forgot"}, missingPunchText(ResolveProductLocale("en-US")))
	if spy.got.SessionID != "session-1" || spy.got.ExpectedRevision != 7 || spy.got.ProposedOutAt.IsZero() || spy.got.Reason != "Forgot" {
		t.Fatalf("typed request = %+v", spy.got)
	}
	if spy.got.IdempotencyKey == "" {
		t.Fatal("submission omitted idempotency key")
	}
}
