// Package agentconnect owns the agent-facing projection of administrator
// managed connections. It deliberately composes the existing connectivity
// lifecycle, secrets metadata and destination-scoped credential leases; it
// never stores or returns credential material.
package agentconnect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/secrets"
)

var (
	ErrInvalid             = errors.New("agentconnect: invalid request")
	ErrNotFound            = errors.New("agentconnect: connection not found")
	ErrDenied              = errors.New("agentconnect: access denied")
	ErrConnectionRevoked   = errors.New("agentconnect: connection revoked")
	ErrAccountNotLinked    = errors.New("agentconnect: external account is not linked")
	ErrLeaseFenced         = errors.New("agentconnect: credential lease fenced")
	ErrCredential          = errors.New("agentconnect: external credential unusable")
	ErrSecondAdminRequired = errors.New("agentconnect: second administrator approval required")
)

// CredentialMode determines whose external account is used.
type CredentialMode string

const (
	UserDelegated CredentialMode = "USER_DELEGATED"
	Brokered      CredentialMode = "BROKERED"
)

// SideEffectTier is the agent product's side-effect ladder. Brokered
// credentials are intentionally limited to T0 reads, with an explicit
// record-filter declaration for per-user-filtered reads.
type SideEffectTier string

const (
	TierT0 SideEffectTier = "T0_READ"
	TierT1 SideEffectTier = "T1_PRIVATE_DRAFT"
	TierT2 SideEffectTier = "T2_COMMUNICATE"
	TierT3 SideEffectTier = "T3_SUBMIT_GOVERNED"
	TierT4 SideEffectTier = "T4_EXTERNAL_WRITE"
)

// AnyScope is the explicit, reviewable wildcard for a grant dimension.
// Empty dimensions are invalid and never act as wildcards.
const AnyScope = "*"

// CredentialBinding contains only secret metadata and an opaque custody
// handle. The handle is what the existing lease manager resolves at use time.
type CredentialBinding struct {
	Reference secrets.SecretReference
	Handle    custody.Handle
}

// AccountLink is the metadata-only record for one user's external account.
// ExternalAccountID is an identifier at the provider, never a credential.
type AccountLink struct {
	UserID            string
	ConnectionID      string
	ExternalAccountID string
	Binding           CredentialBinding
	LinkedAt          time.Time
}

func (b CredentialBinding) validate(tenant string) error {
	if err := b.Reference.Validate(); err != nil {
		return fmt.Errorf("%w: credential reference: %v", ErrInvalid, err)
	}
	if b.Reference.Tenant != tenant || b.Handle.Tenant != tenant || b.Reference.ID != b.Handle.ID || b.Reference.Version != b.Handle.Version || b.Reference.Region != b.Handle.Region {
		return fmt.Errorf("%w: credential tenant does not match connection", ErrInvalid)
	}
	if b.Reference.State != secrets.Active && b.Reference.State != secrets.ActiveNew && b.Reference.State != secrets.Rotating {
		return fmt.Errorf("%w: credential reference is not usable", ErrCredential)
	}
	if err := b.Handle.Validate(); err != nil {
		return fmt.Errorf("%w: credential handle: %v", ErrInvalid, err)
	}
	return nil
}

func (b CredentialBinding) validateUserOAuth(tenant string) error {
	if err := b.validate(tenant); err != nil {
		return err
	}
	if b.Reference.Kind != secrets.OAuthGrant || b.Handle.Kind != custody.Secret {
		return fmt.Errorf("%w: linked account credential must be an OAuth grant secret", ErrInvalid)
	}
	return nil
}

// SkillExposure binds one reviewed agent skill to a connector operation.
// Tool is the existing agentsecurity descriptor; this package does not create
// a second tool gateway or bypass its security metadata.
type SkillExposure struct {
	ID                  string
	Version             string
	Tool                agentsecurity.ToolDescriptor
	Tier                SideEffectTier
	CredentialOperation custody.Operation
	SharedRead          bool
	RecordFilter        string
}

func (s SkillExposure) name() string {
	if strings.TrimSpace(s.ID) != "" {
		return s.ID
	}
	return s.Tool.Name
}

