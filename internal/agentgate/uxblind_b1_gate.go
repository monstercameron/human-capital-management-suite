// Package agentgate is the per-call policy boundary for on-behalf-of skills.
//
// A skill pin and an agent run are not authority.  This package resolves the
// signed-in user's current grant, purpose, organization scope, consent and
// capability policy for every call, then filters the result with the same
// field rulings.  It deliberately owns no credentials, persistence or skill
// execution port.
package agentgate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/capability/authority"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var (
	ErrInvalid = errors.New("agentgate: invalid request")
	ErrDenied  = errors.New("agentgate: call denied")
)

// DenialCode is stable, non-sensitive telemetry for a refused discovery or
// call.  A denial never includes a record value, credential or prompt.
type DenialCode string

const (
	DenyInvalid             DenialCode = "INVALID"
	DenySkillNotFound       DenialCode = "SKILL_NOT_FOUND"
	DenySkillNotGranted     DenialCode = "SKILL_NOT_GRANTED"
	DenyRole                DenialCode = "ROLE_NOT_GRANTED"
	DenyPopulation          DenialCode = "POPULATION_OUT_OF_SCOPE"
	DenyOrganization        DenialCode = "ORGANIZATION_OUT_OF_SCOPE"
	DenyPurpose             DenialCode = "PURPOSE_NOT_GRANTED"
	DenyConsent             DenialCode = "CONSENT_REQUIRED_OR_INVALID"
	DenyCapability          DenialCode = "CAPABILITY_NOT_AUTHORIZED"
	DenySubject             DenialCode = "SUBJECT_NOT_AUTHORIZED"
	DenyField               DenialCode = "FIELD_NOT_AUTHORIZED"
	DenyAgent               DenialCode = "AGENT_CONTEXT_INVALID"
	DenyConnectionOperation DenialCode = "CONNECTION_POLICY_REQUIRED"
)

// DeniedError is the typed refusal the agent surface must show to its caller.
// Detail is policy-safe and must not be populated with request arguments.
type DeniedError struct {
	Code       DenialCode
	Skill      agentskills.SkillKey
	Capability capability.Key
	Field      authz.FieldID
	Detail     string
}

func (e *DeniedError) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := []string{string(e.Code)}
	if e.Skill.ID != "" {
		parts = append(parts, e.Skill.String())
	}
	if e.Capability.ID != "" {
		parts = append(parts, e.Capability.String())
	}
	if e.Field != "" {
		parts = append(parts, string(e.Field))
	}
	if e.Detail != "" {
		parts = append(parts, e.Detail)
	}
	return "agentgate: " + strings.Join(parts, ": ")
}

func (e *DeniedError) Unwrap() error { return ErrDenied }

// AnyScope is an explicit grant wildcard.  Empty grant dimensions are never
// interpreted as tenant-wide or organization-wide access.
const AnyScope = "*"

// UserContext is the server-resolved signed-in user. Roles and organization
// scopes are supplied by the current directory view on every call; nil roles
// fall back to the immutable verified principal only for compatibility with
// callers that have no role store yet.
type UserContext struct {
	Principal          *trust.Principal
	Population         string
	Roles              []string
	OrganizationScopes []string
}

// AgentActor identifies the run, but carries no authority.
type AgentActor struct {
	AgentVersion   string
	InstallationID string
	RunID          string
	StepID         string
}

// SkillGrant is one administrator grant for one exact skill version. Every
// dimension is explicit so an omitted filter cannot widen access.
type SkillGrant struct {
	ID                 string
	Tenant             values.TenantId
	Skill              agentskills.SkillKey
	Roles              []string
	Population         string
	OrganizationScopes []string
	Purposes           []string
	ConsentRequired    bool
}

// GrantProvider is the durable admin-grant projection. Implementations must
// return the current view, not a value cached at installation or task start.
type GrantProvider interface {
	Grants(context.Context, values.TenantId, agentskills.SkillKey) ([]SkillGrant, error)
}

// StaticGrants is a small immutable-by-convention provider useful for wiring
// and tests. The gate copies and sorts returned grants before using them.
type StaticGrants []SkillGrant

