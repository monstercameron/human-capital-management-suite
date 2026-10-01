// Package agentdelegation implements the on-behalf-of credential boundary for
// agent runs. Grants are durable, narrow ceilings; credentials are short-lived
// signed identity envelopes. Authority is resolved again by the caller at
// every verification, so a token never becomes a cached user permission.
package agentdelegation

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	// MaxGrantLifetime is the maximum lifetime of a task's durable grant.
	MaxGrantLifetime = 7 * 24 * time.Hour
	// MaxTokenLifetime is the maximum lifetime of a per-step credential.
	MaxTokenLifetime = 5 * time.Minute
	// DelegationGrantTokenType identifies the only subject-token kind accepted
	// by this exchange service. A browser session or machine token is not one.
	DelegationGrantTokenType = "urn:ietf:params:oauth:token-type:delegation-grant"
	// DelegatedAccessTokenType identifies the short-lived token minted for one
	// capability-gateway or connection step.
	DelegatedAccessTokenType = "urn:ietf:params:oauth:token-type:access_token"
	delegationIssuer         = "hcmnext.agentdelegation"
	maxActorDepth            = 16
)

var (
	ErrInvalidRequest        = errors.New("agentdelegation: invalid request")
	ErrInvalidGrant          = errors.New("agentdelegation: invalid delegation grant")
	ErrGrantNotFound         = errors.New("agentdelegation: delegation grant not found")
	ErrGrantRevoked          = errors.New("agentdelegation: delegation grant revoked")
	ErrGrantExpired          = errors.New("agentdelegation: delegation grant expired")
	ErrUserInactive          = errors.New("agentdelegation: signed-in user is inactive")
	ErrSkillNotGranted       = errors.New("agentdelegation: skill is outside the grant")
	ErrScopeExpanded         = errors.New("agentdelegation: requested scope expands authority")
	ErrAudienceRequired      = errors.New("agentdelegation: one exact audience is required")
	ErrSenderRequired        = errors.New("agentdelegation: sender-constrained workload identity is required")
	ErrTokenInvalid          = errors.New("agentdelegation: delegated token is invalid")
	ErrTokenExpired          = errors.New("agentdelegation: delegated token expired")
	ErrTokenRevoked          = errors.New("agentdelegation: delegated token revoked")
	ErrTokenSenderMismatch   = errors.New("agentdelegation: sender constraint mismatch")
	ErrTokenAudienceMismatch = errors.New("agentdelegation: audience mismatch")
)

// Grant is the durable, run-bound ceiling from which step credentials are
// exchanged. Authority carries only server-resolved bounds; it is never a
// claim supplied by an agent worker.
type Grant struct {
	GrantID           string
	ParentGrantID     string
	ParentActor       *ActorClaim
	CommonAdmissionID string
	UserID            string
	Tenant            values.TenantId
	AgentVersion      string
	// TargetAgentID is the exact agent identity bound by Authority.Delegate.
	// Empty preserves grants created before exact target identity was recorded.
	TargetAgentID       string
	InstallationID      string
	TaskID              string
	PlanSkillSetDigest  string
	Purpose             string
	OrganizationScopeID string
	Skills              []string
	SkillScopes         map[string][]string
	SkillAuthorities    SkillAuthorities
	NotBefore           time.Time
	ExpiresAt           time.Time
	RevocationEpoch     uint64
	Revoked             bool
	Authority           trust.DelegationGrant
}

// GrantRequest is the server-resolved input used when a user starts a task.
// UserAuthority must come from the current policy decision point.
type GrantRequest struct {
	parentGrantID     string
	parentActor       *ActorClaim
	CommonAdmissionID string
	GrantID           string
	UserID            string
	Tenant            values.TenantId
	AgentVersion      string
	// TargetAgentID is resolved from trusted server-owned run facts. When set,
	// it becomes the delegation target while AgentVersion remains attribution.
	TargetAgentID       string
	InstallationID      string
	TaskID              string
	PlanSkillSetDigest  string
	Purpose             string
	OrganizationScopeID string
	Skills              []string
	SkillScopes         map[string][]string
	NotBefore           time.Time
	ExpiresAt           time.Time
	UserAuthority       trust.AuthorityScope
	SkillAuthorities    SkillAuthorities
}

// SkillAuthority aliases the trust-layer per-skill authority contract.
type SkillAuthority = trust.SkillAuthority

// SkillAuthorities aliases the trust-layer per-skill authority contract.
type SkillAuthorities = trust.SkillAuthorities

// UserAuthority is returned by the current user authority resolver. Active
// is intentionally separate from the authority snapshot: deactivation must
// refuse exchange even if a stale role snapshot still exists.
type UserAuthority struct {
	UserID           string
	Active           bool
	Authority        trust.AuthorityScope
	SkillAuthorities SkillAuthorities
}

// AuthorityResolver is the policy seam. Implementations resolve the user's
// current authority at the instant of the call; they do not read authority
// from a token or from agent input.
type AuthorityResolver interface {
	Resolve(userID string, tenant values.TenantId, purpose string, at time.Time) (UserAuthority, error)
}

// ResolverFunc adapts a function to AuthorityResolver.
type ResolverFunc func(userID string, tenant values.TenantId, purpose string, at time.Time) (UserAuthority, error)

func (f ResolverFunc) Resolve(userID string, tenant values.TenantId, purpose string, at time.Time) (UserAuthority, error) {
	return f(userID, tenant, purpose, at)
}

