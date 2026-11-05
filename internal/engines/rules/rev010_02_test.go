package rules

import (
	"fmt"
	"math/rand"
	"testing"
)

// RED for REV-010-02: the Promotion approval-threshold table must resolve
// per tenant from published tenant parameters instead of serving one fixed
// 10%/20% table to every tenant. Two tenants with different published
// thresholds evaluating the identical increase receive different tiers, each
// decision citing its own resolved table version; no published override
// falls back to the reference table, and a malformed override fails closed.
func rev01002Input(t *testing.T, percent string) PromotionApprovalInput {
	t.Helper()
	return PromotionApprovalInput{
		IncreasePercent: dec(t, percent, IncreasePercentScale),
		BandPosition:    BandPositionInBand,
		BudgetAuthority: BudgetAuthoritySufficient,
		GradeChange:     false,
	}
}

func TestTodo_REV_010_02(t *testing.T) {
	resA := TenantThresholdResolution{FinanceText: "5.0000", ExecutiveText: "15.0000", Revision: 7, DefinitionVersion: "tenant-params/v7", Found: true}
	resB := TenantThresholdResolution{FinanceText: "25.0000", ExecutiveText: "40.0000", Revision: 9, DefinitionVersion: "tenant-params/v9", Found: true}

	tableA, err := PromotionApprovalThresholdTableForTenant(TenantThresholdScope{Tenant: "tenant-a"}, resA)
	if err != nil {
		t.Fatal(err)
	}
	tableB, err := PromotionApprovalThresholdTableForTenant(TenantThresholdScope{Tenant: "tenant-b"}, resB)
	if err != nil {
		t.Fatal(err)
	}

	same := rev01002Input(t, "18.0000")
	decisionA, err := EvaluatePromotionApproval(tableA, same)
	if err != nil {
		t.Fatal(err)
	}
	decisionB, err := EvaluatePromotionApproval(tableB, same)
	if err != nil {
		t.Fatal(err)
	}
	if decisionA.Tier != ApprovalTierExecutiveRequired {
		t.Fatalf("tenant-a 18%% raise tier = %s, want EXECUTIVE", decisionA.Tier)
	}
	if decisionB.Tier != ApprovalTierStandard {
		t.Fatalf("tenant-b 18%% raise tier = %s, want STANDARD", decisionB.Tier)
	}
	if decisionA.TableVersion != "tenant-params/v7" || decisionB.TableVersion != "tenant-params/v9" {
		t.Fatalf("cited versions = %q/%q, want the resolved definition versions", decisionA.TableVersion, decisionB.TableVersion)
	}
	if decisionA.TableDigest == "" || decisionA.TableDigest == decisionB.TableDigest {
		t.Fatal("tenant tables must digest distinctly")
	}

	t.Run("no published override falls back to the reference table", func(t *testing.T) {
		fallback, err := PromotionApprovalThresholdTableForTenant(TenantThresholdScope{Tenant: "tenant-z"}, TenantThresholdResolution{})
		if err != nil {
			t.Fatal(err)
		}
		want, err := PromotionApprovalThresholdTable().Digest()
		if err != nil {
			t.Fatal(err)
		}
		got, err := fallback.Digest()
		if err != nil {
			t.Fatal(err)
		}
		if got != want || fallback.Version != PromotionApprovalTableVersion {
			t.Fatal("unresolved tenant did not receive the byte-identical reference table")
		}
	})

	t.Run("malformed overrides fail closed, never silently reference", func(t *testing.T) {
		scope := TenantThresholdScope{Tenant: "tenant-a"}
		for _, tc := range []struct {
			name string
			res  TenantThresholdResolution
		}{
			{name: "finance above executive", res: TenantThresholdResolution{FinanceText: "30.0000", ExecutiveText: "20.0000", Revision: 3, Found: true}},
			{name: "equal thresholds", res: TenantThresholdResolution{FinanceText: "20.0000", ExecutiveText: "20.0000", Revision: 3, Found: true}},
			{name: "unparseable finance", res: TenantThresholdResolution{FinanceText: "ten", ExecutiveText: "20.0000", Revision: 3, Found: true}},
			{name: "negative executive", res: TenantThresholdResolution{FinanceText: "5.0000", ExecutiveText: "-1.0000", Revision: 3, Found: true}},
			{name: "no definition version", res: TenantThresholdResolution{FinanceText: "5.0000", ExecutiveText: "20.0000", Revision: 3, Found: true}},
			{name: "empty tenant scope", res: resA},
		} {
			res, sc := tc.res, scope
			if tc.name == "empty tenant scope" {
				sc = TenantThresholdScope{}
			}
			if _, err := PromotionApprovalThresholdTableForTenant(sc, res); err == nil {
				t.Fatalf("%s: malformed tenant table was accepted", tc.name)
			}
		}
	})
}

