// Package envelope implements provider-neutral envelope encryption.
//
// Plaintext is sealed with a fresh, in-memory AES-GCM data-encryption key
// (DEK). The DEK is immediately wrapped through the custody Provider using a
// tenant key-encryption key (KEK); the root custody key reference is retained
// as the root of the hierarchy but is never resolved by this package. A
// ciphertext carries only opaque custody output and a header binding tenant,
// object identity, KEK version, and DEK identity.
package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

const (
	Version       = 1
	dataAlgorithm = "AES-256-GCM"
	keyBytes      = 32
)

var (
	ErrInvalidRequest    = errors.New("envelope: invalid request")
	ErrInvalidCiphertext = errors.New("envelope: invalid ciphertext")
	ErrTenantMismatch    = errors.New("envelope: tenant separation check failed")
	ErrKeyUnavailable    = errors.New("envelope: tenant key is unavailable")
	ErrKeyRotation       = errors.New("envelope: key rotation failed")
	ErrObjectMismatch    = errors.New("envelope: object identity check failed")
)

// Header is authenticated as AES-GCM additional data. KEKID and KEKVersion
// together identify the exact tenant key version used to wrap DEKID.
type Header struct {
	Version    int    `json:"version"`
	Tenant     string `json:"tenant"`
	ObjectID   string `json:"object_id,omitempty"`
	KEKID      string `json:"kek_id"`
	KEKVersion string `json:"kek_version"`
	DEKID      string `json:"dek_id"`
	Algorithm  string `json:"algorithm"`
	Nonce      []byte `json:"nonce"`
}

// Envelope contains no plaintext or unwrapped DEK. WrappedDEK is the
// provider-neutral custody result for the DEK; Data is AES-GCM ciphertext.
type Envelope struct {
	Header     Header             `json:"header"`
	WrappedDEK custody.Ciphertext `json:"wrapped_dek"`
	Data       []byte             `json:"data"`
}

// Evidence is safe to persist: it references the operation without carrying
// plaintext or DEK material.
type Evidence struct {
	Operation     custody.Operation
	Tenant        string
	KEKID         string
	KEKVersion    string
	DEKID         string
	At            time.Time
	ContextDigest [32]byte
	ReceiptID     string
}

type tenantKeys struct {
	Root    custody.Handle
	Current custody.Handle
	History []custody.Handle
}

// Manager owns the non-sensitive hierarchy metadata and delegates every KEK
// operation to custody.Provider. It never stores raw keys.
type Manager struct {
	provider custody.Provider
	root     custody.Handle

	mu    sync.RWMutex
	keys  map[string]tenantKeys
	clock func() time.Time
}

// New creates a hierarchy rooted at root. Root must be an opaque key handle;
// root key material is never requested from custody.
func New(root custody.Handle, provider custody.Provider) (*Manager, error) {
	if provider == nil {
		return nil, fmt.Errorf("%w: custody provider is required", ErrInvalidRequest)
	}
	if err := root.Validate(); err != nil || root.Kind != custody.Key {
		return nil, fmt.Errorf("%w: root custody key reference is invalid", ErrInvalidRequest)
	}
	return &Manager{provider: provider, root: root, keys: make(map[string]tenantKeys), clock: func() time.Time { return time.Now().UTC() }}, nil
}

// NewManager is an explicit alias for New.
func NewManager(root custody.Handle, provider custody.Provider) (*Manager, error) {
	return New(root, provider)
}

// Root returns the opaque root custody reference.
func (m *Manager) Root() custody.Handle {
	if m == nil {
		return custody.Handle{}
	}
	return m.root
}