// GrantStore is the durable state port. CurrentRevocationEpoch must return a
// monotonically increasing value whose initial value is one. Repositories
// should implement BumpRevocationEpoch transactionally with their durable
// identity/session revoke event.
type GrantStore interface {
	Save(Grant) error
	Get(grantID string) (Grant, error)
	Revoke(grantID, reason string) error
	CurrentRevocationEpoch(tenant values.TenantId, userID string) uint64
	BumpRevocationEpoch(tenant values.TenantId, userID, reason string) (uint64, error)
}

// MemoryGrantStore is a concurrency-safe store for tests and single-process
// development. Production adapters persist the same values and epoch rules.
type MemoryGrantStore struct {
	mu     sync.RWMutex
	grants map[string]Grant
	epochs map[string]uint64
}

func NewMemoryGrantStore() *MemoryGrantStore {
	return &MemoryGrantStore{grants: make(map[string]Grant), epochs: make(map[string]uint64)}
}

func epochKey(tenant values.TenantId, userID string) string { return tenant.String() + "\x00" + userID }

func (s *MemoryGrantStore) CurrentRevocationEpoch(tenant values.TenantId, userID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := epochKey(tenant, userID)
	if s.epochs[key] == 0 {
		s.epochs[key] = 1
	}
	return s.epochs[key]
}

func (s *MemoryGrantStore) BumpRevocationEpoch(tenant values.TenantId, userID, reason string) (uint64, error) {
	if s == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(reason) == "" {
		return 0, fmt.Errorf("%w: epoch subject and reason are required", ErrInvalidRequest)
	}
	if err := tenant.Validate(); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := epochKey(tenant, userID)
	if s.epochs[key] == 0 {
		s.epochs[key] = 1
	}
	s.epochs[key]++
	return s.epochs[key], nil
}

func (s *MemoryGrantStore) Save(g Grant) error {
	if s == nil {
		return fmt.Errorf("%w: nil grant store", ErrInvalidGrant)
	}
	if err := validateGrant(g); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.grants[g.GrantID]; exists {
		return fmt.Errorf("%w: grant %q already exists", ErrInvalidGrant, g.GrantID)
	}
	key := epochKey(g.Tenant, g.UserID)
	if s.epochs[key] == 0 {
		s.epochs[key] = 1
	}
	if g.RevocationEpoch != s.epochs[key] {
		return fmt.Errorf("%w: stale revocation epoch", ErrGrantRevoked)
	}
	s.grants[g.GrantID] = cloneGrant(g)
	return nil
}

func (s *MemoryGrantStore) Get(grantID string) (Grant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.grants[grantID]
	if !ok {
		return Grant{}, fmt.Errorf("%w: %s", ErrGrantNotFound, grantID)
	}
	return cloneGrant(g), nil
}

func (s *MemoryGrantStore) Revoke(grantID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: revocation reason is required", ErrInvalidRequest)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[grantID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrGrantNotFound, grantID)
	}
	g.Revoked = true
	g.Authority.Revoked = true
	s.grants[grantID] = cloneGrant(g)
	return nil
}

// ActorClaim is the signed actor chain. A child actor is nested in Act rather
// than replacing the parent, preserving attribution to the user at every
// delegation depth.
type ActorClaim struct {
	AgentVersion   string      `json:"agent_version"`
	InstallationID string      `json:"installation_id"`
	RunID          string      `json:"run_id"`
	StepID         string      `json:"step_id"`
	Act            *ActorClaim `json:"act,omitempty"`
}

// Claims is the identity-only payload of one step credential. Scope is the
// exact skill scope for this step, not a reusable agent permission document.
type Claims struct {
	TokenType        string     `json:"token_type"`
	Issuer           string     `json:"iss"`
	JTI              string     `json:"jti"`
	Subject          string     `json:"sub"`
	Tenant           string     `json:"tenant"`
	GrantID          string     `json:"grant_id"`
	Purpose          string     `json:"purpose"`
	Skill            string     `json:"skill"`
	Scope            []string   `json:"scope"`
	Audience         string     `json:"aud"`
	SenderConstraint string     `json:"cnf_workload"`
	RevocationEpoch  uint64     `json:"revocation_epoch"`
	IssuedAtUnix     int64      `json:"iat"`
	ExpiresAtUnix    int64      `json:"exp"`
	Actor            ActorClaim `json:"act"`
}

// DelegatedCredential is returned by Exchange. Raw is the transport form;
// Claims is a decoded convenience view and is never trusted without Verify.
type DelegatedCredential struct {
	Raw    string
	Claims Claims
}

// ExchangeRequest follows RFC 8693's subject-token exchange shape while
// accepting only an HCM delegation-grant subject token.
type ExchangeRequest struct {
	SubjectToken     string
	SubjectTokenType string
	RunID            string
	StepID           string
	AgentVersion     string
	InstallationID   string
	Skill            string
	Scope            []string
	Audience         string
	Lifetime         time.Duration
	Sender           string
	Parent           *DelegatedCredential
}

// VerifyRequest binds a credential to the exact gateway/connection and the
// authenticated workload presenting it. Scope is optional; when supplied it
// must be a subset of the step scope.
type VerifyRequest struct {
	Audience string
	Sender   string
	Skill    string
	Scope    []string
	At       time.Time
}

type Config struct {
	Store     GrantStore
	Authority AuthorityResolver
	Secret    []byte
	Now       func() time.Time
}

