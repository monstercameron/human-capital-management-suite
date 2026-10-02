package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var errPersonaCatalogSource = errors.New("application: persona catalog authority source unavailable")

// PersonaCatalogDirectory resolves a named, current directory subject after
// the chat authority has established that the subject is visible in a room.
// It must never return task content or caller supplied role claims.
type PersonaCatalogDirectory interface {
	ResolvePersonaCatalogTarget(context.Context, values.TenantId, string) (productui.PersonaAdminTarget, error)
}

// PersonaCatalogChatReader is the authorized chat read surface used to find
// preview rooms and their current members.
type PersonaCatalogChatReader interface {
	ListConversations(context.Context, chat.ListConversationsRequest) (chat.ListConversationsResponse, error)
	ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error)
}

// ChatDirectoryPersonaCatalogTargets derives both target lists from current
// authorized chat state. A directory lookup only enriches exact members that
// the chat service has already disclosed; it is never used to enumerate users.
type ChatDirectoryPersonaCatalogTargets struct {
	Chat      PersonaCatalogChatReader
	Directory PersonaCatalogDirectory
	Roles     AgentRoleDirectory
	RoleNames roleaccess.Store
}

// ListPersonaCatalogTargets returns visible members and rooms for the trusted
// principal. Missing authority, malformed rows, and cross-tenant data fail
// closed.
func (s *ChatDirectoryPersonaCatalogTargets) ListPersonaCatalogTargets(ctx context.Context, principal trust.Principal, tenant values.TenantId) ([]productui.PersonaAdminTarget, []productui.PersonaAdminTarget, error) {
	users, rooms, _, err := s.ListPersonaCatalogTargetsWithState(ctx, principal, tenant)
	return users, rooms, err
}

// ListPersonaCatalogTargetsWithState returns a request-local omission count so
// malformed or unresolvable items remain fail-closed without hiding that the
// projected target set is incomplete.
func (s *ChatDirectoryPersonaCatalogTargets) ListPersonaCatalogTargetsWithState(ctx context.Context, principal trust.Principal, tenant values.TenantId) ([]productui.PersonaAdminTarget, []productui.PersonaAdminTarget, productui.PersonaAdminRegionState, error) {
	state := productui.PersonaAdminRegionState{}
	if s == nil || s.Chat == nil || s.Directory == nil || ctx == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant() != tenant || tenant.Validate() != nil || strings.TrimSpace(principal.Subject()) == "" {
		return nil, nil, state, personaCatalogStage("target_authority", errPersonaCatalogSource)
	}
	chatPrincipal := chat.Principal{TenantID: string(tenant), SubjectID: principal.Subject()}
	rooms, omitted, err := s.listRooms(ctx, chatPrincipal, tenant)
	state.Omitted += omitted
	if err != nil {
		return nil, nil, state, personaCatalogStage("targets", err)
	}
	users := make([]productui.PersonaAdminTarget, 0)
	visibleRooms := make([]chat.Conversation, 0, len(rooms))
	seenUsers := make(map[string]struct{})
	resolvedUsers := make(map[string]productui.PersonaAdminTarget)
	humansByRoom := make(map[string][]productui.PersonaAdminTarget)
	for _, room := range rooms {
		members, omitted, err := s.listMembers(ctx, chatPrincipal, tenant, room.ID)
		state.Omitted += omitted
		if err != nil {
			state.Omitted++
			slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "conversation", "item_id", room.ID, "reason", "members_unavailable")
			continue
		}
		visibleRooms = append(visibleRooms, room)
		for _, member := range members {
			key := member.HomeTenantID + "\x00" + member.SubjectID
			if _, ok := seenUsers[key]; ok {
				if target, resolved := resolvedUsers[key]; resolved {
					humansByRoom[room.ID] = append(humansByRoom[room.ID], target)
				}
				continue
			}
			target, err := s.Directory.ResolvePersonaCatalogTarget(ctx, values.TenantId(member.HomeTenantID), member.SubjectID)
			if err != nil {
				state.Omitted++
				slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "member", "item_id", member.SubjectID, "reason", "directory_target_unavailable")
				continue
			}
			if target.ID != member.SubjectID {
				state.Omitted++
				slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "member", "item_id", member.SubjectID, "reason", "directory_identity_mismatch")
				continue
			}
			if strings.TrimSpace(target.Label) == "" {
				state.Omitted++
				slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "member", "item_id", member.SubjectID, "reason", "directory_label_missing")
				continue
			}
			if values.TenantId(member.HomeTenantID) != tenant {
				state.Omitted++
				slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "member", "item_id", member.SubjectID, "reason", "cross_tenant_member")
				continue
			}
			if s.Roles != nil {
				roles, roleErr := s.Roles.CurrentRoles(ctx, tenant, member.SubjectID)
				if roleErr != nil || len(roles) == 0 {
					state.Omitted++
					slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "member", "item_id", member.SubjectID, "reason", "roles_unavailable")
					continue
				}
				target.Role = personaCatalogPrimaryRole(roles)
			}
			seenUsers[key] = struct{}{}
			resolvedUsers[key] = target
			humansByRoom[room.ID] = append(humansByRoom[room.ID], target)
			users = append(users, target)
		}
	}
	return users, roomsToTargets(visibleRooms, humansByRoom, principal.Subject()), state, nil
}

