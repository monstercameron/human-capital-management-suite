package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ALIGN-031: product submissions are semantically idempotent. Every
// submission carries the key the acceptance was granted under; the registry
// replays an identical resubmission without a second effect and refuses a
// conflicting reuse of the same key. Keys are tenant-scoped, content is
// pinned by digest, and the registry is safe for concurrent use.

// SubmissionOutcome describes what a Submit did.
type SubmissionOutcome string

// Submission outcomes.
const (
	SubmissionAccepted SubmissionOutcome = "ACCEPTED"
	SubmissionReplayed SubmissionOutcome = "REPLAYED"
)

// Submission errors.
var (
	ErrSubmissionInvalid   = errors.New("app: product submission is invalid")
	ErrIdempotencyConflict = errors.New("app: idempotency key already used for different content")
)

// ProductSubmission is one tenant-scoped product submission. IdempotencyKey
// is the semantic key the acceptance carries (see AcceptedAction); the
// proposal and material digests pin the exact content the key was granted
// for.
type ProductSubmission struct {
	Tenant             values.TenantId `json:"tenant"`
	IntentID           string          `json:"intent_id"`
	ProposalRevisionID string          `json:"proposal_revision_id"`
	ProposalDigest     string          `json:"proposal_digest"`
	MaterialDigest     string          `json:"material_digest"`
	SubmittedBy        string          `json:"submitted_by"`
	SubmittedAt        values.Instant  `json:"submitted_at"`
	IdempotencyKey     string          `json:"idempotency_key"`
}

// SubmissionRecord is the retained effect of an accepted submission.
type SubmissionRecord struct {
	Tenant             values.TenantId `json:"tenant"`
	IntentID           string          `json:"intent_id"`
	ProposalRevisionID string          `json:"proposal_revision_id"`
	ProposalDigest     string          `json:"proposal_digest"`
	MaterialDigest     string          `json:"material_digest"`
	SubmittedBy        string          `json:"submitted_by"`
	SubmittedAt        values.Instant  `json:"submitted_at"`
	IdempotencyKey     string          `json:"idempotency_key"`
	Digest             string          `json:"digest"`
}

func (s ProductSubmission) validate() error {
	if err := s.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrSubmissionInvalid, err)
	}
	for _, req := range []struct{ field, value string }{
		{"intent_id", s.IntentID},
		{"proposal_revision_id", s.ProposalRevisionID},
		{"proposal_digest", s.ProposalDigest},
		{"material_digest", s.MaterialDigest},
		{"submitted_by", s.SubmittedBy},
		{"idempotency_key", s.IdempotencyKey},
	} {
		if strings.TrimSpace(req.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrSubmissionInvalid, req.field)
		}
	}
	if err := s.SubmittedAt.Validate(); err != nil {
		return fmt.Errorf("%w: submitted_at: %v", ErrSubmissionInvalid, err)
	}
	return nil
}

func (s ProductSubmission) contentDigest() string {
	b, err := json.Marshal(struct {
		Tenant             string `json:"tenant"`
		IntentID           string `json:"intent_id"`
		ProposalRevisionID string `json:"proposal_revision_id"`
		ProposalDigest     string `json:"proposal_digest"`
		MaterialDigest     string `json:"material_digest"`
		SubmittedBy        string `json:"submitted_by"`
		IdempotencyKey     string `json:"idempotency_key"`
	}{
		s.Tenant.String(), s.IntentID, s.ProposalRevisionID,
		s.ProposalDigest, s.MaterialDigest, s.SubmittedBy, s.IdempotencyKey,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SubmissionFromAccepted lifts a bound acceptance into a submission. The
// proposal digest pins both the revision and the material content the
// acceptance was granted against.
func SubmissionFromAccepted(action AcceptedAction) ProductSubmission {
	return ProductSubmission{
		Tenant:             action.Tenant,
		IntentID:           action.IntentID,
		ProposalRevisionID: action.ProposalRevisionID,
		ProposalDigest:     action.ProposalDigest,
		MaterialDigest:     action.ProposalDigest,
		SubmittedBy:        action.AcceptedBy,
		SubmittedAt:        action.AcceptedAt,
		IdempotencyKey:     action.IdempotencyKey,
	}
}

// SubmissionRegistry is the tenant-scoped idempotency registry. The zero
// value is not usable; build one with NewSubmissionRegistry.
type SubmissionRegistry struct {
	mu      sync.Mutex
	records map[string]SubmissionRecord
}

// NewSubmissionRegistry builds an empty registry.
func NewSubmissionRegistry() *SubmissionRegistry {
	return &SubmissionRegistry{records: make(map[string]SubmissionRecord)}
}

func submissionSlot(tenant values.TenantId, key string) string {
	return tenant.String() + "\x00" + key
}

// Submit records a submission. An identical resubmission under the same
// tenant key replays the stored record with no second effect; the same key
// with different content is a conflict and is refused with
// ErrIdempotencyConflict.
func (r *SubmissionRegistry) Submit(sub ProductSubmission) (SubmissionRecord, SubmissionOutcome, error) {
	if err := sub.validate(); err != nil {
		return SubmissionRecord{}, "", err
	}
	digest := sub.contentDigest()
	if digest == "" {
		return SubmissionRecord{}, "", fmt.Errorf("%w: content digest failed", ErrSubmissionInvalid)
	}
	slot := submissionSlot(sub.Tenant, sub.IdempotencyKey)
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.records[slot]; ok {
		if existing.Digest != digest {
			return SubmissionRecord{}, "", fmt.Errorf("%w: key %q", ErrIdempotencyConflict, sub.IdempotencyKey)
		}
		return existing, SubmissionReplayed, nil
	}
	record := SubmissionRecord{
		Tenant: sub.Tenant, IntentID: sub.IntentID,
		ProposalRevisionID: sub.ProposalRevisionID, ProposalDigest: sub.ProposalDigest,
		MaterialDigest: sub.MaterialDigest, SubmittedBy: sub.SubmittedBy,
		SubmittedAt: sub.SubmittedAt, IdempotencyKey: sub.IdempotencyKey, Digest: digest,
	}
	r.records[slot] = record
	return record, SubmissionAccepted, nil
}

// Lookup returns the stored record for a tenant key, if any.
func (r *SubmissionRegistry) Lookup(tenant values.TenantId, key string) (SubmissionRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[submissionSlot(tenant, key)]
	return record, ok
}

// Explain returns a value-free description of the idempotency contract.
func (r *SubmissionRegistry) Explain() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.records))
	for slot := range r.records {
		keys = append(keys, slot)
	}
	sort.Strings(keys)
	return fmt.Sprintf("semantic submission idempotency with %d tenant-scoped keys", len(keys))
}