// Service is the internal token service for agent on-behalf-of exchange.
type Service struct {
	store     GrantStore
	authority AuthorityResolver
	secret    []byte
	now       func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	if cfg.Store == nil || cfg.Authority == nil {
		return nil, fmt.Errorf("%w: store and authority resolver are required", ErrInvalidRequest)
	}
	secret := slices.Clone(cfg.Secret)
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("%w: generating token signing key: %v", ErrInvalidRequest, err)
		}
	}
	if len(secret) < 16 {
		return nil, fmt.Errorf("%w: token signing key is too short", ErrInvalidRequest)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{store: cfg.Store, authority: cfg.Authority, secret: secret, now: now}, nil
}

// CreateGrant durably records one task grant after intersecting its requested
// skill scopes with the authority snapshot supplied by the policy service.
func (s *Service) CreateGrant(req GrantRequest) (Grant, error) {
	if s == nil || s.store == nil || s.authority == nil {
		return Grant{}, fmt.Errorf("%w: nil service", ErrInvalidRequest)
	}
	now := s.now().UTC()
	if req.NotBefore.IsZero() {
		req.NotBefore = now
	}
	if req.ExpiresAt.IsZero() {
		req.ExpiresAt = req.NotBefore.Add(MaxGrantLifetime)
	}
	if err := validateGrantRequest(req); err != nil {
		return Grant{}, err
	}
	if req.ExpiresAt.Before(now) || !req.ExpiresAt.After(req.NotBefore) || req.ExpiresAt.Sub(req.NotBefore) > MaxGrantLifetime {
		return Grant{}, fmt.Errorf("%w: grant lifetime must be between now and seven days", ErrInvalidGrant)
	}
	if req.UserAuthority.Tenant != req.Tenant || req.UserAuthority.OrganizationScopeID != req.OrganizationScopeID {
		return Grant{}, fmt.Errorf("%w: user authority is not for the grant tenant and organization", ErrScopeExpanded)
	}
	current := authorityWindow(req.UserAuthority, req.NotBefore, req.ExpiresAt)
	if !current.Assurance.AtLeast(trust.AssuranceLow) {
		return Grant{}, fmt.Errorf("%w: user assurance is unspecified", ErrScopeExpanded)
	}
	epoch := s.store.CurrentRevocationEpoch(req.Tenant, req.UserID)
	if epoch == 0 {
		epoch = 1
	}
	// Persist only the intersection. A grant is a durable ceiling, so a
	// requested skill scope that the user does not currently hold must not be
	// retained as if it were authority that a later exchange may acquire.
	narrowedSkillScopes := make(map[string][]string, len(req.SkillScopes))
	for skill, scopes := range req.SkillScopes {
		narrowed := intersect(scopes, current.Capabilities)
		if len(narrowed) == 0 {
			return Grant{}, fmt.Errorf("%w: skill %q has no current user authority", ErrScopeExpanded, skill)
		}
		narrowedSkillScopes[skill] = sortedClone(narrowed)
	}
	capabilities := skillCapabilities(narrowedSkillScopes)
	grantAuthority := trust.DelegationGrant{
		GrantID: req.GrantID, RootID: req.GrantID, Kind: trust.GrantKindDirect,
		Delegator: req.UserID, Delegate: delegationTarget(req), Tenant: req.Tenant,
		OrganizationScopeID: req.OrganizationScopeID, Capabilities: capabilities,
		Resources: slices.Clone(current.Resources), Fields: slices.Clone(current.Fields),
		Purposes: []string{req.Purpose}, NotBefore: req.NotBefore, ExpiresAt: req.ExpiresAt,
		RequiredAssurance: current.Assurance, RevocationEpoch: epoch,
	}
	grantScope := trust.AuthorityScope{Tenant: req.Tenant, OrganizationScopeID: req.OrganizationScopeID,
		Capabilities: capabilities, Resources: grantAuthority.Resources, Fields: grantAuthority.Fields,
		Purposes: grantAuthority.Purposes, Assurance: current.Assurance, NotBefore: req.NotBefore, ExpiresAt: req.ExpiresAt}
	if _, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: grantAuthority, Delegator: current, Delegate: grantScope, EvaluatedAt: req.NotBefore, CurrentRevocationEpoch: epoch}); err != nil {
		return Grant{}, fmt.Errorf("%w: %v", ErrScopeExpanded, err)
	}
	g := Grant{GrantID: req.GrantID, UserID: req.UserID, Tenant: req.Tenant,
		AgentVersion: req.AgentVersion, TargetAgentID: req.TargetAgentID, InstallationID: req.InstallationID, TaskID: req.TaskID,
		PlanSkillSetDigest: req.PlanSkillSetDigest, Purpose: req.Purpose,
		OrganizationScopeID: req.OrganizationScopeID, Skills: slices.Clone(req.Skills),
		SkillScopes: cloneSkillScopes(narrowedSkillScopes), NotBefore: req.NotBefore.UTC(),
		ExpiresAt: req.ExpiresAt.UTC(), RevocationEpoch: epoch, Authority: grantAuthority,
		ParentGrantID: req.parentGrantID, ParentActor: cloneActor(req.parentActor), CommonAdmissionID: req.CommonAdmissionID}
	if err := s.store.Save(g); err != nil {
		return Grant{}, err
	}
	return cloneGrant(g), nil
}