func (s *ChatDirectoryPersonaCatalogTargets) listRooms(ctx context.Context, principal chat.Principal, tenant values.TenantId) ([]chat.Conversation, int, error) {
	var out []chat.Conversation
	omitted := 0
	page := chat.Page{PageSize: 200}
	seen := map[string]struct{}{}
	for {
		// Preview targets are rooms whose current membership the caller can
		// inspect. Discoverable public rooms are joinable, but the caller may
		// not yet have read access to their membership list; asking for those
		// rooms here would make the entire catalog snapshot fail.
		response, err := s.Chat.ListConversations(ctx, chat.ListConversationsRequest{Principal: principal, TenantID: string(tenant), Page: page})
		if err != nil {
			return nil, omitted, personaCatalogStage("target_rooms", fmt.Errorf("%w: list conversations: %v", errPersonaCatalogSource, err))
		}
		for _, room := range response.Conversations {
			if room.TenantID != string(tenant) || strings.TrimSpace(room.ID) == "" || strings.TrimSpace(room.Name) == "" || room.Archived {
				omitted++
				slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "conversation", "item_id", room.ID, "reason", "not_previewable")
				continue
			}
			if _, ok := seen[room.ID]; ok {
				omitted++
				slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "conversation", "item_id", room.ID, "reason", "duplicate_conversation")
				continue
			}
			seen[room.ID] = struct{}{}
			out = append(out, room)
		}
		if response.NextCursor == "" {
			return out, omitted, nil
		}
		page.Cursor = response.NextCursor
	}
}

func (s *ChatDirectoryPersonaCatalogTargets) listMembers(ctx context.Context, principal chat.Principal, tenant values.TenantId, conversationID string) ([]chat.Membership, int, error) {
	var out []chat.Membership
	omitted := 0
	page := chat.Page{PageSize: 200}
	seen := map[string]struct{}{}
	for {
		response, err := s.Chat.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: principal, TenantID: string(tenant), ConversationID: conversationID, Page: page})
		if err != nil {
			return nil, omitted, personaCatalogStage("target_memberships", fmt.Errorf("%w: list memberships: %v", errPersonaCatalogSource, err))
		}
		for _, member := range response.Memberships {
			if member.TenantID != string(tenant) || member.ConversationID != conversationID || strings.TrimSpace(member.HomeTenantID) == "" || strings.TrimSpace(member.SubjectID) == "" || member.LeftAt != nil {
				omitted++
				slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "member", "item_id", member.SubjectID, "reason", "invalid_membership")
				continue
			}
			key := member.HomeTenantID + "\x00" + member.SubjectID
			if _, ok := seen[key]; ok {
				omitted++
				slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "persona_admin", "item_type", "member", "item_id", member.SubjectID, "reason", "duplicate_membership")
				continue
			}
			seen[key] = struct{}{}
			out = append(out, member)
		}
		if response.NextCursor == "" {
			return out, omitted, nil
		}
		page.Cursor = response.NextCursor
	}
}

func roomsToTargets(rooms []chat.Conversation, humansByRoom map[string][]productui.PersonaAdminTarget, viewer string) []productui.PersonaAdminTarget {
	out := make([]productui.PersonaAdminTarget, 0, len(rooms))
	for _, room := range rooms {
		target := productui.PersonaAdminTarget{ID: room.ID, Label: room.Name, Kind: string(room.Kind)}
		if room.Kind == chat.Direct {
			humans := append([]productui.PersonaAdminTarget(nil), humansByRoom[room.ID]...)
			slices.SortFunc(humans, func(a, b productui.PersonaAdminTarget) int { return strings.Compare(a.Label, b.Label) })
			for _, human := range humans {
				if target.PlacementLabel == "" || human.ID == viewer {
					target.PlacementLabel = human.Label
				}
				if human.ID == viewer {
					target.ViewerDirect = true
				}
			}
		}
		out = append(out, target)
	}
	return out
}

