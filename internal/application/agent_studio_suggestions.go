package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrAgentStudioSuggestionUnavailable = errors.New("application: agent studio suggestion unavailable")
	ErrAgentStudioSuggestionStale       = errors.New("application: agent studio suggestion is stale")
	ErrAgentStudioSuggestionInvalid     = errors.New("application: invalid agent studio suggestion")
)

// AgentStudioSuggestionField is deliberately limited to presentation and
// bounded conversation fields. AI output can never propose grants, skills,
// audience, data reach, tier, instructions, or evaluation policy.
type AgentStudioSuggestionField string

const (
	AgentStudioFieldHandle         AgentStudioSuggestionField = "handle"
	AgentStudioFieldDisplayName    AgentStudioSuggestionField = "display_name"
	AgentStudioFieldPurpose        AgentStudioSuggestionField = "purpose"
	AgentStudioFieldChannelClasses AgentStudioSuggestionField = "channel_classes"
)

// AgentStudioSuggestionChange is inert model output. Reasons and uncertainty
// are retained in the request/receipt so the reviewer can understand each
// selected change; neither value grants authority.
//
// The JSON names are the wire contract of both the model output and the apply
// request: the names the model is asked for, and the same snake_case every
// other field of this surface uses.
type AgentStudioSuggestionChange struct {
	Field          AgentStudioSuggestionField  `json:"field"`
	Value          string                      `json:"value,omitempty"`
	ChannelClasses []agentpersona.ChannelClass `json:"channel_classes,omitempty"`
	Reason         string                      `json:"reason"`
	Uncertainty    float64                     `json:"uncertainty"`
}

// AgentStudioSuggestion is bound to the exact draft observed by the model.
type AgentStudioSuggestion struct {
	PersonaID string                        `json:"persona_id"`
	Revision  uint64                        `json:"revision"`
	Digest    string                        `json:"digest"`
	Changes   []AgentStudioSuggestionChange `json:"changes"`
}

type AgentStudioDraftSnapshot struct {
	PersonaID string
	Revision  uint64
	Version   agentpersonastore.PersonaVersion
	State     agentpersonastore.LifecycleState
}

// AgentStudioDraftSource reads the current tenant-scoped draft. Production
// implementations must resolve the tenant from the authenticated context.
type AgentStudioDraftSource interface {
	CurrentAgentStudioDraft(context.Context, string) (AgentStudioDraftSnapshot, error)
}

// AgentStudioDraftStoreSource adapts the existing tenant-scoped lifecycle
// store. The lifecycle event count is the optimistic revision; a deployment
// with a stronger draft revision may provide AgentStudioDraftSource directly.
type AgentStudioDraftStoreSource struct{ Store PersonaAdminLifecycleStore }

func (s AgentStudioDraftStoreSource) CurrentAgentStudioDraft(ctx context.Context, personaID string) (AgentStudioDraftSnapshot, error) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.Tenant().Validate() != nil || strings.TrimSpace(personaID) == "" {
		return AgentStudioDraftSnapshot{}, ErrAgentStudioSuggestionUnavailable
	}
	if s.Store == nil {
		return AgentStudioDraftSnapshot{}, ErrAgentStudioSuggestionUnavailable
	}
	tenant, err := s.Store.ForTenant(ctx, principal.Tenant())
	if err != nil || tenant == nil {
		return AgentStudioDraftSnapshot{}, ErrAgentStudioSuggestionUnavailable
	}
	versions, err := tenant.ListVersions(ctx, personaID)
	if err != nil {
		return AgentStudioDraftSnapshot{}, err
	}
	var selected agentpersonastore.PersonaVersion
	var revision uint64
	for _, version := range versions {
		if version.TenantID != principal.Tenant() || version.PersonaID != personaID || version.Version <= 0 {
			return AgentStudioDraftSnapshot{}, ErrAgentStudioSuggestionUnavailable
		}
		events, eventErr := tenant.ListLifecycle(ctx, personaID, version.Version)
		if eventErr != nil {
			return AgentStudioDraftSnapshot{}, eventErr
		}
		state, stateErr := tenant.Lifecycle(ctx, personaID, version.Version)
		if stateErr != nil {
			return AgentStudioDraftSnapshot{}, stateErr
		}
		if state == agentpersonastore.StateDraft && version.Version > selected.Version {
			selected, revision = version, uint64(len(events)+1)
		}
	}
	if selected.Version == 0 || revision == 0 {
		return AgentStudioDraftSnapshot{}, agentpersonastore.ErrNotFound
	}
	return AgentStudioDraftSnapshot{PersonaID: personaID, Revision: revision, Version: selected, State: agentpersonastore.StateDraft}, nil
}

