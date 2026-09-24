package recruiting

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func impactFixture(t *testing.T) (*Aggregate, *StageLedger, []DeclaredApplicantGroup) {
	t.Helper()
	a := openRecruiting(t)
	type applicant struct {
		id, group string
		selected  bool
	}
	people := make([]applicant, 0, 13)
	for i := 0; i < 10; i++ {
		group, selected := "Group A", i < 5
		if i >= 5 {
			group, selected = "Group B", i == 5
		}
		people = append(people, applicant{id: "cand-" + string(rune('a'+i)), group: group, selected: selected})
	}
	for i := 0; i < 3; i++ {
		people = append(people, applicant{id: "cand-" + string(rune('k'+i)), group: "Small cohort", selected: true})
	}
	for i, person := range people {
		appID, candidateID := "app-"+string(rune('a'+i)), "person-"+string(rune('a'+i))
		a = submitRecruiting(t, a, appID, candidateID)
		if err := a.CreateCandidacy(person.id, appID, candidateID, "consent:"+person.id, "hiring-analysis", "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
			t.Fatal(err)
		}
	}
	ledger, err := NewStageLedger(&a, []string{"hiring-manager-1"})
	if err != nil {
		t.Fatal(err)
	}
	groups := make([]DeclaredApplicantGroup, 0, len(people))
	for i, person := range people {
		groups = append(groups, DeclaredApplicantGroup{CandidacyID: person.id, Group: person.group, Purpose: "hiring-analysis", AuthorizationRef: "consent:" + person.id})
		if person.selected {
			evidenceID := "ev-screen-" + string(rune('a'+i))
			if err := ledger.RegisterEvidence(StageEvidence{EvidenceID: evidenceID, CandidacyID: person.id, Kind: EvidenceScreening, ContentDigest: "digest:" + evidenceID, SourceRef: "source:screening"}); err != nil {
				t.Fatal(err)
			}
			cmd := AdvanceCmd{RequisitionID: "req-1", RequisitionRevision: 1, CandidacyID: person.id, ExpectedCandidacyRevision: 1, ToStage: StageScreening, EvidenceIDs: []string{evidenceID}, Decision: stageDecision(t, person.id, 1, StageScreening), EffectiveAt: recruitInstant(t, 9), KnownAt: recruitKnown(t, 9)}
			if err := ledger.AdvanceStage(cmd); err != nil {
				t.Fatal(err)
			}
		} else if err := a.TransitionCandidacy(person.id, 1, StageRejected, recruitInstant(t, 9), recruitKnown(t, 9)); err != nil {
			t.Fatal(err)
		}
	}
	return &a, ledger, groups
}

