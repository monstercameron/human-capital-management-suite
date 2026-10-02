package application

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PersonaStarterVersionRequest contains the editable fields for a new
// immutable version. Authority-bearing fields are omitted or must match the
// current version exactly.
type PersonaStarterVersionRequest struct {
	StarterID          string
	StarterVersion     uint32
	Version            uint32
	Handle             string
	DisplayName        string
	Purpose            string
	Instructions       string
	Guidance           *string
	DocumentReferences []agentdocref.Reference
	ChannelClasses     []agentpersona.ChannelClass
	BusinessOwnerID    string
	TechnicalStewardID string
}

// BuildVersion derives and revalidates a new version from the latest trusted
// persona version. It retains the exact manifest, skill pins, tier ceiling,
// audience, and evaluation limits. It does not persist, request review, or
// publish the resulting version.
func (b *PersonaStarterDraftBuilder) BuildVersion(ctx context.Context, current agentpersona.PersonaVersion, req PersonaStarterVersionRequest) (agentpersona.PersonaVersion, error) {
	if b == nil || ctx == nil || b.Drafts == nil || b.Drafts.Profiles == nil || b.Manifests == nil || b.Instructions == nil {
		return agentpersona.PersonaVersion{}, ErrPersonaStarterDraftUnavailable
	}
	starter, ok := agenttemplate.PersonaStarterFor(req.StarterID, req.StarterVersion)
	if !ok || current.Verify() != nil || !validPersonaStarterVersionRequest(current, req) || !personaVersionWithinStarter(current.Profile, starter) {
		return agentpersona.PersonaVersion{}, ErrPersonaDraftInvalid
	}
	manifest, err := b.Manifests.ResolveCurrentPersonaManifest(ctx, current.Profile.Manifest.ID)
	if err != nil || manifest.ID != current.Profile.Manifest.ID || manifest.Version != uint64(current.Profile.Manifest.Version) || manifest.SchemaVersion != current.Profile.Manifest.SchemaVersion || manifestDigest(manifest) != current.Profile.Manifest.Digest {
		return agentpersona.PersonaVersion{}, fmt.Errorf("%w: current manifest changed", ErrPersonaDraftInvalid)
	}
	instructions, err := b.Instructions.ResolvePersonaInstructions(ctx, manifest.ID, manifest.Version, manifest.InstructionsDigest)
	if err != nil || !personaInstructionsMatchDigest(instructions, manifest.InstructionsDigest) || current.Profile.Instructions != instructions {
		return agentpersona.PersonaVersion{}, fmt.Errorf("%w: current executable instructions changed", ErrPersonaDraftInvalid)
	}
	profile := clonePersonaVersionProfile(current.Profile)
	profile.Version = req.Version
	if req.Handle != "" {
		profile.Handle = req.Handle
	}
	if req.DisplayName != "" {
		profile.DisplayName = req.DisplayName
	}
	if req.Purpose != "" {
		profile.Purpose = req.Purpose
	}
	if len(req.ChannelClasses) > 0 {
		profile.ChannelClasses = append([]agentpersona.ChannelClass(nil), req.ChannelClasses...)
	}
	if req.DocumentReferences != nil {
		profile.DocumentReferences = append([]agentdocref.Reference(nil), req.DocumentReferences...)
	}
	if req.Guidance != nil {
		profile.Guidance = strings.Clone(*req.Guidance)
	}
	version, err := buildStarterVersionProfile(ctx, b.Drafts.Profiles, profile)
	if err != nil {
		return agentpersona.PersonaVersion{}, fmt.Errorf("%w: new version validation failed: %v", ErrPersonaDraftInvalid, err)
	}
	return version, nil
}

func buildStarterVersionProfile(ctx context.Context, builder PersonaProfileBuilder, profile agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	if scoped, ok := builder.(ContextPersonaProfileBuilder); ok {
		principal, trusted := trust.FromContext(ctx)
		if !trusted || principal == nil {
			return agentpersona.PersonaVersion{}, ErrPersonaDraftInvalid
		}
		return scoped.BuildForTenant(ctx, principal.Tenant(), profile)
	}
	if builder == nil {
		return agentpersona.PersonaVersion{}, ErrPersonaStarterDraftUnavailable
	}
	return builder.Build(profile)
}

