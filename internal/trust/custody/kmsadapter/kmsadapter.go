// Package kmsadapter adapts a provider-neutral KMS client to the custody
// ports. The client exposes only KMS-shaped operations and returns opaque key
// version metadata, wrapped data, signatures, and an explicitly transient
// unwrap result. Long-lived key material remains inside the KMS implementation.
package kmsadapter

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

var (
	ErrInvalidRequest     = errors.New("kmsadapter: invalid request")
	ErrTenantMismatch     = errors.New("kmsadapter: tenant field mismatch")
	ErrResidencyMismatch  = errors.New("kmsadapter: residency field mismatch")
	ErrKeyNotFound        = errors.New("kmsadapter: key version not found")
	ErrTenantIsolation    = errors.New("kmsadapter: tenant-scoped key isolation refused")
	ErrKeyRevoked         = errors.New("kmsadapter: key version is revoked")
	ErrKeyUnavailable     = errors.New("kmsadapter: key version is unavailable")
	ErrProviderFailure    = errors.New("kmsadapter: provider client failure")
	ErrInvalidLease       = errors.New("kmsadapter: invalid lease")
	ErrLeaseUnknown       = errors.New("kmsadapter: unknown lease")
	ErrLeaseRevoked       = errors.New("kmsadapter: lease is revoked")
	ErrLeaseExpired       = errors.New("kmsadapter: lease is expired")
	ErrLeaseTampered      = errors.New("kmsadapter: lease copy is tampered")
	ErrUnsupportedKeyType = errors.New("kmsadapter: unsupported public key type")
)

// KeyVersion is the only key identity that crosses ProviderClient. Tenant and
// Residency are mandatory parts of the reference; an ID alone is never a
// valid KMS request. PublicKey is public metadata used for local signature
// verification and certificate consumers.
type KeyVersion struct {
	ID        string
	Tenant    string
	Residency string
	Version   string
	Algorithm string
	PublicKey crypto.PublicKey
}

func (k KeyVersion) validate() error {
	if strings.TrimSpace(k.ID) == "" || strings.TrimSpace(k.Tenant) == "" || strings.TrimSpace(k.Residency) == "" || strings.TrimSpace(k.Version) == "" {
		return fmt.Errorf("%w: key id, tenant, residency and version are required", ErrInvalidRequest)
	}
	return nil
}

// CreateKeyRequest contains metadata only. A provider chooses and retains the
// private or symmetric key material.
type CreateKeyRequest struct {
	ID            string
	Tenant        string
	Residency     string
	Purpose       string
	Kind          custody.Kind
	ProviderClass custody.ProviderClass
}

// WrapRequest asks a KMS to wrap an ephemeral data key. Plaintext is limited
// to the transient DEK supplied by custody's envelope layer; the KMS master
// key never crosses this port.
type WrapRequest struct {
	Key       KeyVersion
	Plaintext []byte
}

// WrappedValue is provider-neutral wrapped data. It has no unwrapped key.
type WrappedValue struct {
	Key        KeyVersion
	Ciphertext []byte
	Algorithm  string
}

// UnwrapRequest identifies an opaque wrapped value and its tenant scope.
type UnwrapRequest struct {
	Key        KeyVersion
	Ciphertext []byte
	Algorithm  string
}

// UnwrappedValue contains only the transient result of a KMS unwrap operation.
// It is not a long-lived custody key and is immediately consumed by the
// envelope layer. The adapter never stores it or puts it in evidence.
type UnwrappedValue struct {
	Key       KeyVersion
	Plaintext []byte
}

// SignRequest asks a KMS to sign an opaque message or digest.
type SignRequest struct {
	Key     KeyVersion
	Message []byte
}

type SignedValue struct {
	Key       KeyVersion
	Signature []byte
	Algorithm string
}

type RotateRequest struct {
	Key KeyVersion
}

type ListVersionsRequest struct {
	ID        string
	Tenant    string
	Residency string
}

// ProviderClient is the deliberately narrow KMS/HSM seam. It contains
// exactly the six operations needed by the adapter and no provider SDK types:
// create key, wrap, unwrap, sign, rotate, and list versions.
type ProviderClient interface {
	CreateKey(CreateKeyRequest) (KeyVersion, error)
	Wrap(WrapRequest) (WrappedValue, error)
	Unwrap(UnwrapRequest) (UnwrappedValue, error)
	Sign(SignRequest) (SignedValue, error)
	Rotate(RotateRequest) (KeyVersion, error)
	ListVersions(ListVersionsRequest) ([]KeyVersion, error)
}