// CreateScopedGrant creates a grant whose authority is independently bounded
// for every skill. It never falls back to the legacy flat authority path.
func (s *Service) CreateScopedGrant(req GrantRequest) (Grant, error) {
	if req.SkillAuthorities == nil || req.UserAuthority.SkillAuthorities == nil {
		return Grant{}, fmt.Errorf("%w: complete per-skill authority is required", ErrScopeExpanded)
	}
	for skill := range req.SkillAuthorities {
		if !contains(req.Skills, skill) {
			return Grant{}, fmt.Errorf("%w: authority contains unrequested skill %q", ErrScopeExpanded, skill)
		}
	}
	for _, skill := range req.Skills {
		requested, ok := req.SkillAuthorities[skill]
		current, currentOK := req.UserAuthority.SkillAuthorities[skill]
		if !ok || !currentOK {
			return Grant{}, fmt.Errorf("%w: missing authority for skill %q", ErrScopeExpanded, skill)
		}
		if !subset(requested.Capabilities, current.Capabilities) || !subset(requested.Resources, current.Resources) || !subset(requested.Fields, current.Fields) || !subset(requested.Purposes, current.Purposes) {
			return Grant{}, fmt.Errorf("%w: skill %q authority widens current authority", ErrScopeExpanded, skill)
		}
		if len(intersect(requested.Capabilities, current.Capabilities)) == 0 || len(intersect(requested.Resources, current.Resources)) == 0 || len(intersect(requested.Purposes, current.Purposes)) == 0 {
			return Grant{}, fmt.Errorf("%w: skill %q authority is not within current authority", ErrScopeExpanded, skill)
		}
	}
	base := req.UserAuthority
	base.SkillAuthorities = trust.CloneSkillAuthorities(req.UserAuthority.SkillAuthorities)
	current := trust.IntersectSkillAuthorities(req.SkillAuthorities, base.SkillAuthorities)
	if current == nil {
		return Grant{}, fmt.Errorf("%w: per-skill authority intersection is empty", ErrScopeExpanded)
	}
	narrowedSkillScopes := make(map[string][]string, len(req.Skills))
	for _, skill := range req.Skills {
		a := current[skill]
		requestedScopes := intersect(req.SkillScopes[skill], a.Capabilities)
		if len(requestedScopes) == 0 || len(a.Resources) == 0 || len(a.Purposes) == 0 || !contains(a.Purposes, req.Purpose) {
			return Grant{}, fmt.Errorf("%w: skill %q has empty scoped authority", ErrScopeExpanded, skill)
		}
		narrowedSkillScopes[skill] = sortedClone(requestedScopes)
		current[skill] = a
	}
	// Reuse the compatibility path for common validation and lifecycle bounds,
	// then replace its flat result with the canonical scoped result.
	flat := req.UserAuthority
	flat.Capabilities = skillCapabilities(narrowedSkillScopes)
	flat.Resources, flat.Fields, flat.Purposes = nil, nil, []string{req.Purpose}
	for _, a := range current {
		flat.Resources = append(flat.Resources, a.Resources...)
		flat.Fields = append(flat.Fields, a.Fields...)
	}
	flat.Capabilities = skillCapabilities(narrowedSkillScopes)
	flat.SkillAuthorities = current
	req.UserAuthority = flat
	req.SkillScopes = narrowedSkillScopes
	g, err := s.createGrant(req, current)
	if err != nil {
		return Grant{}, err
	}
	return g, nil
}

func (s *Service) createGrant(req GrantRequest, scoped trust.SkillAuthorities) (Grant, error) {
	// This private implementation mirrors CreateGrant while retaining the
	// existing compatibility entry point and explicitly carrying the map.
	now := s.now().UTC()
	if req.NotBefore.IsZero() {
		req.NotBefore = now
	}
	if req.ExpiresAt.IsZero() {
		req.ExpiresAt = req.NotBefore.Add(MaxGrantLifetime)
	}
	if err := validateGrantRequest(req); err != nil {
		return Grant{}, err
	}
	if req.ExpiresAt.Before(now) || !req.ExpiresAt.After(req.NotBefore) || req.ExpiresAt.Sub(req.NotBefore) > MaxGrantLifetime {
		return Grant{}, fmt.Errorf("%w: grant lifetime must be between now and seven days", ErrInvalidGrant)
	}
	if req.UserAuthority.Tenant != req.Tenant || req.UserAuthority.OrganizationScopeID != req.OrganizationScopeID {
		return Grant{}, fmt.Errorf("%w: user authority is not for the grant tenant and organization", ErrScopeExpanded)
	}
	if !req.UserAuthority.Assurance.AtLeast(trust.AssuranceLow) {
		return Grant{}, fmt.Errorf("%w: user assurance is unspecified", ErrScopeExpanded)
	}
	epoch := s.store.CurrentRevocationEpoch(req.Tenant, req.UserID)
	if epoch == 0 {
		epoch = 1
	}
	flatCaps := skillCapabilities(req.SkillScopes)
	resources, fields, purposes := []string{}, []string{}, []string{}
	for _, a := range scoped {
		resources = append(resources, a.Resources...)
		fields = append(fields, a.Fields...)
		purposes = append(purposes, a.Purposes...)
	}
	grantAuthority := trust.DelegationGrant{GrantID: req.GrantID, RootID: req.GrantID, Kind: trust.GrantKindDirect, Delegator: req.UserID, Delegate: delegationTarget(req), Tenant: req.Tenant, OrganizationScopeID: req.OrganizationScopeID, Capabilities: flatCaps, Resources: uniqueSorted(resources), Fields: uniqueSorted(fields), Purposes: uniqueSorted(purposes), SkillAuthorities: trust.CloneSkillAuthorities(scoped), NotBefore: req.NotBefore, ExpiresAt: req.ExpiresAt, RequiredAssurance: req.UserAuthority.Assurance, RevocationEpoch: epoch}
	grantScope := trust.AuthorityScope{Tenant: req.Tenant, OrganizationScopeID: req.OrganizationScopeID, Capabilities: flatCaps, Resources: grantAuthority.Resources, Fields: grantAuthority.Fields, Purposes: grantAuthority.Purposes, SkillAuthorities: scoped, Assurance: req.UserAuthority.Assurance, NotBefore: req.NotBefore, ExpiresAt: req.ExpiresAt}
	if _, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: grantAuthority, Delegator: req.UserAuthority, Delegate: grantScope, EvaluatedAt: req.NotBefore, CurrentRevocationEpoch: epoch}); err != nil {
		return Grant{}, fmt.Errorf("%w: %v", ErrScopeExpanded, err)
	}
	g := Grant{GrantID: req.GrantID, UserID: req.UserID, Tenant: req.Tenant, AgentVersion: req.AgentVersion, TargetAgentID: req.TargetAgentID, InstallationID: req.InstallationID, TaskID: req.TaskID, PlanSkillSetDigest: req.PlanSkillSetDigest, Purpose: req.Purpose, OrganizationScopeID: req.OrganizationScopeID, Skills: slices.Clone(req.Skills), SkillScopes: cloneSkillScopes(req.SkillScopes), SkillAuthorities: trust.CloneSkillAuthorities(scoped), NotBefore: req.NotBefore.UTC(), ExpiresAt: req.ExpiresAt.UTC(), RevocationEpoch: epoch, Authority: grantAuthority, ParentGrantID: req.parentGrantID, ParentActor: cloneActor(req.parentActor), CommonAdmissionID: req.CommonAdmissionID}
	if err := s.store.Save(g); err != nil {
		return Grant{}, err
	}
	return cloneGrant(g), nil
}

