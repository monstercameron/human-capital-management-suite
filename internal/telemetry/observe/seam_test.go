package observe

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// stringID formats like the workflow identifiers: its zero value is the nil
// UUID, which the seam must drop rather than record.
type stringID [16]byte

func (id stringID) String() string {
	if id == (stringID{}) {
		return "00000000-0000-0000-0000-000000000000"
	}
	return fmt.Sprintf("id-%x", id[0])
}

// nilCoded has a pointer-receiver code; a nil pointer must not classify as a
// refusal.
type nilCoded struct{}

func (*nilCoded) Error() string     { return "nil coded" }
func (*nilCoded) ErrorCode() string { return "FENCE_STALE" }

func TestOfExtractsEveryAllowlistedField(t *testing.T) {
	type request struct {
		TenantID      string
		WorkflowID    string
		InstanceID    string
		NodeID        string
		Attempt       int
		WorkItemID    string
		TimerID       string
		Token         uint64
		Status        string
		TerminalCode  string
		Disposition   string
		CorrelationID string
		Payload       string // not allowlisted
	}
	got := Of(request{
		TenantID: "t", WorkflowID: "w", InstanceID: "i", NodeID: "n", Attempt: 3,
		WorkItemID: "wi", TimerID: "tm", Token: 9, Status: "DONE", TerminalCode: "TC",
		Disposition: "KEEP", CorrelationID: "corr", Payload: "secret",
	})
	want := map[string]string{
		KeyTenant: "t", KeyWorkflow: "w", KeyInstance: "i", KeyNode: "n", KeyAttempt: "3",
		KeyWorkItem: "wi", KeyTimer: "tm", KeyFence: "9", KeyStatus: "DONE", KeyCode: "TC",
		KeyDisposition: "KEEP", KeyCorrelation: "corr",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("Of produced %d attributes, want %d: %v", len(got), len(want), got)
	}
	for _, v := range got {
		if v == "secret" {
			t.Fatal("a non-allowlisted field became an attribute")
		}
	}
}

func TestOfSkipsNilAndZeroValuesAndFormatsStringers(t *testing.T) {
	type withPointers struct {
		NodeID   *string
		TenantID stringID
		Status   any
	}
	if got := Of(withPointers{}); len(got) != 0 {
		t.Fatalf("nil pointer, zero UUID and nil interface produced %v", got)
	}
	id := stringID{7}
	got := Of(withPointers{TenantID: id})
	if got[KeyTenant] != "id-7" {
		t.Fatalf("Stringer identifier = %q, want id-7", got[KeyTenant])
	}
	// A pointer to a struct is followed, but a pointer-to-string id is not
	// dereferenced by scalar: the seam records only values and Stringers.
	node := "node-4"
	if got = Of(&withPointers{NodeID: &node}); len(got) != 0 {
		t.Fatalf("pointer-to-string field = %v, want no attribute", got)
	}
	if got = Of(&struct{ NodeID string }{NodeID: "node-5"}); got[KeyNode] != "node-5" {
		t.Fatalf("pointer to struct = %v, want node-5", got)
	}
}

func TestOfOutermostOccurrenceWins(t *testing.T) {
	type inner struct{ TenantID string }
	type outer struct {
		TenantID string
		Inner    inner
	}
	if got := Of(outer{TenantID: "outer", Inner: inner{TenantID: "inner"}}); got[KeyTenant] != "outer" {
		t.Fatalf("tenant = %q, want the outermost occurrence", got[KeyTenant])
	}
}

func TestOfMergesAttrsAndIgnoresNonStructs(t *testing.T) {
	got := Of(Attrs{KeyTenant: "t", KeyWorkflow: ""}, 42, nil, "text")
	if len(got) != 1 || got[KeyTenant] != "t" {
		t.Fatalf("Of = %v, want only the tenant from the Attrs value", got)
	}
}

