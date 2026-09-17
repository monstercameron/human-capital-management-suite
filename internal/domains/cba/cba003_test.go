package cba

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func cba003At() time.Time { return time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) }

func cba003Change() AgreementChange {
	return AgreementChange{
		AgreementID: "agreement-1", FromRevision: "2026.1", ToRevision: "2026.2",
		ChangedClauses: []ClauseChange{
			{ClauseRef: "wage-14", Kind: ConstraintWage, FromValue: "18.00", ToValue: "19.25"},
			{ClauseRef: "discipline-3", Kind: ConstraintDiscipline, FromValue: "verbal", ToValue: "written"},
		},
		AffectedPopulation:   PopulationPin{ID: "unit-7", FrozenAt: cba003At(), Digest: "sha256:population", MemberCount: 42},
		GrievanceDeadlineRef: "deadline:grievance:2026-05", ArbitrationDeadlineRef: "deadline:arbitration:2026-06",
		RepresentationRef: "union:local-7", CalendarRef: "calendar:us-ny:2026",
		EffectiveAt: cba003At().Add(30 * 24 * time.Hour), EvidenceDigest: "evidence:change:2026.2",
	}
}

func cba003Prior() CompositionResult {
	return CompositionResult{Outcome: CompositionAllowWithObligations, Obligations: []CompositionObligation{{ID: "wage-14", Kind: "WAGE", ClauseRef: "wage-14", ReleaseRef: "2026.1", Source: SourceAgreement, Mandatory: true}}, Reason: "prior composition"}
}

// TestCBARevisionImpactCreatesRepresentationAndGrievanceWorkWithoutRewritingHistory
// is the RED contract: silent alteration, missed population, missed
// deadlines and waived representation all refuse.
func TestCBARevisionImpactCreatesRepresentationAndGrievanceWorkWithoutRewritingHistory(t *testing.T) {
	impact, err := AnalyzeAgreementChange(cba003Change(), cba003Prior())
	if err != nil {
		t.Fatalf("AnalyzeAgreementChange: %v", err)
	}
	if impact.Digest == "" || len(impact.Intents) == 0 {
		t.Fatalf("impact = %+v", impact)
	}
	kinds := map[ChangeIntentKind]int{}
	for _, in := range impact.Intents {
		kinds[in.Kind]++
		if in.CalendarRef == "" || in.EvidenceRef == "" {
			t.Fatalf("intent lacks calendar/evidence: %+v", in)
		}
	}
	for _, want := range []ChangeIntentKind{IntentNotice, IntentReevaluation, IntentConsultation, IntentGrievanceWindow, IntentArbitrationWindow} {
		if kinds[want] == 0 {
			t.Fatalf("missing %s intent: %+v", want, impact.Intents)
		}
	}
	if impact.PriorDigest == "" {
		t.Fatal("prior decisions left no explainable digest")
	}

	// Seeded defect: the manager waives representation and the change still
	// produces work.
	waived := cba003Change()
	waived.RepresentationRef = ""
	if _, err := AnalyzeAgreementChange(waived, cba003Prior()); !errors.Is(err, ErrRepresentationRequired) {
		t.Fatalf("waived representation err=%v, want refusal", err)
	}

	// Missed grievance deadline.
	nodeadline := cba003Change()
	nodeadline.GrievanceDeadlineRef = ""
	if _, err := AnalyzeAgreementChange(nodeadline, cba003Prior()); !errors.Is(err, ErrChangeDeadlineRequired) {
		t.Fatalf("missing deadline err=%v, want refusal", err)
	}

	// Missed population.
	nopop := cba003Change()
	nopop.AffectedPopulation.MemberCount = 0
	if _, err := AnalyzeAgreementChange(nopop, cba003Prior()); !errors.Is(err, ErrInvalidAgreementChange) {
		t.Fatalf("empty population err=%v, want refusal", err)
	}
}

// TestTodo_CBA_003_Property proves intent generation is total over clause
// kinds: every change yields notice plus reevaluation, and each kind adds
// its typed work.
func TestTodo_CBA_003_Property(t *testing.T) {
	cases := []struct {
		kind ConstraintKind
		want []ChangeIntentKind
	}{
		{ConstraintWage, []ChangeIntentKind{IntentNotice, IntentReevaluation, IntentConsultation}},
		{ConstraintSchedule, []ChangeIntentKind{IntentNotice, IntentReevaluation, IntentConsultation}},
		{ConstraintLeave, []ChangeIntentKind{IntentNotice, IntentReevaluation, IntentConsultation}},
		{ConstraintSeniority, []ChangeIntentKind{IntentNotice, IntentReevaluation, IntentGrievanceWindow}},
		{ConstraintDiscipline, []ChangeIntentKind{IntentNotice, IntentReevaluation, IntentGrievanceWindow, IntentArbitrationWindow}},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			change := cba003Change()
			change.ChangedClauses = []ClauseChange{{ClauseRef: "clause-1", Kind: tc.kind, FromValue: "a", ToValue: "b"}}
			impact, err := AnalyzeAgreementChange(change, cba003Prior())
			if err != nil {
				t.Fatal(err)
			}
			got := map[ChangeIntentKind]bool{}
			for _, in := range impact.Intents {
				got[in.Kind] = true
			}
			for _, want := range tc.want {
				if !got[want] {
					t.Fatalf("missing %s in %+v", want, impact.Intents)
				}
			}
			if len(impact.AffectedKinds) != 1 || impact.AffectedKinds[0] != tc.kind {
				t.Fatalf("affected = %v", impact.AffectedKinds)
			}
		})
	}
}

