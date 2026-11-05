// WF-REV-008 RED: effects that cannot be undone must route to a human
// decision block instead of ending in repair with no one asked.
package repair_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
)

// TestTodo_WF_REV_008 is the PRIMARY test: a published decision fragment
// presents each remaining effect with its options (start a correction
// intent, record a waiver with authority, accept as residual) and records
// the decision and any open obligation.
func TestTodo_WF_REV_008(t *testing.T) {
	effects := []repair.RemainingEffect{
		{EffectID: "eff-pay-001", Kind: "pay.release", Description: "March payroll released to rails", Reason: repair.ReasonIrreversible},
		{EffectID: "eff-notice-002", Kind: "notice.send", Description: "Termination notice sent to employee", Reason: repair.ReasonOutOfWindow},
	}

	t.Run("publish presents every remaining effect with all three options", func(t *testing.T) {
		frag, err := repair.PublishDecisionFragment("run-7", effects)
		if err != nil {
			t.Fatalf("PublishDecisionFragment: %v", err)
		}
		pending := frag.Pending()
		if len(pending) != 2 {
			t.Fatalf("pending = %d, want 2", len(pending))
		}
		for _, p := range pending {
			opts := frag.Options(p.EffectID)
			if len(opts) != 3 {
				t.Fatalf("options for %s = %d, want 3", p.EffectID, len(opts))
			}
		}
		if frag.Complete() {
			t.Fatal("fragment complete before any decision")
		}
	})

	t.Run("correct starts an open correction obligation", func(t *testing.T) {
		frag, _ := repair.PublishDecisionFragment("run-7", effects)
		dec, err := frag.Decide("eff-pay-001", repair.OptionCorrect,
			repair.Actor{ID: "u-payroll-lead", Authority: "payroll:correct"},
			"overpayment of one shift; run an off-cycle correction")
		if err != nil {
			t.Fatalf("Decide correct: %v", err)
		}
		if dec.OpenObligationID == "" {
			t.Fatal("correction decision records no open obligation")
		}
		ob := frag.Obligation(dec.OpenObligationID)
		if ob.Kind != repair.ObligationCorrectionIntent || ob.Status != repair.ObligationOpen {
			t.Fatalf("obligation = %s/%s, want CORRECTION_INTENT/OPEN", ob.Kind, ob.Status)
		}
		if frag.Decision("eff-pay-001").Option != repair.OptionCorrect {
			t.Fatal("decision not recorded")
		}
	})

	t.Run("waive records authority and rationale", func(t *testing.T) {
		frag, _ := repair.PublishDecisionFragment("run-7", effects)
		dec, err := frag.Decide("eff-notice-002", repair.OptionWaive,
			repair.Actor{ID: "u-hr-director", Authority: "hr:waive-notice"},
			"notice wording approved by counsel; no re-send")
		if err != nil {
			t.Fatalf("Decide waive: %v", err)
		}
		if dec.Authority != "hr:waive-notice" || dec.Rationale == "" {
			t.Fatal("waiver records no authority/rationale")
		}
		if got := frag.Decision("eff-notice-002"); got.Option != repair.OptionWaive {
			t.Fatal("waiver decision not recorded")
		}
	})

	t.Run("accept records the residual", func(t *testing.T) {
		frag, _ := repair.PublishDecisionFragment("run-7", effects)
		dec, err := frag.Decide("eff-pay-001", repair.OptionAcceptResidual,
			repair.Actor{ID: "u-controller", Authority: "finance:accept"},
			"four-cent rounding residual; immaterial")
		if err != nil {
			t.Fatalf("Decide accept: %v", err)
		}
		if dec.Option != repair.OptionAcceptResidual {
			t.Fatal("residual acceptance not recorded")
		}
	})

	t.Run("waiver without authority is refused", func(t *testing.T) {
		frag, _ := repair.PublishDecisionFragment("run-7", effects)
		_, err := frag.Decide("eff-pay-001", repair.OptionWaive,
			repair.Actor{ID: "u-clerk"}, "looks fine")
		if !errors.Is(err, repair.ErrAuthorityRequired) {
			t.Fatalf("err = %v, want ErrAuthorityRequired", err)
		}
	})

	t.Run("decisions are single-shot and effects must exist", func(t *testing.T) {
		frag, _ := repair.PublishDecisionFragment("run-7", effects)
		actor := repair.Actor{ID: "u-controller", Authority: "finance:accept"}
		if _, err := frag.Decide("eff-pay-001", repair.OptionAcceptResidual, actor, "r"); err != nil {
			t.Fatalf("first decide: %v", err)
		}
		if _, err := frag.Decide("eff-pay-001", repair.OptionWaive, actor, "change of mind"); !errors.Is(err, repair.ErrAlreadyDecided) {
			t.Fatalf("re-decide err = %v, want ErrAlreadyDecided", err)
		}
		if _, err := frag.Decide("eff-nope", repair.OptionWaive, actor, "r"); !errors.Is(err, repair.ErrUnknownEffect) {
			t.Fatalf("unknown effect err = %v, want ErrUnknownEffect", err)
		}
	})

	t.Run("publish rejects empty, duplicate and reasonless effects", func(t *testing.T) {
		if _, err := repair.PublishDecisionFragment("", effects); !errors.Is(err, repair.ErrFragmentInvalid) {
			t.Fatalf("empty run err = %v, want ErrFragmentInvalid", err)
		}
		if _, err := repair.PublishDecisionFragment("r", nil); !errors.Is(err, repair.ErrFragmentInvalid) {
			t.Fatalf("nil effects err = %v, want ErrFragmentInvalid", err)
		}
		dup := []repair.RemainingEffect{effects[0], effects[0]}
		if _, err := repair.PublishDecisionFragment("r", dup); !errors.Is(err, repair.ErrFragmentInvalid) {
			t.Fatalf("dup err = %v, want ErrFragmentInvalid", err)
		}
		noreason := []repair.RemainingEffect{{EffectID: "e", Kind: "k", Description: "d"}}
		if _, err := repair.PublishDecisionFragment("r", noreason); !errors.Is(err, repair.ErrFragmentInvalid) {
			t.Fatalf("no-reason err = %v, want ErrFragmentInvalid", err)
		}
	})
}

