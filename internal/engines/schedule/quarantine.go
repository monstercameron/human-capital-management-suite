package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrQuarantineInvalid reports a quarantine request that cannot be
	// recorded: an invalid firing, a blank reason or failure, or a
	// non-positive attempt number.
	ErrQuarantineInvalid = errors.New("schedule: invalid quarantine request")

	// ErrQuarantineStale reports a failure report behind the recorded
	// attempt for its key: history never rewinds.
	ErrQuarantineStale = errors.New("schedule: quarantine report is stale")

	// ErrDeadLetterNotFound reports a redrive for a key with no dead
	// letter.
	ErrDeadLetterNotFound = errors.New("schedule: no dead letter for key")

	// ErrRedriveApproval reports a redrive without an accountable
	// approver and reason: only an approved new attempt may replay.
	ErrRedriveApproval = errors.New("schedule: redrive requires approver and reason")

	// ErrRedrivePayloadChanged reports a redrive whose payload
	// fingerprint differs from the quarantined firing: changed-payload
	// replay is refused, never redriven.
	ErrRedrivePayloadChanged = errors.New("schedule: redrive payload differs from quarantined firing")

	// ErrRedriveExhausted reports a redrive against a terminal dead
	// letter: the attempt budget is spent, so retries can never run
	// forever.
	ErrRedriveExhausted = errors.New("schedule: dead letter attempt budget is exhausted")
)

// QuarantinePolicy bounds one quarantine store. MaxAttempts is the hard
// cap on attempts per key: the letter recorded for the final attempt is
// terminal and refuses redrive.
type QuarantinePolicy struct {
	MaxAttempts int
	Clock       func() time.Time
}

// QuarantineRequest is one failed firing plus the accountability for its
// quarantine. Attempt is the 1-based attempt number that failed.
type QuarantineRequest struct {
	Firing  Firing
	Reason  string
	Failure string
	Attempt int
}

// DeadLetter is one immutable quarantine record. It preserves the failed
// firing's identity and versions: key, sequence, trigger digest and
// trigger revision, plus the payload fingerprint a redrive must replay
// exactly. Supersedes chains a re-failure after redrive to its prior
// letter; Duplicate is transport state set on redelivery and never part
// of the seal.
type DeadLetter struct {
	Key            string
	Sequence       uint64
	Kind           FiringKind
	TriggerTenant  string
	TriggerID      string
	TriggerVersion string
	TriggerDigest  string
	OccurrenceKey  string
	PayloadDigest  string
	Fingerprint    string
	Reason         string
	Failure        string
	Attempt        int
	Terminal       bool
	Supersedes     string
	QuarantinedAt  time.Time
	Duplicate      bool
	Digest         string
}

// RedriveRequest is one approved replay of a quarantined firing.
type RedriveRequest struct {
	Key            string
	Firing         Firing
	Approver       string
	ApprovalReason string
}

// RedriveAttempt is one approved new attempt. It carries the dead letter
// digest it replays and the payload fingerprint it must match, so the
// resulting lineage is explicit: original identity, original versions,
// new attempt number.
type RedriveAttempt struct {
	Key              string
	Attempt          int
	Fingerprint      string
	DeadLetterDigest string
	Approver         string
	ApprovalReason   string
	ApprovedAt       time.Time
	Digest           string
}

// QuarantineSnapshot is the durable state a restarted store resumes from.
type QuarantineSnapshot struct {
	Letters  []DeadLetter
	Redrives []RedriveAttempt
}

// QuarantineStore quarantines failed trigger firings and releases
// approved redrives. It is safe for concurrent use.
type QuarantineStore struct {
	mu       sync.Mutex
	max      int
	clock    func() time.Time
	current  map[string]DeadLetter
	history  map[string][]DeadLetter
	redrives map[string][]RedriveAttempt
}

// NewQuarantineStore validates one quarantine policy.
func NewQuarantineStore(policy QuarantinePolicy) (*QuarantineStore, error) {
	if policy.MaxAttempts < 1 {
		return nil, fmt.Errorf("schedule: NewQuarantineStore: %w", ErrQuarantineInvalid)
	}
	clock := policy.Clock
	if clock == nil {
		clock = time.Now
	}
	return &QuarantineStore{
		max:      policy.MaxAttempts,
		clock:    clock,
		current:  make(map[string]DeadLetter),
		history:  make(map[string][]DeadLetter),
		redrives: make(map[string][]RedriveAttempt),
	}, nil
}