func (s SkillExposure) validate(mode CredentialMode) error {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Version) == "" || !s.Tier.valid() || s.CredentialOperation == "" {
		return fmt.Errorf("%w: skill exposure is incomplete", ErrInvalid)
	}
	if s.Tool.Name != "" && s.Tool.Name != s.ID {
		return fmt.Errorf("%w: skill %q does not match its tool descriptor", ErrInvalid, s.ID)
	}
	if mode == Brokered {
		if s.Tier != TierT0 {
			return fmt.Errorf("%w: brokered credentials may expose only T0 reads", ErrDenied)
		}
		if !s.SharedRead && strings.TrimSpace(s.RecordFilter) == "" {
			return fmt.Errorf("%w: brokered skill %q needs a declared record filter", ErrInvalid, s.ID)
		}
		if s.SharedRead && strings.TrimSpace(s.RecordFilter) != "" {
			return fmt.Errorf("%w: brokered shared read cannot also declare a per-user filter", ErrInvalid)
		}
	}
	return nil
}

// GrantScope is the role/population/organization binding for one set of
// skills. All dimensions are required so an omitted admin filter cannot
// silently become tenant-wide access.
type GrantScope struct {
	ID                 string
	Roles              []string
	Population         string
	OrganizationScopes []string
	Skills             []string
}

func (g GrantScope) validate(skillIDs map[string]bool) error {
	if strings.TrimSpace(g.ID) == "" || len(g.Roles) == 0 || strings.TrimSpace(g.Population) == "" || len(g.OrganizationScopes) == 0 || len(g.Skills) == 0 {
		return fmt.Errorf("%w: grant %q is missing a role, population, organization scope or skill", ErrInvalid, g.ID)
	}
	for _, values := range [][]string{g.Roles, g.OrganizationScopes, g.Skills} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
				return fmt.Errorf("%w: grant %q contains an empty scope", ErrInvalid, g.ID)
			}
		}
	}
	if strings.TrimSpace(g.Population) != g.Population {
		return fmt.Errorf("%w: grant %q has an invalid population", ErrInvalid, g.ID)
	}
	for _, skillID := range g.Skills {
		if !skillIDs[skillID] {
			return fmt.Errorf("%w: grant %q references unknown skill %q", ErrInvalid, g.ID, skillID)
		}
	}
	return nil
}

// UserContext is the server-resolved identity used for every discovery and
// call decision. It is not an agent identity and contains no session token.
type UserContext struct {
	TenantID           string
	UserID             string
	Roles              []string
	Population         string
	OrganizationScopes []string
}

func (u UserContext) validate(tenant string) error {
	if strings.TrimSpace(u.TenantID) == "" || strings.TrimSpace(u.UserID) == "" || strings.TrimSpace(u.Population) == "" || u.TenantID != strings.TrimSpace(u.TenantID) || u.UserID != strings.TrimSpace(u.UserID) || u.Population != strings.TrimSpace(u.Population) || u.TenantID != tenant || len(u.Roles) == 0 || len(u.OrganizationScopes) == 0 {
		return fmt.Errorf("%w: user context is incomplete or tenant-scoped incorrectly", ErrDenied)
	}
	for _, values := range [][]string{u.Roles, u.OrganizationScopes} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
				return fmt.Errorf("%w: user context contains an invalid scope", ErrDenied)
			}
		}
	}
	return nil
}

func containsScope(values []string, want string) bool {
	for _, value := range values {
		if value == want || value == AnyScope {
			return true
		}
	}
	return false
}

func (g GrantScope) matches(u UserContext, skillID string) bool {
	role := false
	for _, userRole := range u.Roles {
		if containsScope(g.Roles, userRole) {
			role = true
			break
		}
	}
	return role && (g.Population == AnyScope || g.Population == u.Population) && containsScope(g.OrganizationScopes, firstMatchingOrganization(u.OrganizationScopes, g.OrganizationScopes)) && containsScope(g.Skills, skillID)
}

