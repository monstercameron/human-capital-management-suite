package crewshift

import (
	"errors"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	if Version() != contractVersion {
		t.Fatalf("Version() = %d, want %d", Version(), contractVersion)
	}
}

func TestExplain(t *testing.T) {
	published := mustPublish(t, fixtureDraft("explain-1"))
	ok := PublishOutcome{Shift: published}
	exp := Explain(ok)
	if exp.Outcome != "PUBLISHED" || exp.ShiftID != published.ID {
		t.Fatalf("Explain(published) = %+v, want outcome PUBLISHED for %s", exp, published.ID)
	}

	rejected := PublishOutcome{Rejection: &PublishRejection{Field: "x", State: "Y", Reason: "z"}}
	rexp := rejected.Explain()
	if rexp.Outcome != "REJECTED" || len(rexp.Reasons) == 0 {
		t.Fatalf("Explain(rejected) = %+v, want outcome REJECTED with a reason", rexp)
	}
}

func TestPublishRejectionError(t *testing.T) {
	err := publishReject("field.x", "STATE", "reason text")
	if !errors.Is(err, ErrPublishRejected) {
		t.Fatalf("publishReject does not unwrap to ErrPublishRejected: %v", err)
	}
	if !strings.Contains(err.Error(), "field.x") || !strings.Contains(err.Error(), "reason text") {
		t.Fatalf("PublishRejection.Error() = %q, want field and reason present", err.Error())
	}
}

func TestLifecycleRejectionError(t *testing.T) {
	err := lifecycleReject("field.y", "STATE", "reason text")
	if !errors.Is(err, ErrLifecycleRejected) {
		t.Fatalf("lifecycleReject does not unwrap to ErrLifecycleRejected: %v", err)
	}
	if !strings.Contains(err.Error(), "field.y") || !strings.Contains(err.Error(), "reason text") {
		t.Fatalf("LifecycleRejection.Error() = %q, want field and reason present", err.Error())
	}
}

func TestNewPublishCheckName(t *testing.T) {
	check := NewPublishCheck("custom", func(PublishInput) error { return nil })
	if check.Name() != "custom" {
		t.Fatalf("check.Name() = %q, want %q", check.Name(), "custom")
	}
	if err := check.Check(PublishInput{}); err != nil {
		t.Fatalf("custom check.Check() = %v, want nil", err)
	}
}

func TestHistoryAt(t *testing.T) {
	published := mustPublish(t, fixtureDraft("history-1"))
	var h History
	h = h.Append(published)
	if got, ok := h.At(published.Revision); !ok || got.ID != published.ID {
		t.Fatalf("History.At(%d) = %+v, %v, want the appended shift", published.Revision, got, ok)
	}
	if _, ok := h.At(published.Revision + 99); ok {
		t.Fatalf("History.At of an unrecorded revision reported found")
	}
}

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{StatusDraft, StatusPublished, StatusCancelled} {
		if !s.Valid() {
			t.Fatalf("Status %q should be valid", s)
		}
	}
	if Status("BOGUS").Valid() {
		t.Fatalf("Status BOGUS should not be valid")
	}
}

func TestLifecycleActionValid(t *testing.T) {
	for _, a := range []LifecycleAction{ActionPublish, ActionCancel, ActionReassign} {
		if !a.Valid() {
			t.Fatalf("LifecycleAction %q should be valid", a)
		}
	}
	if LifecycleAction("BOGUS").Valid() {
		t.Fatalf("LifecycleAction BOGUS should not be valid")
	}
}

func TestProposalSourceValid(t *testing.T) {
	if !SourceManual.Valid() || !SourceOptimizer.Valid() {
		t.Fatalf("declared proposal sources should be valid")
	}
	if ProposalSource("BOGUS").Valid() {
		t.Fatalf("ProposalSource BOGUS should not be valid")
	}
}
