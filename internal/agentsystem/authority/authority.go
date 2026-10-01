// Package authority binds agent runs to current, revocable authority inputs.
// It resolves policy facts on every admission, tool, and delivery check; a
// Snapshot is explanatory and must never be cached as a permission.
package authority

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrDenied identifies a current policy or principal-chain denial.
	ErrDenied = errors.New("agent authority denied")
	// ErrInvalid identifies an incomplete or unsupported authority request.
	ErrInvalid = errors.New("invalid agent authority request")
)

type RunMode string

const (
	// ModeOnBehalfOf uses the invoker's current delegated authority.
	ModeOnBehalfOf RunMode = "ON_BEHALF_OF"
	// ModeSponsored uses only the current nonhuman sponsor authority.
	ModeSponsored RunMode = "SPONSORED"
)

type Tier uint8

const (
	// TierRead is the T0 read-only effect tier.
	TierRead Tier = iota
	// TierPrivateDraft is the T1 private-draft effect tier.
	TierPrivateDraft
	// TierCommunicate is the T2 communication effect tier.
	TierCommunicate
	// TierSubmitGoverned is the T3 governed-submission effect tier.
	TierSubmitGoverned
	// TierExternalWrite is the T4 external-write effect tier.
	TierExternalWrite
)

// RunContext is the server-resolved identity chain for one run. Fields must
// come from authenticated invocation/admission records, never model output.
type RunContext struct {
	RunID             string
	TenantID          values.TenantId
	EntityID          string
	AgentID           string
	AgentPrincipalID  string
	AgentVersion      string
	InstallationID    string
	Purpose           string
	Audience          string
	Mode              RunMode
	InvokerID         string
	SponsorID         string
	DelegationGrantID string
}

// Scope is one current policy ceiling. Empty dimensions deny that dimension.
type Scope struct {
	GrantRef     string
	TenantID     values.TenantId
	EntityIDs    []string
	Purposes     []string
	Audiences    []string
	Skills       []string
	Capabilities []string
	Sources      []string
}

// DelegationGrant is the current AGENT2-003 durable grant projection. The
// resolver must load this from the authoritative grant store on every check.
type DelegationGrant struct {
	GrantID        string
	SubjectID      string
	TenantID       values.TenantId
	AgentVersion   string
	InstallationID string
	RunID          string
	Purpose        string
	Active         bool
	Scope          Scope
}

// CurrentGrants contains fresh policy/store results, never caller claims.
type CurrentGrants struct {
	// ResolvedMode must be derived from the authenticated trigger, not RunContext.Mode.
	ResolvedMode       RunMode
	AgentPrincipalID   string
	AgentActive        bool
	InstallationActive bool
	InvokerActive      bool
	SponsorPrincipalID string
	SponsorActive      bool
	Agent              Scope
	Installation       Scope
	Context            Scope
	Invoker            Scope
	Sponsor            Scope
	Delegation         DelegationGrant
}

// GrantResolver loads current principal, sponsor, installation, context and
// user authority. Implementations must not serve cached authority decisions.
type GrantResolver interface {
	ResolveCurrent(context.Context, RunContext) (CurrentGrants, error)
}

// CredentialRequest binds a delegated token to one exact boundary and run.
type CredentialRequest struct {
	TenantID       values.TenantId
	GrantID        string
	SubjectID      string
	AgentVersion   string
	InstallationID string
	RunID          string
	Purpose        string
	SkillID        string
	Audience       string
	Sender         string
	Scope          []string
}

// DelegatedCredentialVerifier must verify signature, audience, sender, time,
// user activity, grant revocation and current user authority through AGENT2-003.
type DelegatedCredentialVerifier interface {
	VerifyDelegated(context.Context, agentdelegation.DelegatedCredential, CredentialRequest) error
}

// Verifier makes a fresh current-state decision at each boundary.
type Verifier struct {
	grants    GrantResolver
	delegated DelegatedCredentialVerifier
}

// NewVerifier requires both current grants and AGENT2-003 token verification.
func NewVerifier(grants GrantResolver, delegated DelegatedCredentialVerifier) (*Verifier, error) {
	if grants == nil || delegated == nil {
		return nil, fmt.Errorf("%w: current grant resolver and delegated verifier are required", ErrInvalid)
	}
	return &Verifier{grants: grants, delegated: delegated}, nil
}

// Snapshot contains a fresh intersection for admission explanation. Its sets
// are not credentials and must be re-resolved by VerifyTool/VerifyDelivery.
type Snapshot struct {
	Mode              RunMode
	AgentID           string
	AgentPrincipalID  string
	AgentVersion      string
	InstallationID    string
	DelegationGrantID string
	PolicyRefs        []string
	InvokerID         string
	SponsorID         string
	TenantID          values.TenantId
	EntityID          string
	Purpose           string
	Audience          string
	Skills            []string
	Capabilities      []string
	Sources           []string
}

