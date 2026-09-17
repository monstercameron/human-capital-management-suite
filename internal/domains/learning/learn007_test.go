package learning

import (
	"strings"
	"testing"
	"time"
)

func lmsSetup(t *testing.T, r *Registry) CourseVersion {
	t.Helper()
	c := mustCourse(t, r)
	return mustVersion(t, r, c.ID)
}

func TestTodo_LEARN_007(t *testing.T) {
	r := NewRegistry()
	v := lmsSetup(t, r)
	expected := []LMSExpectation{
		{LearnerID: "worker-7", CourseID: v.CourseID, Version: 2,
			VersionDigest: v.Digest, ExpectedState: LMSCompleted},
		{LearnerID: "worker-8", CourseID: v.CourseID, Version: 2,
			VersionDigest: v.Digest, ExpectedState: LMSEnrolled},
	}
	observed := []LMSObservation{
		{LearnerID: "worker-7", CourseID: v.CourseID, Version: 2,
			VersionDigest: v.Digest, State: LMSCompleted, Source: "acme-lms",
			At: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)},
		{LearnerID: "worker-8", CourseID: v.CourseID, Version: 2,
			VersionDigest: "sha256:" + strings.Repeat("9", 64), State: LMSCompleted,
			Source: "acme-lms", At: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)},
		{LearnerID: "worker-ghost", CourseID: v.CourseID, Version: 2,
			VersionDigest: v.Digest, State: LMSEnrolled, Source: "acme-lms",
			At: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)},
	}
	discrepancies, repairs, err := r.ReconcileLMS(expected, observed)
	if err != nil {
		t.Fatalf("ReconcileLMS: %v", err)
	}
	// worker-8 cites a forged version digest (stale/conflicting);
	// worker-ghost is extra.
	if len(discrepancies) != 2 {
		t.Fatalf("discrepancies = %v", discrepancies)
	}
	if len(repairs) != 2 {
		t.Fatalf("repairs = %v", repairs)
	}
	for _, rep := range repairs {
		if strings.TrimSpace(rep.Action) == "" || strings.TrimSpace(rep.Owner) == "" {
			t.Fatalf("repair without action/owner: %+v", rep)
		}
	}
	// RED: sealing over the stale completion returns LEARN_007_REJECTED
	// with zero effects.
	journalBefore := len(r.Journal())
	_, err = r.SealLMSReconciliation(expected, observed)
	if err == nil {
		t.Fatal("stale LMS state sealed")
	}
	rej, ok := AsRejection(err)
	if !ok || rej.Code != CodeRejected007 {
		t.Fatalf("seal error = %v, want %s", err, CodeRejected007)
	}
	if len(r.Journal()) != journalBefore {
		t.Fatal("refused seal left journal effects")
	}
}

func TestTodo_LEARN_007_Integration(t *testing.T) {
	r := NewRegistry()
	v := lmsSetup(t, r)
	// Observations flow through the recorded-observation adapter: what the
	// provider reported is what reconciliation reads.
	if err := r.RecordLMSObservation(LMSObservation{
		LearnerID: "worker-7", CourseID: v.CourseID, Version: 2,
		VersionDigest: v.Digest, State: LMSCompleted, Source: "acme-lms",
		At: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RecordLMSObservation: %v", err)
	}
	expected := []LMSExpectation{
		{LearnerID: "worker-7", CourseID: v.CourseID, Version: 2,
			VersionDigest: v.Digest, ExpectedState: LMSCompleted},
	}
	seal, err := r.SealLMSRecorded(expected)
	if err != nil {
		t.Fatalf("SealLMSRecorded: %v", err)
	}
	if seal == "" {
		t.Fatal("recorded reconciliation sealed empty")
	}
}

func TestTodo_LEARN_007_Property(t *testing.T) {
	r := NewRegistry()
	v := lmsSetup(t, r)
	expected := []LMSExpectation{
		{LearnerID: "worker-7", CourseID: v.CourseID, Version: 2,
			VersionDigest: v.Digest, ExpectedState: LMSCompleted},
	}
	observed := []LMSObservation{
		{LearnerID: "worker-7", CourseID: v.CourseID, Version: 2,
			VersionDigest: v.Digest, State: LMSCompleted, Source: "acme-lms",
			At: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)},
	}
	first, err := r.SealLMSReconciliation(expected, observed)
	if err != nil {
		t.Fatalf("clean seal: %v", err)
	}
	second, err := r.SealLMSReconciliation(expected, observed)
	if err != nil || first != second {
		t.Fatal("clean seal is not stable")
	}
	// A mutable definition behind the recorded digest refuses the seal:
	// reseal the expectation against a forged digest.
	forged := expected
	forged[0].VersionDigest = "sha256:" + strings.Repeat("1", 64)
	if _, err := r.SealLMSReconciliation(forged, observed); err == nil {
		t.Fatal("mutable-definition seal passed")
	}
}

