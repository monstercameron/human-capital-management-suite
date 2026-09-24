package subscription

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidCredential     = errors.New("subscription: invalid destination signing credential")
	ErrCredentialNotFound    = errors.New("subscription: signing credential not found")
	ErrCredentialExpired     = errors.New("subscription: signing credential expired")
	ErrCredentialNotYetValid = errors.New("subscription: signing credential is not yet valid")
	ErrCredentialRevoked     = errors.New("subscription: signing credential revoked")
	ErrCredentialDestination = errors.New("subscription: signing credential destination mismatch")
	ErrCredentialProfile     = errors.New("subscription: signing profile mismatch")
	ErrCredentialRotation    = errors.New("subscription: invalid signing credential rotation")
	ErrSignatureInvalid      = errors.New("subscription: signature verification failed")
)

// SignatureContext is the exact metadata bound into a signature. KeyRef is
// an opaque custody reference; raw key material never belongs in this model.
type SignatureContext struct {
	Destination string
	Profile     string
	Version     string
	KeyRef      string
}

// SignatureProvider is the provider-neutral custody boundary for signing and
// verification. Implementations may call KMS/HSM; this package only retains
// references and signatures.
type SignatureProvider interface {
	Sign(SignatureContext, string) (string, error)
	Verify(SignatureContext, string, string) error
}

// SigningCredential is one destination-scoped signing version. It is a
// reference to key custody, not a container for key material.
type SigningCredential struct {
	Destination string
	Profile     string
	Version     string
	KeyRef      string
	NotBefore   time.Time
	NotAfter    time.Time
	RevokedAt   time.Time
	Provider    SignatureProvider
}

func (c SigningCredential) context() SignatureContext {
	return SignatureContext{Destination: c.Destination, Profile: c.Profile, Version: c.Version, KeyRef: c.KeyRef}
}

func (c SigningCredential) validate() error {
	for name, value := range map[string]string{"destination": c.Destination, "profile": c.Profile, "version": c.Version, "key_ref": c.KeyRef} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidCredential, name)
		}
	}
	if c.NotBefore.IsZero() || c.NotAfter.IsZero() || !c.NotAfter.After(c.NotBefore) {
		return fmt.Errorf("%w: credential validity interval is invalid", ErrInvalidCredential)
	}
	if c.Provider == nil {
		return fmt.Errorf("%w: signature provider is required", ErrInvalidCredential)
	}
	return nil
}

func (c SigningCredential) usableAt(at time.Time) error {
	at = at.UTC()
	if !at.Before(c.NotBefore.UTC()) && at.Before(c.NotAfter.UTC()) {
		if !c.RevokedAt.IsZero() && !at.Before(c.RevokedAt.UTC()) {
			return ErrCredentialRevoked
		}
		return nil
	}
	if at.Before(c.NotBefore.UTC()) {
		return ErrCredentialNotYetValid
	}
	return ErrCredentialExpired
}

// SignedDelivery carries the exact destination, profile, version and message
// digest that a recipient must verify. It is safe to log as metadata except
// for the signature value itself, which remains provider output.
type SignedDelivery struct {
	Destination   string `json:"destination"`
	Profile       string `json:"profile"`
	Version       string `json:"version"`
	MessageDigest string `json:"message_digest"`
	Signature     string `json:"signature"`
}

func (s SignedDelivery) validate() error {
	for name, value := range map[string]string{"destination": s.Destination, "profile": s.Profile, "version": s.Version, "message_digest": s.MessageDigest, "signature": s.Signature} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidCredential, name)
		}
	}
	return nil
}

// RotationEvidence describes the overlap declared by Rotate. It does not
// contain secret or key material.
type RotationEvidence struct {
	Destination string
	Profile     string
	Previous    string
	Current     string
	ActivatedAt time.Time
	Overlap     time.Duration
}

// CredentialRing owns the active and overlapping signing versions for one
// destination. It is safe for concurrent signing, verification and rotation.
type CredentialRing struct {
	mu          sync.RWMutex
	destination string
	now         func() time.Time
	credentials map[string]SigningCredential
	active      string
}

