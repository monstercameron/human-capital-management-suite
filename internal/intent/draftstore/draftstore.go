// Package draftstore defines the durable persistence semantics of intent
// authoring drafts (ALIGN-025).
//
// A draft is saved and resumed; it is never a submission and it never
// produces an effect. The semantics every durable [Port] implementation must
// keep (the package's conformance tests exercise each one):
//
//   - Ownership: a draft belongs to one tenant and one author. Any other
//     tenant or principal sees it as not found -- never as "denied", which
//     would confirm it exists.
//   - Revisions: every save is a compare-and-set on the stored revision;
//     creation expects revision 0, and a stale writer gets [ErrConflict]
//     rather than overwriting a newer save.
//   - Validity: inputs are validated against the draft's pinned definition
//     on every save, so a server-owned or undeclared path is never persisted.
//   - Retention: a draft untouched for its TTL is EXPIRED; its inputs are not
//     returned once expired.
//   - Definition drift: resuming against a newer definition version returns
//     REBASE_REQUIRED with the stored inputs, never a silently migrated draft.
//   - Submission: a draft is stamped with the one intent it became exactly
//     once; afterwards it is immutable and a second stamp conflicts.
//   - Safe listing: listing returns metadata and an input digest, never input
//     values.
package draftstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DefaultTTL is the retention of an untouched draft.
const DefaultTTL = 30 * 24 * time.Hour

// Sentinels.
var (
	ErrInvalid          = errors.New("draftstore: invalid draft")
	ErrNotFound         = errors.New("draftstore: draft not found")
	ErrConflict         = errors.New("draftstore: revision conflict")
	ErrAlreadySubmitted = errors.New("draftstore: draft already submitted")
)

// Outcome is the result of resuming a draft.
type Outcome string

// Resume outcomes.
const (
	OutcomeCurrent        Outcome = "CURRENT"
	OutcomeRebaseRequired Outcome = "REBASE_REQUIRED"
	OutcomeExpired        Outcome = "EXPIRED"
	OutcomeSubmitted      Outcome = "SUBMITTED"
)

// Owner scopes every read and write.
type Owner struct {
	Tenant      values.TenantId
	PrincipalID string
}

func (o Owner) validate() error {
	if o.Tenant.Validate() != nil || strings.TrimSpace(o.PrincipalID) == "" {
		return fmt.Errorf("%w: owner tenant and principal are required", ErrInvalid)
	}
	return nil
}

// Record is one stored draft revision.
type Record struct {
	Draft       intent.AuthoringDraft
	Revision    uint64
	InputDigest string
	ExpiresAt   time.Time
}

// Summary is the safe listing view of a draft: no input values.
type Summary struct {
	DraftID           string `json:"draft_id"`
	Definition        string `json:"definition"`
	Revision          uint64 `json:"revision"`
	InputDigest       string `json:"input_digest"`
	MissingInputs     int    `json:"missing_inputs"`
	SubmittedIntentID string `json:"submitted_intent_id,omitempty"`
	ExpiresAt         string `json:"expires_at"`
}

// Resumed is the result of resuming a draft.
type Resumed struct {
	Outcome Outcome
	// Record is populated for CURRENT, REBASE_REQUIRED and SUBMITTED; an
	// expired draft returns no inputs.
	Record Record
}

// Port is a durable draft store.
type Port interface {
	// Save creates (expectedRevision 0) or replaces the owner's draft.
	Save(ctx context.Context, owner Owner, draft intent.AuthoringDraft, def intent.Definition, expectedRevision uint64, now time.Time) (Record, error)
	// Resume reads the owner's draft against the current definition.
	Resume(ctx context.Context, owner Owner, draftID string, current intent.Definition, now time.Time) (Resumed, error)
	// MarkSubmitted stamps the draft with the intent it became, once.
	MarkSubmitted(ctx context.Context, owner Owner, draftID string, expectedRevision uint64, intentID string, now time.Time) (Record, error)
	// List returns the owner's unexpired drafts as safe summaries.
	List(ctx context.Context, owner Owner, defs func(intent.Ref) (intent.Definition, bool), now time.Time) ([]Summary, error)
}

