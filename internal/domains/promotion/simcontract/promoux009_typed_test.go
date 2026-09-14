package simcontract_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
)

func TestTodo_PROMOUX_009_Typed(t *testing.T) {
	in := promotionFixtureInput(t)
	in.Findings = []promotion.Finding{
		{Code: "budget.observation", Severity: promotion.SeverityAdvisory, Field: "budget", Message: "observation from rule A"},
		{Code: "budget.observation", Severity: promotion.SeverityAdvisory, Field: "budget", Message: "observation from rule B"},
	}
	corroborated, err := simcontract.NewFinding("budget.observation", "rewards", promotion.SeverityAdvisory, "budget", "promotion.proposal", "canonical budget observation", "rewards.band")
	if err != nil {
		t.Fatal(err)
	}
	in.CanonicalFindings = []simcontract.Finding{corroborated}

	result, err := simcontract.Assemble(in)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if got := len(result.CanonicalFindings); got != 2 {
		t.Fatalf("canonical finding count = %d, want 2 (one legacy promotion owner and one independently owned corroboration)", got)
	}
	if result.CanonicalFindings[0].Explanation == "" || result.CanonicalFindings[1].Explanation == "" {
		t.Fatal("canonical findings must retain an explanation")
	}
	if len(result.Findings) != 1 || result.Findings[0].Owner != "promotion" || !reflect.DeepEqual(result.Findings[0].CorroboratedBy, []string{"rewards"}) {
		t.Fatalf("compatibility finding = %+v, want one semantic finding with promotion/rewards corroboration", result.Findings)
	}
}

func TestTodo_PROMOUX_009_Typed_Property(t *testing.T) {
	in := promotionFixtureInput(t)
	a, err := simcontract.NewFinding("budget.observation", "promotion", promotion.SeverityAdvisory, "budget", "promotion.proposal", "z explanation", "rule-z")
	if err != nil {
		t.Fatal(err)
	}
	b, err := simcontract.NewFinding("budget.observation", "promotion", promotion.SeverityAdvisory, "budget", "promotion.proposal", "a explanation", "rule-a")
	if err != nil {
		t.Fatal(err)
	}
	in.Findings = nil
	in.CanonicalFindings = []simcontract.Finding{a, b, b, a}
	one, err := simcontract.Assemble(in)
	if err != nil {
		t.Fatal(err)
	}
	in.CanonicalFindings = []simcontract.Finding{b, a}
	two, err := simcontract.Assemble(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one.CanonicalFindings, two.CanonicalFindings) || one.Digest != two.Digest {
		t.Fatal("finding canonicalization is not permutation-stable")
	}
	if got := one.CanonicalFindings[0].Source; got != "rule-a\x00rule-z" {
		t.Fatalf("sources = %q, want deterministic corroboration order", got)
	}
}

func TestTodo_PROMOUX_009_Typed_Golden(t *testing.T) {
	in := promotionFixtureInput(t)
	f, err := simcontract.NewFinding("increase.review", "rewards", promotion.SeverityAdvisory, "proposed.base", "promotion.proposal", "review the annualized increase", "compensation")
	if err != nil {
		t.Fatal(err)
	}
	in.Findings = nil
	in.CanonicalFindings = []simcontract.Finding{f}
	result, err := simcontract.Assemble(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Canonical()) == "" || result.Digest == "" {
		t.Fatal("canonical finding was not included in the contract")
	}
}

func TestTodo_PROMOUX_009_Typed_Integration(t *testing.T) {
	in := promotionFixtureInput(t)
	f, err := simcontract.NewFinding("budget.observation", "rewards", promotion.SeverityAdvisory, "budget", "promotion.proposal", "budget was observed", "rewards")
	if err != nil {
		t.Fatal(err)
	}
	in.Findings = nil
	in.CanonicalFindings = []simcontract.Finding{f}
	result, err := simcontract.Assemble(in)
	if err != nil {
		t.Fatal(err)
	}
	store := simcontract.NewMemoryStore()
	if _, _, err = store.Store(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load(context.Background(), result.Digest)
	if err != nil || !ok {
		t.Fatalf("Load = (%v, %v), want stored artifact", err, ok)
	}
	if !reflect.DeepEqual(result.CanonicalFindings, loaded.CanonicalFindings) || result.Digest != loaded.Digest {
		t.Fatal("typed findings changed across persistence/reload")
	}
}