// Option configures an Adapter without coupling it to a provider SDK.
type Option func(*Adapter)

// WithClock supplies a deterministic clock for tests and evidence.
func WithClock(clock func() time.Time) Option {
	return func(a *Adapter) {
		if clock != nil {
			a.clock = clock
		}
	}
}

type leaseRecord struct {
	lease   custody.Lease
	revoked bool
	used    bool
}

type typedRevision struct {
	lifecycle custody.Lifecycle
	digest    string
}

type lifecycleRevision struct {
	event custody.KeyLifecycleEvent
}

// Adapter is a custody Provider, lifecycle provider, and short-lived lease
// issuer backed by one ProviderClient. The typed metadata port is exposed by
// Metadata because custody.Provider and custody.MetadataCustody deliberately
// use different Rotate result types and therefore cannot be implemented by one
// Go method set.
// Local state contains only revocation/lease metadata and immutable digested
// revisions; key material is never stored here.
type Adapter struct {
	client ProviderClient
	clock  func() time.Time

	mu       sync.Mutex
	revoked  map[custody.Handle]string
	typed    map[custody.Handle][]typedRevision
	lifecycl map[custody.KeyHandle][]lifecycleRevision
	leases   map[string]leaseRecord
}

// New constructs an adapter over a provider-neutral client.
func New(client ProviderClient, options ...Option) (*Adapter, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: provider client is required", ErrInvalidRequest)
	}
	a := &Adapter{
		client:   client,
		clock:    func() time.Time { return time.Now().UTC() },
		revoked:  make(map[custody.Handle]string),
		typed:    make(map[custody.Handle][]typedRevision),
		lifecycl: make(map[custody.KeyHandle][]lifecycleRevision),
		leases:   make(map[string]leaseRecord),
	}
	for _, option := range options {
		if option != nil {
			option(a)
		}
	}
	return a, nil
}

func (a *Adapter) now() time.Time { return a.clock().UTC() }

func keyVersion(h custody.Handle) KeyVersion {
	return KeyVersion{ID: h.ID, Tenant: h.Tenant, Residency: h.Region, Version: h.Version}
}

func (a *Adapter) validate(ctx custody.Context, handle custody.Handle) error {
	if a == nil || a.client == nil {
		return ErrInvalidRequest
	}
	if err := ctx.Validate(); err != nil {
		return fmt.Errorf("%w: custody context: %v", ErrInvalidRequest, err)
	}
	if err := handle.Validate(); err != nil {
		return fmt.Errorf("%w: custody handle: %v", ErrInvalidRequest, err)
	}
	if ctx.Tenant != handle.Tenant {
		return fmt.Errorf("%w: tenant field context=%q handle=%q", ErrTenantMismatch, ctx.Tenant, handle.Tenant)
	}
	if ctx.Region != handle.Region {
		return fmt.Errorf("%w: residency field context=%q handle=%q", ErrResidencyMismatch, ctx.Region, handle.Region)
	}
	return a.checkRevoked(handle)
}

func (a *Adapter) checkRevoked(handle custody.Handle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if reason, ok := a.revoked[handle]; ok {
		return fmt.Errorf("%w: reason=%s", ErrKeyRevoked, reason)
	}
	return nil
}

func (a *Adapter) lookup(handle custody.Handle) (KeyVersion, error) {
	ref := keyVersion(handle)
	versions, err := a.client.ListVersions(ListVersionsRequest{ID: ref.ID, Tenant: ref.Tenant, Residency: ref.Residency})
	if err != nil {
		return KeyVersion{}, fmt.Errorf("%w: list versions: %v", ErrProviderFailure, err)
	}
	var match *KeyVersion
	for _, version := range versions {
		if version.ID == ref.ID && version.Version == ref.Version && version.Tenant != ref.Tenant {
			return KeyVersion{}, fmt.Errorf("%w: tenant field returned by provider", ErrTenantIsolation)
		}
		if version.ID == ref.ID && version.Version == ref.Version && version.Residency != ref.Residency {
			return KeyVersion{}, fmt.Errorf("%w: residency field returned by provider", ErrResidencyMismatch)
		}
		if version.ID == ref.ID && version.Tenant == ref.Tenant && version.Residency == ref.Residency && version.Version == ref.Version {
			copy := version
			match = &copy
		}
	}
	if match != nil {
		return *match, nil
	}
	return KeyVersion{}, fmt.Errorf("%w: %s/%s", ErrKeyNotFound, handle.ID, handle.Version)
}

