package rules

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_RULE_003 proves the compiled Promotion approval-threshold rule
// against the reference workflow's four factors: raise size, band position,
// budget authority and grade change. Every case cites the exact table
// version it ran against.
func TestTodo_RULE_003(t *testing.T) {
	table := PromotionApprovalThresholdTable()
	if err := table.Validate(); err != nil {
		t.Fatalf("PromotionApprovalThresholdTable is invalid: %v", err)
	}
	wantDigest, err := table.Digest()
	if err != nil {
		t.Fatalf("table digest: %v", err)
	}

	cases := []struct {
		name     string
		in       PromotionApprovalInput
		wantRow  string
		wantTier ApprovalTier
	}{
		{
			name:     "small in-band raise with sufficient budget needs only the baseline reviewers",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "5.0000", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthoritySufficient},
			wantRow:  "otherwise-standard",
			wantTier: ApprovalTierStandard,
		},
		{
			name:     "exactly at the finance threshold does not escalate",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "10.0000", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthoritySufficient},
			wantRow:  "otherwise-standard",
			wantTier: ApprovalTierStandard,
		},
		{
			name:     "one unit over the finance threshold escalates",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "10.0001", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthoritySufficient},
			wantRow:  "increase-exceeds-finance-threshold",
			wantTier: ApprovalTierFinanceRequired,
		},
		{
			name:     "a raise over the executive threshold escalates further",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "25.0000", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthoritySufficient},
			wantRow:  "increase-exceeds-executive-threshold",
			wantTier: ApprovalTierExecutiveRequired,
		},
		{
			name:     "landing above the band maximum needs finance even under threshold",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "2.0000", 4), BandPosition: BandPositionAboveMaximum, BudgetAuthority: BudgetAuthoritySufficient},
			wantRow:  "resulting-pay-above-band-maximum",
			wantTier: ApprovalTierFinanceRequired,
		},
		{
			name:     "insufficient budget authority needs finance regardless of raise size",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "2.0000", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthorityInsufficient},
			wantRow:  "insufficient-budget-authority",
			wantTier: ApprovalTierFinanceRequired,
		},
		{
			name:     "a grade change needs finance even for a small raise",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "2.0000", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthoritySufficient, GradeChange: true},
			wantRow:  "grade-change-standard",
			wantTier: ApprovalTierFinanceRequired,
		},
		{
			name:     "a grade change landing above the band maximum needs executive review",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "2.0000", 4), BandPosition: BandPositionAboveMaximum, BudgetAuthority: BudgetAuthoritySufficient, GradeChange: true},
			wantRow:  "grade-change-above-band-maximum",
			wantTier: ApprovalTierExecutiveRequired,
		},
		{
			name:     "an unresolved band position blocks rather than defaulting to standard",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "2.0000", 4), BandPosition: BandPositionUnknown, BudgetAuthority: BudgetAuthoritySufficient},
			wantRow:  "unknown-band-position",
			wantTier: ApprovalTierUnknownBlocked,
		},
		{
			name:     "an unresolved budget authority blocks rather than defaulting to standard",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "2.0000", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthorityUnknown},
			wantRow:  "unknown-budget-authority",
			wantTier: ApprovalTierUnknownBlocked,
		},
		{
			name:     "below minimum in band with a large raise still escalates on raise size",
			in:       PromotionApprovalInput{IncreasePercent: dec(t, "30.0000", 4), BandPosition: BandPositionBelowMinimum, BudgetAuthority: BudgetAuthoritySufficient},
			wantRow:  "increase-exceeds-executive-threshold",
			wantTier: ApprovalTierExecutiveRequired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := EvaluatePromotionApproval(table, tc.in)
			if err != nil {
				t.Fatalf("EvaluatePromotionApproval: %v", err)
			}
			if decision.Tier != tc.wantTier {
				t.Errorf("tier = %s, want %s", decision.Tier, tc.wantTier)
			}
			if decision.MatchedRowID != tc.wantRow {
				t.Errorf("matched row = %s, want %s", decision.MatchedRowID, tc.wantRow)
			}
			if decision.TableID != PromotionApprovalTableID || decision.TableVersion != PromotionApprovalTableVersion {
				t.Errorf("decision cites %s@%s, want %s@%s", decision.TableID, decision.TableVersion, PromotionApprovalTableID, PromotionApprovalTableVersion)
			}
			if decision.TableDigest != wantDigest {
				t.Errorf("decision digest = %s, want %s", decision.TableDigest, wantDigest)
			}
			if len(decision.Trace) == 0 {
				t.Error("decision carries no explanation trace")
			}
		})
	}

	t.Run("a malformed input is refused before it reaches the table", func(t *testing.T) {
		_, err := EvaluatePromotionApproval(table, PromotionApprovalInput{
			IncreasePercent: values.Decimal{},
			BandPosition:    BandPositionInBand,
			BudgetAuthority: BudgetAuthoritySufficient,
		})
		if !errors.Is(err, ErrPromotionInputInvalid) {
			t.Fatalf("error = %v, want ErrPromotionInputInvalid", err)
		}
	})

	t.Run("an unset band position or budget authority is a caller defect, not UNKNOWN", func(t *testing.T) {
		_, err := EvaluatePromotionApproval(table, PromotionApprovalInput{
			IncreasePercent: dec(t, "5.0000", 4),
			BudgetAuthority: BudgetAuthoritySufficient,
		})
		if !errors.Is(err, ErrPromotionInputInvalid) {
			t.Fatalf("error = %v, want ErrPromotionInputInvalid for an unset band position", err)
		}
	})
}

