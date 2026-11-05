// WF-DATA-025 RED: custom records have only create, update and supersede
// operations with no state model.
package statelifecycle_test

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/custom/statelifecycle"
)

var wfdata025Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func wfdata025Lifecycle() statelifecycle.Lifecycle {
	return statelifecycle.Lifecycle{
		TypeID:  "access-request",
		Initial: "DRAFT",
		States:  []statelifecycle.State{"DRAFT", "PENDING_APPROVAL", "APPROVED", "REJECTED", "FULFILLED"},
		Transitions: []statelifecycle.Transition{
			{Name: "submit", From: "DRAFT", To: "PENDING_APPROVAL",
				Guard: statelifecycle.Guard{Roles: []string{"requester"}}},
			{Name: "approve", From: "PENDING_APPROVAL", To: "APPROVED",
				Guard: statelifecycle.Guard{Roles: []string{"approver"}, RequireApproval: true}},
			{Name: "reject", From: "PENDING_APPROVAL", To: "REJECTED",
				Guard: statelifecycle.Guard{Roles: []string{"approver"}, RequireApproval: true}},
			{Name: "fulfill", From: "APPROVED", To: "FULFILLED",
				Guard: statelifecycle.Guard{Roles: []string{"fulfiller"}, AccessRule: "custody.write", StartWorkflow: "wf-provision-access/v3"}},
		},
	}
}

type wfdata025Deps struct {
	access   map[string]bool
	approved map[string]bool
	runs     []string
}

func (d *wfdata025Deps) CheckAccess(rule string, actor statelifecycle.Actor, _ statelifecycle.Record) error {
	if d.access[actor.ID+"|"+rule] {
		return nil
	}
	return errors.New("denied")
}

func (d *wfdata025Deps) CheckApproved(transition string, _ statelifecycle.Actor, _ statelifecycle.Record) error {
	if d.approved[transition] {
		return nil
	}
	return errors.New("not approved")
}

func (d *wfdata025Deps) StartWorkflow(pinnedID string, _ statelifecycle.Actor, rec statelifecycle.Record) (string, error) {
	run := "run-" + pinnedID + "-" + rec.ID
	d.runs = append(d.runs, run)
	return run, nil
}

func (d *wfdata025Deps) bundle() statelifecycle.Deps {
	return statelifecycle.Deps{Access: d, Approval: d, Starter: d}
}

func wfdata025Actor(id string, roles ...string) statelifecycle.Actor {
	return statelifecycle.Actor{ID: id, Roles: roles}
}