func personaCatalogPrimaryRole(roles []string) string {
	roles = uniqueSorted(roles)
	for _, role := range roles {
		if roleaccess.IsAdministratorRole(role) {
			return role
		}
	}
	if len(roles) > 0 {
		return roles[0]
	}
	return ""
}

func (s *ChatDirectoryPersonaCatalogTargets) ResolvePersonaCatalogTarget(ctx context.Context, tenant values.TenantId, subject string) (productui.PersonaAdminTarget, error) {
	if s == nil || s.Directory == nil {
		return productui.PersonaAdminTarget{}, errPersonaCatalogSource
	}
	return s.Directory.ResolvePersonaCatalogTarget(ctx, tenant, subject)
}

func (s *ChatDirectoryPersonaCatalogTargets) ResolvePersonaCatalogRoleTargets(ctx context.Context, principal trust.Principal, tenant values.TenantId, roles, scopes []string) []productui.PersonaAdminTarget {
	names := make(map[string]string)
	for _, role := range roleaccess.DefaultRoles() {
		if role.Active {
			names[role.ID] = role.Name
		}
	}
	if s != nil && s.RoleNames != nil {
		readScopes := uniqueSorted(scopes)
		if len(readScopes) == 0 && principal.OrganizationScopeID() != "" {
			readScopes = []string{principal.OrganizationScopeID()}
		}
		for _, scope := range readScopes {
			snapshot, err := s.RoleNames.Load(ctx, tenant, scope)
			if err != nil {
				continue
			}
			for _, role := range snapshot.Roles {
				role = roleaccess.NormalizeRole(role)
				if role.Active && role.Name != "" {
					names[role.ID] = role.Name
				}
			}
		}
	}
	out := make([]productui.PersonaAdminTarget, 0, len(roles))
	for _, role := range uniqueSorted(roles) {
		out = append(out, productui.PersonaAdminTarget{ID: role, Label: names[role]})
	}
	return out
}

// PersonaCatalogSkillEvaluator is the narrow policy boundary used by the
// current-grant adapter. *agentgate.Gate satisfies this interface.
type PersonaCatalogSkillEvaluator interface {
	Authorize(context.Context, agentgate.CallRequest) (agentgate.CallDecision, error)
}

// PersonaCatalogPreviewContext resolves current user authority and exact
// subjects/fields for a preview purpose.
type PersonaCatalogPreviewContext interface {
	Resolve(context.Context, *trust.Principal, string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error)
}

// CurrentPersonaCatalogGrants evaluates the selected pin through the same
// current grant and capability gate used for execution. It never treats a
// request role or a missing evaluator as a grant.
type CurrentPersonaCatalogGrants struct {
	Evaluator PersonaCatalogSkillEvaluator
	Context   PersonaCatalogPreviewContext
	Skills    agentpersona.SkillResolver
	Chat      PersonaCatalogChatReader
	Purpose   string
	Now       func() time.Time
}

