package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// Business states: the closed leave lifecycle vocabulary.
const (
	BusinessLeaveActive = "LEAVE_ACTIVE"
	BusinessLeaveEnded  = "LEAVE_ENDED"
)

// Commit steps: every one of them commits once, or none do.
var commitSteps = []string{
	"leave-revision", "absence-relationship", "availability-interval",
	"balance-entries", "ledger-events", "projections", "effect-outbox",
}

// StartCommitInput carries the typed step receipts plus the employment
// assertion. Employment state is asserted, never mutated: anything but
// ACTIVE refuses.
type StartCommitInput struct {
	IdempotencyKey  string
	EmploymentState string
	StepReceipts    map[string]string
}

// CommitRecord is the atomic commit: Business state becomes LEAVE_ACTIVE
// independently of external consistency, and historical revisions stay
// append-only because the record only references them.
type CommitRecord struct {
	CommitID      string
	BusinessState string
	Steps         []string
	Digest        string
}

func commitDigest(key string, receipts map[string]string) string {
	parts := []string{"leave-start-commit", key}
	for _, step := range commitSteps {
		parts = append(parts, step+"="+receipts[step])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Committer is the owning atomic commit: one mutex-guarded registry.
// Failpoints yield nothing: active leave never exists without its
// availability, balance, event and outbox counterparts.
type Committer struct {
	mu      sync.Mutex
	records map[string]CommitRecord
}

// NewCommitter starts an empty committer.
func NewCommitter() *Committer {
	return &Committer{records: make(map[string]CommitRecord)}
}

// Commit atomically records one leave start. Every step receipt must be
// present with leave-revision first; any injected failure rolls the
// whole start back; repeats return the identical record.
func (committer *Committer) Commit(input StartCommitInput, inject func(string) error) (CommitRecord, error) {
	if committer == nil {
		return CommitRecord{}, fmt.Errorf("leave: nil committer")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return CommitRecord{}, fmt.Errorf("leave: commit needs an idempotency key")
	}
	if input.EmploymentState != "ACTIVE" {
		return CommitRecord{}, fmt.Errorf("leave: leave start never mutates employment state %q", input.EmploymentState)
	}
	for _, step := range commitSteps {
		if strings.TrimSpace(input.StepReceipts[step]) == "" {
			return CommitRecord{}, fmt.Errorf("leave: commit step %s has no receipt", step)
		}
	}
	committer.mu.Lock()
	defer committer.mu.Unlock()
	if prior, done := committer.records[input.IdempotencyKey]; done {
		return prior, nil
	}
	fail := func(step string) error {
		if inject == nil {
			return nil
		}
		return inject(step)
	}
	for _, step := range commitSteps {
		if err := fail(step); err != nil {
			return CommitRecord{}, fmt.Errorf("leave: rolled back at %s: %v", step, err)
		}
	}
	sum := sha256.Sum256([]byte("leave-commit-id\x00" + input.IdempotencyKey))
	record := CommitRecord{
		CommitID:      "sha256:" + hex.EncodeToString(sum[:]),
		BusinessState: BusinessLeaveActive, Steps: append([]string(nil), commitSteps...),
	}
	record.Digest = commitDigest(input.IdempotencyKey, input.StepReceipts)
	committer.records[input.IdempotencyKey] = record
	return record, nil
}

// Verify recomputes the commit seal.
func (record CommitRecord) Verify(key string, receipts map[string]string) error {
	if record.Digest == "" || commitDigest(key, receipts) != record.Digest {
		return fmt.Errorf("leave: commit seal is broken")
	}
	if record.BusinessState != BusinessLeaveActive {
		return fmt.Errorf("leave: commit reports %q instead of leave-active business state", record.BusinessState)
	}
	return nil
}
