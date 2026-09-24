package pseudonym

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

const (
	escrowSchemaVersion = 1
	// MaxEscrowReleaseTTL prevents a release request from becoming standing
	// authority. The returned subject is never retained after Release returns.
	MaxEscrowReleaseTTL = 24 * time.Hour
)

var (
	ErrInvalidEscrowConfig = errors.New("pseudonym: invalid escrow configuration")
	ErrEscrowKeySeparation = errors.New("pseudonym: escrow key must be distinct from derivation and tenant keys")
	ErrEscrowMappingExists = errors.New("pseudonym: escrow mapping already exists")
	ErrEscrowNotFound      = errors.New("pseudonym: escrow mapping not found")
	ErrEscrowReleaseDenied = errors.New("pseudonym: escrow release denied")
)

// EscrowConfig contains only provider ports and opaque key handles. Neither
// a derivation key nor an escrow key is ever represented by key material.
type EscrowConfig struct {
	Deriver           custody.KeyDeriver
	Provider          custody.Provider
	DerivationKey     custody.Handle
	EscrowKey         custody.Handle
	TenantKEKs        []custody.Handle
	Clock             func() time.Time
	ApprovalAuthority RevelationApprovalAuthority
}

// EscrowRecord is a ciphertext-only identity mapping. Subject is deliberately
// absent; the provider-owned escrow key is required to open Ciphertext.
type EscrowRecord struct {
	PseudonymID string             `json:"pseudonym_id"`
	Tenant      string             `json:"tenant"`
	Scope       string             `json:"scope"`
	Purpose     string             `json:"purpose"`
	Generation  int                `json:"generation"`
	Ciphertext  custody.Ciphertext `json:"ciphertext"`
	StoredAt    time.Time          `json:"stored_at"`
}

type escrowMapping struct {
	Pseudonym Pseudonym `json:"pseudonym"`
	Subject   string    `json:"subject"`
}

// EscrowReleaseRequest is the complete dual-control release authority. The
// requester and custodian must be different principals.
type EscrowReleaseRequest struct {
	Pseudonym         Pseudonym                 `json:"pseudonym"`
	RequestedBy       string                    `json:"requested_by"`
	EscrowCustodian   string                    `json:"escrow_custodian"`
	Purpose           string                    `json:"purpose"`
	EvidenceRef       string                    `json:"evidence_ref"`
	TTL               time.Duration             `json:"ttl"`
	RequesterDecision approval.ApprovalDecision `json:"requester_decision"`
	CustodianDecision approval.ApprovalDecision `json:"custodian_decision"`
}

// EscrowEvent is digest-only evidence for a release attempt. It contains no
// subject, plaintext mapping, or key material.
type EscrowEvent struct {
	Operation       string        `json:"operation"`
	PseudonymID     string        `json:"pseudonym_id,omitempty"`
	Tenant          string        `json:"tenant,omitempty"`
	RequestedBy     string        `json:"requested_by,omitempty"`
	EscrowCustodian string        `json:"escrow_custodian,omitempty"`
	Purpose         string        `json:"purpose,omitempty"`
	EvidenceRef     string        `json:"evidence_ref,omitempty"`
	TTL             time.Duration `json:"ttl,omitempty"`
	At              time.Time     `json:"at"`
	ExpiresAt       time.Time     `json:"expires_at,omitempty"`
	Outcome         string        `json:"outcome"`
	Digest          string        `json:"digest"`
}

// IdentityEscrow owns encrypted mappings and the release evidence stream.
// It never derives pseudonyms and never uses the derivation key to encrypt or
// decrypt a mapping.
type IdentityEscrow struct {
	provider  custody.Provider
	escrowKey custody.Handle
	clock     func() time.Time
	authority RevelationApprovalAuthority

	mu             sync.RWMutex
	records        map[string]EscrowRecord
	events         []EscrowEvent
	revelationUses revelationUseState
}

