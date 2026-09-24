package workload

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ProcessRole is a process's declared role from the closed vocabulary in
// definitions/architecture/process-roles.yaml. It is a policy token, never
// a hostname, pod name or other node/network identity: a workload
// identity's authority comes from this field having been attested by
// [Issuer], not from where a call originated on the network.
type ProcessRole string

// The process-roles.yaml vocabulary. hcmnext, worker, projector and migrate
// ship in P1A; scheduler and admin are reserved for P1B and are declared
// here (so the vocabulary is complete) but carry no [AllowMatrix] entries
// yet — issuing either today is legal, using it against the authorizer is
// not.
const (
	RoleHCMNext   ProcessRole = "hcmnext"
	RoleWorker    ProcessRole = "worker"
	RoleProjector ProcessRole = "projector"
	RoleMigrate   ProcessRole = "migrate"
	RoleScheduler ProcessRole = "scheduler"
	RoleAdmin     ProcessRole = "hcmctl"
)

var processRoleVocabulary = map[ProcessRole]bool{
	RoleHCMNext:   true,
	RoleWorker:    true,
	RoleProjector: true,
	RoleMigrate:   true,
	RoleScheduler: true,
	RoleAdmin:     true,
}

// Valid reports whether r is a declared process role.
func (r ProcessRole) Valid() bool { return processRoleVocabulary[r] }

// MaxIdentityLifetime is the longest lifetime a workload identity may be
// issued for. It is enforced both at issuance ([Issuer.Issue] refuses a
// longer request) and at verification ([Verifier.Verify] refuses a
// credential whose own iat/exp span exceeds it, in case a compromised or
// misconfigured issuer ever minted one): short-lived is a property of every
// credential this package will accept, not only the ones it mints itself.
const MaxIdentityLifetime = 15 * time.Minute

const tokenPrefix = "wlid1"

// maxTokenBytesDefault bounds credential parsing before any allocation that
// depends on the credential's own length.
const maxTokenBytesDefault = 4 << 10

var tokenEncoding = base64.RawURLEncoding.Strict()

// claims is the signed payload of a workload identity credential.
type claims struct {
	Issuer        string `json:"iss"`
	Subject       string `json:"sub"`
	Role          string `json:"role"`
	Cell          string `json:"cell"`
	KeyID         string `json:"kid"`
	IssuedAtUnix  int64  `json:"iat"`
	ExpiresAtUnix int64  `json:"exp"`
}

// Identity is one immutable workload identity. A signed identity is created
// only by [Verifier.Verify]; a verified mTLS identity can also be adapted
// for service authorization, and [BindMTLSIdentity] combines both proofs.
type Identity struct {
	issuer             string
	subject            string
	role               ProcessRole
	cell               string
	keyID              string
	issuedAt           time.Time
	expiresAt          time.Time
	fingerprint        string
	credentialVerified bool
}

// Issuer returns the workload identity authority that signed this identity.
func (id Identity) Issuer() string { return id.issuer }

// Subject returns the unique workload instance identifier.
func (id Identity) Subject() string { return id.subject }

// Role returns the attested process role.
func (id Identity) Role() ProcessRole { return id.role }

// Cell returns the deployment cell the identity was issued for.
func (id Identity) Cell() string { return id.cell }

// KeyID returns the issuer signing key id that produced this identity,
// which is the rotation evidence: two identities signed under different key
// ids prove the issuer actually rotated its key between them.
func (id Identity) KeyID() string { return id.keyID }

// IssuedAt returns the issuance instant in UTC.
func (id Identity) IssuedAt() time.Time { return id.issuedAt }

// ExpiresAt returns the expiry instant in UTC.
func (id Identity) ExpiresAt() time.Time { return id.expiresAt }

// ValidAt reports whether the identity's validity window covers at.
func (id Identity) ValidAt(at time.Time) bool {
	return !at.Before(id.issuedAt) && at.Before(id.expiresAt)
}

// Fingerprint returns a canonical digest over every trusted field.
func (id Identity) Fingerprint() string { return id.fingerprint }

