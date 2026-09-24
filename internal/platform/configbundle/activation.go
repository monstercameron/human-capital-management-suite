package configbundle

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// ActivationPolicy is the verifier's trusted binding context. Empty policy
// fields are not wildcards for the dimensions that a caller supplies in an
// ActivationRequest; they simply mean the deployment has no additional
// fixed value for that dimension.
type ActivationPolicy struct {
	Scope             Scope
	Environment       string
	TrustProfile      string
	AntiRollbackFloor string
	// MinimumRuntimeVersion is a compatibility spelling for the floor.
	MinimumRuntimeVersion string
	RuntimeVersion        string
}

// ActivationRequest is the untrusted request to activate one signed bundle.
// Its issued/expiry window and epoch are checked before the registry changes
// state. ReceiptSigner is an in-process port and is never serialized.
type ActivationRequest struct {
	Bundle SignedBundle
	// SignedBundle is a compatibility spelling for Bundle.
	SignedBundle          SignedBundle
	Scope                 Scope
	TargetScope           Scope
	TenantID              string
	CellID                string
	Environment           string
	TrustProfile          string
	Epoch                 uint64
	ActivationEpoch       uint64
	IssuedAt              time.Time
	ExpiresAt             time.Time
	RuntimeVersion        string
	AntiRollbackFloor     string
	MinimumRuntimeVersion string
	ReceiptSigner         ReceiptSigner `json:"-"`
}

// ActivationReceipt is immutable evidence that one bundle was accepted for
// one tenant epoch. Its signature covers the receipt digest, which covers the
// scope, trust context, time window, epoch, and bundle digest.
type ActivationReceipt struct {
	BundleID     string          `json:"bundle_id"`
	BundleDigest string          `json:"bundle_digest"`
	Scope        Scope           `json:"scope"`
	Environment  string          `json:"environment"`
	TrustProfile string          `json:"trust_profile"`
	Epoch        uint64          `json:"epoch"`
	IssuedAt     time.Time       `json:"issued_at"`
	ExpiresAt    time.Time       `json:"expires_at"`
	ActivatedAt  time.Time       `json:"activated_at"`
	Signer       BundleSignature `json:"signer"`
	Digest       string          `json:"digest"`
	Signature    BundleSignature `json:"signature"`
}

// Receipt is an alias for ActivationReceipt.
type Receipt = ActivationReceipt

// ReceiptSigner is the provider-neutral signing port for activation receipts.
type ReceiptSigner interface {
	SignDigest(digest string) (BundleSignature, error)
}

// Ed25519ReceiptSigner is a fixture-friendly receipt signer. It holds private
// key material only in the caller's process and never places it in a receipt.
type Ed25519ReceiptSigner struct {
	KeyHandle  string
	KeyVersion string
	PrivateKey ed25519.PrivateKey
}

// NewEd25519ReceiptSigner validates and wraps a receipt signing key.
func NewEd25519ReceiptSigner(handle, version string, privateKey ed25519.PrivateKey) (*Ed25519ReceiptSigner, error) {
	if strings.TrimSpace(handle) == "" || strings.TrimSpace(version) == "" || len(privateKey) != ed25519.PrivateKeySize {
		return nil, &Error{Code: "INVALID_RECEIPT_SIGNER", Cause: ErrReceiptSignerRequired, Detail: "key handle, key version, and a standard Ed25519 private key are required"}
	}
	return &Ed25519ReceiptSigner{KeyHandle: strings.TrimSpace(handle), KeyVersion: strings.TrimSpace(version), PrivateKey: append(ed25519.PrivateKey(nil), privateKey...)}, nil
}

// SignDigest signs the raw SHA-256 digest bytes using the repository's
// detached Ed25519 convention.
func (s *Ed25519ReceiptSigner) SignDigest(digest string) (BundleSignature, error) {
	if s == nil || len(s.PrivateKey) != ed25519.PrivateKeySize {
		return BundleSignature{}, ErrReceiptSignerRequired
	}
	digestBytes, err := digestBytes(digest)
	if err != nil {
		return BundleSignature{}, err
	}
	return BundleSignature{Algorithm: "ed25519", KeyHandle: s.KeyHandle, KeyVersion: s.KeyVersion, KeyID: s.KeyHandle, Value: encodeSignature(ed25519.Sign(s.PrivateKey, digestBytes))}, nil
}

func encodeSignature(raw []byte) string {
	const hexAlphabet = "0123456789abcdef"
	out := make([]byte, len(raw)*2)
	for i, b := range raw {
		out[2*i] = hexAlphabet[b>>4]
		out[2*i+1] = hexAlphabet[b&0x0f]
	}
	return string(out)
}

