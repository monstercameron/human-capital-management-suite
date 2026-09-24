package selectedjurisdiction

import (
	"bytes"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func contextFixture(t *testing.T, j legal.Jurisdiction) *legal.LegalContext {
	t.Helper()
	pack, err := legal.CaliforniaPromotionPack()
	if j.State == "NY" {
		pack, err = legal.NewYorkPromotionPack()
	}
	if err != nil {
		t.Fatal(err)
	}
	reg := legal.NewRegistry()
	if err := reg.Register(pack); err != nil {
		t.Fatal(err)
	}
	signer, err := legal.NewSigner(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := values.NewLocalDate(2026, time.March, 1)
	knownInstant, _ := values.NewInstantFromUnix(1770000000, 0)
	recordedInstant, _ := values.NewInstantFromUnix(1770100000, 0)
	k, _ := values.NewKnownAt(knownInstant)
	input := legal.LegalContextInput{LegalEntityID: "entity-1", WorkLocation: j, EmploymentJurisdiction: j, EffectiveDate: d, KnownAt: k}
	ctx, err := legal.Resolve(input, reg, signer, recordedInstant)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestSelectedJurisdictionPromotionAndLeaveHandleAmbiguityRuleTimeAndReplanConsistently(t *testing.T) {
	for _, slice := range []Slice{Promotion, MedicalLeave} {
		t.Run(string(slice), func(t *testing.T) {
			ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
			got, err := Evaluate(Request{Slice: slice, Context: ctx, Ambiguous: true})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != Blocked || got.SelectedJurisdiction != (legal.Jurisdiction{}) {
				t.Fatalf("ambiguity = %#v", got)
			}
			stable, err := Evaluate(Request{Slice: slice, Context: ctx})
			if err != nil {
				t.Fatal(err)
			}
			if stable.Status != Allowed || stable.SelectedJurisdiction != ctx.Jurisdiction() {
				t.Fatalf("stable = %#v", stable)
			}
			replan, err := Evaluate(Request{Slice: slice, Context: ctx, MaterialChange: true})
			if err != nil {
				t.Fatal(err)
			}
			if replan.Status != ReplanRequired || replan.SuccessorProposal == "" || replan.StaleEffects != 0 {
				t.Fatalf("replan = %#v", replan)
			}
		})
	}
}

func TestTodo_CROSS_CONF_001_Conformance(t *testing.T) {
	ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
	for _, slice := range []Slice{Promotion, MedicalLeave} {
		got, err := Evaluate(Request{Slice: slice, Context: ctx})
		if err != nil {
			t.Fatalf("Evaluate(%s): %v", slice, err)
		}
		if got.Status != Allowed || got.SelectedJurisdiction != ctx.Jurisdiction() || got.CompositionDigest == "" {
			t.Errorf("conformance result for %s = %#v", slice, got)
		}
	}
}
func TestTodo_CROSS_CONF_001_Fault(t *testing.T) {
	ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
	if got, err := Evaluate(Request{Slice: Promotion, Context: ctx}); err != nil || got.Status != Allowed {
		t.Fatalf("unexpected fault baseline: %#v %v", got, err)
	}
	if got, err := Evaluate(Request{Slice: Promotion}); err == nil || got.Status != Blocked {
		t.Fatalf("missing context: %#v %v", got, err)
	}
}
func TestTodo_CROSS_CONF_001_Golden(t *testing.T) {
	ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
	a, _ := Evaluate(Request{Slice: Promotion, Context: ctx})
	b, _ := Evaluate(Request{Slice: Promotion, Context: ctx})
	if a.CompositionDigest != b.CompositionDigest {
		t.Fatalf("digest is not deterministic")
	}
}
func TestTodo_CROSS_CONF_001_ModelBased(t *testing.T) {
	for _, slice := range []Slice{Promotion, MedicalLeave} {
		for _, ambiguous := range []bool{false, true} {
			got, err := Evaluate(Request{Slice: slice, Context: contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"}), Ambiguous: ambiguous})
			if err != nil {
				t.Fatalf("Evaluate(%s, ambiguous=%t): %v", slice, ambiguous, err)
			}
			want := Allowed
			if ambiguous {
				want = Blocked
			}
			if got.Status != want {
				t.Fatalf("Evaluate(%s, ambiguous=%t) status = %s, want %s", slice, ambiguous, got.Status, want)
			}
		}
	}
}
func TestTodo_CROSS_CONF_001_Mutation(t *testing.T) {
	ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
	got, err := Evaluate(Request{Slice: Promotion, Context: ctx, MaterialChange: true})
	if err != nil || got.Status != ReplanRequired || got.SuccessorProposal == "" {
		t.Fatalf("material change result = %#v, %v", got, err)
	}
}
func TestTodo_CROSS_CONF_001_Property(t *testing.T) {
	ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
	var wantDigest string
	for i := 0; i < 8; i++ {
		got, err := Evaluate(Request{Slice: Promotion, Context: ctx})
		if err != nil {
			t.Fatal(err)
		}
		if got.CompositionDigest == "" {
			t.Fatalf("iteration %d returned empty digest", i)
		}
		if i == 0 {
			wantDigest = got.CompositionDigest
			continue
		}
		if got.CompositionDigest != wantDigest {
			t.Fatalf("iteration %d digest = %q, want stable digest %q", i, got.CompositionDigest, wantDigest)
		}
	}
}
func TestTodo_CROSS_CONF_001_Recovery(t *testing.T) {
	ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
	initial, err := Evaluate(Request{Slice: MedicalLeave, Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	afterChange, err := Evaluate(Request{Slice: MedicalLeave, Context: ctx, MaterialChange: true})
	if err != nil {
		t.Fatal(err)
	}
	if afterChange.Status != ReplanRequired || afterChange.StaleEffects != 0 || initial.Status != Allowed {
		t.Fatalf("recovery transition initial=%#v changed=%#v", initial, afterChange)
	}
}
func TestTodo_CROSS_CONF_001_Security(t *testing.T) {
	if _, err := Evaluate(Request{Slice: Promotion}); err == nil {
		t.Fatal("missing signed jurisdiction context was accepted")
	}
}