// RevokeGrant revokes a single run grant. BumpUserRevocationEpoch is the
// broad operation used by deactivation, role/session revoke and kill switches.
func (s *Service) RevokeGrant(grantID, reason string) error { return s.store.Revoke(grantID, reason) }

func (s *Service) BumpUserRevocationEpoch(tenant values.TenantId, userID, reason string) (uint64, error) {
	return s.store.BumpRevocationEpoch(tenant, userID, reason)
}

// Exchange mints one short-lived, audience- and workload-bound step token.
// It performs a final grant/epoch read immediately before signing so a
// concurrent revoke cannot return a credential that Verify would accept.
func (s *Service) Exchange(req ExchangeRequest) (DelegatedCredential, error) {
	if s == nil {
		return DelegatedCredential{}, fmt.Errorf("%w: nil service", ErrInvalidRequest)
	}
	if req.SubjectTokenType != DelegationGrantTokenType {
		return DelegatedCredential{}, fmt.Errorf("%w: unsupported subject token type", ErrInvalidRequest)
	}
	g, err := s.store.Get(req.SubjectToken)
	if err != nil {
		return DelegatedCredential{}, err
	}
	now := s.now().UTC()
	if req.Lifetime == 0 {
		req.Lifetime = MaxTokenLifetime
	}
	if err := s.validateExchangeShape(g, req, now); err != nil {
		return DelegatedCredential{}, err
	}
	effective, err := s.effectiveAuthority(g, now)
	if err != nil {
		return DelegatedCredential{}, err
	}
	if g.SkillAuthorities != nil {
		a, ok := effectiveSkillAuthority(effective, req.Skill)
		if !ok || !subset(req.Scope, a.Capabilities) {
			return DelegatedCredential{}, ErrScopeExpanded
		}
	}
	if req.Parent != nil {
		parent, err := s.Verify(req.Parent.Raw, VerifyRequest{Audience: req.Audience, Sender: req.Sender, At: now})
		if err != nil {
			return DelegatedCredential{}, err
		}
		if parent.GrantID != g.GrantID || parent.Subject != g.UserID || parent.Actor.RunID != req.RunID || !subset(req.Scope, parent.Scope) {
			return DelegatedCredential{}, ErrScopeExpanded
		}
	}
	if current := s.store.CurrentRevocationEpoch(g.Tenant, g.UserID); current > g.RevocationEpoch {
		return DelegatedCredential{}, ErrGrantRevoked
	}
	// A second read closes the exchange window used by revocation races.
	latest, err := s.store.Get(g.GrantID)
	if err != nil || latest.Revoked || latest.RevocationEpoch != g.RevocationEpoch {
		return DelegatedCredential{}, ErrGrantRevoked
	}
	actor := ActorClaim{AgentVersion: g.AgentVersion, InstallationID: g.InstallationID, RunID: req.RunID, StepID: req.StepID, Act: cloneActor(g.ParentActor)}
	if req.Parent != nil {
		parent, err := parseSignedClaims(s.secret, req.Parent.Raw)
		if err != nil {
			return DelegatedCredential{}, ErrTokenInvalid
		}
		actor.AgentVersion, actor.InstallationID = req.AgentVersion, req.InstallationID
		actor.Act = &parent.Actor
	}
	if req.Parent != nil && (actor.AgentVersion == "" || actor.InstallationID == "") {
		return DelegatedCredential{}, fmt.Errorf("%w: child actor identity is required", ErrInvalidRequest)
	}
	exp := now.Add(req.Lifetime)
	if exp.After(g.ExpiresAt) {
		exp = g.ExpiresAt
	}
	claims := Claims{TokenType: DelegatedAccessTokenType, Issuer: delegationIssuer,
		Subject: g.UserID, Tenant: g.Tenant.String(), GrantID: g.GrantID, Purpose: g.Purpose, Skill: req.Skill,
		Scope: slices.Clone(req.Scope), Audience: req.Audience, SenderConstraint: req.Sender,
		RevocationEpoch: g.RevocationEpoch, IssuedAtUnix: now.Unix(), ExpiresAtUnix: exp.Unix(), Actor: actor}
	claims.Scope = sortedClone(claims.Scope)
	claims.JTI = tokenID(s.secret, claims)
	raw, err := signClaims(s.secret, claims)
	if err != nil {
		return DelegatedCredential{}, err
	}
	return DelegatedCredential{Raw: raw, Claims: claims}, nil
}