type AgentStudioSuggestionService struct {
	Drafts     AgentStudioDraftSource
	Profiles   PersonaProfileBuilder
	Executor   PersonaAdminSuggestionExecutor
	Authorizer PersonaAdminCommandAuthorizer
}

// PersonaAdminSuggestionExecutor is the ordinary version/draft command seam.
// It is intentionally the same command path used by manual Agent Studio edits.
type PersonaAdminSuggestionExecutor interface {
	ExecutePersonaAdminCommand(context.Context, PersonaAdminCommandActor, PersonaAdminCommand) error
}

type AgentStudioAppliedChange struct {
	Field       AgentStudioSuggestionField
	Reason      string
	Uncertainty float64
}

type AgentStudioSuggestionReceipt struct {
	Draft   PersonaDraft
	Applied []AgentStudioAppliedChange
}

// AuthorizeSuggestion checks the same current admin grant used by version
// creation before a draft is read or sent to a model provider.
func (s *AgentStudioSuggestionService) AuthorizeSuggestion(ctx context.Context, personaID string) (PersonaAdminCommandActor, error) {
	if s == nil || ctx == nil || s.Authorizer == nil || strings.TrimSpace(personaID) != personaID || personaID == "" {
		return PersonaAdminCommandActor{}, ErrAgentStudioSuggestionUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().Validate() != nil {
		return PersonaAdminCommandActor{}, ErrAgentStudioSuggestionUnavailable
	}
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	if err := s.Authorizer.AuthorizePersonaAdminCommand(ctx, actor, PersonaAdminCreateVersion, personaID); err != nil {
		return PersonaAdminCommandActor{}, ErrPersonaDraftDenied
	}
	return actor, nil
}

// AgentStudioModelGenerator is the production SchemaFlux adapter. The model
// only returns the inert diff shape; the service below binds it to a draft
// revision and validates every selected field before any command is issued.
type AgentStudioModelGenerator struct {
	Gateway     *agentmodel.Gateway
	Skill       agentskills.SkillPin
	DataClasses []string
	Estimate    agentbudget.Usage
	Purpose     string
}

type agentStudioModelOutput struct {
	Changes []AgentStudioSuggestionChange `json:"changes"`
}

func (g *AgentStudioModelGenerator) Suggest(ctx context.Context, tenant string, actor agentmodel.ActorChain, draft AgentStudioDraftSnapshot, goal string) (AgentStudioSuggestion, agentmodel.Result[agentStudioModelOutput], error) {
	if g == nil || g.Gateway == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(goal) == "" || draft.PersonaID == "" || draft.Revision == 0 || draft.Version.ContentDigest == "" {
		return AgentStudioSuggestion{}, agentmodel.Result[agentStudioModelOutput]{}, ErrAgentStudioSuggestionUnavailable
	}
	if !json.Valid(draft.Version.Profile) {
		return AgentStudioSuggestion{}, agentmodel.Result[agentStudioModelOutput]{}, ErrAgentStudioSuggestionUnavailable
	}
	profile := draft.Version.Profile
	purpose := g.Purpose
	if purpose == "" {
		purpose = "agent_studio.suggest"
	}
	request := agentmodel.Request{TenantID: tenant, Actor: actor, Purpose: purpose, Skill: g.Skill, DataClasses: append([]string(nil), g.DataClasses...), Estimate: g.Estimate, MaxRetries: 1,
		Prompt: "Propose a reviewable refinement for this agent draft. Return only changes with field, value or channel_classes, reason, and uncertainty. Never propose skills, grants, audience, tier, data classes, instructions, evaluation, or publication authority.\nGoal: " + goal + "\nDraft JSON: " + string(profile)}
	result, err := agentmodel.Generate[agentStudioModelOutput](ctx, g.Gateway, request)
	if err != nil {
		return AgentStudioSuggestion{}, result, err
	}
	if err := validateStudioSuggestionChanges(result.Value.Changes); err != nil {
		return AgentStudioSuggestion{}, result, err
	}
	return AgentStudioSuggestion{PersonaID: draft.PersonaID, Revision: draft.Revision, Digest: draft.Version.ContentDigest, Changes: result.Value.Changes}, result, nil
}

func (s *AgentStudioSuggestionService) ApplySelected(ctx context.Context, suggestion AgentStudioSuggestion) (AgentStudioSuggestionReceipt, error) {
	if s == nil || ctx == nil || s.Drafts == nil || s.Profiles == nil || s.Executor == nil || s.Authorizer == nil {
		return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().Validate() != nil || strings.TrimSpace(principal.Subject()) == "" {
		return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionUnavailable
	}
	if strings.TrimSpace(suggestion.PersonaID) == "" || suggestion.Revision == 0 || strings.TrimSpace(suggestion.Digest) == "" || len(suggestion.Changes) == 0 {
		return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionInvalid
	}
	actor, err := s.AuthorizeSuggestion(ctx, suggestion.PersonaID)
	if err != nil {
		return AgentStudioSuggestionReceipt{}, err
	}
	current, err := s.Drafts.CurrentAgentStudioDraft(ctx, suggestion.PersonaID)
	if err != nil {
		return AgentStudioSuggestionReceipt{}, err
	}
	if current.PersonaID != suggestion.PersonaID || current.Version.TenantID != principal.Tenant() || current.Version.PersonaID != suggestion.PersonaID || current.Revision != suggestion.Revision || current.Version.ContentDigest != suggestion.Digest || current.State != agentpersonastore.StateDraft {
		return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionStale
	}
	var storedProfile agentpersona.PersonaProfile
	if err := json.Unmarshal(current.Version.Profile, &storedProfile); err != nil {
		return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionInvalid
	}
	profile := clonePersonaVersionProfileFromStudio(storedProfile)
	seen := make(map[AgentStudioSuggestionField]struct{}, len(suggestion.Changes))
	applied := make([]AgentStudioAppliedChange, 0, len(suggestion.Changes))
	for _, change := range suggestion.Changes {
		if _, exists := seen[change.Field]; exists || !validStudioChangeMetadata(change) {
			return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionInvalid
		}
		seen[change.Field] = struct{}{}
		switch change.Field {
		case AgentStudioFieldHandle:
			if change.Value == "" || !validPersonaHandle(change.Value) {
				return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionInvalid
			}
			profile.Handle = change.Value
		case AgentStudioFieldDisplayName:
			if change.Value == "" || len(change.Value) > 120 || !utf8ValidStudio(change.Value) || strings.TrimSpace(change.Value) != change.Value {
				return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionInvalid
			}
			profile.DisplayName = change.Value
		case AgentStudioFieldPurpose:
			if change.Value == "" || len(change.Value) > 1000 || !utf8ValidStudio(change.Value) || strings.TrimSpace(change.Value) != change.Value {
				return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionInvalid
			}
			profile.Purpose = change.Value
		case AgentStudioFieldChannelClasses:
			if len(change.ChannelClasses) == 0 || !studioChannelsNarrower(storedProfile.ChannelClasses, change.ChannelClasses) {
				return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionInvalid
			}
			profile.ChannelClasses = slices.Clone(change.ChannelClasses)
		default:
			return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionInvalid
		}
		applied = append(applied, AgentStudioAppliedChange{Field: change.Field, Reason: change.Reason, Uncertainty: change.Uncertainty})
	}
	if storedProfile.Version == ^uint32(0) {
		return AgentStudioSuggestionReceipt{}, ErrAgentStudioSuggestionInvalid
	}
	profile.Version++
	validated, err := buildPersonaProfile(ctx, s.Profiles, principal.Tenant(), profile)
	if err != nil {
		return AgentStudioSuggestionReceipt{}, fmt.Errorf("%w: profile validation failed: %v", ErrAgentStudioSuggestionInvalid, err)
	}
	if err := s.Executor.ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminCreateVersion, PersonaID: suggestion.PersonaID, Version: validated, BusinessOwnerID: profile.Owner, TechnicalStewardID: profile.Steward}); err != nil {
		return AgentStudioSuggestionReceipt{}, err
	}
	return AgentStudioSuggestionReceipt{Draft: PersonaDraft{PersonaID: profile.PersonaID, Version: profile.Version, Digest: validated.Digest, Lifecycle: string(agentpersonastore.StateDraft)}, Applied: applied}, nil
}

