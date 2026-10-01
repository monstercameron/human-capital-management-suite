package application

import (
	"context"
	"fmt"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func uxblind093ThresholdInputs(t *testing.T, h *promoux015Harness, intentID string) (string, string) {
	t.Helper()
	services, err := app.NewPromotionStepServices(h.composed.Cell())
	if err != nil {
		t.Fatalf("compose promotion step services: %v", err)
	}
	principal := h.principals["admin"]
	call := app.PromotionStepCall{
		Delegation: runtime.ExecutionDelegation{Subject: principal.Subject(), SubjectKind: principal.SubjectKind().String(), TenantKey: principal.Tenant().String(), OrganizationScopeID: principal.OrganizationScopeID(), Roles: principal.Roles(), Purposes: []string{authz.PurposeCompensationReview}, AuthenticationMethod: principal.AuthenticationMethod().String(), Assurance: principal.Assurance().String(), SessionRef: principal.SessionRef(), EvidenceRef: principal.EvidenceID(), RecordedAt: time.Now().UTC()},
		IntentID:   intentID, NodeID: promotionexec.NodeRaiseThreshold, IdempotencyKey: "test:uxblind093:threshold", Deadline: time.Now().UTC().Add(time.Minute), DeclaredEffects: []capability.EffectClass{capability.EffectPure, capability.EffectReadOnly},
	}
	in, _, err := services.ThresholdInputs(context.Background(), call)
	if err != nil {
		t.Fatalf("derive actual threshold inputs: %v", err)
	}
	return in.IncreasePercent.String(), fmt.Sprintf("band=%s budget=%s grade_change=%t", in.BandPosition, in.BudgetAuthority, in.GradeChange)
}

// uxblind093WaitForSubscription is a readiness barrier for the served
// scheduler. ResumeFiredTimer returns after the durable advancement, while
// the signal subscription is the observable contract needed by a provider.
// Polling that durable row with a bounded context avoids a timing sleep and
// makes a missing wait fail with the actual readiness error.
func uxblind093WaitForSubscription(t *testing.T, h *promoux015Harness, node string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var open int64
		if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM workflow_signal_subscription WHERE node_id = $1 AND subscription_state = 'OPEN'`, node).Scan(&open); err == nil && open == 1 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for %s readiness: %v", node, ctx.Err())
		case <-ticker.C:
		}
	}
}

func uxblind093ConfirmPayroll(t *testing.T, h *promoux015Harness) {
	t.Helper()
	placement := h.committedPlacement(h.afterEffectiveDate()())
	h.confirmProvider(payrollProviderWait, promotionsteps.ProviderReceipt{Outcome: promotionsteps.ProviderOutcomeApplied, Details: map[string]string{
		promotionsteps.ProviderDetailAmount:        placement.BasePay,
		promotionsteps.ProviderDetailCurrency:      placement.Currency,
		promotionsteps.ProviderDetailEffectiveDate: h.effective,
	}})
}

// TestTodo_UXBLIND_093 proves the reachable promotion commit opens the
// payroll provider wait, remains non-terminal while that wait is open, and
// closes that durable wait after the provider's correlated confirmation.
func TestTodo_UXBLIND_093(t *testing.T) {
	h := promoux015ComposeWithPublishedWorkforce(t)
	h.runSeparatedPromotion()
	if fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background()); err != nil || fired.Fired != 1 {
		t.Fatalf("effective-date scheduler tick = %+v, %v; want one fired timer", fired, err)
	}
	uxblind093WaitForSubscription(t, h, promotionexec.NodeAwaitPayrollConfirmation)
	if got := h.wfrun034Count(`SELECT count(*) FROM workflow_signal_subscription WHERE node_id = $1 AND subscription_state = 'OPEN'`, promotionexec.NodeAcknowledgeRelease); got != 0 {
		t.Fatalf("acknowledgement wait opened before providers answered = %d, want 0", got)
	}
	uxblind093ConfirmPayroll(t, h)
	if got := h.wfrun034Count(`SELECT count(*) FROM workflow_signal_subscription WHERE node_id = $1 AND subscription_state = 'OPEN'`, promotionexec.NodeAwaitPayrollConfirmation); got != 0 {
		t.Fatalf("payroll wait remained open after confirmation = %d, want 0", got)
	}
	if got := h.wfrun034Count(`SELECT count(*) FROM workflow_signal_subscription WHERE node_id = $1 AND subscription_state = 'OPEN'`, promotionexec.NodeAwaitAccessConfirmation); got != 1 {
		t.Fatalf("access wait after payroll confirmation = %d, want 1", got)
	}
}

// TestTodo_UXBLIND_093_Browser checks the served journey projection after the
// effective-date transition. It is the native server/component proof: the
// parent lane must still run the real browser at its required viewport because
// Go tests cannot measure painted layout or browser accessibility output.
func TestTodo_UXBLIND_093_Browser(t *testing.T) {
	h := promoux015ComposeWithPublishedWorkforce(t)
	id := h.runSeparatedPromotion()
	if _, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background()); err != nil {
		t.Fatalf("effective-date scheduler tick: %v", err)
	}
	uxblind093WaitForSubscription(t, h, promotionexec.NodeAwaitPayrollConfirmation)
	detail, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("InspectJourney while payroll confirmation is pending: %v", err)
	}
	if detail.GetDetail().GetJourney().GetStage().String() == "RECORDED" {
		t.Fatal("browser-facing journey reported RECORDED while payroll confirmation was still open")
	}
}

// TestTodo_UXBLIND_093_Regression proves both provider waits are chained and
// the real engine reaches its terminal only after the separate acknowledgement.
func TestTodo_UXBLIND_093_Regression(t *testing.T) {
	h := promoux015ComposeWithPublishedWorkforce(t)
	id := h.runSeparatedPromotion()
	beforePercent, beforeFacts := uxblind093ThresholdInputs(t, h, id)
	if _, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background()); err != nil {
		t.Fatalf("effective-date scheduler tick: %v", err)
	}
	uxblind093WaitForSubscription(t, h, promotionexec.NodeAwaitPayrollConfirmation)
	transitionPercent, transitionFacts := uxblind093ThresholdInputs(t, h, id)
	t.Logf("RULE-004 actual threshold inputs before=%s (%s), after effective-date transition=%s (%s)", beforePercent, beforeFacts, transitionPercent, transitionFacts)
	uxblind093ConfirmPayroll(t, h)
	uxblind093WaitForSubscription(t, h, promotionexec.NodeAwaitAccessConfirmation)
	h.confirmProvider(accessProviderWait, promotionsteps.ProviderReceipt{Outcome: promotionsteps.ProviderOutcomeGranted, Details: map[string]string{
		promotionsteps.ProviderDetailJobCode: "PPL-HRBP4", promotionsteps.ProviderDetailGrade: "P5",
	}})
	if got := h.wfrun034Count(`SELECT count(*) FROM workflow_signal_subscription WHERE node_id = $1 AND subscription_state = 'OPEN'`, promotionexec.NodeAcknowledgeRelease); got != 1 {
		t.Fatalf("acknowledgement waits after provider confirmations = %d, want 1", got)
	}
	if got := h.journeyFor("hiring-manager", id).GetStage().String(); got == "RECORDED" {
		t.Fatal("promotion completed before acknowledgement")
	}
	h.acknowledgeParkedPromotion(id)
	if got := h.journeyFor("hiring-manager", id).GetStage().String(); got != "RECORDED" {
		t.Fatalf("promotion after acknowledgement = %s, want RECORDED", got)
	}
}