// String returns a redacted, log-safe description. It never contains the
// signature or any credential bytes.
func (id Identity) String() string {
	return fmt.Sprintf("workload(issuer=%s subject=%s role=%s cell=%s kid=%s)", id.issuer, id.subject, id.role, id.cell, id.keyID)
}

func computeFingerprint(c claims) string {
	h := sha256.New()
	write := func(label, v string) { fmt.Fprintf(h, "%s=%d:%s;", label, len(v), v) }
	write("iss", c.Issuer)
	write("sub", c.Subject)
	write("role", c.Role)
	write("cell", c.Cell)
	write("kid", c.KeyID)
	write("iat", strconv.FormatInt(c.IssuedAtUnix, 10))
	write("exp", strconv.FormatInt(c.ExpiresAtUnix, 10))
	return hex.EncodeToString(h.Sum(nil))
}

// Identity/credential errors. All are matchable with errors.Is.
var (
	ErrMalformedCredential = errors.New("workload: credential is malformed")
	ErrInvalidSignature    = errors.New("workload: credential signature does not verify")
	ErrCredentialExpired   = errors.New("workload: credential is expired or not yet valid")
	ErrLifetimeTooLong     = errors.New("workload: credential lifetime exceeds the 15 minute maximum")
	ErrUnknownRole         = errors.New("workload: credential role is not in the process-roles.yaml vocabulary")
	ErrWrongCell           = errors.New("workload: credential was issued for a different cell")
	ErrKeyNotFound         = errors.New("workload: no signing key resolves for this issuer and key id")
)

// KeySource resolves the Ed25519 public key an issuer signed a credential
// with. Production deployment resolves this from the workload identity
// authority's published, rotated key set; [StaticKeySource] is the
// deterministic test double.
type KeySource interface {
	ResolveKey(ctx context.Context, issuer, keyID string) (ed25519.PublicKey, error)
}

// StaticKeySource is a fixed, compiled-in [KeySource].
type StaticKeySource struct {
	keys map[string]map[string]ed25519.PublicKey
}

// NewStaticKeySource returns an empty static key source.
func NewStaticKeySource() *StaticKeySource {
	return &StaticKeySource{keys: make(map[string]map[string]ed25519.PublicKey)}
}

// WithKey registers a public key for issuer/keyID and returns the receiver.
func (s *StaticKeySource) WithKey(issuer, keyID string, pub ed25519.PublicKey) *StaticKeySource {
	if s.keys[issuer] == nil {
		s.keys[issuer] = make(map[string]ed25519.PublicKey)
	}
	s.keys[issuer][keyID] = pub
	return s
}

// ResolveKey implements [KeySource].
func (s *StaticKeySource) ResolveKey(_ context.Context, issuer, keyID string) (ed25519.PublicKey, error) {
	set, ok := s.keys[issuer]
	if !ok {
		return nil, fmt.Errorf("%w: issuer %q", ErrKeyNotFound, issuer)
	}
	pub, ok := set[keyID]
	if !ok {
		return nil, fmt.Errorf("%w: issuer %q key %q", ErrKeyNotFound, issuer, keyID)
	}
	return pub, nil
}

// Issuer configuration errors.
var (
	ErrIssuerName = errors.New("workload: issuer needs a name")
	ErrIssuerKey  = errors.New("workload: issuer needs a key id and an ed25519 private key")
)

// IssuerConfig configures an [Issuer].
type IssuerConfig struct {
	// Name is this issuer's identity, recorded as the credential's issuer
	// field and used by a [Verifier]'s [KeySource] to scope key lookup.
	Name string
	// KeyID identifies the specific signing key below, so that a [Verifier]
	// and its [KeySource] can tell which of the issuer's (possibly several,
	// during a rotation window) keys produced a given credential.
	KeyID string
	// Private is the Ed25519 private key this issuer signs with.
	Private ed25519.PrivateKey
	// Now supplies the current time. Nil means time.Now.
	Now func() time.Time
}

// Issuer mints short-lived, Ed25519-signed workload identity credentials.
// One Issuer signs with exactly one key; rotating the signing key means
// constructing a new Issuer with a new key id, while a [Verifier]'s
// [KeySource] can still resolve the outgoing key until every credential it
// signed has naturally expired (at most [MaxIdentityLifetime] later).
type Issuer struct {
	name    string
	keyID   string
	private ed25519.PrivateKey
	now     func() time.Time
}