func validPersonaStarterVersionRequest(current agentpersona.PersonaVersion, req PersonaStarterVersionRequest) bool {
	if req.Version == 0 || current.Profile.Version == math.MaxUint32 || req.Version != current.Profile.Version+1 ||
		req.BusinessOwnerID != "" && req.BusinessOwnerID != current.Profile.Owner ||
		req.TechnicalStewardID != "" && req.TechnicalStewardID != current.Profile.Steward {
		return false
	}
	if req.Handle != "" && (len(req.Handle) > 64 || !validPersonaHandle(req.Handle)) {
		return false
	}
	if req.DisplayName != "" && (len(req.DisplayName) > 120 || strings.TrimSpace(req.DisplayName) != req.DisplayName || !utf8.ValidString(req.DisplayName)) {
		return false
	}
	if req.Purpose != "" && (len(req.Purpose) > 1000 || strings.TrimSpace(req.Purpose) != req.Purpose || !utf8.ValidString(req.Purpose)) {
		return false
	}
	if req.Instructions != "" && req.Instructions != current.Profile.Instructions {
		return false
	}
	seenChannels := make(map[agentpersona.ChannelClass]struct{}, len(req.ChannelClasses))
	for _, class := range req.ChannelClasses {
		if class == agentpersona.ChannelExternal || !containsPersonaChannel(current.Profile.ChannelClasses, class) {
			return false
		}
		if _, exists := seenChannels[class]; exists {
			return false
		}
		seenChannels[class] = struct{}{}
	}
	return true
}

func personaVersionWithinStarter(profile agentpersona.PersonaProfile, starter agenttemplate.PersonaStarter) bool {
	pin := agenttemplate.PersonaStarterPin(starter)
	if profile.Template == nil || profile.Template.ID != pin.ID || profile.Template.Version != pin.Version || profile.Template.Digest != pin.Digest || profile.AlwaysPrivate != starter.AlwaysPrivate {
		return false
	}
	for _, class := range profile.AllowedPlacementClasses {
		if !personaStarterContainsString(starter.AllowedChannelClasses, class) {
			return false
		}
	}
	if len(profile.AllowedPlacementClasses) == 0 {
		return false
	}
	ceilings := personaStarterTierCeilings(starter)
	if len(profile.ConversationTierCeilings) != len(ceilings) {
		return false
	}
	for kind, ceiling := range ceilings {
		if got, ok := profile.ConversationTierCeilings[kind]; !ok || got != ceiling {
			return false
		}
	}
	maxTier, ok := personaStarterTier(starter.TierCeiling)
	if !ok || profile.TierCeiling > maxTier || !sameSkillPins(profile.SkillPins, starter.SkillPins) {
		return false
	}
	channels := personaStarterChannels(starter)
	seenChannels := make(map[agentpersona.ChannelClass]struct{}, len(profile.ChannelClasses))
	for _, class := range profile.ChannelClasses {
		if !containsPersonaChannel(channels, class) || class == agentpersona.ChannelExternal {
			return false
		}
		if _, exists := seenChannels[class]; exists {
			return false
		}
		seenChannels[class] = struct{}{}
	}
	for _, role := range profile.Audience.Roles {
		if !personaStarterContainsString(starter.AudienceRoles, "ALL_MEMBERS") && !personaStarterContainsString(starter.AudienceRoles, role) {
			return false
		}
	}
	for _, population := range profile.Audience.Populations {
		if !personaStarterContainsString(starter.AudiencePopulations, "TENANT_WIDE") && !personaStarterContainsString(starter.AudiencePopulations, population) {
			return false
		}
	}
	allowedKinds := []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationGroup, agentpersona.ConversationChannel, agentpersona.ConversationThread}
	seenKinds := make(map[agentpersona.ConversationKind]struct{}, len(profile.ConversationKinds))
	for _, kind := range profile.ConversationKinds {
		if !containsPersonaConversation(allowedKinds, kind) {
			return false
		}
		if _, exists := seenKinds[kind]; exists {
			return false
		}
		seenKinds[kind] = struct{}{}
	}
	return true
}

