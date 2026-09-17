// CASE-003 RED: assignment, SLA, evidence review, finding and disposition.
// Tests are written before the production code; they must fail until
// resolution.go provides the named symbols.
package hrcase

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func case003At() time.Time { return time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC) }

func case003Eligible() []EligibleAssignee {
	return []EligibleAssignee{
		{Principal: "principal:manager", Role: "CASE_MANAGER"},
		{Principal: "principal:investigator", Role: "INVESTIGATOR"},
		{Principal: "principal:investigator-2", Role: "INVESTIGATOR"},
		{Principal: "principal:counsel", Role: "LEGAL"},
	}
}

// case003Ready builds a resolution through assignment, SLA and one approved
// evidence review, leaving finding and obligations to the caller.
func case003Ready(t *testing.T) *Resolution {
	t.Helper()
	at := case003At()
	r, err := NewResolution("case-003", case003Eligible())
	if err != nil {
		t.Fatalf("NewResolution: %v", err)
	}
	if _, err := r.Assign("principal:investigator", at); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	sla := CaseSLA{
		CalendarID:      "cal:hr-er",
		CalendarVersion: "2026.03",
		Timezone:        "America/Chicago",
		OpenedAt:        at,
		DueAt:           at.Add(10 * 24 * time.Hour),
	}
	if err := r.SetSLA(sla); err != nil {
		t.Fatalf("SetSLA: %v", err)
	}
	rev := EvidenceReview{EvidenceID: "ev-1", ArtifactDigest: "sha256:artifact-1", Reviewer: "principal:investigator", Approved: true, At: at}
	if err := r.RecordReview(rev); err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
	return r
}

func case003Finding() CaseFinding {
	return CaseFinding{ID: "finding-1", EvidenceDigests: []string{"sha256:artifact-1"}, Rationale: "preponderance of reviewed evidence"}
}

