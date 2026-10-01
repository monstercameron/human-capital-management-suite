package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaAdminStarterSource = errors.New("application: persona admin starter catalog unavailable")

// PersonaAdminStarterSource projects only platform starters whose current
// manifest, instructions, active skill pins, and tenant grants are verified.
type PersonaAdminStarterSource struct {
	Skills       agentpersona.SkillResolver
	Manifests    PersonaStarterManifestResolverSource
	Instructions PersonaStarterInstructionsResolverSource
	Grants       PersonaProfileGrantSource
	Authorizer   PersonaCatalogAuthorizer
}

// NewPersonaAdminStarterSource creates the server-backed starter projection.
func NewPersonaAdminStarterSource(skills agentpersona.SkillResolver, manifests PersonaStarterManifestResolverSource, instructions PersonaStarterInstructionsResolverSource, grants PersonaProfileGrantSource, authorizer PersonaCatalogAuthorizer) (*PersonaAdminStarterSource, error) {
	if skills == nil || manifests == nil || instructions == nil || grants == nil || authorizer == nil {
		return nil, errPersonaAdminStarterSource
	}
	return &PersonaAdminStarterSource{Skills: skills, Manifests: manifests, Instructions: instructions, Grants: grants, Authorizer: authorizer}, nil
}

// PersonaAdminStarterCatalog returns only ready starter templates for the
// principal and tenant established by trusted middleware.
func (s *PersonaAdminStarterSource) PersonaAdminStarterCatalog(ctx context.Context, req productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminStarterCatalog, error) {
	if s == nil || ctx == nil || s.Skills == nil || s.Manifests == nil || s.Instructions == nil || s.Grants == nil || s.Authorizer == nil {
		return productui.PersonaAdminStarterCatalog{}, nil
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Subject() == "" || p.Tenant().Validate() != nil ||
		req.TenantID != string(p.Tenant()) || req.Principal != p.Subject() {
		return productui.PersonaAdminStarterCatalog{}, ErrPersonaCatalogDenied
	}
	tenant := p.Tenant()
	if err := s.Authorizer.AuthorizePersonaCatalog(ctx, p, tenant); err != nil {
		return productui.PersonaAdminStarterCatalog{}, fmt.Errorf("%w: %v", ErrPersonaCatalogDenied, err)
	}
	manifestResolver, err := s.Manifests.ForTenant(ctx, tenant)
	if err != nil || manifestResolver == nil {
		return productui.PersonaAdminStarterCatalog{}, nil
	}
	instructionResolver, err := s.Instructions.ForTenant(ctx, tenant)
	if err != nil || instructionResolver == nil {
		return productui.PersonaAdminStarterCatalog{}, nil
	}
	grantReader, err := s.Grants.ForTenant(ctx, tenant)
	if err != nil || grantReader == nil {
		return productui.PersonaAdminStarterCatalog{}, nil
	}
	starters := agenttemplate.PersonaStarters()
	ready := make([]productui.PersonaAdminStarter, 0, len(starters))
	for _, starter := range starters {
		projected, ok := s.readyStarter(ctx, tenant, starter, manifestResolver, instructionResolver, grantReader)
		if ok {
			ready = append(ready, projected)
		}
	}
	return productui.PersonaAdminStarterCatalog{Available: true, Starters: ready}, nil
}

func (s *PersonaAdminStarterSource) readyStarter(ctx context.Context, tenant values.TenantId, starter agenttemplate.PersonaStarter, manifests PersonaStarterManifestResolver, instructions PersonaStarterInstructionsResolver, grants PersonaProfileGrantReader) (productui.PersonaAdminStarter, bool) {
	manifestID := personaAdminStarterManifestID(starter.ID)
	manifest, err := manifests.ResolveCurrentPersonaManifest(ctx, manifestID)
	if err != nil || !personaAdminStarterManifestMatches(manifest, starter, manifestID) {
		return productui.PersonaAdminStarter{}, false
	}
	text, err := instructions.ResolvePersonaInstructions(ctx, manifest.ID, manifest.Version, manifest.InstructionsDigest)
	if err != nil || text != personaStarterInstructions(starter) || !personaInstructionsMatchDigest(text, manifest.InstructionsDigest) {
		return productui.PersonaAdminStarter{}, false
	}
	grantIDs := make([]string, 0, len(starter.SkillPins))
	for _, pin := range starter.SkillPins {
		record, err := s.Skills.ResolvePin(pin)
		if err != nil || record.Status != agentskills.StatusActive || record.Digest != pin.Digest || record.Definition.Key() != pin.Key() {
			return productui.PersonaAdminStarter{}, false
		}
		rows, err := grants.Grants(ctx, tenant, pin.Key())
		if err != nil {
			return productui.PersonaAdminStarter{}, false
		}
		current := make([]agentgate.SkillGrant, 0, len(rows))
		for _, row := range rows {
			if validPersonaSkillGrant(row, tenant, pin.Key()) {
				current = append(current, row)
				grantIDs = append(grantIDs, row.ID)
			}
		}
		if !personaStarterGrantRowsCoverAudience(current, starter) {
			return productui.PersonaAdminStarter{}, false
		}
	}
	slices.Sort(grantIDs)
	grantIDs = slices.Compact(grantIDs)
	return productui.PersonaAdminStarter{
		ID: starter.ID, Version: starter.Version, Name: starter.DisplayName, Handle: starter.Handle,
		Purpose: starter.Purpose, ManifestID: manifest.ID,
		ChannelClasses: slices.Clone(starter.AllowedChannelClasses), SkillGrantIDs: grantIDs,
	}, true
}

func personaAdminStarterManifestID(starterID string) string {
	const prefix = "hcmnext.persona_template."
	if !strings.HasPrefix(starterID, prefix) || len(starterID) == len(prefix) {
		return ""
	}
	return "agent.starter." + strings.TrimPrefix(starterID, prefix)
}

func personaAdminStarterManifestMatches(manifest agentmanifest.Manifest, starter agenttemplate.PersonaStarter, id string) bool {
	if id == "" || manifest.Validate() != nil || manifest.ID != id || manifest.Version != 1 || manifest.SchemaVersion != agentmanifest.CurrentSchemaVersion ||
		manifest.Purpose != starter.Purpose || manifest.InstructionsDigest != personaInstructionDigest(personaStarterInstructions(starter)) ||
		manifest.AutonomyCeiling != "ASSISTED" || len(manifest.SourceCeiling) != 0 || len(manifest.ToolCeiling) != 0 || len(manifest.ContextGrants) != 0 ||
		len(manifest.EvaluationRefs) != 1 || manifest.EvaluationRefs[0].ID != starter.EvaluationSuite || !validTrustedPersonaReference(manifest.ModelPolicy) || !validTrustedPersonaReference(manifest.OutputSchema) {
		return false
	}
	return manifest.Budget == (agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 4000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1})
}

func personaStarterGrantRowsCoverAudience(grants []agentgate.SkillGrant, starter agenttemplate.PersonaStarter) bool {
	if len(grants) == 0 || len(starter.AudienceRoles) == 0 || len(starter.AudiencePopulations) == 0 {
		return false
	}
	organizationScopes := make(map[string]struct{})
	for _, grant := range grants {
		for _, organization := range grant.OrganizationScopes {
			organizationScopes[organization] = struct{}{}
		}
	}
	if len(organizationScopes) == 0 {
		return false
	}
	for _, role := range starter.AudienceRoles {
		for _, population := range starter.AudiencePopulations {
			for organization := range organizationScopes {
				if !tupleGrantCovers(grants, role, population, organization) {
					return false
				}
			}
		}
	}
	return true
}

var _ productui.PersonaAdminStarterSource = (*PersonaAdminStarterSource)(nil)
