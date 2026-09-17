package recruiting

import (
	"strings"
	"sync"
	"testing"
)

// TestTodo_RECRUIT_002_Property: governed advancement is deterministic,
// revisions advance by exactly one, scores stay append-only, provider
// observations never append stage events, and every refusal is
// zero-effect.
func TestTodo_RECRUIT_002_Property(t *testing.T) {
	build := func(t *testing.T) *StageLedger {
		ledger := openStageLedger(t)
		mustRegisterScreening(t, ledger)
		if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, stageDecision(t, "cand-1", 1, StageScreening))); err != nil {
			t.Fatal(err)
		}
		return ledger
	}

	first, second := build(t), build(t)
	frontier, _ := first.Frontier("cand-1")
	other, _ := second.Frontier("cand-1")
	if first.FrontierDigest("cand-1") != second.FrontierDigest("cand-1") {
		t.Fatal("identical stage histories replayed to different frontier digests")
	}
	if frontier.CandidacyRevision != 2 || other.CandidacyRevision != 2 {
		t.Fatalf("frontiers = %+v %+v", frontier, other)
	}
	if first.FrontierDigest("cand-1") == "" || first.FrontierDigest("unknown") != "" {
		t.Fatal("frontier digest is missing for a current stage or present for none")
	}

	// Scores stay append-only: the original revision is preserved and
	// the successor carries revision+1.
	ledger := build(t)
	score := ScoreCmd{CandidacyID: "cand-1", Stage: StageScreening, Assessor: "panel-1", Score: 70, EffectiveAt: recruitInstant(t, 9), KnownAt: recruitKnown(t, 9)}
	if err := ledger.RecordScore(score); err != nil {
		t.Fatal(err)
	}
	if err := ledger.CorrectScore("cand-1", StageScreening, "panel-1", 75, recruitInstant(t, 10), recruitKnown(t, 10)); err != nil {
		t.Fatal(err)
	}
	history := ledger.ScoreHistory("cand-1", StageScreening, "panel-1")
	if len(history) != 2 || history[0].Score != 70 || history[1].Score != 75 || history[0].Revision+1 != history[1].Revision {
		t.Fatalf("score history = %+v", history)
	}
	if err := ledger.CorrectScore("cand-1", StageScreening, "panel-9", 75, recruitInstant(t, 10), recruitKnown(t, 10)); codeOf(err) != CodeMissingStageRequirement {
		t.Fatalf("correction without a submitted score err = %v", err)
	}

	// Provider observations never advance the frontier or append events.
	before, beforeOutbox := ledgerCounts(ledger)
	if err := ledger.RecordObservation(ProviderObservation{
		CandidacyID: "cand-1", Stage: StageScreening, ProviderRef: "provider:assessment",
		Result: "pass", ObservedAt: recruitInstant(t, 10),
	}); err != nil {
		t.Fatal(err)
	}
	if frontier, _ := ledger.Frontier("cand-1"); frontier.Stage != StageScreening {
		t.Fatalf("observation moved the frontier: %+v", frontier)
	}
	if gotEvents, gotOutbox := ledgerCounts(ledger); gotEvents != before || gotOutbox != beforeOutbox {
		t.Fatal("observation appended events/outbox entries")
	}

	// Every refusal is zero-effect across entities, events and outbox.
	entities := len(ledger.agg.Requisitions) + len(ledger.agg.Postings) + len(ledger.agg.Applications) + len(ledger.agg.Candidacies)
	badDecision := stageDecision(t, "cand-1", 2, StageOffer)
	refusals := []error{
		ledger.AdvanceStage(advanceCmd(t, "cand-1", 2, StageOffer, nil, badDecision)),
		ledger.AdvanceStage(advanceCmd(t, "cand-1", 9, StageInterview, []string{"ev-screen-1"}, stageDecision(t, "cand-1", 9, StageInterview))),
		ledger.AdvanceStage(advanceCmd(t, "cand-x", 1, StageScreening, nil, stageDecision(t, "cand-x", 1, StageScreening))),
		ledger.RecordScore(ScoreCmd{CandidacyID: "cand-1", Stage: StageScreening, Assessor: "panel-1", Score: 101, EffectiveAt: recruitInstant(t, 9), KnownAt: recruitKnown(t, 9)}),
	}
	for i, err := range refusals {
		if err == nil {
			t.Fatalf("refusal %d accepted", i)
		}
	}
	got := len(ledger.agg.Requisitions) + len(ledger.agg.Postings) + len(ledger.agg.Applications) + len(ledger.agg.Candidacies)
	if gotEvents, gotOutbox := ledgerCounts(ledger); got != entities || gotEvents != before || gotOutbox != beforeOutbox {
		t.Fatal("refused stage commands mutated the ledger")
	}
}

