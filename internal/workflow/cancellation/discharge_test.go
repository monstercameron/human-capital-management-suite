package cancellation_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// scriptCompensator is the WF-REV-001 test double for the Compensator port
// WF-REV-002 will bind to the real compensate executor. It records every
// presented item in order, fails or refuses scripted effects, and observes
// every other compensation with a bound evidence reference.
type scriptCompensator struct {
	mu     sync.Mutex
	calls  []cancellation.CompensationItem
	fail   map[string]error
	refuse map[string]bool
}

func (s *scriptCompensator) Compensate(_ context.Context, _ cancellation.Executor, item cancellation.CompensationItem) (cancellation.CompensationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, item)
	if err, ok := s.fail[item.EffectID]; ok {
		return cancellation.CompensationResult{}, err
	}
	if s.refuse[item.EffectID] {
		return cancellation.CompensationResult{Compensated: false}, nil
	}
	return cancellation.CompensationResult{Compensated: true, EvidenceRef: "obs:" + item.EffectID}, nil
}

func (s *scriptCompensator) order() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, c.EffectID)
	}
	return out
}

// seedTwoSucceededAttempts records an instance whose one write node settled
// successfully twice, so the cancellation judge records a two-item
// compensation obligation over a single compensation binding.
func seedTwoSucceededAttempts(f *fixture, plan *workflow.CompiledWorkflow, frontier []string, nodeID string) uuid.UUID {
	f.t.Helper()
	ctx := context.Background()
	inst, err := runtime.NewInstance(f.tenant, uuid.New(), "cell-local", plan, workflow.ModeExecute, "sha256:input", "corr-"+uuid.NewString()[:8], at)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		store := runtime.Store{}
		stored, err := store.CreateInstance(ctx, tx, inst)
		if err != nil {
			return err
		}
		running, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{TenantID: f.tenant, InstanceID: inst.InstanceID,
			ExpectedVersion: stored.InstanceVersion, Status: runtime.InstanceRunning, CurrentNodeIDs: frontier})
		if err != nil {
			return err
		}
		version := running.InstanceVersion
		cn, _ := plan.Node(nodeID)
		for attempt := 1; attempt <= 2; attempt++ {
			exec := runtime.NewNodeExecution(f.tenant, inst.InstanceID, nodeID, attempt, cn.Type, runtime.NodeReady)
			exec.RecordedAt = at
			if _, version, err = store.RecordNodeExecution(ctx, tx, exec, version); err != nil {
				return err
			}
			for _, s := range []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded} {
				if _, version, err = store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{TenantID: f.tenant,
					InstanceID: inst.InstanceID, NodeID: nodeID, Attempt: attempt, ExpectedInstanceVersion: version, Status: s}); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		f.t.Fatalf("seed two-attempt instance: %v", err)
	}
	return inst.InstanceID
}

func (f *fixture) discharge(id uuid.UUID, expected int64, comp cancellation.Compensator) (cancellation.Outcome, error) {
	var out cancellation.Outcome
	err := f.tx(f.conn, func(tx dbport.Tx) error {
		var err error
		out, err = cancellation.Discharge(context.Background(), tx, cancellation.DischargeRequest{TenantID: f.tenant, InstanceID: id,
			ExpectedInstanceVersion: expected, Reason: "PROMOTION_WITHDRAWN", RequestedBy: "principal:operator", RecordedAt: at}, comp)
		return err
	})
	return out, err
}

func compensationOf(recs []cancellation.Record, decision workflow.CancellationDecision) cancellation.Record {
	for _, r := range recs {
		if r.Decision == decision {
			return r
		}
	}
	return cancellation.Record{}
}

