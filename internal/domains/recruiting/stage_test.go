package recruiting

import (
	"strings"
	"sync"
	"testing"
)

// openStageLedger builds the governed fixture: an open requisition,
// published posting, submitted application and one APPLIED candidacy,
// wrapped in a StageLedger whose only authorized decider is
// hiring-manager-1.
func openStageLedger(t *testing.T) *StageLedger {
	t.Helper()
	aggregate := openRecruiting(t)
	aggregate = submitRecruiting(t, aggregate, "app-1", "candidate-1")
	if err := aggregate.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewStageLedger(&aggregate, []string{"hiring-manager-1"})
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func stageDecision(t *testing.T, candidacyID string, rev uint64, to CandidacyStage) StageDecision {
	t.Helper()
	return StageDecision{
		Decider: "hiring-manager-1", AuthorityRef: "authority:hire-decision",
		CandidacyID: candidacyID, CandidacyRevision: rev, ToStage: to,
		DecidedAt: recruitInstant(t, 8),
	}
}

func advanceCmd(t *testing.T, candidacyID string, rev uint64, to CandidacyStage, evidence []string, decision StageDecision) AdvanceCmd {
	t.Helper()
	return AdvanceCmd{
		RequisitionID: "req-1", RequisitionRevision: 1,
		CandidacyID: candidacyID, ExpectedCandidacyRevision: rev, ToStage: to,
		EvidenceIDs: evidence, Decision: decision,
		EffectiveAt: recruitInstant(t, 9), KnownAt: recruitKnown(t, 9),
	}
}

func ledgerCounts(ledger *StageLedger) (events, outbox int) {
	return len(ledger.agg.Events), len(ledger.agg.Outbox)
}

func mustRegisterScreening(t *testing.T, ledger *StageLedger) {
	t.Helper()
	if err := ledger.RegisterEvidence(StageEvidence{
		EvidenceID: "ev-screen-1", CandidacyID: "cand-1", Kind: EvidenceScreening,
		Protected: true, RequiredClearance: "hr-confidential",
		ContentDigest: "digest:screen-1", SourceRef: "source:screening-provider",
	}); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_RECRUIT_002: a candidacy advances only through its mandatory
// stages with required evidence and an authorized decision receipt.
// Skipping a stage, changing a submitted score, exposing protected
// screening evidence, advancing on provider acceptance and concurrent
// decisions are all refused with typed codes and zero appended events.
func TestCandidacyStageTransitionBindsRequirementsEvidenceAndDecisionAuthority(t *testing.T) {
	ledger := openStageLedger(t)
	mustRegisterScreening(t, ledger)

	// A mandatory stage cannot be skipped: APPLIED must go to SCREENING.
	events, outbox := ledgerCounts(ledger)
	err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageInterview, nil, stageDecision(t, "cand-1", 1, StageInterview)))
	if codeOf(err) != CodeStageSkipped {
		t.Fatalf("stage skip err = %v", err)
	}
	if gotEvents, gotOutbox := ledgerCounts(ledger); gotEvents != events || gotOutbox != outbox {
		t.Fatal("skipped stage appended events/outbox entries")
	}

	// The governed advance binds revisions, evidence and decision, and
	// appends exactly one stage event with a single current frontier.
	before, beforeOutbox := ledgerCounts(ledger)
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 1, StageScreening, []string{"ev-screen-1"}, stageDecision(t, "cand-1", 1, StageScreening))); err != nil {
		t.Fatal(err)
	}
	if gotEvents, gotOutbox := ledgerCounts(ledger); gotEvents != before+1 || gotOutbox != beforeOutbox+1 {
		t.Fatalf("governed advance events=%d outbox=%d", gotEvents, gotOutbox)
	}
	frontier, ok := ledger.Frontier("cand-1")
	if !ok || frontier.Stage != StageScreening || frontier.CandidacyRevision != 2 {
		t.Fatalf("frontier = %+v ok=%v", frontier, ok)
	}

	// A submitted score is immutable: recording a different score for
	// the same candidacy, stage and assessor is refused.
	scoreAt := ScoreCmd{CandidacyID: "cand-1", Stage: StageScreening, Assessor: "panel-1", Score: 82, EffectiveAt: recruitInstant(t, 9), KnownAt: recruitKnown(t, 9)}
	if err := ledger.RecordScore(scoreAt); err != nil {
		t.Fatal(err)
	}
	changed := scoreAt
	changed.Score = 90
	if err := ledger.RecordScore(changed); codeOf(err) != CodeScoreLocked {
		t.Fatalf("score change err = %v", err)
	}
	if err := ledger.CorrectScore("cand-1", StageScreening, "panel-1", 90, recruitInstant(t, 10), recruitKnown(t, 10)); err != nil {
		t.Fatal(err)
	}
	history := ledger.ScoreHistory("cand-1", StageScreening, "panel-1")
	if len(history) != 2 || history[0].Score != 82 || history[1].Score != 90 || history[0].Revision+1 != history[1].Revision {
		t.Fatalf("score history = %+v", history)
	}

	// Protected screening evidence is not exposed without clearance.
	if _, err := ledger.ViewEvidence("interviewer", "ev-screen-1"); codeOf(err) != CodeEvidenceProtected {
		t.Fatalf("protected evidence err = %v", err)
	}
	viewed, err := ledger.ViewEvidence("hr-confidential", "ev-screen-1")
	if err != nil || viewed.EvidenceID != "ev-screen-1" {
		t.Fatalf("authorized view = %+v err = %v", viewed, err)
	}

	// Provider acceptance is an observation, never a decision: advancing
	// on a provider authority is refused.
	before, beforeOutbox = ledgerCounts(ledger)
	provider := stageDecision(t, "cand-1", 2, StageInterview)
	provider.AuthorityRef = "provider:background-check"
	if err := ledger.RegisterEvidence(StageEvidence{
		EvidenceID: "ev-interview-1", CandidacyID: "cand-1", Kind: EvidenceInterview,
		ContentDigest: "digest:interview-1", SourceRef: "source:interview-panel",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.AdvanceStage(advanceCmd(t, "cand-1", 2, StageInterview, []string{"ev-interview-1"}, provider)); codeOf(err) != CodeProviderNotDecision {
		t.Fatalf("provider advance err = %v", err)
	}
	if gotEvents, gotOutbox := ledgerCounts(ledger); gotEvents != before || gotOutbox != beforeOutbox {
		t.Fatal("provider acceptance appended events/outbox entries")
	}

	// Concurrent interview decisions create exactly one current stage:
	// one advance wins, the other loses CAS with zero extra effect.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- ledger.AdvanceStage(advanceCmd(t, "cand-1", 2, StageInterview, []string{"ev-interview-1"}, stageDecision(t, "cand-1", 2, StageInterview)))
		}()
	}
	wg.Wait()
	close(results)
	var won, conflicted int
	for result := range results {
		if result == nil {
			won++
		} else if codeOf(result) == CodeStageConflict {
			conflicted++
		} else {
			t.Fatalf("concurrent advance err = %v", result)
		}
	}
	if won != 1 || conflicted != 1 {
		t.Fatalf("concurrent advance won=%d conflicted=%d", won, conflicted)
	}
	frontier, ok = ledger.Frontier("cand-1")
	if !ok || frontier.Stage != StageInterview || frontier.CandidacyRevision != 3 {
		t.Fatalf("concurrent frontier = %+v ok=%v", frontier, ok)
	}
	if gotEvents, _ := ledgerCounts(ledger); gotEvents != before+1 {
		t.Fatalf("concurrent advance events=%d want %d", gotEvents, before+1)
	}
	if strings.Contains(ledger.Explain(), "candidate-1") {
		t.Fatalf("explain leaks candidate identity: %q", ledger.Explain())
	}
}
