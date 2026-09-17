package legal

import (
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func boolPtr(b bool) *bool { return &b }

// classificationPack is a fixture release carrying the two CLASSIFICATION
// dimensions LEGAL-005 decides under. Citations are fixture markers, never
// counsel-approved interpretations.
func classificationPack(t *testing.T) RulePack {
	t.Helper()
	cite := func() Citation {
		return Citation{
			SourceFile:       "planning/research/state-employment-law/texas.md",
			Section:          "fixture-classification",
			Status:           ReviewStatusUnreviewed,
			ConfidenceMarker: ConfidenceMarkerConfirmed,
		}
	}
	window, err := NewOpenEffectiveWindow(mustDate(t, 2020, time.January, 1))
	if err != nil {
		t.Fatal(err)
	}
	return RulePack{
		PackID:            "us-tx-classification-test",
		Version:           1,
		VocabularyVersion: VocabularyVersion2,
		Jurisdiction:      testTXJurisdiction(),
		Window:            window,
		Classifications: []ClassificationRule{
			{
				ID:              "tx-exempt-test",
				Dimension:       "EXEMPTION",
				TestDescription: "salary basis plus duties test",
				SalaryThreshold: mustMoneyValue("35568"),
				Citation:        cite(),
			},
			{
				ID:              "tx-contractor-test",
				Dimension:       "CONTRACTOR",
				TestDescription: "independence test",
				Citation:        cite(),
			},
		},
	}
}

func classificationFixture(t *testing.T) (*LegalContext, *Registry, *Signer) {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(classificationPack(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	signer := fixedSigner(t, 0x50)
	ctx, err := Resolve(validInput(t, testTXJurisdiction()), registry, signer, mustInstant(t, 1_770_100_000))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return ctx, registry, signer
}

func classificationFactors() ClassificationFactors {
	return ClassificationFactors{
		SalaryBasisPaid:           boolPtr(true),
		AnnualSalary:              mustMoneyValue("70000"),
		SalaryStated:              true,
		DutiesTestSatisfied:       boolPtr(true),
		ContractorIndependenceMet: boolPtr(false),
		WorkAuthorized:            boolPtr(true),
	}
}

func mustMoneyValue(amount string) values.Money {
	m, err := values.NewMoney(amount, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		panic(err)
	}
	return m
}

func classificationInput(t *testing.T, f ClassificationFactors) ClassificationInput {
	t.Helper()
	return ClassificationInput{
		WorkerID:                  "worker-1",
		Proposed:                  ClassificationExempt,
		WorkAuthorizationRequired: true,
		Factors:                   f,
		ObservedAsOf:              mustDate(t, 2026, time.February, 15),
		EffectiveDate:             mustDate(t, 2026, time.March, 1),
		MaxFactorAgeDays:          90,
	}
}

func TestTodo_LEGAL_005(t *testing.T) {
	ctx, registry, signer := classificationFixture(t)
	now := mustInstant(t, 1_770_100_000)
	known := mustKnownAt(t, 1_770_000_000)

	classify := func(input ClassificationInput) ClassificationResult {
		t.Helper()
		input.KnownAt = known
		result, err := ClassifyWorker(ctx, input, registry, signer, now)
		if err != nil {
			t.Fatalf("ClassifyWorker: %v", err)
		}
		return result
	}

	t.Run("RED/incomplete factors never decide conclusively", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			mutate func(*ClassificationFactors)
		}{
			{"unknown salary basis", func(f *ClassificationFactors) { f.SalaryBasisPaid = nil }},
			{"unstated salary", func(f *ClassificationFactors) { f.SalaryStated = false }},
			{"unknown duties", func(f *ClassificationFactors) { f.DutiesTestSatisfied = nil }},
			{"unknown work authorization", func(f *ClassificationFactors) { f.WorkAuthorized = nil }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := classificationFactors()
				tc.mutate(&f)
				result := classify(classificationInput(t, f))
				if result.Outcome != ClassificationReviewRequired {
					t.Fatalf("outcome = %s, want REVIEW_REQUIRED", result.Outcome)
				}
			})
		}
	})

	t.Run("RED/stale factors never decide conclusively", func(t *testing.T) {
		f := classificationFactors()
		input := classificationInput(t, f)
		input.ObservedAsOf = mustDate(t, 2025, time.January, 1)
		result := classify(input)
		if result.Outcome != ClassificationReviewRequired {
			t.Fatalf("stale outcome = %s, want REVIEW_REQUIRED", result.Outcome)
		}
	})

	t.Run("RED/future factors never decide conclusively", func(t *testing.T) {
		f := classificationFactors()
		input := classificationInput(t, f)
		input.ObservedAsOf = mustDate(t, 2026, time.April, 1)
		result := classify(input)
		if result.Outcome != ClassificationReviewRequired {
			t.Fatalf("future-dated outcome = %s, want REVIEW_REQUIRED", result.Outcome)
		}
	})

	t.Run("RED/contractor unknowns route to review", func(t *testing.T) {
		f := classificationFactors()
		f.ContractorIndependenceMet = nil
		input := classificationInput(t, f)
		input.Proposed = ClassificationContractor
		input.WorkAuthorizationRequired = false
		result := classify(input)
		if result.Outcome != ClassificationReviewRequired {
			t.Fatalf("outcome = %s, want REVIEW_REQUIRED", result.Outcome)
		}
	})

	t.Run("GREEN/complete exempt facts are eligible with evidence", func(t *testing.T) {
		result := classify(classificationInput(t, classificationFactors()))
		if result.Outcome != ClassificationEligible {
			t.Fatalf("outcome = %s, want ELIGIBLE", result.Outcome)
		}
		if result.RuleRelease.PackID != "us-tx-classification-test" || result.RuleRelease.Version != 1 {
			t.Fatalf("rule release = %+v", result.RuleRelease)
		}
		if result.EffectiveDate != mustDate(t, 2026, time.March, 1) {
			t.Fatalf("effective date = %s", result.EffectiveDate)
		}
		if !reflect.DeepEqual(result.Factors, classificationFactors()) {
			t.Fatalf("factors snapshot = %+v", result.Factors)
		}
		if len(result.Reasons) == 0 {
			t.Fatal("eligible result carries no reasons")
		}
		if err := result.VerifyWithKey(signer.PublicKey()); err != nil {
			t.Fatalf("VerifyWithKey: %v", err)
		}
	})

	t.Run("GREEN/salary below threshold is ineligible", func(t *testing.T) {
		f := classificationFactors()
		f.AnnualSalary = mustMoneyValue("30000")
		result := classify(classificationInput(t, f))
		if result.Outcome != ClassificationIneligible {
			t.Fatalf("outcome = %s, want INELIGIBLE", result.Outcome)
		}
	})

	t.Run("GREEN/failed duties test is ineligible", func(t *testing.T) {
		f := classificationFactors()
		f.DutiesTestSatisfied = boolPtr(false)
		result := classify(classificationInput(t, f))
		if result.Outcome != ClassificationIneligible {
			t.Fatalf("outcome = %s, want INELIGIBLE", result.Outcome)
		}
	})

	t.Run("GREEN/unauthorized work is ineligible", func(t *testing.T) {
		f := classificationFactors()
		f.WorkAuthorized = boolPtr(false)
		input := classificationInput(t, f)
		input.Proposed = ClassificationNonexempt
		result := classify(input)
		if result.Outcome != ClassificationIneligible {
			t.Fatalf("outcome = %s, want INELIGIBLE", result.Outcome)
		}
	})

	t.Run("GREEN/contractor independence decides contractor status", func(t *testing.T) {
		f := classificationFactors()
		f.ContractorIndependenceMet = boolPtr(true)
		input := classificationInput(t, f)
		input.Proposed = ClassificationContractor
		input.WorkAuthorizationRequired = false
		if got := classify(input).Outcome; got != ClassificationEligible {
			t.Fatalf("independent contractor outcome = %s, want ELIGIBLE", got)
		}
		f.ContractorIndependenceMet = boolPtr(false)
		input.Factors = f
		if got := classify(input).Outcome; got != ClassificationIneligible {
			t.Fatalf("dependent contractor outcome = %s, want INELIGIBLE", got)
		}
	})

	t.Run("GREEN/reviewer override carries authority", func(t *testing.T) {
		f := classificationFactors()
		f.DutiesTestSatisfied = nil
		input := classificationInput(t, f)
		input.Override = &ClassificationOverride{
			Reviewer:  "customer-counsel-1",
			Authority: "customer-counsel",
			Decision:  ClassificationIneligible,
			Rationale: "duties evidence pending; treat as nonexempt until reviewed",
		}
		result := classify(input)
		if result.Outcome != ClassificationIneligible {
			t.Fatalf("outcome = %s, want INELIGIBLE", result.Outcome)
		}
		if result.Reviewer != "customer-counsel-1" || result.OverrideAuthority != "customer-counsel" {
			t.Fatalf("reviewer evidence = %+v", result)
		}
	})

	t.Run("GREEN/invalid override is refused", func(t *testing.T) {
		input := classificationInput(t, classificationFactors())
		input.Override = &ClassificationOverride{Reviewer: "customer-counsel-1"}
		input.KnownAt = known
		if _, err := ClassifyWorker(ctx, input, registry, signer, now); err == nil {
			t.Fatal("partial override was accepted")
		}
	})

	t.Run("GREEN/no classification rule pinned routes to review", func(t *testing.T) {
		bare := NewRegistry()
		pack := classificationPack(t)
		pack.PackID = "us-tx-no-classification"
		pack.Classifications = nil
		if err := bare.Register(pack); err != nil {
			t.Fatal(err)
		}
		bareCtx, err := Resolve(validInput(t, testTXJurisdiction()), bare, signer, mustInstant(t, 1_770_100_000))
		if err != nil {
			t.Fatal(err)
		}
		input := classificationInput(t, classificationFactors())
		input.KnownAt = known
		result, err := ClassifyWorker(bareCtx, input, bare, signer, now)
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != ClassificationReviewRequired {
			t.Fatalf("outcome = %s, want REVIEW_REQUIRED", result.Outcome)
		}
	})

	t.Run("GREEN/uncertainty causes zero registry mutation", func(t *testing.T) {
		before, err := registry.GetExact(classificationPack(t).Release())
		if err != nil {
			t.Fatal(err)
		}
		f := classificationFactors()
		f.DutiesTestSatisfied = nil
		_ = classify(classificationInput(t, f))
		_ = classify(classificationInput(t, classificationFactors()))
		after, err := registry.GetExact(classificationPack(t).Release())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("classification mutated registry state")
		}
	})
}

