package execute

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Exercise the driver's scheduling seam with synthetic advancement receipts;
// the approval and WorkItem left waiting underneath are real PostgreSQL rows.
// Approval decision atomicity is separately exercised below and by WORK-006.
func TestTodo_NAAS_002(t *testing.T) {
	for _, mode := range []string{"queued sibling", "new sibling", "approval continuation"} {
		t.Run(mode, func(t *testing.T) {
			f := newWork006Fixture(t)
			req := f.request(&work006Authority{allowed: true})
			selection, err := req.Start.Resolver.ResolveWorkflow(context.Background(), req.Start)
			if err != nil {
				t.Fatal(err)
			}
			run := runContext{start: req.Start, selection: selection, instanceID: f.instanceID}
			var calls []string
			// These END nodes are convenient runnable plan nodes for a scheduling
			// unit test; the injected Advance does not perform a terminal write.
			first, second := prototype.NodeApproved, prototype.NodeCancelled
			waiting := runtime.ContinuationRecord{Kind: frontier.IntentWorkItemRequired, TargetNodeID: prototype.NodeApproval}
			d := f.driver(t, func(_ context.Context, _ runtime.Executor, in runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
				calls = append(calls, in.Outcome.NodeID)
				out := runtime.AdvanceReceipt{NewInstanceVersion: f.instanceVersion, Frontier: []string{prototype.NodeApproval}}
				if in.Outcome.NodeID == first {
					out.Continuations = []runtime.ContinuationRecord{waiting}
					if mode == "new sibling" {
						out.Continuations = append(out.Continuations, runtime.ContinuationRecord{Kind: frontier.IntentReady, TargetNodeID: second})
					}
				}
				return out, nil
			})
			base := Result{InstanceVersion: f.instanceVersion, Frontier: []string{prototype.NodeApproval}}
			var result Result
			if mode == "approval continuation" {
				result, err = d.continueAfterAdvance(context.Background(), run, base, runtime.AdvanceReceipt{
					Continuations: []runtime.ContinuationRecord{waiting, {Kind: frontier.IntentReady, TargetNodeID: second}},
				})
			} else {
				ready := []string{first}
				if mode == "queued sibling" {
					ready = append(ready, second)
				}
				result, err = d.drainReady(context.Background(), run, base, ready)
			}
			want := []string{first, second}
			if mode == "approval continuation" {
				want = []string{second}
			}
			if err != nil || result.Status != StatusParked || !reflect.DeepEqual(calls, want) {
				t.Fatalf("status=%s calls=%v want=%v err=%v", result.Status, calls, want, err)
			}
			work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
				item, err := (workitem.Store{}).Load(context.Background(), tx, f.tenantID, f.item.WorkItemID)
				if err != nil {
					return err
				}
				if item.Status != f.item.Status || item.ItemVersion != f.item.ItemVersion {
					t.Fatalf("waiting approval changed: %+v", item)
				}
				return nil
			})
		})
	}
}

func TestTodo_NAAS_002_Recovery(t *testing.T) {
	f := newWork006Fixture(t)
	req := f.request(&work006Authority{allowed: true})
	crash := errors.New("process lost after approval commit")
	d := f.driver(t, nil)
	d.opts.Steps = approvalQueueFailedRunner{err: crash}
	if _, err := d.CompleteApproval(context.Background(), req); !errors.Is(err, crash) {
		t.Fatalf("failure=%v", err)
	}
	// The vote and READY successor were already committed. The scheduler's
	// recovery entry can continue without asking the person to vote again.
	var currentVersion int64
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		inst, err := (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
		currentVersion = inst.InstanceVersion
		return err
	})
	recovered, err := f.driver(t, nil).RedeliverReady(context.Background(), RedeliverRequest{
		Start: req.Start, InstanceID: f.instanceID, ExpectedInstanceVersion: currentVersion,
	})
	if err != nil || recovered.Status != StatusComplete {
		t.Fatalf("queued recovery=%+v err=%v", recovered, err)
	}
	// Re-delivering the original decision is now a harmless replay.
	got, err := f.driver(t, nil).CompleteApproval(context.Background(), req)
	if err != nil || !got.Replay || got.Status != StatusComplete {
		t.Fatalf("recovery=%+v err=%v", got, err)
	}
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM work_item_transition WHERE tenant_id=$1 AND work_item_id=$2 AND to_status='COMPLETED'`, f.tenantID, f.item.WorkItemID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("decision transitions=%d, want one", count)
		}
		return nil
	})
}

type approvalQueueFailedRunner struct{ err error }

func (r approvalQueueFailedRunner) Run(context.Context, StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, r.err
}

func TestTodo_NAAS_002_Security(t *testing.T) {
	f := newWork006Fixture(t)
	req := f.request(&work006Authority{allowed: true})
	selection, err := req.Start.Resolver.ResolveWorkflow(context.Background(), req.Start)
	if err != nil {
		t.Fatal(err)
	}
	run := runContext{start: req.Start, selection: selection, instanceID: f.instanceID}
	d := f.driver(t, nil)
	t.Run("stale frontier is not a successful wait", func(t *testing.T) {
		_, err := d.parkWaitingFrontier(context.Background(), run, Result{InstanceVersion: f.instanceVersion - 1})
		if !errors.Is(err, ErrNoProgress) {
			t.Fatalf("stale frontier error=%v", err)
		}
	})
	t.Run("other tenant cannot inspect a parked approval", func(t *testing.T) {
		other := run
		other.start.TenantID = uuid.New()
		_, err := d.parkWaitingFrontier(context.Background(), other, Result{InstanceVersion: f.instanceVersion})
		if runtime.CodeOf(err) != runtime.CodeInstanceNotFound {
			t.Fatalf("cross-tenant error=%v", err)
		}
	})
	t.Run("revoked authority does not release successors", func(t *testing.T) {
		_, err := d.CompleteApproval(context.Background(), f.request(&work006Authority{allowed: false}))
		if !errors.Is(err, ErrApprovalAuthorityDenied) {
			t.Fatalf("revoked authority error=%v", err)
		}
		work006AssertUnchanged(t, f)
	})
}

func TestTodo_NAAS_002_Rejection(t *testing.T) {
	f := newWork006Fixture(t)
	req := f.request(&work006Authority{allowed: true})
	req.Decision.Outcome = intentapproval.OutcomeRejected
	req.Decision.DecisionID = "decision:work-006:rejected"
	got, err := f.driver(t, nil).CompleteApproval(context.Background(), req)
	if err != nil || got.Status != StatusComplete || got.Resolution.Outcome != "REJECTED" {
		t.Fatalf("rejection=%+v err=%v", got, err)
	}
	if len(got.Advances) != 2 || got.Advances[1].NodeID != prototype.NodeRejected {
		t.Fatalf("rejection took wrong continuation: %+v", got.Advances)
	}
	for _, advance := range got.Advances {
		if advance.NodeID == prototype.NodeApproved {
			t.Fatal("rejection ran the approved continuation")
		}
	}
	// A conflicting second decision cannot change the recorded route.
	_, err = f.driver(t, nil).CompleteApproval(context.Background(), f.request(&work006Authority{allowed: true}))
	if !errors.Is(err, ErrApprovalCompletionConflict) {
		t.Fatalf("opposite replay error=%v", err)
	}
}
