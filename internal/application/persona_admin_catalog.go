package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
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
	Icon               agenticon.Value
	IconRevision       int64
	Profile            agentpersona.PersonaVersion
	Lifecycle          agentpersona.LifecycleState
	Owner              string
	Steward            string
	ReviewRequired     bool
	ReviewApproved     bool
	Reviewer           string
	EvaluationRef      string
	ReviewApprovedAt   string
	EvaluationPassedAt string
	InvocationsPerHour string
	ConcurrentTasks    string
	DailySpend         string
	WorkspaceDocuments int
	WorkspaceIndexedAt time.Time
	WorkspacePending   int
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
	State              string
	SuspensionReason   string
	SuspensionMessage  string
	RestartAvailable   bool
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

type personaCatalogTargetStateReader interface {
	ListPersonaCatalogTargetsWithState(context.Context, trust.Principal, values.TenantId) ([]productui.PersonaAdminTarget, []productui.PersonaAdminTarget, productui.PersonaAdminRegionState, error)
}

type personaCatalogReadableProjection interface {
	ResolvePersonaCatalogTarget(context.Context, values.TenantId, string) (productui.PersonaAdminTarget, error)
	ResolvePersonaCatalogRoleTargets(context.Context, trust.Principal, values.TenantId, []string, []string) []productui.PersonaAdminTarget
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
	Documents     PersonaAdminDocumentReader
	Runtime       PersonaCatalogRuntimeStatusReader
}

type PersonaAdminPlacementDocument struct {
	DocumentID string
	VersionID  string
	Title      string
}

type PersonaAdminPlacementDocumentReader interface {
	ListPersonaAdminPlacementDocuments(context.Context, string, string, string) ([]PersonaAdminPlacementDocument, error)
}

