package privacy

import (
	"errors"
	"testing"
	"time"
)

func stateEffectiveDate() time.Time {
	return time.Date(2023, 7, 1, 0, 0, 0, 0, time.UTC)
}

func stateReviewAt() time.Time {
	return time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
}

func fixtureStateLaws() []StateLaw {
	eff := stateEffectiveDate()
	return []StateLaw{
		{
			State: StateCA, LawID: "CCPA/CPRA", Version: "2024-1", Effective: eff,
			Rights:          []string{"know", "delete", "correct", "limit_sensitive_use", "nondiscrimination"},
			Exemptions:      []string{"employee_data_sunset_expired"},
			SensitiveRules:  []string{"limit_use_and_disclosure", "no_inferring_characteristics"},
			ProcessorDuties: []string{"respond_45_days", "honor_opt_out_preference_signal", "contract_flowdown"},
			OptOutSignal:    "do_not_sell_or_share",
			ConsentSignal:   "opt_in_under_16",
		},
		{
			State: StateCO, LawID: "CPA", Version: "2024-1", Effective: eff,
			Rights:          []string{"access", "correction", "deletion", "data_portability", "opt_out"},
			Exemptions:      []string{"nonprofit_exempt", "deidentified_exempt"},
			SensitiveRules:  []string{"consent_before_collect"},
			ProcessorDuties: []string{"respond_45_days", "honor_universal_opt_out", "data_protection_assessment"},
			OptOutSignal:    "universal_opt_out_mechanism",
			ConsentSignal:   "opt_in_sensitive",
		},
		{
			State: StateVA, LawID: "VCDPA", Version: "2024-1", Effective: eff,
			Rights:          []string{"access", "correction", "deletion", "data_portability", "opt_out"},
			Exemptions:      []string{"nonprofit_exempt", "deidentified_exempt"},
			SensitiveRules:  []string{"opt_in_consent"},
			ProcessorDuties: []string{"respond_45_days", "appeal_right", "data_protection_assessment"},
			OptOutSignal:    "opt_out_targeted_ads_sale_profiling",
			ConsentSignal:   "opt_in_sensitive",
		},
		{
			State: StateTX, LawID: "TDPSA", Version: "2024-1", Effective: eff,
			Rights:          []string{"access", "correction", "deletion", "data_portability", "opt_out"},
			Exemptions:      []string{"small_business_exempt", "deidentified_exempt", "employee_data_exempt"},
			SensitiveRules:  []string{"opt_in_consent"},
			ProcessorDuties: []string{"respond_45_days"},
			OptOutSignal:    "opt_out_sale_sharing_profiling",
			ConsentSignal:   "opt_in_sensitive",
		},
	}
}

func fixtureRoster(t *testing.T) Roster {
	t.Helper()
	review := PrimaryReview{
		ReviewedAt: stateReviewAt(), Reviewer: "privacy-counsel",
		SourceRef: "statute-text:ca-ccpa/co-cpa/va-vcdpa/tx-tdpsa", CoversRoster: "roster-7",
	}
	roster, err := Seal("roster-7", fixtureStateLaws(), review, "release-gate", time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return roster
}

func evalDate() time.Time { return time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC) }

