package runtime_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_WF_EXT_007(t *testing.T) {
	t.Run("trigger and parent are typed sources", func(t *testing.T) {
		trigger := runtime.StartSource{Kind: runtime.StartSourceTrigger, IntentType: "hcmnext.people.hire", Trigger: &runtime.TriggerStartSource{
			TriggerID: "trigger:hire-date", TriggerType: "DATE", Key: "2026-09-23:employee-1",
		}}
		if err := trigger.Validate(); err != nil {
			t.Fatalf("valid trigger source: %v", err)
		}
		parent := runtime.StartSource{Kind: runtime.StartSourceParent, IntentType: "hcmnext.people.hire", Parent: &runtime.ParentStartSource{
			InstanceID: uuid.New(), WorkflowID: "workflow:onboarding", ChildKey: "employee:1",
		}}
		if err := parent.Validate(); err != nil {
			t.Fatalf("valid parent source: %v", err)
		}
		if err := (runtime.StartSource{Kind: runtime.StartSourceTrigger, IntentType: "hcmnext.people.hire", Parent: parent.Parent}).Validate(); runtime.CodeOf(err) != runtime.CodeInvalidRecord {
			t.Fatalf("mismatched source payload error = %v; want INVALID_RECORD", err)
		}
	})

	t.Run("proposal source retains its typed intent", func(t *testing.T) {
		revision := newTestProposalRevision(t, "intent:wf-ext-007-proposal", values.TenantId("wfext007-proposal"))
		source := runtime.StartSource{Kind: runtime.StartSourceProposal, IntentType: "hcmnext.people.promotion",
			Proposal: &runtime.ProposalBinding{Revision: revision}}
		if err := source.Validate(); err != nil {
			t.Fatalf("valid proposal source: %v", err)
		}
		_, err := runtime.Start(context.Background(), nil, runtime.StartRequest{Source: &runtime.StartSource{
			Kind: runtime.StartSourceProposal, Proposal: source.Proposal,
		}})
		if runtime.CodeOf(err) != runtime.CodeInvalidRecord {
			t.Fatalf("missing typed intent error = %v", err)
		}
	})

	t.Run("registration keys control revalidation", func(t *testing.T) {
		evidence := runtime.RevalidationEvidence{
			Pinned:  map[string]string{"manager": "person:1", "policy": "v1"},
			Current: map[string]string{"manager": "person:2", "policy": "v1"},
		}
		result, err := runtime.EvaluateRevalidation([]string{"manager", "policy"}, evidence)
		if runtime.CodeOf(err) != runtime.CodeReapprovalRequired {
			t.Fatalf("changed revalidation error = %v", err)
		}
		if result.ChangedFact != "manager" || result.Requirement != runtime.RevalidationReapprovalRequired {
			t.Fatalf("changed result = %+v", result)
		}
		confirmed, err := runtime.EvaluateRevalidation([]string{"policy"}, evidence)
		if err != nil || !confirmed.Confirmed {
			t.Fatalf("unchanged declared key = %+v, %v", confirmed, err)
		}
		if _, err := runtime.EvaluateRevalidation([]string{"missing"}, evidence); runtime.CodeOf(err) != runtime.CodeInvalidRecord {
			t.Fatalf("missing key error = %v; want INVALID_RECORD", err)
		}
	})
}

func TestTodo_WF_EXT_007_Integration(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "wfext007-trigger")
	pf := newPromotionFixture(t, values.TenantId("wfext007-trigger"), "intent:wf-ext-007-trigger")
	req := pf.baseStartRequest(tenant, "trigger:start:2026-09-23")
	req.Proposal = runtime.ProposalBinding{}
	req.ProposalFacts, req.ApprovalFacts = nil, nil
	req.Source = &runtime.StartSource{Kind: runtime.StartSourceTrigger, IntentType: workflow.PromotionReferenceDefinition().IntentType, Trigger: &runtime.TriggerStartSource{
		TriggerID: "trigger:hire-date", TriggerType: "DATE", Key: "2026-09-23:employee:1",
	}}
	var started runtime.StartReceipt
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		started, err = runtime.Start(context.Background(), tx, req)
		return err
	})
	if started.InstanceID == uuid.Nil || started.WorkflowID != pf.Plan.WorkflowID {
		t.Fatalf("typed trigger start receipt = %+v", started)
	}
	var executionContext runtime.ExecutionContext
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var found bool
		var err error
		executionContext, found, err = runtime.LoadExecutionContext(context.Background(), tx, tenant, started.InstanceID)
		if err == nil && !found {
			t.Error("typed trigger start did not persist its execution context")
		}
		return err
	})
	if executionContext.Principal != "trigger:trigger:hire-date" || executionContext.PrincipalKind != "SYSTEM" {
		t.Fatalf("trigger principal = %q (%s)", executionContext.Principal, executionContext.PrincipalKind)
	}
	if executionContext.ExecutionMode != workflow.ModeSimulate {
		t.Fatalf("trigger execution mode = %q", executionContext.ExecutionMode)
	}
}

func TestTodo_WF_EXT_007_Recovery(t *testing.T) {
	keys := []string{"manager", "policy"}
	evidence := runtime.RevalidationEvidence{
		Pinned:  map[string]string{"manager": "m1", "policy": "p1"},
		Current: map[string]string{"manager": "m1", "policy": "p2"},
	}
	first, firstErr := runtime.EvaluateRevalidation(keys, evidence)
	second, secondErr := runtime.EvaluateRevalidation(keys, evidence)
	if runtime.CodeOf(firstErr) != runtime.CodeOf(secondErr) || first.ChangedFact != second.ChangedFact {
		t.Fatalf("revalidation retry changed outcome: first=(%+v,%v) second=(%+v,%v)", first, firstErr, second, secondErr)
	}
}
