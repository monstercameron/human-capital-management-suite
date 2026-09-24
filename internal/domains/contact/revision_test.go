package contact

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var contactNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func contactSubject() values.EntityRef {
	return values.EntityRef{Tenant: "tenant-1", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
}

func contactEndpoint(t *testing.T) ContactEndpointRevision {
	t.Helper()
	e, err := NewContactEndpointRevision(contactSubject(), "email-1", EndpointEmail, "Person@example.com", "account-recovery", 1, "worker-profile")
	if err != nil {
		t.Fatalf("NewContactEndpointRevision: %v", err)
	}
	return e
}

func contactChallenge(t *testing.T, budget int) ContactVerificationChallenge {
	t.Helper()
	c, _, err := IssueContactChallenge("challenge-1", contactSubject(), contactEndpoint(t), "account-recovery", "123456", contactNow, time.Hour, budget)
	if err != nil {
		t.Fatalf("IssueContactChallenge: %v", err)
	}
	return c
}

func TestContactEndpointAndChallengeRejectAmbiguousNormalizationReplayAndScopeMixing(t *testing.T) {
	endpoint := contactEndpoint(t)
	if endpoint.NormalizedValueDigest == "" || endpoint.DisplayHint != "p****n@example.com" {
		t.Fatalf("digest-backed endpoint is incomplete: %+v", endpoint)
	}
	verified, err := endpoint.MarkVerified()
	if err != nil || verified.Verification != Verified || verified.Revision != 2 {
		t.Fatalf("verification successor: %+v err=%v", verified, err)
	}
	challenge := contactChallenge(t, 3)
	answered, status, err := challenge.Respond("123456", contactNow.Add(5*time.Minute))
	if err != nil || status != ContactChallengeVerified || answered.Status != ContactChallengeVerified {
		t.Fatalf("challenge response: status=%s err=%v", status, err)
	}
	replay, status, err := answered.Respond("123456", contactNow.Add(6*time.Minute))
	if err != nil || replay.Status != ContactChallengeVerified || status != ContactChallengeVerified {
		t.Fatalf("replay changed terminal challenge: status=%s err=%v", status, err)
	}
	if len(answered.Events) != 3 || answered.Events[1].AnswerDigest == "" || answered.Events[1].AnswerDigest == "123456" {
		t.Fatalf("challenge answer was not digested: %+v", answered.Events)
	}
}

func TestTodo_CONTACT_001_Property(t *testing.T) {
	left := contactEndpoint(t)
	right := contactEndpoint(t)
	if left.CanonicalDigest != right.CanonicalDigest || left.NormalizedValueDigest != right.NormalizedValueDigest {
		t.Fatal("equal endpoint inputs must have stable digests")
	}
	if strings.Contains(string(left.Canonical()), "Person@example.com") {
		t.Fatal("canonical endpoint stores normalized value instead of its digest")
	}
}

func TestTodo_CONTACT_001_Golden(t *testing.T) {
	endpoint := contactEndpoint(t)
	if canonicalbytes.Digest(endpoint.Canonical()) != endpoint.CanonicalDigest {
		t.Fatal("endpoint canonical digest does not match canonical bytes")
	}
	challenge := contactChallenge(t, 2)
	if canonicalbytes.Digest(challenge.Canonical()) != challenge.CanonicalDigest {
		t.Fatal("challenge canonical digest does not match canonical bytes")
	}
}

func TestTodo_CONTACT_001_Race(t *testing.T) {
	store := NewInMemoryChallengeStore()
	challenge := contactChallenge(t, 1)
	const workers = 16
	var wait sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := store.Put(challenge); err != nil {
				errs <- err
				return
			}
			if _, ok := store.Get(challenge.ChallengeID); !ok {
				errs <- errors.New("concurrent challenge write was not readable")
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestTodo_CONTACT_001_Fault(t *testing.T) {
	if _, err := NewContactEndpointRevision(contactSubject(), "email-1", EndpointEmail, " person@example.com", "purpose", 1, "source"); !errors.Is(err, ErrAmbiguousNormalization) {
		t.Fatalf("ambiguous endpoint: %v", err)
	}
	if _, _, err := IssueContactChallenge("challenge", contactSubject(), contactEndpoint(t), "purpose", "", contactNow, time.Hour, 1); !errors.Is(err, ErrChallengeTokenRequired) {
		t.Fatalf("missing token: %v", err)
	}
}

func TestTodo_CONTACT_001_Security(t *testing.T) {
	challenge := contactChallenge(t, 2)
	if strings.Contains(string(challenge.Canonical()), "123456") {
		t.Fatal("challenge canonical form contains plaintext code")
	}
	wrong, status, err := challenge.Respond("654321", contactNow.Add(time.Minute))
	if err != nil || status != ContactChallengeIssued || wrong.Attempts != 1 {
		t.Fatalf("wrong answer should consume one attempt: %+v status=%s err=%v", wrong, status, err)
	}
	if wrong.Events[1].AnswerDigest == "654321" {
		t.Fatal("wrong answer was stored plaintext")
	}
}

func TestTodo_CONTACT_001_Conformance(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version = %d", Version())
	}
	if err := contactEndpoint(t).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CONTACT_001_Mutation(t *testing.T) {
	challenge := contactChallenge(t, 2)
	first, status, err := challenge.Respond("wrong", contactNow.Add(time.Minute))
	if err != nil || status != ContactChallengeIssued {
		t.Fatalf("first wrong answer: status=%s err=%v", status, err)
	}
	second, status, err := first.Respond("wrong-again", contactNow.Add(2*time.Minute))
	if err != nil || status != ContactChallengeExhausted || second.Status != ContactChallengeExhausted {
		t.Fatalf("budget exhaustion: status=%s err=%v challenge=%+v", status, err, second)
	}
	if challenge.Attempts != 0 || challenge.Status != ContactChallengeIssued {
		t.Fatal("answer mutated original challenge")
	}
}
