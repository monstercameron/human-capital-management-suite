package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// Unit 1b (spec-compliance plan): the acknowledgement gate, served. A funded
// promotion that reconciles every downstream leg parks on the gate with a
// durable OPEN subscription instead of completing silently; the HR operator's
// attestation receives the correlated signal and resumes the driver to
// PROMOTION_COMPLETE; the initiator cannot attest their own promotion; and a
// second attestation after completion is a stage refusal, never a second
// wakeup.

func (h *promoux015Harness) ackOperatorCtx() context.Context {
	h.t.Helper()
	now := time.Now()
	token, err := h.verifier.Issue(trust.Claims{
		Issuer: h.cfg.Issuer, Audience: h.cfg.Audience, Subject: "principal:promoux015-acknowledger", SubjectKind: "human", Tenant: h.cfg.Tenant,
		OrganizationScopeID: "org:" + h.cfg.Tenant + ":people-ops", Roles: []string{"hcm_admin", "comp_admin", "promotion_operator"},
		Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-promoux015-acknowledger",
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		h.t.Fatalf("issue acknowledger credential: %v", err)
	}
	operator, err := h.verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: h.cfg.Audience})
	if err != nil {
		h.t.Fatalf("verify acknowledger credential: %v", err)
	}
	return trust.WithPrincipal(context.Background(), operator)
}

func TestTodo_PROMO_ACK_ServedCompletionAndRefusals(t *testing.T) {
	h := promoux015Compose(t)
	id := h.runSeparatedPromotion()
	fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background())
	if err != nil || fired.Fired != 1 {
		t.Fatalf("Tick after the effective date = %+v, %v; want one timer fired", fired, err)
	}
	// Promotion 1.1.0: the providers confirm the committed change first.
	h.confirmProviders()
	if got := h.wfrun034Count(`SELECT count(*) FROM workflow_signal_subscription WHERE node_id = $1 AND subscription_state = 'OPEN'`, promotionexec.NodeAcknowledgeRelease); got != 1 {
		t.Fatalf("open acknowledgement subscriptions = %d, want 1", got)
	}

	// The initiator cannot attest their own promotion, and the refusal
	// writes nothing: the wait stays open.
	engine, initiatorCtx := h.engine("hiring-manager")
	if _, err := engine.Acknowledge(initiatorCtx, id, workspace.Acknowledgement{EvidenceRef: "hris:signature:self"}); !errors.Is(err, workspace.ErrDenied) {
		t.Fatalf("Acknowledge as the initiator = %v, want ErrDenied", err)
	}
	if got := h.wfrun034Count(`SELECT count(*) FROM workflow_signal_subscription WHERE node_id = $1 AND subscription_state = 'OPEN'`, promotionexec.NodeAcknowledgeRelease); got != 1 {
		t.Fatalf("open acknowledgement subscriptions after refused attestation = %d, want 1", got)
	}

	acked, err := h.composed.Cell().Journey.Acknowledge(h.ackOperatorCtx(), id, workspace.Acknowledgement{
		EvidenceRef: "hris:signature:ack-1", Note: "signed copy verified against the HRIS record",
	})
	if err != nil {
		t.Fatalf("Journey.Acknowledge as the HR operator: %v", err)
	}
	if got := acked.Summary.Stage; got != workspace.JourneyStageRecorded {
		t.Fatalf("acknowledged journey stage = %s, want RECORDED", got)
	}
	routes := h.wfrun034Routes()
	if !wfrun034Took(routes, promotionexec.NodeAcknowledgeRelease, promotionexec.NodeEndComplete) {
		t.Fatalf("after the acknowledgement the run did not advance to completion; routes %+v", routes)
	}
	completed := false
	for _, r := range routes {
		completed = completed || (r.kind == "COMPLETE" && r.terminal == "PROMOTION_COMPLETE")
	}
	if !completed {
		t.Fatalf("no PROMOTION_COMPLETE terminal: %+v", routes)
	}

	// The receipt is inspectable evidence: one signal, one ACCEPTED
	// disposition, and a payload naming the attester and the artifact.
	if got := h.wfrun034Count(`SELECT count(*) FROM workflow_signal WHERE event_type = $1`, "hcmnext.events.promotion_ack"); got != 1 {
		t.Fatalf("acknowledgement signals = %d, want 1", got)
	}
	// One ACCEPTED disposition per wait: the payroll and identity
	// providers' confirmations and the acknowledgement.
	if got := h.wfrun034Count(`SELECT count(*) FROM workflow_signal_disposition WHERE status = 'ACCEPTED'`); got != 3 {
		t.Fatalf("ACCEPTED dispositions = %d, want 3", got)
	}
	var payload string
	if err := h.pool.QueryRow(context.Background(), `SELECT payload::text FROM workflow_signal WHERE event_type = $1`, "hcmnext.events.promotion_ack").Scan(&payload); err != nil {
		t.Fatalf("read acknowledgement payload: %v", err)
	}
	for _, want := range []string{"principal:promoux015-acknowledger", "hris:signature:ack-1", id} {
		if !strings.Contains(payload, want) {
			t.Fatalf("acknowledgement payload = %s, want it to name %q", payload, want)
		}
	}

	// A second attestation after completion is a stage refusal: the wait is
	// settled, and no second wakeup exists to consume.
	if _, err := h.composed.Cell().Journey.Acknowledge(h.ackOperatorCtx(), id, workspace.Acknowledgement{}); !errors.Is(err, workspace.ErrJourneyStage) {
		t.Fatalf("second Acknowledge = %v, want ErrJourneyStage", err)
	}
}

// acknowledgeParkedPromotion plays the payroll and identity providers
// confirming the committed change (promotion 1.1.0), then records the HR
// operator's attestation against the journey's open acknowledgement gate and
// requires the run to reach its recorded terminal. Every served commit path
// parks at the provider waits and then at the gate, so a test asserting
// terminal effects must clear them first; the exactly-once oracles after it
// are unchanged.
func (h *promoux015Harness) acknowledgeParkedPromotion(id string) {
	h.t.Helper()
	h.confirmProviders()
	acked, err := h.composed.Cell().Journey.Acknowledge(h.ackOperatorCtx(), id, workspace.Acknowledgement{
		EvidenceRef: "hris:signature:served-commit", Note: "operator attestation recorded on the served path",
	})
	if err != nil {
		h.t.Fatalf("Journey.Acknowledge as the HR operator: %v", err)
	}
	if got := acked.Summary.Stage; got != workspace.JourneyStageRecorded {
		h.t.Fatalf("acknowledged journey stage = %s, want RECORDED", got)
	}
}

func TestTodo_PROMO_ACK_UnknownIntent(t *testing.T) {
	h := promoux015Compose(t)
	if _, err := h.composed.Cell().Journey.Acknowledge(h.ackOperatorCtx(), "intent:does-not-exist", workspace.Acknowledgement{}); !errors.Is(err, workspace.ErrJourneyUnknown) {
		t.Fatalf("Acknowledge of an unknown intent = %v, want ErrJourneyUnknown", err)
	}
}