// NewIdentityEscrow creates a ciphertext-only escrow with an independently
// scoped custody key. Tenant KEKs are accepted so separation can be checked at
// the boundary rather than inferred by callers.
func NewIdentityEscrow(config EscrowConfig) (*IdentityEscrow, error) {
	if config.Provider == nil || config.Deriver == nil {
		return nil, fmt.Errorf("%w: provider and deriver are required", ErrInvalidEscrowConfig)
	}
	if err := validateKeyHandle(config.DerivationKey); err != nil {
		return nil, fmt.Errorf("%w: derivation key: %v", ErrInvalidEscrowConfig, err)
	}
	if err := validateKeyHandle(config.EscrowKey); err != nil {
		return nil, fmt.Errorf("%w: escrow key: %v", ErrInvalidEscrowConfig, err)
	}
	if sameKeyIdentity(config.DerivationKey, config.EscrowKey) {
		return nil, ErrEscrowKeySeparation
	}
	for _, tenantKEK := range config.TenantKEKs {
		if err := validateKeyHandle(tenantKEK); err != nil {
			return nil, fmt.Errorf("%w: tenant KEK: %v", ErrInvalidEscrowConfig, err)
		}
		if sameKeyIdentity(config.EscrowKey, tenantKEK) {
			return nil, ErrEscrowKeySeparation
		}
	}
	clock := config.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &IdentityEscrow{provider: config.Provider, escrowKey: config.EscrowKey, clock: clock, authority: config.ApprovalAuthority, records: make(map[string]EscrowRecord)}, nil
}

// NewEscrow is a concise constructor alias.
func NewEscrow(config EscrowConfig) (*IdentityEscrow, error) { return NewIdentityEscrow(config) }

// Store encrypts the minimal mapping through the escrow key. The returned
// record contains ciphertext and safe coordinates only.
func (e *IdentityEscrow) Store(ctx custody.Context, p Pseudonym, subject string) (EscrowRecord, error) {
	if e == nil {
		return EscrowRecord{}, ErrInvalidEscrowConfig
	}
	if err := validateEscrowContext(ctx, p); err != nil {
		return EscrowRecord{}, err
	}
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(subject) != subject {
		return EscrowRecord{}, fmt.Errorf("%w: subject is required and may not be padded", ErrInvalidRequest)
	}
	id := pseudonymID(p)
	e.mu.RLock()
	existing, exists := e.records[id]
	e.mu.RUnlock()
	if exists {
		return existing, nil
	}
	payload, err := json.Marshal(escrowMapping{Pseudonym: p, Subject: subject})
	if err != nil {
		return EscrowRecord{}, fmt.Errorf("%w: encode mapping", ErrEscrowReleaseDenied)
	}
	sealed, _, err := e.provider.Encrypt(ctx, e.escrowKey, payload)
	if err != nil || sealed.Handle != e.escrowKey || len(sealed.Data) == 0 {
		return EscrowRecord{}, fmt.Errorf("%w: escrow encryption failed", ErrEscrowReleaseDenied)
	}
	now := e.clock().UTC()
	record := EscrowRecord{PseudonymID: id, Tenant: p.Tenant, Scope: p.Scope, Purpose: p.Purpose, Generation: p.Generation, Ciphertext: sealed, StoredAt: now}
	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, exists := e.records[id]; exists {
		return existing, nil
	}
	e.records[id] = record
	return record, nil
}

// StoreMapping is a descriptive alias for Store.
func (e *IdentityEscrow) StoreMapping(ctx custody.Context, p Pseudonym, subject string) (EscrowRecord, error) {
	return e.Store(ctx, p, subject)
}