// TestTodo_WF_DATA_025 is the PRIMARY test: a type declares states and
// transitions guarded by roles, access rules and approvals, optionally
// starting a pinned workflow; a transition is a governed operation with
// evidence.
func TestTodo_WF_DATA_025(t *testing.T) {
	lc := wfdata025Lifecycle()
	if err := lc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	t.Run("declared transitions walk the lifecycle with evidence", func(t *testing.T) {
		deps := &wfdata025Deps{access: map[string]bool{"u-fulfill|custody.write": true}, approved: map[string]bool{"approve": true}}
		rec, err := statelifecycle.NewRecord(lc, "req-1", map[string]string{"system": "vpn"})
		if err != nil {
			t.Fatalf("NewRecord: %v", err)
		}
		if rec.State() != "DRAFT" {
			t.Fatalf("initial = %s, want DRAFT", rec.State())
		}
		rec, ev, err := statelifecycle.ApplyTransition(lc, rec, "submit", wfdata025Actor("u-req", "requester"), deps.bundle(), wfdata025Now)
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if rec.State() != "PENDING_APPROVAL" || ev.From != "DRAFT" || ev.To != "PENDING_APPROVAL" || ev.Actor != "u-req" {
			t.Fatalf("evidence: %+v state=%s", ev, rec.State())
		}
		rec, _, err = statelifecycle.ApplyTransition(lc, rec, "approve", wfdata025Actor("u-mgr", "approver"), deps.bundle(), wfdata025Now)
		if err != nil {
			t.Fatalf("approve: %v", err)
		}
		rec, ev, err = statelifecycle.ApplyTransition(lc, rec, "fulfill", wfdata025Actor("u-fulfill", "fulfiller"), deps.bundle(), wfdata025Now)
		if err != nil {
			t.Fatalf("fulfill: %v", err)
		}
		if rec.State() != "FULFILLED" || ev.WorkflowRunID == "" {
			t.Fatalf("fulfill evidence: %+v state=%s", ev, rec.State())
		}
		if len(deps.runs) != 1 {
			t.Fatalf("started runs = %v, want one pinned workflow run", deps.runs)
		}
	})

	t.Run("undeclared transitions and wrong states are refused", func(t *testing.T) {
		deps := &wfdata025Deps{}
		rec, _ := statelifecycle.NewRecord(lc, "req-2", nil)
		if _, _, err := statelifecycle.ApplyTransition(lc, rec, "teleport", wfdata025Actor("u-req", "requester"), deps.bundle(), wfdata025Now); !errors.Is(err, statelifecycle.ErrUndeclaredTransition) {
			t.Fatalf("undeclared err = %v, want ErrUndeclaredTransition", err)
		}
		if _, _, err := statelifecycle.ApplyTransition(lc, rec, "approve", wfdata025Actor("u-mgr", "approver"), deps.bundle(), wfdata025Now); !errors.Is(err, statelifecycle.ErrBadState) {
			t.Fatalf("wrong-state err = %v, want ErrBadState", err)
		}
		if rec.State() != "DRAFT" {
			t.Fatal("refused transition moved the state")
		}
	})

	t.Run("guards deny without role, access or approval", func(t *testing.T) {
		deps := &wfdata025Deps{access: map[string]bool{}, approved: map[string]bool{}}
		rec, _ := statelifecycle.NewRecord(lc, "req-3", nil)
		if _, _, err := statelifecycle.ApplyTransition(lc, rec, "submit", wfdata025Actor("u-stranger", "viewer"), deps.bundle(), wfdata025Now); !errors.Is(err, statelifecycle.ErrGuardDenied) {
			t.Fatalf("roleless err = %v, want ErrGuardDenied", err)
		}
		rec, _, _ = statelifecycle.ApplyTransition(lc, rec, "submit", wfdata025Actor("u-req", "requester"), deps.bundle(), wfdata025Now)
		if _, _, err := statelifecycle.ApplyTransition(lc, rec, "approve", wfdata025Actor("u-mgr", "approver"), deps.bundle(), wfdata025Now); !errors.Is(err, statelifecycle.ErrGuardDenied) {
			t.Fatalf("unapproved err = %v, want ErrGuardDenied", err)
		}
		deps.approved["approve"] = true
		rec, _, _ = statelifecycle.ApplyTransition(lc, rec, "approve", wfdata025Actor("u-mgr", "approver"), deps.bundle(), wfdata025Now)
		if _, _, err := statelifecycle.ApplyTransition(lc, rec, "fulfill", wfdata025Actor("u-fulfill", "fulfiller"), deps.bundle(), wfdata025Now); !errors.Is(err, statelifecycle.ErrGuardDenied) {
			t.Fatalf("access-denied err = %v, want ErrGuardDenied", err)
		}
	})

	t.Run("lifecycle validation fails closed", func(t *testing.T) {
		bad := lc
		bad.Initial = "MISSING"
		if err := bad.Validate(); !errors.Is(err, statelifecycle.ErrLifecycleInvalid) {
			t.Fatalf("bad initial err = %v, want ErrLifecycleInvalid", err)
		}
		bad = lc
		bad.Transitions = append(bad.Transitions, statelifecycle.Transition{Name: "submit", From: "DRAFT", To: "APPROVED"})
		if err := bad.Validate(); !errors.Is(err, statelifecycle.ErrLifecycleInvalid) {
			t.Fatalf("dup name err = %v, want ErrLifecycleInvalid", err)
		}
		bad = lc
		bad.Transitions[0].To = "NOWHERE"
		if err := bad.Validate(); !errors.Is(err, statelifecycle.ErrLifecycleInvalid) {
			t.Fatalf("unknown state err = %v, want ErrLifecycleInvalid", err)
		}
	})
}

