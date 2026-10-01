package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrPersonaCatalogDenied = errors.New("application: persona catalog access denied")

type personaCatalogStageError struct {
	stage string
	err   error
}

func (e personaCatalogStageError) Error() string {
	return fmt.Sprintf("persona catalog %s: %v", e.stage, e.err)
}
func (e personaCatalogStageError) Unwrap() error { return e.err }
func (e personaCatalogStageError) PersonaCatalogFailureStage() string {
	var nested interface{ PersonaCatalogFailureStage() string }
	if errors.As(e.err, &nested) {
		return nested.PersonaCatalogFailureStage()
	}
	return e.stage
}

func personaCatalogStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	return personaCatalogStageError{stage: stage, err: err}
}

// PersonaCatalogVersion is the immutable profile and trusted lifecycle/review projection read for one tenant.
type PersonaCatalogVersion struct {
	Profile            agentpersona.PersonaVersion
	Lifecycle          agentpersona.LifecycleState
	Owner              string
	Steward            string
	ReviewRequired     bool
	ReviewApproved     bool
	Reviewer           string
	EvaluationRef      string
	InvocationsPerHour string
	ConcurrentTasks    string
	DailySpend         string
}

// PersonaCatalogVersionReader lists at most one current catalog version per persona for one tenant.
type PersonaCatalogVersionReader interface {
	ListPersonaCatalogVersions(context.Context, values.TenantId) ([]PersonaCatalogVersion, error)
}

// PersonaCatalogInstallation is a tenant-scoped placement projection.
type PersonaCatalogInstallation struct {
	ID                 string
	PersonaID          string
	PersonaVersion     uint32
	ConversationID     string
	Conversation       string
	Kind               string
	Audience           string
	ReplyPlacement     string
	Active             bool
	ChannelClass       string
	ConversationKind   string
	MaxTier            agentskills.SideEffectTier
	AllowedDataClasses []string
}

// PersonaCatalogInstallationReader lists placements for exactly one tenant.
type PersonaCatalogInstallationReader interface {
	ListPersonaCatalogInstallations(context.Context, values.TenantId) ([]PersonaCatalogInstallation, error)
}

// PersonaCatalogTargetReader supplies only targets the caller may inspect.
type PersonaCatalogTargetReader interface {
	ListPersonaCatalogTargets(context.Context, trust.Principal, values.TenantId) ([]productui.PersonaAdminTarget, []productui.PersonaAdminTarget, error)
}

// PersonaCatalogGrant is an explicit result from the effective-grant system.
// A missing grant is represented by Allowed=false and never promoted to access.
type PersonaCatalogGrant struct {
	Allowed     bool
	Tier        string
	DataClasses []string
	Reason      string
}

// PersonaCatalogGrantReader resolves a pinned skill under the chosen subject and conversation.
type PersonaCatalogGrantReader interface {
	ResolvePersonaSkillGrant(context.Context, trust.Principal, values.TenantId, string, string, agentskills.SkillPin) (PersonaCatalogGrant, error)
}

// PersonaCatalogAuthorizer verifies the server-owned administration permission.
type PersonaCatalogAuthorizer interface {
	AuthorizePersonaCatalog(context.Context, *trust.Principal, values.TenantId) error
}

// PersonaAdminCatalogService implements the metadata-only PersonaAdminClient reads.
type PersonaAdminCatalogService struct {
	Versions      PersonaCatalogVersionReader
	Installations PersonaCatalogInstallationReader
	Targets       PersonaCatalogTargetReader
	Skills        agentpersona.SkillResolver
	Grants        PersonaCatalogGrantReader
	Authorizer    PersonaCatalogAuthorizer
	Starters      productui.PersonaAdminStarterSource
}

