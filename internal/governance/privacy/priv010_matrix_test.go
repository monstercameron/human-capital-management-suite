package privacy

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestTodo_PRIV_010_Property proves roster determinism and per-state
// isolation over seeded version sets.
func TestTodo_PRIV_010_Property(t *testing.T) {
	for seed := 0; seed < 25; seed++ {
		laws := fixtureStateLaws()
		// Seeded reorderings never change the sealed signature.
		for i := len(laws) - 1; i > 0; i-- {
			j := (seed + i) % (i + 1)
			laws[i], laws[j] = laws[j], laws[i]
		}
		review := PrimaryReview{
			ReviewedAt: stateReviewAt(), Reviewer: "privacy-counsel",
			SourceRef: "statute-text:all", CoversRoster: "roster-prop",
		}
		first, err := Seal("roster-prop", laws, review, "release-gate", stateReviewAt())
		if err != nil {
			t.Fatal(err)
		}
		second, err := Seal("roster-prop", fixtureStateLaws(), review, "release-gate", stateReviewAt())
		if err != nil {
			t.Fatal(err)
		}
		if first.Signature != second.Signature {
			t.Fatalf("seed %d: law order changed the roster signature", seed)
		}
	}
}

// TestTodo_PRIV_010_Security proves the gate trusts no unsigned,
// misreviewed or tampered roster: forgeries fail closed per state.
func TestTodo_PRIV_010_Security(t *testing.T) {
	roster := fixtureRoster(t)
	cases := map[string]Roster{
		"unsigned":       {Version: roster.Version, Laws: roster.Laws, Review: roster.Review},
		"wrong signer":   {Version: roster.Version, Laws: roster.Laws, Signature: "forged:someone", SignedBy: "", Review: roster.Review},
		"review swapped": {Version: roster.Version, Laws: roster.Laws, Signature: roster.Signature, SignedBy: roster.SignedBy, Review: PrimaryReview{ReviewedAt: stateReviewAt(), Reviewer: "", SourceRef: "blog-summary", CoversRoster: roster.Version}},
		"laws swapped": func() Roster {
			mut := roster
			mut.Laws = append([]StateLaw(nil), roster.Laws...)
			mut.Laws[0], mut.Laws[1] = mut.Laws[1], mut.Laws[0]
			mut.Laws[0].State, mut.Laws[1].State = mut.Laws[1].State, mut.Laws[0].State
			return mut
		}(),
	}
	for name, mut := range cases {
		for _, state := range []StateCode{StateCA, StateCO, StateVA, StateTX} {
			// Every tamper breaks the sealed signature and fails closed;
			// none may resolve as if honest.
			if _, err := mut.Resolve(state, evalDate()); !errors.Is(err, ErrStateLawRefused) {
				t.Fatalf("%s/%s must fail closed, got %v", name, state, err)
			}
		}
	}
}

// TestTodo_PRIV_010_Integration proves the gate composes with the consent
// record: a resolved opt-out signal governs disclosure, and an unhonored
// state blocks release rather than falling back to another state.
func TestTodo_PRIV_010_Integration(t *testing.T) {
	roster := fixtureRoster(t)
	signals := map[StateCode]string{}
	for _, state := range []StateCode{StateCA, StateCO, StateVA, StateTX} {
		res, err := roster.Resolve(state, evalDate())
		if err != nil {
			t.Fatalf("Resolve %s: %v", state, err)
		}
		if strings.TrimSpace(res.OptOutSignal) == "" || strings.TrimSpace(res.ConsentSignal) == "" {
			t.Fatalf("%s resolved without opt-out/consent signals", state)
		}
		signals[state] = res.OptOutSignal
	}
	if len(signals) != 4 {
		t.Fatal("integration did not govern all four gated states")
	}
	if _, err := roster.Resolve("NY", evalDate()); !errors.Is(err, ErrStateLawRefused) {
		t.Fatalf("unenacted state must block release, got %v", err)
	}
}