func firstMatchingOrganization(userScopes, grantScopes []string) string {
	for _, userScope := range userScopes {
		for _, grantScope := range grantScopes {
			if grantScope == AnyScope || grantScope == userScope {
				return userScope
			}
		}
	}
	return ""
}

func (t SideEffectTier) valid() bool {
	switch t {
	case TierT0, TierT1, TierT2, TierT3, TierT4:
		return true
	default:
		return false
	}
}

// SecondAdminApproval proves separation of duties for a brokered credential.
type SecondAdminApproval struct {
	RequestedBy string
	ApprovedBy  string
	RequestHash string
	ApprovedAt  time.Time
	StepUp      bool
}

// ConnectionRevision binds a published connectivity connection to reviewed
// agent skill exposures and grants. Connection owns external lifecycle; this
// type adds the agent-facing authorization ceiling.
type ConnectionRevision struct {
	ID                 string
	TenantID           string
	Revision           uint64
	Endpoint           string
	Connection         *connectivity.ConnectorConnection
	CredentialMode     CredentialMode
	BrokeredCredential CredentialBinding
	Skills             []SkillExposure
	Grants             []GrantScope
	Approval           *SecondAdminApproval
}

func (c ConnectionRevision) unsignedDigest() (string, error) {
	type skill struct {
		ID, Version, Tier, CredentialOperation, RecordFilter string
		SharedRead                                           bool
		ToolName, ToolCapability, ToolClass, ToolSchema      string
		ToolVersion, ToolCost                                uint32
		ToolDataScope                                        []string
	}
	type grant struct {
		ID, Population                    string
		Roles, OrganizationScopes, Skills []string
	}
	view := struct {
		ID, TenantID, Endpoint, CredentialMode, ConnectorID, ConnectorVersion string
		Revision                                                              uint64
		Skills                                                                []skill
		Grants                                                                []grant
		CredentialReference, CredentialVersion, CredentialHandle              string
	}{ID: c.ID, TenantID: c.TenantID, Endpoint: c.Endpoint, CredentialMode: string(c.CredentialMode), Revision: c.Revision}
	if c.Connection != nil {
		view.ConnectorID = c.Connection.ConnectorID()
		view.ConnectorVersion = c.Connection.ConnectorVersion().String()
	}
	for _, s := range c.Skills {
		scope := append([]string(nil), s.Tool.DataScope...)
		sort.Strings(scope)
		view.Skills = append(view.Skills, skill{ID: s.ID, Version: s.Version, Tier: string(s.Tier), CredentialOperation: string(s.CredentialOperation), RecordFilter: s.RecordFilter, SharedRead: s.SharedRead, ToolName: s.Tool.Name, ToolCapability: s.Tool.Capability, ToolClass: string(s.Tool.Class), ToolSchema: s.Tool.Schema, ToolVersion: s.Tool.Version, ToolCost: uint32(s.Tool.Cost), ToolDataScope: scope})
	}
	for _, g := range c.Grants {
		roles := append([]string(nil), g.Roles...)
		orgs := append([]string(nil), g.OrganizationScopes...)
		skills := append([]string(nil), g.Skills...)
		sort.Strings(roles)
		sort.Strings(orgs)
		sort.Strings(skills)
		view.Grants = append(view.Grants, grant{ID: g.ID, Population: g.Population, Roles: roles, OrganizationScopes: orgs, Skills: skills})
	}
	sort.Slice(view.Skills, func(i, j int) bool { return view.Skills[i].ID < view.Skills[j].ID })
	sort.Slice(view.Grants, func(i, j int) bool { return view.Grants[i].ID < view.Grants[j].ID })
	if c.CredentialMode == Brokered {
		view.CredentialReference = c.BrokeredCredential.Reference.ID
		view.CredentialVersion = c.BrokeredCredential.Reference.Version
		view.CredentialHandle = c.BrokeredCredential.Handle.ID
	}
	b, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ApprovalDigest returns the digest a second administrator must approve.
func (c ConnectionRevision) ApprovalDigest() (string, error) { return c.unsignedDigest() }

func (c ConnectionRevision) validate() error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.TenantID) == "" || c.Revision == 0 || strings.TrimSpace(c.Endpoint) == "" || strings.ContainsAny(c.Endpoint, " \t\r\n") || c.Connection == nil {
		return fmt.Errorf("%w: connection revision is incomplete", ErrInvalid)
	}
	if c.Connection.ID() != c.ID || c.Connection.TenantID() != c.TenantID {
		return fmt.Errorf("%w: connectivity connection does not match revision", ErrInvalid)
	}
	if c.CredentialMode != UserDelegated && c.CredentialMode != Brokered {
		return fmt.Errorf("%w: unknown credential mode %q", ErrInvalid, c.CredentialMode)
	}
	if len(c.Skills) == 0 || len(c.Grants) == 0 {
		return fmt.Errorf("%w: connection exposes no skills or grants", ErrInvalid)
	}
	skillIDs := make(map[string]bool, len(c.Skills))
	for _, skill := range c.Skills {
		if skillIDs[skill.ID] {
			return fmt.Errorf("%w: duplicate skill %q", ErrInvalid, skill.ID)
		}
		if err := skill.validate(c.CredentialMode); err != nil {
			return err
		}
		skillIDs[skill.ID] = true
	}
	for _, grant := range c.Grants {
		if err := grant.validate(skillIDs); err != nil {
			return err
		}
	}
	grantIDs := make(map[string]bool, len(c.Grants))
	for _, grant := range c.Grants {
		if grantIDs[grant.ID] {
			return fmt.Errorf("%w: duplicate grant %q", ErrInvalid, grant.ID)
		}
		grantIDs[grant.ID] = true
	}
	if c.CredentialMode == Brokered {
		if err := c.BrokeredCredential.validate(c.TenantID); err != nil {
			return err
		}
		if c.Approval == nil || strings.TrimSpace(c.Approval.RequestedBy) == "" || strings.TrimSpace(c.Approval.ApprovedBy) == "" || c.Approval.RequestedBy == c.Approval.ApprovedBy || !c.Approval.StepUp || c.Approval.ApprovedAt.IsZero() {
			return ErrSecondAdminRequired
		}
		digest, err := c.unsignedDigest()
		if err != nil {
			return err
		}
		if c.Approval.RequestHash != digest {
			return fmt.Errorf("%w: brokered approval does not cover this revision", ErrSecondAdminRequired)
		}
	} else if c.BrokeredCredential != (CredentialBinding{}) {
		return fmt.Errorf("%w: user-delegated revisions cannot carry a brokered credential", ErrInvalid)
	}
	return nil
}