// Snapshot returns an authorized, tenant-bound catalog without task content.
func (s *PersonaAdminCatalogService) Snapshot(ctx context.Context, req productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	p, tenant, err := s.authorize(ctx, req.TenantID, req.Principal)
	if err != nil {
		return productui.PersonaAdminSnapshot{}, personaCatalogStage("authorization", err)
	}
	versions, err := s.Versions.ListPersonaCatalogVersions(ctx, tenant)
	if err != nil {
		return productui.PersonaAdminSnapshot{}, personaCatalogStage("versions", fmt.Errorf("list persona versions: %w", err))
	}
	installs, err := s.Installations.ListPersonaCatalogInstallations(ctx, tenant)
	if err != nil {
		return productui.PersonaAdminSnapshot{}, personaCatalogStage("installations", fmt.Errorf("list persona installations: %w", err))
	}
	users, conversations, err := s.Targets.ListPersonaCatalogTargets(ctx, *p, tenant)
	if err != nil {
		return productui.PersonaAdminSnapshot{}, personaCatalogStage("targets", fmt.Errorf("list persona preview targets: %w", err))
	}
	if !validTargets(users) || !validTargets(conversations) {
		return productui.PersonaAdminSnapshot{}, personaCatalogStage("target_validation", ErrPersonaCatalogDenied)
	}
	byPersona := make(map[catalogPersonaVersionKey][]PersonaCatalogInstallation)
	for _, in := range installs {
		if in.ID == "" || in.PersonaID == "" || in.ConversationID == "" {
			return productui.PersonaAdminSnapshot{}, personaCatalogStage("installation_validation", ErrPersonaCatalogDenied)
		}
		if in.Active {
			key := catalogPersonaVersionKey{personaID: in.PersonaID, version: in.PersonaVersion}
			byPersona[key] = append(byPersona[key], in)
		}
	}
	out := productui.PersonaAdminSnapshot{Available: true, SubjectOptions: slices.Clone(users), Conversations: slices.Clone(conversations)}
	if s.Starters != nil {
		starters, starterErr := s.Starters.PersonaAdminStarterCatalog(ctx, req)
		if starterErr == nil && starters.Available {
			out.StarterCatalogAvailable = true
			out.Starters = slices.Clone(starters.Starters)
		}
	}
	seenPersonas := make(map[string]struct{}, len(versions))
	for _, v := range versions {
		if v.Profile.Verify() != nil || v.Profile.Profile.PersonaID == "" || v.Profile.Profile.Version == 0 || !validLifecycle(v.Lifecycle) {
			return productui.PersonaAdminSnapshot{}, personaCatalogStage("profile_validation", ErrPersonaCatalogDenied)
		}
		profile := v.Profile.Profile
		if _, exists := seenPersonas[profile.PersonaID]; exists {
			return productui.PersonaAdminSnapshot{}, personaCatalogStage("profile_validation", ErrPersonaCatalogDenied)
		}
		seenPersonas[profile.PersonaID] = struct{}{}
		persona := productui.PersonaAdminPersona{ID: profile.PersonaID, Handle: profile.Handle, Name: profile.DisplayName, Purpose: profile.Purpose, Lifecycle: productui.PersonaAdminLifecycle(v.Lifecycle), Owner: v.Owner, Steward: v.Steward, Version: fmt.Sprint(profile.Version), Audience: audienceLabel(profile.Audience), ReviewRequired: v.ReviewRequired, ReviewApproved: v.ReviewApproved, Reviewer: v.Reviewer, EvaluationRef: v.EvaluationRef, Limits: productui.PersonaAdminLimits{InvocationsPerHour: v.InvocationsPerHour, ConcurrentTasks: v.ConcurrentTasks, DailySpend: v.DailySpend}}
		persona.ChannelClasses = personaCatalogChannelClasses(profile.ChannelClasses)
		if starter, ok := personaCatalogStarter(profile); ok {
			persona.StarterID = starter.ID
			persona.StarterVersion = starter.Version
		}
		for _, pin := range profile.SkillPins {
			record, e := s.Skills.ResolvePin(pin)
			if e != nil {
				return productui.PersonaAdminSnapshot{}, personaCatalogStage("skill_resolution", fmt.Errorf("resolve pinned persona skill: %w", e))
			}
			tier := max(record.Definition.SideEffectTier, record.HighestCapabilityTier)
			data := uniqueSorted(append(slices.Clone(record.Definition.DataClassesRead), record.Definition.DataClassesWritten...))
			persona.Skills = append(persona.Skills, productui.PersonaAdminSkill{ID: pin.ID, Name: pin.ID, Tier: tier.String(), DataClasses: data})
			persona.DerivedData = append(persona.DerivedData, data...)
		}
		persona.DerivedData = uniqueSorted(persona.DerivedData)
		for _, in := range installs {
			if !in.Active || in.PersonaID != persona.ID {
				continue
			}
			persona.Installations = append(persona.Installations, productui.PersonaAdminInstallation{Version: fmt.Sprint(in.PersonaVersion), ConversationID: in.ConversationID, Conversation: in.Conversation, Kind: in.Kind, Audience: in.Audience, ReplyPlacement: in.ReplyPlacement})
		}
		out.Personas = append(out.Personas, persona)
	}
	if len(out.Personas) > 0 && len(users) > 0 && len(conversations) > 0 {
		for _, persona := range out.Personas {
			for _, in := range byPersona[catalogPersonaVersionKey{personaID: persona.ID, version: uint32(parseVersion(persona.Version))}] {
				if !targetContains(conversations, in.ConversationID) {
					continue
				}
				preview, e := s.preview(ctx, p, tenant, versions, persona.ID, users[0].ID, in.ConversationID)
				if e != nil {
					return productui.PersonaAdminSnapshot{}, personaCatalogStage("preview", e)
				}
				out.Preview = preview
				return out, nil
			}
		}
	}
	return out, nil
}

