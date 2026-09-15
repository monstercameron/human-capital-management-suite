package runtime_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func proposalWrite(value string) runtime.VariableWrite {
	return runtime.VariableWrite{
		VariableName: "proposal",
		SchemaRef:    "schema.promotion.proposal/v1",
		Value:        json.RawMessage(value),
		WriterNodeID: "build_proposal",
		WriterRef:    "transform-execution:1",
		Reason:       "proposal rebuilt after band evaluation",
		CausationID:  "cause:band-evaluated",
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if !errors.Is(err, runtime.ErrRuntime) || runtime.CodeOf(err) != code {
		t.Fatalf("err = %v, want runtime refusal %s", err, code)
	}
}

// TestVariableRevisionLog_AppendsNeverOverwrites proves a variable write
// appends a new revision carrying writer, reason, schema and causation, and
// that every earlier value stays recoverable as of its revision.
func TestVariableRevisionLog_AppendsNeverOverwrites(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	log, err := runtime.NewVariableRevisionLog(tenant, instance, nil)
	if err != nil {
		t.Fatalf("empty log: %v", err)
	}
	if log.Head() != 0 {
		t.Fatalf("head = %d, want 0", log.Head())
	}

	first, err := log.Append(proposalWrite(`{"grade":"L5","band":{"min":100}}`), fixedInstant)
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	other := proposalWrite(`{"approved":false}`)
	other.VariableName = "approval"
	if _, err := log.Append(other, fixedInstant.Add(time.Minute)); err != nil {
		t.Fatalf("append other variable: %v", err)
	}
	third, err := log.Append(proposalWrite(`{"band":{"min":100},"grade":"L6"}`), fixedInstant.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("append overwrite: %v", err)
	}

	if first.Revision != 1 || first.PreviousRevision != 0 || first.PriorDigest != "" {
		t.Fatalf("first = %+v, want revision 1 starting the chain", first)
	}
	if third.Revision != 3 || third.PreviousRevision != 1 || third.PriorDigest == "" {
		t.Fatalf("third = %+v, want revision 3 following proposal revision 1", third)
	}
	if string(first.Value) != `{"band":{"min":100},"grade":"L5"}` {
		t.Fatalf("value not canonical: %s", first.Value)
	}
	if third.Reason == "" || third.WriterNodeID != "build_proposal" || third.CausationID == "" || third.SchemaRef == "" {
		t.Fatalf("revision lost its writer/reason/schema/causation: %+v", third)
	}

	asOf, ok := log.AsOf("proposal", 2)
	if !ok || asOf.Revision != 1 || string(asOf.Value) != string(first.Value) {
		t.Fatalf("proposal as of revision 2 = %+v, want the L5 revision", asOf)
	}
	current, ok := log.Current("proposal")
	if !ok || current.Revision != 3 {
		t.Fatalf("current proposal = %+v, want revision 3", current)
	}
	if _, ok := log.Current("never_written"); ok {
		t.Fatal("an unwritten variable has no revision")
	}

	// Returned copies do not alias the log.
	revs := log.Revisions()
	revs[0].Value[1] = 'X'
	if again, _ := log.AsOf("proposal", 1); string(again.Value) != string(first.Value) {
		t.Fatal("editing a returned revision rewrote the log")
	}

	// Rebuilding from the stored history reproduces the same log.
	rebuilt, err := runtime.NewVariableRevisionLog(tenant, instance, log.Revisions())
	if err != nil || rebuilt.Head() != 3 {
		t.Fatalf("rebuild = head %v, err %v", rebuilt, err)
	}
}

func TestVariableRevisionLog_RefusesIncompleteWrites(t *testing.T) {
	cases := map[string]func(w *runtime.VariableWrite){
		"no name":      func(w *runtime.VariableWrite) { w.VariableName = "" },
		"no schema":    func(w *runtime.VariableWrite) { w.SchemaRef = " " },
		"no writer":    func(w *runtime.VariableWrite) { w.WriterRef = "" },
		"no node":      func(w *runtime.VariableWrite) { w.WriterNodeID = "" },
		"no reason":    func(w *runtime.VariableWrite) { w.Reason = "" },
		"no causation": func(w *runtime.VariableWrite) { w.CausationID = "" },
		"scalar value": func(w *runtime.VariableWrite) { w.Value = json.RawMessage(`"L5"`) },
		"broken json":  func(w *runtime.VariableWrite) { w.Value = json.RawMessage(`{"a":`) },
		"trailing":     func(w *runtime.VariableWrite) { w.Value = json.RawMessage(`{"a":1} {}`) },
		"exponent":     func(w *runtime.VariableWrite) { w.Value = json.RawMessage(`{"a":[1e3]}`) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			log, _ := runtime.NewVariableRevisionLog(uuid.New(), uuid.New(), nil)
			w := proposalWrite(`{"a":1}`)
			mutate(&w)
			_, err := log.Append(w, fixedInstant)
			wantCode(t, err, runtime.CodeInvalidRecord)
			if log.Head() != 0 {
				t.Fatal("a refused write appended a revision")
			}
		})
	}
	log, _ := runtime.NewVariableRevisionLog(uuid.New(), uuid.New(), nil)
	_, err := log.Append(proposalWrite(`{"a":1}`), time.Time{})
	wantCode(t, err, runtime.CodeInvalidRecord)
}

