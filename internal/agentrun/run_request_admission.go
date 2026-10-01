package agentrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// SourceKind identifies the governed entry point that produced a request.
type SourceKind string

const (
	SourceChat           SourceKind = "CHAT"
	SourcePersonaMention SourceKind = "PERSONA_MENTION"
	SourceAPI            SourceKind = "API"
	SourceEvent          SourceKind = "EVENT"
	SourceSchedule       SourceKind = "SCHEDULE"
	SourceWorkflow       SourceKind = "WORKFLOW"
)

// RunMode separates human-delegated work from autonomous sponsored work.
type RunMode string

const (
	ModeOnBehalfOf RunMode = "ON_BEHALF_OF"
	ModeSponsored  RunMode = "SPONSORED"
)

// Decision is persisted before downstream inference can begin.
type Decision string

const (
	DecisionAccepted Decision = "ACCEPTED"
	DecisionRefused  Decision = "REFUSED"
)

var (
	ErrInvalidRequest   = errors.New("agentrun: invalid run request")
	ErrSourceConflict   = errors.New("agentrun: source key reused with different request")
	ErrAuthorityMissing = errors.New("agentrun: authority verifier is required")
	ErrAuthorityRefusal = errors.New("agentrun: admission authority refused request")
	ErrInvalidSnapshot  = errors.New("agentrun: invalid authority snapshot")
	ErrSourceConverter  = errors.New("agentrun: source key converter is required")
)

// SourceIdentity is the canonical dedupe namespace. Key is an opaque stable
// value supplied by the source adapter; it is not a display string.
type SourceIdentity struct {
	TenantID string     `json:"tenant_id"`
	Kind     SourceKind `json:"kind"`
	Key      string     `json:"key,omitempty"`
	Ref      string     `json:"ref,omitempty"`
}