func TestTodo_REV_010_02_Golden(t *testing.T) {
	res := TenantThresholdResolution{FinanceText: "12.5000", ExecutiveText: "30.0000", Revision: 11, DefinitionVersion: "tenant-params/v11", Found: true}
	table, err := PromotionApprovalThresholdTableForTenant(TenantThresholdScope{Tenant: "tenant-golden"}, res)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := table.Digest()
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:e196499ea8d1342a30990e03b611aef11ec9dabdcd282e05678503b7110c5935"
	if digest != wantDigest {
		t.Fatalf("tenant table digest = %s, want %s", digest, wantDigest)
	}
	if table.Version != "tenant-params/v11" {
		t.Fatalf("tenant table version = %q", table.Version)
	}
	reference, err := PromotionApprovalThresholdTable().Digest()
	if err != nil {
		t.Fatal(err)
	}
	const wantReference = "sha256:b5444018e1f51eb51ca41e2144d4fdd9fa08eb2af4a078d9582eccab08141003"
	if reference != wantReference {
		t.Fatalf("reference table digest = %s, want %s", reference, wantReference)
	}
}

func TestTodo_REV_010_02_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(1002))
	for i := 0; i < 200; i++ {
		fPct := rng.Intn(4000)
		ePct := fPct + 1 + rng.Intn(4000-fPct)
		incPct := rng.Intn(6001)
		res := TenantThresholdResolution{
			FinanceText:       formatThresholdCents(fPct),
			ExecutiveText:     formatThresholdCents(ePct),
			Revision:          uint64(i + 1),
			DefinitionVersion: "tenant-params/property",
			Found:             true,
		}
		table, err := PromotionApprovalThresholdTableForTenant(TenantThresholdScope{Tenant: "tenant-property"}, res)
		if err != nil {
			t.Fatalf("case %d: valid thresholds refused: %v", i, err)
		}
		decision, err := EvaluatePromotionApproval(table, PromotionApprovalInput{
			IncreasePercent: dec(t, formatThresholdCents(incPct), IncreasePercentScale),
			BandPosition:    BandPositionInBand,
			BudgetAuthority: BudgetAuthoritySufficient,
		})
		if err != nil {
			t.Fatal(err)
		}
		var want ApprovalTier
		switch {
		case incPct > ePct:
			want = ApprovalTierExecutiveRequired
		case incPct > fPct:
			want = ApprovalTierFinanceRequired
		default:
			want = ApprovalTierStandard
		}
		if decision.Tier != want {
			t.Fatalf("case %d: increase %d vs %d/%d -> %s, want %s", i, incPct, fPct, ePct, decision.Tier, want)
		}
		if decision.TableVersion != "tenant-params/property" {
			t.Fatalf("case %d: cited version %q", i, decision.TableVersion)
		}
	}
}

func formatThresholdCents(cents int) string {
	return fmt.Sprintf("%d.%04d", cents/100, (cents%100)*100)
}