// Record returns one ciphertext-only record.
func (e *IdentityEscrow) Record(id string) (EscrowRecord, bool) {
	if e == nil {
		return EscrowRecord{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	record, ok := e.records[id]
	return record, ok
}

// ErrRevelationEvidenceRequired is returned by every direct Release call:
// since ANON-004 the only path that opens the escrow is ReleaseWithEvidence,
// which validates a RevelationEvidence receipt for the exact pseudonym
// generation and consumes it once. It wraps ErrEscrowReleaseDenied so
// callers that only check for a denial keep working.
var ErrRevelationEvidenceRequired = fmt.Errorf("%w: revelation evidence required (use ReleaseWithEvidence)", ErrEscrowReleaseDenied)

// Release refuses every direct call and records the refusal as a denied
// escrow event. It exists so an unevidenced release is an auditable
// refusal rather than a silent success; see ReleaseWithEvidence.
func (e *IdentityEscrow) Release(ctx custody.Context, request EscrowReleaseRequest) (string, EscrowEvent, error) {
	if e == nil {
		return "", EscrowEvent{}, ErrEscrowReleaseDenied
	}
	base := EscrowEvent{Operation: "identity_escrow.release", PseudonymID: pseudonymID(request.Pseudonym), Tenant: request.Pseudonym.Tenant, RequestedBy: request.RequestedBy, EscrowCustodian: request.EscrowCustodian, Purpose: request.Purpose, EvidenceRef: request.EvidenceRef, TTL: request.TTL, At: e.clock().UTC()}
	return "", e.recordDenied(base, ErrRevelationEvidenceRequired), ErrRevelationEvidenceRequired
}

// release opens exactly one mapping after validating dual control, purpose,
// evidence, and a bounded TTL. A digested event is appended for every release
// that reaches the escrow decision point. Only ReleaseWithEvidence may call
// it, after the revelation receipt has been validated and consumed.
func (e *IdentityEscrow) release(ctx custody.Context, request EscrowReleaseRequest) (string, EscrowEvent, error) {
	if e == nil {
		return "", EscrowEvent{}, ErrEscrowReleaseDenied
	}
	id := pseudonymID(request.Pseudonym)
	now := e.clock().UTC()
	base := EscrowEvent{Operation: "identity_escrow.release", PseudonymID: id, Tenant: request.Pseudonym.Tenant, RequestedBy: request.RequestedBy, EscrowCustodian: request.EscrowCustodian, Purpose: request.Purpose, EvidenceRef: request.EvidenceRef, TTL: request.TTL, At: now}
	if err := validateEscrowContext(ctx, request.Pseudonym); err != nil {
		return "", e.recordDenied(base, err), err
	}
	if err := validateReleaseRequest(ctx, request); err != nil {
		return "", e.recordDenied(base, err), err
	}
	e.mu.RLock()
	record, ok := e.records[id]
	e.mu.RUnlock()
	if !ok {
		err := ErrEscrowNotFound
		return "", e.recordDenied(base, err), err
	}
	if record.Tenant != request.Pseudonym.Tenant || record.Scope != request.Pseudonym.Scope || record.Purpose != request.Pseudonym.Purpose || record.Generation != request.Pseudonym.Generation {
		err := ErrEscrowReleaseDenied
		return "", e.recordDenied(base, err), err
	}
	plaintext, _, err := e.provider.Decrypt(ctx, e.escrowKey, record.Ciphertext)
	if err != nil {
		return "", e.recordDenied(base, ErrEscrowReleaseDenied), ErrEscrowReleaseDenied
	}
	var mapping escrowMapping
	if err := json.Unmarshal(plaintext, &mapping); err != nil || pseudonymID(mapping.Pseudonym) != id || mapping.Pseudonym.Tenant != request.Pseudonym.Tenant || mapping.Subject == "" {
		return "", e.recordDenied(base, ErrEscrowReleaseDenied), ErrEscrowReleaseDenied
	}
	base.Outcome = "released"
	base.ExpiresAt = now.Add(request.TTL)
	base.Digest = digestEscrowEvent(base)
	e.mu.Lock()
	e.events = append(e.events, base)
	e.mu.Unlock()
	return mapping.Subject, base, nil
}

// Events returns digest-only release evidence in append order.
func (e *IdentityEscrow) Events() []EscrowEvent {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]EscrowEvent(nil), e.events...)
}

