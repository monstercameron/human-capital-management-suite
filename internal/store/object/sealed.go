package object

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/envelope"
)

// This file binds object byte storage to a tenant's envelope keys
// (internal/trust/envelope). Object bytes are never written to, or read
// back from, storage in the clear: every write goes through
// envelope.Manager.Seal and every read through envelope.Manager.Open. No
// AES, GCM, KDF, nonce, or key-wrapping logic is implemented here; all of it
// is delegated to the already-complete envelope Manager.

var (
	// ErrEncryptionUnavailable is returned when object bytes cannot be
	// sealed or opened because the tenant KEK, custody provider, or manager
	// itself is unavailable. Every caller of this file fails closed on this
	// error: bytes are never written or returned in the clear as a
	// fallback.
	ErrEncryptionUnavailable = errors.New("object: encryption unavailable, refusing to expose object bytes")
	// ErrObjectBinding is returned when an authenticated payload names a
	// different logical object than the caller requested. envelope.Manager
	// binds that identity in AES-GCM AAD; this plaintext assertion remains a
	// second, independent check after every open.
	ErrObjectBinding = errors.New("object: decrypted envelope is not bound to the requested object")
	// ErrSealedEnvelope is returned when stored bytes cannot be parsed as a
	// sealed envelope at all (corrupt JSON, wrong shape).
	ErrSealedEnvelope = errors.New("object: stored bytes are not a valid sealed envelope")
	// ErrRewrapConflict is returned when a rewrap loses a race against a
	// concurrent mutation of the same object and must not be committed.
	ErrRewrapConflict = errors.New("object: object changed concurrently during rewrap")
)

// sealedPayload is the plaintext actually handed to envelope.Manager. It
// keeps an independent object identity assertion inside the encrypted
// payload as defense in depth alongside the envelope header AAD.
type sealedPayload struct {
	ObjectID string `json:"object_id"`
	Data     []byte `json:"data"`
}

// sealObject seals plaintext for objectID under the tenant identified by
// cctx. It never returns a partially-sealed result: any error means no
// envelope was produced.
func sealObject(manager *envelope.Manager, cctx custody.Context, objectID string, plaintext []byte) (envelope.Envelope, error) {
	if manager == nil {
		return envelope.Envelope{}, ErrEncryptionUnavailable
	}
	if err := validateID(objectID); err != nil {
		return envelope.Envelope{}, err
	}
	payload, err := json.Marshal(sealedPayload{ObjectID: objectID, Data: plaintext})
	if err != nil {
		return envelope.Envelope{}, fmt.Errorf("%w: encode sealed payload: %v", ErrInvalidRequest, err)
	}
	env, _, err := manager.Seal(cctx, objectID, payload)
	if err != nil {
		// Fail closed: the caller receives a typed, unambiguous error and
		// an empty Envelope. There is no cleartext fallback path.
		return envelope.Envelope{}, fmt.Errorf("%w: %v", ErrEncryptionUnavailable, err)
	}
	return env, nil
}

// openObject opens env and verifies it is bound to objectID. A tenant
// mismatch, unknown or revoked key, or tampered ciphertext or header
// surfaces the envelope package's own typed sentinel; an object-identity
// mismatch surfaces ErrObjectBinding. Neither path returns partial or
// substitute plaintext.
func openObject(manager *envelope.Manager, cctx custody.Context, objectID string, env envelope.Envelope) ([]byte, error) {
	if manager == nil {
		return nil, ErrEncryptionUnavailable
	}
	var payloadBytes []byte
	var err error
	if env.Header.ObjectID == "" {
		// Older envelopes authenticated object identity only inside the
		// plaintext payload. Open that legacy format with its original AAD;
		// the authenticated payload check below still must match objectID.
		payloadBytes, _, err = manager.Open(cctx, env)
	} else {
		payloadBytes, _, err = manager.Open(cctx, env, objectID)
	}
	if err != nil {
		if errors.Is(err, envelope.ErrObjectMismatch) {
			return nil, fmt.Errorf("%w: %w", ErrObjectBinding, err)
		}
		return nil, err
	}
	var payload sealedPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSealedEnvelope, err)
	}
	if payload.ObjectID != objectID {
		return nil, fmt.Errorf("%w: envelope is bound to %q, requested %q", ErrObjectBinding, payload.ObjectID, objectID)
	}
	return payload.Data, nil
}

type sealedEntry struct {
	envelope envelope.Envelope
	stat     Info
}

// SealedObjectStore stores artifact bytes only as tenant-bound envelope
// ciphertext, keyed by a caller-supplied logical object id. It is the
// provider-neutral contract a persistence adapter commits underneath, the
// same role Store and MultipartManager play for their own concerns; it
// never itself becomes a plaintext byte store.
type SealedObjectStore struct {
	manager *envelope.Manager

	mu      sync.RWMutex
	objects map[string]sealedEntry
}

