package contact

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Store is the tenant-aware persistence port for the digest-backed contact
// generation. The older EndpointRevision and VerificationChallenge types are
// intentionally outside this port; PERSIST-CONTACT-001 persists only the
// newer revision.go vocabulary.
type Store interface {
	PutEndpointRevision(context.Context, values.TenantId, ContactEndpointRevision, ...uint64) error
	GetEndpointRevision(context.Context, values.TenantId, string, uint64) (ContactEndpointRevision, error)
	ListEndpointRevisions(context.Context, values.TenantId, string) ([]ContactEndpointRevision, error)
	PutChallenge(context.Context, values.TenantId, ContactVerificationChallenge, ...string) error
	GetChallenge(context.Context, values.TenantId, string) (ContactVerificationChallenge, error)
	ListChallengeEvents(context.Context, values.TenantId, string) ([]ContactChallengeEvent, error)
}

var (
	ErrStoreInvalid   = errors.New("contact: invalid store input")
	ErrStoreNotFound  = errors.New("contact: stored contact record not found")
	ErrStoreDuplicate = errors.New("contact: duplicate revision or event")
	ErrStoreStaleCAS  = errors.New("contact: stale compare-and-swap")
	ErrStoreDatabase  = errors.New("contact: persistence failure")
)

// StoreErrorCode is the stable machine-readable classification of a contact
// persistence refusal.
type StoreErrorCode string

const (
	StoreInvalidCode        StoreErrorCode = "INVALID"
	StoreNotFoundCode       StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode      StoreErrorCode = "DUPLICATE_REVISION"
	StoreDuplicateEventCode StoreErrorCode = "DUPLICATE_EVENT"
	StoreStaleCASCode       StoreErrorCode = "STALE_CAS"
	StoreDatabaseCode       StoreErrorCode = "DATABASE"
)

// StoreError carries a stable code while retaining a semantic cause for
// callers that want errors.Is behavior.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual string
	Cause            error
}

func (e *StoreError) Error() string {
	if e == nil {
		return "contact: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("contact: %s", e.Code)
	}
	return fmt.Sprintf("contact: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.Cause != nil {
		return e.Cause
	}
	switch e.Code {
	case StoreInvalidCode:
		return ErrStoreInvalid
	case StoreNotFoundCode:
		return ErrStoreNotFound
	case StoreDuplicateCode, StoreDuplicateEventCode:
		return ErrStoreDuplicate
	case StoreStaleCASCode:
		return ErrStoreStaleCAS
	case StoreDatabaseCode:
		return ErrStoreDatabase
	default:
		return nil
	}
}

func storeFailure(code StoreErrorCode, detail string, cause error) error {
	return &StoreError{Code: code, Detail: detail, Cause: cause}
}