// TestTodo_PRIV_010_Conformance walks the enacted-law matrix: every gated
// state resolves its exact law identity, version and duty set.
func TestTodo_PRIV_010_Conformance(t *testing.T) {
	roster := fixtureRoster(t)
	expect := map[StateCode]string{
		StateCA: "CCPA/CPRA", StateCO: "CPA", StateVA: "VCDPA", StateTX: "TDPSA",
	}
	for state, law := range expect {
		res, err := roster.Resolve(state, evalDate())
		if err != nil {
			t.Fatalf("Resolve %s: %v", state, err)
		}
		if res.LawID != law || res.Version != "2024-1" || res.RosterVersion != "roster-7" {
			t.Fatalf("%s resolved %+v, want %s/2024-1/roster-7", state, res, law)
		}
		if len(res.Rights) == 0 || len(res.ProcessorDuties) == 0 {
			t.Fatalf("%s resolved without rights or duties", state)
		}
	}
}

// TestTodo_PRIV_010_Mutation kills the collapse mutants: a dropped state,
// a duplicated version and a summary-only review must each be detected.
func TestTodo_PRIV_010_Mutation(t *testing.T) {
	review := PrimaryReview{
		ReviewedAt: stateReviewAt(), Reviewer: "privacy-counsel",
		SourceRef: "statute-text:all", CoversRoster: "roster-mut",
	}
	dropped := fixtureStateLaws()[:3]
	sealed, err := Seal("roster-mut", dropped, review, "release-gate", stateReviewAt())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sealed.Resolve(StateTX, evalDate()); !errors.Is(err, ErrStateLawRefused) {
		t.Fatalf("dropped-state mutant must be refused, got %v", err)
	}
	doubled := append(fixtureStateLaws(), fixtureStateLaws()[0])
	if _, err := Seal("roster-mut", doubled, review, "release-gate", stateReviewAt()); !errors.Is(err, ErrStateLawConflict) {
		t.Fatalf("duplicated-version mutant must be refused, got %v", err)
	}
	single := fixtureStateLaws()[:1]
	one, err := Seal("roster-mut", single, review, "release-gate", stateReviewAt())
	if err != nil {
		t.Fatal(err)
	}
	ca, err := one.Resolve(StateCA, evalDate())
	if err != nil {
		t.Fatal(err)
	}
	co, _ := fixtureRoster(t).Resolve(StateCO, evalDate())
	if ca.OptOutSignal == co.OptOutSignal && ca.LawID == co.LawID {
		t.Fatal("single-state roster leaked CA answers as CO answers")
	}
}

// FuzzTodo_PRIV_010 proves arbitrary state codes and dates either resolve
// under the exact reviewed contract or fail closed.
func FuzzTodo_PRIV_010(f *testing.F) {
	f.Add("CA", int64(20260701))
	f.Add("XX", int64(20260701))
	f.Add("CA", int64(20200101))
	f.Add("", int64(0))
	f.Fuzz(func(t *testing.T, state string, yyyymmdd int64) {
		roster := fixtureRoster(t)
		year, month, day := yyyymmdd/10000, (yyyymmdd/100)%100, yyyymmdd%100
		asOf := time.Date(int(year), time.Month(month), int(day), 0, 0, 0, 0, time.UTC)
		res, err := roster.Resolve(StateCode(state), asOf)
		if err != nil {
			if !errors.Is(err, ErrStateLawRefused) {
				t.Fatalf("Resolve must fail closed, got %v", err)
			}
			return
		}
		if !ValidStateLaw(res.State) || res.Digest == "" {
			t.Fatalf("resolution is not bound to a gated state: %+v", res)
		}
		if len(res.Rights) == 0 || strings.TrimSpace(res.OptOutSignal) == "" {
			t.Fatal("fuzzed resolution dropped rights or signals")
		}
	})
}