// NewSealedObjectStore binds an in-memory object store to a tenant envelope
// hierarchy. A nil manager is a caller error: there is nothing this type
// can safely fall back to.
func NewSealedObjectStore(manager *envelope.Manager) (*SealedObjectStore, error) {
	if manager == nil {
		return nil, fmt.Errorf("%w: an envelope manager is required", ErrInvalidRequest)
	}
	return &SealedObjectStore{manager: manager, objects: make(map[string]sealedEntry)}, nil
}

// Put seals plaintext under the tenant's current envelope key (identified
// by cctx) and stores only the resulting envelope. If sealing fails for any
// reason — unregistered tenant KEK, unavailable custody provider, or any
// other encryption error — the store is left completely untouched: no
// entry, sealed or otherwise, is ever created.
func (s *SealedObjectStore) Put(ctx context.Context, cctx custody.Context, objectID, mediaType string, plaintext []byte) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if s == nil || s.manager == nil {
		return Info{}, ErrEncryptionUnavailable
	}
	if err := validateID(objectID); err != nil {
		return Info{}, err
	}
	if len(plaintext) == 0 {
		return Info{}, fmt.Errorf("%w: content is empty", ErrInvalidRequest)
	}
	s.mu.RLock()
	_, exists := s.objects[objectID]
	s.mu.RUnlock()
	if exists {
		return Info{}, fmt.Errorf("%w: %s", ErrAlreadyExists, objectID)
	}

	env, err := sealObject(s.manager, cctx, objectID, plaintext)
	if err != nil {
		return Info{}, err
	}
	envBytes, err := json.Marshal(env)
	if err != nil {
		return Info{}, fmt.Errorf("%w: encode envelope: %v", ErrEncryptionUnavailable, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.objects[objectID]; exists {
		return Info{}, fmt.Errorf("%w: %s", ErrAlreadyExists, objectID)
	}
	info := Info{ArtifactID: objectID, Digest: digest(envBytes), Size: int64(len(plaintext)), MediaType: mediaType, Generation: 1}
	s.objects[objectID] = sealedEntry{envelope: env, stat: info}
	return info, nil
}

// PutNewGeneration reseals plaintext as the next generation of an existing
// object, preserving its identity (ArtifactID) while advancing Generation
// by exactly one. Unlike Put, which is create-only, this is the one path
// that lets an object's content and recorded digest legitimately change; it
// is what makes "an older generation of the same object" a real, otherwise
// unreachable state instead of a hypothetical one, which integrity
// verification (ARTIFACT-006) must be able to detect on a replica that
// never advanced. Sealing follows the same fail-closed contract as Put: any
// error leaves the previous generation untouched.
func (s *SealedObjectStore) PutNewGeneration(ctx context.Context, cctx custody.Context, objectID, mediaType string, plaintext []byte) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if s == nil || s.manager == nil {
		return Info{}, ErrEncryptionUnavailable
	}
	if err := validateID(objectID); err != nil {
		return Info{}, err
	}
	if len(plaintext) == 0 {
		return Info{}, fmt.Errorf("%w: content is empty", ErrInvalidRequest)
	}
	s.mu.RLock()
	before, exists := s.objects[objectID]
	s.mu.RUnlock()
	if !exists {
		return Info{}, fmt.Errorf("%w: %s", ErrNotFound, objectID)
	}

	env, err := sealObject(s.manager, cctx, objectID, plaintext)
	if err != nil {
		return Info{}, err
	}
	envBytes, err := json.Marshal(env)
	if err != nil {
		return Info{}, fmt.Errorf("%w: encode envelope: %v", ErrEncryptionUnavailable, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.objects[objectID]
	if !exists {
		return Info{}, fmt.Errorf("%w: %s", ErrNotFound, objectID)
	}
	if current.stat.Generation != before.stat.Generation {
		// Another write landed a newer generation first; refuse rather than
		// silently clobbering it or skipping a generation number.
		return Info{}, ErrRewrapConflict
	}
	info := Info{ArtifactID: objectID, Digest: digest(envBytes), Size: int64(len(plaintext)), MediaType: mediaType, Generation: current.stat.Generation + 1}
	s.objects[objectID] = sealedEntry{envelope: env, stat: info}
	return info, nil
}

// Get opens the envelope stored under objectID and returns the original
// plaintext. Decryption fails closed on tenant mismatch, unknown or revoked
// key, tampered ciphertext or header, or an object-identity mismatch.
func (s *SealedObjectStore) Get(ctx context.Context, cctx custody.Context, objectID string) ([]byte, Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, Info{}, err
	}
	if s == nil || s.manager == nil {
		return nil, Info{}, ErrEncryptionUnavailable
	}
	s.mu.RLock()
	entry, ok := s.objects[objectID]
	s.mu.RUnlock()
	if !ok {
		return nil, Info{}, fmt.Errorf("%w: %s", ErrNotFound, objectID)
	}
	plaintext, err := openObject(s.manager, cctx, objectID, entry.envelope)
	if err != nil {
		return nil, Info{}, err
	}
	return plaintext, entry.stat, nil
}

// Stat returns metadata about the stored envelope without decrypting it.
// This is what keeps crypto-erasure honest: the object's existence, digest,
// and size describe the stored envelope bytes, never key material, so
// destroying a tenant's key can never make Stat lie about an object that
// still exists, nor make it vanish silently.
func (s *SealedObjectStore) Stat(ctx context.Context, objectID string) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if s == nil {
		return Info{}, ErrEncryptionUnavailable
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.objects[objectID]
	if !ok {
		return Info{}, fmt.Errorf("%w: %s", ErrNotFound, objectID)
	}
	return entry.stat, nil
}

// Envelope returns a defensive copy of the exact envelope stored for
// objectID. It never decrypts; it exists so a persistence adapter, backup
// job, or conformance test can inspect or migrate the stored ciphertext
// form directly.
func (s *SealedObjectStore) Envelope(objectID string) (envelope.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.objects[objectID]
	if !ok {
		return envelope.Envelope{}, fmt.Errorf("%w: %s", ErrNotFound, objectID)
	}
	return copyEnvelope(entry.envelope), nil
}

// ObjectIDs returns every stored object id in a stable, sorted order, for
// building a rotation/rewrap batch.
func (s *SealedObjectStore) ObjectIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.objects))
	for id := range s.objects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// RewrapOne moves the stored envelope for objectID onto the tenant's