// TestTodo_WF_REV_001 proves the PRIMARY contract: the discharge driver
// reads the recorded compensation obligation, runs each compensation in
// reverse obligation order, observes each result onto the effect's own node
// execution, and moves the instance CANCELLING -> CANCELLED with the
// obligation closed.
func TestTodo_WF_REV_001(t *testing.T) {
	f := newFixture(t)
	plan := compensablePlan(t)
	id := seedTwoSucceededAttempts(f, plan, []string{promotionexec.NodeObservePayroll}, promotionexec.NodeExecutePromotion)

	before, _ := f.load(id)
	obligation, err := f.decide(f.conn, plan, id, before.InstanceVersion)
	if err != nil {
		t.Fatal(err)
	}
	if obligation.Decision != workflow.CompensationRequired || len(obligation.CompensationRefs) != 2 {
		t.Fatalf("obligation = %+v", obligation)
	}
	wantRefs := []string{
		"execute_promotion#1=compensation.promotion.reverse@1",
		"execute_promotion#2=compensation.promotion.reverse@1",
	}
	for i, want := range wantRefs {
		if obligation.CompensationRefs[i] != want {
			t.Fatalf("obligation refs = %v, want %v", obligation.CompensationRefs, wantRefs)
		}
	}

	comp := &scriptCompensator{}
	mid, _ := f.load(id)
	out, err := f.discharge(id, mid.InstanceVersion, comp)
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != workflow.Cancelled || out.Instance.RuntimeStatus != runtime.InstanceCancelled {
		t.Fatalf("discharge = %+v", out)
	}
	if got := comp.order(); len(got) != 2 || got[0] != "execute_promotion#2" || got[1] != "execute_promotion#1" {
		t.Fatalf("discharge order = %v, want most-recently-committed first", got)
	}
	for _, c := range comp.calls {
		if c.ObligationID != obligation.DecisionID || c.Compensation != "compensation.promotion.reverse@1" {
			t.Fatalf("presented item = %+v, want obligation binding", c)
		}
	}
	if len(out.CompensationRefs) != 0 || out.StatusBefore != runtime.InstanceCancelling || out.Evidence.Digest == "" ||
		out.Evidence.Decision != workflow.Cancelled || len(out.Evidence.Effects) != 2 {
		t.Fatalf("outcome = %+v", out)
	}
	for _, e := range out.Evidence.Effects {
		if e.Disposition != cancellation.EffectCompensated {
			t.Fatalf("evidence = %+v, want every effect COMPENSATED", out.Evidence.Effects)
		}
	}
	after, nodes := f.load(id)
	if after.RuntimeStatus != runtime.InstanceCancelled || after.CompletedAt == nil {
		t.Fatalf("instance after = %+v", after)
	}
	for _, n := range nodes {
		if n.Status != runtime.NodeCompensated {
			t.Fatalf("node %s attempt %d = %s, want COMPENSATED", n.NodeID, n.Attempt, n.Status)
		}
		if n.OutputArtifactRef != "obs:"+n.NodeID+"#"+strconv.Itoa(n.Attempt) {
			t.Fatalf("node %s attempt %d evidence = %q", n.NodeID, n.Attempt, n.OutputArtifactRef)
		}
	}
	recs := f.decisions(id)
	if len(recs) != 2 {
		t.Fatalf("decisions = %+v, want obligation then discharge", recs)
	}
	done := compensationOf(recs, workflow.Cancelled)
	if done.DecisionID != out.DecisionID || done.StatusBefore != runtime.InstanceCancelling ||
		done.StatusAfter != runtime.InstanceCancelled || done.VersionAfter != after.InstanceVersion {
		t.Fatalf("discharge record = %+v", recs)
	}
	if recs[1].ParentDecisionID == nil || *recs[1].ParentDecisionID != obligation.DecisionID {
		t.Fatalf("discharge record has no parent link to the obligation: %+v", recs[1])
	}

	t.Run("invalid request is refused", func(t *testing.T) {
		if _, err := f.discharge(id, 0, nil); !errors.Is(err, cancellation.ErrInvalid) {
			t.Fatalf("nil compensator err = %v, want ErrInvalid", err)
		}
		if err := f.tx(f.conn, func(tx dbport.Tx) error {
			_, err := cancellation.Discharge(context.Background(), tx, cancellation.DischargeRequest{}, comp)
			if !errors.Is(err, cancellation.ErrInvalid) {
				t.Fatalf("empty request err = %v, want ErrInvalid", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("replay returns the standing decision", func(t *testing.T) {
		again, err := f.discharge(id, 0, comp)
		if err != nil {
			t.Fatal(err)
		}
		if !again.Replayed || again.DecisionID != out.DecisionID || again.Decision != workflow.Cancelled {
			t.Fatalf("replay = %+v", again)
		}
		if got := comp.order(); len(got) != 2 {
			t.Fatalf("replay re-presented compensations: %v", got)
		}
	})
}

// TestTodo_WF_REV_001_Recovery proves a half-discharged obligation resumes
// without repeating a finished compensation: an effect whose COMPENSATED
// transition is already durably recorded is counted discharged and never
// re-presented to the compensator.
func TestTodo_WF_REV_001_Recovery(t *testing.T) {
	f := newFixture(t)
	plan := compensablePlan(t)
	id := seedTwoSucceededAttempts(f, plan, []string{promotionexec.NodeObservePayroll}, promotionexec.NodeExecutePromotion)

	before, _ := f.load(id)
	if _, err := f.decide(f.conn, plan, id, before.InstanceVersion); err != nil {
		t.Fatal(err)
	}
	// Commit the first half of the discharge out of band, as an earlier
	// driver pass would have: the most recent effect is already compensated.
	mid, _ := f.load(id)
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		_, _, err := (runtime.Store{}).RecordNodeTransition(context.Background(), tx, runtime.NodeTransition{TenantID: f.tenant,
			InstanceID: id, NodeID: promotionexec.NodeExecutePromotion, Attempt: 2, ExpectedInstanceVersion: mid.InstanceVersion,
			Status: runtime.NodeCompensated, OutputArtifactRef: "obs:execute_promotion#2:earlier", CompletedAt: &at})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	comp := &scriptCompensator{}
	ready, _ := f.load(id)
	out, err := f.discharge(id, ready.InstanceVersion, comp)
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != workflow.Cancelled || out.Instance.RuntimeStatus != runtime.InstanceCancelled {
		t.Fatalf("resumed discharge = %+v", out)
	}
	if got := comp.order(); len(got) != 1 || got[0] != "execute_promotion#1" {
		t.Fatalf("resumed calls = %v, want only the unfinished effect", got)
	}
	_, nodes := f.load(id)
	for _, n := range nodes {
		if n.Status != runtime.NodeCompensated {
			t.Fatalf("node attempt %d = %s, want COMPENSATED", n.Attempt, n.Status)
		}
	}
	for _, n := range nodes {
		if n.Attempt == 2 && n.OutputArtifactRef != "obs:execute_promotion#2:earlier" {
			t.Fatalf("finished compensation was repeated or rebound: %q", n.OutputArtifactRef)
		}
	}
}

// TestTodo_WF_REV_001_Fault proves a failed compensation lands the instance
// in REPAIR_REQUIRED naming the remaining effects, never back in CANCELLING,
// and that a refused (unapplied) compensation does the same.
func TestTodo_WF_REV_001_Fault(t *testing.T) {
	t.Run("compensator error", func(t *testing.T) {
		f := newFixture(t)
		plan := compensablePlan(t)
		id := seedTwoSucceededAttempts(f, plan, []string{promotionexec.NodeObservePayroll}, promotionexec.NodeExecutePromotion)
		before, _ := f.load(id)
		if _, err := f.decide(f.conn, plan, id, before.InstanceVersion); err != nil {
			t.Fatal(err)
		}
		comp := &scriptCompensator{fail: map[string]error{"execute_promotion#2": errors.New("ledger unavailable")}}
		mid, _ := f.load(id)
		out, err := f.discharge(id, mid.InstanceVersion, comp)
		if err != nil {
			t.Fatal(err)
		}
		assertRepairRequired(t, f, id, out, "execute_promotion#2")
	})

	t.Run("unapplied compensation", func(t *testing.T) {
		f := newFixture(t)
		plan := compensablePlan(t)
		id := seedTwoSucceededAttempts(f, plan, []string{promotionexec.NodeObservePayroll}, promotionexec.NodeExecutePromotion)
		before, _ := f.load(id)
		if _, err := f.decide(f.conn, plan, id, before.InstanceVersion); err != nil {
			t.Fatal(err)
		}
		comp := &scriptCompensator{refuse: map[string]bool{"execute_promotion#2": true}}
		mid, _ := f.load(id)
		out, err := f.discharge(id, mid.InstanceVersion, comp)
		if err != nil {
			t.Fatal(err)
		}
		assertRepairRequired(t, f, id, out, "execute_promotion#2")
	})
}

func assertRepairRequired(t *testing.T, f *fixture, id uuid.UUID, out cancellation.Outcome, remaining string) {
	t.Helper()
	if out.Decision != workflow.RepairRequired {
		t.Fatalf("discharge = %+v, want REPAIR_REQUIRED", out)
	}
	after, nodes := f.load(id)
	if after.RuntimeStatus != runtime.InstanceRepairRequired || after.CompletedAt == nil {
		t.Fatalf("instance after = %+v, want REPAIR_REQUIRED, never CANCELLING", after)
	}
	if out.Instance.RuntimeStatus != runtime.InstanceRepairRequired {
		t.Fatalf("outcome instance = %+v", out.Instance)
	}
	named := false
	for _, r := range out.Reasons {
		if r.Ref == remaining && r.Code == cancellation.ReasonEffectCompensation {
			named = true
		}
	}
	if !named {
		t.Fatalf("reasons = %+v, want the remaining effect named", out.Reasons)
	}
	seen := map[string]string{}
	for _, e := range out.Evidence.Effects {
		seen[e.ID] = e.Disposition
	}
	if len(seen) != 2 || !strings.HasPrefix(seen[remaining], cancellation.EffectRemaining) {
		t.Fatalf("evidence = %+v, want the remaining effect marked", out.Evidence.Effects)
	}
	if len(out.CompensationRefs) != 1 {
		t.Fatalf("remaining obligation = %v, want one ref", out.CompensationRefs)
	}
	compensated := map[string]bool{}
	for _, n := range nodes {
		if n.Status == runtime.NodeCompensated {
			compensated[n.NodeID+"#"+strconv.Itoa(n.Attempt)] = true
		}
	}
	if len(compensated) != 1 || compensated[remaining] {
		t.Fatalf("compensated nodes = %v", compensated)
	}
	recs := f.decisions(id)
	done := compensationOf(recs, workflow.RepairRequired)
	if done.DecisionID != out.DecisionID || done.StatusAfter != runtime.InstanceRepairRequired {
		t.Fatalf("repair record = %+v", recs)
	}
	// The terminal repair stands: a second pass replays it instead of
	// inventing a new verdict or returning to CANCELLING.
	comp := &scriptCompensator{}
	again, err := f.discharge(id, 0, comp)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Replayed || again.Decision != workflow.RepairRequired {
		t.Fatalf("second pass = %+v, want the standing repair replayed", again)
	}
	if len(comp.calls) != 0 {
		t.Fatalf("second pass re-presented compensations: %+v", comp.calls)
	}
}

// TestTodo_WF_REV_001_Integration proves the full governed path against the
// real store: decide records the obligation, discharge drains it, timers
// close exactly as a clean cancel closes them, and the decision chain links
// the discharge to its obligation.
func TestTodo_WF_REV_001_Integration(t *testing.T) {
	f := newFixture(t)
	plan := compensablePlan(t)
	id := f.instance(plan, []string{promotionexec.NodeObservePayroll},
		node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
	timerID := uuid.New()
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		return (runtimestate.TimerStore{}).Set(context.Background(), tx, runtimestate.Timer{TenantID: f.tenant, TimerID: timerID,
			InstanceID: id, NodeID: promotionexec.NodeWaitEffectiveDate, Key: "effective-date", Kind: runtimestate.TimerDelay,
			FiresAt: at.Add(24 * time.Hour), CreatedAt: at})
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := f.load(id)
	obligation, err := f.decide(f.conn, plan, id, before.InstanceVersion)
	if err != nil {
		t.Fatal(err)
	}
	if obligation.Decision != workflow.CompensationRequired {
		t.Fatalf("obligation = %+v", obligation)
	}
	comp := &scriptCompensator{}
	mid, _ := f.load(id)
	out, err := f.discharge(id, mid.InstanceVersion, comp)
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != workflow.Cancelled || out.Instance.RuntimeStatus != runtime.InstanceCancelled {
		t.Fatalf("discharge = %+v", out)
	}
	if got := comp.order(); len(got) != 1 || got[0] != "execute_promotion#1" {
		t.Fatalf("calls = %v", got)
	}
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		timer, err := (runtimestate.TimerStore{}).Load(context.Background(), tx, f.tenant, timerID)
		if err == nil && timer.State != runtimestate.TimerCancelled {
			t.Errorf("pending timer after discharge = %s, want CANCELLED", timer.State)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	recs := f.decisions(id)
	if len(recs) != 2 || recs[0].Decision != workflow.CompensationRequired || recs[1].Decision != workflow.Cancelled ||
		recs[1].ParentDecisionID == nil || *recs[1].ParentDecisionID != recs[0].DecisionID {
		t.Fatalf("decision chain = %+v", recs)
	}
}