// Snapshot returns an authorized, tenant-bound catalog without task content.
func (s *PersonaAdminCatalogService) Snapshot(ctx context.Context, req productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	if s == nil || s.Versions == nil {
		return productui.PersonaAdminSnapshot{}, personaCatalogStage("persona_store", ErrPersonaCatalogDenied)
	}
	p, tenant, err := s.authorize(ctx, req.TenantID, req.Principal)
	if err != nil {
		return productui.PersonaAdminSnapshot{}, personaCatalogStage("authorization", err)
	}
	out := productui.PersonaAdminSnapshot{Available: true, DocumentServiceAvailable: s.Documents != nil}
	out.DocumentsState.Unavailable = s.Documents == nil
	var users, conversations []productui.PersonaAdminTarget
	if s.Targets == nil {
		out.TargetsState.Unavailable = true
	} else {
		if detailed, ok := s.Targets.(personaCatalogTargetStateReader); ok {
			var targetState productui.PersonaAdminRegionState
			users, conversations, targetState, err = detailed.ListPersonaCatalogTargetsWithState(ctx, *p, tenant)
			out.TargetsState.Omitted += targetState.Omitted
		} else {
			users, conversations, err = s.Targets.ListPersonaCatalogTargets(ctx, *p, tenant)
		}
		if err != nil {
			out.TargetsState.Unavailable = true
		} else {
			var omitted int
			users, omitted = sanitizePersonaCatalogTargets(users)
			out.TargetsState.Omitted += omitted
			conversations, omitted = sanitizePersonaCatalogTargets(conversations)
			out.TargetsState.Omitted += omitted
			out.SubjectOptions = slices.Clone(users)
			out.Conversations = slices.Clone(conversations)
		}
	}
	if s.Starters == nil {
		out.StartersState.Unavailable = true
	} else {
		starters, starterErr := s.Starters.PersonaAdminStarterCatalog(ctx, req)
		if starterErr != nil || !starters.Available {
			out.StartersState.Unavailable = true
		} else {
			out.StarterCatalogAvailable = true
			out.Starters = slices.Clone(starters.Starters)
		}
	}
	versions, err := s.Versions.ListPersonaCatalogVersions(ctx, tenant)
	if err != nil {
		if errors.Is(err, ErrPersonaCatalogDenied) {
			return productui.PersonaAdminSnapshot{}, err
		}
		out.CatalogState.Unavailable = true
		return out, nil
	}
	var installs []PersonaCatalogInstallation
	if s.Installations == nil {
		out.CatalogState.Unavailable = true
	} else {
		if states, ok := s.Installations.(interface {
			ListPersonaCatalogInstallationStates(context.Context, values.TenantId) ([]PersonaCatalogInstallation, error)
		}); ok {
			installs, err = states.ListPersonaCatalogInstallationStates(ctx, tenant)
		} else {
			installs, err = s.Installations.ListPersonaCatalogInstallations(ctx, tenant)
		}
		if err != nil {
			out.CatalogState.Unavailable = true
			installs = nil
		}
	}
	installs = personaCatalogVisibleInstallations(installs)
	for i := range installs {
		installs[i] = ProjectPersonaCatalogInstallationRuntime(ctx, tenant, installs[i], s.Runtime)
	}
	byPersona := make(map[catalogPersonaVersionKey][]PersonaCatalogInstallation)
	for _, in := range installs {
		if !validPersonaCatalogInstallation(in) {
			out.CatalogState.Omitted++
			continue
		}
		if in.Active || in.State == string(agentpersonastore.InstallationSuspended) {
			key := catalogPersonaVersionKey{personaID: in.PersonaID, version: in.PersonaVersion}
			byPersona[key] = append(byPersona[key], in)
		}
	}
	history := map[string][]productui.PersonaAdminVersionHistory{}
	if reader, ok := s.Versions.(interface {
		ListPersonaAdminVersionHistory(context.Context, values.TenantId) (map[string][]productui.PersonaAdminVersionHistory, error)
	}); ok {
		history, err = reader.ListPersonaAdminVersionHistory(ctx, tenant)
		if err != nil {
			return out, personaCatalogStage("history", err)
		}
	}
	seenPersonas := make(map[string]struct{}, len(versions))
	for _, v := range versions {
		if v.Profile.Verify() != nil || v.Profile.Profile.PersonaID == "" || v.Profile.Profile.Version == 0 || !validLifecycle(v.Lifecycle) {
			out.CatalogState.Omitted++
			continue
		}
		profile := v.Profile.Profile
		if _, exists := seenPersonas[profile.PersonaID]; exists {
			return productui.PersonaAdminSnapshot{}, ErrPersonaCatalogDenied
		}
		seenPersonas[profile.PersonaID] = struct{}{}
		persona := productui.PersonaAdminPersona{ID: profile.PersonaID, Handle: profile.Handle, Name: profile.DisplayName, Purpose: profile.Purpose, Lifecycle: productui.PersonaAdminLifecycle(v.Lifecycle), Owner: v.Owner, Steward: v.Steward, Version: fmt.Sprint(profile.Version), Audience: audienceLabel(profile.Audience), ReviewRequired: v.ReviewRequired, ReviewApproved: v.ReviewApproved, Reviewer: v.Reviewer, EvaluationRef: v.EvaluationRef, ReviewApprovedAt: v.ReviewApprovedAt, EvaluationPassedAt: v.EvaluationPassedAt, Instructions: profile.Instructions, Limits: productui.PersonaAdminLimits{InvocationsPerHour: v.InvocationsPerHour, ConcurrentTasks: v.ConcurrentTasks, DailySpend: v.DailySpend}, WorkspaceDocuments: v.WorkspaceDocuments, WorkspacePending: v.WorkspacePending}
		if !v.WorkspaceIndexedAt.IsZero() {
			persona.WorkspaceIndexedAt = v.WorkspaceIndexedAt.UTC().Format(time.RFC3339)
		}
		applyAgentDocGuidanceProjection(&persona, profile)
		persona.Icon, persona.IconRevision = v.Icon, v.IconRevision
		persona.VersionHistory = history[persona.ID]
		for i := range persona.VersionHistory {
			row := &persona.VersionHistory[i]
			resolved, resolveErr := resolvePersonaAdminDocumentReferences(ctx, s.Documents, string(tenant), p.Subject(), row.DocumentReferences)
			if resolveErr != nil {
				out.DocumentsState.Unavailable = true
				row.Documents = nil
			} else {
				row.Documents = resolved
			}
			row.DocumentReferences = nil
			if row.Version == persona.Version {
				persona.PublishedAt = row.PublishedAt
				if row.ReviewApprovedAt != "" {
					persona.ReviewApprovedAt = row.ReviewApprovedAt
				}
				if row.EvaluationPassedAt != "" {
					persona.EvaluationPassedAt = row.EvaluationPassedAt
				}
			}
			if row.PublishedBy != "" {
				name := personaCatalogTargetName(users, row.PublishedBy)
				if name == "" {
					if projection, ok := s.Targets.(personaCatalogReadableProjection); ok {
						if person, e := projection.ResolvePersonaCatalogTarget(ctx, tenant, row.PublishedBy); e == nil {
							name = person.Label
						}
					}
				}
				row.PublishedBy = name
			}
		}
		persona.OwnerName = personaCatalogTargetName(users, v.Owner)
		persona.StewardName = personaCatalogTargetName(users, v.Steward)
		persona.ReviewerName = personaCatalogTargetName(users, v.Reviewer)
		if projection, ok := s.Targets.(personaCatalogReadableProjection); ok {
			if persona.OwnerName == "" {
				if target, resolveErr := projection.ResolvePersonaCatalogTarget(ctx, tenant, v.Owner); resolveErr == nil {
					persona.OwnerName = target.Label
				}
			}
			if persona.StewardName == "" {
				if target, resolveErr := projection.ResolvePersonaCatalogTarget(ctx, tenant, v.Steward); resolveErr == nil {
					persona.StewardName = target.Label
				}
			}
			if persona.ReviewerName == "" && v.Reviewer != "" {
				if target, resolveErr := projection.ResolvePersonaCatalogTarget(ctx, tenant, v.Reviewer); resolveErr == nil {
					persona.ReviewerName = target.Label
				}
			}
			persona.AudienceRoles = projection.ResolvePersonaCatalogRoleTargets(ctx, *p, tenant, profile.Audience.Roles, profile.Audience.OrganizationScopes)
		} else {
			for _, role := range uniqueSorted(profile.Audience.Roles) {
				persona.AudienceRoles = append(persona.AudienceRoles, productui.PersonaAdminTarget{ID: role})
			}
		}
		persona.OwnerInitials = personaCatalogInitials(persona.OwnerName)
		persona.StewardInitials = personaCatalogInitials(persona.StewardName)
		persona.ReviewerInitials = personaCatalogInitials(persona.ReviewerName)
		for _, organization := range uniqueSorted(profile.Audience.OrganizationScopes) {
			persona.Organizations = append(persona.Organizations, productui.PersonaAdminTarget{ID: organization})
		}
		persona.DocumentReferences, err = resolvePersonaAdminDocumentReferences(ctx, s.Documents, string(tenant), p.Subject(), profile.DocumentReferences)
		if err != nil {
			out.DocumentsState.Unavailable = true
			persona.DocumentReferences = nil
		}
		persona.ChannelClasses = personaCatalogChannelClasses(profile.ChannelClasses)
		if starter, ok := personaCatalogStarter(profile); ok {
			persona.StarterID = starter.ID
			persona.StarterVersion = starter.Version
			if strings.HasSuffix(starter.EvaluationSuite, ".policy_helper") && len(starter.SkillPins) > 0 {
				persona.EvaluationCaseCount = len(agenteval.PolicyHelperSuite(starter.SkillPins[0].ID).Cases)
			} else if starter.ID == localAgentDemoAssistantStarterID && starter.Version >= 2 {
				persona.EvaluationCaseCount = len(agenteval.AssistantWorkspaceSuite(personaPolicyHelperSkillID, personaWorkspaceSearchSkillID, personaChatReplySkillID).Cases)
			} else if starter.ID == localAgentDemoAssistantStarterID {
				persona.EvaluationCaseCount = len(agenteval.AssistantSuite(personaPolicyHelperSkillID, personaChatReplySkillID).Cases)
			}
		}
		if s.Skills == nil {
			out.CatalogState.Unavailable = true
		}
		for _, pin := range profile.SkillPins {
			if s.Skills == nil {
				out.CatalogState.Omitted++
				continue
			}
			record, e := s.Skills.ResolvePin(pin)
			if e != nil {
				out.CatalogState.Omitted++
				continue
			}
			tier := max(record.Definition.SideEffectTier, record.HighestCapabilityTier)
			data := uniqueSorted(append(slices.Clone(record.Definition.DataClassesRead), record.Definition.DataClassesWritten...))
			persona.Skills = append(persona.Skills, productui.PersonaAdminSkill{ID: pin.ID, Name: pin.ID, Description: record.Definition.Description, Tier: tier.String(), DataClasses: data})
			persona.DerivedData = append(persona.DerivedData, data...)
		}
		persona.DerivedData = uniqueSorted(persona.DerivedData)
		// A draft or in-review version is not what people use. The version
		// with active placements is the live one: name it on the card and
		// keep listing its placements, so creating version 5 does not make
		// version 4's conversations disappear from the page.
		if liveVersion := personaCatalogLiveVersion(installs, persona.ID); liveVersion != 0 && liveVersion != profile.Version {
			persona.LiveVersion = fmt.Sprint(liveVersion)
		}
		for _, in := range installs {
			if in.PersonaID != persona.ID || !validPersonaCatalogInstallation(in) {
				continue
			}
			target, ok := personaCatalogTarget(conversations, in.ConversationID)
			if !ok {
				out.CatalogState.Omitted++
				continue
			}
			conversation := target.Label
			if strings.TrimSpace(target.PlacementLabel) != "" {
				conversation = target.PlacementLabel
			}
			installation := productui.PersonaAdminInstallation{InstallationID: in.ID, Version: fmt.Sprint(in.PersonaVersion), ConversationID: in.ConversationID, Conversation: conversation, Kind: target.Kind, Audience: in.Audience, ReplyPlacement: in.ReplyPlacement}
			if documents, ok := s.Documents.(PersonaAdminPlacementDocumentReader); ok {
				rows, readErr := documents.ListPersonaAdminPlacementDocuments(ctx, string(tenant), p.Subject(), in.ConversationID)
				if readErr != nil {
					out.DocumentsState.Unavailable = true
				} else {
					count := len(rows)
					installation.OfficialDocumentCount = &count
					for _, row := range rows {
						if strings.TrimSpace(row.Title) != "" {
							installation.OfficialDocumentTitles = append(installation.OfficialDocumentTitles, row.Title)
						}
					}
				}
			}
			persona.Installations = append(persona.Installations, installation)
		}
		out.Personas = append(out.Personas, persona)
	}
	if s.Targets != nil && s.Installations != nil && s.Skills != nil && s.Grants != nil && len(users) > 0 && len(conversations) > 0 {
		for _, version := range versions {
			if version.Lifecycle != agentpersona.StatePublished {
				continue
			}
			profile := version.Profile.Profile
			for _, installation := range byPersona[catalogPersonaVersionKey{personaID: profile.PersonaID, version: profile.Version}] {
				if !installation.Active || !targetContains(conversations, installation.ConversationID) {
					continue
				}
				preview, previewErr := s.preview(ctx, p, tenant, versions, profile.PersonaID, users[0].ID, installation.ConversationID)
				if previewErr == nil {
					out.Preview = preview
					return out, nil
				}
				out.CatalogState.Omitted++
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

func personaCatalogSuspensionMessage(reason string) string {
	switch strings.TrimSpace(reason) {
	case "AGENT_PRINCIPAL_MISSING_AFTER_RESTORE":
		return "this version has no runtime identity"
	case "AGENT_PRINCIPAL_RETIRED_AFTER_RESTORE":
		return "this version's runtime identity is no longer active"
	case "PERSONA_PUBLICATION_MISSING_AFTER_RESTORE":
		return "this version is no longer published"
	case agentpersonastore.MissingVersionAfterRestore:
		return "this version is no longer available"
	case "":
		return ""
	default:
		return "this installation needs attention from the person who manages it"
	}
}

func personaCatalogStarter(profile agentpersona.PersonaProfile) (agenttemplate.PersonaStarter, bool) {
	if strings.TrimSpace(profile.EvalSuiteRef) == "" {
		return agenttemplate.PersonaStarter{}, false
	}
	var match agenttemplate.PersonaStarter
	// Assistant's second starter image is kept beside, not inside, the starter
	// catalog so the reviewed first image stays byte for byte as it was.
	starters := agenttemplate.PersonaStarters()
	if profile.Template != nil {
		starters = append(starters, AssistantWorkspaceStarter())
	}
	for _, starter := range starters {
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
	if s.Versions == nil || s.Targets == nil || s.Installations == nil || s.Skills == nil || s.Grants == nil {
		return productui.PersonaAdminPreview{}, errPersonaCatalogSource
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
	profileData := uniqueSorted(append(slices.Clone(selected.Profile.Profile.DataClassesRead), selected.Profile.Profile.DataClassesWritten...))
	if !stringSetSubset(profileData, installation.AllowedDataClasses) {
		return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
	}
	for _, pin := range selected.Profile.Profile.SkillPins {
		record, err := s.Skills.ResolvePin(pin)
		if err != nil {
			return productui.PersonaAdminPreview{}, fmt.Errorf("resolve preview pinned skill: %w", err)
		}
		pinnedTier := max(record.Definition.SideEffectTier, record.HighestCapabilityTier)
		pinnedData := uniqueSorted(append(slices.Clone(record.Definition.DataClassesRead), record.Definition.DataClassesWritten...))
		if !pinnedTier.Valid() || pinnedTier > selected.Profile.Profile.TierCeiling || pinnedTier > installation.MaxTier {
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
		if !validTier || grantedTier > pinnedTier || grantedTier > installation.MaxTier || !stringSetSubset(g.DataClasses, pinnedData) {
			return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
		}
		preview.EffectiveSkills = append(preview.EffectiveSkills, productui.PersonaAdminSkill{ID: pin.ID, Name: pin.ID, Description: record.Definition.Description, Tier: g.Tier, DataClasses: slices.Clone(g.DataClasses)})
		preview.DerivedData = append(preview.DerivedData, g.DataClasses...)
	}
	preview.DerivedData = uniqueSorted(preview.DerivedData)
	if len(preview.EffectiveSkills) == 0 {
		return preview, nil
	}
	preview.ReplyPlacement = installation.ReplyPlacement
	if documents, ok := s.Documents.(PersonaAdminPlacementDocumentReader); ok {
		allRows, allErr := documents.ListPersonaAdminPlacementDocuments(ctx, string(tenant), p.Subject(), conversationID)
		rows := allRows
		readErr := allErr
		if subjectID != p.Subject() {
			rows, readErr = documents.ListPersonaAdminPlacementDocuments(ctx, string(tenant), subjectID, conversationID)
		}
		if readErr == nil {
			count := len(rows)
			preview.OfficialDocumentCount = &count
			for _, row := range rows {
				if strings.TrimSpace(row.Title) != "" {
					preview.OfficialDocumentTitles = append(preview.OfficialDocumentTitles, row.Title)
				}
			}
			if allErr == nil && len(allRows) > len(rows) {
				preview.UnreadableDocumentCount = len(allRows) - len(rows)
			}
		}
	}
	return preview, nil
}

func (s *PersonaAdminCatalogService) authorize(ctx context.Context, tenantID, principalID string) (*trust.Principal, values.TenantId, error) {
	if s == nil || ctx == nil || s.Authorizer == nil {
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
func personaCatalogTargetName(ts []productui.PersonaAdminTarget, id string) string {
	for _, target := range ts {
		if target.ID == id {
			return target.Label
		}
	}
	return ""
}
func personaCatalogInitials(name string) string {
	parts := strings.Fields(strings.TrimSpace(name))
	if len(parts) == 0 {
		return ""
	}
	first := []rune(parts[0])
	initials := []rune{first[0]}
	if len(parts) > 1 {
		last := []rune(parts[len(parts)-1])
		initials = append(initials, last[0])
	}
	return strings.ToUpper(string(initials))
}
func personaCatalogTarget(ts []productui.PersonaAdminTarget, id string) (productui.PersonaAdminTarget, bool) {
	for _, target := range ts {
		if target.ID == id {
			return target, true
		}
	}
	return productui.PersonaAdminTarget{}, false
}
func sanitizePersonaCatalogTargets(targets []productui.PersonaAdminTarget) ([]productui.PersonaAdminTarget, int) {
	out := make([]productui.PersonaAdminTarget, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	omitted := 0
	for _, target := range targets {
		if strings.TrimSpace(target.ID) == "" || strings.TrimSpace(target.Label) == "" {
			omitted++
			continue
		}
		if _, exists := seen[target.ID]; exists {
			omitted++
			continue
		}
		seen[target.ID] = struct{}{}
		out = append(out, target)
	}
	return out, omitted
}
func validPersonaCatalogInstallation(installation PersonaCatalogInstallation) bool {
	return strings.TrimSpace(installation.ID) != "" && strings.TrimSpace(installation.PersonaID) != "" && installation.PersonaVersion > 0 &&
		strings.TrimSpace(installation.ConversationID) != "" && installation.MaxTier.Valid() &&
		strings.TrimSpace(installation.ChannelClass) != "" && strings.TrimSpace(installation.ConversationKind) != ""
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

// personaCatalogLiveVersion returns the highest version of a persona that has
// an active placement, or zero when it is added nowhere.
func personaCatalogLiveVersion(installs []PersonaCatalogInstallation, personaID string) uint32 {
	var live uint32
	for _, in := range installs {
		if in.Active && in.PersonaID == personaID && validPersonaCatalogInstallation(in) && in.PersonaVersion > live {
			live = in.PersonaVersion
		}
	}
	return live
}

// ListPersonaAdminVersionHistory projects each immutable version in the same tenant catalog.
func (r personaAdminCatalogVersions) ListPersonaAdminVersionHistory(ctx context.Context, tenant values.TenantId) (map[string][]productui.PersonaAdminVersionHistory, error) {
	entries, err := listPersonaAdminCatalog(ctx, r.store, tenant)
	if err != nil {
		return nil, err
	}
	scoped, err := r.store.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := map[string][]productui.PersonaAdminVersionHistory{}
	for _, entry := range entries {
		profile, err := decodeAvailableProfile(entry.Version)
		if err != nil {
			return nil, err
		}
		projected := productui.PersonaAdminPersona{}
		applyAgentDocGuidanceProjection(&projected, profile.Profile)
		row := productui.PersonaAdminVersionHistory{Version: fmt.Sprint(entry.Version.Version), Lifecycle: productui.PersonaAdminLifecycle(entry.Lifecycle), Instructions: profile.Profile.Instructions, Guidance: projected.Guidance, DocumentReferences: profile.Profile.DocumentReferences}
		for _, in := range entry.Installations {
			if in.State == string(agentpersonastore.InstallationActive) {
				row.ConversationCount++
			}
		}
		if events, ok := scoped.(interface {
			ListLifecycle(context.Context, string, int64) ([]agentpersonastore.LifecycleEvent, error)
		}); ok {
			history, e := events.ListLifecycle(ctx, entry.Version.PersonaID, entry.Version.Version)
			if e != nil {
				return nil, e
			}
			for _, event := range history {
				if event.To == agentpersonastore.StatePublished {
					row.PublishedAt = event.OccurredAt.UTC().Format(time.RFC3339)
					row.PublishedBy = event.ActorID
				}
			}
		}
		if details, ok := scoped.(interface {
			ReadPublicationEvidenceDetails(context.Context, string, int64) (agentpersonastore.PublicationEvidenceDetails, error)
		}); ok && (entry.Lifecycle == agentpersonastore.StatePublished || entry.Lifecycle == agentpersonastore.StateSuspended) {
			if evidence, e := details.ReadPublicationEvidenceDetails(ctx, entry.Version.PersonaID, entry.Version.Version); e == nil {
				row.ReviewApprovedAt = evidence.ReviewApprovedAt.UTC().Format(time.RFC3339)
				row.EvaluationPassedAt = evidence.EvaluationPassedAt.UTC().Format(time.RFC3339)
			}
		}
		out[entry.Version.PersonaID] = append(out[entry.Version.PersonaID], row)
	}
	return out, nil
}