// TestTodo_RECRUIT_002_Golden: a fixed screening-to-hire pipeline pins
// its exact event trace and frontier digest.
func TestTodo_RECRUIT_002_Golden(t *testing.T) {
	ledger := openStageLedger(t)
	mustRegisterScreening(t, ledger)
	for _, ev := range []StageEvidence{
		{EvidenceID: "ev-interview-1", CandidacyID: "cand-1", Kind: EvidenceInterview, ContentDigest: "digest:interview-1", SourceRef: "source:interview-panel"},
		{EvidenceID: "ev-assess-1", CandidacyID: "cand-1", Kind: EvidenceAssessment, ContentDigest: "digest:assess-1", SourceRef: "source:assessment-provider"},
	} {
		if err := ledger.RegisterEvidence(ev); err != nil {
			t.Fatal(err)
		}
	}
	steps := []struct {
		rev      uint64
		to       CandidacyStage
		evidence []string
	}{
		{1, StageScreening, []string{"ev-screen-1"}},
		{2, StageInterview, []string{"ev-interview-1"}},
		{3, StageOffer, []string{"ev-assess-1"}},
		{4, StageHired, nil},
	}
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, stageDecision(t, "cand-1", 1, StageScreening))); err != nil {
		t.Fatal(err)
	}
	// The assessor scores the stage under evaluation; the later offer
	// binds this locked score.
	if err := ledger.RecordScore(ScoreCmd{CandidacyID: "cand-1", Stage: StageScreening, Assessor: "panel-1", Score: 82, EffectiveAt: recruitInstant(t, 9), KnownAt: recruitKnown(t, 9)}); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps[1:] {
		if step.to == StageOffer {
			if got := ledger.ScoreHistory("cand-1", StageScreening, "panel-1"); len(got) != 1 || got[0].Score != 82 {
				t.Fatalf("offer score = %+v", got)
			}
		}
		if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", step.rev, step.to, step.evidence, stageDecision(t, "cand-1", step.rev, step.to))); err != nil {
			t.Fatal(err)
		}
	}
	wantKinds := []string{
		"REQUISITION_OPENED", "POSTING_CREATED", "POSTING_PUBLISHED",
		"APPLICATION_SUBMITTED", "CANDIDACY_CREATED",
		"CANDIDACY_ADVANCED", "CANDIDACY_ADVANCED", "CANDIDACY_ADVANCED", "CANDIDACY_HIRED",
	}
	gotKinds := ledger.agg.EventKinds()
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("event kinds = %v", gotKinds)
	}
	for i := range wantKinds {
		if gotKinds[i] != wantKinds[i] {
			t.Fatalf("event kinds = %v", gotKinds)
		}
	}
	frontier, ok := ledger.Frontier("cand-1")
	if !ok || frontier.Stage != StageHired || frontier.CandidacyRevision != 5 {
		t.Fatalf("golden frontier = %+v ok=%v", frontier, ok)
	}
	const wantDigest = "sha256:1b489261844a6c8c176b28529f13d132dcaf026e99bb9214e7e7784a6f9088d5"
	if got := ledger.FrontierDigest("cand-1"); got != wantDigest {
		t.Fatalf("frontier digest = %q want %q", got, wantDigest)
	}
}