// Activator is a concurrency-safe in-memory activation port. It does not
// persist anything and has no clock, database, or network dependency.
type Activator struct {
	keys          KeyResolver
	policy        ActivationPolicy
	receiptSigner ReceiptSigner
	now           func() time.Time

	mu       sync.Mutex
	epochs   map[string]uint64
	receipts map[string]map[uint64]ActivationReceipt
}

// ActivationRegistry and ActivationManager are descriptive aliases.
type ActivationRegistry = Activator
type ActivationManager = Activator

// NewActivator builds an activation port. Optional arguments may be an
// ActivationPolicy, *ActivationPolicy, ReceiptSigner, or func() time.Time.
// Keeping these dependencies optional makes the pure port easy to compose in
// tests while activation itself still refuses without a receipt signer.
func NewActivator(keys KeyResolver, options ...any) *Activator {
	a := &Activator{keys: keys, epochs: make(map[string]uint64), receipts: make(map[string]map[uint64]ActivationReceipt), now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		switch value := option.(type) {
		case ActivationPolicy:
			a.policy = value
		case *ActivationPolicy:
			if value != nil {
				a.policy = *value
			}
		case ReceiptSigner:
			a.receiptSigner = value
		case func() time.Time:
			a.now = value
		}
	}
	return a
}

// NewActivationRegistry is an explicit-name alias for NewActivator.
func NewActivationRegistry(keys KeyResolver, options ...any) *Activator {
	return NewActivator(keys, options...)
}

// NewActivationManager is an explicit-name alias for NewActivator.
func NewActivationManager(keys KeyResolver, options ...any) *Activator {
	return NewActivator(keys, options...)
}