func personaCatalogChannelClasses(classes []agentpersona.ChannelClass) []string {
	out := make([]string, 0, len(classes))
	for _, class := range classes {
		out = append(out, string(class))
	}
	return uniqueSorted(out)
}

func personaCatalogStarter(profile agentpersona.PersonaProfile) (agenttemplate.PersonaStarter, bool) {
	if strings.TrimSpace(profile.EvalSuiteRef) == "" {
		return agenttemplate.PersonaStarter{}, false
	}
	var match agenttemplate.PersonaStarter
	for _, starter := range agenttemplate.PersonaStarters() {
		if profile.Template != nil {
			pin := agenttemplate.PersonaStarterPin(starter)
			if profile.Template.ID != pin.ID || profile.Template.Version != pin.Version || profile.Template.Digest != pin.Digest {
				continue
			}
		}
		if starter.EvaluationSuite != profile.EvalSuiteRef {
			continue
		}
		if match.ID != "" {
			return agenttemplate.PersonaStarter{}, false
		}
		match = starter
	}
	return match, match.ID != ""
}

// Preview computes effective skills only from explicit grants for the selected targets.
func (s *PersonaAdminCatalogService) Preview(ctx context.Context, req productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	p, tenant, err := s.authorize(ctx, "", "")
	if err != nil {
		return productui.PersonaAdminPreview{}, err
	}
	if req.PersonaID == "" || req.SubjectID == "" || req.ConversationID == "" {
		return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
	}
	users, conversations, err := s.Targets.ListPersonaCatalogTargets(ctx, *p, tenant)
	if err != nil {
		return productui.PersonaAdminPreview{}, err
	}
	if !validTargets(users) || !validTargets(conversations) || !targetContains(users, req.SubjectID) || !targetContains(conversations, req.ConversationID) {
		return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
	}
	versions, err := s.Versions.ListPersonaCatalogVersions(ctx, tenant)
	if err != nil {
		return productui.PersonaAdminPreview{}, err
	}
	return s.preview(ctx, p, tenant, versions, req.PersonaID, req.SubjectID, req.ConversationID)
}

func (s *PersonaAdminCatalogService) preview(ctx context.Context, p *trust.Principal, tenant values.TenantId, versions []PersonaCatalogVersion, personaID, subjectID, conversationID string) (productui.PersonaAdminPreview, error) {
	var selected *PersonaCatalogVersion
	for i := range versions {
		if versions[i].Profile.Profile.PersonaID == personaID {
			if selected != nil {
				return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
			}
			selected = &versions[i]
		}
	}
	if selected == nil || selected.Lifecycle != agentpersona.StatePublished || selected.Profile.Verify() != nil {
		return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
	}
	preview := productui.PersonaAdminPreview{Subject: subjectID, Conversation: conversationID, Audience: audienceLabel(selected.Profile.Profile.Audience)}
	installs, err := s.Installations.ListPersonaCatalogInstallations(ctx, tenant)
	if err != nil {
		return productui.PersonaAdminPreview{}, fmt.Errorf("list persona preview installations: %w", err)
	}
	installation, ok := matchingPreviewInstallation(installs, *selected, conversationID)
	if !ok || !slices.Contains(selected.Profile.Profile.ChannelClasses, agentpersona.ChannelClass(installation.ChannelClass)) || !slices.Contains(selected.Profile.Profile.ConversationKinds, agentpersona.ConversationKind(installation.ConversationKind)) {
		return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
	}
	preview.ConversationKind = installation.ConversationKind
	preview.ReplyPlacement = installation.ReplyPlacement
	for _, pin := range selected.Profile.Profile.SkillPins {
		record, err := s.Skills.ResolvePin(pin)
		if err != nil {
			return productui.PersonaAdminPreview{}, fmt.Errorf("resolve preview pinned skill: %w", err)
		}
		pinnedTier := max(record.Definition.SideEffectTier, record.HighestCapabilityTier)
		pinnedData := uniqueSorted(append(slices.Clone(record.Definition.DataClassesRead), record.Definition.DataClassesWritten...))
		if !pinnedTier.Valid() || pinnedTier > selected.Profile.Profile.TierCeiling || pinnedTier > installation.MaxTier || !stringSetSubset(pinnedData, installation.AllowedDataClasses) {
			return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
		}
		g, err := s.Grants.ResolvePersonaSkillGrant(ctx, *p, tenant, subjectID, conversationID, pin)
		if err != nil {
			return productui.PersonaAdminPreview{}, fmt.Errorf("resolve preview grant: %w", err)
		}
		if !g.Allowed {
			preview.Warnings = append(preview.Warnings, nonempty(g.Reason, "skill is unavailable under this principal"))
			continue
		}
		grantedTier, validTier := parsePersonaTier(g.Tier)
		if !validTier || grantedTier > pinnedTier || grantedTier > installation.MaxTier || !stringSetSubset(g.DataClasses, pinnedData) || !stringSetSubset(g.DataClasses, installation.AllowedDataClasses) {
			return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
		}
		preview.EffectiveSkills = append(preview.EffectiveSkills, productui.PersonaAdminSkill{ID: pin.ID, Name: pin.ID, Tier: g.Tier, DataClasses: slices.Clone(g.DataClasses)})
		preview.DerivedData = append(preview.DerivedData, g.DataClasses...)
	}
	preview.DerivedData = uniqueSorted(preview.DerivedData)
	return preview, nil
}