// TestVariableRevisionLog_RefusesTamperedHistory proves a rebuilt log refuses
// a gap, a reorder, a foreign row and an edited row.
func TestVariableRevisionLog_RefusesTamperedHistory(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	log, _ := runtime.NewVariableRevisionLog(tenant, instance, nil)
	for i, v := range []string{`{"v":1}`, `{"v":2}`, `{"v":3}`} {
		if _, err := log.Append(proposalWrite(v), fixedInstant.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	history := log.Revisions()
	cases := map[string]func() []runtime.VariableRevision{
		"gap": func() []runtime.VariableRevision { return []runtime.VariableRevision{history[0], history[2]} },
		"reorder": func() []runtime.VariableRevision {
			return []runtime.VariableRevision{history[1], history[0], history[2]}
		},
		"edited value": func() []runtime.VariableRevision {
			h := log.Revisions()
			h[1].Value = json.RawMessage(`{"v":99}`)
			return h
		},
		"edited reason": func() []runtime.VariableRevision {
			h := log.Revisions()
			h[1].Reason = "someone else's reason"
			return h
		},
		"wrong previous revision": func() []runtime.VariableRevision {
			h := log.Revisions()
			h[2].PreviousRevision = 1
			return h
		},
		"previous not before": func() []runtime.VariableRevision {
			h := log.Revisions()
			h[0].PreviousRevision = 1
			return h
		},
		"chain start": func() []runtime.VariableRevision {
			h := log.Revisions()
			h[0].PriorDigest = "sha256:forged"
			return h
		},
		"no identity": func() []runtime.VariableRevision {
			h := log.Revisions()
			h[0].TenantID = uuid.Nil
			return h
		},
		"revision zero": func() []runtime.VariableRevision {
			h := log.Revisions()
			h[0].Revision = 0
			return h
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := runtime.NewVariableRevisionLog(tenant, instance, build())
			wantCode(t, err, runtime.CodeInvalidRecord)
		})
	}
	if _, err := runtime.NewVariableRevisionLog(uuid.New(), instance, history); runtime.CodeOf(err) != runtime.CodeInvalidRecord {
		t.Fatalf("foreign tenant history err = %v", err)
	}
	if err := history[0].Validate(); err != nil {
		t.Fatalf("an untouched revision validates: %v", err)
	}
}

// TestVariableRevisionLog_DigestIgnoresKeyOrderAndSubMicroseconds proves the
// value digest survives the store's jsonb key reordering and timestamptz
// microsecond precision.
func TestVariableRevisionLog_DigestIgnoresKeyOrderAndSubMicroseconds(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	a, _ := runtime.NewVariableRevisionLog(tenant, instance, nil)
	b, _ := runtime.NewVariableRevisionLog(tenant, instance, nil)
	ra, err := a.Append(proposalWrite(`{"x":1,"y":"<b>"}`), fixedInstant.Add(123))
	if err != nil {
		t.Fatalf("append a: %v", err)
	}
	rb, err := b.Append(proposalWrite(`{ "y" : "<b>", "x" : 1 }`), fixedInstant)
	if err != nil {
		t.Fatalf("append b: %v", err)
	}
	if ra.RevisionDigest != rb.RevisionDigest || ra.ValueDigest != rb.ValueDigest {
		t.Fatalf("digests differ: %s vs %s", ra.RevisionDigest, rb.RevisionDigest)
	}
	next, err := a.Next(proposalWrite(`{"x":2}`), fixedInstant)
	if err != nil || next.Revision != 2 || a.Head() != 1 {
		t.Fatalf("Next must not append: next %+v head %d err %v", next, a.Head(), err)
	}
}
