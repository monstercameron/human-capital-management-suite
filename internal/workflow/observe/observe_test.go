package observe

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type codedErr struct{ code string }

func (e codedErr) Error() string     { return "secret detail " + e.code }
func (e codedErr) ErrorCode() string { return e.code }

type fakeOp struct {
	attrs   Attrs
	outcome string
	err     error
	ends    int
}

func (o *fakeOp) Set(k, v string) { o.attrs[k] = v }
func (o *fakeOp) End(outcome string, err error) {
	o.ends++
	o.outcome, o.err = outcome, err
}

type fakeRecorder struct{ ops map[string]*fakeOp }

func (r *fakeRecorder) Start(ctx context.Context, name string, attrs Attrs) (context.Context, Operation) {
	op := &fakeOp{attrs: Attrs{}}
	for k, v := range attrs {
		op.attrs[k] = v
	}
	r.ops[name] = op
	return ctx, op
}

func TestStartWithoutRecorderIsNoop(t *testing.T) {
	ctx := context.Background()
	if RecorderFrom(ctx) != nil {
		t.Fatal("background context carries a recorder")
	}
	if got := WithRecorder(ctx, nil); got != ctx {
		t.Fatal("WithRecorder(nil) changed the context")
	}
	gotCtx, op := Start(ctx, "workflow.lease.acquire", Attrs{KeyTenant: "t"})
	if gotCtx != ctx {
		t.Fatal("noop Start changed the context")
	}
	op.Set(KeyFence, "1")
	if err := Done(op, errors.New("boom")); err == nil {
		t.Fatal("Done swallowed the error")
	}
}

func TestStartRoutesToRecorderAndClassifiesOutcomes(t *testing.T) {
	rec := &fakeRecorder{ops: map[string]*fakeOp{}}
	ctx := WithRecorder(context.Background(), rec)
	if RecorderFrom(ctx) != rec {
		t.Fatal("recorder not carried")
	}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"ok", nil, OutcomeSuccess},
		{"refused", fmt.Errorf("wrap: %w", codedErr{"FENCE_STALE"}), OutcomeRefused},
		{"storage", codedErr{"STORAGE_FAILED"}, OutcomeFailure},
		{"uncoded", errors.New("boom"), OutcomeFailure},
		{"canceled", context.Canceled, OutcomeFailure},
	}
	for _, c := range cases {
		_, op := Start(ctx, c.name, Attrs{KeyInstance: "i"}.With(KeyNode, "n").With(KeyAttempt, ""))
		op.Set(KeyAttempt, Int(3))
		if got := Done(op, c.err); !errors.Is(got, c.err) && got != c.err {
			t.Errorf("%s: Done returned %v, want %v", c.name, got, c.err)
		}
		fo := rec.ops[c.name]
		if fo.outcome != c.want || fo.ends != 1 {
			t.Errorf("%s: outcome %q ends %d, want %q once", c.name, fo.outcome, fo.ends, c.want)
		}
		if fo.attrs[KeyNode] != "n" || fo.attrs[KeyAttempt] != "3" {
			t.Errorf("%s: attrs %v", c.name, fo.attrs)
		}
	}
	_, op := Start(ctx, "custom", nil)
	Finish(op, errors.New("x"), func(error) bool { return true })
	if rec.ops["custom"].outcome != OutcomeRefused {
		t.Errorf("custom classifier ignored: %q", rec.ops["custom"].outcome)
	}
}

func TestErrorCodeNeverLeaksMessage(t *testing.T) {
	for err, want := range map[error]string{
		nil:                                   "",
		codedErr{"LEASE_HELD"}:                "LEASE_HELD",
		codedErr{""}:                          "ERROR",
		context.DeadlineExceeded:              "DEADLINE_EXCEEDED",
		fmt.Errorf("x: %w", context.Canceled): "CONTEXT_CANCELED",
		errors.New("tenant 42 payload"):       "ERROR",
	} {
		if got := ErrorCode(err); got != want {
			t.Errorf("ErrorCode(%v) = %q, want %q", err, got, want)
		}
	}
	if Refused(codedErr{""}) || Refused(nil) {
		t.Error("empty code or nil classified as refusal")
	}
}