// fingerprintFiring binds the replayable identity of one firing: its key,
// sequence, trigger versions and payload. Two firings share a fingerprint
// only when a redrive replays the quarantined payload exactly.
func fingerprintFiring(firing Firing) string {
	parts := []string{string(firing.Kind), firing.Key, fmt.Sprintf("%d", firing.Sequence), firing.Trigger.Digest, firing.Occurrence.Key, firing.Observation.EventID, firing.Observation.PayloadDigest}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validateQuarantineFiring(firing Firing) error {
	if (firing.Kind != FiringOccurrence && firing.Kind != FiringEvent) ||
		strings.TrimSpace(firing.Key) == "" || firing.Key != strings.TrimSpace(firing.Key) || firing.Sequence == 0 {
		return ErrQuarantineInvalid
	}
	if firing.Trigger.Digest != "" {
		if err := firing.Trigger.Verify(); err != nil {
			return fmt.Errorf("%w: quarantined trigger: %v", ErrQuarantineInvalid, err)
		}
	}
	return nil
}

func sealDeadLetter(letter DeadLetter) DeadLetter {
	body, _ := json.Marshal(struct {
		Key            string
		Sequence       uint64
		Kind           FiringKind
		TriggerTenant  string
		TriggerID      string
		TriggerVersion string
		TriggerDigest  string
		OccurrenceKey  string
		PayloadDigest  string
		Fingerprint    string
		Reason         string
		Failure        string
		Attempt        int
		Terminal       bool
		Supersedes     string
		QuarantinedAt  time.Time
	}{letter.Key, letter.Sequence, letter.Kind, letter.TriggerTenant, letter.TriggerID, letter.TriggerVersion, letter.TriggerDigest, letter.OccurrenceKey, letter.PayloadDigest, letter.Fingerprint, letter.Reason, letter.Failure, letter.Attempt, letter.Terminal, letter.Supersedes, letter.QuarantinedAt})
	sum := sha256.Sum256(body)
	letter.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return letter
}

// Verify checks the letter's seal. Duplicate is transport state and is
// never covered by the digest.
func (l DeadLetter) Verify() error {
	if l.Digest == "" {
		return ErrQuarantineInvalid
	}
	quiet := l
	quiet.Duplicate = false
	if sealDeadLetter(quiet).Digest != l.Digest {
		return fmt.Errorf("%w: dead letter seal mismatch", ErrQuarantineInvalid)
	}
	return nil
}

func sealRedrive(attempt RedriveAttempt) RedriveAttempt {
	body, _ := json.Marshal(struct {
		Key              string
		Attempt          int
		Fingerprint      string
		DeadLetterDigest string
		Approver         string
		ApprovalReason   string
		ApprovedAt       time.Time
	}{attempt.Key, attempt.Attempt, attempt.Fingerprint, attempt.DeadLetterDigest, attempt.Approver, attempt.ApprovalReason, attempt.ApprovedAt})
	sum := sha256.Sum256(body)
	attempt.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return attempt
}

// Verify checks the redrive record's seal.
func (a RedriveAttempt) Verify() error {
	if a.Digest == "" || sealRedrive(a).Digest != a.Digest {
		return fmt.Errorf("%w: redrive seal mismatch", ErrRedriveApproval)
	}
	return nil
}

// Quarantine records one failed firing as an immutable dead letter.
// Redelivering the same attempt converges on the stored letter with
// Duplicate set; a later attempt after redrive supersedes with lineage;
// a report behind the recorded attempt is stale.
func (s *QuarantineStore) Quarantine(request QuarantineRequest) (DeadLetter, error) {
	if err := validateQuarantineFiring(request.Firing); err != nil {
		return DeadLetter{}, err
	}
	if strings.TrimSpace(request.Reason) == "" || strings.TrimSpace(request.Failure) == "" || request.Attempt < 1 {
		return DeadLetter{}, ErrQuarantineInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if letter, ok := s.current[request.Firing.Key]; ok {
		if request.Attempt == letter.Attempt {
			letter.Duplicate = true
			return letter, nil
		}
		if request.Attempt < letter.Attempt {
			return DeadLetter{}, fmt.Errorf("schedule: Quarantine %s: %w", request.Firing.Key, ErrQuarantineStale)
		}
	}
	firing := request.Firing
	letter := DeadLetter{
		Key: firing.Key, Sequence: firing.Sequence, Kind: firing.Kind,
		TriggerTenant: firing.Trigger.Definition.TenantID, TriggerID: firing.Trigger.Definition.ID,
		TriggerVersion: firing.Trigger.Definition.Version, TriggerDigest: firing.Trigger.Digest,
		OccurrenceKey: firing.Occurrence.Key, PayloadDigest: firing.Observation.PayloadDigest,
		Fingerprint: fingerprintFiring(firing),
		Reason:      request.Reason, Failure: request.Failure, Attempt: request.Attempt,
		Terminal: request.Attempt >= s.max, QuarantinedAt: s.clock().UTC(),
	}
	if prior, ok := s.current[request.Firing.Key]; ok {
		letter.Supersedes = prior.Digest
	}
	letter = sealDeadLetter(letter)
	s.current[request.Firing.Key] = letter
	s.history[request.Firing.Key] = append(s.history[request.Firing.Key], letter)
	return letter, nil
}

// Redrive releases one approved new attempt for a live dead letter. The
// replay must fingerprint-match the quarantined firing exactly:
// changed-payload replay is refused, and terminal letters never redrive.
func (s *QuarantineStore) Redrive(request RedriveRequest) (RedriveAttempt, error) {
	if err := validateQuarantineFiring(request.Firing); err != nil {
		return RedriveAttempt{}, err
	}
	if strings.TrimSpace(request.Approver) == "" || strings.TrimSpace(request.ApprovalReason) == "" {
		return RedriveAttempt{}, ErrRedriveApproval
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	letter, ok := s.current[request.Key]
	if !ok {
		return RedriveAttempt{}, fmt.Errorf("schedule: Redrive %s: %w", request.Key, ErrDeadLetterNotFound)
	}
	if letter.Terminal {
		return RedriveAttempt{}, fmt.Errorf("schedule: Redrive %s: %w", request.Key, ErrRedriveExhausted)
	}
	if fingerprint := fingerprintFiring(request.Firing); fingerprint != letter.Fingerprint {
		return RedriveAttempt{}, fmt.Errorf("schedule: Redrive %s: %w", request.Key, ErrRedrivePayloadChanged)
	}
	attempt := sealRedrive(RedriveAttempt{
		Key: request.Key, Attempt: letter.Attempt + 1, Fingerprint: letter.Fingerprint,
		DeadLetterDigest: letter.Digest, Approver: request.Approver,
		ApprovalReason: request.ApprovalReason, ApprovedAt: s.clock().UTC(),
	})
	s.redrives[request.Key] = append(s.redrives[request.Key], attempt)
	return attempt, nil
}

// History returns the immutable dead-letter chain for one key, oldest
// first. It is empty for an unknown key.
func (s *QuarantineStore) History(key string) []DeadLetter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DeadLetter(nil), s.history[key]...)
}

// Snapshot exports the durable quarantine state.
func (s *QuarantineStore) Snapshot() QuarantineSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := QuarantineSnapshot{}
	for _, chain := range s.history {
		snapshot.Letters = append(snapshot.Letters, chain...)
	}
	for _, attempts := range s.redrives {
		snapshot.Redrives = append(snapshot.Redrives, attempts...)
	}
	sort.Slice(snapshot.Letters, func(i, j int) bool {
		if snapshot.Letters[i].Key != snapshot.Letters[j].Key {
			return snapshot.Letters[i].Key < snapshot.Letters[j].Key
		}
		return snapshot.Letters[i].Attempt < snapshot.Letters[j].Attempt
	})
	sort.Slice(snapshot.Redrives, func(i, j int) bool {
		if snapshot.Redrives[i].Key != snapshot.Redrives[j].Key {
			return snapshot.Redrives[i].Key < snapshot.Redrives[j].Key
		}
		return snapshot.Redrives[i].Attempt < snapshot.Redrives[j].Attempt
	})
	return snapshot
}