// TestTodo_RULE_003_Property asserts invariants that must hold across the
// whole input space, not just the pinned cases above: escalation never
// relaxes as the raise grows, and an unresolved input always blocks
// regardless of what the other three factors say.
func TestTodo_RULE_003_Property(t *testing.T) {
	table := PromotionApprovalThresholdTable()
	severity := map[ApprovalTier]int{
		ApprovalTierStandard:          0,
		ApprovalTierFinanceRequired:   1,
		ApprovalTierExecutiveRequired: 2,
	}

	t.Run("increasing the raise percent never relaxes the required tier", func(t *testing.T) {
		percents := []string{"0.0000", "5.0000", "9.9999", "10.0000", "10.0001", "15.0000", "19.9999", "20.0000", "20.0001", "35.0000"}
		last := -1
		for _, p := range percents {
			decision, err := EvaluatePromotionApproval(table, PromotionApprovalInput{
				IncreasePercent: dec(t, p, 4),
				BandPosition:    BandPositionInBand,
				BudgetAuthority: BudgetAuthoritySufficient,
			})
			if err != nil {
				t.Fatalf("EvaluatePromotionApproval(%s): %v", p, err)
			}
			level, ok := severity[decision.Tier]
			if !ok {
				t.Fatalf("EvaluatePromotionApproval(%s) tier = %s is not an escalation tier", p, decision.Tier)
			}
			if level < last {
				t.Fatalf("raise %s%% produced tier %s, which is less severe than a smaller raise's tier", p, decision.Tier)
			}
			last = level
		}
	})

	t.Run("an unresolved budget authority blocks no matter what the raise or band say", func(t *testing.T) {
		percents := []string{"0.0000", "5.0000", "50.0000"}
		positions := []BandPosition{BandPositionBelowMinimum, BandPositionInBand, BandPositionAboveMaximum}
		for _, p := range percents {
			for _, pos := range positions {
				for _, grade := range []bool{false, true} {
					decision, err := EvaluatePromotionApproval(table, PromotionApprovalInput{
						IncreasePercent: dec(t, p, 4),
						BandPosition:    pos,
						BudgetAuthority: BudgetAuthorityUnknown,
						GradeChange:     grade,
					})
					if err != nil {
						t.Fatalf("EvaluatePromotionApproval: %v", err)
					}
					if pos == BandPositionUnknown {
						continue
					}
					if decision.Tier != ApprovalTierUnknownBlocked {
						t.Fatalf("pct=%s pos=%s grade=%v: tier = %s, want UNKNOWN_BLOCKED", p, pos, grade, decision.Tier)
					}
				}
			}
		}
	})

	t.Run("insufficient budget never resolves to STANDARD", func(t *testing.T) {
		for _, p := range []string{"0.0000", "1.0000", "9.0000"} {
			decision, err := EvaluatePromotionApproval(table, PromotionApprovalInput{
				IncreasePercent: dec(t, p, 4),
				BandPosition:    BandPositionInBand,
				BudgetAuthority: BudgetAuthorityInsufficient,
			})
			if err != nil {
				t.Fatalf("EvaluatePromotionApproval: %v", err)
			}
			if decision.Tier == ApprovalTierStandard {
				t.Fatalf("pct=%s with insufficient budget resolved to STANDARD", p)
			}
		}
	})
}