func (e *IdentityEscrow) recordDenied(event EscrowEvent, reason error) EscrowEvent {
	event.Outcome = "refused"
	// The error class is evidence-safe; its detailed text is intentionally not
	// persisted so an event cannot become a plaintext mapping oracle.
	event.Digest = digestEscrowEvent(event)
	e.mu.Lock()
	e.events = append(e.events, event)
	e.mu.Unlock()
	return event
}

func validateReleaseRequest(ctx custody.Context, request EscrowReleaseRequest) error {
	if strings.TrimSpace(request.RequestedBy) == "" || strings.TrimSpace(request.EscrowCustodian) == "" || request.RequestedBy != strings.TrimSpace(request.RequestedBy) || request.EscrowCustodian != strings.TrimSpace(request.EscrowCustodian) {
		return fmt.Errorf("%w: requester and escrow custodian are required", ErrEscrowReleaseDenied)
	}
	if strings.EqualFold(request.RequestedBy, request.EscrowCustodian) {
		return fmt.Errorf("%w: requester and escrow custodian must be distinct", ErrEscrowReleaseDenied)
	}
	if strings.TrimSpace(request.Purpose) == "" || request.Purpose != strings.TrimSpace(request.Purpose) || request.Purpose != ctx.Purpose || request.Purpose != request.Pseudonym.Purpose {
		return fmt.Errorf("%w: release purpose is not bound", ErrEscrowReleaseDenied)
	}
	if strings.TrimSpace(request.EvidenceRef) == "" || request.EvidenceRef != strings.TrimSpace(request.EvidenceRef) {
		return fmt.Errorf("%w: evidence reference is required", ErrEscrowReleaseDenied)
	}
	if request.TTL <= 0 || request.TTL > MaxEscrowReleaseTTL {
		return fmt.Errorf("%w: release ttl must be positive and bounded", ErrEscrowReleaseDenied)
	}
	return nil
}

func validateEscrowContext(ctx custody.Context, p Pseudonym) error {
	if err := ctx.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrEscrowReleaseDenied, err)
	}
	if id := pseudonymID(p); id == "" || strings.TrimSpace(p.Tenant) == "" || strings.TrimSpace(p.Scope) == "" || strings.TrimSpace(p.Purpose) == "" || p.Generation <= 0 || p.Tenant != ctx.Tenant || p.Purpose != ctx.Purpose {
		return fmt.Errorf("%w: pseudonym coordinates do not match context", ErrEscrowReleaseDenied)
	}
	return nil
}

func validateKeyHandle(handle custody.Handle) error {
	if err := handle.Validate(); err != nil {
		return err
	}
	if handle.Kind != custody.Key {
		return custody.ErrInvalidHandle
	}
	return nil
}

func sameKeyIdentity(left, right custody.Handle) bool {
	return left.ID == right.ID && left.Tenant == right.Tenant && left.Region == right.Region
}

func pseudonymID(p Pseudonym) string {
	if strings.TrimSpace(p.ID) != "" {
		return p.ID
	}
	return p.Value
}