// TestCaseResolutionRequiresEligibleAssigneeCompleteObligationsAndBoundFinding
// is the PRIMARY contract: close succeeds once only when the assignee is
// eligible, the SLA is explicit, the finding binds reviewed evidence and all
// mandatory obligations are satisfied.
func TestCaseResolutionRequiresEligibleAssigneeCompleteObligationsAndBoundFinding(t *testing.T) {
	at := case003At()
	r, err := NewResolution("case-003", case003Eligible())
	if err != nil {
		t.Fatalf("NewResolution: %v", err)
	}

	// Conflicted investigator cannot be assigned.
	r.Recuse("principal:investigator")
	if _, err := r.Assign("principal:investigator", at); !errors.Is(err, ErrAssignmentIneligible) {
		t.Fatalf("recused assign err=%v, want ErrAssignmentIneligible", err)
	}
	// Unknown principal cannot be assigned either.
	if _, err := r.Assign("principal:stranger", at); !errors.Is(err, ErrAssignmentIneligible) {
		t.Fatalf("unknown assign err=%v, want ErrAssignmentIneligible", err)
	}

	// Ambient SLA (no timezone/calendar) is refused.
	ambient := CaseSLA{OpenedAt: at, DueAt: at.Add(24 * time.Hour)}
	if err := r.SetSLA(ambient); !errors.Is(err, ErrSLAInvalid) {
		t.Fatalf("ambient SLA err=%v, want ErrSLAInvalid", err)
	}

	// Eligible assignment with an explicit SLA succeeds.
	a, err := r.Assign("principal:investigator-2", at)
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if a.Assignee != "principal:investigator-2" || a.Role != "INVESTIGATOR" || a.Seq != 1 {
		t.Fatalf("assignment = %+v", a)
	}
	sla := CaseSLA{CalendarID: "cal:hr-er", CalendarVersion: "2026.03", Timezone: "America/Chicago", OpenedAt: at, DueAt: at.Add(10 * 24 * time.Hour)}
	if err := r.SetSLA(sla); err != nil {
		t.Fatalf("SetSLA: %v", err)
	}

	// Finding that cites unreviewed evidence is refused.
	if err := r.SetFinding(case003Finding()); !errors.Is(err, ErrEvidenceUnreviewed) {
		t.Fatalf("unreviewed finding err=%v, want ErrEvidenceUnreviewed", err)
	}
	if err := r.RecordReview(EvidenceReview{EvidenceID: "ev-1", ArtifactDigest: "sha256:artifact-1", Reviewer: "principal:investigator-2", Approved: true, At: at}); err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
	if err := r.SetFinding(case003Finding()); err != nil {
		t.Fatalf("SetFinding: %v", err)
	}

	// Mandatory legal task blocks close.
	if err := r.AddObligation(Obligation{ID: "ob-legal", Kind: "LEGAL_REVIEW", Mandatory: true}); err != nil {
		t.Fatalf("AddObligation: %v", err)
	}
	if err := r.ApproveFinding("approval:board-1"); err != nil {
		t.Fatalf("ApproveFinding: %v", err)
	}
	if _, err := r.Close("SUSTAINED", "approval:board-1", 0, at); !errors.Is(err, ErrObligationOpen) {
		t.Fatalf("close with open obligation err=%v, want ErrObligationOpen", err)
	}
	if err := r.SatisfyObligation("ob-legal"); err != nil {
		t.Fatalf("SatisfyObligation: %v", err)
	}

	// Reassignment preserves evidence reviews.
	if _, err := r.Assign("principal:manager", at); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if got := r.Reviews(); len(got) != 1 || got[0].ArtifactDigest != "sha256:artifact-1" {
		t.Fatalf("reviews after reassignment = %+v", got)
	}

	// Finding is immutable once approved.
	mutated := case003Finding()
	mutated.Rationale = "rewritten after approval"
	if err := r.SetFinding(mutated); !errors.Is(err, ErrFindingImmutable) {
		t.Fatalf("post-approval finding err=%v, want ErrFindingImmutable", err)
	}

	// First closer wins; the second close with the same CAS sequence fails.
	d, err := r.Close("SUSTAINED", "approval:board-1", 0, at)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if d.Outcome != "SUSTAINED" || d.FindingID != "finding-1" || d.ApprovalRef != "approval:board-1" || d.Seq != 1 {
		t.Fatalf("disposition = %+v", d)
	}
	if _, err := r.Close("SUSTAINED", "approval:board-1", 0, at); !errors.Is(err, ErrStaleClose) {
		t.Fatalf("duplicate close err=%v, want ErrStaleClose", err)
	}
}