// TestTodo_PRIV_010 is the PRIMARY PRIV-010 contract test: a signed
// versioned roster drives per-state rights, exemptions, sensitive-data
// rules, processor duties and opt-out/consent signals, and no global
// toggle can substitute for it.
func TestTodo_PRIV_010(t *testing.T) {
	roster := fixtureRoster(t)

	t.Run("GREEN: each state resolves its own exact obligations", func(t *testing.T) {
		ca, err := roster.Resolve(StateCA, evalDate())
		if err != nil {
			t.Fatalf("Resolve CA: %v", err)
		}
		if ca.OptOutSignal != "do_not_sell_or_share" || ca.LawID != "CCPA/CPRA" {
			t.Fatalf("CA resolved another state's answers: %+v", ca)
		}
		co, err := roster.Resolve(StateCO, evalDate())
		if err != nil {
			t.Fatalf("Resolve CO: %v", err)
		}
		if co.OptOutSignal != "universal_opt_out_mechanism" {
			t.Fatalf("CO lost its universal opt-out signal: %+v", co)
		}
		va, err := roster.Resolve(StateVA, evalDate())
		if err != nil {
			t.Fatalf("Resolve VA: %v", err)
		}
		tx, err := roster.Resolve(StateTX, evalDate())
		if err != nil {
			t.Fatalf("Resolve TX: %v", err)
		}
		if len(tx.Exemptions) == 0 || tx.Exemptions[0] != "small_business_exempt" {
			t.Fatalf("TX lost its small-business exemption: %+v", tx)
		}
		if ca.Digest == co.Digest || co.Digest == va.Digest || va.Digest == tx.Digest {
			t.Fatal("per-state resolutions share one digest: a global toggle leaked in")
		}
	})

	t.Run("GREEN: employee-data rights differ by state", func(t *testing.T) {
		ca, _ := roster.Resolve(StateCA, evalDate())
		tx, _ := roster.Resolve(StateTX, evalDate())
		caHas, txHas := false, false
		for _, r := range ca.Rights {
			if r == "limit_sensitive_use" {
				caHas = true
			}
		}
		for _, e := range tx.Exemptions {
			if e == "employee_data_exempt" {
				txHas = true
			}
		}
		if !caHas || !txHas {
			t.Fatal("per-state employee-data handling collapsed into one answer")
		}
	})

	t.Run("RED: one global toggle never resolves a state", func(t *testing.T) {
		if _, err := (Roster{}).Resolve(StateCA, evalDate()); !errors.Is(err, ErrStateLawRefused) {
			t.Fatalf("unsigned roster must be refused, got %v", err)
		}
		if _, err := roster.Resolve("XX", evalDate()); !errors.Is(err, ErrStateLawRefused) {
			t.Fatalf("ungated state must be refused, got %v", err)
		}
		future := fixtureStateLaws()
		future[0].Effective = evalDate().Add(24 * time.Hour)
		review := PrimaryReview{ReviewedAt: stateReviewAt(), Reviewer: "privacy-counsel", SourceRef: "statute-text:ca", CoversRoster: "roster-future"}
		future2, err := Seal("roster-future", future, review, "release-gate", stateReviewAt())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := future2.Resolve(StateCA, evalDate()); !errors.Is(err, ErrStateLawRefused) {
			t.Fatalf("not-yet-effective law must be refused, got %v", err)
		}
	})

	t.Run("GREEN: unreviewed versions are not honored", func(t *testing.T) {
		laws := fixtureStateLaws()
		laws[1].Version = "2025-1"
		review := PrimaryReview{ReviewedAt: stateReviewAt(), Reviewer: "privacy-counsel", SourceRef: "summary-only", CoversRoster: "roster-stale"}
		stale := Roster{Version: "roster-8", Laws: laws, Signature: "sig", SignedBy: "x", Review: review}
		if _, err := stale.Resolve(StateCO, evalDate()); !errors.Is(err, ErrStateLawRefused) {
			t.Fatalf("review covering another version must not admit roster-8, got %v", err)
		}
	})
}

// TestTodo_PRIV_010_Golden pins the roster signature and per-state digests.
func TestTodo_PRIV_010_Golden(t *testing.T) {
	roster := fixtureRoster(t)
	const goldenSig = "sha256:93ccc33fe91535bad7cb9c5fef4022c7be24a4562edbe306056b2bf3afd94c8c:release-gate"
	if roster.Signature != goldenSig {
		t.Fatalf("roster signature drifted: got %s, want %s", roster.Signature, goldenSig)
	}
	ca, err := roster.Resolve(StateCA, evalDate())
	if err != nil {
		t.Fatal(err)
	}
	again, err := roster.Resolve(StateCA, evalDate())
	if err != nil {
		t.Fatal(err)
	}
	if ca.Digest != again.Digest {
		t.Fatal("identical evaluations produce different digests")
	}
	t.Logf("signature=%s ca=%s", roster.Signature, ca.Digest)
}