func TestIntFormatsSigned(t *testing.T) {
	if Int(0) != "0" || Int(12) != "12" || Int(-5) != "-5" {
		t.Fatalf("Int = %q %q %q", Int(0), Int(12), Int(-5))
	}
}

func TestWithRecorderAndRecorderFromRoundTrip(t *testing.T) {
	bare := context.Background()
	if RecorderFrom(bare) != nil {
		t.Fatal("RecorderFrom of a bare context returned a recorder")
	}
	if WithRecorder(bare, nil) != bare {
		t.Fatal("WithRecorder(nil) changed the context")
	}
	rec := &recordingRecorder{ops: map[string]*recordedOp{}}
	ctx := WithRecorder(bare, rec)
	if RecorderFrom(ctx) != Recorder(rec) {
		t.Fatal("RecorderFrom did not return the attached recorder")
	}
}

func TestStartOpensOnRecorderOrReturnsNoop(t *testing.T) {
	_, op := Start(context.Background(), "unattached", Attrs{KeyTenant: "t"})
	op.Set(KeyStatus, "ignored")
	op.End(OutcomeSuccess, nil) // the no-op accepts both without effect

	rec := &recordingRecorder{ops: map[string]*recordedOp{}}
	_, op = Start(WithRecorder(context.Background(), rec), "attached", Attrs{KeyTenant: "t"})
	op.Set(KeyNode, "n-1")
	op.End(OutcomeRefused, errors.New("held"))
	got := rec.ops["attached"]
	if got == nil || got.attrs[KeyTenant] != "t" || got.attrs[KeyNode] != "n-1" {
		t.Fatalf("recorded operation = %+v", got)
	}
	if got.outcome != OutcomeRefused || got.ends != 1 {
		t.Fatalf("outcome = %q after %d ends, want one REFUSED", got.outcome, got.ends)
	}
}

func TestDoneWithSetsResultAttributesThenEnds(t *testing.T) {
	type receipt struct{ Status string }
	rec := &recordingRecorder{ops: map[string]*recordedOp{}}
	_, op := Begin(WithRecorder(context.Background(), rec), "workflow.some")
	if err := DoneWith(op, nil, receipt{Status: "DONE"}); err != nil {
		t.Fatalf("DoneWith(nil) = %v", err)
	}
	got := rec.ops["workflow.some"]
	if got.attrs[KeyStatus] != "DONE" || got.outcome != OutcomeSuccess {
		t.Fatalf("result attribute %v, outcome %q", got.attrs, got.outcome)
	}

	boom := errors.New("boom")
	_, op = Begin(context.Background(), "unattached")
	if err := DoneWith(op, boom, receipt{Status: "IGNORED"}); !errors.Is(err, boom) {
		t.Fatalf("DoneWith on the no-op returned %v, want the error", err)
	}
}

func TestRefusedClassifiesNilAndStorageFailures(t *testing.T) {
	var nilPtr *nilCoded
	if Refused(nilPtr) {
		t.Error("a typed nil coded error classified as a refusal")
	}
	if Refused(nil) {
		t.Error("nil classified as a refusal")
	}
	if !Refused(fmt.Errorf("guard: %w", &nilCoded{})) {
		t.Error("a coded refusal did not classify as a refusal")
	}
	joined := errors.Join(codedErr{"FENCE_STALE"}, codedErr{"STORAGE_WRITE"})
	if Refused(joined) {
		t.Error("a joined storage failure classified as a refusal")
	}
	wrapped := fmt.Errorf("refusal: %w", fmt.Errorf("inner: %w", codedErr{"STORAGE_READ"}))
	if Refused(wrapped) {
		t.Error("a storage failure hidden under a refusal wrapper classified as a refusal")
	}
	if Refused(context.Canceled) {
		t.Error("cancellation classified as a refusal")
	}
}

func TestClockIsAFunctionOfTime(t *testing.T) {
	var clock Clock = time.Now
	if clock().IsZero() {
		t.Fatal("Clock returned the zero time")
	}
}