// Activate verifies a request completely, then atomically records its epoch
// and receipt. A same-digest request at the current epoch returns the exact
// earlier receipt without a second activation. An older epoch, or a
// different digest at the current epoch, changes no activation state.
func (a *Activator) Activate(request ActivationRequest) (ActivationReceipt, error) {
	if a == nil {
		return ActivationReceipt{}, activationRefusal("NO_ACTIVATOR", "activator", "activation port is nil", ErrActivationRefused)
	}
	signed := request.Bundle
	if signed.Bundle.BundleID == "" {
		signed = request.SignedBundle
	}
	if signed.Bundle.BundleID == "" {
		return ActivationReceipt{}, activationRefusal("MISSING_BUNDLE", "bundle", "signed bundle is required", ErrActivationRefused)
	}
	scope, err := activationScope(request, signed.Bundle)
	if err != nil {
		return ActivationReceipt{}, err
	}
	if err := a.verifyRequest(request, signed, scope); err != nil {
		return ActivationReceipt{}, err
	}
	epoch, err := requestEpoch(request)
	if err != nil {
		return ActivationReceipt{}, err
	}
	activatedAt := a.now().UTC()
	receipt := ActivationReceipt{
		BundleID: signed.Bundle.BundleID, BundleDigest: signed.Bundle.Digest,
		Scope: scope, Environment: request.Environment, TrustProfile: request.TrustProfile,
		Epoch: epoch, IssuedAt: request.IssuedAt.UTC(), ExpiresAt: request.ExpiresAt.UTC(), ActivatedAt: activatedAt,
		Signer: signed.Signature,
	}
	receiptDigest, err := receipt.DigestValue()
	if err != nil {
		return ActivationReceipt{}, err
	}
	receipt.Digest = receiptDigest
	signer := request.ReceiptSigner
	if signer == nil {
		signer = a.receiptSigner
	}
	if signer == nil {
		return ActivationReceipt{}, activationRefusal("MISSING_RECEIPT_SIGNER", "receipt.signature", "a receipt signer is required", ErrReceiptSignerRequired)
	}
	receipt.Signature, err = signer.SignDigest(receipt.Digest)
	if err != nil {
		return ActivationReceipt{}, activationRefusal("INVALID_RECEIPT_SIGNATURE", "receipt.signature", "receipt signing failed", err)
	}
	if err := validateSignatureIdentity(receipt.Signature); err != nil {
		return ActivationReceipt{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.epochs[scope.TenantID]
	if epoch < current {
		return ActivationReceipt{}, activationRefusal("EPOCH_REPLAY", "epoch", "activation epoch is older than the current tenant epoch", ErrEpochReplay)
	}
	if epoch == current {
		prior, ok := a.receipts[scope.TenantID][epoch]
		if ok && prior.BundleDigest == signed.Bundle.Digest {
			return cloneReceipt(prior), nil
		}
		return ActivationReceipt{}, activationRefusal("EPOCH_CONFLICT", "epoch", "the current epoch already contains another bundle", ErrEpochConflict)
	}
	if a.receipts[scope.TenantID] == nil {
		a.receipts[scope.TenantID] = make(map[uint64]ActivationReceipt)
	}
	a.epochs[scope.TenantID] = epoch
	a.receipts[scope.TenantID][epoch] = cloneReceipt(receipt)
	return cloneReceipt(receipt), nil
}

// ActivateBundle is the package-level convenience form.
func ActivateBundle(a *Activator, request ActivationRequest) (ActivationReceipt, error) {
	if a == nil {
		return ActivationReceipt{}, ErrActivationRefused
	}
	return a.Activate(request)
}

// Activate is the package-level convenience form with the same semantics as
// Activator.Activate.
func Activate(a *Activator, request ActivationRequest) (ActivationReceipt, error) {
	return ActivateBundle(a, request)
}

// CurrentEpoch returns the last accepted epoch for tenant, or zero when no
// activation has been accepted.
func (a *Activator) CurrentEpoch(tenant string) uint64 {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.epochs[tenant]
}

// Receipt returns the immutable receipt for tenant/epoch, if any.
func (a *Activator) Receipt(tenant string, epoch uint64) (ActivationReceipt, bool) {
	if a == nil {
		return ActivationReceipt{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	receipt, ok := a.receipts[tenant][epoch]
	return cloneReceipt(receipt), ok
}

// ActivationCount reports the number of accepted epochs for tenant.
func (a *Activator) ActivationCount(tenant string) int {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.receipts[tenant])
}

func (a *Activator) verifyRequest(request ActivationRequest, signed SignedBundle, scope Scope) error {
	if a.keys == nil {
		return activationRefusal("UNKNOWN_SIGNING_KEY", "signature.key_handle", "no signing-key resolver is configured", ErrUnknownSigningKey)
	}
	handle := signed.Signature.KeyHandle
	version := signed.Signature.KeyVersion
	if handle == "" {
		handle = signed.Signature.KeyID
	}
	if handle == "" {
		handle = signed.SignerKeyHandle
	}
	if version == "" {
		version = signed.SignerKeyVersion
	}
	key, found, err := a.keys.ResolveBundleKey(handle, version)
	if err != nil {
		return activationRefusal("SIGNING_KEY_LOOKUP_FAILED", "signature.key_handle", "signing-key lookup failed", err)
	}
	if !found || key.Handle != handle || key.Version != version {
		return activationRefusal("UNKNOWN_SIGNING_KEY", "signature.key_handle", "signing key handle/version is not trusted", ErrUnknownSigningKey)
	}
	if key.Revoked {
		return activationRefusal("REVOKED_SIGNING_KEY", "signature.key_handle", "signing key is revoked", ErrRevokedSigningKey)
	}
	if !key.validAt(request.IssuedAt) {
		return activationRefusal("SIGNING_KEY_EXPIRED", "issued_at", "signing key is outside its validity window", ErrExpiredBundle)
	}
	if key.TrustProfile != "" && key.TrustProfile != request.TrustProfile {
		return activationRefusal("TRUST_PROFILE_MISMATCH", "trust_profile", "signing key is not bound to the requested trust profile", ErrTrustProfileMismatch)
	}
	if err := VerifyBundleSignature(signed, key.PublicKey); err != nil {
		return activationRefusal("INVALID_SIGNATURE", "signature.value", "bundle signature verification failed", err)
	}
	bundleScope := signed.Bundle.TargetScope
	if bundleScope.TenantID == "" {
		bundleScope = signed.Bundle.Scope
	}
	if bundleScope != scope {
		return activationRefusal("SCOPE_MISMATCH", "scope", "activation scope differs from bundle scope", ErrActivationScopeMismatch)
	}
	if a.policy.Scope.TenantID != "" && a.policy.Scope != scope {
		return activationRefusal("SCOPE_MISMATCH", "scope", "activation scope differs from verifier policy", ErrActivationScopeMismatch)
	}
	if strings.TrimSpace(request.Environment) == "" {
		return activationRefusal("MISSING_ENVIRONMENT", "environment", "environment is required", ErrActivationRefused)
	}
	if a.policy.Environment != "" && request.Environment != a.policy.Environment {
		return activationRefusal("ENVIRONMENT_MISMATCH", "environment", "environment differs from verifier policy", ErrEnvironmentMismatch)
	}
	if strings.TrimSpace(request.TrustProfile) == "" {
		return activationRefusal("MISSING_TRUST_PROFILE", "trust_profile", "trust profile is required", ErrActivationRefused)
	}
	if request.IssuedAt.IsZero() || request.ExpiresAt.IsZero() || !request.ExpiresAt.After(request.IssuedAt) {
		return activationRefusal("INVALID_VALIDITY_WINDOW", "issued_at", "issued and expiry times must form a non-empty interval", ErrInvalidActivationWindow)
	}
	now := a.now().UTC()
	if now.Before(request.IssuedAt) || !now.Before(request.ExpiresAt) {
		return activationRefusal("BUNDLE_EXPIRED", "expires_at", "activation request is outside its validity window", ErrExpiredBundle)
	}
	floor := a.policy.AntiRollbackFloor
	if floor == "" {
		floor = a.policy.MinimumRuntimeVersion
	}
	if request.AntiRollbackFloor != "" {
		floor = request.AntiRollbackFloor
	}
	if floor != "" && compareRuntimeVersion(signed.Bundle.MinimumRuntimeVersion, floor) < 0 {
		return activationRefusal("BELOW_ROLLBACK_FLOOR", "minimum_runtime_version", "bundle runtime floor is below the anti-rollback floor", ErrBelowRollbackFloor)
	}
	runtimeVersion := request.RuntimeVersion
	if runtimeVersion == "" {
		runtimeVersion = a.policy.RuntimeVersion
	}
	if runtimeVersion != "" && compareRuntimeVersion(runtimeVersion, signed.Bundle.MinimumRuntimeVersion) < 0 {
		return activationRefusal("RUNTIME_BELOW_BUNDLE_FLOOR", "runtime_version", "activation runtime is below the bundle minimum", ErrBelowRollbackFloor)
	}
	return nil
}

func activationScope(request ActivationRequest, bundle Bundle) (Scope, error) {
	scope := request.Scope
	if scope.TenantID == "" {
		scope = request.TargetScope
	}
	if scope.TenantID == "" && request.TenantID != "" {
		scope = Scope{TenantID: request.TenantID, CellID: request.CellID}
	}
	if scope.TenantID == "" {
		scope = bundle.TargetScope
		if scope.TenantID == "" {
			scope = bundle.Scope
		}
	}
	if scope.TenantID == "" {
		return Scope{}, activationRefusal("MISSING_SCOPE", "scope", "tenant scope is required", ErrActivationScopeMismatch)
	}
	if request.Scope.TenantID != "" && request.TargetScope.TenantID != "" && request.Scope != request.TargetScope {
		return Scope{}, activationRefusal("SCOPE_MISMATCH", "scope", "scope aliases disagree", ErrActivationScopeMismatch)
	}
	return scope, nil
}

func requestEpoch(request ActivationRequest) (uint64, error) {
	if request.Epoch != 0 && request.ActivationEpoch != 0 && request.Epoch != request.ActivationEpoch {
		return 0, activationRefusal("EPOCH_MISMATCH", "epoch", "epoch aliases disagree", ErrInvalidEpoch)
	}
	epoch := request.Epoch
	if epoch == 0 {
		epoch = request.ActivationEpoch
	}
	if epoch == 0 {
		return 0, activationRefusal("INVALID_EPOCH", "epoch", "activation epoch must be positive", ErrInvalidEpoch)
	}
	return epoch, nil
}

func (r ActivationReceipt) canonical() ([]byte, error) {
	return canonicalbytes.New("hcmnext.platform.configbundle.ActivationReceipt", 1).
		String("bundle_id", r.BundleID).
		String("bundle_digest", r.BundleDigest).
		String("tenant_id", r.Scope.TenantID).
		String("cell_id", r.Scope.CellID).
		String("environment", r.Environment).
		String("trust_profile", r.TrustProfile).
		Int("epoch", int64(r.Epoch)).
		String("issued_at", r.IssuedAt.UTC().Format(time.RFC3339Nano)).
		String("expires_at", r.ExpiresAt.UTC().Format(time.RFC3339Nano)).
		String("activated_at", r.ActivatedAt.UTC().Format(time.RFC3339Nano)).
		String("signer_key_handle", r.Signer.KeyHandle).
		String("signer_key_version", r.Signer.KeyVersion).Bytes()
}

// DigestValue computes the receipt digest without trusting Digest or
// Signature fields.
func (r ActivationReceipt) DigestValue() (string, error) {
	canonical, err := r.canonical()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(canonical), nil
}

// Verify checks the receipt digest and its detached signature.
func (r ActivationReceipt) Verify(publicKey ed25519.PublicKey) error {
	if err := validateSignatureIdentity(r.Signature); err != nil {
		return err
	}
	digest, err := r.DigestValue()
	if err != nil {
		return err
	}
	if digest != r.Digest {
		return activationRefusal("RECEIPT_MUTATED", "digest", "recorded receipt digest differs from canonical receipt", ErrReceiptInvalid)
	}
	digestBytes, err := digestBytes(r.Digest)
	if err != nil {
		return err
	}
	signature, err := decodeSignature(r.Signature.Value)
	if err != nil || !ed25519.Verify(publicKey, digestBytes, signature) {
		return activationRefusal("INVALID_RECEIPT_SIGNATURE", "signature.value", "receipt signature verification failed", ErrReceiptInvalid)
	}
	return nil
}

// Explain returns audit-safe activation facts and excludes signature bytes.
func (r ActivationReceipt) Explain() string {
	return fmt.Sprintf("activation receipt bundle=%s epoch=%d tenant=%s environment=%s trust_profile=%s digest=%s", r.BundleID, r.Epoch, r.Scope.TenantID, r.Environment, r.TrustProfile, r.BundleDigest)
}

func validateSignatureIdentity(signature BundleSignature) error {
	if signature.Algorithm != "ed25519" || strings.TrimSpace(signature.KeyHandle) == "" || strings.TrimSpace(signature.KeyVersion) == "" {
		return activationRefusal("INVALID_SIGNATURE", "signature", "algorithm, key handle, and key version are required", ErrInvalidSignature)
	}
	if _, err := decodeSignature(signature.Value); err != nil {
		return activationRefusal("INVALID_SIGNATURE", "signature.value", "signature is not a valid Ed25519 encoding", err)
	}
	return nil
}

func decodeSignature(value string) ([]byte, error) {
	if len(value) != ed25519.SignatureSize*2 {
		return nil, ErrInvalidSignature
	}
	out := make([]byte, ed25519.SignatureSize)
	for i := range out {
		hi, ok := hexNibble(value[2*i])
		if !ok {
			return nil, ErrInvalidSignature
		}
		lo, ok := hexNibble(value[2*i+1])
		if !ok {
			return nil, ErrInvalidSignature
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func cloneReceipt(receipt ActivationReceipt) ActivationReceipt { return receipt }

func activationRefusal(code, field, detail string, cause error) error {
	return &Error{Code: code, Ref: field, Detail: detail, Cause: cause}
}

var (
	ErrActivationRefused       = errors.New("configbundle: activation refused")
	ErrEpochReplay             = errors.New("configbundle: activation epoch is a replay")
	ErrEpochConflict           = errors.New("configbundle: activation epoch already contains another bundle")
	ErrInvalidEpoch            = errors.New("configbundle: activation epoch is invalid")
	ErrInvalidActivationWindow = errors.New("configbundle: activation validity window is invalid")
	ErrExpiredBundle           = errors.New("configbundle: bundle is expired")
	ErrBelowRollbackFloor      = errors.New("configbundle: bundle is below the anti-rollback floor")
	ErrActivationScopeMismatch = errors.New("configbundle: activation scope does not match bundle scope")
	ErrEnvironmentMismatch     = errors.New("configbundle: activation environment does not match policy")
	ErrTrustProfileMismatch    = errors.New("configbundle: activation trust profile does not match signer")
	ErrReceiptSignerRequired   = errors.New("configbundle: signed activation receipt requires a signer")
	ErrReceiptInvalid          = errors.New("configbundle: activation receipt is invalid")
)

// compareRuntimeVersion compares numeric components in runtime strings such
// as go1.26.3. Non-numeric prefixes are ignored, keeping comparison stable
// for Go and runtime labels without importing a version package.
func compareRuntimeVersion(left, right string) int {
	ln := numericVersionParts(left)
	rn := numericVersionParts(right)
	for len(ln) < len(rn) {
		ln = append(ln, 0)
	}
	for len(rn) < len(ln) {
		rn = append(rn, 0)
	}
	for i := range ln {
		if ln[i] < rn[i] {
			return -1
		}
		if ln[i] > rn[i] {
			return 1
		}
	}
	return 0
}

func numericVersionParts(value string) []int {
	var parts []int
	current := -1
	for _, r := range value {
		if r >= '0' && r <= '9' {
			if current < 0 {
				current = 0
			}
			current = current*10 + int(r-'0')
			continue
		}
		if current >= 0 {
			parts = append(parts, current)
			current = -1
		}
	}
	if current >= 0 {
		parts = append(parts, current)
	}
	return parts
}
