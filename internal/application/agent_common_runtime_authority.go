package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CommonAgentSourceAuthority verifies the durable source occurrence against its
// owner. A schedule, event or workflow reference is never authority by itself.
type CommonAgentSourceAuthority interface {
	CheckRequest(context.Context, agentrun.Request) error
}

// CommonAgentModelPolicyAuthority resolves the exact manifest policy pin and
// current provider eligibility from the model-policy owner before acceptance.
type CommonAgentModelPolicyAuthority interface {
	CheckCurrentModelPolicy(context.Context, agentrun.Request, agentmanifest.Reference) error
}

// CommonAgentBindingScope is the exact target and ceiling recorded by the
// trust authority in authority_binding.scope. Both the service principal and
// its sponsor must have a current independently issued binding. These facts
// are loaded from the core trust database, never from the invocation payload.
type CommonAgentBindingScope struct {
	SchemaVersion  uint32                 `json:"schema_version"`
	TenantID       string                 `json:"tenant_id"`
	Agent          agentrun.VersionRef    `json:"agent"`
	InstallationID string                 `json:"installation_id"`
	AgentPrincipal string                 `json:"agent_principal_id"`
	SponsorID      string                 `json:"sponsor_id"`
	InvokerID      string                 `json:"invoker_id,omitempty"`
	DelegationRef  string                 `json:"delegated_credential_ref,omitempty"`
	UserAuthority  *trust.AuthorityScope  `json:"user_authority,omitempty"`
	LegalEntity    string                 `json:"legal_entity_id"`
	Purpose        string                 `json:"purpose"`
	Audience       agentrun.AudienceScope `json:"audience"`
	Context        agentrun.ContextScope  `json:"context_scope"`
	Sources        []agentrun.SourceKind  `json:"source_kinds"`
	BudgetCeiling  agentrun.Budget        `json:"budget_ceiling"`
	Deadline       time.Time              `json:"deadline"`
}

// CommonAgentAuthorityConfig composes actual manifest and trust readers with
// the authoritative source owners. Unconfigured sources are refused.
type CommonAgentAuthorityConfig struct {
	CoreDB     dbport.Beginner
	Agents     *agentstore.Store
	TenantUUID func(values.TenantId) uuid.UUID
	Sources    map[agentrun.SourceKind]CommonAgentSourceAuthority
	Models     CommonAgentModelPolicyAuthority
	Now        func() time.Time
}

// CommonAgentAuthority authorizes sponsored background work using current
// trust rows and the exact immutable installed manifest.
type CommonAgentAuthority struct{ cfg CommonAgentAuthorityConfig }

func NewCommonAgentAuthority(cfg CommonAgentAuthorityConfig) (*CommonAgentAuthority, error) {
	if cfg.CoreDB == nil || cfg.Agents == nil || cfg.TenantUUID == nil || cfg.Now == nil || cfg.Models == nil || len(cfg.Sources) == 0 {
		return nil, agentrun.ErrAuthorityMissing
	}
	sources := make(map[agentrun.SourceKind]CommonAgentSourceAuthority, len(cfg.Sources))
	for kind, owner := range cfg.Sources {
		if owner == nil {
			return nil, agentrun.ErrAuthorityMissing
		}
		sources[kind] = owner
	}
	cfg.Sources = sources
	return &CommonAgentAuthority{cfg: cfg}, nil
}

func commonAgentRefusal(code string) error { return &agentrun.AdmissionRefusal{Code: code} }

// VerifyAdmission reads both principals and their authority sources on every
// call. An inactive principal, expired binding, changed source or broadened
// request cannot survive a durable accepted admission.
func (a *CommonAgentAuthority) VerifyAdmission(ctx context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if a == nil || ctx == nil {
		return agentrun.AuthoritySnapshot{}, agentrun.ErrAuthorityMissing
	}
	owner := a.cfg.Sources[request.Source.Kind]
	if owner == nil {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("SOURCE_OR_MODE_DENIED")
	}
	if err := owner.CheckRequest(ctx, request); err != nil {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("source owner: %w", err)
	}
	return a.VerifyBindings(ctx, request)
}