// TestTodo_RULE_003_Golden pins the compiled table's version digest and a set
// of representative evaluation vectors against
// testdata/rule_003_golden.json. A change to the thresholds, the row order,
// or the engine's wire format fails this test even when the specific
// assertions above still happen to pass.
func TestTodo_RULE_003_Golden(t *testing.T) {
	raw, err := os.ReadFile("testdata/rule_003_golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		TableID      string `json:"table_id"`
		TableVersion string `json:"table_version"`
		TableDigest  string `json:"table_digest"`
		Cases        []struct {
			IncreasePercent string `json:"increase_percent"`
			BandPosition    string `json:"band_position"`
			BudgetAuthority string `json:"budget_authority"`
			GradeChange     bool   `json:"grade_change"`
			Tier            string `json:"tier"`
			Row             string `json:"row"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}

	table := PromotionApprovalThresholdTable()
	if table.ID != golden.TableID || table.Version != golden.TableVersion {
		t.Fatalf("table identity = %s@%s, want %s@%s", table.ID, table.Version, golden.TableID, golden.TableVersion)
	}
	digest, err := table.Digest()
	if err != nil {
		t.Fatalf("table digest: %v", err)
	}
	if digest != golden.TableDigest {
		t.Errorf("table digest = %s, want %s (a threshold or row-order change must publish a new version)", digest, golden.TableDigest)
	}

	for _, tc := range golden.Cases {
		decision, err := EvaluatePromotionApproval(table, PromotionApprovalInput{
			IncreasePercent: dec(t, tc.IncreasePercent, 4),
			BandPosition:    BandPosition(tc.BandPosition),
			BudgetAuthority: BudgetAuthority(tc.BudgetAuthority),
			GradeChange:     tc.GradeChange,
		})
		if err != nil {
			t.Errorf("EvaluatePromotionApproval(%+v): %v", tc, err)
			continue
		}
		if string(decision.Tier) != tc.Tier {
			t.Errorf("case %+v: tier = %s, want %s", tc, decision.Tier, tc.Tier)
		}
		if decision.MatchedRowID != tc.Row {
			t.Errorf("case %+v: row = %s, want %s", tc, decision.MatchedRowID, tc.Row)
		}
	}
}

// TestTodo_RULE_003_Mutation proves the threshold comparisons are exact
// decimal comparisons, never rounded, and that changing a threshold produces
// a genuinely new table version rather than an in-place edit: the two
// versions carry different digests and can disagree on the same input.
func TestTodo_RULE_003_Mutation(t *testing.T) {
	table := PromotionApprovalThresholdTable()

	t.Run("the finance boundary is exact to the smallest declared unit", func(t *testing.T) {
		below, err := EvaluatePromotionApproval(table, PromotionApprovalInput{
			IncreasePercent: dec(t, "10.0000", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthoritySufficient,
		})
		if err != nil {
			t.Fatalf("EvaluatePromotionApproval: %v", err)
		}
		if below.Tier != ApprovalTierStandard {
			t.Fatalf("exactly at threshold: tier = %s, want STANDARD", below.Tier)
		}
		above, err := EvaluatePromotionApproval(table, PromotionApprovalInput{
			IncreasePercent: dec(t, "10.0001", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthoritySufficient,
		})
		if err != nil {
			t.Fatalf("EvaluatePromotionApproval: %v", err)
		}
		if above.Tier != ApprovalTierFinanceRequired {
			t.Fatalf("one unit above threshold: tier = %s, want FINANCE_REQUIRED", above.Tier)
		}
	})

	t.Run("a different threshold configuration is a new version with a new digest", func(t *testing.T) {
		v1 := PromotionApprovalThresholdTable()
		v1Digest, err := v1.Digest()
		if err != nil {
			t.Fatalf("v1 digest: %v", err)
		}

		v2 := PromotionApprovalThresholdTable()
		v2.Version = "2026.2"
		// Tighten the finance threshold from 10% to 5%: a customer parameter
		// change, expressed as editing the compiled row rather than a live
		// config value, exactly as RULE-003's REFACTOR contract requires -
		// this produces a new version, it does not rewrite 2026.1's rows.
		for i, row := range v2.Rows {
			if row.ID == "increase-exceeds-finance-threshold" {
				v2.Rows[i].Conditions[0] = GreaterThan(DecimalValue(dec(t, "5.0000", 4)))
			}
		}
		if err := v2.Validate(); err != nil {
			t.Fatalf("v2 table: %v", err)
		}
		v2Digest, err := v2.Digest()
		if err != nil {
			t.Fatalf("v2 digest: %v", err)
		}
		if v1Digest == v2Digest {
			t.Fatal("changing a threshold did not change the table's digest")
		}

		in := PromotionApprovalInput{IncreasePercent: dec(t, "7.0000", 4), BandPosition: BandPositionInBand, BudgetAuthority: BudgetAuthoritySufficient}
		d1, err := EvaluatePromotionApproval(v1, in)
		if err != nil {
			t.Fatalf("v1 evaluate: %v", err)
		}
		d2, err := EvaluatePromotionApproval(v2, in)
		if err != nil {
			t.Fatalf("v2 evaluate: %v", err)
		}
		if d1.Tier != ApprovalTierStandard {
			t.Fatalf("v1 at 7%%: tier = %s, want STANDARD (10%% threshold not yet crossed)", d1.Tier)
		}
		if d2.Tier != ApprovalTierFinanceRequired {
			t.Fatalf("v2 at 7%%: tier = %s, want FINANCE_REQUIRED (5%% threshold crossed)", d2.Tier)
		}
		if d1.TableVersion == d2.TableVersion {
			t.Fatal("two differently-configured tables must not cite the same version")
		}

		// v1's original rows must be unaffected by building v2 from it.
		freshV1, err := v1.Digest()
		if err != nil {
			t.Fatalf("v1 digest after v2 built: %v", err)
		}
		if freshV1 != v1Digest {
			t.Fatal("building v2 mutated v1's own digest")
		}
	})

	t.Run("PromotionApprovalThresholdTable never shares backing storage across calls", func(t *testing.T) {
		a := PromotionApprovalThresholdTable()
		b := PromotionApprovalThresholdTable()
		a.Rows[0].Conditions[0] = GreaterThan(DecimalValue(dec(t, "0.0000", 4)))
		bDigest, err := b.Digest()
		if err != nil {
			t.Fatalf("b digest: %v", err)
		}
		freshDigest, err := PromotionApprovalThresholdTable().Digest()
		if err != nil {
			t.Fatalf("fresh digest: %v", err)
		}
		if bDigest != freshDigest {
			t.Fatal("mutating one call's table changed another call's independently-built table")
		}
	})
}

// FuzzTodo_RULE_003 drives the promotion approval decision with arbitrary
// factor combinations. It asserts the engine never panics, always returns
// one of the four declared tiers, and is deterministic for a fixed input.
func TestTodo_WF_EXT_006_PublishedPromotionRouteTable(t *testing.T) {
	legacy := PromotionApprovalThresholdTable()
	current := PromotionWorkflowThresholdTable()
	legacyDigest, err := legacy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	currentDigest, err := current.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Version != PromotionApprovalTableVersion || current.Version != PromotionWorkflowThresholdTableVersion || legacyDigest == currentDigest {
		t.Fatalf("legacy/current versions or digests are not independently pinned: %s %s %s %s", legacy.Version, current.Version, legacyDigest, currentDigest)
	}
	if len(current.Outputs) != 2 || current.Outputs[0].Name != "approval_tier" || current.Outputs[1].Name != "route_key" {
		t.Fatalf("workflow outputs = %+v", current.Outputs)
	}
	for _, row := range current.Rows {
		if len(row.Outputs) != 2 {
			t.Fatalf("row %s outputs = %+v", row.ID, row.Outputs)
		}
		tier, route := ApprovalTier(row.Outputs[0].String()), row.Outputs[1].String()
		want := "EXCEEDS_THRESHOLD"
		if tier == ApprovalTierStandard {
			want = "WITHIN_THRESHOLD"
		}
		if tier == ApprovalTierUnknownBlocked {
			want = "UNKNOWN"
		}
		if route != want {
			t.Errorf("row %s route = %q, want %q for tier %q", row.ID, route, want, tier)
		}
	}
}

func FuzzTodo_RULE_003(f *testing.F) {
	f.Add(int64(500), int32(4), 0, 0, false)
	f.Add(int64(100000), int32(4), 2, 1, true)
	f.Add(int64(0), int32(2), 3, 2, false)
	f.Add(int64(-500), int32(4), 1, 3, true)

	bandPositions := []BandPosition{BandPositionBelowMinimum, BandPositionInBand, BandPositionAboveMaximum, BandPositionUnknown}
	budgetAuthorities := []BudgetAuthority{BudgetAuthoritySufficient, BudgetAuthorityInsufficient, BudgetAuthorityUnknown}

	f.Fuzz(func(t *testing.T, unscaled int64, scale int32, bandIdx, budgetIdx int, gradeChange bool) {
		if scale < 0 || scale > values.MaxScale {
			t.Skip("out of range for this fixture")
		}
		text := formatCents(unscaled)
		pct, err := values.NewDecimal(text, 4, values.RoundingHalfEven)
		if err != nil {
			t.Skip("not a value this fixture needs to construct")
		}
		table := PromotionApprovalThresholdTable()
		in := PromotionApprovalInput{
			IncreasePercent: pct,
			BandPosition:    bandPositions[((bandIdx%len(bandPositions))+len(bandPositions))%len(bandPositions)],
			BudgetAuthority: budgetAuthorities[((budgetIdx%len(budgetAuthorities))+len(budgetAuthorities))%len(budgetAuthorities)],
			GradeChange:     gradeChange,
		}

		decision, err := EvaluatePromotionApproval(table, in)
		if err != nil {
			t.Fatalf("EvaluatePromotionApproval(%+v) failed on a valid input: %v", in, err)
		}
		switch decision.Tier {
		case ApprovalTierStandard, ApprovalTierFinanceRequired, ApprovalTierExecutiveRequired, ApprovalTierUnknownBlocked:
		default:
			t.Fatalf("tier = %q is not a declared approval tier", decision.Tier)
		}
		if (in.BandPosition == BandPositionUnknown || in.BudgetAuthority == BudgetAuthorityUnknown) && decision.Tier != ApprovalTierUnknownBlocked {
			t.Fatalf("an unresolved input resolved to %s instead of UNKNOWN_BLOCKED", decision.Tier)
		}

		again, err := EvaluatePromotionApproval(table, in)
		if err != nil {
			t.Fatalf("repeat evaluation failed: %v", err)
		}
		if again.Tier != decision.Tier || again.MatchedRowID != decision.MatchedRowID || again.TableDigest != decision.TableDigest {
			t.Fatalf("two evaluations of the same input disagreed: %+v vs %+v", decision, again)
		}
	})
}