func digestEscrowEvent(event EscrowEvent) string {
	canonical := strings.Join([]string{event.Operation, event.PseudonymID, event.Tenant, event.RequestedBy, event.EscrowCustodian, event.Purpose, event.EvidenceRef, fmt.Sprint(event.TTL), event.At.UTC().Format(time.RFC3339Nano), event.ExpiresAt.UTC().Format(time.RFC3339Nano), event.Outcome}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// EscrowedService derives pseudonyms with one custody handle and writes their
// identity mappings only through an independently keyed IdentityEscrow.
type EscrowedService struct {
	deriver       custody.KeyDeriver
	derivationKey custody.Handle
	escrow        *IdentityEscrow

	mu         sync.RWMutex
	generation int
}

// NewEscrowedService constructs a complete derivation-plus-escrow service.
func NewEscrowedService(config EscrowConfig) (*EscrowedService, error) {
	escrow, err := NewIdentityEscrow(config)
	if err != nil {
		return nil, err
	}
	return &EscrowedService{deriver: config.Deriver, derivationKey: config.DerivationKey, escrow: escrow, generation: 1}, nil
}

// NewEscrowService is the positional constructor for callers that already
// have the two distinct handles. Optional trailing handles are tenant KEKs.
func NewEscrowService(deriver custody.KeyDeriver, provider custody.Provider, derivationKey, escrowKey custody.Handle, tenantKEKs ...custody.Handle) (*EscrowedService, error) {
	return NewEscrowedService(EscrowConfig{Deriver: deriver, Provider: provider, DerivationKey: derivationKey, EscrowKey: escrowKey, TenantKEKs: tenantKEKs})
}

// Generate derives a scoped identifier and stores its mapping only in the
// independently keyed escrow.
func (s *EscrowedService) Generate(ctx custody.Context, request GenerateRequest) (Pseudonym, error) {
	if s == nil || s.deriver == nil || s.escrow == nil {
		return Pseudonym{}, ErrDerivationUnavailable
	}
	if err := ctx.Validate(); err != nil {
		return Pseudonym{}, err
	}
	if strings.TrimSpace(request.Subject) == "" || strings.TrimSpace(request.Scope) == "" || strings.TrimSpace(request.Purpose) == "" || request.Purpose != ctx.Purpose {
		return Pseudonym{}, ErrInvalidRequest
	}
	s.mu.RLock()
	generation := s.generation
	s.mu.RUnlock()
	derived, _, err := s.deriver.Derive(ctx, s.derivationKey, []byte(strings.Join([]string{"hcm-next/pseudonym", ctx.Tenant, request.Scope, request.Purpose, fmt.Sprint(generation)}, "\x00")))
	if err != nil || len(derived.Output) < sha256.Size {
		return Pseudonym{}, ErrDerivationUnavailable
	}
	mac := hmac.New(sha256.New, derived.Output)
	_, _ = mac.Write([]byte(request.Subject))
	identifier := fmt.Sprintf("psn:v%d:g%d:%s", escrowSchemaVersion, generation, hex.EncodeToString(mac.Sum(nil)))
	p := Pseudonym{ID: identifier, Value: identifier, Generation: generation, Tenant: ctx.Tenant, Scope: request.Scope, Purpose: request.Purpose}
	if _, err := s.escrow.Store(ctx, p, request.Subject); err != nil {
		return Pseudonym{}, err
	}
	return p, nil
}

// Pseudonymize is an alias for Generate.
func (s *EscrowedService) Pseudonymize(ctx custody.Context, request GenerateRequest) (Pseudonym, error) {
	return s.Generate(ctx, request)
}

// Rotate starts a new derivation generation while preserving older ciphertext
// records for separately authorized escrow release.
func (s *EscrowedService) Rotate(ctx custody.Context) (int, error) {
	if s == nil || s.escrow == nil {
		return 0, ErrDerivationUnavailable
	}
	if err := ctx.Validate(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	return s.generation, nil
}

// Release delegates the dual-control operation to the ciphertext-only escrow.
func (s *EscrowedService) Release(ctx custody.Context, request EscrowReleaseRequest) (string, EscrowEvent, error) {
	if s == nil || s.escrow == nil {
		return "", EscrowEvent{}, ErrEscrowReleaseDenied
	}
	return s.escrow.Release(ctx, request)
}

// Escrow exposes only the escrow metadata port; its records remain
// ciphertext-only and its key handle is not returned.
func (s *EscrowedService) Escrow() *IdentityEscrow {
	if s == nil {
		return nil
	}
	return s.escrow
}

// ExplainEscrow describes the separate custody boundary without including a
// subject or a mapping.
func ExplainEscrow() string {
	return "Identity escrow stores only ciphertext under a custody handle distinct from derivation and tenant KEK handles; release requires distinct requester and custodian, purpose, evidence, and bounded TTL, and emits a digest-only event."
}

// Explain describes this service's separate-custody contract.
func (s *EscrowedService) Explain() string { return ExplainEscrow() }