// Verify authenticates a transport token and re-resolves the signed-in
// user's current authority. It returns identity claims only; callers must
// pass those claims to the capability PDP for the actual decision.
func (s *Service) Verify(raw string, req VerifyRequest) (Claims, error) {
	if s == nil {
		return Claims{}, ErrTokenInvalid
	}
	claims, err := parseSignedClaims(s.secret, raw)
	if err != nil {
		return Claims{}, err
	}
	now := req.At
	if now.IsZero() {
		now = s.now().UTC()
	} else {
		now = now.UTC()
	}
	if req.Audience == "" || claims.Audience != req.Audience {
		return Claims{}, ErrTokenAudienceMismatch
	}
	if req.Sender == "" || claims.SenderConstraint != req.Sender {
		return Claims{}, ErrTokenSenderMismatch
	}
	if now.Unix() < claims.IssuedAtUnix || now.Unix() >= claims.ExpiresAtUnix {
		return Claims{}, ErrTokenExpired
	}
	g, err := s.store.Get(claims.GrantID)
	if err != nil {
		return Claims{}, ErrTokenRevoked
	}
	if g.Revoked || g.Authority.Revoked || claims.RevocationEpoch < s.store.CurrentRevocationEpoch(g.Tenant, g.UserID) || claims.RevocationEpoch != g.RevocationEpoch {
		return Claims{}, ErrTokenRevoked
	}
	if claims.Subject != g.UserID || claims.Tenant != g.Tenant.String() || claims.Purpose != g.Purpose || now.Before(g.NotBefore) || !now.Before(g.ExpiresAt) || !actorChainMatchesGrant(claims.Actor, g) {
		return Claims{}, ErrTokenInvalid
	}
	if req.Skill != "" && req.Skill != claims.Skill {
		return Claims{}, ErrSkillNotGranted
	}
	if !contains(g.Skills, claims.Skill) || !subset(claims.Scope, g.SkillScopes[claims.Skill]) {
		return Claims{}, ErrSkillNotGranted
	}
	if len(req.Scope) > 0 && !subset(req.Scope, claims.Scope) {
		return Claims{}, ErrScopeExpanded
	}
	effective, err := s.effectiveAuthority(g, now)
	if err != nil {
		return Claims{}, err
	}
	allowed := effective.Capabilities
	if g.SkillAuthorities != nil {
		a, ok := effectiveSkillAuthority(effective, claims.Skill)
		if !ok {
			return Claims{}, ErrSkillNotGranted
		}
		allowed = a.Capabilities
	}
	if !subset(claims.Scope, allowed) || (len(req.Scope) > 0 && !subset(req.Scope, allowed)) {
		return Claims{}, ErrScopeExpanded
	}
	return claims, nil
}

func (s *Service) validateExchangeShape(g Grant, req ExchangeRequest, now time.Time) error {
	if !safeText(req.SubjectToken) || req.SubjectToken != g.GrantID || !safeText(req.RunID) || !safeText(req.StepID) || !safeText(req.Skill) {
		return fmt.Errorf("%w: subject token, run, step and skill are required", ErrInvalidRequest)
	}
	if !safeText(req.Audience) {
		return ErrAudienceRequired
	}
	if !safeText(req.Sender) {
		return ErrSenderRequired
	}
	if req.Lifetime == 0 {
		req.Lifetime = MaxTokenLifetime
	}
	if req.Lifetime <= 0 || req.Lifetime > MaxTokenLifetime {
		return fmt.Errorf("%w: token lifetime exceeds five minutes", ErrInvalidRequest)
	}
	if g.Revoked || g.Authority.Revoked {
		return ErrGrantRevoked
	}
	if now.Before(g.NotBefore) || !now.Before(g.ExpiresAt) {
		return ErrGrantExpired
	}
	if !contains(g.Skills, req.Skill) {
		return ErrSkillNotGranted
	}
	if len(req.Scope) == 0 || !validSet(req.Scope) || !subset(req.Scope, g.SkillScopes[req.Skill]) {
		return ErrScopeExpanded
	}
	if req.Parent == nil {
		if req.AgentVersion != "" || req.InstallationID != "" {
			return fmt.Errorf("%w: root actor identity is grant-bound", ErrInvalidRequest)
		}
	} else if !safeText(req.AgentVersion) || !safeText(req.InstallationID) {
		return fmt.Errorf("%w: child actor identity is required", ErrInvalidRequest)
	}
	return nil
}