// VerifyAdmission resolves and validates the run's principal chain before
// model work. ON_BEHALF_OF requires an active bound AGENT2-003 grant; it never
// treats the agent principal as authority. SPONSORED requires the sponsor.
func (v *Verifier) VerifyAdmission(ctx context.Context, run RunContext) (Snapshot, error) {
	if v == nil || v.grants == nil || v.delegated == nil {
		return Snapshot{}, fmt.Errorf("%w: verifier is not configured", ErrInvalid)
	}
	if err := validateRun(run); err != nil {
		return Snapshot{}, err
	}
	current, err := v.grants.ResolveCurrent(ctx, run)
	if err != nil {
		return Snapshot{}, err
	}
	if current.ResolvedMode != run.Mode {
		return Snapshot{}, fmt.Errorf("%w: run mode differs from the authenticated trigger", ErrDenied)
	}
	if !current.AgentActive || !current.InstallationActive || current.AgentPrincipalID != run.AgentPrincipalID {
		return Snapshot{}, fmt.Errorf("%w: agent principal or installation is inactive or mismatched", ErrDenied)
	}
	layers := []Scope{current.Agent, current.Installation, current.Context}
	snapshot := Snapshot{Mode: run.Mode, AgentID: run.AgentID, AgentPrincipalID: current.AgentPrincipalID, AgentVersion: run.AgentVersion, InstallationID: run.InstallationID, DelegationGrantID: run.DelegationGrantID, TenantID: run.TenantID, EntityID: run.EntityID, Purpose: run.Purpose, Audience: run.Audience}
	switch run.Mode {
	case ModeOnBehalfOf:
		if run.InvokerID == "" || run.SponsorID != "" || run.DelegationGrantID == "" || !current.InvokerActive {
			return Snapshot{}, fmt.Errorf("%w: on-behalf-of run requires an active invoker and delegated grant, without a sponsor", ErrDenied)
		}
		if err := validateDelegation(run, current.Delegation); err != nil {
			return Snapshot{}, err
		}
		layers = append(layers, current.Invoker, current.Delegation.Scope)
		snapshot.InvokerID = run.InvokerID
	case ModeSponsored:
		if run.SponsorID == "" || run.InvokerID != "" || run.DelegationGrantID != "" || !current.SponsorActive || current.SponsorPrincipalID != run.SponsorID {
			return Snapshot{}, fmt.Errorf("%w: sponsored run requires its active nonhuman sponsor and no user delegation", ErrDenied)
		}
		layers = append(layers, current.Sponsor)
		snapshot.SponsorID = run.SponsorID
	default:
		return Snapshot{}, fmt.Errorf("%w: unsupported run mode", ErrInvalid)
	}
	if err := validateScopeBindings(run, layers); err != nil {
		return Snapshot{}, err
	}
	for _, layer := range layers {
		if layer.GrantRef == "" {
			return Snapshot{}, fmt.Errorf("%w: current authority layer has no auditable grant reference", ErrDenied)
		}
		snapshot.PolicyRefs = append(snapshot.PolicyRefs, layer.GrantRef)
	}
	snapshot.Skills = intersectDimension(layers, func(s Scope) []string { return s.Skills })
	snapshot.Capabilities = intersectDimension(layers, func(s Scope) []string { return s.Capabilities })
	snapshot.Sources = intersectDimension(layers, func(s Scope) []string { return s.Sources })
	return snapshot, nil
}

// ToolRequest names the exact skill, capabilities, sources and effect tier.
type ToolRequest struct {
	SkillID      string
	Capabilities []string
	SourceIDs    []string
	Tier         Tier
	Audience     string
	Sender       string
	Credential   agentdelegation.DelegatedCredential
}

// Decision returns the newly resolved effective authority for a boundary.
type Decision struct{ Snapshot Snapshot }

// VerifyTool re-resolves current grants and verifies the step credential for
// every tool call. A revoked source or delegation cannot survive a cached grant.
func (v *Verifier) VerifyTool(ctx context.Context, run RunContext, req ToolRequest) (Decision, error) {
	snapshot, err := v.VerifyAdmission(ctx, run)
	if err != nil {
		return Decision{}, err
	}
	if req.SkillID == "" || len(req.Capabilities) == 0 || req.Tier > TierExternalWrite || req.Audience != run.Audience || !contains(snapshot.Skills, req.SkillID) || !containsAll(snapshot.Capabilities, req.Capabilities) || !containsAll(snapshot.Sources, req.SourceIDs) {
		return Decision{}, fmt.Errorf("%w: tool exceeds current skill, capability, source or audience grant", ErrDenied)
	}
	if run.Mode == ModeSponsored && req.Tier > TierCommunicate {
		return Decision{}, fmt.Errorf("%w: sponsored runs are capped at T2", ErrDenied)
	}
	if run.Mode == ModeOnBehalfOf {
		if req.Credential.Raw == "" {
			return Decision{}, fmt.Errorf("%w: delegated credential is required", ErrDenied)
		}
		if err := v.delegated.VerifyDelegated(ctx, req.Credential, credentialRequest(run, req.SkillID, req.Audience, req.Sender, req.Capabilities)); err != nil {
			return Decision{}, fmt.Errorf("%w: delegated credential: %w", ErrDenied, err)
		}
	} else if req.Credential.Raw != "" {
		return Decision{}, fmt.Errorf("%w: sponsored runs cannot carry a user delegated credential", ErrDenied)
	}
	return Decision{Snapshot: snapshot}, nil
}