// TestTodo_RECRUIT_002_Race: eight concurrent advances from one revision
// elect exactly one winner with no lost or duplicated stage effect.
func TestTodo_RECRUIT_002_Race(t *testing.T) {
	ledger := openStageLedger(t)
	mustRegisterScreening(t, ledger)
	const racers = 8
	results := make(chan error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, stageDecision(t, "cand-1", 1, StageScreening)))
		}()
	}
	wg.Wait()
	close(results)
	var won, conflicted int
	for result := range results {
		switch {
		case result == nil:
			won++
		case codeOf(result) == CodeStageConflict:
			conflicted++
		default:
			t.Fatalf("racer err = %v", result)
		}
	}
	if won != 1 || conflicted != racers-1 {
		t.Fatalf("racers won=%d conflicted=%d", won, conflicted)
	}
	advanced := 0
	for _, event := range ledger.agg.Events {
		if event.AggregateID == "cand-1" && event.Kind == "CANDIDACY_ADVANCED" {
			advanced++
		}
	}
	if advanced != 1 {
		t.Fatalf("stage effects = %d want exactly one", advanced)
	}
	if frontier, _ := ledger.Frontier("cand-1"); frontier.CandidacyRevision != 2 {
		t.Fatalf("race frontier = %+v", frontier)
	}
}

// TestTodo_RECRUIT_002_Fault: stale, late and unknown inputs reach an
// allowed durable state with zero lost or duplicated effect.
func TestTodo_RECRUIT_002_Fault(t *testing.T) {
	ledger := openStageLedger(t)
	mustRegisterScreening(t, ledger)
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, stageDecision(t, "cand-1", 1, StageScreening))); err != nil {
		t.Fatal(err)
	}
	before, beforeOutbox := ledgerCounts(ledger)

	// A stale expected revision loses CAS and changes nothing.
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, stageDecision(t, "cand-1", 1, StageScreening))); codeOf(err) != CodeStageConflict {
		t.Fatalf("stale advance err = %v", err)
	}
	// A decision receipt bound to a past revision is a conflict, not a
	// replay: it advances nothing twice.
	stale := stageDecision(t, "cand-1", 1, StageInterview)
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 2, StageInterview, []string{"ev-screen-1"}, stale)); codeOf(err) != CodeMissingStageRequirement {
		t.Fatalf("mismatched decision err = %v", err)
	}
	// An observation for an unknown candidacy is refused outright.
	if err := ledger.RecordObservation(ProviderObservation{
		CandidacyID: "cand-x", Stage: StageScreening, ProviderRef: "provider:assessment",
		Result: "pass", ObservedAt: recruitInstant(t, 10),
	}); codeOf(err) != CodeMissingParent {
		t.Fatalf("unknown observation err = %v", err)
	}
	// A late observation for a superseded stage is recorded as history
	// only: the current frontier does not move.
	if err := ledger.RecordObservation(ProviderObservation{
		CandidacyID: "cand-1", Stage: StageApplied, ProviderRef: "provider:screening",
		Result: "pass", ObservedAt: recruitInstant(t, 10),
	}); err != nil {
		t.Fatal(err)
	}
	if frontier, _ := ledger.Frontier("cand-1"); frontier.Stage != StageScreening || frontier.CandidacyRevision != 2 {
		t.Fatalf("late observation moved the frontier: %+v", frontier)
	}
	if gotEvents, gotOutbox := ledgerCounts(ledger); gotEvents != before || gotOutbox != beforeOutbox {
		t.Fatal("fault inputs appended events/outbox entries")
	}
}