// NewCredentialRing creates a ring bound to exactly one destination.
func NewCredentialRing(destination string, now ...func() time.Time) (*CredentialRing, error) {
	if strings.TrimSpace(destination) == "" || strings.TrimSpace(destination) != destination {
		return nil, fmt.Errorf("%w: destination is required", ErrInvalidCredential)
	}
	clock := func() time.Time { return time.Now().UTC() }
	if len(now) > 0 && now[0] != nil {
		clock = now[0]
	}
	return &CredentialRing{destination: destination, now: clock, credentials: make(map[string]SigningCredential)}, nil
}

// Add registers an inactive credential version. Activation is explicit so a
// newly published key cannot silently become the sender's current version.
func (r *CredentialRing) Add(c SigningCredential) error {
	if r == nil {
		return ErrInvalidCredential
	}
	if err := c.validate(); err != nil {
		return err
	}
	if c.Destination != r.destination {
		return ErrCredentialDestination
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.credentials[c.Version]; exists {
		return fmt.Errorf("%w: version %s already exists", ErrCredentialRotation, c.Version)
	}
	r.credentials[c.Version] = c
	return nil
}

// Activate promotes an already-added version without changing its declared
// validity interval.
func (r *CredentialRing) Activate(version string, at time.Time) error {
	if r == nil || strings.TrimSpace(version) == "" {
		return ErrInvalidCredential
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.credentials[version]
	if !ok {
		return ErrCredentialNotFound
	}
	if err := c.usableAt(at); err != nil {
		return err
	}
	r.active = version
	return nil
}

// Rotate installs and activates next at at. The previous version remains
// usable through the declared overlap, preserving delivery while consumers
// adopt the new exact profile/version.
func (r *CredentialRing) Rotate(next SigningCredential, at time.Time, overlap time.Duration) (RotationEvidence, error) {
	if r == nil {
		return RotationEvidence{}, ErrInvalidCredential
	}
	if overlap < 0 || at.IsZero() {
		return RotationEvidence{}, fmt.Errorf("%w: overlap and activation time are invalid", ErrCredentialRotation)
	}
	if err := next.validate(); err != nil {
		return RotationEvidence{}, err
	}
	if next.Destination != r.destination {
		return RotationEvidence{}, ErrCredentialDestination
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.credentials[next.Version]; exists {
		return RotationEvidence{}, fmt.Errorf("%w: version %s already exists", ErrCredentialRotation, next.Version)
	}
	if err := next.usableAt(at); err != nil && !errors.Is(err, ErrCredentialNotYetValid) {
		return RotationEvidence{}, err
	}
	previous := r.active
	if previous != "" {
		old := r.credentials[previous]
		cutoff := at.UTC().Add(overlap)
		if old.NotAfter.Before(cutoff) {
			cutoff = old.NotAfter
		}
		if !cutoff.After(old.NotBefore) {
			return RotationEvidence{}, fmt.Errorf("%w: overlap ends before the previous credential became usable", ErrCredentialRotation)
		}
		old.NotAfter = cutoff
		r.credentials[previous] = old
	}
	r.credentials[next.Version] = next
	r.active = next.Version
	return RotationEvidence{Destination: r.destination, Profile: next.Profile, Previous: previous, Current: next.Version, ActivatedAt: at.UTC(), Overlap: overlap}, nil
}

// Revoke prevents future signing and verification with version at or after
// the supplied instant. Existing signatures remain evidence but do not verify
// as current recipient-authorized signatures after revocation.
func (r *CredentialRing) Revoke(version string, at time.Time) error {
	if r == nil || strings.TrimSpace(version) == "" || at.IsZero() {
		return ErrInvalidCredential
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.credentials[version]
	if !ok {
		return ErrCredentialNotFound
	}
	if c.RevokedAt.IsZero() || at.Before(c.RevokedAt) {
		c.RevokedAt = at.UTC()
		r.credentials[version] = c
	}
	return nil
}

// CurrentVersion returns the exact version selected for new signatures.
func (r *CredentialRing) CurrentVersion() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

// Destination returns the endpoint this credential ring is bound to.
func (r *CredentialRing) Destination() string {
	if r == nil {
		return ""
	}
	return r.destination
}

// Sign signs a canonical message digest with the current credential.
func (r *CredentialRing) Sign(messageDigest string, at ...time.Time) (SignedDelivery, error) {
	if r == nil {
		return SignedDelivery{}, ErrInvalidCredential
	}
	when := r.now().UTC()
	if len(at) > 0 {
		when = at[0].UTC()
	}
	if strings.TrimSpace(messageDigest) == "" || strings.TrimSpace(messageDigest) != messageDigest {
		return SignedDelivery{}, fmt.Errorf("%w: message digest is required", ErrInvalidCredential)
	}
	r.mu.RLock()
	c, ok := r.credentials[r.active]
	r.mu.RUnlock()
	if !ok {
		return SignedDelivery{}, ErrCredentialNotFound
	}
	if err := c.usableAt(when); err != nil {
		return SignedDelivery{}, err
	}
	signature, err := c.Provider.Sign(c.context(), messageDigest)
	if err != nil {
		return SignedDelivery{}, fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	}
	signed := SignedDelivery{Destination: c.Destination, Profile: c.Profile, Version: c.Version, MessageDigest: messageDigest, Signature: signature}
	if err := signed.validate(); err != nil {
		return SignedDelivery{}, err
	}
	return signed, nil
}

// Verify checks the exact destination, profile and version carried by a
// signature against the credential active for that version at at.
func (r *CredentialRing) Verify(signed SignedDelivery, messageDigest string, at ...time.Time) error {
	if r == nil {
		return ErrInvalidCredential
	}
	when := r.now().UTC()
	if len(at) > 0 {
		when = at[0].UTC()
	}
	if err := signed.validate(); err != nil {
		return err
	}
	if signed.MessageDigest != messageDigest {
		return ErrSignatureInvalid
	}
	if signed.Destination != r.destination {
		return ErrCredentialDestination
	}
	r.mu.RLock()
	c, ok := r.credentials[signed.Version]
	r.mu.RUnlock()
	if !ok {
		return ErrCredentialNotFound
	}
	if c.Profile != signed.Profile {
		return ErrCredentialProfile
	}
	if err := c.usableAt(when); err != nil {
		return err
	}
	if err := c.Provider.Verify(c.context(), messageDigest, signed.Signature); err != nil {
		return fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	}
	return nil
}

// HMACProvider is a small standard-library provider useful for tests and
// local adapters. Production callers should use a custody-backed provider;
// this helper keeps its secret in memory only and never persists it.
type HMACProvider struct {
	keyRef string
	secret []byte
}

func NewHMACProvider(keyRef string, secret []byte) (*HMACProvider, error) {
	if strings.TrimSpace(keyRef) == "" || len(secret) == 0 {
		return nil, ErrInvalidCredential
	}
	return &HMACProvider{keyRef: keyRef, secret: append([]byte(nil), secret...)}, nil
}

func (p *HMACProvider) Sign(ctx SignatureContext, messageDigest string) (string, error) {
	if p == nil || ctx.KeyRef != p.keyRef || messageDigest == "" {
		return "", ErrSignatureInvalid
	}
	sum := hmacDigest(p.secret, ctx, messageDigest)
	return hex.EncodeToString(sum[:]), nil
}

func (p *HMACProvider) Verify(ctx SignatureContext, messageDigest, signature string) error {
	if p == nil || ctx.KeyRef != p.keyRef {
		return ErrSignatureInvalid
	}
	expected, err := p.Sign(ctx, messageDigest)
	if err != nil || !hmac.Equal([]byte(expected), []byte(signature)) {
		return ErrSignatureInvalid
	}
	return nil
}

func hmacDigest(secret []byte, ctx SignatureContext, messageDigest string) [32]byte {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte(strings.Join([]string{"hcmnext.subscription.signature/v1", ctx.Destination, ctx.Profile, ctx.Version, ctx.KeyRef, messageDigest}, "\x00")))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