// LeaseIssuer is the existing destination-scoped lease service. Production
// wiring uses trust/lease.Manager backed by the governed custody/vault port.
type LeaseIssuer interface {
	Mint(lease.Request) (lease.CredentialLease, lease.Evidence, error)
	Use(lease.CredentialLease, string, custody.Operation) (lease.Evidence, error)
	Revoke(string, string) (lease.Evidence, error)
}

// CallLease is the safe handoff to a connector adapter. It records the acting
// user and filter while carrying only the opaque existing credential lease.
type CallLease struct {
	ConnectionID string
	TenantID     string
	UserID       string
	AgentID      string
	RunID        string
	SkillID      string
	Audience     string
	RecordFilter string
	Epoch        uint64
	UserEpoch    uint64
	Credential   lease.CredentialLease
}

type issuedLease struct {
	call CallLease
}

type connectionRecord struct {
	revision ConnectionRevision
	epoch    uint64
}

// Registry is a tenant-scoped, in-memory policy aggregate. A durable adapter
// may persist the same revision and evidence; this value never owns secrets.
type Registry struct {
	mu       sync.RWMutex
	now      func() time.Time
	issuer   LeaseIssuer
	items    map[string]*connectionRecord
	links    map[string]CredentialBinding
	accounts map[string]AccountLink
	userEp   map[string]uint64
	issued   map[string]issuedLease
	// store is the optional durable write-through seam; nil keeps the Registry
	// purely in memory. Leases (issued) are never persisted.
	store Store
}