func validateReturned(ref, want KeyVersion) error {
	if err := ref.validate(); err != nil {
		return fmt.Errorf("%w: provider returned invalid key reference", ErrProviderFailure)
	}
	if ref.ID != want.ID {
		return fmt.Errorf("%w: key id field returned %q for %q", ErrTenantIsolation, ref.ID, want.ID)
	}
	if ref.Tenant != want.Tenant {
		return fmt.Errorf("%w: tenant field returned %q for %q", ErrTenantIsolation, ref.Tenant, want.Tenant)
	}
	if ref.Residency != want.Residency {
		return fmt.Errorf("%w: residency field returned %q for %q", ErrResidencyMismatch, ref.Residency, want.Residency)
	}
	return nil
}

func (a *Adapter) receipt(ctx custody.Context, handle custody.Handle, op custody.Operation) custody.Receipt {
	at := a.now()
	return custody.Receipt{ID: fmt.Sprintf("kms:%s:%s:%d", op, handle.ID, at.UnixNano()), Handle: handle, Operation: op, ContextDigest: custody.ContextDigest(ctx.RequestContext), At: at}
}

// Encrypt wraps an ephemeral DEK through the KMS.
func (a *Adapter) Encrypt(ctx custody.Context, handle custody.Handle, plaintext []byte) (custody.Ciphertext, custody.Receipt, error) {
	if err := a.validate(ctx, handle); err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	key, err := a.lookup(handle)
	if err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	if len(plaintext) == 0 {
		return custody.Ciphertext{}, custody.Receipt{}, fmt.Errorf("%w: plaintext field is required", ErrInvalidRequest)
	}
	wrapped, err := a.client.Wrap(WrapRequest{Key: key, Plaintext: append([]byte(nil), plaintext...)})
	if err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, fmt.Errorf("%w: wrap: %v", ErrProviderFailure, err)
	}
	if err := validateReturned(wrapped.Key, key); err != nil || len(wrapped.Ciphertext) == 0 {
		if err == nil {
			err = fmt.Errorf("%w: ciphertext field is empty", ErrProviderFailure)
		}
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	return custody.Ciphertext{Handle: handle, Algorithm: wrapped.Algorithm, Data: append([]byte(nil), wrapped.Ciphertext...)}, a.receipt(ctx, handle, custody.Encrypt), nil
}

// Decrypt unwraps an ephemeral DEK. The result is returned only to the
// immediate caller and is never retained or included in a receipt.
func (a *Adapter) Decrypt(ctx custody.Context, handle custody.Handle, ciphertext custody.Ciphertext) ([]byte, custody.Receipt, error) {
	if err := a.validate(ctx, handle); err != nil {
		return nil, custody.Receipt{}, err
	}
	if ciphertext.Handle != handle || len(ciphertext.Data) == 0 {
		return nil, custody.Receipt{}, fmt.Errorf("%w: ciphertext handle or data field is invalid", ErrInvalidRequest)
	}
	key, err := a.lookup(handle)
	if err != nil {
		return nil, custody.Receipt{}, err
	}
	unwrapped, err := a.client.Unwrap(UnwrapRequest{Key: key, Ciphertext: append([]byte(nil), ciphertext.Data...), Algorithm: ciphertext.Algorithm})
	if err != nil {
		return nil, custody.Receipt{}, fmt.Errorf("%w: unwrap: %v", ErrProviderFailure, err)
	}
	if err := validateReturned(unwrapped.Key, key); err != nil || len(unwrapped.Plaintext) == 0 {
		if err == nil {
			err = fmt.Errorf("%w: unwrap plaintext field is empty", ErrProviderFailure)
		}
		return nil, custody.Receipt{}, err
	}
	return append([]byte(nil), unwrapped.Plaintext...), a.receipt(ctx, handle, custody.Decrypt), nil
}

// Sign delegates signing to the KMS and returns only the signature bytes.
func (a *Adapter) Sign(ctx custody.Context, handle custody.Handle, message []byte) (custody.Signature, custody.Receipt, error) {
	if err := a.validate(ctx, handle); err != nil {
		return custody.Signature{}, custody.Receipt{}, err
	}
	if len(message) == 0 {
		return custody.Signature{}, custody.Receipt{}, fmt.Errorf("%w: message field is required", ErrInvalidRequest)
	}
	key, err := a.lookup(handle)
	if err != nil {
		return custody.Signature{}, custody.Receipt{}, err
	}
	signed, err := a.client.Sign(SignRequest{Key: key, Message: append([]byte(nil), message...)})
	if err != nil {
		return custody.Signature{}, custody.Receipt{}, fmt.Errorf("%w: sign: %v", ErrProviderFailure, err)
	}
	if err := validateReturned(signed.Key, key); err != nil || len(signed.Signature) == 0 {
		if err == nil {
			err = fmt.Errorf("%w: signature field is empty", ErrProviderFailure)
		}
		return custody.Signature{}, custody.Receipt{}, err
	}
	return custody.Signature{Handle: handle, Algorithm: signed.Algorithm, Data: append([]byte(nil), signed.Signature...)}, a.receipt(ctx, handle, custody.Sign), nil
}

