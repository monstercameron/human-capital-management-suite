package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// promoux014Findings returns the six WAIT_* codes internal/intent/app's
// journeyWaitFindings mints for a real WAITING_EFFECTIVE_DATE journey,
// spelled out here rather than imported (this module cannot import
// internal/, the same reason waitExplanationLabels restates them).
func promoux014Findings() []*journeyv1.Finding {
	return []*journeyv1.Finding{
		{Severity: "info", Code: codeWaitEffectiveInstant, Message: "Waits until 2026-12-01T05:00:00Z."},
		{Severity: "info", Code: codeWaitOwner, Message: "Approved by principal:manager-approver."},
		{Severity: "info", Code: codeWaitScheduledAction, Message: "At that instant, the workflow revalidates the promotion's pinned facts."},
		{Severity: "info", Code: codeWaitRemainingChecks, Message: "Remaining checks: revalidation, then execution and effect observation."},
		{Severity: "info", Code: codeWaitNotification, Message: "No notification is sent when this wait resolves."},
		{Severity: "info", Code: codeWaitIntervention, Message: "None. Production exposes no manual timer bypass."},
	}
}

// TestTodo_PROMOUX_014_Regression is the REGRESSION matrix test. It guards
// the two traps this todo's own brief calls out by name:
//
//  1. A component built but wired to no caller: waitExplanationFacts on its
//     own proves nothing about whether DetailPage, the actual caller,
//     feeds it real wire data. This test calls DetailPage itself, the same
//     production entry point tools/uxqual/render/journey's HTML comes
//     from, not a hand-built journey.DetailView.
//  2. A fixture that does not actually contain the condition under test: a
//     wait-explanation test whose fixture carries no waiting journey would
//     pass trivially. testDetail's stage argument here is the real enum
//     value JOURNEY_STAGE_WAITING_EFFECTIVE_DATE, and the sub-tests below
//     each assert both directions -- present when complete, absent when
//     not -- so a regression that always returns nil (or always returns
//     something) fails one side or the other.
//
// It also pins the boundary PROMOUX-014 added to findings(): the six
// WAIT_* codes must never reach the generic "Preflight and simulation"
// board, which is the render-layer trap TestTodo_PROMOUX_014_Browser
// documents from the other side.
func TestTodo_PROMOUX_014_Regression(t *testing.T) {
	t.Run("DetailPage wires a complete set of WAIT_ findings into WaitExplanation", func(t *testing.T) {
		detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE)
		detail.Findings = append(detail.Findings, promoux014Findings()...)

		p := DetailPage(testConfig(), detail, nil, nil)

		if len(p.Detail.WaitExplanation) != 6 {
			t.Fatalf("DetailPage produced %d WaitExplanation facts, want 6: %+v", len(p.Detail.WaitExplanation), p.Detail.WaitExplanation)
		}
		wantLabels := []string{"Effective instant", "Owner", "Scheduled action", "Remaining checks", "Notification", "Authorized intervention"}
		for i, want := range wantLabels {
			if p.Detail.WaitExplanation[i].Label != want {
				t.Errorf("WaitExplanation[%d].Label = %q, want %q (order matters: it is the reading order)", i, p.Detail.WaitExplanation[i].Label, want)
			}
		}

		for _, code := range []string{codeWaitEffectiveInstant, codeWaitOwner, codeWaitScheduledAction, codeWaitRemainingChecks, codeWaitNotification, codeWaitIntervention} {
			for _, f := range p.Detail.Findings {
				if f.Code == code {
					t.Errorf("wait code %s leaked into the generic Findings board", code)
				}
			}
		}
	})

	t.Run("DetailPage produces no WaitExplanation when the engine sent an incomplete set", func(t *testing.T) {
		detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE)
		incomplete := promoux014Findings()[:5] // every code except WAIT_INTERVENTION
		detail.Findings = append(detail.Findings, incomplete...)

		p := DetailPage(testConfig(), detail, nil, nil)

		if p.Detail.WaitExplanation != nil {
			t.Fatalf("WaitExplanation = %+v, want nil for an incomplete set of WAIT_ codes", p.Detail.WaitExplanation)
		}
		// The five codes that did arrive must still not leak into the
		// generic board: an incomplete explanation is still not a
		// simulation finding.
		for _, f := range p.Detail.Findings {
			if isWaitExplanationCode(f.Code) {
				t.Errorf("incomplete wait code %s leaked into the generic Findings board", f.Code)
			}
		}
	})

	t.Run("a journey with no wait findings at all gets no WaitExplanation", func(t *testing.T) {
		detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
		p := DetailPage(testConfig(), detail, nil, nil)
		if p.Detail.WaitExplanation != nil {
			t.Fatalf("WaitExplanation = %+v, want nil for a journey that never reached the wait", p.Detail.WaitExplanation)
		}
	})
}