// TestTodo_RECRUIT_002_Security: protected screening evidence and the
// decision roster deny without leaking content, and every denial is
// zero-effect.
func TestTodo_RECRUIT_002_Security(t *testing.T) {
	ledger := openStageLedger(t)
	mustRegisterScreening(t, ledger)
	before, beforeOutbox := ledgerCounts(ledger)

	for _, viewer := range []string{"", "interviewer", "hr-general"} {
		evidence, err := ledger.ViewEvidence(viewer, "ev-screen-1")
		if codeOf(err) != CodeEvidenceProtected {
			t.Fatalf("viewer %q err = %v", viewer, err)
		}
		if evidence != (StageEvidence{}) {
			t.Fatalf("viewer %q received evidence: %+v", viewer, evidence)
		}
		if strings.Contains(err.Error(), "digest:screen-1") || strings.Contains(err.Error(), "hr-confidential") {
			t.Fatalf("denial leaks protected content: %q", err.Error())
		}
	}
	if _, err := ledger.ViewEvidence("hr-confidential", "missing"); codeOf(err) != CodeMissingParent {
		t.Fatalf("unknown evidence err = %v", err)
	}

	// A decider outside the authorized roster cannot advance anyone.
	intruder := stageDecision(t, "cand-1", 1, StageScreening)
	intruder.Decider = "intruder"
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, intruder)); codeOf(err) != CodeDecisionUnauthorized {
		t.Fatalf("intruder advance err = %v", intruder)
	}
	// A provider authority is not a decision even when the decider is
	// otherwise authorized.
	provider := stageDecision(t, "cand-1", 1, StageScreening)
	provider.AuthorityRef = "provider:screening"
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, provider)); codeOf(err) != CodeProviderNotDecision {
		t.Fatalf("provider advance err = %v", provider)
	}
	if gotEvents, gotOutbox := ledgerCounts(ledger); gotEvents != before || gotOutbox != beforeOutbox {
		t.Fatal("denied advances appended events/outbox entries")
	}
}

// TestTodo_RECRUIT_002_Mutation: seeded semantic mutants are killed — a
// provider authority accepted as a decision, a skipped mandatory stage,
// an overwritten score and a duplicated current stage each fail.
func TestTodo_RECRUIT_002_Mutation(t *testing.T) {
	ledger := openStageLedger(t)
	mustRegisterScreening(t, ledger)

	// Mutant "provider authority decides": killed by PROVIDER_NOT_DECISION.
	provider := stageDecision(t, "cand-1", 1, StageScreening)
	provider.AuthorityRef = "provider:screening"
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, provider)); codeOf(err) != CodeProviderNotDecision {
		t.Fatalf("provider-authority mutant survived: %v", err)
	}
	// Mutant "mandatory stages are advisory": killed by STAGE_SKIPPED.
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageOffer, nil, stageDecision(t, "cand-1", 1, StageOffer))); codeOf(err) != CodeStageSkipped {
		t.Fatalf("skip-stage mutant survived: %v", err)
	}
	// Mutant "scores overwrite in place": killed by SCORE_LOCKED.
	score := ScoreCmd{CandidacyID: "cand-1", Stage: StageApplied, Assessor: "panel-1", Score: 60, EffectiveAt: recruitInstant(t, 6), KnownAt: recruitKnown(t, 6)}
	if err := ledger.RecordScore(score); err != nil {
		t.Fatal(err)
	}
	overwrite := score
	overwrite.Score = 61
	if err := ledger.RecordScore(overwrite); codeOf(err) != CodeScoreLocked {
		t.Fatalf("score-overwrite mutant survived: %v", err)
	}
	// Mutant "two stages stay current": killed — the ledger holds exactly
	// one frontier per candidacy across sequential advances.
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, stageDecision(t, "cand-1", 1, StageScreening))); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RegisterEvidence(StageEvidence{EvidenceID: "ev-interview-1", CandidacyID: "cand-1", Kind: EvidenceInterview, ContentDigest: "digest:interview-1", SourceRef: "source:panel"}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 2, StageInterview, []string{"ev-interview-1"}, stageDecision(t, "cand-1", 2, StageInterview))); err != nil {
		t.Fatal(err)
	}
	if n := ledger.FrontierCount("cand-1"); n != 1 {
		t.Fatalf("duplicate-frontier mutant survived: %d current stages", n)
	}
	if frontier, _ := ledger.Frontier("cand-1"); frontier.Stage != StageInterview {
		t.Fatalf("frontier = %+v", frontier)
	}
}
