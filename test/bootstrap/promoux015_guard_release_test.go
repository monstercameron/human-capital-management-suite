package bootstrap_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

func TestTodo_PROMOUX_015_Recovery_TerminalReleasesPromotionWindow(t *testing.T) {
	h := newJourneyHarness(t)
	proposer := h.operatorCtx(t)
	proposal, err := h.engine.Propose(proposer, journeyProposalFor("omar-reyes"))
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := h.engine.Execute(proposer, proposal.IntentID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	completed, err := h.engine.Decide(h.approverCtx(t), proposal.IntentID, workspace.Decision{Approve: true, Reason: "approved"})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if completed.Summary.Stage != workspace.JourneyStageCompleted || completed.Ledger == nil {
		t.Fatalf("promotion did not reach one governed terminal outcome: %+v", completed.Summary)
	}
	status := queryOne[string](t, h.cell,
		`SELECT status FROM promotion_active_intent_guard WHERE tenant_id = $1 AND intent_id = $2::uuid`,
		pgstore.TenantID(testTenant), proposal.IntentID)
	if status != "CLOSED" {
		t.Fatalf("terminal promotion left its admission window %q; want CLOSED", status)
	}
}

func TestTodo_PROMOUX_015_Recovery_RejectedPromotionReleasesWindow(t *testing.T) {
	h := newJourneyHarness(t)
	proposer := h.operatorCtx(t)
	proposal, err := h.engine.Propose(proposer, journeyProposalFor("omar-reyes"))
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := h.engine.Execute(proposer, proposal.IntentID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rejected, err := h.engine.Decide(h.approverCtx(t), proposal.IntentID, workspace.Decision{Approve: false, Reason: "not approved"})
	if err != nil {
		t.Fatalf("Decide reject: %v", err)
	}
	if rejected.Summary.Stage != workspace.JourneyStageRejected {
		t.Fatalf("rejection did not reach its terminal outcome: %+v", rejected.Summary)
	}
	var payload []byte
	if err := h.cell.pool.QueryRow(context.Background(), `
		SELECT payload FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		pgstore.TenantID(testTenant), effects.StreamKeyFor(prototype.ApprovalWorkflowID, rejected.Instance.InstanceID)).Scan(&payload); err != nil {
		t.Fatalf("read rejected terminal outcome: %v", err)
	}
	var outcome struct {
		TerminalCode string `json:"terminal_code"`
	}
	if err := json.Unmarshal(payload, &outcome); err != nil || outcome.TerminalCode != "REJECTED" {
		t.Fatalf("rejected terminal payload = %q, decode error = %v", payload, err)
	}
	status := queryOne[string](t, h.cell,
		`SELECT status FROM promotion_active_intent_guard WHERE tenant_id = $1 AND intent_id = $2::uuid`,
		pgstore.TenantID(testTenant), proposal.IntentID)
	if status != "CLOSED" {
		t.Fatalf("rejected promotion left its admission window %q; want CLOSED", status)
	}
}

func TestTodo_PROMOUX_015_Recovery_UnconfirmedReservationClosesAtTerminal(t *testing.T) {
	h := newJourneyHarness(t)
	proposer := h.operatorCtx(t)
	proposal, err := h.engine.Propose(proposer, journeyProposalFor("omar-reyes"))
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	// Model a lost post-CreateIntent confirmation: the intent and admission
	// both committed, but the reservation never acquired its intent ID.
	if _, err := h.cell.pool.Exec(context.Background(), `UPDATE promotion_active_intent_guard SET intent_id = NULL
		WHERE tenant_id = $1 AND intent_id = $2::uuid`, pgstore.TenantID(testTenant), proposal.IntentID); err != nil {
		t.Fatalf("inject lost confirmation: %v", err)
	}
	if _, err := h.engine.Execute(proposer, proposal.IntentID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	completed, err := h.engine.Decide(h.approverCtx(t), proposal.IntentID, workspace.Decision{Approve: true, Reason: "approved"})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if completed.Summary.Stage != workspace.JourneyStageCompleted {
		t.Fatalf("promotion did not complete: %+v", completed.Summary)
	}
	status := queryOne[string](t, h.cell,
		`SELECT status FROM promotion_active_intent_guard WHERE tenant_id = $1
		  AND idempotency_key = (SELECT idempotency_key FROM intent_instance
		      WHERE tenant_id = $1 AND intent_id = $2::uuid)`,
		pgstore.TenantID(testTenant), proposal.IntentID)
	if status != "CLOSED" {
		t.Fatalf("unconfirmed admission window remained %q; want CLOSED", status)
	}
}
