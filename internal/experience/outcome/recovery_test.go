package outcome

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestFlowRecoveryNeverOffersActionThatCanDuplicateOrContradictKnownOutcome(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		kind Kind
		want []Action
	}{
		{"partial", Partial, []Action{Wait, Refresh, RequestHelp}},
		{"unknown", Unknown, []Action{Refresh, RequestHelp}},
		{"ambiguous", Ambiguous, []Action{RequestHelp, OpenRepair}},
		{"repair", RepairRequired, []Action{OpenRepair, RequestHelp, Correct}},
		{"partial_expired", Partial, []Action{Refresh, RequestHelp}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Recover(Request{Kind: tc.kind, LastSafeOperation: "op-7", Components: []Component{{Name: "business", Known: true}, {Name: "provider", Known: false}}, Now: now, ObservationDeadline: func() time.Time {
				if tc.name == "partial_expired" {
					return now
				}
				return now.Add(time.Hour)
			}()})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(r.Actions, tc.want) {
				t.Fatalf("actions = %v, want %v", r.Actions, tc.want)
			}
			for _, a := range r.Actions {
				if a == "RETRY" || a == "RESUBMIT" {
					t.Fatalf("unsafe action %q", a)
				}
			}
			if r.LastSafeOperation != "op-7" || len(r.KnownComponents) != 1 || len(r.UnknownComponents) != 1 {
				t.Fatalf("state not preserved: %+v", r)
			}
		})
	}
}

func TestRecoveryAllowsCancelOnlyWhenExplicitlySafe(t *testing.T) {
	base := Request{Kind: Partial, LastSafeOperation: "before-effect", Components: []Component{{Name: "business", Known: true}}, Now: time.Now()}
	r, err := Recover(base)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.Actions, CancelIfSafe) {
		t.Fatalf("safe cancellation missing: %v", r.Actions)
	}
	base.EffectApplied = true
	r, err = Recover(base)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(r.Actions, CancelIfSafe) {
		t.Fatalf("cancellation offered after effect: %v", r.Actions)
	}
}

// The matrix names are intentionally present in the package so planning
// coverage can point at executable recovery oracles rather than prose.
func TestTodo_UXFLOW_007_Property(t *testing.T) {
	r, err := Recover(Request{Kind: Unknown, LastSafeOperation: "safe-op", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Actions, []Action{Refresh, RequestHelp}) || r.LastSafeOperation != "safe-op" {
		t.Fatalf("unknown outcome offered unsafe or unrelated recovery: %+v", r)
	}
}

func TestTodo_UXFLOW_007_Golden(t *testing.T) {
	r, err := Recover(Request{Kind: RepairRequired, LastSafeOperation: "safe-op", RepairReference: "repair-9", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	want := []Action{OpenRepair, RequestHelp, Correct}
	if !slices.Equal(r.Actions, want) || r.Reason != "a governed repair is required before completion" || r.Kind != RepairRequired {
		t.Fatalf("repair recovery=%+v", r)
	}
}

func TestTodo_UXFLOW_007_Race(t *testing.T) {
	const workers = 12
	start := make(chan struct{})
	errch := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			<-start
			r, err := Recover(Request{Kind: Partial, LastSafeOperation: "op", Components: []Component{{Name: "business", Known: true}}, Now: time.Unix(int64(i+1), 0)})
			if err == nil && !slices.Equal(r.Actions, []Action{Wait, Refresh, RequestHelp, CancelIfSafe}) {
				err = errors.New("unexpected concurrent recovery actions")
			}
			errch <- err
		}(i)
	}
	close(start)
	for i := 0; i < workers; i++ {
		if err := <-errch; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_UXFLOW_007_Fault(t *testing.T) {
	if _, err := Recover(Request{Kind: "SUCCESS", LastSafeOperation: "op", Now: time.Unix(1, 0)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported state error=%v", err)
	}
}

func TestTodo_UXFLOW_007_Security(t *testing.T) {
	r, err := Recover(Request{Kind: Partial, LastSafeOperation: "op", EffectApplied: true, Components: []Component{{Name: "provider", Known: false}}, Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(r.Actions, CancelIfSafe) || slices.Contains(r.Actions, Action("RETRY")) || slices.Contains(r.Actions, Action("RESUBMIT")) {
		t.Fatalf("unsafe action exposed: %v", r.Actions)
	}
}

func TestTodo_UXFLOW_007_Conformance(t *testing.T) {
	for _, kind := range []Kind{Partial, Unknown, Ambiguous, RepairRequired} {
		r, err := Recover(Request{Kind: kind, LastSafeOperation: "op", Now: time.Unix(1, 0)})
		if err != nil || r.Kind != kind || len(r.Actions) == 0 {
			t.Fatalf("kind %s: recovery=%+v err=%v", kind, r, err)
		}
	}
}

func TestTodo_UXFLOW_007_Browser(t *testing.T) {
	r, err := Recover(Request{Kind: Unknown, LastSafeOperation: "op", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range r.Actions {
		if a != Refresh && a != RequestHelp {
			t.Fatalf("participant view exposes unexpected action %q", a)
		}
	}
}

func TestTodo_UXFLOW_007_Recovery(t *testing.T) {
	now := time.Unix(10, 0)
	r, err := Recover(Request{Kind: Partial, LastSafeOperation: "op", ObservationDeadline: now, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !r.DeadlineExceeded || slices.Contains(r.Actions, Wait) || !slices.Contains(r.Actions, RequestHelp) {
		t.Fatalf("expired recovery=%+v", r)
	}
}

func TestTodo_UXFLOW_007_Mutation(t *testing.T) {
	first, err := Recover(Request{Kind: Unknown, LastSafeOperation: "op", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	first.Actions[0] = Correct
	second, err := Recover(Request{Kind: Unknown, LastSafeOperation: "op", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(second.Actions, []Action{Refresh, RequestHelp}) {
		t.Fatalf("caller mutation affected a later recovery: %v", second.Actions)
	}
}
