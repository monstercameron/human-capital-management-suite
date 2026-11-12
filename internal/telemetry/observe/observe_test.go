package observe

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// codedErr carries a stable machine code, as every workflow error does.
type codedErr struct{ code string }

func (e codedErr) Error() string     { return "detail that must never become an attribute " + e.code }
func (e codedErr) ErrorCode() string { return e.code }

// recordedOp captures what the seam tells an operation.
type recordedOp struct {
	attrs   Attrs
	ends    int
	outcome string
	err     error
}

func (o *recordedOp) Set(k, v string) { o.attrs[k] = v }
func (o *recordedOp) End(outcome string, err error) {
	o.ends++
	o.outcome, o.err = outcome, err
}

// recordingRecorder keeps every operation it opens, by name.
type recordingRecorder struct {
	ops map[string]*recordedOp
}

func (r *recordingRecorder) Start(ctx context.Context, name string, attrs Attrs) (context.Context, Operation) {
	op := &recordedOp{attrs: Attrs{}}
	for k, v := range attrs {
		op.attrs[k] = v
	}
	r.ops[name] = op
	return ctx, op
}

type ctxKeyForTest struct{}

func TestBeginWithoutRecorderIsSilentAndDoneReturnsErr(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKeyForTest{}, "kept")
	gotCtx, op := Begin(ctx, "workflow.designer.palette", Attrs{KeyTenant: "t-1"})
	if gotCtx != ctx {
		t.Fatal("Begin without a recorder changed the context")
	}
	boom := errors.New("boom")
	if err := Done(op, boom); !errors.Is(err, boom) {
		t.Fatalf("Done returned %v, want the original error", err)
	}
	// Setting and ending the no-op must not panic and must return nil from Done.
	op.Set(KeyStatus, "ignored")
	if err := Done(op, nil); err != nil {
		t.Fatalf("Done(nil) = %v, want nil", err)
	}
}

func TestBeginRecordsBoundedAttributesAndOutcome(t *testing.T) {
	rec := &recordingRecorder{ops: map[string]*recordedOp{}}
	ctx := WithRecorder(context.Background(), rec)

	type request struct {
		TenantID   string
		WorkflowID string
		Payload    string // not on the allowlist: must never become an attribute
	}
	_, op := Begin(ctx, "workflow.designer.compile", request{TenantID: "tenant-9", WorkflowID: "wf-3", Payload: "secret body"})
	if err := Done(op, nil); err != nil {
		t.Fatalf("Done(nil) = %v", err)
	}

	got := rec.ops["workflow.designer.compile"]
	if got == nil {
		t.Fatal("no operation was opened under its name")
	}
	if got.attrs[KeyTenant] != "tenant-9" || got.attrs[KeyWorkflow] != "wf-3" {
		t.Fatalf("attributes = %v, want tenant and workflow from the request", got.attrs)
	}
	for k, v := range got.attrs {
		if v == "secret body" {
			t.Fatalf("payload leaked into attribute %s", k)
		}
	}
	if got.ends != 1 || got.outcome != OutcomeSuccess || got.err != nil {
		t.Fatalf("end = (%d, %q, %v), want one SUCCESS with no error", got.ends, got.outcome, got.err)
	}
}

func TestDoneClassifiesOutcome(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"success", nil, OutcomeSuccess},
		{"governed refusal", fmt.Errorf("wrap: %w", codedErr{"FENCE_STALE"}), OutcomeRefused},
		{"storage failure stays a failure", codedErr{"STORAGE_FAILED"}, OutcomeFailure},
		{"uncoded error", errors.New("boom"), OutcomeFailure},
		{"cancellation is a failure", context.Canceled, OutcomeFailure},
	}
	for _, c := range cases {
		rec := &recordingRecorder{ops: map[string]*recordedOp{}}
		_, op := Start(WithRecorder(context.Background(), rec), c.name, Attrs{})
		if err := Done(op, c.err); !errors.Is(err, c.err) && err != c.err {
			t.Errorf("%s: Done returned %v, want %v", c.name, err, c.err)
		}
		got := rec.ops[c.name]
		if got.outcome != c.want {
			t.Errorf("%s: outcome %q, want %q", c.name, got.outcome, c.want)
		}
	}
}

func TestFinishUsesTheSuppliedClassifier(t *testing.T) {
	rec := &recordingRecorder{ops: map[string]*recordedOp{}}
	_, op := Start(WithRecorder(context.Background(), rec), "op", Attrs{})
	err := codedErr{"LEASE_HELD"}
	if got := Finish(op, err, func(error) bool { return false }); got != err {
		t.Fatalf("Finish returned %v, want the original error", got)
	}
	if rec.ops["op"].outcome != OutcomeFailure {
		t.Fatalf("classifier said not refused, outcome = %q, want FAILURE", rec.ops["op"].outcome)
	}
}

func TestErrorCodeNeverReturnsMessageText(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"nil":                   {nil, ""},
		"coded":                 {codedErr{"LEASE_HELD"}, "LEASE_HELD"},
		"wrapped coded":         {fmt.Errorf("x: %w", codedErr{"FENCE_STALE"}), "FENCE_STALE"},
		"canceled":              {context.Canceled, "CONTEXT_CANCELED"},
		"deadline":              {context.DeadlineExceeded, "DEADLINE_EXCEEDED"},
		"plain":                 {errors.New("personal detail"), "ERROR"},
		"empty code falls back": {codedErr{""}, "ERROR"},
	}
	for name, c := range cases {
		if got := ErrorCode(c.err); got != c.want {
			t.Errorf("%s: ErrorCode = %q, want %q", name, got, c.want)
		}
	}
}

func TestAttrsWithSkipsEmptyValues(t *testing.T) {
	base := Attrs{KeyTenant: "t"}
	withValue := base.With(KeyWorkflow, "wf")
	if withValue[KeyWorkflow] != "wf" || withValue[KeyTenant] != "t" {
		t.Fatalf("With dropped a value: %v", withValue)
	}
	withEmpty := base.With(KeyWorkflow, "")
	if _, present := withEmpty[KeyWorkflow]; present {
		t.Fatal("With stored an empty value")
	}
	if _, present := base[KeyWorkflow]; present {
		t.Fatal("With mutated its receiver")
	}
}

func TestOfSkipsZeroIdentifiers(t *testing.T) {
	type ids struct {
		TenantID string
		Attempt  int
	}
	got := Of(ids{TenantID: "", Attempt: 0})
	if len(got) != 0 {
		t.Fatalf("Of kept zero values: %v", got)
	}
	got = Of(ids{TenantID: "t-2", Attempt: 4})
	if got[KeyTenant] != "t-2" || got[KeyAttempt] != "4" {
		t.Fatalf("Of = %v, want tenant and attempt", got)
	}
}
