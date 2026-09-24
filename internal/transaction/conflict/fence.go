package conflict

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidIntent   = errors.New("conflict: invalid write intent")
	ErrIntentConflict  = errors.New("conflict: write intent conflict")
	ErrStaleBaseline   = errors.New("conflict: stale baseline")
	ErrFence           = errors.New("conflict: stale fence")
	ErrAlreadyTerminal = errors.New("conflict: intent already terminal")
)

// Error is a stable machine-readable commit refusal.
type Error struct {
	Code     string
	IntentID string
	Err      error
}

func (e *Error) Error() string { return fmt.Sprintf("conflict: %s for %s", e.Code, e.IntentID) }
func (e *Error) Unwrap() error { return e.Err }
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

const (
	CodeConflictStaleBaseline = "CONFLICT_STALE_BASELINE"
	CodeConflictOrdering      = "CONFLICT_ORDERING"
	CodeConflictFence         = "CONFLICT_STALE_FENCE"
)

// CommitRequest supplies the observation made by the coordinator immediately
// before commit. Current footprints use the observed revision in
// ExpectedRevision; no database or transaction is opened by this package.
type CommitRequest struct {
	TenantID       string
	IntentID       string
	SnapshotDigest string
	Current        []WriteFootprint
	// Streams is the complete stream baseline set bound into the executable
	// transaction plan. Durable registries require an exact match with the
	// streams declared by the registered footprints; an unrelated intent must
	// never fence a plan that writes a different stream.
	Streams          []StreamBaseline
	Writes           []WriteBaseline
	FootprintDigests []string
}

type StreamBaseline struct {
	StreamKey        string
	ExpectedSequence uint64
}

type WriteBaseline struct {
	ResourceCanonical       string
	FieldPath               FieldPath
	StreamKey               string
	ExpectedSequence        uint64
	AuthorityDomain         string
	SourceAuthorityDecision string
	Operation               Operation
	EffectiveInterval       values.EffectiveInterval
}

// BaselineFromFootprint converts one validated write footprint to the compact
// commit-bound representation. Expected sequence is carried explicitly so a
// reconstructed footprint retains the approved stream revision.
func BaselineFromFootprint(footprint WriteFootprint) (WriteBaseline, error) {
	if err := footprint.Validate(); err != nil {
		return WriteBaseline{}, err
	}
	sequence, ok := footprint.ExpectedRevision.Sequence()
	if !ok {
		return WriteBaseline{}, fmt.Errorf("%w: footprint revision is not a sequence", ErrInvalidFootprint)
	}
	return WriteBaseline{
		ResourceCanonical:       footprint.Resource.String(),
		FieldPath:               footprint.Field,
		StreamKey:               footprint.ExpectedRevision.Stream(),
		ExpectedSequence:        sequence,
		AuthorityDomain:         footprint.Authority.Domain,
		SourceAuthorityDecision: footprint.Authority.PolicyRef,
		Operation:               footprint.Operation,
		EffectiveInterval:       footprint.Interval,
	}, nil
}

// Footprint reconstructs a validated write footprint from the representation
// carried by a transaction plan or the durable conflict fence.
func (baseline WriteBaseline) Footprint() (WriteFootprint, error) {
	var resource values.ResourceKey
	if err := resource.UnmarshalText([]byte(baseline.ResourceCanonical)); err != nil {
		return WriteFootprint{}, fmt.Errorf("%w: resource: %v", ErrInvalidFootprint, err)
	}
	revision, err := values.NewSequenceRevision(baseline.StreamKey, baseline.ExpectedSequence)
	if err != nil {
		return WriteFootprint{}, fmt.Errorf("%w: expected revision: %v", ErrInvalidFootprint, err)
	}
	footprint := WriteFootprint{
		Resource:         resource,
		Field:            baseline.FieldPath,
		Interval:         baseline.EffectiveInterval,
		Operation:        baseline.Operation,
		ExpectedRevision: revision,
		Authority: AuthorityScope{
			Domain: baseline.AuthorityDomain, PolicyRef: baseline.SourceAuthorityDecision,
		},
	}
	if err := footprint.Validate(); err != nil {
		return WriteFootprint{}, err
	}
	return footprint, nil
}