// NewIssuer validates cfg and returns the issuer.
func NewIssuer(cfg IssuerConfig) (*Issuer, error) {
	if cfg.Name == "" {
		return nil, ErrIssuerName
	}
	if cfg.KeyID == "" || len(cfg.Private) != ed25519.PrivateKeySize {
		return nil, ErrIssuerKey
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	private := make(ed25519.PrivateKey, len(cfg.Private))
	copy(private, cfg.Private)
	return &Issuer{name: cfg.Name, keyID: cfg.KeyID, private: private, now: now}, nil
}

// IssueSpec is what [Issuer.Issue] needs to mint one workload identity.
type IssueSpec struct {
	// Subject is the unique workload instance identifier (a replica or pod
	// identity), not a process role: several instances of the same role
	// each get their own Subject.
	Subject string
	Role    ProcessRole
	Cell    string
	// Lifetime must be positive and at most [MaxIdentityLifetime].
	Lifetime time.Duration
}

// IssueSpec errors.
var (
	ErrInvalidIssueSpec = errors.New("workload: issue spec is incomplete or invalid")
)

// Issue mints a signed credential text for spec.
func (i *Issuer) Issue(spec IssueSpec) (string, error) {
	if spec.Subject == "" {
		return "", fmt.Errorf("%w: subject is empty", ErrInvalidIssueSpec)
	}
	if !spec.Role.Valid() {
		return "", fmt.Errorf("%w: role %q is not in the process-roles.yaml vocabulary", ErrInvalidIssueSpec, spec.Role)
	}
	if spec.Cell == "" {
		return "", fmt.Errorf("%w: cell is empty", ErrInvalidIssueSpec)
	}
	if spec.Lifetime <= 0 {
		return "", fmt.Errorf("%w: lifetime must be positive", ErrInvalidIssueSpec)
	}
	if spec.Lifetime > MaxIdentityLifetime {
		return "", fmt.Errorf("%w: requested %s", ErrLifetimeTooLong, spec.Lifetime)
	}

	now := i.now().UTC()
	c := claims{
		Issuer:        i.name,
		Subject:       spec.Subject,
		Role:          string(spec.Role),
		Cell:          spec.Cell,
		KeyID:         i.keyID,
		IssuedAtUnix:  now.Unix(),
		ExpiresAtUnix: now.Add(spec.Lifetime).Unix(),
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("workload: encoding claims: %w", err)
	}
	body := tokenEncoding.EncodeToString(payload)
	signingInput := tokenPrefix + "." + body
	sig := ed25519.Sign(i.private, []byte(signingInput))
	return signingInput + "." + tokenEncoding.EncodeToString(sig), nil
}

// Verifier configuration errors.
var (
	ErrVerifierKeys = errors.New("workload: verifier needs a key source")
	ErrVerifierCell = errors.New("workload: verifier needs the cell it expects callers to be issued for")
)

// VerifierConfig configures a [Verifier].
type VerifierConfig struct {
	Keys KeySource
	// Cell is the deployment cell this listener runs in. A credential
	// issued for any other cell is refused.
	Cell string
	// Now supplies the current time. Nil means time.Now.
	Now func() time.Time
	// Leeway tolerates clock skew on the validity window. Zero means none.
	Leeway time.Duration
	// MaxTokenBytes bounds credential length. Zero means 4 KiB.
	MaxTokenBytes int
}

// Verifier turns a presented credential into a verified [Identity]. It is
// the only way an Identity comes into existence in a running process.
type Verifier struct {
	keys     KeySource
	cell     string
	now      func() time.Time
	leeway   time.Duration
	maxBytes int
}

// NewVerifier validates cfg and returns the verifier.
func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	if cfg.Keys == nil {
		return nil, ErrVerifierKeys
	}
	if cfg.Cell == "" {
		return nil, ErrVerifierCell
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	maxBytes := cfg.MaxTokenBytes
	if maxBytes <= 0 {
		maxBytes = maxTokenBytesDefault
	}
	return &Verifier{keys: cfg.Keys, cell: cfg.Cell, now: now, leeway: cfg.Leeway, maxBytes: maxBytes}, nil
}

// Verify checks raw as a workload identity credential and returns the
// [Identity] it attests. A credential with node/network identity as its
// only proof never reaches this far: there is no argument to Verify that
// accepts one, so the only way to obtain an [Identity] at all is a
// credential this function actually verifies.
func (v *Verifier) Verify(ctx context.Context, raw string) (Identity, error) {
	if raw == "" {
		return Identity{}, fmt.Errorf("%w: empty", ErrMalformedCredential)
	}
	if len(raw) > v.maxBytes {
		return Identity{}, fmt.Errorf("%w: exceeds %d bytes", ErrMalformedCredential, v.maxBytes)
	}
	prefix, rest, ok := strings.Cut(raw, ".")
	if !ok || prefix != tokenPrefix {
		return Identity{}, ErrMalformedCredential
	}
	body, sigText, ok := strings.Cut(rest, ".")
	if !ok || body == "" || sigText == "" || strings.Contains(sigText, ".") {
		return Identity{}, ErrMalformedCredential
	}
	sig, err := tokenEncoding.DecodeString(sigText)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: signature encoding: %v", ErrMalformedCredential, err)
	}
	payload, err := tokenEncoding.DecodeString(body)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: payload encoding: %v", ErrMalformedCredential, err)
	}

	var c claims
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Identity{}, fmt.Errorf("%w: claims: %v", ErrMalformedCredential, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Identity{}, fmt.Errorf("%w: trailing content after claims", ErrMalformedCredential)
	}
	if c.Issuer == "" || c.Subject == "" || c.KeyID == "" {
		return Identity{}, fmt.Errorf("%w: missing issuer, subject or key id", ErrMalformedCredential)
	}

	pub, err := v.keys.ResolveKey(ctx, c.Issuer, c.KeyID)
	if err != nil {
		return Identity{}, err
	}
	if len(sig) != ed25519.SignatureSize {
		return Identity{}, fmt.Errorf("%w: signature must be %d bytes, got %d", ErrInvalidSignature, ed25519.SignatureSize, len(sig))
	}
	if !ed25519.Verify(pub, []byte(tokenPrefix+"."+body), sig) {
		return Identity{}, ErrInvalidSignature
	}

	if !ProcessRole(c.Role).Valid() {
		return Identity{}, fmt.Errorf("%w: %q", ErrUnknownRole, c.Role)
	}
	if c.Cell != v.cell {
		return Identity{}, fmt.Errorf("%w: credential cell %q, this listener is %q", ErrWrongCell, c.Cell, v.cell)
	}
	if c.IssuedAtUnix == 0 || c.ExpiresAtUnix == 0 {
		return Identity{}, fmt.Errorf("%w: validity window is not stated", ErrCredentialExpired)
	}
	issuedAt := time.Unix(c.IssuedAtUnix, 0).UTC()
	expiresAt := time.Unix(c.ExpiresAtUnix, 0).UTC()
	if !expiresAt.After(issuedAt) {
		return Identity{}, fmt.Errorf("%w: exp does not follow iat", ErrCredentialExpired)
	}
	if expiresAt.Sub(issuedAt) > MaxIdentityLifetime {
		return Identity{}, fmt.Errorf("%w: %s", ErrLifetimeTooLong, expiresAt.Sub(issuedAt))
	}
	now := v.now().UTC()
	if !now.Before(expiresAt.Add(v.leeway)) {
		return Identity{}, fmt.Errorf("%w: expired at %s", ErrCredentialExpired, expiresAt.Format(time.RFC3339))
	}
	if now.Before(issuedAt.Add(-v.leeway)) {
		return Identity{}, fmt.Errorf("%w: not valid until %s", ErrCredentialExpired, issuedAt.Format(time.RFC3339))
	}

	return Identity{
		issuer:             c.Issuer,
		subject:            c.Subject,
		role:               ProcessRole(c.Role),
		cell:               c.Cell,
		keyID:              c.KeyID,
		issuedAt:           issuedAt,
		expiresAt:          expiresAt,
		fingerprint:        computeFingerprint(c),
		credentialVerified: true,
	}, nil
}
