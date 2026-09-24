package proofing

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var proofingNow = values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))

func proofingSubject() values.EntityRef {
	return values.EntityRef{Tenant: "tenant-1", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
}

func proofingEvidence(t *testing.T, level AssuranceLevel) EvidenceItem {
	t.Helper()
	item, err := NewEvidenceItem(EvidencePassport, "sha256:passport-digest", "sha256:provider-digest", "custody://passport-1", level, proofingNow)
	if err != nil {
		t.Fatalf("NewEvidenceItem: %v", err)
	}
	return item
}

func proofingSession(t *testing.T, target AssuranceLevel, evidenceLevel AssuranceLevel) ProofingSession {
	t.Helper()
	session, err := NewProofingSession("session-1", proofingSubject(), "onboarding", target, []EvidenceItem{proofingEvidence(t, evidenceLevel)}, "verifier-1", values.NewInstant(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewProofingSession: %v", err)
	}
	return session
}

func proofingDate(t *testing.T, year int, month time.Month, day int) values.LocalDate {
	t.Helper()
	d, err := values.NewLocalDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func proofingAuthorization(t *testing.T) WorkAuthorizationEvidence {
	t.Helper()
	evidence, err := NewWorkAuthorizationEvidence(WorkAuthorizationEvidence{
		EvidenceID: "authorization-1", Subject: proofingSubject(), Revision: 1,
		DocumentClass: DocumentWorkPermit, VerificationMethod: VerificationGovernmentSource,
		Jurisdiction: "US-NY", Category: "EMPLOYMENT", ValidFrom: proofingDate(t, 2026, time.January, 1),
		ValidUntil: proofingDate(t, 2027, time.January, 1), ReverificationDue: proofingDate(t, 2026, time.October, 1),
		EvidenceDigest: "sha256:work-permit", SourceRef: "provider-observation-1",
	})
	if err != nil {
		t.Fatalf("NewWorkAuthorizationEvidence: %v", err)
	}
	return evidence
}

func TestIdentityProofAndWorkAuthorizationRequireScopedEvidenceAssuranceAndExpiry(t *testing.T) {
	session := proofingSession(t, AssuranceIAL2, AssuranceIAL2)
	verified, err := session.RecordOutcome(OutcomeVerified)
	if err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	if verified.Revision != 2 || verified.SupersedesRevision != 1 || verified.Outcome != OutcomeVerified {
		t.Fatalf("unexpected immutable successor: %+v", verified)
	}
	if session.Outcome != OutcomeUnknown {
		t.Fatalf("original session was mutated: %+v", session)
	}
	expired, err := verified.Evaluate(values.NewInstant(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)))
	if err != nil || expired.Outcome != OutcomeExpired {
		t.Fatalf("Evaluate expiry: got=%+v err=%v", expired, err)
	}
	authorization := proofingAuthorization(t)
	result, err := NewWorkAuthorizationResult(WorkAuthorizationResult{
		ResultID: "result-1", Subject: proofingSubject(), EvidenceRevisionDigest: authorization.CanonicalDigest,
		Jurisdiction: authorization.Jurisdiction, Category: authorization.Category, EffectiveFrom: authorization.ValidFrom,
		EffectiveUntil: authorization.ValidUntil, Decision: DecisionAuthorized,
	})
	if err != nil {
		t.Fatalf("NewWorkAuthorizationResult: %v", err)
	}
	if _, err := result.Explain(); err != nil {
		t.Fatalf("Explain: %v", err)
	}
}

func TestTodo_PROOF_001_Property(t *testing.T) {
	left := proofingSession(t, AssuranceIAL2, AssuranceIAL2)
	right := proofingSession(t, AssuranceIAL2, AssuranceIAL2)
	if left.CanonicalDigest != right.CanonicalDigest {
		t.Fatalf("equal semantic sessions must have equal digest: %s != %s", left.CanonicalDigest, right.CanonicalDigest)
	}
	copyOf := append([]EvidenceItem(nil), left.Evidence...)
	copyOf[0] = left.Evidence[0]
	copyOf[0].EvidenceDigest = "sha256:changed"
	if left.Evidence[0].EvidenceDigest == copyOf[0].EvidenceDigest {
		t.Fatalf("evidence slice was not detached")
	}
}

func TestTodo_PROOF_001_Golden(t *testing.T) {
	session := proofingSession(t, AssuranceIAL2, AssuranceIAL2)
	if !strings.HasPrefix(session.CanonicalDigest, "sha256:") || len(session.Canonical()) == 0 {
		t.Fatalf("missing canonical proofing session digest")
	}
	if got := canonicalbytes.Digest(session.Canonical()); got != session.CanonicalDigest {
		t.Fatalf("canonical digest mismatch: got %s want %s", got, session.CanonicalDigest)
	}
}

func TestTodo_PROOF_001_Race(t *testing.T) {
	store := NewInMemorySessionStore()
	session := proofingSession(t, AssuranceIAL1, AssuranceIAL1)
	if err := store.Put(session); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wait sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got, ok := store.Get(session.SessionID)
			if !ok {
				errs <- errors.New("concurrent session read missed stored session")
				return
			}
			if got.CanonicalDigest != session.CanonicalDigest {
				errs <- errors.New("concurrent session read changed canonical digest")
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestTodo_PROOF_001_Fault(t *testing.T) {
	bad := proofingSession(t, AssuranceIAL2, AssuranceIAL2)
	bad.Subject = values.EntityRef{}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "subject") {
		t.Fatalf("subject fault: %v", err)
	}
	if _, err := proofingSession(t, AssuranceIAL3, AssuranceIAL2).RecordOutcome(OutcomeVerified); !errors.Is(err, ErrUnsupportedAssurance) {
		t.Fatalf("unsupported assurance: %v", err)
	}
}

func TestTodo_PROOF_001_Security(t *testing.T) {
	item := proofingEvidence(t, AssuranceIAL2)
	if strings.Contains(string(item.Canonical()), "document image") || strings.Contains(string(item.Canonical()), "raw") {
		t.Fatal("evidence canonical form exposes a protected payload marker")
	}
	if _, err := NewEvidenceItem(EvidenceKind("RAW_DOCUMENT_IMAGE"), "sha256:x", "", "custody://x", AssuranceIAL1, proofingNow); err == nil {
		t.Fatal("closed evidence vocabulary accepted raw document image kind")
	}
}

func TestTodo_PROOF_001_Conformance(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version = %d", Version())
	}
	if _, err := Explain(proofingSession(t, AssuranceIAL1, AssuranceIAL1)); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PROOF_001_Mutation(t *testing.T) {
	evidence := proofingAuthorization(t)
	next := evidence
	next.Revision = 2
	next.SupersedesRevision = 1
	next.Category = "VOLUNTEER"
	child, err := evidence.Successor(next)
	if err != nil || child.CanonicalDigest == evidence.CanonicalDigest {
		t.Fatalf("successor mutation did not create a new digest: child=%+v err=%v", child, err)
	}
	bad := child
	bad.SupersedesRevision = 0
	if _, err := evidence.Successor(bad); !errors.Is(err, ErrRevisionLineage) {
		t.Fatalf("lineage mutation: %v", err)
	}
}