func (s *PersonaAdminCatalogService) authorize(ctx context.Context, tenantID, principalID string) (*trust.Principal, values.TenantId, error) {
	if s == nil || ctx == nil || s.Versions == nil || s.Installations == nil || s.Targets == nil || s.Skills == nil || s.Grants == nil || s.Authorizer == nil {
		return nil, "", ErrPersonaCatalogDenied
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant().Validate() != nil || p.Subject() == "" {
		return nil, "", ErrPersonaCatalogDenied
	}
	tenant := p.Tenant()
	if tenantID != "" && tenantID != string(tenant) || principalID != "" && principalID != p.Subject() {
		return nil, "", ErrPersonaCatalogDenied
	}
	if err := s.Authorizer.AuthorizePersonaCatalog(ctx, p, tenant); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrPersonaCatalogDenied, err)
	}
	return p, tenant, nil
}

func matchingPreviewInstallation(installs []PersonaCatalogInstallation, version PersonaCatalogVersion, conversationID string) (PersonaCatalogInstallation, bool) {
	var match PersonaCatalogInstallation
	found := false
	for _, in := range installs {
		if in.ID == "" || in.PersonaID == "" || in.ConversationID == "" || !in.MaxTier.Valid() {
			return PersonaCatalogInstallation{}, false
		}
		if in.PersonaID != version.Profile.Profile.PersonaID || in.PersonaVersion != version.Profile.Profile.Version || in.ConversationID != conversationID || !in.Active {
			continue
		}
		if found {
			return PersonaCatalogInstallation{}, false
		}
		match, found = in, true
	}
	return match, found
}

type catalogPersonaVersionKey struct {
	personaID string
	version   uint32
}

func parseVersion(value string) uint64 { n, _ := strconv.ParseUint(value, 10, 32); return n }

func stringSetSubset(values, ceiling []string) bool {
	allowed := make(map[string]struct{}, len(ceiling))
	for _, value := range ceiling {
		if strings.TrimSpace(value) == "" {
			return false
		}
		allowed[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			return false
		}
	}
	return true
}

func parsePersonaTier(value string) (agentskills.SideEffectTier, bool) {
	for tier := agentskills.TierRead; tier <= agentskills.TierExternalWrite; tier++ {
		if tier.String() == strings.TrimSpace(value) {
			return tier, true
		}
	}
	return 0, false
}

func validTargets(ts []productui.PersonaAdminTarget) bool {
	seen := map[string]bool{}
	for _, t := range ts {
		if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Label) == "" || seen[t.ID] {
			return false
		}
		seen[t.ID] = true
	}
	return true
}
func targetContains(ts []productui.PersonaAdminTarget, id string) bool {
	return slices.ContainsFunc(ts, func(t productui.PersonaAdminTarget) bool { return t.ID == id })
}
func validLifecycle(v agentpersona.LifecycleState) bool {
	switch v {
	case agentpersona.StateDraft, agentpersona.StateInReview, agentpersona.StatePublished, agentpersona.StateSuspended, agentpersona.StateRetired:
		return true
	}
	return false
}
func audienceLabel(a agentpersona.Audience) string {
	xs := append(append([]string{}, a.Roles...), a.Populations...)
	xs = append(xs, a.OrganizationScopes...)
	return strings.Join(uniqueSorted(xs), ", ")
}
func uniqueSorted(xs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	slices.Sort(out)
	return out
}
func nonempty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