// VerifyBindings checks independently issued trust and manifest ceilings.
// Source owners call this during publication and firing checks; it deliberately
// does not call back into the source owner, avoiding recursive authorization.
func (a *CommonAgentAuthority) VerifyBindings(ctx context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if a == nil || ctx == nil {
		return agentrun.AuthoritySnapshot{}, agentrun.ErrAuthorityMissing
	}
	now := a.cfg.Now().UTC()
	owner := a.cfg.Sources[request.Source.Kind]
	if owner == nil || request.Persona != nil || !commonAgentPrincipalMode(request.Principal) || now.IsZero() || !request.Deadline.After(now) {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("SOURCE_OR_MODE_DENIED")
	}
	tenant := values.TenantId(request.Source.TenantID)
	tenantID := a.cfg.TenantUUID(tenant)
	if tenant.Validate() != nil || tenantID == uuid.Nil {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("TENANT_DENIED")
	}
	version, err := strconv.ParseUint(request.Agent.Version, 10, 64)
	if err != nil || version == 0 {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("VERSION_DENIED")
	}
	manifest, err := a.cfg.Agents.ManifestVersion(ctx, tenantID, request.Agent.AgentID, version)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("load immutable agent manifest: %w", err)
	}
	digest, err := manifest.Digest()
	manifestBudget := agentrun.Budget{MaxCostMicros: manifest.Budget.MaxCostMicros, MaxInputTokens: manifest.Budget.MaxInputTokens, MaxOutputTokens: manifest.Budget.MaxOutputTokens}
	if err != nil || digest != request.Agent.Digest || manifest.Purpose != request.Purpose || !personaRunBudgetWithin(request.Budget, manifestBudget) {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("MANIFEST_DENIED")
	}
	if err := a.cfg.Models.CheckCurrentModelPolicy(ctx, request, manifest.ModelPolicy); err != nil {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("current model policy: %w", err)
	}
	principal, err := uuid.Parse(request.Principal.AgentPrincipalID)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("PRINCIPAL_DENIED")
	}
	tx, err := a.cfg.CoreDB.Begin(ctx)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT enabled FROM tenant_agent_setting WHERE tenant_id=$1`, tenantID).Scan(&enabled); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return agentrun.AuthoritySnapshot{}, commonAgentRefusal("TENANT_AGENTS_DISABLED")
		}
		return agentrun.AuthoritySnapshot{}, err
	}
	if !enabled {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("TENANT_AGENTS_DISABLED")
	}
	if request.Principal.Mode == agentrun.ModeOnBehalfOf {
		return a.verifyOnBehalfOf(ctx, tx, tenantID, principal, request, manifestBudget, now)
	}
	sponsor, err := uuid.Parse(request.Principal.SponsorID)
	if err != nil || sponsor == principal {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("SPONSOR_DENIED")
	}
	agentBinding, agentScope, err := readCommonAgentBinding(ctx, tx, tenantID, principal, request, now)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	sponsorBinding, sponsorScope, err := readCommonAgentBinding(ctx, tx, tenantID, sponsor, request, now)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, err
	}
	ceiling := agentScope.BudgetCeiling
	ceiling.MaxCostMicros = commonAgentMinimum(ceiling.MaxCostMicros, sponsorScope.BudgetCeiling.MaxCostMicros)
	ceiling.MaxInputTokens = commonAgentMinimum(ceiling.MaxInputTokens, sponsorScope.BudgetCeiling.MaxInputTokens)
	ceiling.MaxOutputTokens = commonAgentMinimum(ceiling.MaxOutputTokens, sponsorScope.BudgetCeiling.MaxOutputTokens)
	ceiling.MaxCostMicros = commonAgentMinimum(ceiling.MaxCostMicros, manifestBudget.MaxCostMicros)
	ceiling.MaxInputTokens = commonAgentMinimum(ceiling.MaxInputTokens, manifestBudget.MaxInputTokens)
	ceiling.MaxOutputTokens = commonAgentMinimum(ceiling.MaxOutputTokens, manifestBudget.MaxOutputTokens)
	proof, _ := json.Marshal([]any{agentBinding, agentScope, sponsorBinding, sponsorScope})
	sum := sha256.Sum256(proof)
	return agentrun.AuthoritySnapshot{Agent: agentScope.Agent, InstallationID: agentScope.InstallationID,
		Principal: agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: agentScope.AgentPrincipal, SponsorID: sponsorScope.SponsorID},
		Audience:  agentScope.Audience, Context: agentScope.Context, BudgetCeiling: ceiling,
		GrantRef: agentBinding + ":" + sponsorBinding, PolicyDigest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

// CheckCurrent is the source-owner publication/dispatch ceiling check.
func (a *CommonAgentAuthority) CheckCurrent(ctx context.Context, request agentrun.Request) error {
	_, err := a.VerifyBindings(ctx, request)
	return err
}

func readCommonAgentBinding(ctx context.Context, tx dbport.Tx, tenant, principal uuid.UUID, request agentrun.Request, now time.Time) (string, CommonAgentBindingScope, error) {
	rows, err := tx.Query(ctx, `SELECT b.binding_id::text,b.scope,p.revocation_epoch,s.version,s.content_digest
		FROM principal p JOIN authority_binding b ON b.tenant_id=p.tenant_id AND b.principal_id=p.principal_id
		JOIN authority_source s ON s.tenant_id=b.tenant_id AND s.authority_source_id=b.authority_source_id
		WHERE p.tenant_id=$1 AND p.principal_id=$2 AND p.kind IN ('SERVICE','SYSTEM') AND p.lifecycle='ACTIVE'
		AND (p.expires_at IS NULL OR p.expires_at>$3) AND b.valid_from<=$3 AND (b.valid_to IS NULL OR b.valid_to>$3)
		AND s.kind='POLICY_BUNDLE' AND s.valid_interval @> $3::timestamptz`, tenant, principal, now)
	if err != nil {
		return "", CommonAgentBindingScope{}, err
	}
	defer rows.Close()
	var found string
	var selected CommonAgentBindingScope
	for rows.Next() {
		var id, sourceDigest string
		var raw []byte
		var epoch, sourceVersion int64
		if err := rows.Scan(&id, &raw, &epoch, &sourceVersion, &sourceDigest); err != nil {
			return "", CommonAgentBindingScope{}, err
		}
		var scope CommonAgentBindingScope
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&scope) != nil || decoder.Decode(new(any)) != io.EOF || !commonAgentBindingMatches(scope, request, now) {
			continue
		}
		if found != "" || epoch < 1 || sourceVersion < 1 || !personaRunAuthorityDigest("sha256:"+sourceDigest) {
			return "", CommonAgentBindingScope{}, commonAgentRefusal("AMBIGUOUS_AUTHORITY")
		}
		found = fmt.Sprintf("%s@%d:%d:%s", id, epoch, sourceVersion, sourceDigest)
		selected = scope
	}
	if err := rows.Err(); err != nil {
		return "", CommonAgentBindingScope{}, err
	}
	if found == "" {
		return "", CommonAgentBindingScope{}, commonAgentRefusal("CURRENT_BINDING_DENIED")
	}
	return found, selected, nil
}

func commonAgentBindingMatches(scope CommonAgentBindingScope, request agentrun.Request, now time.Time) bool {
	return scope.SchemaVersion == 1 && scope.TenantID == request.Source.TenantID && scope.Agent == request.Agent &&
		scope.InstallationID == request.InstallationID && scope.AgentPrincipal == request.Principal.AgentPrincipalID && scope.SponsorID == request.Principal.SponsorID &&
		scope.InvokerID == request.Principal.InvokerID && scope.DelegationRef == request.Principal.DelegatedCredentialRef &&
		scope.LegalEntity == request.LegalEntity && scope.Purpose == request.Purpose && scope.Audience == request.Audience && scope.Context == request.Context &&
		slices.Contains(scope.Sources, request.Source.Kind) && scope.Deadline.After(now) && !request.Deadline.After(scope.Deadline) && personaRunBudgetWithin(request.Budget, scope.BudgetCeiling)
}

func commonAgentPrincipalMode(principal agentrun.PrincipalChain) bool {
	switch principal.Mode {
	case agentrun.ModeSponsored:
		return principal.InvokerID == "" && principal.DelegatedCredentialRef == "" && principal.SponsorID != ""
	case agentrun.ModeOnBehalfOf:
		return principal.SponsorID == "" && runAuthorityCleanRequired(principal.InvokerID, 256) && runAuthorityCleanRequired(principal.DelegatedCredentialRef, 512)
	default:
		return false
	}
}

func commonAgentMinimum(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