func (s *Service) effectiveAuthority(g Grant, at time.Time) (trust.EffectiveAuthority, error) {
	if err := s.checkParentGrant(g, at, map[string]bool{}); err != nil {
		return trust.EffectiveAuthority{}, err
	}
	snapshot, err := s.authority.Resolve(g.UserID, g.Tenant, g.Purpose, at)
	if err != nil {
		return trust.EffectiveAuthority{}, err
	}
	if snapshot.UserID != "" && snapshot.UserID != g.UserID {
		return trust.EffectiveAuthority{}, fmt.Errorf("%w: resolver returned another user", ErrUserInactive)
	}
	if !snapshot.Active {
		return trust.EffectiveAuthority{}, ErrUserInactive
	}
	current := authorityWindow(snapshot.Authority, at, g.ExpiresAt)
	if g.SkillAuthorities != nil {
		current.SkillAuthorities = trust.CloneSkillAuthorities(snapshot.SkillAuthorities)
		if current.SkillAuthorities == nil {
			return trust.EffectiveAuthority{}, ErrScopeExpanded
		}
	}
	if current.Tenant != g.Tenant || current.OrganizationScopeID != g.OrganizationScopeID {
		return trust.EffectiveAuthority{}, ErrScopeExpanded
	}
	delegate := trust.AuthorityScope{Tenant: g.Tenant, OrganizationScopeID: g.OrganizationScopeID,
		Capabilities: skillCapabilities(g.SkillScopes), Resources: slices.Clone(g.Authority.Resources),
		Fields: slices.Clone(g.Authority.Fields), Purposes: slices.Clone(g.Authority.Purposes),
		Assurance: g.Authority.RequiredAssurance, NotBefore: g.NotBefore, ExpiresAt: g.ExpiresAt}
	if g.SkillAuthorities != nil {
		delegate.SkillAuthorities = trust.CloneSkillAuthorities(g.SkillAuthorities)
	}
	eff, err := trust.EvaluateDelegation(trust.DelegationRequest{Grant: g.Authority, Delegator: current, Delegate: delegate, EvaluatedAt: at, CurrentRevocationEpoch: s.store.CurrentRevocationEpoch(g.Tenant, g.UserID)})
	if err != nil {
		if errors.Is(err, trust.ErrDelegationRevoked) {
			return trust.EffectiveAuthority{}, ErrGrantRevoked
		}
		return trust.EffectiveAuthority{}, fmt.Errorf("%w: %v", ErrScopeExpanded, err)
	}
	narrowed, err := trust.NarrowToCurrentAuthority(eff, current, at)
	if err != nil {
		return trust.EffectiveAuthority{}, fmt.Errorf("%w: %v", ErrScopeExpanded, err)
	}
	return narrowed, nil
}

func effectiveSkillAuthority(eff trust.EffectiveAuthority, skill string) (trust.SkillAuthority, bool) {
	if eff.SkillAuthorities == nil {
		return trust.SkillAuthority{}, false
	}
	a, ok := eff.SkillAuthorities[skill]
	return a, ok
}

func authorityWindow(a trust.AuthorityScope, notBefore, expiresAt time.Time) trust.AuthorityScope {
	if a.NotBefore.IsZero() {
		a.NotBefore = notBefore.Add(-time.Nanosecond)
	}
	if a.ExpiresAt.IsZero() {
		a.ExpiresAt = expiresAt
	}
	return a
}

func validateGrantRequest(r GrantRequest) error {
	for name, value := range map[string]string{"grant_id": r.GrantID, "user_id": r.UserID, "agent_version": r.AgentVersion, "installation_id": r.InstallationID, "task_id": r.TaskID, "skill_set_digest": r.PlanSkillSetDigest, "purpose": r.Purpose, "organization_scope": r.OrganizationScopeID} {
		if !safeText(value) {
			return fmt.Errorf("%w: %s is required and must be printable", ErrInvalidGrant, name)
		}
	}
	if r.TargetAgentID != "" && !safeText(r.TargetAgentID) {
		return fmt.Errorf("%w: target_agent_id must be printable when supplied", ErrInvalidGrant)
	}
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidGrant, err)
	}
	if len(r.Skills) == 0 || !validSet(r.Skills) {
		return fmt.Errorf("%w: skills are required and unique", ErrInvalidGrant)
	}
	for skill, scopes := range r.SkillScopes {
		if !contains(r.Skills, skill) || len(scopes) == 0 || !validSet(scopes) {
			return fmt.Errorf("%w: invalid scopes for skill %q", ErrInvalidGrant, skill)
		}
	}
	for _, skill := range r.Skills {
		if len(r.SkillScopes[skill]) == 0 {
			return fmt.Errorf("%w: skill %q has no scope", ErrInvalidGrant, skill)
		}
	}
	return nil
}

// ValidateGrant applies the same structural rules MemoryGrantStore.Save
// enforces, so durable GrantStore adapters accept and refuse exactly the
// grants the reference implementation does.
func ValidateGrant(g Grant) error { return validateGrant(g) }

func validateGrant(g Grant) error {
	if (g.ParentGrantID == "") != (g.ParentActor == nil) || (g.ParentActor != nil && (!safeText(g.ParentGrantID) || !validActor(*g.ParentActor, 1))) {
		return fmt.Errorf("%w: invalid child grant lineage", ErrInvalidGrant)
	}
	if g.CommonAdmissionID != "" && (!safeText(g.CommonAdmissionID) || g.ParentActor == nil) {
		return fmt.Errorf("%w: common admission requires an exact child grant", ErrInvalidGrant)
	}
	if err := validateGrantRequest(GrantRequest{GrantID: g.GrantID, UserID: g.UserID, Tenant: g.Tenant, AgentVersion: g.AgentVersion, TargetAgentID: g.TargetAgentID, InstallationID: g.InstallationID, TaskID: g.TaskID, PlanSkillSetDigest: g.PlanSkillSetDigest, Purpose: g.Purpose, OrganizationScopeID: g.OrganizationScopeID, Skills: g.Skills, SkillScopes: g.SkillScopes}); err != nil {
		return err
	}
	if g.Authority.Delegate != delegationTarget(GrantRequest{AgentVersion: g.AgentVersion, TargetAgentID: g.TargetAgentID}) {
		return fmt.Errorf("%w: authority target does not match the exact grant target", ErrInvalidGrant)
	}
	if g.NotBefore.IsZero() || g.ExpiresAt.IsZero() || !g.ExpiresAt.After(g.NotBefore) || g.ExpiresAt.Sub(g.NotBefore) > MaxGrantLifetime || g.RevocationEpoch == 0 {
		return fmt.Errorf("%w: validity and revocation epoch are required", ErrInvalidGrant)
	}
	return nil
}