// TestTodo_WF_DATA_025_Property proves no record reaches a state through
// an undeclared transition: random walks over random lifecycles only ever
// traverse declared edges, and every refusal leaves the state untouched.
func TestTodo_WF_DATA_025_Property(t *testing.T) {
	states := []statelifecycle.State{"S0", "S1", "S2", "S3"}
	for seed := int64(1); seed <= 200; seed++ {
		rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic property seed
		lc := statelifecycle.Lifecycle{TypeID: "prop", Initial: "S0", States: states}
		names := []string{"a", "b", "c", "d", "e"}
		for i := 0; i < 1+rng.Intn(5); i++ {
			from := states[rng.Intn(len(states))]
			to := states[rng.Intn(len(states))]
			if from == to {
				continue
			}
			lc.Transitions = append(lc.Transitions, statelifecycle.Transition{
				Name: names[i], From: from, To: to,
			})
		}
		if err := lc.Validate(); err != nil {
			t.Fatalf("seed %d: Validate: %v", seed, err)
		}
		declared := map[string]statelifecycle.Transition{}
		for _, tr := range lc.Transitions {
			declared[tr.Name] = tr
		}
		deps := &wfdata025Deps{}
		actor := wfdata025Actor("u-prop", "any")
		rec, err := statelifecycle.NewRecord(lc, "p", nil)
		if err != nil {
			t.Fatalf("seed %d: NewRecord: %v", seed, err)
		}
		for step := 0; step < 30; step++ {
			before := rec.State()
			name := names[rng.Intn(len(names))]
			next, _, err := statelifecycle.ApplyTransition(lc, rec, name, actor, deps.bundle(), wfdata025Now)
			want, ok := declared[name]
			if !ok || want.From != before {
				if err == nil {
					t.Fatalf("seed %d step %d: undeclared/mismatched %q applied", seed, step, name)
				}
				if rec.State() != before {
					t.Fatalf("seed %d step %d: refusal moved state", seed, step)
				}
				continue
			}
			if err != nil {
				t.Fatalf("seed %d step %d: declared %q refused: %v", seed, step, name, err)
			}
			rec = next
			if rec.State() != want.To {
				t.Fatalf("seed %d step %d: state=%s want %s", seed, step, rec.State(), want.To)
			}
		}
	}
}

// TestTodo_WF_DATA_025_Security proves a guard cannot be bypassed through
// the generic update operation: UpdateData never moves state, and every
// denied transition leaves no trace.
func TestTodo_WF_DATA_025_Security(t *testing.T) {
	lc := wfdata025Lifecycle()

	t.Run("generic update cannot move state", func(t *testing.T) {
		rec, _ := statelifecycle.NewRecord(lc, "req-s", map[string]string{"system": "vpn"})
		updated := statelifecycle.UpdateData(rec, map[string]string{"state": "FULFILLED", "system": "wifi"})
		if updated.State() != "DRAFT" {
			t.Fatalf("update moved state to %s", updated.State())
		}
		if updated.Data["system"] != "wifi" {
			t.Fatal("update did not merge data")
		}
		if updated.Version != rec.Version+1 {
			t.Fatal("update did not bump the version")
		}
	})

	t.Run("denied transitions leave no trace", func(t *testing.T) {
		deps := &wfdata025Deps{}
		rec, _ := statelifecycle.NewRecord(lc, "req-t", nil)
		before := rec
		if _, _, err := statelifecycle.ApplyTransition(lc, rec, "submit", wfdata025Actor("u-x", "nobody"), deps.bundle(), wfdata025Now); !errors.Is(err, statelifecycle.ErrGuardDenied) {
			t.Fatalf("err = %v, want ErrGuardDenied", err)
		}
		if !reflect.DeepEqual(rec, before) {
			t.Fatal("denied transition mutated the record")
		}
	})

	t.Run("workflow start failure fails the transition closed", func(t *testing.T) {
		deps := &failStarter{wfdata025Deps: &wfdata025Deps{
			access:   map[string]bool{"u-fulfill|custody.write": true},
			approved: map[string]bool{"approve": true},
		}}
		rec, _ := statelifecycle.NewRecord(lc, "req-w", nil)
		rec, _, _ = statelifecycle.ApplyTransition(lc, rec, "submit", wfdata025Actor("u-req", "requester"), deps.bundle(), wfdata025Now)
		rec, _, _ = statelifecycle.ApplyTransition(lc, rec, "approve", wfdata025Actor("u-mgr", "approver"), deps.bundle(), wfdata025Now)
		if _, _, err := statelifecycle.ApplyTransition(lc, rec, "fulfill", wfdata025Actor("u-fulfill", "fulfiller"), deps.bundle(), wfdata025Now); err == nil {
			t.Fatal("transition applied despite workflow start failure")
		}
		if rec.State() != "APPROVED" {
			t.Fatalf("state = %s, want APPROVED (unchanged)", rec.State())
		}
	})
}

type failStarter struct {
	*wfdata025Deps
}

func (f *failStarter) StartWorkflow(string, statelifecycle.Actor, statelifecycle.Record) (string, error) {
	return "", errors.New("starter down")
}

func (f *failStarter) bundle() statelifecycle.Deps {
	return statelifecycle.Deps{Access: f.wfdata025Deps, Approval: f.wfdata025Deps, Starter: f}
}
