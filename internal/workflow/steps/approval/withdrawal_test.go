package approval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

// TestTodo_WF_STEP_018_Withdrawal proves a pending decision is withdrawn
// durably: LoadDecisions reads the committed vote back, RecordWithdrawal
// appends exactly one tenant-scoped, immutable withdrawal for it, and a
// malformed or repeated withdrawal is refused.
func TestTodo_WF_STEP_018_Withdrawal(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wf-step-018-withdrawal")
	other := insertTenant(t, db, "wf-step-018-other")
	insertInstance(t, db, tenant, instanceID, "approve")
	conn := appConn(t, db)
	store := workitem.Store{}
	ctx := context.Background()
	at := now.Time()
	meta := func(reason string) workitem.TransitionMeta {
		return workitem.TransitionMeta{ActorPrincipalID: "principal:one", Reason: reason, At: at}
	}

	req := withInvalidators(requirement("approval.board", 2, true), humanwork.InvalidatorMaterialProposalChange)
	set := humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}
	node := stepapproval.CompiledApprovalNode{
		WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: "approve", WorkType: "promotion.approval",
		PolicyRouteRef: "route.board/v1", Visibility: workitem.VisibilityCandidateSet, OrganizationScopeID: "org:acme",
	}
	var items []workitem.WorkItem
	var decided workitem.WorkItem
	d := decision(req, "principal:one", "decision:board:1", intentapproval.OutcomeApproved)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		for range 2 {
			item, err := stepapproval.Open(ctx, tx, store, stepapproval.OpenInput{
				TenantID: tenant, WorkflowInstanceID: instanceID, CorrelationID: "corr", SubjectRefs: []string{"worker:jane"},
				Node: node, Requirement: req, Proposal: proposal(), Now: at, Meta: meta("workitem.created"),
			})
			if err != nil {
				return err
			}
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, workitem.Assignment{
				Resolution: humanwork.Resolution{
					RequirementID: req.RequirementID, RequirementRevision: req.Revision, Outcome: humanwork.OutcomeResolved,
					Candidates: []humanwork.Candidate{{PrincipalID: "principal:one", Via: humanwork.SourceDirect, TermRef: "role:board"}},
					ResolvedAt: now, EffectiveAt: now, DirectoryVersion: "directory/1",
					ExpressionDigest: req.ExpressionDigest, RequirementDigest: req.Digest(), QuorumRequired: 2,
				},
				GovernancePolicyRef: req.Source.GovernancePolicyRef, Trigger: workitem.TriggerInitialRouting,
			}, meta("workitem.routed"))
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		claimed, err := store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: items[0].WorkItemID, ExpectedVersion: items[0].ItemVersion,
			ClaimantPrincipalID: "principal:one", ClaimExpiresAt: at.Add(time.Hour), Now: at, Meta: meta("workitem.claimed"),
		})
		if err != nil {
			return err
		}
		started, err := store.Start(ctx, tx, tenant, claimed.WorkItemID, claimed.ItemVersion, at, meta("workitem.started"))
		if err != nil {
			return err
		}
		decided, err = stepapproval.Complete(ctx, tx, store, started, d, at, meta("workitem.completed"))
		items[0] = decided
		return err
	})
	cont := continuation(t, set, items)

	var decisions []intentapproval.ApprovalDecision
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		decisions, err = stepapproval.LoadDecisions(ctx, tx, cont, items)
		return err
	})
	if len(decisions) != 1 || decisions[0].Digest() != d.Digest() {
		t.Fatalf("LoadDecisions = %d decisions, want exactly the committed vote", len(decisions))
	}

	w := stepapproval.NewWithdrawal(cont, decided, decisions[0], req.Invalidators[0], "the proposal was revised", "proposal:promotion:2", "system:invalidator", at)
	var stored stepapproval.Withdrawal
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		stored, err = stepapproval.RecordWithdrawal(ctx, tx, w)
		return err
	})
	if stored.DecisionDigest != d.Digest() || stored.Invalidator != req.Invalidators[0] || stored.RecordedAt.IsZero() {
		t.Fatalf("RecordWithdrawal stored %+v", stored)
	}
	var loaded []stepapproval.Withdrawal
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		loaded, err = stepapproval.LoadWithdrawals(ctx, tx, tenant, instanceID)
		return err
	})
	if len(loaded) != 1 || loaded[0].WorkItemID != decided.WorkItemID || loaded[0].ContinuationDigest != cont.Digest {
		t.Fatalf("LoadWithdrawals = %+v", loaded)
	}

	t.Run("a decision is withdrawn once", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := stepapproval.RecordWithdrawal(ctx, tx, w)
			return err
		})
		if err == nil {
			t.Fatal("a second withdrawal of the same decision was recorded")
		}
	})
	t.Run("a withdrawal is immutable", func(t *testing.T) {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE workflow_approval_withdrawal SET reason = 'rewritten' WHERE tenant_id = $1`, tenant)
			return err
		})
		if err == nil {
			t.Fatal("a withdrawal was rewritten")
		}
	})
	t.Run("another tenant sees no withdrawal", func(t *testing.T) {
		inTenantTx(t, conn, other, func(tx dbport.Tx) error {
			got, err := stepapproval.LoadWithdrawals(ctx, tx, tenant, instanceID)
			if err == nil && len(got) != 0 {
				t.Fatalf("tenant isolation leaked %d withdrawals", len(got))
			}
			return err
		})
	})
	t.Run("an incomplete withdrawal is refused before any write", func(t *testing.T) {
		for name, mutate := range map[string]func(stepapproval.Withdrawal) stepapproval.Withdrawal{
			"no reason":        func(x stepapproval.Withdrawal) stepapproval.Withdrawal { x.Reason = ""; return x },
			"bad digest":       func(x stepapproval.Withdrawal) stepapproval.Withdrawal { x.DecisionDigest = "abc"; return x },
			"undeclared kind":  func(x stepapproval.Withdrawal) stepapproval.Withdrawal { x.Invalidator.Kind = "NOPE"; return x },
			"no instant":       func(x stepapproval.Withdrawal) stepapproval.Withdrawal { x.WithdrawnAt = time.Time{}; return x },
			"no withdrawer":    func(x stepapproval.Withdrawal) stepapproval.Withdrawal { x.WithdrawnBy = ""; return x },
			"no work identity": func(x stepapproval.Withdrawal) stepapproval.Withdrawal { x.WorkItemID = [16]byte{}; return x },
		} {
			err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				_, err := stepapproval.RecordWithdrawal(ctx, tx, mutate(w))
				return err
			})
			if !errors.Is(err, stepapproval.ErrInvalidWithdrawal) {
				t.Errorf("%s: RecordWithdrawal = %v, want ErrInvalidWithdrawal", name, err)
			}
		}
	})
	t.Run("a completed slot without its decision row is invalid evidence", func(t *testing.T) {
		ghost := items[1]
		ghost.Status = workitem.StatusCompleted
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := stepapproval.LoadDecisions(ctx, tx, cont, []workitem.WorkItem{decided, ghost})
			return err
		})
		if !errors.Is(err, stepapproval.ErrInvalidEvidence) {
			t.Fatalf("LoadDecisions with a missing decision row = %v, want ErrInvalidEvidence", err)
		}
	})
}