// TestTodo_CASE_003_Property proves assignment and SLA rules are total over
// the eligibility roster and that pause extensions stay monotonic.
func TestTodo_CASE_003_Property(t *testing.T) {
	at := case003At()
	for _, recused := range []string{"", "principal:investigator"} {
		r, err := NewResolution("case-003", case003Eligible())
		if err != nil {
			t.Fatalf("NewResolution: %v", err)
		}
		if recused != "" {
			r.Recuse(recused)
		}
		for _, e := range case003Eligible() {
			_, err := r.Assign(e.Principal, at)
			if e.Principal == recused {
				if !errors.Is(err, ErrAssignmentIneligible) {
					t.Fatalf("recused %s err=%v, want ineligible", e.Principal, err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("eligible %s: %v", e.Principal, err)
			}
		}
		if _, err := r.Assign("principal:nobody", at); !errors.Is(err, ErrAssignmentIneligible) {
			t.Fatalf("unknown err=%v, want ineligible", err)
		}
	}

	r := case003Ready(t)
	before, _ := r.SLA()
	for i, pause := range []time.Duration{time.Hour, 24 * time.Hour, 7 * 24 * time.Hour} {
		from := at.Add(time.Duration(i) * 24 * time.Hour)
		if err := r.PauseSLA("pause reason", from, from.Add(pause)); err != nil {
			t.Fatalf("PauseSLA: %v", err)
		}
		after, _ := r.SLA()
		if !after.DueAt.Equal(before.DueAt.Add(pause)) {
			t.Fatalf("pause %d: due=%v want %v", i, after.DueAt, before.DueAt.Add(pause))
		}
		if len(after.Pauses) != i+1 {
			t.Fatalf("pause %d: reasons=%d", i, len(after.Pauses))
		}
		before = after
	}

	// Determinism: two identical constructions share one digest.
	s, _ := NewResolution("case-003", case003Eligible())
	if _, err := s.Assign("principal:investigator", at); err != nil {
		t.Fatal(err)
	}
	again, _ := NewResolution("case-003", case003Eligible())
	if _, err := again.Assign("principal:investigator", at); err != nil {
		t.Fatal(err)
	}
	if s.CanonicalDigest() == "" || s.CanonicalDigest() != again.CanonicalDigest() {
		t.Fatal("identical resolutions diverge")
	}
}

// TestTodo_CASE_003_Golden pins the canonical digest of one fixed resolution.
func TestTodo_CASE_003_Golden(t *testing.T) {
	at := case003At()
	r, err := NewResolution("case-003", case003Eligible())
	if err != nil {
		t.Fatalf("NewResolution: %v", err)
	}
	if _, err := r.Assign("principal:investigator", at); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSLA(CaseSLA{CalendarID: "cal:hr-er", CalendarVersion: "2026.03", Timezone: "America/Chicago", OpenedAt: at, DueAt: at.Add(10 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordReview(EvidenceReview{EvidenceID: "ev-1", ArtifactDigest: "sha256:artifact-1", Reviewer: "principal:investigator", Approved: true, At: at}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFinding(case003Finding()); err != nil {
		t.Fatal(err)
	}
	if err := r.ApproveFinding("approval:board-1"); err != nil {
		t.Fatal(err)
	}
	if d, err := r.Close("SUSTAINED", "approval:board-1", 0, at); err != nil {
		t.Fatal(err)
	} else if d.Outcome != "SUSTAINED" {
		t.Fatalf("disposition = %+v", d)
	}
	const golden = "sha256:bc2848c1abd35d2dec192147b49f08126a13b37dd8d3eed592eadef9d7eac3ce"
	if got := r.CanonicalDigest(); got != golden {
		t.Fatalf("digest=%s want golden %s", got, golden)
	}
}

// TestTodo_CASE_003_Race proves concurrent closers produce exactly one
// disposition under go test -race.
func TestTodo_CASE_003_Race(t *testing.T) {
	r := case003Ready(t)
	if err := r.SetFinding(case003Finding()); err != nil {
		t.Fatal(err)
	}
	if err := r.ApproveFinding("approval:board-1"); err != nil {
		t.Fatal(err)
	}
	const closers = 8
	var wg sync.WaitGroup
	wins := make(chan CaseDisposition, closers)
	stale := make(chan error, closers)
	for i := 0; i < closers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := r.Close("SUSTAINED", "approval:board-1", 0, case003At())
			if err != nil {
				if !errors.Is(err, ErrStaleClose) {
					stale <- err
					return
				}
				stale <- nil
				return
			}
			wins <- d
		}()
	}
	wg.Wait()
	close(wins)
	close(stale)
	if len(wins) != 1 {
		t.Fatalf("winners=%d want exactly 1", len(wins))
	}
	for err := range stale {
		if err != nil {
			t.Fatalf("closer err=%v, want ErrStaleClose", err)
		}
	}
	for d := range wins {
		if d.Seq != 1 || d.Outcome != "SUSTAINED" {
			t.Fatalf("winning disposition = %+v", d)
		}
	}
}

// TestTodo_CASE_003_Fault proves failed closes leave zero duplicate effect:
// a stale close changes nothing and the correct sequence still succeeds.
func TestTodo_CASE_003_Fault(t *testing.T) {
	r := case003Ready(t)
	if err := r.SetFinding(case003Finding()); err != nil {
		t.Fatal(err)
	}
	if err := r.ApproveFinding("approval:board-1"); err != nil {
		t.Fatal(err)
	}
	pinned := r.CanonicalDigest()
	if _, err := r.Close("SUSTAINED", "approval:board-1", 7, case003At()); !errors.Is(err, ErrStaleClose) {
		t.Fatalf("stale close err=%v, want ErrStaleClose", err)
	}
	if got := r.CanonicalDigest(); got != pinned {
		t.Fatal("failed close mutated resolution state")
	}
	if _, err := r.Close("SUSTAINED", "approval:board-1", 0, case003At()); err != nil {
		t.Fatalf("recovery close: %v", err)
	}
	closed := r.CanonicalDigest()
	if _, err := r.Close("SUSTAINED", "approval:board-1", 1, case003At()); !errors.Is(err, ErrStaleClose) {
		t.Fatalf("post-close err=%v, want ErrStaleClose", err)
	}
	if got := r.CanonicalDigest(); got != closed {
		t.Fatal("duplicate close mutated closed state")
	}
}

// TestTodo_CASE_003_Security proves assignment refusal is non-disclosing:
// unknown and recused principals receive the identical error with no roster
// leakage.
func TestTodo_CASE_003_Security(t *testing.T) {
	at := case003At()
	r, err := NewResolution("case-003", case003Eligible())
	if err != nil {
		t.Fatal(err)
	}
	r.Recuse("principal:investigator")
	_, unknownErr := r.Assign("principal:stranger", at)
	_, recusedErr := r.Assign("principal:investigator", at)
	if !errors.Is(unknownErr, ErrAssignmentIneligible) || !errors.Is(recusedErr, ErrAssignmentIneligible) {
		t.Fatalf("errs=%v/%v, want identical ErrAssignmentIneligible", unknownErr, recusedErr)
	}
	if unknownErr.Error() != recusedErr.Error() {
		t.Fatal("refusals distinguish unknown from recused")
	}
	for _, secret := range []string{"principal:manager", "principal:counsel", "investigator-2"} {
		if strings.Contains(unknownErr.Error(), secret) {
			t.Fatalf("refusal leaks roster %q", secret)
		}
	}
}

// TestTodo_CASE_003_Mutation asserts the guards that kill each seeded
// semantic-mutant class: dropped recusal, ambient SLA, post-approval finding
// rewrite, skipped obligation gate and duplicated disposition.
func TestTodo_CASE_003_Mutation(t *testing.T) {
	at := case003At()
	// Recusal check removed: recused assignee must still be refused.
	r, _ := NewResolution("case-003", case003Eligible())
	r.Recuse("principal:investigator")
	if _, err := r.Assign("principal:investigator", at); !errors.Is(err, ErrAssignmentIneligible) {
		t.Fatal("recusal mutant survives")
	}
	// Timezone requirement dropped: ambient SLA must still be refused.
	if err := r.SetSLA(CaseSLA{CalendarID: "cal", CalendarVersion: "v", OpenedAt: at, DueAt: at.Add(time.Hour)}); !errors.Is(err, ErrSLAInvalid) {
		t.Fatal("ambient-SLA mutant survives")
	}
	// Post-approval rewrite allowed: finding must stay immutable and intact.
	full := case003Ready(t)
	if err := full.SetFinding(case003Finding()); err != nil {
		t.Fatal(err)
	}
	if err := full.ApproveFinding("approval:board-1"); err != nil {
		t.Fatal(err)
	}
	pinned := full.CanonicalDigest()
	rewritten := case003Finding()
	rewritten.Rationale = "mutant rewrite"
	if err := full.SetFinding(rewritten); !errors.Is(err, ErrFindingImmutable) {
		t.Fatal("finding-rewrite mutant survives")
	}
	if got := full.CanonicalDigest(); got != pinned {
		t.Fatal("finding-rewrite mutant altered state")
	}
	// Obligation gate skipped: open mandatory task must still block close.
	if err := full.AddObligation(Obligation{ID: "ob-legal", Kind: "LEGAL_REVIEW", Mandatory: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := full.Close("SUSTAINED", "approval:board-1", 0, at); !errors.Is(err, ErrObligationOpen) {
		t.Fatal("obligation-skip mutant survives")
	}
	// Single-disposition CAS removed: second close must still fail.
	if err := full.SatisfyObligation("ob-legal"); err != nil {
		t.Fatal(err)
	}
	if _, err := full.Close("SUSTAINED", "approval:board-1", 0, at); err != nil {
		t.Fatal(err)
	}
	if _, err := full.Close("SUSTAINED", "approval:board-1", 1, at); !errors.Is(err, ErrStaleClose) {
		t.Fatal("double-close mutant survives")
	}
}