// ResumeQuarantine rebuilds a store from durable state and verifies every
// sealed record before trusting it, so a restart preserves dead letters,
// redrive lineage and the attempt cursor.
func ResumeQuarantine(snapshot QuarantineSnapshot, policy QuarantinePolicy) (*QuarantineStore, error) {
	store, err := NewQuarantineStore(policy)
	if err != nil {
		return nil, err
	}
	for _, letter := range snapshot.Letters {
		quiet := letter
		quiet.Duplicate = false
		if err := quiet.Verify(); err != nil {
			return nil, err
		}
		if current, ok := store.current[letter.Key]; ok && letter.Attempt <= current.Attempt {
			return nil, fmt.Errorf("schedule: ResumeQuarantine %s: %w", letter.Key, ErrQuarantineStale)
		}
		store.current[letter.Key] = quiet
		store.history[letter.Key] = append(store.history[letter.Key], quiet)
	}
	for _, attempt := range snapshot.Redrives {
		if err := attempt.Verify(); err != nil {
			return nil, err
		}
		if _, ok := store.current[attempt.Key]; !ok {
			return nil, fmt.Errorf("schedule: ResumeQuarantine %s: %w", attempt.Key, ErrDeadLetterNotFound)
		}
		store.redrives[attempt.Key] = append(store.redrives[attempt.Key], attempt)
	}
	return store, nil
}