// CodeOf returns the stable code carried by the nearest StoreError.
func CodeOf(err error) StoreErrorCode {
	var typed *StoreError
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

// MemoryStore is the kernel-pure reference implementation of Store. It
// mirrors the append-only revision/event and challenge CAS rules without I/O.
type MemoryStore struct {
	mu         sync.RWMutex
	endpoints  map[string]ContactEndpointRevision
	challenges map[string]ContactVerificationChallenge
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		endpoints:  make(map[string]ContactEndpointRevision),
		challenges: make(map[string]ContactVerificationChallenge),
	}
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return storeFailure(StoreInvalidCode, "context is nil", ErrStoreInvalid)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func endpointKey(tenant values.TenantId, id string, revision uint64) string {
	return tenant.String() + "\x00" + id + fmt.Sprintf("\x00%d", revision)
}

func challengeKey(tenant values.TenantId, id string) string {
	return tenant.String() + "\x00" + id
}

func (s *MemoryStore) PutEndpointRevision(ctx context.Context, tenant values.TenantId, revision ContactEndpointRevision, expected ...uint64) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return storeFailure(StoreInvalidCode, "nil memory store", ErrStoreInvalid)
	}
	if err := tenant.Validate(); err != nil || revision.Subject.Tenant != tenant {
		return storeFailure(StoreInvalidCode, "tenant does not match endpoint subject", err)
	}
	if err := revision.Validate(); err != nil {
		return storeFailure(StoreInvalidCode, err.Error(), err)
	}
	if len(expected) > 1 {
		return storeFailure(StoreInvalidCode, "at most one expected revision is allowed", ErrStoreInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	latest := uint64(0)
	var prior ContactEndpointRevision
	for key, candidate := range s.endpoints {
		if key == endpointKey(tenant, revision.EndpointID, candidate.Revision) && candidate.Revision > latest {
			latest = candidate.Revision
			prior = candidate
		}
	}
	if _, exists := s.endpoints[endpointKey(tenant, revision.EndpointID, revision.Revision)]; exists {
		return storeFailure(StoreDuplicateCode, "endpoint revision already exists", ErrStoreDuplicate)
	}
	if len(expected) == 0 {
		if latest != 0 {
			return staleStore("", latest)
		}
	} else if expected[0] != latest || revision.Revision != latest+1 || revision.SupersedesRevision != latest {
		return staleStore(fmt.Sprintf("%d", expected[0]), latest)
	}
	if latest != 0 && (revision.Subject != prior.Subject || revision.Kind != prior.Kind || revision.Purpose != prior.Purpose) {
		return storeFailure(StoreInvalidCode, "endpoint identity or immutable scope changed", ErrStoreInvalid)
	}
	if latest == 0 && revision.Revision != 1 || latest == 0 && revision.SupersedesRevision != 0 {
		return storeFailure(StoreInvalidCode, "initial endpoint revision must start at one", ErrStoreInvalid)
	}
	s.endpoints[endpointKey(tenant, revision.EndpointID, revision.Revision)] = revision
	return nil
}

func staleStore(expected string, actual uint64) error {
	err := &StoreError{Code: StoreStaleCASCode, Detail: fmt.Sprintf("expected %q, actual %d", expected, actual), Cause: ErrStoreStaleCAS, Actual: fmt.Sprintf("%d", actual), Expected: expected}
	return err
}

func (s *MemoryStore) GetEndpointRevision(ctx context.Context, tenant values.TenantId, id string, revision uint64) (ContactEndpointRevision, error) {
	if err := contextErr(ctx); err != nil {
		return ContactEndpointRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rev, ok := s.endpoints[endpointKey(tenant, id, revision)]
	if !ok {
		return ContactEndpointRevision{}, storeFailure(StoreNotFoundCode, id, ErrStoreNotFound)
	}
	return rev, nil
}

func (s *MemoryStore) ListEndpointRevisions(ctx context.Context, tenant values.TenantId, id string) ([]ContactEndpointRevision, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ContactEndpointRevision, 0)
	for _, rev := range s.endpoints {
		if rev.Subject.Tenant == tenant && rev.EndpointID == id {
			out = append(out, rev)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	if len(out) == 0 {
		return nil, storeFailure(StoreNotFoundCode, id, ErrStoreNotFound)
	}
	return out, nil
}

func (s *MemoryStore) PutChallenge(ctx context.Context, tenant values.TenantId, challenge ContactVerificationChallenge, expected ...string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return storeFailure(StoreInvalidCode, "nil memory store", ErrStoreInvalid)
	}
	if err := tenant.Validate(); err != nil || challenge.Subject.Tenant != tenant {
		return storeFailure(StoreInvalidCode, "tenant does not match challenge subject", err)
	}
	if err := challenge.Validate(); err != nil {
		return storeFailure(StoreInvalidCode, err.Error(), err)
	}
	if duplicateChallengeEvent(challenge.Events) {
		return storeFailure(StoreDuplicateEventCode, "challenge event digest already exists", ErrStoreDuplicate)
	}
	if len(expected) > 1 {
		return storeFailure(StoreInvalidCode, "at most one expected digest is allowed", ErrStoreInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := challengeKey(tenant, challenge.ChallengeID)
	prior, exists := s.challenges[key]
	if !exists {
		if len(expected) == 1 && expected[0] != "" {
			return staleStore(expected[0], 0)
		}
		s.challenges[key] = cloneChallenge(challenge)
		return nil
	}
	if len(expected) == 0 || expected[0] != prior.CanonicalDigest {
		actual := prior.CanonicalDigest
		return &StoreError{Code: StoreStaleCASCode, Detail: "challenge canonical digest changed", Expected: firstExpected(expected), Actual: actual, Cause: ErrStoreStaleCAS}
	}
	if challenge.Subject != prior.Subject || challenge.EndpointID != prior.EndpointID ||
		challenge.EndpointRevisionDigest != prior.EndpointRevisionDigest ||
		challenge.NormalizedValueDigest != prior.NormalizedValueDigest ||
		challenge.Purpose != prior.Purpose || challenge.IssuedAt != prior.IssuedAt ||
		challenge.ExpiresAt != prior.ExpiresAt || challenge.AttemptBudget != prior.AttemptBudget ||
		challenge.TokenDigest != prior.TokenDigest {
		return storeFailure(StoreInvalidCode, "challenge identity or immutable scope changed", ErrStoreInvalid)
	}
	if len(challenge.Events) < len(prior.Events) {
		return storeFailure(StoreStaleCASCode, "challenge event history regressed", ErrStoreStaleCAS)
	}
	for i := range prior.Events {
		if challenge.Events[i].Digest != prior.Events[i].Digest {
			return storeFailure(StoreStaleCASCode, "challenge event history was rewritten", ErrStoreStaleCAS)
		}
	}
	s.challenges[key] = cloneChallenge(challenge)
	return nil
}

func firstExpected(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func duplicateChallengeEvent(events []ContactChallengeEvent) bool {
	seen := make(map[string]struct{}, len(events))
	for _, event := range events {
		if _, ok := seen[event.Digest]; ok {
			return true
		}
		seen[event.Digest] = struct{}{}
	}
	return false
}

func cloneChallenge(in ContactVerificationChallenge) ContactVerificationChallenge {
	in.Events = append([]ContactChallengeEvent(nil), in.Events...)
	return in
}

func (s *MemoryStore) GetChallenge(ctx context.Context, tenant values.TenantId, id string) (ContactVerificationChallenge, error) {
	if err := contextErr(ctx); err != nil {
		return ContactVerificationChallenge{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	challenge, ok := s.challenges[challengeKey(tenant, id)]
	if !ok {
		return ContactVerificationChallenge{}, storeFailure(StoreNotFoundCode, id, ErrStoreNotFound)
	}
	return cloneChallenge(challenge), nil
}

func (s *MemoryStore) ListChallengeEvents(ctx context.Context, tenant values.TenantId, id string) ([]ContactChallengeEvent, error) {
	challenge, err := s.GetChallenge(ctx, tenant, id)
	if err != nil {
		return nil, err
	}
	return append([]ContactChallengeEvent(nil), challenge.Events...), nil
}