// Verify verifies a KMS signature using public metadata returned by the KMS.
// The client port remains limited to the six KMS operations and needs no
// provider-specific Verify operation.
func (a *Adapter) Verify(ctx custody.Context, handle custody.Handle, message []byte, signature custody.Signature) (bool, custody.Receipt, error) {
	if err := a.validate(ctx, handle); err != nil {
		return false, custody.Receipt{}, err
	}
	if signature.Handle != handle || len(message) == 0 || len(signature.Data) == 0 {
		return false, custody.Receipt{}, fmt.Errorf("%w: signature handle, message or data field is invalid", ErrInvalidRequest)
	}
	key, err := a.lookup(handle)
	if err != nil {
		return false, custody.Receipt{}, err
	}
	ok, err := verifyPublic(key.PublicKey, message, signature.Data)
	if err != nil {
		return false, custody.Receipt{}, err
	}
	return ok, a.receipt(ctx, handle, custody.Verify), nil
}

func verifyPublic(public crypto.PublicKey, message, signature []byte) (bool, error) {
	switch key := public.(type) {
	case ed25519.PublicKey:
		if len(key) != ed25519.PublicKeySize {
			return false, fmt.Errorf("%w: invalid ed25519 public key length %d", ErrUnsupportedKeyType, len(key))
		}
		return ed25519.Verify(key, message, signature), nil
	case *ecdsa.PublicKey:
		//lint:ignore SA1019 read-only nil guard: the coordinates are only inspected for nil (never modified or operated on) to fail malformed keys closed before VerifyASN1. owner=security-cryptography expires=2027-03-24
		if key == nil || key.Curve == nil || key.X == nil || key.Y == nil {
			return false, fmt.Errorf("%w: malformed ecdsa public key", ErrUnsupportedKeyType)
		}
		return ecdsa.VerifyASN1(key, message, signature), nil
	default:
		return false, fmt.Errorf("%w: public key type %T", ErrUnsupportedKeyType, public)
	}
}

// IssueLease issues a short-lived, single-use custody lease. It contains no
// credential bytes and is backed only by immutable local lease metadata.
func (a *Adapter) IssueLease(ctx custody.Context, handle custody.Handle, operation custody.Operation, ttl time.Duration) (custody.Lease, error) {
	if err := a.validate(ctx, handle); err != nil {
		return custody.Lease{}, err
	}
	if !validOperation(operation) || ttl <= 0 {
		return custody.Lease{}, fmt.Errorf("%w: operation or ttl field is invalid", ErrInvalidLease)
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return custody.Lease{}, fmt.Errorf("%w: generate lease id: %v", ErrInvalidLease, err)
	}
	lease := custody.Lease{ID: "kms-lease-" + hex.EncodeToString(idBytes), Handle: handle, Operation: operation, ExpiresAt: a.now().Add(ttl), ContextDigest: custody.ContextDigest(ctx.RequestContext)}
	a.mu.Lock()
	a.leases[lease.ID] = leaseRecord{lease: lease}
	a.mu.Unlock()
	return lease, nil
}

