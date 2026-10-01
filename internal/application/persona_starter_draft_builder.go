package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrPersonaStarterDraftUnavailable = errors.New("application: persona starter draft builder unavailable")

// PersonaStarterManifestResolver returns the tenant's currently published
// manifest for an ID. The returned immutable manifest, rather than a digest
// supplied by the caller, determines the persona's manifest reference.
type PersonaStarterManifestResolver interface {
	ResolveCurrentPersonaManifest(context.Context, string) (agentmanifest.Manifest, error)
}

// PersonaStarterInstructionsResolver loads executable text by its exact
// manifest identity and instructions digest.
type PersonaStarterInstructionsResolver interface {
	ResolvePersonaInstructions(context.Context, string, uint64, string) (string, error)
}

// PersonaStarterDraftRequest identifies a starter and the tenant-owned persona
// identity. Tenant, actor, manifest digest, skills, and executable instructions
// are resolved by server authorities and cannot be asserted here.
type PersonaStarterDraftRequest struct {
	StarterID          string
	StarterVersion     uint32
	PersonaID          string
	AvatarRef          string
	OrganizationScopes []string
	ManifestID         string
	BusinessOwnerID    string
	TechnicalStewardID string
}

// PersonaStarterDraftBuilder turns a governed starter into a validated DRAFT.
// It never issues review/evaluation evidence or changes lifecycle state.
type PersonaStarterDraftBuilder struct {
	Drafts       *PersonaAdminDraftService
	Manifests    PersonaStarterManifestResolver
	Instructions PersonaStarterInstructionsResolver
}

// CreateDraft resolves a starter, current exact manifest, and digest-bound
// executable instructions, validates the resulting profile, then delegates
// persistence and create authorization to PersonaAdminDraftService.
func (b *PersonaStarterDraftBuilder) CreateDraft(ctx context.Context, req PersonaStarterDraftRequest) (PersonaDraft, error) {
	if b == nil || ctx == nil || b.Drafts == nil || b.Drafts.Profiles == nil || b.Manifests == nil || b.Instructions == nil {
		return PersonaDraft{}, ErrPersonaStarterDraftUnavailable
	}
	starter, ok := agenttemplate.PersonaStarterFor(req.StarterID, req.StarterVersion)
	if !ok || !validPersonaStarterDraftRequest(req) {
		return PersonaDraft{}, ErrPersonaDraftInvalid
	}
	manifest, err := b.Manifests.ResolveCurrentPersonaManifest(ctx, req.ManifestID)
	if err != nil || manifest.ID != req.ManifestID || manifest.Validate() != nil || manifest.Version > math.MaxUint32 {
		return PersonaDraft{}, fmt.Errorf("%w: current manifest unavailable", ErrPersonaDraftInvalid)
	}
	instructions, err := b.Instructions.ResolvePersonaInstructions(ctx, manifest.ID, manifest.Version, manifest.InstructionsDigest)
	if err != nil || !personaInstructionsMatchDigest(instructions, manifest.InstructionsDigest) {
		return PersonaDraft{}, fmt.Errorf("%w: executable instructions do not match manifest", ErrPersonaDraftInvalid)
	}
	profile := personaStarterProfile(starter, req, manifest, instructions)
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return PersonaDraft{}, ErrPersonaDraftDenied
	}
	version, err := buildPersonaProfile(ctx, b.Drafts.Profiles, principal.Tenant(), profile)
	if err != nil {
		return PersonaDraft{}, fmt.Errorf("%w: starter profile validation failed: %v", ErrPersonaDraftInvalid, err)
	}
	return b.Drafts.CreateDraft(ctx, PersonaDraftRequest{Version: version, BusinessOwnerID: req.BusinessOwnerID, TechnicalStewardID: req.TechnicalStewardID})
}

func validPersonaStarterDraftRequest(req PersonaStarterDraftRequest) bool {
	for _, value := range []string{req.PersonaID, req.AvatarRef, req.ManifestID, req.BusinessOwnerID, req.TechnicalStewardID} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return false
		}
	}
	return req.StarterVersion > 0 && req.BusinessOwnerID != req.TechnicalStewardID && req.OrganizationScopes != nil
}

func personaInstructionsMatchDigest(instructions, digest string) bool {
	if strings.TrimSpace(instructions) == "" || len(digest) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	actual := sha256.Sum256([]byte(instructions))
	return digest == "sha256:"+hex.EncodeToString(actual[:])
}

func personaStarterProfile(starter agenttemplate.PersonaStarter, req PersonaStarterDraftRequest, manifest agentmanifest.Manifest, instructions string) agentpersona.PersonaProfile {
	tier := map[string]agentskills.SideEffectTier{"T0": agentskills.TierT0, "T1": agentskills.TierT1, "T2": agentskills.TierT2, "T3": agentskills.TierT3}[starter.TierCeiling]
	classes := personaStarterChannels(starter)
	kinds := []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationGroup, agentpersona.ConversationChannel, agentpersona.ConversationThread}
	ceilings := personaStarterTierCeilings(starter)
	pin := agenttemplate.PersonaStarterPin(starter)
	return agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: manifest.ID, Version: uint32(manifest.Version), Digest: manifestDigest(manifest), SchemaVersion: manifest.SchemaVersion},
		PersonaID: req.PersonaID, Version: 1, Handle: starter.Handle, DisplayName: starter.DisplayName,
		AvatarRef: req.AvatarRef, Purpose: starter.Purpose,
		Audience:  agentpersona.Audience{Roles: append([]string(nil), starter.AudienceRoles...), Populations: append([]string(nil), starter.AudiencePopulations...), OrganizationScopes: append([]string(nil), req.OrganizationScopes...)},
		SkillPins: append([]agentskills.SkillPin(nil), starter.SkillPins...), TierCeiling: tier,
		ConversationKinds: kinds,
		ChannelClasses:    classes, Instructions: instructions, Owner: req.BusinessOwnerID, Steward: req.TechnicalStewardID,
		AllowedPlacementClasses: append([]string(nil), starter.AllowedChannelClasses...), AlwaysPrivate: starter.AlwaysPrivate, ConversationTierCeilings: ceilings,
		Template:     &agentpersona.TemplateProvenance{ID: pin.ID, Version: pin.Version, Digest: pin.Digest},
		EvalSuiteRef: starter.EvaluationSuite, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 20, MaxSteps: 10, MaxLatencyMS: 2000},
	}
}

func personaStarterTierCeilings(starter agenttemplate.PersonaStarter) map[agentpersona.ConversationKind]agentskills.SideEffectTier {
	if starter.ID != "hcmnext.persona_template.schedule_fixer" {
		return nil
	}
	return map[agentpersona.ConversationKind]agentskills.SideEffectTier{agentpersona.ConversationDirect: agentskills.TierT3, agentpersona.ConversationGroup: agentskills.TierT1, agentpersona.ConversationChannel: agentskills.TierT1, agentpersona.ConversationThread: agentskills.TierT1}
}

func manifestDigest(manifest agentmanifest.Manifest) string {
	digest, _ := manifest.Digest()
	return digest
}