// TestTodo_CBA_003_Golden pins the exact intent order of the fixture change.
func TestTodo_CBA_003_Golden(t *testing.T) {
	impact, err := AnalyzeAgreementChange(cba003Change(), cba003Prior())
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{
		{"discipline-3", string(IntentArbitrationWindow)},
		{"discipline-3", string(IntentGrievanceWindow)},
		{"discipline-3", string(IntentNotice)},
		{"discipline-3", string(IntentReevaluation)},
		{"wage-14", string(IntentNotice)},
		{"wage-14", string(IntentReevaluation)},
		{"wage-14", string(IntentConsultation)},
	}
	if len(impact.Intents) != len(want) {
		t.Fatalf("intents = %+v", impact.Intents)
	}
	for i, w := range want {
		if impact.Intents[i].ClauseRef != w[0] || string(impact.Intents[i].Kind) != w[1] {
			t.Fatalf("intent %d = %+v, want %v", i, impact.Intents[i], w)
		}
	}
	again, err := AnalyzeAgreementChange(cba003Change(), cba003Prior())
	if err != nil || again.Digest != impact.Digest {
		t.Fatal("impact digest unstable")
	}
}

// TestTodo_CBA_003_Race proves concurrent analyses of one shared change
// agree exactly.
func TestTodo_CBA_003_Race(t *testing.T) {
	change := cba003Change()
	prior := cba003Prior()
	first, err := AnalyzeAgreementChange(change, prior)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := AnalyzeAgreementChange(change, prior)
			if err != nil {
				t.Error(err)
				return
			}
			if got.Digest != first.Digest || len(got.Intents) != len(first.Intents) {
				t.Errorf("divergent impact: %+v", got)
			}
		}()
	}
	wg.Wait()
}

// TestTodo_CBA_003_Fault proves malformed changes refuse before any intent
// is generated.
func TestTodo_CBA_003_Fault(t *testing.T) {
	base := cba003Change()
	cases := []struct {
		name   string
		mutate func(*AgreementChange)
	}{
		{"no-clauses", func(c *AgreementChange) { c.ChangedClauses = nil }},
		{"same-revision", func(c *AgreementChange) { c.ToRevision = c.FromRevision }},
		{"no-population", func(c *AgreementChange) { c.AffectedPopulation.Digest = "" }},
		{"no-arbitration", func(c *AgreementChange) { c.ArbitrationDeadlineRef = "" }},
		{"no-calendar", func(c *AgreementChange) { c.CalendarRef = "" }},
		{"no-evidence", func(c *AgreementChange) { c.EvidenceDigest = "" }},
		{"predated", func(c *AgreementChange) { c.EffectiveAt = cba003At().Add(-time.Hour) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change := base
			tc.mutate(&change)
			if _, err := AnalyzeAgreementChange(change, cba003Prior()); err == nil {
				t.Fatal("malformed change produced intents")
			}
		})
	}
}

// TestTodo_CBA_003_Security proves the frozen population is bound, not
// described: a substituted population changes the digest, and waived
// representation never passes.
func TestTodo_CBA_003_Security(t *testing.T) {
	first, err := AnalyzeAgreementChange(cba003Change(), cba003Prior())
	if err != nil {
		t.Fatal(err)
	}
	swapped := cba003Change()
	swapped.AffectedPopulation.Digest = "sha256:substituted"
	second, err := AnalyzeAgreementChange(swapped, cba003Prior())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == second.Digest {
		t.Fatal("substituted population kept the impact digest")
	}
	if second.AffectedPopulation.Digest != "sha256:substituted" {
		t.Fatal("impact does not bind the population it analyzed")
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	tampered := second
	tampered.AffectedPopulation.Digest = "sha256:forged"
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered impact validated")
	}
}

// TestTodo_CBA_003_Conformance proves the impact agrees with an independent
// recomputation: kinds re-derived from clauses, and the prior digest
// recomputed from the prior result.
func TestTodo_CBA_003_Conformance(t *testing.T) {
	change := cba003Change()
	impact, err := AnalyzeAgreementChange(change, cba003Prior())
	if err != nil {
		t.Fatal(err)
	}
	if got := AffectedKinds(change); len(got) != len(impact.AffectedKinds) {
		t.Fatalf("kinds = %v vs %v", got, impact.AffectedKinds)
	} else {
		for i := range got {
			if got[i] != impact.AffectedKinds[i] {
				t.Fatalf("kinds = %v vs %v", got, impact.AffectedKinds)
			}
		}
	}
	if got := PriorDigestOf(cba003Prior()); got != impact.PriorDigest {
		t.Fatal("prior digest disagrees with recomputation")
	}
}

// TestTodo_CBA_003_Mutation proves forged changes cannot slip through as
// clean impacts.
func TestTodo_CBA_003_Mutation(t *testing.T) {
	change := cba003Change()
	change.ChangedClauses[0].Kind = "BONUS"
	if _, err := AnalyzeAgreementChange(change, cba003Prior()); !errors.Is(err, ErrInvalidAgreementChange) {
		t.Fatalf("unknown kind err=%v", err)
	}
	backdated := cba003Change()
	backdated.AffectedPopulation.FrozenAt = backdated.EffectiveAt.Add(time.Hour)
	if _, err := AnalyzeAgreementChange(backdated, cba003Prior()); !errors.Is(err, ErrInvalidAgreementChange) {
		t.Fatalf("unfrozen population err=%v", err)
	}
	impact, err := AnalyzeAgreementChange(cba003Change(), cba003Prior())
	if err != nil {
		t.Fatal(err)
	}
	trimmed := impact
	trimmed.Intents = trimmed.Intents[:len(trimmed.Intents)-1]
	if err := trimmed.Validate(); err == nil {
		t.Fatal("trimmed impact validated")
	}
}