func TestTodo_LEGAL_005_Race(t *testing.T) {
	ctx, registry, signer := classificationFixture(t)
	now := mustInstant(t, 1_770_100_000)
	input := classificationInput(t, classificationFactors())
	input.KnownAt = mustKnownAt(t, 1_770_000_000)
	results := make(chan string, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			r, err := ClassifyWorker(ctx, input, registry, signer, now)
			if err != nil {
				results <- err.Error()
				return
			}
			results <- string(r.CanonicalBytes())
		}()
	}
	want := <-results
	for i := 1; i < cap(results); i++ {
		if got := <-results; got != want {
			t.Fatal("concurrent classifications differ")
		}
	}
}

func TestTodo_LEGAL_005_Security(t *testing.T) {
	ctx, registry, signer := classificationFixture(t)
	now := mustInstant(t, 1_770_100_000)
	input := classificationInput(t, classificationFactors())
	input.KnownAt = mustKnownAt(t, 1_770_000_000)
	result, err := ClassifyWorker(ctx, input, registry, signer, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := result.VerifyWithKey(fixedSigner(t, 0x51).PublicKey()); err == nil {
		t.Fatal("wrong authority key verified")
	}
	tampered := result
	tampered.Outcome = ClassificationIneligible
	if err := tampered.Verify(); err == nil {
		t.Fatal("tampered classification verified")
	}
	if _, err := ClassifyWorker(nil, input, registry, signer, now); err == nil {
		t.Fatal("nil context classified")
	}
	tamperedCtx := *ctx
	if _, err := ClassifyWorker(&tamperedCtx, input, registry, signer, now); err != nil {
		t.Fatalf("value-copied context should still verify: %v", err)
	}
}

func TestTodo_LEGAL_005_Mutation(t *testing.T) {
	ctx, registry, signer := classificationFixture(t)
	now := mustInstant(t, 1_770_100_000)
	known := mustKnownAt(t, 1_770_000_000)
	base := classificationInput(t, classificationFactors())
	base.KnownAt = known
	eligible, err := ClassifyWorker(ctx, base, registry, signer, now)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*ClassificationResult)
	}{
		{"outcome", func(r *ClassificationResult) { r.Outcome = "" }},
		{"release", func(r *ClassificationResult) { r.RuleRelease = RulePackRelease{} }},
		{"effective date", func(r *ClassificationResult) { r.EffectiveDate = values.LocalDate{} }},
		{"observed as of", func(r *ClassificationResult) { r.ObservedAsOf = values.LocalDate{} }},
		{"reasons", func(r *ClassificationResult) { r.Reasons = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := eligible
			tc.mutate(&mutated)
			mutated.Digest, mutated.Signature = signer.SignDigest(mutated.CanonicalBytes())
			if err := mutated.Verify(); err == nil {
				t.Fatalf("signed incomplete classification verified (%s)", tc.name)
			}
		})
	}
	t.Run("guard/threshold flip is killed", func(t *testing.T) {
		f := classificationFactors()
		f.AnnualSalary = mustMoneyValue("1")
		input := classificationInput(t, f)
		input.KnownAt = known
		result, err := ClassifyWorker(ctx, input, registry, signer, now)
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome == ClassificationEligible {
			t.Fatal("$1 salary classified ELIGIBLE for exemption")
		}
	})
	t.Run("guard/unknown defaults are killed", func(t *testing.T) {
		input := classificationInput(t, ClassificationFactors{})
		input.ObservedAsOf = mustDate(t, 2026, time.February, 15)
		input.KnownAt = known
		result, err := ClassifyWorker(ctx, input, registry, signer, now)
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != ClassificationReviewRequired {
			t.Fatalf("empty factors outcome = %s, want REVIEW_REQUIRED", result.Outcome)
		}
	})
}