// RegisterTenant adds a tenant KEK below the root custody reference. The
// provider remains authoritative for the KEK lifecycle and tenant scope.
func (m *Manager) RegisterTenant(ctx custody.Context, tenant string, kek custody.Handle) error {
	if m == nil {
		return fmt.Errorf("%w: nil manager", ErrInvalidRequest)
	}
	if err := ctx.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if strings.TrimSpace(tenant) == "" || tenant != ctx.Tenant || kek.Kind != custody.Key || kek.Tenant != tenant || kek.Region != ctx.Region {
		return fmt.Errorf("%w: tenant and KEK scope do not match", ErrInvalidRequest)
	}
	if err := kek.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.keys[tenant]; exists {
		return fmt.Errorf("%w: tenant KEK already registered", ErrInvalidRequest)
	}
	m.keys[tenant] = tenantKeys{Root: m.root, Current: kek, History: []custody.Handle{kek}}
	return nil
}

// RegisterTenantKEK is a descriptive alias for RegisterTenant.
func (m *Manager) RegisterTenantKEK(ctx custody.Context, tenant string, kek custody.Handle) error {
	return m.RegisterTenant(ctx, tenant, kek)
}

// Encrypt seals plaintext for the tenant in ctx. objectID is an opaque
// logical identifier and is not used as cryptographic key material.
func (m *Manager) Encrypt(ctx custody.Context, objectID string, plaintext []byte) (Envelope, Evidence, error) {
	if err := m.validateContext(ctx); err != nil {
		return Envelope{}, Evidence{}, err
	}
	if strings.TrimSpace(objectID) == "" {
		return Envelope{}, Evidence{}, fmt.Errorf("%w: object id is required", ErrInvalidRequest)
	}
	kek, err := m.currentKey(ctx.Tenant)
	if err != nil {
		return Envelope{}, Evidence{}, err
	}
	dek := make([]byte, keyBytes)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return Envelope{}, Evidence{}, fmt.Errorf("%w: generate DEK: %v", ErrInvalidRequest, err)
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return Envelope{}, Evidence{}, fmt.Errorf("%w: create AES-GCM: %v", ErrInvalidRequest, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, Evidence{}, fmt.Errorf("%w: create AES-GCM: %v", ErrInvalidRequest, err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Envelope{}, Evidence{}, fmt.Errorf("%w: generate nonce: %v", ErrInvalidRequest, err)
	}
	dekIDBytes, err := randomBytes(16)
	if err != nil {
		return Envelope{}, Evidence{}, fmt.Errorf("%w: generate DEK id: %v", ErrInvalidRequest, err)
	}
	header := Header{Version: Version, Tenant: ctx.Tenant, ObjectID: objectID, KEKID: kek.ID, KEKVersion: kek.Version, DEKID: "dek-" + hex.EncodeToString(dekIDBytes), Algorithm: dataAlgorithm, Nonce: nonce}
	aad, err := headerBytes(header)
	if err != nil {
		return Envelope{}, Evidence{}, err
	}
	data := gcm.Seal(nil, nonce, plaintext, aad)
	wrapped, receipt, err := m.provider.Encrypt(ctx, kek, dek)
	if err != nil {
		return Envelope{}, Evidence{}, fmt.Errorf("%w: wrap DEK: %v", ErrInvalidCiphertext, err)
	}
	if wrapped.Handle != kek || len(wrapped.Data) == 0 {
		return Envelope{}, Evidence{}, fmt.Errorf("%w: custody returned an unbound DEK wrapper", ErrInvalidCiphertext)
	}
	result := Envelope{Header: header, WrappedDEK: wrapped, Data: data}
	return result, Evidence{Operation: custody.Encrypt, Tenant: ctx.Tenant, KEKID: kek.ID, KEKVersion: kek.Version, DEKID: header.DEKID, At: m.clock().UTC(), ContextDigest: custody.ContextDigest(ctx.RequestContext), ReceiptID: receipt.ID}, nil
}

// Seal is the concise alias for Encrypt.
func (m *Manager) Seal(ctx custody.Context, objectID string, plaintext []byte) (Envelope, Evidence, error) {
	return m.Encrypt(ctx, objectID, plaintext)
}

// Decrypt authenticates the header and opens an envelope only when its
// tenant, KEK reference, and custody context all agree. Object-bound
// envelopes require the expected objectID so a caller cannot accidentally
// open a valid envelope belonging to another object. The optional argument
// preserves source compatibility and supports legacy envelopes whose header
// predates object identity binding.
func (m *Manager) Decrypt(ctx custody.Context, envelope Envelope, objectID ...string) ([]byte, Evidence, error) {
	if err := m.validateContext(ctx); err != nil {
		return nil, Evidence{}, err
	}
	if err := validateEnvelope(envelope); err != nil {
		return nil, Evidence{}, err
	}
	if envelope.Header.Tenant != ctx.Tenant || envelope.WrappedDEK.Handle.Tenant != ctx.Tenant {
		return nil, Evidence{}, ErrTenantMismatch
	}
	if (len(objectID) == 0 && envelope.Header.ObjectID != "") || len(objectID) > 1 || (len(objectID) == 1 && (strings.TrimSpace(objectID[0]) == "" || envelope.Header.ObjectID != objectID[0])) {
		return nil, Evidence{}, fmt.Errorf("%w: %w: expected %q, envelope names %q", ErrInvalidCiphertext, ErrObjectMismatch, firstObjectID(objectID), envelope.Header.ObjectID)
	}
	if len(objectID) == 1 && envelope.Header.ObjectID == "" {
		return nil, Evidence{}, fmt.Errorf("%w: %w: legacy envelope has no object identity", ErrInvalidCiphertext, ErrObjectMismatch)
	}
	kek, err := m.keyFor(ctx.Tenant, envelope.Header.KEKID, envelope.Header.KEKVersion)
	if err != nil {
		return nil, Evidence{}, err
	}
	if envelope.WrappedDEK.Handle != kek {
		return nil, Evidence{}, fmt.Errorf("%w: wrapper handle does not match authenticated header", ErrInvalidCiphertext)
	}
	dek, receipt, err := m.provider.Decrypt(ctx, kek, envelope.WrappedDEK)
	if err != nil {
		return nil, Evidence{}, fmt.Errorf("%w: unwrap DEK: %v", ErrInvalidCiphertext, err)
	}
	if len(dek) != keyBytes {
		return nil, Evidence{}, fmt.Errorf("%w: custody returned an invalid DEK", ErrInvalidCiphertext)
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, Evidence{}, fmt.Errorf("%w: create AES-GCM: %v", ErrInvalidCiphertext, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, Evidence{}, fmt.Errorf("%w: create AES-GCM: %v", ErrInvalidCiphertext, err)
	}
	aad, err := headerBytes(envelope.Header)
	if err != nil {
		return nil, Evidence{}, err
	}
	plaintext, err := gcm.Open(nil, envelope.Header.Nonce, envelope.Data, aad)
	if err != nil {
		return nil, Evidence{}, fmt.Errorf("%w: authenticate data: %v", ErrInvalidCiphertext, err)
	}
	return plaintext, Evidence{Operation: custody.Decrypt, Tenant: ctx.Tenant, KEKID: kek.ID, KEKVersion: kek.Version, DEKID: envelope.Header.DEKID, At: m.clock().UTC(), ContextDigest: custody.ContextDigest(ctx.RequestContext), ReceiptID: receipt.ID}, nil
}

// Open is the concise alias for Decrypt.
func (m *Manager) Open(ctx custody.Context, envelope Envelope, objectID ...string) ([]byte, Evidence, error) {
	return m.Decrypt(ctx, envelope, objectID...)
}

func firstObjectID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// RotateKEK rotates the current tenant KEK through custody and rewraps the
// supplied envelopes. The data ciphertext and header DEKID remain unchanged;
// only the opaque wrapper and KEK version change. Passing no envelope is valid
// and rotates the hierarchy for future objects.
func (m *Manager) RotateKEK(ctx custody.Context, batches ...[]Envelope) ([]Envelope, Evidence, error) {
	if err := m.validateContext(ctx); err != nil {
		return nil, Evidence{}, err
	}
	m.mu.RLock()
	state, ok := m.keys[ctx.Tenant]
	m.mu.RUnlock()
	if !ok {
		return nil, Evidence{}, ErrKeyUnavailable
	}
	var input []Envelope
	if len(batches) > 0 {
		input = append([]Envelope(nil), batches[0]...)
	}
	deks := make([][]byte, len(input))
	for i, env := range input {
		if env.Header.Tenant != ctx.Tenant {
			return nil, Evidence{}, ErrTenantMismatch
		}
		dek, _, err := m.unwrap(ctx, env, state.Current)
		if err != nil {
			return nil, Evidence{}, err
		}
		deks[i] = dek
	}
	next, receipt, err := m.provider.Rotate(ctx, state.Current)
	if err != nil {
		return nil, Evidence{}, fmt.Errorf("%w: custody rotate: %v", ErrKeyRotation, err)
	}
	if next.Kind != custody.Key || next.Tenant != ctx.Tenant || next.Version == state.Current.Version {
		return nil, Evidence{}, fmt.Errorf("%w: custody returned an invalid next KEK", ErrKeyRotation)
	}
	updated := make([]Envelope, len(input))
	for i, env := range input {
		wrapped, _, err := m.provider.Encrypt(ctx, next, deks[i])
		if err != nil {
			return nil, Evidence{}, fmt.Errorf("%w: rewrap DEK %s: %v", ErrKeyRotation, env.Header.DEKID, err)
		}
		if wrapped.Handle != next {
			return nil, Evidence{}, fmt.Errorf("%w: rewrap returned an unbound wrapper", ErrKeyRotation)
		}
		env.Header.KEKID = next.ID
		env.Header.KEKVersion = next.Version
		env.WrappedDEK = wrapped
		updated[i] = env
	}
	m.mu.Lock()
	state.Current = next
	state.History = append(state.History, next)
	m.keys[ctx.Tenant] = state
	m.mu.Unlock()
	return updated, Evidence{Operation: custody.Rotate, Tenant: ctx.Tenant, KEKID: next.ID, KEKVersion: next.Version, At: m.clock().UTC(), ContextDigest: custody.ContextDigest(ctx.RequestContext), ReceiptID: receipt.ID}, nil
}

// RotateTenantKEK rotates a tenant KEK without rewrapping an object list.
// Call Rewrap for each existing envelope that must move to the new version.
func (m *Manager) RotateTenantKEK(ctx custody.Context) (custody.Handle, Evidence, error) {
	updated, ev, err := m.RotateKEK(ctx)
	if err != nil {
		return custody.Handle{}, ev, err
	}
	_ = updated
	kek, err := m.currentKey(ctx.Tenant)
	return kek, ev, err
}

// Rewrap moves one existing envelope to the current KEK without decrypting
// its data layer. The returned envelope has identical Data and DEKID.
func (m *Manager) Rewrap(ctx custody.Context, envelope Envelope) (Envelope, Evidence, error) {
	if err := m.validateContext(ctx); err != nil {
		return Envelope{}, Evidence{}, err
	}
	current, err := m.currentKey(ctx.Tenant)
	if err != nil {
		return Envelope{}, Evidence{}, err
	}
	if envelope.Header.Tenant != ctx.Tenant {
		return Envelope{}, Evidence{}, ErrTenantMismatch
	}
	key, err := m.keyFor(ctx.Tenant, envelope.Header.KEKID, envelope.Header.KEKVersion)
	if err != nil {
		return Envelope{}, Evidence{}, err
	}
	dek, _, err := m.unwrap(ctx, envelope, key)
	if err != nil {
		return Envelope{}, Evidence{}, err
	}
	wrapped, receipt, err := m.provider.Encrypt(ctx, current, dek)
	if err != nil {
		return Envelope{}, Evidence{}, fmt.Errorf("%w: custody rewrap: %v", ErrKeyRotation, err)
	}
	envelope.Header.KEKID, envelope.Header.KEKVersion = current.ID, current.Version
	envelope.WrappedDEK = wrapped
	return envelope, Evidence{Operation: custody.Rotate, Tenant: ctx.Tenant, KEKID: current.ID, KEKVersion: current.Version, DEKID: envelope.Header.DEKID, At: m.clock().UTC(), ContextDigest: custody.ContextDigest(ctx.RequestContext), ReceiptID: receipt.ID}, nil
}

func (m *Manager) validateContext(ctx custody.Context) error {
	if m == nil {
		return fmt.Errorf("%w: nil manager", ErrInvalidRequest)
	}
	if err := ctx.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return nil
}

func (m *Manager) currentKey(tenant string) (custody.Handle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.keys[tenant]
	if !ok {
		return custody.Handle{}, ErrKeyUnavailable
	}
	return state.Current, nil
}

func (m *Manager) keyFor(tenant, id, version string) (custody.Handle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.keys[tenant]
	if !ok {
		return custody.Handle{}, ErrKeyUnavailable
	}
	for _, key := range state.History {
		if key.ID == id && key.Version == version {
			return key, nil
		}
	}
	return custody.Handle{}, fmt.Errorf("%w: %s/%s", ErrKeyUnavailable, id, version)
}

func (m *Manager) unwrap(ctx custody.Context, env Envelope, expected custody.Handle) ([]byte, custody.Receipt, error) {
	if err := validateEnvelope(env); err != nil {
		return nil, custody.Receipt{}, err
	}
	if expected.Validate() != nil || env.WrappedDEK.Handle != expected || env.Header.KEKID != expected.ID || env.Header.KEKVersion != expected.Version {
		return nil, custody.Receipt{}, fmt.Errorf("%w: wrapper is not bound to the requested KEK version", ErrInvalidCiphertext)
	}
	dek, receipt, err := m.provider.Decrypt(ctx, expected, env.WrappedDEK)
	if err != nil {
		return nil, custody.Receipt{}, fmt.Errorf("%w: unwrap DEK: %v", ErrInvalidCiphertext, err)
	}
	if len(dek) != keyBytes {
		return nil, custody.Receipt{}, fmt.Errorf("%w: invalid DEK returned by custody", ErrInvalidCiphertext)
	}
	return dek, receipt, nil
}

func validateEnvelope(env Envelope) error {
	if env.Header.Version != Version || strings.TrimSpace(env.Header.Tenant) == "" || strings.TrimSpace(env.Header.KEKID) == "" || strings.TrimSpace(env.Header.KEKVersion) == "" || strings.TrimSpace(env.Header.DEKID) == "" || env.Header.Algorithm != dataAlgorithm || len(env.Header.Nonce) != 12 || len(env.Data) == 0 {
		return fmt.Errorf("%w: malformed authenticated header or data", ErrInvalidCiphertext)
	}
	if err := env.WrappedDEK.Handle.Validate(); err != nil || len(env.WrappedDEK.Data) == 0 {
		return fmt.Errorf("%w: malformed wrapped DEK", ErrInvalidCiphertext)
	}
	return nil
}

func headerBytes(header Header) ([]byte, error) {
	// KEK identity is deliberately excluded from the data-layer AAD. A KEK
	// rotation changes only the wrapper and those two header fields; binding
	// them here would make rewrapping require data re-encryption. The wrapper
	// handle and custody operation authenticate the KEK identity separately.
	data, err := json.Marshal(struct {
		Version   int    `json:"version"`
		Tenant    string `json:"tenant"`
		ObjectID  string `json:"object_id,omitempty"`
		DEKID     string `json:"dek_id"`
		Algorithm string `json:"algorithm"`
		Nonce     []byte `json:"nonce"`
	}{Version: header.Version, Tenant: header.Tenant, ObjectID: header.ObjectID, DEKID: header.DEKID, Algorithm: header.Algorithm, Nonce: header.Nonce})
	if err != nil {
		return nil, fmt.Errorf("%w: encode authenticated header: %v", ErrInvalidCiphertext, err)
	}
	return data, nil
}

func randomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// Explain describes the hierarchy and its security boundary for policy and
// operator evidence.
func Explain() string {
	return "envelope: root custody key reference -> tenant KEK versions -> per-object AES-GCM DEKs; DEKs are wrapped through custody, headers bind tenant/object/KEK version/DEK id, and KEK rotation rewraps without re-encrypting data"
}