// current KEK version via envelope.Manager.Rewrap. The data ciphertext and
// DEK identity never change; only the opaque key wrapper does. Calling it
// more than once for the same object — for example, resuming a rewrap
// batch that was interrupted partway through — is safe: Rewrap always
// produces a validly-decryptable envelope on the current KEK, and the
// compare-and-swap commit below refuses to overwrite an object that
// changed underneath it, so no object is ever left unreadable or
// double-wrapped.
func (s *SealedObjectStore) RewrapOne(ctx context.Context, cctx custody.Context, objectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.manager == nil {
		return ErrEncryptionUnavailable
	}
	s.mu.RLock()
	before, ok := s.objects[objectID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, objectID)
	}
	rewrapped, _, err := s.manager.Rewrap(cctx, before.envelope)
	if err != nil {
		return err
	}
	envBytes, err := json.Marshal(rewrapped)
	if err != nil {
		return fmt.Errorf("%w: encode envelope: %v", ErrEncryptionUnavailable, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.objects[objectID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, objectID)
	}
	if current.envelope.Header.DEKID != before.envelope.Header.DEKID || string(current.envelope.Data) != string(before.envelope.Data) {
		// Another rewrap or write committed first; the data layer this
		// rewrap unwrapped from is stale. Refuse rather than risk
		// discarding a newer wrapper.
		return ErrRewrapConflict
	}
	stat := current.stat
	stat.Digest = digest(envBytes)
	s.objects[objectID] = sealedEntry{envelope: rewrapped, stat: stat}
	return nil
}

// RewrapBatch rewraps every id in objectIDs, in order, and returns how many
// completed before the first error (0 and nil when every id succeeds is
// reported as len(objectIDs), nil). A caller that resumes after a failure
// by calling RewrapBatch again with the same (or remaining) ids is safe:
// every id, rewrapped or not, stays independently readable throughout, and
// re-rewrapping an already-current object is a no-op change of wrapper
// bytes only.
func (s *SealedObjectStore) RewrapBatch(ctx context.Context, cctx custody.Context, objectIDs []string) (int, error) {
	for i, id := range objectIDs {
		if err := s.RewrapOne(ctx, cctx, id); err != nil {
			return i, err
		}
	}
	return len(objectIDs), nil
}

func copyEnvelope(env envelope.Envelope) envelope.Envelope {
	out := env
	out.Header.Nonce = append([]byte(nil), env.Header.Nonce...)
	out.Data = append([]byte(nil), env.Data...)
	out.WrappedDEK.Data = append([]byte(nil), env.WrappedDEK.Data...)
	return out
}

// ExplainSealed summarizes the sealed store's contract for policy and
// operator evidence, without naming any object id or tenant.
func ExplainSealed(s *SealedObjectStore) string {
	if s == nil {
		return "object: sealed store unavailable"
	}
	return fmt.Sprintf("object: %d artifact(s) stored only as tenant-bound AES-GCM envelopes; no plaintext fallback on encryption failure", len(s.ObjectIDs()))
}