func (a *Adapter) RenewLease(ctx custody.Context, presented custody.Lease, ttl time.Duration) (custody.Lease, error) {
	if err := a.validate(ctx, presented.Handle); err != nil {
		return custody.Lease{}, err
	}
	if ttl <= 0 {
		return custody.Lease{}, fmt.Errorf("%w: ttl field is invalid", ErrInvalidLease)
	}
	if presented.ContextDigest != custody.ContextDigest(ctx.RequestContext) {
		return custody.Lease{}, ErrLeaseTampered
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.leases[presented.ID]
	if !ok {
		return custody.Lease{}, ErrLeaseUnknown
	}
	if record.lease != presented {
		return custody.Lease{}, ErrLeaseTampered
	}
	if record.revoked {
		return custody.Lease{}, ErrLeaseRevoked
	}
	if record.used || !a.now().Before(record.lease.ExpiresAt) {
		return custody.Lease{}, ErrLeaseExpired
	}
	updated := record.lease
	updated.ExpiresAt = a.now().Add(ttl)
	updated.ContextDigest = custody.ContextDigest(ctx.RequestContext)
	a.leases[updated.ID] = leaseRecord{lease: updated, revoked: record.revoked, used: record.used}
	return updated, nil
}

// Rotate creates a new KMS version and returns its opaque handle.
func (a *Adapter) Rotate(ctx custody.Context, handle custody.Handle) (custody.Handle, custody.Receipt, error) {
	if err := a.validate(ctx, handle); err != nil {
		return custody.Handle{}, custody.Receipt{}, err
	}
	key, err := a.lookup(handle)
	if err != nil {
		return custody.Handle{}, custody.Receipt{}, err
	}
	next, err := a.client.Rotate(RotateRequest{Key: key})
	if err != nil {
		return custody.Handle{}, custody.Receipt{}, fmt.Errorf("%w: rotate: %v", ErrProviderFailure, err)
	}
	if err := validateReturned(next, key); err != nil {
		return custody.Handle{}, custody.Receipt{}, err
	}
	if next.Version == handle.Version {
		return custody.Handle{}, custody.Receipt{}, fmt.Errorf("%w: version field did not advance", ErrProviderFailure)
	}
	result := handle
	result.Version = next.Version
	a.appendTypedRotation(handle, result, next)
	return result, a.receipt(ctx, result, custody.Rotate), nil
}

// Revoke records a local, permanent revocation. KMS clients intentionally do
// not receive a synthetic operation outside the six-operation ProviderClient
// contract; production qualification must map this to the provider's own
// disable/destroy and revocation evidence semantics.
func (a *Adapter) Revoke(ctx custody.Context, handle custody.Handle, reason string) (custody.Receipt, error) {
	if err := a.validate(ctx, handle); err != nil {
		return custody.Receipt{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return custody.Receipt{}, fmt.Errorf("%w: reason field is required", ErrInvalidRequest)
	}
	a.mu.Lock()
	if _, exists := a.revoked[handle]; exists {
		a.mu.Unlock()
		return custody.Receipt{}, ErrKeyRevoked
	}
	a.revoked[handle] = reason
	a.mu.Unlock()
	a.appendTypedRevocation(handle, reason)
	return a.receipt(ctx, handle, custody.Revoke), nil
}

func validOperation(operation custody.Operation) bool {
	switch operation {
	case custody.Encrypt, custody.Decrypt, custody.Sign, custody.Verify, custody.LeaseOperation:
		return true
	default:
		return false
	}
}

func (a *Adapter) appendTypedRotation(oldHandle, newHandle custody.Handle, key KeyVersion) {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.latestTyped(oldHandle)
	old.Status = custody.StatusRotated
	old.UpdatedAt = a.now()
	a.typed[oldHandle] = append(a.typed[oldHandle], typedRevision{lifecycle: old, digest: revisionDigest(oldHandle, old.Status, old.UpdatedAt)})
	next := custody.Lifecycle{Handle: newHandle, Status: custody.StatusActive, Algorithm: key.Algorithm, PublicDigest: publicDigest(key.PublicKey), UpdatedAt: a.now()}
	a.typed[newHandle] = append(a.typed[newHandle], typedRevision{lifecycle: next, digest: revisionDigest(newHandle, next.Status, next.UpdatedAt)})
}

func (a *Adapter) appendTypedRevocation(handle custody.Handle, reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	lifecycle := a.latestTyped(handle)
	lifecycle.Status = custody.StatusRevoked
	lifecycle.UpdatedAt = a.now()
	a.typed[handle] = append(a.typed[handle], typedRevision{lifecycle: lifecycle, digest: revisionDigest(handle, lifecycle.Status, lifecycle.UpdatedAt) + ":" + reason})
}

func (a *Adapter) latestTyped(handle custody.Handle) custody.Lifecycle {
	revisions := a.typed[handle]
	if len(revisions) == 0 {
		return custody.Lifecycle{Handle: handle, Status: custody.StatusActive, UpdatedAt: a.now()}
	}
	return revisions[len(revisions)-1].lifecycle
}

func revisionDigest(handle custody.Handle, status string, at time.Time) string {
	data := fmt.Sprintf("kmsadapter/revision/v1\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", handle.ID, handle.Kind, handle.Version, handle.Tenant, handle.Region, status+"\x00"+at.UTC().Format(time.RFC3339Nano))
	return fmt.Sprintf("sha256:%x", custody.ContextDigest(custody.RequestContext{Workload: "kmsadapter", Tenant: handle.Tenant, Region: handle.Region, Purpose: string(handle.Kind), Destination: data}))
}

func publicDigest(public crypto.PublicKey) string {
	if public == nil {
		return ""
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("sha256:%x", der)
}

// Get returns metadata only for a typed custody handle.
func (a *Adapter) Get(ctx custody.Context, handle custody.Handle) (custody.Lifecycle, error) {
	if err := a.validate(ctx, handle); err != nil {
		if errors.Is(err, ErrKeyRevoked) {
			return custody.Lifecycle{}, custody.ErrObjectRevoked
		}
		return custody.Lifecycle{}, err
	}
	key, err := a.lookup(handle)
	if err != nil {
		return custody.Lifecycle{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	lifecycle := a.latestTyped(handle)
	if lifecycle.Status == custody.StatusRevoked {
		return custody.Lifecycle{}, custody.ErrObjectRevoked
	}
	if lifecycle.Handle == (custody.Handle{}) {
		lifecycle.Handle = handle
	}
	lifecycle.Algorithm = key.Algorithm
	lifecycle.PublicDigest = publicDigest(key.PublicKey)
	return lifecycle, nil
}

func (a *Adapter) Attest(ctx custody.Context, handle custody.Handle) (custody.Attestation, error) {
	lifecycle, err := a.Get(ctx, handle)
	if err != nil {
		return custody.Attestation{}, err
	}
	digest := revisionDigest(handle, lifecycle.Status, lifecycle.UpdatedAt)
	now := a.now()
	return custody.Attestation{Handle: handle, Status: lifecycle.Status, Digest: digest, AttestedAt: now, ValidUntil: now.Add(time.Minute)}, nil
}

func (a *Adapter) RotateTyped(ctx custody.Context, handle custody.Handle) (custody.Lifecycle, custody.Receipt, error) {
	next, receipt, err := a.Rotate(ctx, handle)
	if err != nil {
		if errors.Is(err, ErrKeyRevoked) {
			return custody.Lifecycle{}, custody.Receipt{}, custody.ErrObjectRevoked
		}
		return custody.Lifecycle{}, custody.Receipt{}, err
	}
	lifecycle, err := a.Get(ctx, next)
	return lifecycle, receipt, err
}

// MetadataAdapter is the typed custody view over an Adapter. It delegates to
// the same provider client and local immutable revision store.
type MetadataAdapter struct{ parent *Adapter }

// Metadata returns the typed key, secret, and certificate custody view.
func (a *Adapter) Metadata() *MetadataAdapter {
	if a == nil {
		return nil
	}
	return &MetadataAdapter{parent: a}
}

func (m *MetadataAdapter) Get(ctx custody.Context, handle custody.Handle) (custody.Lifecycle, error) {
	if m == nil || m.parent == nil {
		return custody.Lifecycle{}, ErrInvalidRequest
	}
	return m.parent.Get(ctx, handle)
}

func (m *MetadataAdapter) Rotate(ctx custody.Context, handle custody.Handle) (custody.Lifecycle, custody.Receipt, error) {
	if m == nil || m.parent == nil {
		return custody.Lifecycle{}, custody.Receipt{}, ErrInvalidRequest
	}
	return m.parent.RotateTyped(ctx, handle)
}

func (m *MetadataAdapter) Revoke(ctx custody.Context, handle custody.Handle, reason string) (custody.Receipt, error) {
	if m == nil || m.parent == nil {
		return custody.Receipt{}, ErrInvalidRequest
	}
	return m.parent.Revoke(ctx, handle, reason)
}

func (m *MetadataAdapter) Attest(ctx custody.Context, handle custody.Handle) (custody.Attestation, error) {
	if m == nil || m.parent == nil {
		return custody.Attestation{}, ErrInvalidRequest
	}
	return m.parent.Attest(ctx, handle)
}

func (a *Adapter) CreateKeyHandle(ctx custody.Context, handle custody.KeyHandle) (custody.KeyLifecycle, error) {
	if err := validateLifecycleContext(ctx, handle); err != nil {
		return custody.KeyLifecycle{}, err
	}
	created, err := a.client.CreateKey(CreateKeyRequest{ID: handle.ID, Tenant: handle.Tenant, Residency: ctx.Region, Purpose: handle.Purpose, Kind: custody.Key, ProviderClass: handle.ProviderClass})
	if err != nil {
		return custody.KeyLifecycle{}, fmt.Errorf("%w: create key: %v", ErrProviderFailure, err)
	}
	want := keyVersion(custody.Handle{ID: handle.ID, Kind: custody.Key, Version: handle.Version, Tenant: handle.Tenant, Region: ctx.Region})
	if err := validateReturned(created, want); err != nil {
		return custody.KeyLifecycle{}, err
	}
	return a.appendLifecycle(handle, custody.LifecycleCreate, custody.StatePending, "", ""), nil
}

func validateLifecycleContext(ctx custody.Context, handle custody.KeyHandle) error {
	if err := ctx.Validate(); err != nil {
		return fmt.Errorf("%w: custody context: %v", ErrInvalidRequest, err)
	}
	if err := handle.Validate(); err != nil {
		return err
	}
	if ctx.Tenant != handle.Tenant {
		return fmt.Errorf("%w: tenant field context=%q handle=%q", ErrTenantMismatch, ctx.Tenant, handle.Tenant)
	}
	return nil
}

func (a *Adapter) appendLifecycle(handle custody.KeyHandle, operation custody.LifecycleOperation, to custody.KeyState, evidence, authority string) custody.KeyLifecycle {
	a.mu.Lock()
	defer a.mu.Unlock()
	previous := a.lifecycl[handle]
	from := custody.KeyState("")
	if len(previous) > 0 {
		from = previous[len(previous)-1].event.To
	}
	event := custody.KeyLifecycleEvent{Handle: handle, Operation: operation, From: from, To: to, EvidenceDigest: evidence, AuthorityDigest: authority, At: a.now()}
	event.Digest = fmt.Sprintf("sha256:%x", custody.ContextDigest(custody.RequestContext{Workload: "kmsadapter-lifecycle", Tenant: handle.Tenant, Region: "lifecycle", Purpose: handle.Purpose, Destination: string(operation) + "\x00" + string(from) + "\x00" + string(to) + "\x00" + evidence + "\x00" + authority + "\x00" + event.At.Format(time.RFC3339Nano)}))
	a.lifecycl[handle] = append(previous, lifecycleRevision{event: event})
	return lifecycleView(handle, a.lifecycl[handle])
}

func lifecycleView(handle custody.KeyHandle, revisions []lifecycleRevision) custody.KeyLifecycle {
	events := make([]custody.KeyLifecycleEvent, 0, len(revisions))
	for _, revision := range revisions {
		events = append(events, revision.event)
	}
	state := custody.StatePending
	if len(events) > 0 {
		state = events[len(events)-1].To
	}
	return custody.KeyLifecycle{Handle: handle, State: state, Events: events, UpdatedAt: events[len(events)-1].At}
}

func (a *Adapter) ImportBYOK(ctx custody.Context, request custody.BYOKImportRequest) (custody.KeyLifecycle, error) {
	if request.Handle.ProviderClass != custody.ProviderBYOK {
		return custody.KeyLifecycle{}, fmt.Errorf("%w: provider class field must be BYOK", custody.ErrInvalidLifecycle)
	}
	if err := validateLifecycleContext(ctx, request.Handle); err != nil {
		return custody.KeyLifecycle{}, err
	}
	if strings.TrimSpace(request.WrappingProofDigest) == "" {
		return custody.KeyLifecycle{}, custody.ErrBYOKProofRequired
	}
	if request.Attestation.Handle != request.Handle || strings.TrimSpace(request.Attestation.EvidenceDigest) == "" || !a.now().Before(request.Attestation.ValidUntil) {
		return custody.KeyLifecycle{}, custody.ErrBYOKAttestationInvalid
	}
	a.mu.Lock()
	revisions := a.lifecycl[request.Handle]
	if len(revisions) == 0 {
		a.mu.Unlock()
		return custody.KeyLifecycle{}, custody.ErrKeyHandleNotFound
	}
	a.mu.Unlock()
	return a.appendLifecycle(request.Handle, custody.LifecycleImport, custody.StatePending, request.Attestation.EvidenceDigest, ""), nil
}

func (a *Adapter) ActivateKeyHandle(ctx custody.Context, handle custody.KeyHandle, evidence string) (custody.KeyLifecycle, error) {
	return a.transitionLifecycle(ctx, handle, custody.StateActive, evidence, custody.LifecycleActivate, custody.StatePending, custody.StateRotating)
}

func (a *Adapter) RotateKeyHandle(ctx custody.Context, handle custody.KeyHandle, evidence string) (custody.KeyHandle, error) {
	if strings.TrimSpace(evidence) == "" {
		return custody.KeyHandle{}, custody.ErrLifecycleEvidenceRequired
	}
	if err := validateLifecycleContext(ctx, handle); err != nil {
		return custody.KeyHandle{}, err
	}
	ref := custody.Handle{ID: handle.ID, Kind: custody.Key, Version: handle.Version, Tenant: handle.Tenant, Region: ctx.Region}
	next, _, err := a.Rotate(ctx, ref)
	if err != nil {
		return custody.KeyHandle{}, err
	}
	nextHandle := handle
	nextHandle.Version = next.Version
	a.appendLifecycle(handle, custody.LifecycleRotate, custody.StateRotating, evidence, "")
	a.appendLifecycle(nextHandle, custody.LifecycleRotate, custody.StatePending, evidence, "")
	return nextHandle, nil
}

func (a *Adapter) transitionLifecycle(ctx custody.Context, handle custody.KeyHandle, to custody.KeyState, evidence string, operation custody.LifecycleOperation, allowed ...custody.KeyState) (custody.KeyLifecycle, error) {
	if strings.TrimSpace(evidence) == "" {
		return custody.KeyLifecycle{}, custody.ErrLifecycleEvidenceRequired
	}
	if err := validateLifecycleContext(ctx, handle); err != nil {
		return custody.KeyLifecycle{}, err
	}
	a.mu.Lock()
	revisions := a.lifecycl[handle]
	current := custody.StatePending
	if len(revisions) > 0 {
		current = revisions[len(revisions)-1].event.To
	}
	a.mu.Unlock()
	for _, permitted := range allowed {
		if current == permitted {
			return a.appendLifecycle(handle, operation, to, evidence, ""), nil
		}
	}
	return custody.KeyLifecycle{}, fmt.Errorf("%w: current state %s", custody.ErrInvalidTransition, current)
}

func (a *Adapter) DisableKeyHandle(ctx custody.Context, handle custody.KeyHandle, evidence string) (custody.KeyLifecycle, error) {
	return a.transitionLifecycle(ctx, handle, custody.StateDisabled, evidence, custody.LifecycleDisable, custody.StateActive, custody.StateRotating)
}

func (a *Adapter) DestroyKeyHandle(ctx custody.Context, request custody.DestroyRequest) (custody.KeyLifecycle, error) {
	if err := validateLifecycleContext(ctx, request.Handle); err != nil {
		return custody.KeyLifecycle{}, err
	}
	if strings.TrimSpace(request.RequestedBy) == "" || strings.TrimSpace(request.Approver) == "" || request.RequestedBy == request.Approver {
		return custody.KeyLifecycle{}, custody.ErrDistinctApprover
	}
	if request.RetentionCheck.OnHold {
		return custody.KeyLifecycle{}, custody.ErrRetentionHold
	}
	if !request.RetentionCheck.Declared || strings.TrimSpace(request.RetentionCheck.EvidenceDigest) == "" || request.RetentionCheck.CheckedAt.IsZero() {
		return custody.KeyLifecycle{}, fmt.Errorf("%w: retention check fields are incomplete", custody.ErrInvalidLifecycle)
	}
	a.mu.Lock()
	revisions := a.lifecycl[request.Handle]
	from := custody.StatePending
	if len(revisions) > 0 {
		from = revisions[len(revisions)-1].event.To
	}
	a.mu.Unlock()
	if from == custody.StateDestroyed {
		return custody.KeyLifecycle{}, custody.ErrKeyHandleDestroyed
	}
	authority := fmt.Sprintf("sha256:%x", custody.ContextDigest(custody.RequestContext{Workload: request.RequestedBy, Tenant: request.Handle.Tenant, Region: ctx.Region, Purpose: request.Approver, Destination: "destroy"}))
	return a.appendLifecycle(request.Handle, custody.LifecycleDestroy, custody.StateDestroyed, request.RetentionCheck.EvidenceDigest, authority), nil
}

func (a *Adapter) KeyLifecycle(ctx custody.Context, handle custody.KeyHandle) (custody.KeyLifecycle, error) {
	if err := validateLifecycleContext(ctx, handle); err != nil {
		return custody.KeyLifecycle{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	revisions := a.lifecycl[handle]
	if len(revisions) == 0 {
		return custody.KeyLifecycle{}, custody.ErrKeyHandleNotFound
	}
	return lifecycleView(handle, revisions), nil
}

// Version identifies this adapter contract.
func Version() int { return 1 }

// Explain describes the KMS boundary without provider names or identifiers.
func Explain() string {
	return "kmsadapter: tenant-scoped residency-labelled KMS key versions; create, wrap, unwrap, sign, rotate and list versions cross a narrow provider port, while custody returns only opaque handles, ciphertext, signatures, leases and digested lifecycle evidence"
}

var (
	_ custody.Provider             = (*Adapter)(nil)
	_ custody.TypedProvider        = (*MetadataAdapter)(nil)
	_ custody.KeyLifecycleProvider = (*Adapter)(nil)
)