func validateStudioSuggestionChanges(changes []AgentStudioSuggestionChange) error {
	if len(changes) == 0 {
		return ErrAgentStudioSuggestionInvalid
	}
	seen := make(map[AgentStudioSuggestionField]struct{}, len(changes))
	for _, change := range changes {
		if _, exists := seen[change.Field]; exists || !validStudioChangeMetadata(change) {
			return ErrAgentStudioSuggestionInvalid
		}
		seen[change.Field] = struct{}{}
		switch change.Field {
		case AgentStudioFieldHandle:
			if change.Value == "" || !validPersonaHandle(change.Value) {
				return ErrAgentStudioSuggestionInvalid
			}
		case AgentStudioFieldDisplayName:
			if change.Value == "" || len(change.Value) > 120 || !utf8ValidStudio(change.Value) || strings.TrimSpace(change.Value) != change.Value {
				return ErrAgentStudioSuggestionInvalid
			}
		case AgentStudioFieldPurpose:
			if change.Value == "" || len(change.Value) > 1000 || !utf8ValidStudio(change.Value) || strings.TrimSpace(change.Value) != change.Value {
				return ErrAgentStudioSuggestionInvalid
			}
		case AgentStudioFieldChannelClasses:
			if len(change.ChannelClasses) == 0 {
				return ErrAgentStudioSuggestionInvalid
			}
		default:
			return ErrAgentStudioSuggestionInvalid
		}
	}
	return nil
}

func validStudioChangeMetadata(change AgentStudioSuggestionChange) bool {
	return strings.TrimSpace(change.Reason) != "" && change.Reason == strings.TrimSpace(change.Reason) && change.Uncertainty >= 0 && change.Uncertainty <= 1
}

func studioChannelsNarrower(current, proposed []agentpersona.ChannelClass) bool {
	if len(proposed) == 0 {
		return false
	}
	seen := make(map[agentpersona.ChannelClass]struct{}, len(proposed))
	for _, class := range proposed {
		if class == agentpersona.ChannelExternal {
			return false
		}
		if _, ok := seen[class]; ok {
			return false
		}
		seen[class] = struct{}{}
		if !slices.Contains(current, class) {
			return false
		}
	}
	return true
}

func clonePersonaVersionProfileFromStudio(profile agentpersona.PersonaProfile) agentpersona.PersonaProfile {
	return clonePersonaVersionProfile(profile)
}

func utf8ValidStudio(value string) bool { return strings.ToValidUTF8(value, "") == value }