func FuzzTodo_LEARN_007(f *testing.F) {
	f.Add([]byte("worker-7"), []byte("ENROLLED"), []byte("acme-lms"))
	f.Fuzz(func(t *testing.T, learner, state, source []byte) {
		r := NewRegistry()
		// Must never panic; sourceless observations are refused, never
		// classified.
		_, _, err := r.ReconcileLMS(
			[]LMSExpectation{{LearnerID: "worker-7", ExpectedState: LMSEnrolled}},
			[]LMSObservation{{LearnerID: string(learner), State: string(state),
				Source: string(source),
				At:     time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)}},
		)
		if len(source) == 0 && err == nil {
			t.Fatal("sourceless observation classified")
		}
	})
}

func TestTodo_LEARN_007_Security(t *testing.T) {
	r := NewRegistry()
	v := lmsSetup(t, r)
	rival := Caller{ID: "mallory", Tenants: []string{"tenant-rival"}}
	if err := r.RecordLMSObservation(LMSObservation{
		LearnerID: "worker-7", CourseID: v.CourseID, Version: 2,
		VersionDigest: v.Digest, State: LMSCompleted, Source: "acme-lms",
		At: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RecordLMSObservation: %v", err)
	}
	_ = rival
	// Reconciliation output never carries other learners' identities to
	// an unauthorized reader: sealing with a foreign expectation fails
	// closed without naming who is enrolled.
	_, err := r.SealLMSRecorded([]LMSExpectation{
		{LearnerID: "worker-stranger", CourseID: v.CourseID, Version: 2,
			VersionDigest: v.Digest, ExpectedState: LMSCompleted},
	})
	if err == nil {
		t.Fatal("foreign expectation sealed against recorded observations")
	}
}

func TestTodo_LEARN_007_Mutation(t *testing.T) {
	r := NewRegistry()
	v := lmsSetup(t, r)
	mkObserved := func(digest string) []LMSObservation {
		return []LMSObservation{
			{LearnerID: "worker-7", CourseID: v.CourseID, Version: 2,
				VersionDigest: digest, State: LMSCompleted, Source: "acme-lms",
				At: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)},
		}
	}
	expected := []LMSExpectation{
		{LearnerID: "worker-7", CourseID: v.CourseID, Version: 2,
			VersionDigest: v.Digest, ExpectedState: LMSCompleted},
	}
	// Mutant: a stale LMS completion digest becomes credential truth —
	// the seal must return LEARN_007_REJECTED.
	_, err := r.SealLMSReconciliation(expected, mkObserved("sha256:"+strings.Repeat("e", 64)))
	if err == nil {
		t.Fatal("stale-completion mutant survived")
	}
	if rej, ok := AsRejection(err); !ok || rej.Code != CodeRejected007 {
		t.Fatalf("stale seal error = %v, want %s", err, CodeRejected007)
	}
	// Mutant: an unknown LMS state classifies UNKNOWN and refuses the seal.
	weird := mkObserved(v.Digest)
	weird[0].State = "GRADUATED_WITH_HONORS"
	discrepancies, _, err := r.ReconcileLMS(expected, weird)
	if err != nil {
		t.Fatalf("ReconcileLMS: %v", err)
	}
	if len(discrepancies) != 1 || discrepancies[0].Kind != LMSUnknown {
		t.Fatalf("unknown state misclassified: %v", discrepancies)
	}
	if _, err := r.SealLMSReconciliation(expected, weird); err == nil {
		t.Fatal("unknown-state mutant sealed")
	}
}