// InputDigest is the stable digest of a draft's inputs, order-independent.
func InputDigest(inputs []intent.InputValue) string {
	sorted := slices.Clone(inputs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	body, _ := json.Marshal(sorted)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Memory is the reference [Port]. It is safe for concurrent use.
type Memory struct {
	mu     sync.Mutex
	ttl    time.Duration
	drafts map[string]Record
}

// NewMemory returns an empty store with ttl (DefaultTTL when <= 0).
func NewMemory(ttl time.Duration) *Memory {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Memory{ttl: ttl, drafts: map[string]Record{}}
}

func key(tenant values.TenantId, id string) string { return string(tenant) + "\x1f" + id }

func (m *Memory) owned(owner Owner, id string) (Record, bool) {
	r, ok := m.drafts[key(owner.Tenant, id)]
	if !ok || r.Draft.Tenant != owner.Tenant || r.Draft.Author.PrincipalID != owner.PrincipalID {
		return Record{}, false
	}
	return r, true
}

// Save implements Port.
func (m *Memory) Save(_ context.Context, owner Owner, draft intent.AuthoringDraft, def intent.Definition, expectedRevision uint64, now time.Time) (Record, error) {
	if err := owner.validate(); err != nil {
		return Record{}, err
	}
	if now.IsZero() {
		return Record{}, fmt.Errorf("%w: save needs the caller's instant", ErrInvalid)
	}
	if draft.Tenant != owner.Tenant || draft.Author.PrincipalID != owner.PrincipalID {
		return Record{}, ErrNotFound
	}
	if err := draft.Validate(def); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if draft.Submitted() {
		return Record{}, fmt.Errorf("%w: a submitted draft is stamped through MarkSubmitted", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, exists := m.drafts[key(owner.Tenant, draft.DraftID)]
	switch {
	case exists && (stored.Draft.Author.PrincipalID != owner.PrincipalID):
		return Record{}, ErrNotFound
	case exists && stored.Draft.Submitted():
		return Record{}, ErrAlreadySubmitted
	case !exists && expectedRevision != 0, exists && stored.Revision != expectedRevision:
		return Record{}, fmt.Errorf("%w: expected revision %d", ErrConflict, expectedRevision)
	}
	rec := Record{Draft: cloneDraft(draft), Revision: expectedRevision + 1, InputDigest: InputDigest(draft.Inputs), ExpiresAt: now.UTC().Add(m.ttl)}
	m.drafts[key(owner.Tenant, draft.DraftID)] = rec
	return cloneRecord(rec), nil
}

// Resume implements Port.
func (m *Memory) Resume(_ context.Context, owner Owner, draftID string, current intent.Definition, now time.Time) (Resumed, error) {
	if err := owner.validate(); err != nil {
		return Resumed{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.owned(owner, draftID)
	if !ok {
		return Resumed{}, ErrNotFound
	}
	switch {
	case rec.Draft.Submitted():
		return Resumed{Outcome: OutcomeSubmitted, Record: cloneRecord(rec)}, nil
	case !now.UTC().Before(rec.ExpiresAt):
		return Resumed{Outcome: OutcomeExpired, Record: Record{Draft: intent.AuthoringDraft{DraftID: rec.Draft.DraftID, Tenant: rec.Draft.Tenant, Definition: rec.Draft.Definition, Author: rec.Draft.Author}, Revision: rec.Revision, ExpiresAt: rec.ExpiresAt}}, nil
	case rec.Draft.Definition != current.Ref || rec.Draft.Validate(current) != nil:
		return Resumed{Outcome: OutcomeRebaseRequired, Record: cloneRecord(rec)}, nil
	}
	return Resumed{Outcome: OutcomeCurrent, Record: cloneRecord(rec)}, nil
}

// MarkSubmitted implements Port.
func (m *Memory) MarkSubmitted(_ context.Context, owner Owner, draftID string, expectedRevision uint64, intentID string, now time.Time) (Record, error) {
	if err := owner.validate(); err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(intentID) == "" || now.IsZero() {
		return Record{}, fmt.Errorf("%w: intent id and instant are required", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.owned(owner, draftID)
	switch {
	case !ok:
		return Record{}, ErrNotFound
	case rec.Draft.Submitted():
		return Record{}, ErrAlreadySubmitted
	case rec.Revision != expectedRevision:
		return Record{}, fmt.Errorf("%w: expected revision %d", ErrConflict, expectedRevision)
	case !now.UTC().Before(rec.ExpiresAt):
		return Record{}, fmt.Errorf("%w: draft expired", ErrNotFound)
	}
	rec.Draft.SubmittedIntentID = intentID
	rec.Revision++
	m.drafts[key(owner.Tenant, draftID)] = rec
	return cloneRecord(rec), nil
}

// List implements Port.
func (m *Memory) List(_ context.Context, owner Owner, defs func(intent.Ref) (intent.Definition, bool), now time.Time) ([]Summary, error) {
	if err := owner.validate(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Summary{}
	for _, rec := range m.drafts {
		if rec.Draft.Tenant != owner.Tenant || rec.Draft.Author.PrincipalID != owner.PrincipalID || !now.UTC().Before(rec.ExpiresAt) {
			continue
		}
		missing := 0
		if defs != nil {
			if def, ok := defs(rec.Draft.Definition); ok {
				missing = len(rec.Draft.MissingRequiredInputs(def))
			}
		}
		out = append(out, Summary{DraftID: rec.Draft.DraftID, Definition: rec.Draft.Definition.String(), Revision: rec.Revision,
			InputDigest: rec.InputDigest, MissingInputs: missing, SubmittedIntentID: rec.Draft.SubmittedIntentID,
			ExpiresAt: rec.ExpiresAt.UTC().Format(time.RFC3339)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DraftID < out[j].DraftID })
	return out, nil
}

func cloneDraft(d intent.AuthoringDraft) intent.AuthoringDraft {
	d.Inputs = slices.Clone(d.Inputs)
	return d
}

func cloneRecord(r Record) Record {
	r.Draft = cloneDraft(r.Draft)
	return r
}
