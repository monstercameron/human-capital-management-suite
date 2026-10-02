package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrPersonaDraftDenied  = errors.New("application: persona draft creation denied")
	ErrPersonaDraftInvalid = errors.New("application: invalid persona draft")
)

// PersonaDraftStore atomically persists a persona version, its explicit owners,
// and the initial DRAFT lifecycle event for one tenant.
type PersonaDraftStore interface {
	CreateDraft(context.Context, agentpersonastore.PersonaVersion, agentpersonastore.PersonaOwner, agentpersonastore.PersonaOwner, string, time.Time) error
}

// PersonaCreateAuthorization identifies the trusted actor and tenant for the
// fixed persona-admin create operation.
type PersonaCreateAuthorization struct {
	Principal *trust.Principal
	Tenant    values.TenantId
	PersonaID string
}

// PersonaCreateAuthorizer checks the server-owned persona-admin create grant.
type PersonaCreateAuthorizer interface {
	AuthorizePersonaCreate(context.Context, PersonaCreateAuthorization) error
}

// PersonaProfileBuilder revalidates a sealed persona against the current
// server-owned skill, grant, and manifest registries.
type PersonaProfileBuilder interface {
	Build(agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error)
}

// ContextPersonaProfileBuilder validates with a validator resolved for the
// authenticated tenant. Production composition uses this to avoid pinning a
// manifest resolver to one tenant.
type ContextPersonaProfileBuilder interface {
	PersonaProfileBuilder
	BuildForTenant(context.Context, values.TenantId, agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error)
}

// PersonaProfileBuilderSource creates a validator whose manifest and
// capability dependencies are scoped to exactly one verified tenant.
type PersonaProfileBuilderSource interface {
	ForTenant(context.Context, values.TenantId) (PersonaProfileBuilder, error)
}

// PersonaDraftClock supplies trusted audit time.
type PersonaDraftClock interface {
	Now() time.Time
}

// PersonaAdminDraftService creates tenant-scoped persona drafts.
type PersonaAdminDraftService struct {
	Store      PersonaDraftStore
	Authorizer PersonaCreateAuthorizer
	Profiles   PersonaProfileBuilder
	Clock      PersonaDraftClock
}

// PersonaDraftRequest contains a validated immutable profile and explicit
// owner assignments. It deliberately has no tenant or actor fields.
type PersonaDraftRequest struct {
	Version            agentpersona.PersonaVersion
	BusinessOwnerID    string
	TechnicalStewardID string
}

// PersonaDraft is the non-sensitive receipt returned after durable creation.
type PersonaDraft struct {
	PersonaID string
	Version   uint32
	Digest    string
	Lifecycle string
}

// CreateDraft creates a persona in DRAFT after revalidating its profile and
// authorizing the authenticated principal for the principal's own tenant.
func (s *PersonaAdminDraftService) CreateDraft(ctx context.Context, req PersonaDraftRequest) (PersonaDraft, error) {
	if s == nil || ctx == nil || s.Store == nil || s.Authorizer == nil || s.Profiles == nil || s.Clock == nil {
		return PersonaDraft{}, ErrPersonaDraftDenied
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().Validate() != nil || strings.TrimSpace(principal.Subject()) == "" {
		return PersonaDraft{}, ErrPersonaDraftDenied
	}
	now := s.Clock.Now().UTC()
	if now.IsZero() || now.Before(principal.IssuedAt()) || !now.Before(principal.ExpiresAt()) {
		return PersonaDraft{}, ErrPersonaDraftDenied
	}
	profile := req.Version.Profile
	if req.Version.Verify() != nil || strings.TrimSpace(profile.PersonaID) == "" || profile.Version == 0 ||
		strings.TrimSpace(req.BusinessOwnerID) == "" || strings.TrimSpace(req.TechnicalStewardID) == "" ||
		req.BusinessOwnerID != profile.Owner || req.TechnicalStewardID != profile.Steward || req.BusinessOwnerID == req.TechnicalStewardID {
		return PersonaDraft{}, ErrPersonaDraftInvalid
	}
	if err := validatePersonaInstructionDocumentTokens(profile); err != nil {
		return PersonaDraft{}, err
	}
	validated, err := buildPersonaProfile(ctx, s.Profiles, principal.Tenant(), profile)
	if err != nil || validated.Digest != req.Version.Digest {
		return PersonaDraft{}, fmt.Errorf("%w: profile validation failed", ErrPersonaDraftInvalid)
	}
	if err := s.Authorizer.AuthorizePersonaCreate(ctx, PersonaCreateAuthorization{Principal: principal, Tenant: principal.Tenant(), PersonaID: profile.PersonaID}); err != nil {
		return PersonaDraft{}, fmt.Errorf("%w: %v", ErrPersonaDraftDenied, err)
	}
	profileJSON, err := json.Marshal(validated.Profile)
	if err != nil {
		return PersonaDraft{}, fmt.Errorf("%w: encode validated profile: %v", ErrPersonaDraftInvalid, err)
	}
	tenant := principal.Tenant()
	version := agentpersonastore.PersonaVersion{
		TenantID: tenant, PersonaID: profile.PersonaID, Version: int64(profile.Version),
		AgentVersion: fmt.Sprintf("%s@%d", profile.Manifest.ID, profile.Manifest.Version),
		Handle:       profile.Handle, DisplayName: profile.DisplayName, Profile: profileJSON,
		ContentDigest: validated.Digest, CreatedAt: now,
	}
	owner := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: profile.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: req.BusinessOwnerID}
	steward := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: profile.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: req.TechnicalStewardID}
	if err := createPersonaDraftIcon(ctx, s.Store, version, owner, steward, principal.Subject(), now); err != nil {
		return PersonaDraft{}, err
	}
	return PersonaDraft{PersonaID: profile.PersonaID, Version: profile.Version, Digest: validated.Digest, Lifecycle: string(agentpersonastore.StateDraft)}, nil
}