// TestTodo_WF_REV_008_Integration cancels a run after an irreversible step
// and follows each decision option to its recorded outcome.
func TestTodo_WF_REV_008_Integration(t *testing.T) {
	committed := []repair.CommittedEffect{
		{EffectID: "eff-draft-000", Kind: "case.draft", Description: "case drafted", Reversible: true},
		{EffectID: "eff-pay-001", Kind: "pay.release", Description: "pay released", Reversible: false},
		{EffectID: "eff-notice-002", Kind: "notice.send", Description: "notice sent", Reversible: false, WindowClosed: true},
	}
	remaining := repair.RemainingAfterCancel(committed)
	if len(remaining) != 2 {
		t.Fatalf("remaining = %d, want 2 (reversible draft excluded)", len(remaining))
	}

	follow := func(t *testing.T, opt repair.DecisionOption, actor repair.Actor, rationale string) {
		t.Helper()
		frag, err := repair.PublishDecisionFragment("run-cancel-1", remaining)
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		if got := len(frag.Pending()); got != 2 {
			t.Fatalf("pending = %d, want 2", got)
		}
		for _, eff := range remaining {
			if _, err := frag.Decide(eff.EffectID, opt, actor, rationale); err != nil {
				t.Fatalf("decide %s: %v", eff.EffectID, err)
			}
		}
		if !frag.Complete() {
			t.Fatal("fragment not complete after deciding every effect")
		}
		if len(frag.Pending()) != 0 {
			t.Fatal("pending not empty after all decisions")
		}
	}

	t.Run("correction path opens one obligation per effect", func(t *testing.T) {
		frag, _ := repair.PublishDecisionFragment("run-cancel-1", remaining)
		actor := repair.Actor{ID: "u-ops", Authority: "ops:correct"}
		for _, eff := range remaining {
			dec, err := frag.Decide(eff.EffectID, repair.OptionCorrect, actor, "correct it")
			if err != nil {
				t.Fatalf("decide: %v", err)
			}
			if frag.Obligation(dec.OpenObligationID).Status != repair.ObligationOpen {
				t.Fatal("correction obligation not open")
			}
		}
	})

	t.Run("waiver path", func(t *testing.T) {
		follow(t, repair.OptionWaive, repair.Actor{ID: "u-hr-director", Authority: "hr:waive"}, "risks accepted by HR")
	})

	t.Run("residual path", func(t *testing.T) {
		follow(t, repair.OptionAcceptResidual, repair.Actor{ID: "u-controller", Authority: "finance:accept"}, "immaterial; accepted")
	})
}
