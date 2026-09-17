package recruiting

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var ats004At = time.Date(2026, 3, 13, 9, 0, 0, 0, time.UTC)

func ats004Live() CandidacyRecord {
	return CandidacyRecord{
		Tenant: "acme", CandidacyID: "candidacy-7", CandidateID: "candidate-3", ExternalID: "ext-991",
		History: []Transition{
			{Seq: 1, From: CandidacyApplied, To: CandidacyScreening, Reason: "meets minimum qualifications", AuthorityRef: "recruiter:ana", At: ats004At.Add(-72 * time.Hour)},
			{Seq: 2, From: CandidacyScreening, To: CandidacyInterview, Reason: "screen passed", AuthorityRef: "recruiter:ana", At: ats004At.Add(-48 * time.Hour)},
		},
	}
}

func ats004WithdrawCmd() TerminalCommand {
	return TerminalCommand{Kind: CandidacyWithdrawn, Reason: "candidate withdrew", AuthorityRef: "candidate:portal", At: ats004At}
}

// TestATSConformancePreservesTerminalHistoryAndRepairsExternalDrift is
// the RECRUIT-004 PRIMARY contract: append-only fixtures return exact
// terminal and successor states, preserve restricted reasons and
// evidence, quarantine late or ambiguous identity, and create a scoped
// RepairPlan without duplicating Hire or recruiting events.
func TestATSConformancePreservesTerminalHistoryAndRepairsExternalDrift(t *testing.T) {
	withdrawn, err := ApplyTerminal(ats004Live(), ats004WithdrawCmd())
	if err != nil {
		t.Fatalf("ApplyTerminal withdraw: %v", err)
	}
	if withdrawn.Current() != CandidacyWithdrawn || len(withdrawn.History) != 3 {
		t.Fatalf("withdrawal must append terminal history: %+v", withdrawn)
	}
	if withdrawn.History[0].Reason != "meets minimum qualifications" {
		t.Fatalf("terminal transition must preserve history: %+v", withdrawn.History)
	}

	rejected, err := ApplyTerminal(ats004Live(), TerminalCommand{
		Kind: CandidacyRejected, RestrictedReason: "background-check detail",
		Reason: "did not meet requirements", AuthorityRef: "hiring-manager:lee", At: ats004At,
	})
	if err != nil {
		t.Fatalf("ApplyTerminal reject: %v", err)
	}
	ordinary := Render(rejected, RenderOrdinary)
	for _, reason := range ordinary.Reasons {
		if reason == "background-check detail" || len(reason) > 9 && reason[:11] == "restricted:" {
			t.Fatalf("ordinary render leaked restricted reason: %+v", ordinary)
		}
	}
	review := Render(rejected, RenderRestricted)
	found := false
	for _, reason := range review.Reasons {
		if reason == "restricted:background-check detail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("restricted review must preserve the rationale: %+v", review)
	}

	t.Run("terminal candidacy cannot be rewritten", func(t *testing.T) {
		if _, err := ApplyTerminal(withdrawn, TerminalCommand{Kind: CandidacyRejected, Reason: "second look", AuthorityRef: "recruiter:ana", At: ats004At}); !errors.Is(err, ErrATSRejected) {
			t.Fatalf("post-terminal transition must be RECRUIT_004_REJECTED, got %v", err)
		}
	})

	t.Run("correction opens a successor, preserving the terminal record", func(t *testing.T) {
		successor, err := CorrectTerminal(withdrawn, "candidacy-8", "withdrawal entered in error", "recruiter:ana", ats004At)
		if err != nil {
			t.Fatalf("CorrectTerminal: %v", err)
		}
		if successor.SuccessorOf != "candidacy-7" || successor.Current() != CandidacyApplied {
			t.Fatalf("correction must open a successor: %+v", successor)
		}
		if len(withdrawn.History) != 3 || withdrawn.Current() != CandidacyWithdrawn {
			t.Fatalf("terminal record must stay preserved")
		}
	})

	t.Run("late provider event quarantines, never reopens", func(t *testing.T) {
		plan, err := ReconcileExternal(withdrawn,
			map[string]string{"ext-991": "candidacy-7"},
			ExternalObservation{ExternalID: "ext-991", State: CandidacyInterview, At: ats004At.Add(time.Hour)},
			ats004At.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if plan == nil || plan.Kind != "QUARANTINE_LATE_EVENT" {
			t.Fatalf("late event must quarantine: %+v", plan)
		}
		if withdrawn.Current() != CandidacyWithdrawn {
			t.Fatalf("quarantine must not reopen the candidacy")
		}
	})

	t.Run("duplicate external id quarantines, never merges", func(t *testing.T) {
		plan, err := ReconcileExternal(ats004Live(),
			map[string]string{"ext-991": "candidacy-9"},
			ExternalObservation{ExternalID: "ext-991", State: CandidacyInterview, At: ats004At},
			ats004At.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if plan == nil || plan.Kind != "QUARANTINE_IDENTITY" {
			t.Fatalf("duplicate id must quarantine: %+v", plan)
		}
	})

	t.Run("drift creates a repair, never a blind overwrite", func(t *testing.T) {
		plan, err := ReconcileExternal(ats004Live(),
			map[string]string{"ext-991": "candidacy-7"},
			ExternalObservation{ExternalID: "ext-991", State: CandidacyOffered, At: ats004At},
			ats004At.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if plan == nil || plan.Kind != "RECONCILE_DRIFT" {
			t.Fatalf("drift must repair: %+v", plan)
		}
		if ats004Live().Current() != CandidacyInterview {
			t.Fatalf("repair must not overwrite the authoritative record")
		}
	})
}

func TestTodo_RECRUIT_004_Property(t *testing.T) {
	a, err := ApplyTerminal(ats004Live(), ats004WithdrawCmd())
	if err != nil {
		t.Fatal(err)
	}
	b, err := ApplyTerminal(ats004Live(), ats004WithdrawCmd())
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest() != b.Digest() {
		t.Fatalf("identical terminals must seal identically")
	}
	// History order binds the seal: seq is part of every entry.
	shuffled := ats004Live()
	shuffled.History[0], shuffled.History[1] = shuffled.History[1], shuffled.History[0]
	if shuffled.Digest() == ats004Live().Digest() {
		t.Fatalf("history order must bind the seal")
	}
	// Successor chains reference their terminal predecessor.
	successor, err := CorrectTerminal(a, "candidacy-8", "entered in error", "recruiter:ana", ats004At)
	if err != nil {
		t.Fatal(err)
	}
	if successor.SuccessorOf != a.CandidacyID {
		t.Fatalf("successor must reference its predecessor: %+v", successor)
	}
}

func TestTodo_RECRUIT_004_Golden(t *testing.T) {
	got, err := ApplyTerminal(ats004Live(), ats004WithdrawCmd())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(Render(got, RenderOrdinary))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "recruit_004_golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("golden file not checked in yet: %v", err)
	}
	if string(want) != string(raw)+"\n" {
		t.Fatalf("golden mismatch:\n got %s\nwant %s", raw, want)
	}
}

func TestTodo_RECRUIT_004_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ApplyTerminal(ats004Live(), ats004WithdrawCmd())
			if err != nil {
				t.Error(err)
				return
			}
			if got.Current() != CandidacyWithdrawn {
				t.Errorf("concurrent terminal diverged: %+v", got)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_RECRUIT_004_Integration(t *testing.T) {
	// The terminal record composes with observation comparison: the
	// authoritative ATS owner stays correct across the boundary.
	withdrawn, err := ApplyTerminal(ats004Live(), ats004WithdrawCmd())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ReconcileExternal(withdrawn,
		map[string]string{"ext-991": "candidacy-7"},
		ExternalObservation{ExternalID: "ext-991", State: CandidacyWithdrawn, At: ats004At},
		ats004At.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if plan != nil {
		t.Fatalf("agreeing terminal observation needs no repair: %+v", plan)
	}
}

func TestTodo_RECRUIT_004_Fault(t *testing.T) {
	live := ats004Live()
	for name, cmd := range map[string]TerminalCommand{
		"non-terminal": {Kind: CandidacyInterview, Reason: "x", AuthorityRef: "recruiter:ana", At: ats004At},
		"authority":    {Kind: CandidacyWithdrawn, Reason: "x", At: ats004At},
		"instant":      {Kind: CandidacyWithdrawn, Reason: "x", AuthorityRef: "candidate:portal"},
	} {
		if _, err := ApplyTerminal(live, cmd); !errors.Is(err, ErrATSRejected) {
			t.Fatalf("fault %s must be RECRUIT_004_REJECTED", name)
		}
	}
	if _, err := CorrectTerminal(live, "candidacy-8", "oops", "recruiter:ana", ats004At); !errors.Is(err, ErrATSRejected) {
		t.Fatalf("correcting a live candidacy must be RECRUIT_004_REJECTED")
	}
	if _, err := ReconcileExternal(live, nil, ExternalObservation{State: CandidacyInterview}, ats004At); !errors.Is(err, ErrATSRejected) {
		t.Fatalf("id-free observation must be RECRUIT_004_REJECTED")
	}
}

func TestTodo_RECRUIT_004_Security(t *testing.T) {
	rejected, err := ApplyTerminal(ats004Live(), TerminalCommand{
		Kind: CandidacyRejected, RestrictedReason: "medical-adjacent detail",
		Reason: "did not meet requirements", AuthorityRef: "hiring-manager:lee", At: ats004At,
	})
	if err != nil {
		t.Fatal(err)
	}
	ordinary := Render(rejected, RenderOrdinary)
	raw, _ := json.Marshal(ordinary)
	if len(raw) > 0 && containsRestricted(string(raw), "medical-adjacent detail") {
		t.Fatalf("ordinary render must not carry restricted reasons: %s", raw)
	}
}

func containsRestricted(rendered, secret string) bool {
	for i := 0; i+len(secret) <= len(rendered); i++ {
		if rendered[i:i+len(secret)] == secret {
			return true
		}
	}
	return false
}

func TestTodo_RECRUIT_004_Conformance(t *testing.T) {
	// Every terminal kind preserves history and refuses rewrites: the
	// same fixture shape passes for withdrawal, rejection and rescission.
	for _, kind := range []CandidacyState{CandidacyWithdrawn, CandidacyRejected, CandidacyRescinded} {
		terminal, err := ApplyTerminal(ats004Live(), TerminalCommand{
			Kind: kind, Reason: "conformance", RestrictedReason: "conformance-detail",
			AuthorityRef: "recruiter:ana", At: ats004At,
		})
		if err != nil {
			t.Fatalf("kind %s: %v", kind, err)
		}
		if len(terminal.History) != 3 || terminal.Current() != kind {
			t.Fatalf("kind %s must append terminal history: %+v", kind, terminal)
		}
		if _, err := ApplyTerminal(terminal, TerminalCommand{Kind: kind, Reason: "rewrite", AuthorityRef: "recruiter:ana", At: ats004At}); !errors.Is(err, ErrATSRejected) {
			t.Fatalf("kind %s must refuse rewrites", kind)
		}
	}
}

func TestTodo_RECRUIT_004_Mutation(t *testing.T) {
	a, err := ApplyTerminal(ats004Live(), ats004WithdrawCmd())
	if err != nil {
		t.Fatal(err)
	}
	mutated := a
	mutated.History = append([]Transition(nil), a.History...)
	mutated.History[2].Reason = "forged"
	if mutated.Digest() == a.Digest() {
		t.Fatalf("history mutation must move the seal")
	}
	rescinded, err := ApplyTerminal(ats004Live(), TerminalCommand{Kind: CandidacyRescinded, Reason: "offer error", AuthorityRef: "recruiter:ana", At: ats004At})
	if err != nil {
		t.Fatal(err)
	}
	if rescinded.Digest() == a.Digest() {
		t.Fatalf("distinct terminals must seal distinctly")
	}
}