// VersionRef pins one immutable agent version. A zero version is never a
// request to resolve "latest".
type VersionRef struct {
	AgentID string `json:"agent_id"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// PersonaRef pins the exact persona profile that produced a persona-mention
// request. It is source provenance, separate from the installed agent version.
type PersonaRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// PrincipalChain carries claimed identities into the authority check. The
// accepted record stores the independently resolved chain from the snapshot.
type PrincipalChain struct {
	Mode                   RunMode `json:"mode"`
	AgentPrincipalID       string  `json:"agent_principal_id"`
	SponsorID              string  `json:"sponsor_id,omitempty"`
	InvokerID              string  `json:"invoker_id,omitempty"`
	DelegatedCredentialRef string  `json:"delegated_credential_ref,omitempty"`
}

// ContextScope identifies the bounded source projection for this run.
type ContextScope struct {
	ID         string `json:"scope_id"`
	SnapshotID string `json:"snapshot_id"`
	Digest     string `json:"digest"`
}

// AudienceScope identifies the intended output audience and its pinned
// membership/policy snapshot.
type AudienceScope struct {
	ID         string `json:"audience_id"`
	SnapshotID string `json:"snapshot_id"`
	Digest     string `json:"digest"`
}

// Budget is an explicit upper bound, never an estimate.
type Budget struct {
	MaxCostMicros   uint64 `json:"max_cost_micros"`
	MaxInputTokens  uint64 `json:"max_input_tokens"`
	MaxOutputTokens uint64 `json:"max_output_tokens"`
}

// Request is the one admission payload shared by chat, UI/API, event,
// schedule, and workflow adapters. It contains references and digests, not
// prompt or source-record contents.
type Request struct {
	Source         SourceIdentity `json:"source"`
	Persona        *PersonaRef    `json:"persona,omitempty"`
	LegalEntity    string         `json:"legal_entity_id"`
	Agent          VersionRef     `json:"agent"`
	InstallationID string         `json:"installation_id"`
	Principal      PrincipalChain `json:"principal_chain"`
	Purpose        string         `json:"purpose"`
	Audience       AudienceScope  `json:"audience"`
	Context        ContextScope   `json:"context_scope"`
	Deadline       time.Time      `json:"deadline"`
	Budget         Budget         `json:"budget"`
	CauseID        string         `json:"cause_id"`
}

// AuthoritySnapshot is the server-resolved immutable authority image pinned
// on acceptance. Its values must be produced by Authority.VerifyAdmission.
type AuthoritySnapshot struct {
	Agent          VersionRef     `json:"agent"`
	InstallationID string         `json:"installation_id"`
	Principal      PrincipalChain `json:"principal_chain"`
	Audience       AudienceScope  `json:"audience"`
	Context        ContextScope   `json:"context_scope"`
	BudgetCeiling  Budget         `json:"budget_ceiling"`
	GrantRef       string         `json:"grant_ref"`
	PolicyDigest   string         `json:"policy_digest"`
}

// Authority verifies current grants, installation, purpose, source freshness,
// audience, provider eligibility, stop controls, and version selection.
// Implementations must not treat request fields as authority evidence.
type Authority interface {
	VerifyAdmission(context.Context, Request) (AuthoritySnapshot, error)
}

// Refusal is a stable, non-sensitive denial code suitable for durable storage.
type AdmissionRefusal struct{ Code string }

func (r *AdmissionRefusal) Error() string {
	if r == nil || strings.TrimSpace(r.Code) == "" {
		return ErrAuthorityRefusal.Error()
	}
	return ErrAuthorityRefusal.Error() + ": " + r.Code
}

func (r *AdmissionRefusal) Unwrap() error { return ErrAuthorityRefusal }

// Record contains a durable decision. Refused records have no authority
// snapshot; accepted records always carry a validated pinned snapshot.
type Record struct {
	ID            string
	Request       Request
	RequestDigest string
	Decision      Decision
	RefusalCode   string
	Authority     AuthoritySnapshot
	AdmittedAt    time.Time
}

// Store atomically inserts or returns the unique record for a source identity.
// Implementations must compare RequestDigest on conflict and return
// ErrSourceConflict for a changed payload. The production adapter must make
// the source uniqueness constraint durable and tenant scoped.
type Store interface {
	CreateOrGet(context.Context, Record) (Record, bool, error)
}

// SourceKeyConverter lets entry-point adapters translate their native
// occurrence identity into the shared source namespace without giving the
// admission package dependencies on chat, scheduling, or workflow packages.
type SourceKeyConverter interface {
	ConvertSource(context.Context, Request) (SourceIdentity, error)
}

// CanonicalSourceConverter derives a durable source key from the immutable
// occurrence reference and its requested agent installation. The caller's
// Source.Key is ignored; adapters must set Ref from the source-owned
// occurrence/event/workflow/idempotency identity.
type CanonicalSourceConverter struct{}

// ConvertSource creates a lowercase-hex key scoped to tenant, source kind,
// occurrence, agent and installation. It does not use mutable message text,
// budgets or policy decisions.
func (CanonicalSourceConverter) ConvertSource(_ context.Context, request Request) (SourceIdentity, error) {
	source := request.Source
	if !cleanRequired(source.TenantID, 256) || !validSourceKind(source.Kind) || !cleanRequired(source.Ref, 512) ||
		!cleanRequired(request.Agent.AgentID, 256) || !cleanRequired(request.InstallationID, 256) {
		return SourceIdentity{}, fmt.Errorf("%w: tenant, source occurrence, agent and installation are required", ErrInvalidRequest)
	}
	identity := struct {
		Tenant        string     `json:"tenant"`
		Kind          SourceKind `json:"kind"`
		OccurrenceRef string     `json:"occurrence_ref"`
		AgentID       string     `json:"agent_id"`
		Installation  string     `json:"installation_id"`
		PersonaID     string     `json:"persona_id,omitempty"`
	}{source.TenantID, source.Kind, source.Ref, request.Agent.AgentID, request.InstallationID, personaSourceID(request)}
	data, err := json.Marshal(identity)
	if err != nil {
		return SourceIdentity{}, fmt.Errorf("%w: encode source identity: %v", ErrInvalidRequest, err)
	}
	sum := sha256.Sum256(data)
	source.Key = hex.EncodeToString(sum[:])
	return source, nil
}

func personaSourceID(request Request) string {
	if request.Source.Kind != SourcePersonaMention || request.Persona == nil {
		return ""
	}
	return request.Persona.ID
}

// Config supplies mandatory current authority and durable idempotency ports.
type AdmissionConfig struct {
	Authority       Authority
	Store           Store
	SourceConverter SourceKeyConverter
	Now             func() time.Time
}

// Service runs one authority check and persists its accepted or refused
// result before returning a decision to any inference caller.
type AdmissionService struct {
	authority Authority
	store     Store
	converter SourceKeyConverter
	now       func() time.Time
}

// NewService fails closed unless both current authority and persistence are
// configured.
func NewAdmissionService(cfg AdmissionConfig) (*AdmissionService, error) {
	if cfg.Authority == nil {
		return nil, ErrAuthorityMissing
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: durable store is required", ErrInvalidRequest)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &AdmissionService{authority: cfg.Authority, store: cfg.Store, converter: cfg.SourceConverter, now: now}, nil
}

// AdmitFromSource converts a native trigger reference into the canonical
// source-kind/key namespace before applying the shared admission path.
func (s *AdmissionService) AdmitFromSource(ctx context.Context, request Request) (Record, bool, error) {
	if s == nil || s.converter == nil {
		return Record{}, false, ErrSourceConverter
	}
	source, err := s.converter.ConvertSource(ctx, request)
	if err != nil {
		return Record{}, false, err
	}
	request.Source = source
	return s.Admit(ctx, request)
}

// Admit validates and authorizes a candidate, then durably claims its source
// key. A duplicate with identical content returns the original decision; a
// replay that changes any request field conflicts.
func (s *AdmissionService) Admit(ctx context.Context, request Request) (Record, bool, error) {
	if s == nil || s.authority == nil || s.store == nil {
		return Record{}, false, ErrAuthorityMissing
	}
	request = cloneRequest(request)
	if err := validateSource(request.Source); err != nil {
		return Record{}, false, err
	}
	digest, err := digestRequest(request)
	if err != nil {
		return Record{}, false, err
	}
	record := Record{ID: requestID(request.Source), Request: cloneRequest(request), RequestDigest: digest, AdmittedAt: s.now().UTC()}
	if err := validateRequest(request, record.AdmittedAt); err != nil {
		record.Decision, record.RefusalCode = DecisionRefused, "INVALID_REQUEST"
		return s.persist(ctx, record)
	}
	snapshot, err := s.authority.VerifyAdmission(ctx, cloneRequest(request))
	if err != nil {
		var refusal *AdmissionRefusal
		if !errors.As(err, &refusal) {
			return Record{}, false, err
		}
		record.Decision, record.RefusalCode = DecisionRefused, refusal.Code
		if strings.TrimSpace(record.RefusalCode) == "" {
			record.RefusalCode = "AUTHORITY_REFUSED"
		}
		return s.persist(ctx, record)
	}
	if err := validateSnapshot(request, snapshot); err != nil {
		record.Decision, record.RefusalCode = DecisionRefused, "AUTHORITY_SNAPSHOT_INVALID"
		return s.persist(ctx, record)
	}
	record.Decision, record.Authority = DecisionAccepted, cloneSnapshot(snapshot)
	return s.persist(ctx, record)
}

func (s *AdmissionService) persist(ctx context.Context, candidate Record) (Record, bool, error) {
	stored, created, err := s.store.CreateOrGet(ctx, candidate)
	if err != nil {
		return Record{}, false, err
	}
	// Durable adapters store only a digest of the raw source key. The caller
	// supplied it, and the matching fingerprint binds it to this outcome.
	stored.Request = cloneRequest(candidate.Request)
	return stored, created, nil
}

func validateSource(source SourceIdentity) error {
	if !cleanRequired(source.TenantID, 256) || !cleanRequired(source.Key, 512) || !validSourceKind(source.Kind) || !cleanOptional(source.Ref, 512) {
		return fmt.Errorf("%w: tenant, supported source kind and stable source key are required", ErrInvalidRequest)
	}
	return nil
}

func validateRequest(r Request, now time.Time) error {
	if r.Source.Kind == SourcePersonaMention {
		if r.Persona == nil || !cleanRequired(r.Persona.ID, 256) || !cleanRequired(r.Persona.Version, 256) || !validDigest(r.Persona.Digest) {
			return fmt.Errorf("%w: persona mentions require an exact persona version pin", ErrInvalidRequest)
		}
	} else if r.Persona != nil && (!cleanRequired(r.Persona.ID, 256) || !cleanRequired(r.Persona.Version, 256) || !validDigest(r.Persona.Digest)) {
		return fmt.Errorf("%w: invalid persona version pin", ErrInvalidRequest)
	}
	if !cleanRequired(r.LegalEntity, 256) || !cleanRequired(r.Agent.AgentID, 256) || !cleanRequired(r.Agent.Version, 256) || !validDigest(r.Agent.Digest) || !cleanRequired(r.InstallationID, 256) {
		return fmt.Errorf("%w: legal entity and exact agent version are required", ErrInvalidRequest)
	}
	if !cleanRequired(r.Purpose, 4096) || !cleanRequired(r.Audience.ID, 256) || !cleanRequired(r.Audience.SnapshotID, 256) || !validDigest(r.Audience.Digest) {
		return fmt.Errorf("%w: purpose and pinned audience are required", ErrInvalidRequest)
	}
	if !cleanRequired(r.Context.ID, 256) || !cleanRequired(r.Context.SnapshotID, 256) || !validDigest(r.Context.Digest) || !cleanRequired(r.CauseID, 512) {
		return fmt.Errorf("%w: pinned context and cause are required", ErrInvalidRequest)
	}
	if r.Deadline.IsZero() || !r.Deadline.After(now) {
		return fmt.Errorf("%w: deadline must be in the future", ErrInvalidRequest)
	}
	if r.Budget.MaxCostMicros == 0 || r.Budget.MaxInputTokens == 0 || r.Budget.MaxOutputTokens == 0 {
		return fmt.Errorf("%w: all budget ceilings must be positive", ErrInvalidRequest)
	}
	if r.Principal.Mode != ModeOnBehalfOf && r.Principal.Mode != ModeSponsored {
		return fmt.Errorf("%w: unsupported run mode", ErrInvalidRequest)
	}
	if !cleanRequired(r.Principal.AgentPrincipalID, 256) {
		return fmt.Errorf("%w: agent principal is required", ErrInvalidRequest)
	}
	if r.Principal.Mode == ModeOnBehalfOf {
		if !cleanRequired(r.Principal.InvokerID, 256) || !cleanRequired(r.Principal.DelegatedCredentialRef, 512) || r.Principal.SponsorID != "" {
			return fmt.Errorf("%w: on-behalf-of requires an invoker and delegated credential only", ErrInvalidRequest)
		}
	} else if !cleanRequired(r.Principal.SponsorID, 256) || r.Principal.InvokerID != "" || r.Principal.DelegatedCredentialRef != "" {
		return fmt.Errorf("%w: sponsored mode requires sponsor and forbids borrowed human authority", ErrInvalidRequest)
	}
	return nil
}

func validateSnapshot(r Request, a AuthoritySnapshot) error {
	if a.Agent != r.Agent || a.InstallationID != r.InstallationID || a.Principal != r.Principal || a.Audience != r.Audience || a.Context != r.Context ||
		!cleanRequired(a.GrantRef, 512) || !validDigest(a.PolicyDigest) {
		return ErrInvalidSnapshot
	}
	if !withinBudget(r.Budget, a.BudgetCeiling) {
		return fmt.Errorf("%w: request exceeds current authority budget", ErrInvalidSnapshot)
	}
	return nil
}

func withinBudget(requested, ceiling Budget) bool {
	return ceiling.MaxCostMicros > 0 && ceiling.MaxInputTokens > 0 && ceiling.MaxOutputTokens > 0 &&
		requested.MaxCostMicros <= ceiling.MaxCostMicros && requested.MaxInputTokens <= ceiling.MaxInputTokens && requested.MaxOutputTokens <= ceiling.MaxOutputTokens
}

func validSourceKind(k SourceKind) bool {
	switch k {
	case SourceChat, SourcePersonaMention, SourceAPI, SourceEvent, SourceSchedule, SourceWorkflow:
		return true
	default:
		return false
	}
}

func cleanRequired(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value
}

func cleanOptional(value string, max int) bool {
	return len(value) <= max && strings.TrimSpace(value) == value
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validRefusalCode(value string) bool {
	if value == "" || len(value) > 96 {
		return false
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func digestRequest(request Request) (string, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("%w: encode request: %v", ErrInvalidRequest, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func requestID(source SourceIdentity) string {
	key := struct {
		Tenant string     `json:"tenant"`
		Kind   SourceKind `json:"kind"`
		Key    string     `json:"key"`
	}{source.TenantID, source.Kind, source.Key}
	data, _ := json.Marshal(key)
	sum := sha256.Sum256(data)
	return "arreq_" + hex.EncodeToString(sum[:])
}

// SourceKeyDigest returns the lowercase SHA-256 hex digest used by durable
// stores to enforce the source uniqueness constraint without storing the raw
// source key.
func SourceKeyDigest(source SourceIdentity) (string, error) {
	if err := validateSource(source); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(source.Key))
	return hex.EncodeToString(sum[:]), nil
}

// AdmissionRequestDigest computes the stable lowercase-hex fingerprint saved
// beside a durable decision.
func AdmissionRequestDigest(request Request) (string, error) {
	return digestRequest(cloneRequest(request))
}

// AdmissionRequestID derives the deterministic record ID from its source key.
func AdmissionRequestID(source SourceIdentity) (string, error) {
	if err := validateSource(source); err != nil {
		return "", err
	}
	return requestID(source), nil
}

// ValidateAdmissionRecord checks the invariants an append-only store must
// enforce even when called without AdmissionService.
func ValidateAdmissionRecord(record Record) error {
	if record.AdmittedAt.IsZero() || record.RequestDigest == "" {
		return fmt.Errorf("%w: record identity and admitted time are required", ErrInvalidRequest)
	}
	if err := validateSource(record.Request.Source); err != nil {
		return err
	}
	wantID, _ := AdmissionRequestID(record.Request.Source)
	wantDigest, err := AdmissionRequestDigest(record.Request)
	if err != nil || record.ID != wantID || record.RequestDigest != wantDigest || !validHexDigest(record.RequestDigest) {
		return fmt.Errorf("%w: request identity or digest mismatch", ErrInvalidRequest)
	}
	switch record.Decision {
	case DecisionAccepted:
		if record.RefusalCode != "" || validateRequest(record.Request, record.AdmittedAt) != nil || validateSnapshot(record.Request, record.Authority) != nil {
			return fmt.Errorf("%w: accepted record is not fully pinned", ErrInvalidRequest)
		}
	case DecisionRefused:
		if !validRefusalCode(record.RefusalCode) || record.Authority != (AuthoritySnapshot{}) {
			return fmt.Errorf("%w: refused record has invalid outcome fields", ErrInvalidRequest)
		}
	default:
		return fmt.Errorf("%w: unknown decision", ErrInvalidRequest)
	}
	return nil
}

func cloneRequest(r Request) Request {
	r.Deadline = r.Deadline.UTC()
	if r.Persona != nil {
		persona := *r.Persona
		r.Persona = &persona
	}
	return r
}

func cloneSnapshot(s AuthoritySnapshot) AuthoritySnapshot { return s }

// MemoryAdmissionStore is a concurrent reference implementation for unit tests and
// deterministic local composition. It is not a production durable adapter.
type MemoryAdmissionStore struct {
	mu      sync.Mutex
	records map[string]Record
}

// NewMemoryAdmissionStore creates an empty reference store.
func NewMemoryAdmissionStore() *MemoryAdmissionStore {
	return &MemoryAdmissionStore{records: make(map[string]Record)}
}

// CreateOrGet enforces source-key idempotency under a single mutex.
func (s *MemoryAdmissionStore) CreateOrGet(_ context.Context, candidate Record) (Record, bool, error) {
	if s == nil {
		return Record{}, false, fmt.Errorf("%w: nil durable store", ErrInvalidRequest)
	}
	if err := ValidateAdmissionRecord(candidate); err != nil {
		return Record{}, false, err
	}
	key := sourceIndex(candidate.Request.Source)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = make(map[string]Record)
	}
	if prior, ok := s.records[key]; ok {
		if prior.ID != candidate.ID || prior.RequestDigest != candidate.RequestDigest {
			return Record{}, false, ErrSourceConflict
		}
		return cloneRecord(prior), false, nil
	}
	s.records[key] = cloneRecord(candidate)
	return cloneRecord(candidate), true, nil
}

// Get returns a detached record for one source identity.
func (s *MemoryAdmissionStore) Get(source SourceIdentity) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("%w: nil store", ErrInvalidRequest)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[sourceIndex(source)]
	if !ok {
		return Record{}, fmt.Errorf("%w: record not found", ErrInvalidRequest)
	}
	return cloneRecord(record), nil
}

func sourceIndex(source SourceIdentity) string {
	return source.TenantID + "\x00" + string(source.Kind) + "\x00" + source.Key
}

func cloneRecord(r Record) Record {
	r.Request = cloneRequest(r.Request)
	r.Authority = cloneSnapshot(r.Authority)
	r.AdmittedAt = r.AdmittedAt.UTC()
	return r
}