type CommitResult struct {
	Intent   WriteIntent
	Fence    uint64
	Decision Decision
}

// ValidateAtCommit closes the preflight race and reserves the intent's
// normalized scope. The first overlapping intent with the same baseline wins;
// every later contender receives CONFLICT_STALE_BASELINE. The critical section
// is only the fake's atomic CAS, never a worker/business lock.
func (r *Registry) ValidateAtCommit(req CommitRequest) (CommitResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.items[req.IntentID]
	if !ok {
		return CommitResult{}, fmt.Errorf("%w: %s", ErrInvalidIntent, req.IntentID)
	}
	if in.Status.terminal() {
		return CommitResult{}, ErrAlreadyTerminal
	}
	// A commit without a current observation cannot prove that its preflight
	// baseline is still current. Failing closed here is essential: treating a
	// missing observation as "nothing to check" would let a stale writer evade
	// the fence entirely.
	if len(req.Current) == 0 {
		return r.stale(in)
	}
	for _, want := range in.Footprints {
		matched := false
		for _, got := range req.Current {
			overlap, err := want.Overlaps(got)
			if err != nil {
				return CommitResult{}, err
			}
			if overlap {
				matched = true
				if want.ExpectedRevision.Canonical() == nil || got.ExpectedRevision.Canonical() == nil || string(want.ExpectedRevision.Canonical()) != string(got.ExpectedRevision.Canonical()) {
					return r.stale(in)
				}
				break
			}
		}
		if !matched {
			return r.stale(in)
		}
	}
	for _, f := range in.Footprints {
		for _, otherID := range r.active {
			if otherID == in.ID {
				continue
			}
			other := r.items[otherID]
			for _, of := range other.Footprints {
				overlap, err := f.Overlaps(of)
				if err != nil {
					return CommitResult{}, err
				}
				if overlap {
					return r.stale(in)
				}
			}
		}
	}
	r.next++
	in.Fence = r.next
	in.Status = IntentCommitted
	for _, f := range in.Footprints {
		r.active[f.ScopeDigest()] = in.ID
	}
	return CommitResult{Intent: *in, Fence: in.Fence, Decision: DecisionHardConflict}, nil
}

func (r *Registry) stale(in *WriteIntent) (CommitResult, error) {
	in.Status = IntentConflicted
	return CommitResult{}, &Error{Code: CodeConflictStaleBaseline, IntentID: in.ID, Err: ErrStaleBaseline}
}

// Release closes a committed or reserved intent exactly once. Replaying the
// same fence is idempotent; a different fence cannot release another writer.
func (r *Registry) Release(id string, fence uint64) (WriteIntent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.items[id]
	if !ok {
		return WriteIntent{}, ErrInvalidIntent
	}
	if in.Fence != fence || fence == 0 {
		return WriteIntent{}, ErrFence
	}
	if in.Status == IntentReleased {
		return *in, nil
	}
	if in.Status != IntentCommitted && in.Status != IntentReserved {
		return WriteIntent{}, ErrAlreadyTerminal
	}
	in.Status = IntentReleased
	for _, f := range in.Footprints {
		if r.active[f.ScopeDigest()] == id {
			delete(r.active, f.ScopeDigest())
		}
	}
	return *in, nil
}

func (r *Registry) Lookup(id string) (WriteIntent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.items[id]
	if !ok {
		return WriteIntent{}, false
	}
	return *in, true
}

func NewIntentRegistry() *Registry                                 { return NewRegistry() }
func (r *Registry) Persist(in WriteIntent) (WriteIntent, error)    { return r.Register(in) }
func (r *Registry) Commit(req CommitRequest) (CommitResult, error) { return r.ValidateAtCommit(req) }