func pinnedImpactFixture(t *testing.T) HiringFunnelSnapshot {
	t.Helper()
	a, ledger, groups := impactFixture(t)
	snapshot, err := PinHiringFunnelSnapshot(a, ledger, groups)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestTodo_REV_051_03(t *testing.T) {
	got, err := AnalyzeHiringFunnel(pinnedImpactFixture(t), HiringFunnelPolicy{MinimumApplicants: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Transitions) != 1 || got.Transitions[0].FromStage != StageApplied || got.Transitions[0].ToStage != StageScreening {
		t.Fatalf("transitions = %#v", got.Transitions)
	}
	groups := got.Transitions[0].Groups
	if len(groups) != 3 {
		t.Fatalf("groups = %#v", groups)
	}
	if groups[0].Group != "Group A" || groups[0].Applicants != 5 || groups[0].Selected != 5 || groups[0].Status != SelectionRateKnown || groups[0].Rate != 1 {
		t.Errorf("reference group = %#v", groups[0])
	}
	if groups[1].Group != "Group B" || groups[1].Applicants != 5 || groups[1].Selected != 1 || !groups[1].FourFifths || groups[1].ComparedTo != "Group A" || math.Abs(groups[1].Ratio-0.2) > 1e-12 {
		t.Errorf("comparison group = %#v", groups[1])
	}
	if groups[2].Applicants != 3 || groups[2].Status != SelectionRateUnknown || groups[2].Rate != 0 || groups[2].FourFifths {
		t.Errorf("small group should be unknown: %#v", groups[2])
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "CandidacyID") || strings.Contains(string(encoded), "consent:") {
		t.Fatalf("aggregate output contains raw facts: %s", encoded)
	}
}

func TestTodo_REV_051_03_Golden(t *testing.T) {
	got, err := AnalyzeHiringFunnel(pinnedImpactFixture(t), HiringFunnelPolicy{MinimumApplicants: 5})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"SnapshotID":"PLACEHOLDER","Revision":PLACEHOLDER,"Transitions":[{"FromStage":"APPLIED","ToStage":"SCREENING","Groups":[{"Group":"Group A","Applicants":5,"Selected":5,"Status":"KNOWN","Rate":1,"ComparedTo":"","Ratio":0,"FourFifths":false},{"Group":"Group B","Applicants":5,"Selected":1,"Status":"KNOWN","Rate":0.2,"ComparedTo":"Group A","Ratio":0.2,"FourFifths":true},{"Group":"Small cohort","Applicants":3,"Selected":3,"Status":"UNKNOWN","Rate":0,"ComparedTo":"","Ratio":0,"FourFifths":false}]}]}`
	// Pin identity includes the source revision and is deliberately not
	// fixed across changed fixtures; pin the aggregate report bytes with
	// the generated source identity normalized.
	normalized := strings.Replace(string(b), `"SnapshotID":"`+got.SnapshotID+`"`, `"SnapshotID":"PIN"`, 1)
	normalized = strings.Replace(normalized, `"Revision":`+jsonNumber(got.Revision), `"Revision":PIN`, 1)
	want = strings.Replace(want, `"SnapshotID":"PLACEHOLDER"`, `"SnapshotID":"PIN"`, 1)
	want = strings.Replace(want, `"Revision":PLACEHOLDER`, `"Revision":PIN`, 1)
	if normalized != want {
		t.Fatalf("golden mismatch\n got: %s\nwant: %s", normalized, want)
	}
}

func TestTodo_REV_051_03_Property(t *testing.T) {
	snapshot := pinnedImpactFixture(t)
	first, err := AnalyzeHiringFunnel(snapshot, HiringFunnelPolicy{MinimumApplicants: 5})
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(snapshot.Outcomes)-1; i < j; i, j = i+1, j-1 {
		snapshot.Outcomes[i], snapshot.Outcomes[j] = snapshot.Outcomes[j], snapshot.Outcomes[i]
	}
	// A pin seals order as well as content, so a reordered source is rejected.
	if _, err := AnalyzeHiringFunnel(snapshot, HiringFunnelPolicy{MinimumApplicants: 5}); err == nil {
		t.Fatal("modified pinned outcome order was accepted")
	}
	_ = first
	// A caller's small-sample cutoff is explicit; no universal cutoff is
	// baked into the analysis function.
	snapshot = pinnedImpactFixture(t)
	result, err := AnalyzeHiringFunnel(snapshot, HiringFunnelPolicy{MinimumApplicants: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range result.Transitions[0].Groups {
		if group.Group == "Small cohort" && group.Status != SelectionRateUnknown {
			t.Fatalf("policy cutoff ignored: %+v", group)
		}
	}
	result, err = AnalyzeHiringFunnel(snapshot, HiringFunnelPolicy{MinimumApplicants: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range result.Transitions[0].Groups {
		if group.Group == "Small cohort" && group.Status != SelectionRateKnown {
			t.Fatalf("configured sample cutoff ignored: %+v", group)
		}
	}
	for i := range snapshot.Outcomes {
		if snapshot.Outcomes[i].CandidacyID == "cand-g" || snapshot.Outcomes[i].CandidacyID == "cand-h" || snapshot.Outcomes[i].CandidacyID == "cand-i" {
			snapshot.Outcomes[i].Outcome = FunnelAdvanced
		}
	}
	boundarySeal, err := snapshotSeal(snapshot.Revision, snapshot.sourceDigest, snapshot.Outcomes, snapshot.Groups)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.seal, snapshot.SnapshotID = boundarySeal, boundarySeal
	result, err = AnalyzeHiringFunnel(snapshot, HiringFunnelPolicy{MinimumApplicants: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range result.Transitions[0].Groups {
		if group.Group == "Group B" && (group.Ratio != 0.8 || group.FourFifths) {
			t.Fatalf("exact four-fifths boundary flagged: %+v", group)
		}
	}
}

func TestTodo_REV_051_03_Mutation(t *testing.T) {
	a, ledger, facts := impactFixture(t)
	beforeEvents := append([]RecruitingEvent(nil), a.Events...)
	pinned, err := PinHiringFunnelSnapshot(a, ledger, facts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Events, beforeEvents) {
		t.Fatal("snapshot pin mutated recruiting event state")
	}
	if _, err := AnalyzeHiringFunnel(pinned, HiringFunnelPolicy{MinimumApplicants: 5}); err != nil {
		t.Fatal(err)
	}
	pinned.Outcomes[0].Outcome = FunnelRejected
	if _, err := AnalyzeHiringFunnel(pinned, HiringFunnelPolicy{MinimumApplicants: 5}); err == nil {
		t.Fatal("tampered pinned snapshot was accepted")
	}
	for i := range facts {
		facts[i].AuthorizationRef = "consent:other"
		break
	}
	if _, err := PinHiringFunnelSnapshot(a, ledger, facts); err == nil {
		t.Fatal("group fact without matching authorization was accepted")
	}
	for i := range a.Events {
		if a.Events[i].AggregateID == "cand-a" && a.Events[i].Kind == "CANDIDACY_ADVANCED" {
			a.Events[i].Kind = "CANDIDACY_REJECTED"
			break
		}
	}
	if _, err := PinHiringFunnelSnapshot(a, ledger, impactFactsFor(t, a)); err == nil {
		t.Fatal("tampered source event history was accepted")
	}
}

func TestTodo_REV_051_03_WithdrawnApplicantKeepsCompletedOutcomes(t *testing.T) {
	a, ledger, groups := impactFixture(t)
	appID, candidateID, candidacyID := "app-withdrawn", "person-withdrawn", "cand-withdrawn"
	*a = submitRecruiting(t, *a, appID, candidateID)
	if err := a.CreateCandidacy(candidacyID, appID, candidateID, "consent:withdrawn", "hiring-analysis", "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
		t.Fatal(err)
	}
	evidenceID := "ev-screen-withdrawn"
	if err := ledger.RegisterEvidence(StageEvidence{EvidenceID: evidenceID, CandidacyID: candidacyID, Kind: EvidenceScreening, ContentDigest: "digest:" + evidenceID, SourceRef: "source:screening"}); err != nil {
		t.Fatal(err)
	}
	cmd := AdvanceCmd{RequisitionID: "req-1", RequisitionRevision: 1, CandidacyID: candidacyID, ExpectedCandidacyRevision: 1, ToStage: StageScreening, EvidenceIDs: []string{evidenceID}, Decision: stageDecision(t, candidacyID, 1, StageScreening), EffectiveAt: recruitInstant(t, 9), KnownAt: recruitKnown(t, 9)}
	if err := ledger.AdvanceStage(cmd); err != nil {
		t.Fatal(err)
	}
	if err := a.TransitionCandidacy(candidacyID, 2, StageWithdrawn, recruitInstant(t, 10), recruitKnown(t, 10)); err != nil {
		t.Fatal(err)
	}
	groups = append(groups, DeclaredApplicantGroup{CandidacyID: candidacyID, Group: "Withdraw cohort", Purpose: "hiring-analysis", AuthorizationRef: "consent:withdrawn"})
	pinned, err := PinHiringFunnelSnapshot(a, ledger, groups)
	if err != nil {
		t.Fatal(err)
	}
	var applied, screening int
	for _, outcome := range pinned.Outcomes {
		if outcome.CandidacyID == candidacyID {
			if outcome.FromStage == StageApplied && outcome.Outcome == FunnelAdvanced {
				applied++
			}
			if outcome.FromStage == StageScreening {
				screening++
			}
		}
	}
	if applied != 1 || screening != 0 {
		t.Fatalf("completed/pending withdrawal outcomes applied=%d screening=%d", applied, screening)
	}
	result, err := AnalyzeHiringFunnel(pinned, HiringFunnelPolicy{MinimumApplicants: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range result.Transitions {
		for _, group := range transition.Groups {
			if group.Group == "Withdraw cohort" && (transition.FromStage != StageApplied || group.Applicants != 1 || group.Selected != 1) {
				t.Fatalf("withdrawal did not preserve only completed stage decision: %+v", transition)
			}
		}
	}
}

func TestTodo_REV_051_03_RejectsForgedOrUnboundPin(t *testing.T) {
	if _, err := AnalyzeHiringFunnel(HiringFunnelSnapshot{SnapshotID: "fake", Revision: 1}, HiringFunnelPolicy{MinimumApplicants: 1}); err == nil {
		t.Fatal("caller-supplied snapshot id was trusted")
	}
	a, ledger, facts := impactFixture(t)
	facts[0].Purpose = "unrelated-purpose"
	if _, err := PinHiringFunnelSnapshot(a, ledger, facts); err == nil {
		t.Fatal("unbound purpose was accepted")
	}
	other, err := NewStageLedger(a, []string{"hiring-manager-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PinHiringFunnelSnapshot(a, other, impactFactsFor(t, a)); err == nil {
		t.Fatal("unowned stage ledger was accepted")
	}
}

func impactFactsFor(t *testing.T, a *Aggregate) []DeclaredApplicantGroup {
	t.Helper()
	var facts []DeclaredApplicantGroup
	for id, c := range a.Candidacies {
		if c.Stage != StageRejected {
			facts = append(facts, DeclaredApplicantGroup{CandidacyID: id, Group: "group", Purpose: c.Purpose, AuthorizationRef: c.ConsentRef})
		}
	}
	return facts
}

func jsonNumber(value uint64) string { b, _ := json.Marshal(value); return string(b) }