// DeliveryRequest binds returned material to its current audience and sources.
type DeliveryRequest struct {
	SkillID      string
	Capabilities []string
	SourceIDs    []string
	Tier         Tier
	Audience     string
	Sender       string
	Credential   agentdelegation.DelegatedCredential
}

// VerifyDelivery re-resolves grants at the delivery boundary, including source
// revocation and the exact destination audience.
func (v *Verifier) VerifyDelivery(ctx context.Context, run RunContext, req DeliveryRequest) (Decision, error) {
	snapshot, err := v.VerifyAdmission(ctx, run)
	if err != nil {
		return Decision{}, err
	}
	if req.Audience == "" || req.Audience != run.Audience || req.Tier > TierExternalWrite || !contains(snapshot.Skills, req.SkillID) || len(req.Capabilities) == 0 || !containsAll(snapshot.Capabilities, req.Capabilities) || !containsAll(snapshot.Sources, req.SourceIDs) {
		return Decision{}, fmt.Errorf("%w: delivery exceeds current audience, skill or source grant", ErrDenied)
	}
	if run.Mode == ModeSponsored && req.Tier > TierCommunicate {
		return Decision{}, fmt.Errorf("%w: sponsored runs are capped at T2", ErrDenied)
	}
	if run.Mode == ModeOnBehalfOf {
		if req.Credential.Raw == "" {
			return Decision{}, fmt.Errorf("%w: delegated credential is required", ErrDenied)
		}
		if err := v.delegated.VerifyDelegated(ctx, req.Credential, credentialRequest(run, req.SkillID, req.Audience, req.Sender, req.Capabilities)); err != nil {
			return Decision{}, fmt.Errorf("%w: delegated credential: %w", ErrDenied, err)
		}
	} else if req.Credential.Raw != "" {
		return Decision{}, fmt.Errorf("%w: sponsored runs cannot carry a user delegated credential", ErrDenied)
	}
	return Decision{Snapshot: snapshot}, nil
}

func validateRun(run RunContext) error {
	if strings.TrimSpace(run.RunID) == "" || run.TenantID.Validate() != nil || strings.TrimSpace(run.EntityID) == "" || strings.TrimSpace(run.AgentID) == "" || strings.TrimSpace(run.AgentPrincipalID) == "" || strings.TrimSpace(run.AgentVersion) == "" || strings.TrimSpace(run.InstallationID) == "" || strings.TrimSpace(run.Purpose) == "" || strings.TrimSpace(run.Audience) == "" {
		return fmt.Errorf("%w: run identity, tenant, entity, purpose and audience are required", ErrInvalid)
	}
	if run.Mode != ModeOnBehalfOf && run.Mode != ModeSponsored {
		return fmt.Errorf("%w: unsupported run mode", ErrInvalid)
	}
	return nil
}

func validateDelegation(run RunContext, g DelegationGrant) error {
	if !g.Active || g.GrantID != run.DelegationGrantID || g.SubjectID != run.InvokerID || g.TenantID != run.TenantID || g.AgentVersion != run.AgentVersion || g.InstallationID != run.InstallationID || g.RunID != run.RunID || g.Purpose != run.Purpose {
		return fmt.Errorf("%w: current delegated grant is missing, revoked or bound to another run", ErrDenied)
	}
	return nil
}

func validateScopeBindings(run RunContext, layers []Scope) error {
	for _, scope := range layers {
		if scope.TenantID != run.TenantID || !contains(scope.EntityIDs, run.EntityID) || !contains(scope.Purposes, run.Purpose) || !contains(scope.Audiences, run.Audience) {
			return fmt.Errorf("%w: current scope does not cover tenant, entity, purpose and audience", ErrDenied)
		}
	}
	return nil
}

func credentialRequest(run RunContext, skill, audience, sender string, scope []string) CredentialRequest {
	return CredentialRequest{TenantID: run.TenantID, GrantID: run.DelegationGrantID, SubjectID: run.InvokerID, AgentVersion: run.AgentVersion, InstallationID: run.InstallationID, RunID: run.RunID, Purpose: run.Purpose, SkillID: skill, Audience: audience, Sender: sender, Scope: slices.Clone(scope)}
}

func intersectDimension(layers []Scope, selectValues func(Scope) []string) []string {
	if len(layers) == 0 {
		return nil
	}
	result := slices.Clone(selectValues(layers[0]))
	for _, layer := range layers[1:] {
		allowed := selectValues(layer)
		filtered := result[:0]
		for _, value := range result {
			if contains(allowed, value) {
				filtered = append(filtered, value)
			}
		}
		result = filtered
	}
	slices.Sort(result)
	return result
}

func containsAll(allowed, requested []string) bool {
	for _, value := range requested {
		if !contains(allowed, value) {
			return false
		}
	}
	return true
}

func contains(values []string, value string) bool { return slices.Contains(values, value) }