// ResolvePersonaSkillGrant returns an explicit allow only after the current
// gate authorizes the exact selected subject and conversation.
func (s *CurrentPersonaCatalogGrants) ResolvePersonaSkillGrant(ctx context.Context, principal trust.Principal, tenant values.TenantId, subjectID, conversationID string, pin agentskills.SkillPin) (PersonaCatalogGrant, error) {
	if s == nil || s.Evaluator == nil || s.Context == nil || s.Skills == nil || s.Chat == nil || ctx == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant() != tenant || tenant.Validate() != nil || strings.TrimSpace(subjectID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(s.Purpose) == "" {
		return PersonaCatalogGrant{}, errPersonaCatalogSource
	}
	record, err := s.Skills.ResolvePin(pin)
	if err != nil || record.Digest != pin.Digest || record.Status == agentskills.StatusRetired {
		return PersonaCatalogGrant{}, fmt.Errorf("%w: resolve pinned skill", errPersonaCatalogSource)
	}
	user, subjects, fields, err := s.Context.Resolve(ctx, &principal, s.Purpose)
	if err != nil || user.Principal == nil || user.Principal.Subject() != principal.Subject() || user.Principal.Tenant() != tenant || len(subjects) == 0 || len(fields) == 0 {
		return PersonaCatalogGrant{}, errPersonaCatalogSource
	}
	selected := make([]agentgate.Subject, 0, 1)
	for _, subject := range subjects {
		if subject.Ref.Id == subjectID && subject.Ref.Tenant == tenant {
			selected = append(selected, subject)
		}
	}
	if len(selected) != 1 {
		return PersonaCatalogGrant{Reason: "subject is outside current authority"}, nil
	}
	if err := s.verifyConversationMember(ctx, tenant, principal, subjectID, conversationID); err != nil {
		return PersonaCatalogGrant{Reason: "subject is not a current conversation member"}, nil
	}
	now := time.Time{}
	if s.Now != nil {
		now = s.Now().UTC()
	}
	if _, err := s.Evaluator.Authorize(ctx, agentgate.CallRequest{User: user, Actor: agentgate.AgentActor{AgentVersion: "persona-admin-preview", InstallationID: "persona-admin-preview", RunID: "persona-admin-preview", StepID: "persona-admin-preview"}, Skill: pin, Purpose: s.Purpose, Subjects: selected, Fields: slices.Clone(fields), At: now}); err != nil {
		return PersonaCatalogGrant{Reason: "current skill grant denied"}, nil
	}
	tier := max(record.Definition.SideEffectTier, record.HighestCapabilityTier)
	data := uniqueSorted(append(slices.Clone(record.Definition.DataClassesRead), record.Definition.DataClassesWritten...))
	return PersonaCatalogGrant{Allowed: true, Tier: tier.String(), DataClasses: data}, nil
}

func (s *CurrentPersonaCatalogGrants) verifyConversationMember(ctx context.Context, tenant values.TenantId, principal trust.Principal, subjectID, conversationID string) error {
	page := chat.Page{PageSize: 200}
	seen := map[string]struct{}{}
	for {
		response, err := s.Chat.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: chat.Principal{TenantID: string(tenant), SubjectID: principal.Subject()}, TenantID: string(tenant), ConversationID: conversationID, Page: page})
		if err != nil {
			return err
		}
		for _, member := range response.Memberships {
			if member.TenantID != string(tenant) || member.ConversationID != conversationID || member.HomeTenantID != string(tenant) || strings.TrimSpace(member.SubjectID) == "" || member.LeftAt != nil {
				return errPersonaCatalogSource
			}
			if _, duplicate := seen[member.SubjectID]; duplicate {
				return errPersonaCatalogSource
			}
			seen[member.SubjectID] = struct{}{}
			if member.SubjectID == subjectID {
				return nil
			}
		}
		if response.NextCursor == "" {
			return errPersonaCatalogSource
		}
		page.Cursor = response.NextCursor
	}
}

// TrustedPersonaCatalogAuthorizer delegates the page decision to the durable
// role-access authority. It has no fallback role, request-role, or admin
// bypass when the source is absent.
type TrustedPersonaCatalogAuthorizer struct{ Roles roleaccess.Store }

// AuthorizePersonaCatalog checks the explicit page grant for the verified
// principal and tenant.
func (a TrustedPersonaCatalogAuthorizer) AuthorizePersonaCatalog(ctx context.Context, principal *trust.Principal, tenant values.TenantId) error {
	if ctx == nil || a.Roles == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant() != tenant || tenant.Validate() != nil {
		return errPersonaCatalogSource
	}
	snapshot, err := a.Roles.Load(ctx, tenant, principal.OrganizationScopeID())
	if err != nil {
		return fmt.Errorf("%w: load page authority: %v", errPersonaCatalogSource, err)
	}
	roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
	permissions := roleaccess.EffectivePagePermissions(snapshot, roles)
	if !roleaccess.CanPageAction(permissions, string(productui.PagePersonaAdmin), roleaccess.ActionView) {
		return ErrPersonaCatalogDenied
	}
	return nil
}

var _ PersonaCatalogTargetReader = (*ChatDirectoryPersonaCatalogTargets)(nil)
var _ PersonaCatalogGrantReader = (*CurrentPersonaCatalogGrants)(nil)
var _ PersonaCatalogAuthorizer = TrustedPersonaCatalogAuthorizer{}
