package application

import (
	"context"
	"errors"
	"fmt"
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
}

// ListPersonaCatalogTargets returns visible members and rooms for the trusted
// principal. Missing authority, malformed rows, and cross-tenant data fail
// closed.
func (s *ChatDirectoryPersonaCatalogTargets) ListPersonaCatalogTargets(ctx context.Context, principal trust.Principal, tenant values.TenantId) ([]productui.PersonaAdminTarget, []productui.PersonaAdminTarget, error) {
	if s == nil || s.Chat == nil || s.Directory == nil || ctx == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant() != tenant || tenant.Validate() != nil || strings.TrimSpace(principal.Subject()) == "" {
		return nil, nil, personaCatalogStage("target_authority", errPersonaCatalogSource)
	}
	chatPrincipal := chat.Principal{TenantID: string(tenant), SubjectID: principal.Subject()}
	rooms, err := s.listRooms(ctx, chatPrincipal, tenant)
	if err != nil {
		return nil, nil, personaCatalogStage("targets", err)
	}
	users := make([]productui.PersonaAdminTarget, 0)
	seenUsers := make(map[string]struct{})
	for _, room := range rooms {
		members, err := s.listMembers(ctx, chatPrincipal, tenant, room.ID)
		if err != nil {
			return nil, nil, personaCatalogStage("targets", err)
		}
		for _, member := range members {
			key := member.HomeTenantID + "\x00" + member.SubjectID
			if _, ok := seenUsers[key]; ok {
				continue
			}
			target, err := s.Directory.ResolvePersonaCatalogTarget(ctx, values.TenantId(member.HomeTenantID), member.SubjectID)
			if err != nil {
				return nil, nil, personaCatalogStage("target_directory", fmt.Errorf("%w: resolve exact member: %w", errPersonaCatalogSource, err))
			}
			if target.ID != member.SubjectID {
				return nil, nil, personaCatalogStage("target_directory_identity", errPersonaCatalogSource)
			}
			if strings.TrimSpace(target.Label) == "" {
				return nil, nil, personaCatalogStage("target_directory_label", errPersonaCatalogSource)
			}
			if values.TenantId(member.HomeTenantID) != tenant {
				return nil, nil, personaCatalogStage("target_data", fmt.Errorf("%w: cross-tenant member", errPersonaCatalogSource))
			}
			seenUsers[key] = struct{}{}
			users = append(users, target)
		}
	}
	return users, roomsToTargets(rooms), nil
}

func (s *ChatDirectoryPersonaCatalogTargets) listRooms(ctx context.Context, principal chat.Principal, tenant values.TenantId) ([]chat.Conversation, error) {
	var out []chat.Conversation
	page := chat.Page{PageSize: 200}
	seen := map[string]struct{}{}
	for {
		// Preview targets are rooms whose current membership the caller can
		// inspect. Discoverable public rooms are joinable, but the caller may
		// not yet have read access to their membership list; asking for those
		// rooms here would make the entire catalog snapshot fail.
		response, err := s.Chat.ListConversations(ctx, chat.ListConversationsRequest{Principal: principal, TenantID: string(tenant), Page: page})
		if err != nil {
			return nil, personaCatalogStage("target_rooms", fmt.Errorf("%w: list conversations: %v", errPersonaCatalogSource, err))
		}
		for _, room := range response.Conversations {
			if room.TenantID != string(tenant) || strings.TrimSpace(room.ID) == "" || strings.TrimSpace(room.Name) == "" || room.Archived {
				return nil, personaCatalogStage("target_data", fmt.Errorf("%w: malformed conversation", errPersonaCatalogSource))
			}
			if _, ok := seen[room.ID]; ok {
				return nil, personaCatalogStage("target_data", fmt.Errorf("%w: duplicate conversation", errPersonaCatalogSource))
			}
			seen[room.ID] = struct{}{}
			out = append(out, room)
		}
		if response.NextCursor == "" {
			return out, nil
		}
		page.Cursor = response.NextCursor
	}
}

func (s *ChatDirectoryPersonaCatalogTargets) listMembers(ctx context.Context, principal chat.Principal, tenant values.TenantId, conversationID string) ([]chat.Membership, error) {
	var out []chat.Membership
	page := chat.Page{PageSize: 200}
	seen := map[string]struct{}{}
	for {
		response, err := s.Chat.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: principal, TenantID: string(tenant), ConversationID: conversationID, Page: page})
		if err != nil {
			return nil, personaCatalogStage("target_memberships", fmt.Errorf("%w: list memberships: %v", errPersonaCatalogSource, err))
		}
		for _, member := range response.Memberships {
			if member.TenantID != string(tenant) || member.ConversationID != conversationID || strings.TrimSpace(member.HomeTenantID) == "" || strings.TrimSpace(member.SubjectID) == "" || member.LeftAt != nil {
				return nil, personaCatalogStage("target_data", fmt.Errorf("%w: malformed membership", errPersonaCatalogSource))
			}
			key := member.HomeTenantID + "\x00" + member.SubjectID
			if _, ok := seen[key]; ok {
				return nil, personaCatalogStage("target_data", fmt.Errorf("%w: duplicate membership", errPersonaCatalogSource))
			}
			seen[key] = struct{}{}
			out = append(out, member)
		}
		if response.NextCursor == "" {
			return out, nil
		}
		page.Cursor = response.NextCursor
	}
}

func roomsToTargets(rooms []chat.Conversation) []productui.PersonaAdminTarget {
	out := make([]productui.PersonaAdminTarget, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, productui.PersonaAdminTarget{ID: room.ID, Label: room.Name})
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