func (g StaticGrants) Grants(_ context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]SkillGrant, error) {
	out := make([]SkillGrant, 0, len(g))
	for _, grant := range g {
		if grant.Tenant == tenant && grant.Skill == key {
			out = append(out, cloneGrant(grant))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ConsentRequest is evaluated at discovery and again immediately before a
// call. Subjects are exact record references, never a population wildcard.
type ConsentRequest struct {
	User     UserContext
	Skill    agentskills.SkillKey
	Purpose  string
	Subjects []Subject
	At       time.Time
}

// ConsentChecker owns the consent lifecycle. A nil checker is a denial for a
// consent-reliant grant.
type ConsentChecker interface {
	Check(context.Context, ConsentRequest) error
}

// ConsentCheckerFunc adapts a function to ConsentChecker.
type ConsentCheckerFunc func(context.Context, ConsentRequest) error

func (f ConsentCheckerFunc) Check(ctx context.Context, req ConsentRequest) error {
	if f == nil {
		return ErrDenied
	}
	return f(ctx, req)
}

// Subject is the exact record context passed to the policy decision point.
// The organization and relationship projections are facts, not caller-owned
// authority; production adapters resolve them from their owning services.
type Subject struct {
	Ref           values.EntityRef
	Organization  authz.OrgUnitRef
	OrgEdges      []authz.OrgEdge
	Relationships []authz.RelationshipFact
	Sharing       []authz.SharingGrant
}

// CapabilityRequest is the per-capability policy input. Principal is always
// the signed-in user; Actor is recorded as context and is never substituted
// for that principal.
type CapabilityRequest struct {
	Principal   *trust.Principal
	Roles       []string
	Actor       AgentActor
	Skill       agentskills.SkillKey
	Capability  capability.Record
	Purpose     string
	Subjects    []Subject
	Fields      []authz.FieldID
	EffectiveAt time.Time
}

// SubjectDecision carries only effects, not values. Redacted fields remain
// visible as a marker and raw denied fields never cross the gate.
type SubjectDecision struct {
	Subject values.EntityRef
	Fields  map[authz.FieldID]authz.Effect
}

// CapabilityDecision is one PDP ruling for one underlying capability.
type CapabilityDecision struct {
	Capability capability.Key
	Allowed    bool
	Subjects   []SubjectDecision
	Reason     string
}

// CapabilityAuthorizer is intentionally narrower than any transport or
// capability implementation. The default implementation below composes the
// existing capability authority and trust/authz PDPs.
type CapabilityAuthorizer interface {
	Authorize(context.Context, CapabilityRequest) (CapabilityDecision, error)
}

// PolicyDecisionPoint is an alias that makes the composition seam explicit
// to callers that already name their service a PDP.
type PolicyDecisionPoint = CapabilityAuthorizer

// AuthorizationPDP composes capability-level authority and record/field
// policy. It never caches a decision.
type AuthorizationPDP struct{}

func (AuthorizationPDP) Authorize(ctx context.Context, req CapabilityRequest) (CapabilityDecision, error) {
	if err := contextErr(ctx); err != nil {
		return CapabilityDecision{}, err
	}
	if req.Principal == nil || req.Capability.Definition.ID == "" || req.Purpose == "" || req.EffectiveAt.IsZero() {
		return CapabilityDecision{}, &DeniedError{Code: DenyInvalid, Capability: req.Capability.Definition.Key(), Detail: "policy input is incomplete"}
	}
	for _, field := range req.Fields {
		if !definitionCoversField(req.Capability.Definition, field) {
			return CapabilityDecision{}, &DeniedError{Code: DenyField, Capability: req.Capability.Definition.Key(), Field: field, Detail: "field is outside the capability's declared data scope"}
		}
	}
	decision := authority.Authorize(req.Principal, req.Purpose, req.Capability.Definition, authority.Authority{EffectiveRoles: slices.Clone(req.Roles)}, req.EffectiveAt)
	if decision.Decision != capability.Allow {
		return CapabilityDecision{}, &DeniedError{Code: DenyCapability, Capability: req.Capability.Definition.Key(), Detail: "capability policy denied the signed-in user"}
	}

	out := CapabilityDecision{Capability: req.Capability.Definition.Key(), Allowed: true}
	for _, subject := range req.Subjects {
		if err := subject.Ref.Validate(); err != nil {
			return CapabilityDecision{}, &DeniedError{Code: DenySubject, Capability: req.Capability.Definition.Key(), Detail: "subject reference is invalid"}
		}
		authzDecision, err := authz.Enforce(authz.Request{
			Principal: req.Principal, EffectiveRoles: slices.Clone(req.Roles), Purpose: req.Purpose,
			EffectiveAt: values.NewInstant(req.EffectiveAt), Subject: subject.Ref,
			PrincipalOrg: principalOrg(req.Principal), ResourceOrg: subject.Organization,
			OrgEdges: subject.OrgEdges, Sharing: subject.Sharing, Relationships: subject.Relationships,
			Fields: slices.Clone(req.Fields),
		})
		if err != nil {
			return CapabilityDecision{}, &DeniedError{Code: DenySubject, Capability: req.Capability.Definition.Key(), Detail: "record policy could not be evaluated"}
		}
		if !authzDecision.SubjectDisclosable {
			return CapabilityDecision{}, &DeniedError{Code: DenySubject, Capability: req.Capability.Definition.Key(), Detail: "record policy denied the subject"}
		}
		fieldEffects := make(map[authz.FieldID]authz.Effect, len(req.Fields))
		for _, field := range req.Fields {
			ruling, ok := authzDecision.Fields[field]
			if !ok || ruling.Effect == authz.EffectDenied || ruling.Effect == authz.EffectWithheld {
				return CapabilityDecision{}, &DeniedError{Code: DenyField, Capability: req.Capability.Definition.Key(), Field: field, Detail: "field policy denied disclosure"}
			}
			fieldEffects[field] = ruling.Effect
		}
		out.Subjects = append(out.Subjects, SubjectDecision{Subject: subject.Ref, Fields: fieldEffects})
	}
	return out, nil
}

// SkillCatalog is the read-only portion of agentskills.Registry needed by
// this gate. The exact-version API prevents discovery from silently changing
// a task's skill schema.
type SkillCatalog interface {
	List() []agentskills.SkillRecord
	ResolvePin(agentskills.SkillPin) (agentskills.SkillRecord, error)
}

type Config struct {
	Skills  SkillCatalog
	Grants  GrantProvider
	PDP     CapabilityAuthorizer
	Consent ConsentChecker
	Now     func() time.Time
}

// Gate is safe for concurrent use because all dependencies are read ports and
// each call obtains fresh grant and PDP decisions.
type Gate struct {
	skills  SkillCatalog
	grants  GrantProvider
	pdp     CapabilityAuthorizer
	consent ConsentChecker
	now     func() time.Time
}

func New(cfg Config) (*Gate, error) {
	if cfg.Skills == nil || cfg.Grants == nil {
		return nil, fmt.Errorf("%w: skill catalog and grant provider are required", ErrInvalid)
	}
	if cfg.PDP == nil {
		cfg.PDP = AuthorizationPDP{}
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Gate{skills: cfg.Skills, grants: cfg.Grants, pdp: cfg.PDP, consent: cfg.Consent, now: cfg.Now}, nil
}

// NewGate is the descriptive constructor alias used by composition roots.
func NewGate(cfg Config) (*Gate, error) { return New(cfg) }

// DiscoveryRequest supplies the current user and optional query context. If
// subjects and fields are supplied, every underlying capability is checked
// against that exact context during discovery as well.
type DiscoveryRequest struct {
	User     UserContext
	Purpose  string
	Subjects []Subject
	Fields   []authz.FieldID
	At       time.Time
}

// Discover returns only active skill records currently available to the
// signed-in user. A failed candidate is omitted because discovery itself must
// not disclose the existence of an unavailable grant.
func (g *Gate) Discover(ctx context.Context, req DiscoveryRequest) ([]agentskills.SkillRecord, error) {
	if g == nil {
		return nil, fmt.Errorf("%w: nil gate", ErrInvalid)
	}
	at, err := g.resolveTime(req.At)
	if err != nil {
		return nil, err
	}
	if err := validateUser(req.User, req.Purpose); err != nil {
		return nil, err
	}
	if err := validateSubjects(req.User, req.Subjects); err != nil {
		return []agentskills.SkillRecord{}, nil
	}
	out := make([]agentskills.SkillRecord, 0)
	for _, record := range g.skills.List() {
		if record.Status != agentskills.StatusActive {
			continue
		}
		if !skillAllowsPurpose(record.Definition.RequiredPurposes, req.Purpose) {
			continue
		}
		key := record.Definition.Key()
		grant, err := g.matchGrant(ctx, req.User, key, req.Purpose, at, req.Subjects)
		if err != nil {
			continue
		}
		if err := g.authorizeRecord(ctx, req.User, AgentActor{AgentVersion: "discovery", InstallationID: "discovery", RunID: "discovery", StepID: "discovery"}, key, record, grant, req.Purpose, req.Subjects, req.Fields, at); err != nil {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

// CallRequest is the complete per-call binding. A call without an exact
// subject and field set is rejected rather than interpreted as a wildcard.
type CallRequest struct {
	User     UserContext
	Actor    AgentActor
	Skill    agentskills.SkillPin
	Purpose  string
	Subjects []Subject
	Fields   []authz.FieldID
	At       time.Time
}

type CallDecision struct {
	Skill        agentskills.SkillKey
	GrantID      string
	EvaluatedAt  time.Time
	Purpose      string
	Capabilities []CapabilityDecision
	Subjects     []SubjectDecision
}

// Authorize evaluates the current user, admin grant, consent and every
// operation in the pinned skill. It is intentionally effect-free.
func (g *Gate) Authorize(ctx context.Context, req CallRequest) (CallDecision, error) {
	if g == nil {
		return CallDecision{}, fmt.Errorf("%w: nil gate", ErrInvalid)
	}
	if err := validateCall(ctx, req); err != nil {
		return CallDecision{}, err
	}
	at, err := g.resolveTime(req.At)
	if err != nil {
		return CallDecision{}, err
	}
	if err := validateUser(req.User, req.Purpose); err != nil {
		return CallDecision{}, err
	}
	if err := validateSubjects(req.User, req.Subjects); err != nil {
		return CallDecision{}, err
	}
	record, err := g.skills.ResolvePin(req.Skill)
	if err != nil {
		return CallDecision{}, &DeniedError{Code: DenySkillNotFound, Skill: req.Skill.Key(), Detail: "skill version is not available"}
	}
	if !skillAllowsPurpose(record.Definition.RequiredPurposes, req.Purpose) {
		return CallDecision{}, &DeniedError{Code: DenyPurpose, Skill: req.Skill.Key(), Detail: "skill is not available for the requested purpose"}
	}
	grant, err := g.matchGrant(ctx, req.User, req.Skill.Key(), req.Purpose, at, req.Subjects)
	if err != nil {
		return CallDecision{}, err
	}
	return g.authorizeResolved(ctx, req.User, req.Actor, req.Skill.Key(), record, grant, req.Purpose, req.Subjects, req.Fields, at)
}

func (g *Gate) authorizeResolved(ctx context.Context, user UserContext, actor AgentActor, key agentskills.SkillKey, record agentskills.SkillRecord, grant SkillGrant, purpose string, subjects []Subject, fields []authz.FieldID, at time.Time) (CallDecision, error) {
	decision := CallDecision{Skill: key, GrantID: grant.ID, EvaluatedAt: at, Purpose: purpose}
	for _, operation := range record.ResolvedOperations {
		if !operation.HasCapability {
			return CallDecision{}, &DeniedError{Code: DenyConnectionOperation, Skill: key, Detail: "connection operations require their connection policy adapter"}
		}
		capDecision, err := g.pdp.Authorize(ctx, CapabilityRequest{Principal: user.Principal, Roles: currentRoles(user), Actor: actor, Skill: key, Capability: operation.Capability, Purpose: purpose, Subjects: cloneSubjects(subjects), Fields: slices.Clone(fields), EffectiveAt: at})
		if err != nil {
			return CallDecision{}, wrapDenied(err, DenyCapability, key, operation.Capability.Definition.Key())
		}
		if !capDecision.Allowed {
			return CallDecision{}, &DeniedError{Code: DenyCapability, Skill: key, Capability: operation.Capability.Definition.Key(), Detail: "capability policy denied the signed-in user"}
		}
		if err := validateCapabilityDecision(capDecision, subjects, fields, key, operation.Capability.Definition.Key()); err != nil {
			return CallDecision{}, err
		}
		decision.Capabilities = append(decision.Capabilities, projectCapabilityDecision(capDecision, subjects, fields))
	}
	decision.Subjects = mergeSubjectDecisions(decision.Capabilities, fields)
	return decision, nil
}

// Result is the only value shape this package filters. Skill implementations
// may carry arbitrary values, but the map key is always a governed field ID.
type Result struct {
	Subjects []ResultSubject
}

type ResultSubject struct {
	Subject values.EntityRef
	Fields  map[authz.FieldID]any
}

type Executor func(context.Context, CallDecision) (Result, error)

// Execute proves authorization before invoking executor and filters its
// result after execution. An executor never receives an unauthorized call.
func (g *Gate) Execute(ctx context.Context, req CallRequest, executor Executor) (Result, error) {
	if executor == nil {
		return Result{}, fmt.Errorf("%w: nil executor", ErrInvalid)
	}
	decision, err := g.Authorize(ctx, req)
	if err != nil {
		return Result{}, err
	}
	result, err := executor(ctx, decision)
	if err != nil {
		return Result{}, err
	}
	return FilterResult(decision, result)
}

// FilterResult removes unrequested/denied fields and replaces redacted values
// with a marker. It refuses an unexpected subject, preventing a skill from
// smuggling a second record into an otherwise authorized result.
func FilterResult(decision CallDecision, result Result) (Result, error) {
	allowed := make(map[values.EntityRef]map[authz.FieldID]authz.Effect, len(decision.Subjects))
	for _, subject := range decision.Subjects {
		allowed[subject.Subject] = subject.Fields
	}
	out := Result{Subjects: make([]ResultSubject, 0, len(result.Subjects))}
	for _, subject := range result.Subjects {
		fields, ok := allowed[subject.Subject]
		if !ok {
			return Result{}, &DeniedError{Code: DenySubject, Detail: "result contains an unauthorized subject"}
		}
		filtered := make(map[authz.FieldID]any)
		for field, value := range subject.Fields {
			effect, requested := fields[field]
			if !requested {
				continue
			}
			switch effect {
			case authz.EffectAllow:
				filtered[field] = value
			case authz.EffectRedacted:
				filtered[field] = RedactedValue{}
			}
		}
		out.Subjects = append(out.Subjects, ResultSubject{Subject: subject.Subject, Fields: filtered})
	}
	return out, nil
}

// RedactedValue is deliberately content-free. The caller may render a
// localized label, but it can never recover the withheld raw value.
type RedactedValue struct{}

func (g *Gate) authorizeRecord(ctx context.Context, user UserContext, actor AgentActor, key agentskills.SkillKey, record agentskills.SkillRecord, grant SkillGrant, purpose string, subjects []Subject, fields []authz.FieldID, at time.Time) error {
	for _, operation := range record.ResolvedOperations {
		if !operation.HasCapability {
			return &DeniedError{Code: DenyConnectionOperation, Skill: key, Detail: "connection operations require their connection policy adapter"}
		}
		decision, err := g.pdp.Authorize(ctx, CapabilityRequest{Principal: user.Principal, Roles: currentRoles(user), Actor: actor, Skill: key, Capability: operation.Capability, Purpose: purpose, Subjects: cloneSubjects(subjects), Fields: slices.Clone(fields), EffectiveAt: at})
		if err != nil || !decision.Allowed {
			return wrapDenied(err, DenyCapability, key, operation.Capability.Definition.Key())
		}
		if err := validateCapabilityDecision(decision, subjects, fields, key, operation.Capability.Definition.Key()); err != nil {
			return err
		}
	}
	return nil
}

func (g *Gate) matchGrant(ctx context.Context, user UserContext, key agentskills.SkillKey, purpose string, at time.Time, subjects []Subject) (SkillGrant, error) {
	grants, err := g.grants.Grants(ctx, user.Principal.Tenant(), key)
	if err != nil {
		return SkillGrant{}, &DeniedError{Code: DenySkillNotGranted, Skill: key, Detail: "current administrator grant could not be resolved"}
	}
	if len(grants) == 0 {
		return SkillGrant{}, &DeniedError{Code: DenySkillNotGranted, Skill: key, Detail: "no current administrator grant"}
	}
	sort.Slice(grants, func(i, j int) bool { return grants[i].ID < grants[j].ID })
	var mismatch DenialCode
	for _, grant := range grants {
		if err := validateGrant(grant); err != nil {
			continue
		}
		if grant.Tenant != user.Principal.Tenant() || grant.Skill != key {
			continue
		}
		if !matchesRoles(grant.Roles, currentRoles(user)) {
			mismatch = firstMismatch(mismatch, DenyRole)
			continue
		}
		if !matchesScope(grant.Population, user.Population) {
			mismatch = firstMismatch(mismatch, DenyPopulation)
			continue
		}
		if !matchesAny(grant.OrganizationScopes, user.OrganizationScopes) {
			mismatch = firstMismatch(mismatch, DenyOrganization)
			continue
		}
		if !matchesScopeSet(grant.Purposes, purpose) {
			mismatch = firstMismatch(mismatch, DenyPurpose)
			continue
		}
		if grant.ConsentRequired {
			if g.consent == nil {
				mismatch = firstMismatch(mismatch, DenyConsent)
				continue
			}
			if err := g.consent.Check(ctx, ConsentRequest{User: user, Skill: key, Purpose: purpose, Subjects: cloneSubjects(subjects), At: at}); err != nil {
				mismatch = firstMismatch(mismatch, DenyConsent)
				continue
			}
		}
		return grant, nil
	}
	if mismatch == "" {
		mismatch = DenySkillNotGranted
	}
	return SkillGrant{}, &DeniedError{Code: mismatch, Skill: key, Detail: "current administrator grant does not include the signed-in user"}
}

func validateUser(user UserContext, purpose string) error {
	if user.Principal == nil || strings.TrimSpace(user.Population) == "" || strings.TrimSpace(purpose) == "" {
		return &DeniedError{Code: DenyAgent, Detail: "signed-in user context is incomplete"}
	}
	if user.Principal.SubjectKind() != trust.SubjectKindHuman {
		return &DeniedError{Code: DenyAgent, Detail: "agent skills require a human signed-in user"}
	}
	if !user.Principal.AuthorizesPurpose(purpose) {
		return &DeniedError{Code: DenyPurpose, Detail: "signed-in user is not authorized for this purpose"}
	}
	if len(currentRoles(user)) == 0 || len(user.OrganizationScopes) == 0 {
		return &DeniedError{Code: DenyAgent, Detail: "current roles and organization scope are required"}
	}
	return nil
}

func validateSubjects(user UserContext, subjects []Subject) error {
	for _, subject := range subjects {
		if subject.Ref.Tenant != user.Principal.Tenant() {
			return &DeniedError{Code: DenySubject, Detail: "subject crosses the signed-in user's tenant"}
		}
		if subject.Organization.IsZero() || subject.Organization.Tenant != user.Principal.Tenant() || !matchesScopeSet(user.OrganizationScopes, subject.Organization.ID) {
			return &DeniedError{Code: DenyOrganization, Detail: "subject is outside the signed-in user's organization scope"}
		}
	}
	return nil
}

func validateCall(ctx context.Context, req CallRequest) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if req.Skill.ID == "" || req.Skill.Version == 0 || req.Skill.Digest == "" || len(req.Subjects) == 0 || len(req.Fields) == 0 {
		return &DeniedError{Code: DenyInvalid, Skill: req.Skill.Key(), Detail: "exact skill, subject and field bindings are required"}
	}
	if err := validateActor(req.Actor); err != nil {
		return err
	}
	seen := make(map[authz.FieldID]struct{}, len(req.Fields))
	for _, field := range req.Fields {
		if field == "" {
			return &DeniedError{Code: DenyInvalid, Skill: req.Skill.Key(), Detail: "field identifiers must be non-empty"}
		}
		if _, ok := seen[field]; ok {
			return &DeniedError{Code: DenyInvalid, Skill: req.Skill.Key(), Field: field, Detail: "field identifiers must be unique"}
		}
		seen[field] = struct{}{}
	}
	for _, subject := range req.Subjects {
		if err := subject.Ref.Validate(); err != nil {
			return &DeniedError{Code: DenyInvalid, Skill: req.Skill.Key(), Detail: "subject identifiers must be valid"}
		}
	}
	return nil
}

func validateActor(actor AgentActor) error {
	for _, value := range []string{actor.AgentVersion, actor.InstallationID, actor.RunID, actor.StepID} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
			return &DeniedError{Code: DenyAgent, Detail: "agent actor chain is incomplete"}
		}
	}
	return nil
}

func validateGrant(grant SkillGrant) error {
	if strings.TrimSpace(grant.ID) == "" || grant.Tenant.Validate() != nil || grant.Skill.ID == "" || grant.Skill.Version == 0 || len(grant.Roles) == 0 || strings.TrimSpace(grant.Population) == "" || len(grant.OrganizationScopes) == 0 || len(grant.Purposes) == 0 {
		return ErrInvalid
	}
	return nil
}

func currentRoles(user UserContext) []string {
	if user.Roles != nil {
		return slices.Clone(user.Roles)
	}
	return user.Principal.Roles()
}

func matchesRoles(grant, user []string) bool {
	for _, g := range grant {
		for _, u := range user {
			if g == u || g == AnyScope {
				return true
			}
		}
	}
	return false
}

func matchesAny(grant, user []string) bool {
	for _, g := range grant {
		for _, u := range user {
			if g == u || g == AnyScope {
				return true
			}
		}
	}
	return false
}

func matchesScope(grant, user string) bool { return grant == user || grant == AnyScope }

func matchesScopeSet(grant []string, user string) bool {
	for _, item := range grant {
		if matchesScope(item, user) {
			return true
		}
	}
	return false
}

func skillAllowsPurpose(required []string, purpose string) bool {
	return len(required) == 0 || slices.Contains(required, purpose)
}

func principalOrg(principal *trust.Principal) authz.OrgUnitRef {
	if principal == nil || principal.OrganizationScopeID() == "" {
		return authz.OrgUnitRef{}
	}
	return authz.OrgUnitRef{Tenant: principal.Tenant(), ID: principal.OrganizationScopeID()}
}

func definitionCoversField(def capability.Definition, field authz.FieldID) bool {
	for _, data := range []capability.DataDomainFieldSet{def.ReadData, def.WriteData} {
		if len(data.FieldPaths) > 0 {
			for _, path := range data.FieldPaths {
				if path == string(field) {
					return true
				}
			}
			continue
		}
		for _, domain := range data.DataDomains {
			if capabilityDomainCovers(domain, field) {
				return true
			}
		}
	}
	return false
}

func capabilityDomainCovers(domain string, field authz.FieldID) bool {
	fieldDomain := strings.SplitN(string(field), ".", 2)[0]
	if domain == fieldDomain {
		return true
	}
	aliases := map[string][]string{
		"worker":             {"worker", "core"},
		"assignment":         {"employment", "position"},
		"person":             {"contact"},
		"compensation":       {"compensation"},
		"tax":                {"tax"},
		"bank":               {"bank"},
		"performance":        {"performance"},
		"medical":            {"medical"},
		"employee_relations": {"employee_relations"},
		"immigration":        {"immigration"},
	}
	for _, alias := range aliases[fieldDomain] {
		if domain == alias {
			return true
		}
	}
	return false
}

func (g *Gate) resolveTime(at time.Time) (time.Time, error) {
	if at.IsZero() {
		at = g.now()
	}
	at = at.UTC()
	if at.IsZero() {
		return time.Time{}, fmt.Errorf("%w: evaluation time is required", ErrInvalid)
	}
	return at, nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func firstMismatch(current, next DenialCode) DenialCode {
	if current != "" {
		return current
	}
	return next
}

func wrapDenied(err error, code DenialCode, skill agentskills.SkillKey, cap capability.Key) error {
	var denied *DeniedError
	if errors.As(err, &denied) {
		copy := *denied
		if copy.Skill == (agentskills.SkillKey{}) {
			copy.Skill = skill
		}
		if copy.Capability == (capability.Key{}) {
			copy.Capability = cap
		}
		return &copy
	}
	if err != nil {
		return &DeniedError{Code: code, Skill: skill, Capability: cap, Detail: "policy decision point failed closed"}
	}
	return &DeniedError{Code: code, Skill: skill, Capability: cap, Detail: "policy decision point denied the capability"}
}

func validateCapabilityDecision(decision CapabilityDecision, subjects []Subject, fields []authz.FieldID, skill agentskills.SkillKey, cap capability.Key) error {
	bySubject := make(map[values.EntityRef]map[authz.FieldID]authz.Effect, len(decision.Subjects))
	for _, subject := range decision.Subjects {
		bySubject[subject.Subject] = subject.Fields
	}
	for _, subject := range subjects {
		fieldEffects, ok := bySubject[subject.Ref]
		if !ok {
			return &DeniedError{Code: DenySubject, Skill: skill, Capability: cap, Detail: "policy decision did not cover the exact subject"}
		}
		for _, field := range fields {
			effect, ok := fieldEffects[field]
			if !ok {
				return &DeniedError{Code: DenyField, Skill: skill, Capability: cap, Field: field, Detail: "policy decision did not cover the exact field"}
			}
			if effect != authz.EffectAllow && effect != authz.EffectRedacted {
				return &DeniedError{Code: DenyField, Skill: skill, Capability: cap, Field: field, Detail: "field policy denied disclosure"}
			}
		}
	}
	return nil
}

func mergeSubjectDecisions(decisions []CapabilityDecision, requested []authz.FieldID) []SubjectDecision {
	requestedSet := make(map[authz.FieldID]struct{}, len(requested))
	for _, field := range requested {
		requestedSet[field] = struct{}{}
	}
	bySubject := make(map[values.EntityRef]map[authz.FieldID]authz.Effect)
	for _, decision := range decisions {
		for _, subject := range decision.Subjects {
			fields := bySubject[subject.Subject]
			if fields == nil {
				fields = make(map[authz.FieldID]authz.Effect)
				bySubject[subject.Subject] = fields
			}
			for field, effect := range subject.Fields {
				if _, requested := requestedSet[field]; !requested {
					continue
				}
				if prior, ok := fields[field]; !ok || effectRank(effect) < effectRank(prior) {
					fields[field] = effect
				}
			}
		}
	}
	keys := make([]values.EntityRef, 0, len(bySubject))
	for subject := range bySubject {
		keys = append(keys, subject)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	out := make([]SubjectDecision, 0, len(keys))
	for _, subject := range keys {
		out = append(out, SubjectDecision{Subject: subject, Fields: bySubject[subject]})
	}
	return out
}

func effectRank(effect authz.Effect) int {
	switch effect {
	case authz.EffectAllow:
		return 4
	case authz.EffectRedacted:
		return 3
	case authz.EffectDenied:
		return 2
	case authz.EffectWithheld:
		return 1
	default:
		return 0
	}
}

func cloneGrant(grant SkillGrant) SkillGrant {
	grant.Roles = slices.Clone(grant.Roles)
	grant.OrganizationScopes = slices.Clone(grant.OrganizationScopes)
	grant.Purposes = slices.Clone(grant.Purposes)
	return grant
}

func cloneSubjects(subjects []Subject) []Subject {
	out := make([]Subject, len(subjects))
	for i, subject := range subjects {
		out[i] = subject
		subject.OrgEdges = slices.Clone(subject.OrgEdges)
		subject.Relationships = slices.Clone(subject.Relationships)
		subject.Sharing = slices.Clone(subject.Sharing)
		out[i] = subject
	}
	return out
}

func cloneCapabilityDecision(decision CapabilityDecision) CapabilityDecision {
	out := decision
	out.Subjects = make([]SubjectDecision, len(decision.Subjects))
	for i, subject := range decision.Subjects {
		out.Subjects[i] = SubjectDecision{Subject: subject.Subject, Fields: mapsClone(subject.Fields)}
	}
	return out
}

func projectCapabilityDecision(decision CapabilityDecision, subjects []Subject, fields []authz.FieldID) CapabilityDecision {
	projected := cloneCapabilityDecision(decision)
	wantedSubjects := make(map[values.EntityRef]struct{}, len(subjects))
	for _, subject := range subjects {
		wantedSubjects[subject.Ref] = struct{}{}
	}
	wantedFields := make(map[authz.FieldID]struct{}, len(fields))
	for _, field := range fields {
		wantedFields[field] = struct{}{}
	}
	projected.Subjects = projected.Subjects[:0]
	for _, subject := range decision.Subjects {
		if _, ok := wantedSubjects[subject.Subject]; !ok {
			continue
		}
		filtered := make(map[authz.FieldID]authz.Effect)
		for field, effect := range subject.Fields {
			if _, ok := wantedFields[field]; ok {
				filtered[field] = effect
			}
		}
		projected.Subjects = append(projected.Subjects, SubjectDecision{Subject: subject.Subject, Fields: filtered})
	}
	return projected
}

func mapsClone(in map[authz.FieldID]authz.Effect) map[authz.FieldID]authz.Effect {
	if in == nil {
		return nil
	}
	out := make(map[authz.FieldID]authz.Effect, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
