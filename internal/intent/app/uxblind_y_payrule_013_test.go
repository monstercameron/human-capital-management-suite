package app

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_UXBLIND_013_Property walks every published promotion edge in both
// demo companies. The guardrail's displayed entry bounds and permitted raise
// must be the bounds of the same published percentage rule that the form
// receives through WorkforceOptions.
func TestTodo_UXBLIND_013_Property(t *testing.T) {
	for _, pack := range demoworkforce.Packs() {
		paths, err := publishedPromotionPathsFrom(pack.PromotionPaths())
		if err != nil {
			t.Fatalf("%s published paths: %v", pack.Key, err)
		}
		for _, path := range paths {
			t.Run(pack.Key+"/"+path.Option.PathRef, func(t *testing.T) {
				rule, err := uxblindYPayRuleFromOption(path.Option)
				if err != nil {
					t.Fatalf("pay rule: %v", err)
				}
				basis, err := uxblindYPayBasis(pack.PayBasisFor(path.Option.SourceJobCode))
				if err != nil {
					t.Fatalf("source basis: %v", err)
				}
				amount := "100000.00"
				if basis == rewards.PayBasisHourly {
					amount = "40.00"
				}
				current, err := values.NewMoney(amount, "USD", 2, values.RoundingExactRequired)
				if err != nil {
					t.Fatal(err)
				}
				guardrail := applyUXBlindYPayRule(
					promotion.CompensationGuardrail{Status: promotion.GuardrailStatusAvailable},
					rule,
					rewards.CompensationSnapshot{Base: values.Value(current), PayBasis: basis},
				)
				comparable, err := uxblindYCurrentInTargetBasis(current, basis, rule)
				if err != nil {
					t.Fatalf("target basis: %v", err)
				}
				minimum, maximum, err := promotion.BasePayBounds(comparable, rule.minimumIncrease, rule.maximumIncrease)
				if err != nil {
					t.Fatalf("expected bounds: %v", err)
				}
				if guardrail.MinimumAnnualized.Amount().String() != minimum.Amount().String() || guardrail.MaximumAnnualized.Amount().String() != maximum.Amount().String() {
					t.Fatalf("%s %s bounds = %s..%s, want %s..%s", pack.Key, path.Option.PathRef,
						guardrail.MinimumAnnualized.Amount(), guardrail.MaximumAnnualized.Amount(), minimum.Amount(), maximum.Amount())
				}
				if guardrail.PermittedIncreasePercent.Fraction().Cmp(rule.maximumIncrease.Fraction()) != 0 {
					t.Fatalf("%s %s permitted increase = %s, want published maximum %s", pack.Key, path.Option.PathRef,
						guardrail.PermittedIncreasePercent.Fraction(), rule.maximumIncrease.Fraction())
				}
			})
		}
	}
}