type testID [16]byte

func (id testID) String() string {
	if id == (testID{}) {
		return zeroUUID
	}
	return fmt.Sprintf("id-%x", id[0])
}

type Resource struct{ Kind, ID string }

type testFence struct {
	TenantID testID
	Resource Resource
	Token    uint64
}

type testRequest struct {
	TenantID   testID
	InstanceID testID
	NodeID     string
	Attempt    int
	Reason     string
	Payload    map[string]string
	Fence      *testFence
	Inner      struct{ WorkItemID testID }
	hidden     string
}

type testReceipt struct {
	Status       string
	TerminalCode string
	Disposition  string
	TimerID      testID
	Count        int8
	Version      uint32
}

// TestOfExtractsOnlyAllowlistedIdentifiers proves attribute extraction reads
// bounded ids -- nested, through pointers, outermost first -- and never a
// payload, reason text, unexported field or zero value.
func TestOfExtractsOnlyAllowlistedIdentifiers(t *testing.T) {
	req := testRequest{
		TenantID: testID{1}, InstanceID: testID{2}, NodeID: "approve", Attempt: 3,
		Reason: "salary 90000", Payload: map[string]string{"ssn": "123"}, hidden: "x",
		Fence: &testFence{TenantID: testID{9}, Resource: Resource{Kind: "QUEUE", ID: "queue:secret"}, Token: 7},
	}
	req.Inner.WorkItemID = testID{4}
	got := Of(req, nil, 42, Attrs{KeyCode: "PRESET", KeyStatus: ""}, &testReceipt{Status: "PARKED", Disposition: "RETRY", TerminalCode: "OK"})
	want := Attrs{
		KeyTenant: "id-1", KeyInstance: "id-2", KeyNode: "approve", KeyAttempt: "3",
		KeyFence: "7", KeyResource: "QUEUE", KeyWorkItem: "id-4",
		KeyCode: "PRESET", KeyStatus: "PARKED", KeyDisposition: "RETRY",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Of()[%s] = %q, want %q", k, got[k], v)
		}
	}
	for _, v := range got {
		for _, leak := range []string{"90000", "123", "queue:secret", "x"} {
			if v == leak || v == "salary 90000" {
				t.Errorf("Of leaked %q", v)
			}
		}
	}
	if len(got) != len(want) {
		t.Errorf("Of() = %v, want exactly %v", got, want)
	}
	if zero := Of(testRequest{}, (*testFence)(nil), testReceipt{}, Resource{Kind: "INSTANCE"}); len(zero) != 1 || zero[KeyResource] != "INSTANCE" {
		t.Errorf("zero values produced attributes: %v", zero)
	}
	if unsigned := Of(struct{ Token uint64 }{}, struct{ Attempt int64 }{}); len(unsigned) != 0 {
		t.Errorf("zero integers produced attributes: %v", unsigned)
	}
}

func TestBeginAndDoneWith(t *testing.T) {
	_, op := Begin(context.Background(), "workflow.none", testRequest{NodeID: "n"})
	if _, ok := op.(noop); !ok {
		t.Fatal("Begin without a recorder did not return the noop operation")
	}
	if err := DoneWith(op, nil, testReceipt{Status: "DONE"}); err != nil {
		t.Fatal(err)
	}
	rec := &fakeRecorder{ops: map[string]*fakeOp{}}
	ctx := WithRecorder(context.Background(), rec)
	_, op = Begin(ctx, "workflow.some", testRequest{NodeID: "n"})
	boom := codedErr{"LEASE_HELD"}
	if err := DoneWith(op, boom, testReceipt{Status: "HELD"}); !errors.Is(err, boom) {
		t.Fatalf("DoneWith returned %v", err)
	}
	fo := rec.ops["workflow.some"]
	if fo.attrs[KeyNode] != "n" || fo.attrs[KeyStatus] != "HELD" || fo.outcome != OutcomeRefused {
		t.Errorf("recorded %+v", fo)
	}
}