func delegationTarget(request GrantRequest) string {
	if request.TargetAgentID != "" {
		return request.TargetAgentID
	}
	return request.AgentVersion
}

func safeText(s string) bool {
	return s != "" && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\r\n\x00")
}

func validSet(set []string) bool {
	seen := make(map[string]struct{}, len(set))
	for _, item := range set {
		if !safeText(item) {
			return false
		}
		if _, exists := seen[item]; exists {
			return false
		}
		seen[item] = struct{}{}
	}
	return true
}

func skillCapabilities(scopes map[string][]string) []string {
	var out []string
	for _, values := range scopes {
		out = append(out, values...)
	}
	return uniqueSorted(out)
}

func subset(want, allowed []string) bool {
	for _, value := range want {
		if !contains(allowed, value) {
			return false
		}
	}
	return true
}

func intersect(left, right []string) []string {
	out := make([]string, 0, len(left))
	for _, value := range left {
		if contains(right, value) && !contains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

func contains(set []string, want string) bool { return slices.Contains(set, want) }

func uniqueSorted(set []string) []string {
	out := make([]string, 0, len(set))
	for _, value := range set {
		if !contains(out, value) {
			out = append(out, value)
		}
	}
	slices.Sort(out)
	return out
}

func sortedClone(set []string) []string { return uniqueSorted(set) }

func cloneSkillScopes(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for skill, scopes := range in {
		out[skill] = sortedClone(scopes)
	}
	return out
}

func cloneGrant(g Grant) Grant {
	g.ParentActor = cloneActor(g.ParentActor)
	g.Skills = slices.Clone(g.Skills)
	g.SkillScopes = cloneSkillScopes(g.SkillScopes)
	g.Authority.Capabilities = slices.Clone(g.Authority.Capabilities)
	g.Authority.Resources = slices.Clone(g.Authority.Resources)
	g.Authority.Fields = slices.Clone(g.Authority.Fields)
	g.Authority.Purposes = slices.Clone(g.Authority.Purposes)
	g.SkillAuthorities = trust.CloneSkillAuthorities(g.SkillAuthorities)
	g.Authority.SkillAuthorities = trust.CloneSkillAuthorities(g.Authority.SkillAuthorities)
	return g
}

func signClaims(secret []byte, claims Claims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("%w: encoding claims", ErrTokenInvalid)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return "adt1." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func tokenID(secret []byte, claims Claims) string {
	claims.JTI = ""
	payload, _ := json.Marshal(claims)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return "adt-" + hex.EncodeToString(mac.Sum(nil)[:16])
}

func parseSignedClaims(secret []byte, raw string) (Claims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != "adt1" || parts[1] == "" || parts[2] == "" {
		return Claims{}, ErrTokenInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(payload) > 64<<10 {
		return Claims{}, ErrTokenInvalid
	}
	presented, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrTokenInvalid
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(presented, mac.Sum(nil)) {
		return Claims{}, ErrTokenInvalid
	}
	var claims Claims
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&claims); err != nil || claims.TokenType != DelegatedAccessTokenType || claims.Issuer != delegationIssuer || claims.JTI == "" || !safeText(claims.Subject) || !safeText(claims.Tenant) || !safeText(claims.GrantID) || !safeText(claims.Purpose) || !safeText(claims.Skill) || len(claims.Scope) == 0 || !safeText(claims.Audience) || !safeText(claims.SenderConstraint) || claims.ExpiresAtUnix <= claims.IssuedAtUnix {
		return Claims{}, ErrTokenInvalid
	}
	if !validSet(claims.Scope) || !validActor(claims.Actor, 0) || tokenID(secret, claims) != claims.JTI {
		return Claims{}, ErrTokenInvalid
	}
	return claims, nil
}

func validActor(actor ActorClaim, depth int) bool {
	if depth >= maxActorDepth || !safeText(actor.AgentVersion) || !safeText(actor.InstallationID) || !safeText(actor.RunID) || !safeText(actor.StepID) {
		return false
	}
	return actor.Act == nil || validActor(*actor.Act, depth+1)
}

func actorChainMatchesGrant(actor ActorClaim, g Grant) bool {
	if g.ParentActor != nil {
		return actor.AgentVersion == g.AgentVersion && actor.InstallationID == g.InstallationID && actor.RunID == g.TaskID && sameActor(actor.Act, g.ParentActor)
	}
	root := actor
	for root.Act != nil {
		if root.Act.RunID != actor.RunID {
			return false
		}
		root = *root.Act
	}
	return root.AgentVersion == g.AgentVersion && root.InstallationID == g.InstallationID && root.RunID != ""
}

// parseSignedClaims is intentionally kept private; child exchange uses it
// only after Verify has authenticated the parent token.