// NewRegistry creates an agent connection registry over the existing lease
// path. A nil issuer is refused so no caller can accidentally create a
// registry that hands out unbound credentials.
func NewRegistry(issuer LeaseIssuer, now func() time.Time) (*Registry, error) {
	if issuer == nil {
		return nil, fmt.Errorf("%w: lease issuer is required", ErrInvalid)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Registry{now: now, issuer: issuer, items: make(map[string]*connectionRecord), links: make(map[string]CredentialBinding), accounts: make(map[string]AccountLink), userEp: make(map[string]uint64), issued: make(map[string]issuedLease)}, nil
}

func itemKey(tenant, id string) string { return tenant + "\x00" + id }
func userKey(tenant, connection, user string) string {
	return tenant + "\x00" + connection + "\x00" + user
}

func copyRevision(in ConnectionRevision) ConnectionRevision {
	out := in
	out.Skills = make([]SkillExposure, len(in.Skills))
	for i, skill := range in.Skills {
		out.Skills[i] = copySkill(skill)
	}
	out.Grants = make([]GrantScope, len(in.Grants))
	for i, grant := range in.Grants {
		out.Grants[i] = grant
		out.Grants[i].Roles = append([]string(nil), grant.Roles...)
		out.Grants[i].OrganizationScopes = append([]string(nil), grant.OrganizationScopes...)
		out.Grants[i].Skills = append([]string(nil), grant.Skills...)
	}
	if in.Approval != nil {
		approval := *in.Approval
		out.Approval = &approval
	}
	return out
}

func copySkill(in SkillExposure) SkillExposure {
	out := in
	out.Tool.DataScope = append([]string(nil), in.Tool.DataScope...)
	return out
}

func copyBinding(in CredentialBinding) CredentialBinding { return in }

func copyAccount(in AccountLink) AccountLink {
	out := in
	out.Binding = copyBinding(in.Binding)
	return out
}

// Register adds one immutable agent-facing revision. Brokered revisions must
// have a second admin's step-up approval over ApprovalDigest.
func (r *Registry) Register(revision ConnectionRevision) error {
	if r == nil {
		return ErrInvalid
	}
	if err := revision.validate(); err != nil {
		return err
	}
	key := itemKey(revision.TenantID, revision.ID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[key]; exists {
		return fmt.Errorf("%w: connection revision already exists", ErrInvalid)
	}
	if r.store != nil {
		if err := r.store.PutRevision(revisionRecord(revision)); err != nil {
			return err
		}
	}
	r.items[key] = &connectionRecord{revision: copyRevision(revision), epoch: 1}
	return nil
}

// Revision returns a copy of the registered revision. The underlying
// connectivity aggregate remains the lifecycle owner and is not cloned.
func (r *Registry) Revision(tenant, connectionID string) (ConnectionRevision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[itemKey(tenant, connectionID)]
	if !ok {
		return ConnectionRevision{}, ErrNotFound
	}
	return copyRevision(item.revision), nil
}

// LinkAccount records a user's own external OAuth account metadata. It bumps
// that user's epoch first, fencing any cached lease from a prior account.
func (r *Registry) LinkAccount(user UserContext, connectionID string, binding CredentialBinding, externalAccountID string) error {
	if r == nil || strings.TrimSpace(externalAccountID) == "" || strings.TrimSpace(externalAccountID) != externalAccountID {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[itemKey(user.TenantID, connectionID)]
	if !ok {
		return ErrNotFound
	}
	if item.revision.CredentialMode != UserDelegated {
		return fmt.Errorf("%w: brokered connections do not accept user account links", ErrDenied)
	}
	if err := user.validate(item.revision.TenantID); err != nil {
		return err
	}
	if err := binding.validateUserOAuth(item.revision.TenantID); err != nil {
		return err
	}
	key := userKey(user.TenantID, connectionID, user.UserID)
	link := AccountLink{UserID: user.UserID, ConnectionID: connectionID, ExternalAccountID: externalAccountID, Binding: copyBinding(binding), LinkedAt: r.now().UTC()}
	if r.store != nil {
		if err := r.store.PutUserState(UserLinkState{TenantID: user.TenantID, ConnectionID: connectionID, UserID: user.UserID, Epoch: r.userEp[key] + 1, Link: &link}); err != nil {
			return err
		}
	}
	r.userEp[key]++
	r.fenceLeasesLocked(func(call CallLease) bool {
		return call.ConnectionID == connectionID && call.UserID == user.UserID && call.TenantID == user.TenantID
	}, "account relinked")
	r.links[key] = copyBinding(binding)
	r.accounts[key] = link
	return nil
}

// LinkedAccount returns metadata for a user's linked account without exposing
// any provider credential material.
func (r *Registry) LinkedAccount(user UserContext, connectionID string) (AccountLink, error) {
	if r == nil {
		return AccountLink{}, ErrInvalid
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[itemKey(user.TenantID, connectionID)]
	if !ok {
		return AccountLink{}, ErrNotFound
	}
	if err := user.validate(item.revision.TenantID); err != nil {
		return AccountLink{}, err
	}
	account, ok := r.accounts[userKey(user.TenantID, connectionID, user.UserID)]
	if !ok {
		return AccountLink{}, ErrAccountNotLinked
	}
	return copyAccount(account), nil
}

// UnlinkAccount removes a user's external account and fences its leases.
func (r *Registry) UnlinkAccount(user UserContext, connectionID, reason string) error {
	if r == nil || strings.TrimSpace(reason) == "" || strings.TrimSpace(reason) != reason {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[itemKey(user.TenantID, connectionID)]
	if !ok {
		return ErrNotFound
	}
	if err := user.validate(item.revision.TenantID); err != nil {
		return err
	}
	key := userKey(user.TenantID, connectionID, user.UserID)
	if r.store != nil {
		if err := r.store.PutUserState(UserLinkState{TenantID: user.TenantID, ConnectionID: connectionID, UserID: user.UserID, Epoch: r.userEp[key] + 1}); err != nil {
			return err
		}
	}
	r.userEp[key]++
	delete(r.links, key)
	delete(r.accounts, key)
	r.fenceLeasesLocked(func(call CallLease) bool {
		return call.ConnectionID == connectionID && call.UserID == user.UserID && call.TenantID == user.TenantID
	}, reason)
	return nil
}

// RevokeConnection disconnects the underlying ConnectorConnection and bumps
// the connection epoch. Cached agent leases are revoked through the existing
// lease service as well as fenced locally.
func (r *Registry) RevokeConnection(tenant, connectionID, actor, reason, evidenceRef string) error {
	if r == nil || strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(evidenceRef) == "" {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[itemKey(tenant, connectionID)]
	if !ok {
		return ErrNotFound
	}
	revokedAt := r.now().UTC()
	if item.revision.Connection.State() != connectivity.StateRevoked {
		if err := item.revision.Connection.Transition(connectivity.StateRevoked, connectivity.TransitionEvidence{Reason: reason, ActorRef: actor, EvidenceRef: evidenceRef, OccurredAt: revokedAt}); err != nil {
			return err
		}
	}
	item.epoch++
	r.fenceLeasesLocked(func(call CallLease) bool { return call.ConnectionID == connectionID && call.TenantID == tenant }, reason)
	if r.store != nil {
		// The connection is already revoked and fenced in memory, so a store
		// failure is reported without ever leaving the connection usable.
		return r.store.PutRevocation(RevocationRecord{TenantID: tenant, ConnectionID: connectionID, Actor: actor, Reason: reason, EvidenceRef: evidenceRef, RevokedAt: revokedAt, Epoch: item.epoch})
	}
	return nil
}

// Disconnect is an explicit alias for the admin revoke lifecycle action.
func (r *Registry) Disconnect(tenant, connectionID, actor, reason, evidenceRef string) error {
	return r.RevokeConnection(tenant, connectionID, actor, reason, evidenceRef)
}

func (r *Registry) fenceLeasesLocked(match func(CallLease) bool, reason string) {
	for id, issued := range r.issued {
		if match(issued.call) {
			_, _ = r.issuer.Revoke(id, reason)
			delete(r.issued, id)
		}
	}
}

func (r *Registry) findSkill(item *connectionRecord, skillID string) (SkillExposure, bool) {
	for _, skill := range item.revision.Skills {
		if skill.ID == skillID {
			return skill, true
		}
	}
	return SkillExposure{}, false
}

func allowed(item *connectionRecord, user UserContext, skillID string) bool {
	for _, grant := range item.revision.Grants {
		if grant.matches(user, skillID) {
			return true
		}
	}
	return false
}

func (r *Registry) validateAccessLocked(item *connectionRecord, user UserContext, skillID string) (SkillExposure, string, error) {
	if err := user.validate(item.revision.TenantID); err != nil {
		return SkillExposure{}, "", err
	}
	if !item.revision.Connection.Usable() {
		if item.revision.Connection.State() == connectivity.StateRevoked {
			return SkillExposure{}, "", ErrConnectionRevoked
		}
		return SkillExposure{}, "", fmt.Errorf("%w: connection is %s", ErrDenied, item.revision.Connection.State())
	}
	skill, ok := r.findSkill(item, skillID)
	if !ok || !allowed(item, user, skillID) {
		return SkillExposure{}, "", fmt.Errorf("%w: skill %q is not granted to user", ErrDenied, skillID)
	}
	key := userKey(user.TenantID, item.revision.ID, user.UserID)
	if item.revision.CredentialMode == UserDelegated {
		if _, linked := r.links[key]; !linked {
			return SkillExposure{}, "", ErrAccountNotLinked
		}
	}
	return skill, key, nil
}

// EffectiveSkills returns only skills both granted by the revision and
// usable by the current user. It evaluates links and lifecycle at call time.
func (r *Registry) EffectiveSkills(user UserContext, connectionID string) ([]SkillExposure, error) {
	if r == nil {
		return nil, ErrInvalid
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[itemKey(user.TenantID, connectionID)]
	if !ok {
		return nil, ErrNotFound
	}
	if err := user.validate(item.revision.TenantID); err != nil {
		return nil, err
	}
	if !item.revision.Connection.Usable() {
		if item.revision.Connection.State() == connectivity.StateRevoked {
			return nil, ErrConnectionRevoked
		}
		return nil, fmt.Errorf("%w: connection is %s", ErrDenied, item.revision.Connection.State())
	}
	if item.revision.CredentialMode == UserDelegated {
		if _, linked := r.links[userKey(user.TenantID, connectionID, user.UserID)]; !linked {
			return nil, ErrAccountNotLinked
		}
	}
	out := make([]SkillExposure, 0, len(item.revision.Skills))
	for _, skill := range item.revision.Skills {
		if allowed(item, user, skill.ID) {
			out = append(out, copySkill(skill))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// IssueLease authorizes one agent call. The resulting lease is at most five
// minutes, audience-bound to the connection endpoint, and linked to the
// signed-in user rather than to an agent service identity.
func (r *Registry) IssueLease(user UserContext, connectionID, skillID, agentID, runID, purpose string, ttl time.Duration) (CallLease, error) {
	if r == nil || strings.TrimSpace(agentID) == "" || strings.TrimSpace(runID) == "" || strings.TrimSpace(purpose) == "" {
		return CallLease{}, ErrInvalid
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if ttl > 5*time.Minute {
		return CallLease{}, fmt.Errorf("%w: credential leases are limited to five minutes", ErrInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[itemKey(user.TenantID, connectionID)]
	if !ok {
		return CallLease{}, ErrNotFound
	}
	skill, linkKey, err := r.validateAccessLocked(item, user, skillID)
	if err != nil {
		return CallLease{}, err
	}
	binding := item.revision.BrokeredCredential
	if item.revision.CredentialMode == UserDelegated {
		binding = r.links[linkKey]
	}
	if err := binding.validate(item.revision.TenantID); err != nil {
		return CallLease{}, err
	}
	audience := item.revision.Endpoint
	workload := "agent/" + agentID + "/run/" + runID
	credential, _, err := r.issuer.Mint(lease.Request{Handle: binding.Handle, Workload: workload, Tenant: user.TenantID, Purpose: purpose, Destination: audience, Operation: skill.CredentialOperation, TTL: ttl})
	if err != nil {
		return CallLease{}, mapCredentialError(err, connectionID, user.UserID)
	}
	call := CallLease{ConnectionID: connectionID, TenantID: user.TenantID, UserID: user.UserID, AgentID: agentID, RunID: runID, SkillID: skill.ID, Audience: audience, RecordFilter: skill.RecordFilter, Epoch: item.epoch, UserEpoch: r.userEp[userKey(user.TenantID, connectionID, user.UserID)], Credential: credential}
	r.issued[credential.ID] = issuedLease{call: call}
	return call, nil
}

// UseLease consumes a lease only if the connection and user epochs still
// match. It returns the existing lease evidence, which contains no secret.
func (r *Registry) UseLease(call CallLease, destination string, operation custody.Operation) (lease.Evidence, error) {
	if r == nil {
		return lease.Evidence{}, ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	issued, ok := r.issued[call.Credential.ID]
	if !ok || !callLeaseEqual(issued.call, call) {
		return lease.Evidence{}, ErrLeaseFenced
	}
	item, ok := r.items[itemKey(call.TenantID, call.ConnectionID)]
	if !ok {
		return lease.Evidence{}, ErrNotFound
	}
	if item.epoch != call.Epoch || !item.revision.Connection.Usable() {
		return lease.Evidence{}, ErrLeaseFenced
	}
	if item.revision.CredentialMode == UserDelegated && r.userEp[userKey(call.TenantID, call.ConnectionID, call.UserID)] != call.UserEpoch {
		return lease.Evidence{}, ErrLeaseFenced
	}
	if destination != call.Audience {
		return lease.Evidence{}, fmt.Errorf("%w: destination is not the connection audience", ErrDenied)
	}
	if operation != call.Credential.Operation {
		return lease.Evidence{}, fmt.Errorf("%w: operation is not the authorized skill operation", ErrDenied)
	}
	ev, err := r.issuer.Use(call.Credential, destination, operation)
	if err != nil {
		return lease.Evidence{}, mapCredentialError(err, call.ConnectionID, call.UserID)
	}
	delete(r.issued, call.Credential.ID)
	return ev, nil
}

func (c CallLease) CredentialEqual(other CallLease) bool {
	return c.Credential == other.Credential
}

func callLeaseEqual(left, right CallLease) bool {
	return left.ConnectionID == right.ConnectionID && left.TenantID == right.TenantID && left.UserID == right.UserID && left.AgentID == right.AgentID && left.RunID == right.RunID && left.SkillID == right.SkillID && left.Audience == right.Audience && left.RecordFilter == right.RecordFilter && left.Epoch == right.Epoch && left.UserEpoch == right.UserEpoch && left.CredentialEqual(right)
}

// CredentialFault is a typed reconnect prompt for an expired or revoked
// external credential. It carries identifiers and a prompt, never the token.
type CredentialFault struct {
	ConnectionID string
	UserID       string
	Code         string
	Prompt       string
	Cause        error
}

func (e *CredentialFault) Error() string {
	return fmt.Sprintf("%s: reconnect external account for connection %s", e.Code, e.ConnectionID)
}

func (e *CredentialFault) Unwrap() error { return errors.Join(ErrCredential, e.Cause) }

func mapCredentialError(err error, connectionID, userID string) error {
	if errors.Is(err, lease.ErrExpired) || errors.Is(err, custody.ErrExpired) {
		return &CredentialFault{ConnectionID: connectionID, UserID: userID, Code: "EXTERNAL_CREDENTIAL_EXPIRED", Prompt: "Reconnect the external account before retrying.", Cause: err}
	}
	if errors.Is(err, lease.ErrRevoked) || errors.Is(err, custody.ErrDenied) {
		return &CredentialFault{ConnectionID: connectionID, UserID: userID, Code: "EXTERNAL_CREDENTIAL_REVOKED", Prompt: "Reconnect the external account and re-authorize the requested scope.", Cause: err}
	}
	return err
}
