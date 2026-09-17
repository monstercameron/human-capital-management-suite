package program

import (
	"strings"
	"testing"
	"time"
)

func bonusFormula(inputs map[string]string) (string, error) {
	return "1250.50", nil
}

func benefitFormula(inputs map[string]string) (string, error) {
	return "400.00", nil
}

func baseOutcomeInput(rev Revision) OutcomeInput {
	return OutcomeInput{
		Participant: "worker-7", ProgramID: rev.ProgramID,
		RevisionDigest: rev.Digest,
		PopulationRef:  "pop-2026-q3", EligibilityRef: "elig-2026-q3",
		CycleRef: "cycle-2026-q3", FundingRef: "fund-employer-fy26",
		FormulaID: "bonus-pct-v1", FormulaVersion: "v1",
		Inputs: map[string]string{"base": "25001.00", "rate": "0.05"},
		At:     time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
	}
}

func TestTodo_PROGRAM_005(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	c.RegisterFormula("bonus-pct-v1", bonusFormula)
	res, err := c.Calculate(baseOutcomeInput(rev))
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if res.Value != "1250.5" {
		t.Fatalf("outcome value = %q, want canonical %q", res.Value, "1250.5")
	}
	if res.Status != OutcomeAchieved {
		t.Fatalf("outcome status = %q", res.Status)
	}
	for _, want := range []string{"worker-7", "pop-2026-q3", "bonus-pct-v1", "fund-employer-fy26"} {
		if !strings.Contains(res.Explanation, want) {
			t.Fatalf("explanation omits %q: %q", want, res.Explanation)
		}
	}
	if res.Digest == "" {
		t.Fatal("outcome carries no digest")
	}
}

func TestTodo_PROGRAM_005_Property(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	c.RegisterFormula("bonus-pct-v1", bonusFormula)
	c.RegisterFormula("benefit-flat-v1", benefitFormula)
	// The core carries no formula: two registered formulas through the
	// same core path yield their own values.
	in := baseOutcomeInput(rev)
	first, err := c.Calculate(in)
	if err != nil {
		t.Fatalf("bonus: %v", err)
	}
	in.FormulaID = "benefit-flat-v1"
	second, err := c.Calculate(in)
	if err != nil {
		t.Fatalf("benefit: %v", err)
	}
	if first.Value == second.Value {
		t.Fatal("core collapsed two formulas to one value")
	}
	if second.Value != "400" {
		t.Fatalf("benefit value = %q, want %q", second.Value, "400")
	}
	// Unknown inputs force UNKNOWN status with named unknowns.
	in.UnknownInputs = []string{"ytd-earnings"}
	third, err := c.Calculate(in)
	if err != nil {
		t.Fatalf("unknowns: %v", err)
	}
	if third.Status != OutcomeUnknown {
		t.Fatalf("status with unknowns = %q, want UNKNOWN", third.Status)
	}
	if len(third.Unknowns) != 1 || third.Unknowns[0] != "ytd-earnings" {
		t.Fatalf("unknowns = %v", third.Unknowns)
	}
}

func TestTodo_PROGRAM_005_Golden(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	c.RegisterFormula("bonus-pct-v1", bonusFormula)
	res, err := c.Calculate(baseOutcomeInput(rev))
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if res.Digest != readGolden(t, "program_005.golden") {
		t.Fatalf("outcome digest mismatch:\n got %q", res.Digest)
	}
}

func FuzzTodo_PROGRAM_005(f *testing.F) {
	f.Add([]byte("25001.00"), []byte("0.05"))
	f.Fuzz(func(t *testing.T, base, rate []byte) {
		// Must never panic; unparseable decimals must never canonicalize.
		if _, err := CanonicalDecimal(string(base)); err == nil && !isDecimalText(string(base)) {
			t.Fatalf("non-decimal %q canonicalized", base)
		}
		_ = rate
	})
}

func TestTodo_PROGRAM_005_Security(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	// An unregistered formula id is denied without revealing the registry.
	_, err := c.Calculate(baseOutcomeInput(rev))
	if err == nil {
		t.Fatal("unregistered formula calculated")
	}
	if strings.Contains(err.Error(), "bonus-pct") {
		t.Fatalf("denial leaks registry contents: %v", err)
	}
}

func TestTodo_PROGRAM_005_Mutation(t *testing.T) {
	c := testCatalog(t)
	def := mustDefine(t, c)
	rev := mustRevision(t, c, def.ID)
	c.RegisterFormula("bonus-pct-v1", bonusFormula)
	// Mutant A: mismatched funding ref must be killed (empty funding).
	in := baseOutcomeInput(rev)
	in.FundingRef = ""
	if _, err := c.Calculate(in); err == nil {
		t.Fatal("missing-funding mutant survived")
	}
	// Mutant B: formula returning a non-decimal must be killed.
	c.RegisterFormula("evil-v1", func(map[string]string) (string, error) { return "lots", nil })
	in = baseOutcomeInput(rev)
	in.FormulaID = "evil-v1"
	if _, err := c.Calculate(in); err == nil {
		t.Fatal("non-decimal mutant survived")
	}
	// Mutant C: mismatched revision digest must be killed.
	in = baseOutcomeInput(rev)
	in.RevisionDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := c.Calculate(in); err == nil {
		t.Fatal("revision-swap mutant survived")
	}
}