func personaStarterTier(tier string) (agentskills.SideEffectTier, bool) {
	value, ok := map[string]agentskills.SideEffectTier{"T0": agentskills.TierT0, "T1": agentskills.TierT1, "T2": agentskills.TierT2, "T3": agentskills.TierT3}[tier]
	return value, ok
}

func personaStarterChannels(starter agenttemplate.PersonaStarter) []agentpersona.ChannelClass {
	channels := []agentpersona.ChannelClass{agentpersona.ChannelPrivate}
	if starter.PublicAnswersExpected || personaStarterContainsString(starter.AllowedChannelClasses, "ONBOARDING") || personaStarterContainsString(starter.AllowedChannelClasses, "CREW") || personaStarterContainsString(starter.AllowedChannelClasses, "SCHEDULER") {
		channels = append(channels, agentpersona.ChannelPublic)
	}
	return channels
}

func sameSkillPins(left, right []agentskills.SkillPin) bool {
	if len(left) != len(right) {
		return false
	}
	for _, pin := range left {
		found := false
		for _, expected := range right {
			if pin == expected {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containsPersonaChannel(channels []agentpersona.ChannelClass, wanted agentpersona.ChannelClass) bool {
	for _, channel := range channels {
		if channel == wanted {
			return true
		}
	}
	return false
}

func containsPersonaConversation(kinds []agentpersona.ConversationKind, wanted agentpersona.ConversationKind) bool {
	for _, kind := range kinds {
		if kind == wanted {
			return true
		}
	}
	return false
}

func personaStarterContainsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validPersonaHandle(handle string) bool {
	if handle == "" || strings.TrimSpace(handle) != handle || handle[0] == '-' || handle[len(handle)-1] == '-' {
		return false
	}
	previousHyphen := false
	for _, char := range handle {
		hyphen := char == '-'
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || hyphen) || hyphen && previousHyphen {
			return false
		}
		previousHyphen = hyphen
	}
	return true
}

func clonePersonaVersionProfile(profile agentpersona.PersonaProfile) agentpersona.PersonaProfile {
	profile.AllowedPlacementClasses = append(profile.AllowedPlacementClasses[:0:0], profile.AllowedPlacementClasses...)
	if profile.Template != nil {
		copied := *profile.Template
		profile.Template = &copied
	}
	if profile.ConversationTierCeilings != nil {
		ceilings := make(map[agentpersona.ConversationKind]agentskills.SideEffectTier, len(profile.ConversationTierCeilings))
		for kind, ceiling := range profile.ConversationTierCeilings {
			ceilings[kind] = ceiling
		}
		profile.ConversationTierCeilings = ceilings
	}
	profile.SkillPins = append(profile.SkillPins[:0:0], profile.SkillPins...)
	profile.Audience.Roles = append(profile.Audience.Roles[:0:0], profile.Audience.Roles...)
	profile.Audience.Populations = append(profile.Audience.Populations[:0:0], profile.Audience.Populations...)
	profile.Audience.OrganizationScopes = append(profile.Audience.OrganizationScopes[:0:0], profile.Audience.OrganizationScopes...)
	profile.ConversationKinds = append(profile.ConversationKinds[:0:0], profile.ConversationKinds...)
	profile.ChannelClasses = append(profile.ChannelClasses[:0:0], profile.ChannelClasses...)
	profile.DocumentReferences = append(profile.DocumentReferences[:0:0], profile.DocumentReferences...)
	profile.DataClassesRead = append(profile.DataClassesRead[:0:0], profile.DataClassesRead...)
	profile.DataClassesWritten = append(profile.DataClassesWritten[:0:0], profile.DataClassesWritten...)
	return profile
}
