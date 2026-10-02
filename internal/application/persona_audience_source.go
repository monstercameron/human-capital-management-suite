package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrPersonaAudienceDirectoryFactsMissing means the trusted directory did not
// assert complete routing facts for a current chat member. An empty or
// fabricated fact set must never make a persona appear invocable.
var ErrPersonaAudienceDirectoryFactsMissing = errors.New("persona audience: trusted directory facts missing")

var errPersonaAudienceSourceUnavailable = errors.New("persona audience: source unavailable")

// PersonaAudienceChatAuthority is the read-only chat authority used by the
// source. Implementations must return only current, tenant-scoped rows.
type PersonaAudienceChatAuthority interface {
	ListConversations(context.Context, chat.ListConversationsRequest) (chat.ListConversationsResponse, error)
	ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error)
}

// PersonaAudienceDirectory resolves current role, population, and
// organization facts for a subject in its owning tenant.
type PersonaAudienceDirectory interface {
	ResolvePersonaAudienceMember(context.Context, string, string) (PersonaAudienceMember, error)
}

// PersonaAudienceInstallationReader reads active placements and the exact
// published versions from the isolated persona database.
type PersonaAudienceInstallationReader interface {
	ListActiveByConversation(context.Context, string) ([]agentpersonastore.ActiveInstallation, error)
	ListPublished(context.Context) ([]agentpersonastore.PersonaVersion, error)
	LookupPersonaChatIdentity(context.Context, string) (agentpersonastore.PersonaChatIdentity, error)
}

// PersonaAudienceInstallationStore scopes installation reads to one tenant.
type PersonaAudienceInstallationStore interface {
	ForTenant(context.Context, values.TenantId) (PersonaAudienceInstallationReader, error)
}

// DatabasePersonaAudienceSource composes the independent chat, persona, and
// directory authorities. It never joins their databases or trusts request
// supplied roles, populations, or organization scope.
type DatabasePersonaAudienceSource struct {
	Chat          PersonaAudienceChatAuthority
	Installations PersonaAudienceInstallationStore
	Directory     PersonaAudienceDirectory
}

var _ PersonaAudienceSource = (*DatabasePersonaAudienceSource)(nil)

// ListCurrentPersonaAudience lists every conversation the verified subject is
// currently a member of, projecting human members through the trusted
// directory. Installed persona chat identities are machine members, so they
// are intentionally excluded before any human-directory lookup.
func (s *DatabasePersonaAudienceSource) ListCurrentPersonaAudience(ctx context.Context, tenantID, subjectID string) ([]PersonaAudienceConversation, error) {
	verified, err := s.verify(ctx, tenantID, subjectID)
	if err != nil {
		return nil, err
	}
	if s.Chat == nil || s.Directory == nil {
		return nil, errPersonaAudienceSourceUnavailable
	}
	principal := chat.Principal{TenantID: string(verified.Tenant()), SubjectID: verified.Subject()}
	page := chat.Page{PageSize: 200}
	out := make([]PersonaAudienceConversation, 0)
	seenRooms := make(map[string]struct{})
	// What this read leaves out is counted and reported once at the end
	// (AGENTUX-033), not written once per member of every conversation.
	omitted := &personaAudienceOmissions{}
	defer func() { omitted.report(ctx) }()
	for {
		rooms, err := s.Chat.ListConversations(ctx, chat.ListConversationsRequest{Principal: principal, TenantID: tenantID, Page: page})
		if err != nil {
			return nil, fmt.Errorf("list current chat conversations: %w", err)
		}
		for _, room := range rooms.Conversations {
			if room.TenantID != tenantID || strings.TrimSpace(room.ID) == "" || room.Archived {
				omitted.conversation("invalid_conversation")
				continue
			}
			if _, exists := seenRooms[room.ID]; exists {
				omitted.conversation("duplicate_conversation")
				continue
			}
			seenRooms[room.ID] = struct{}{}
			members, err := s.listMembers(ctx, tenantID, room.ID, principal, omitted)
			if err != nil {
				if errors.Is(err, ErrPersonaAudienceDirectoryFactsMissing) {
					return nil, err
				}
				omitted.conversation("members_unavailable")
				slog.DebugContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "audience", "item_type", "conversation", "item_id", room.ID, "reason", "members_unavailable", "error_type", fmt.Sprintf("%T", err), "cause", personaAudienceOmissionCause(err))
				continue
			}
			out = append(out, PersonaAudienceConversation{TenantID: tenantID, ConversationID: room.ID, Members: members})
		}
		if rooms.NextCursor == "" {
			break
		}
		page.Cursor = rooms.NextCursor
	}
	return out, nil
}

// personaAudienceOmissionCause is the detail logged when a conversation is
// left out of the audience. A conversation silently left out makes its agents
// disappear for everyone in it, so the cause must be findable; the text can
// name people, so it is written only in an opted-in local diagnostic session.
func personaAudienceOmissionCause(err error) string {
	if err == nil || os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") != "1" {
		return "withheld"
	}
	return err.Error()
}

func (s *DatabasePersonaAudienceSource) listMembers(ctx context.Context, tenantID, conversationID string, principal chat.Principal, omitted *personaAudienceOmissions) ([]PersonaAudienceMember, error) {
	page := chat.Page{PageSize: 200}
	out := make([]PersonaAudienceMember, 0)
	seen := make(map[string]struct{})
	for {
		members, err := s.Chat.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: principal, TenantID: tenantID, ConversationID: conversationID, Page: page})
		if err != nil {
			return nil, fmt.Errorf("list current chat members for %q: %w", conversationID, err)
		}
		for _, member := range members.Memberships {
			if member.TenantID != tenantID || member.ConversationID != conversationID || strings.TrimSpace(member.SubjectID) == "" || strings.TrimSpace(member.HomeTenantID) == "" {
				omitted.member("invalid_membership")
				continue
			}
			key := member.HomeTenantID + "\x00" + member.SubjectID
			if _, ok := seen[key]; ok {
				omitted.member("duplicate_membership")
				continue
			}
			seen[key] = struct{}{}
			agent, err := s.isPersonaChatIdentity(ctx, tenantID, member.HomeTenantID, member.SubjectID)
			if err != nil {
				return nil, err
			}
			if agent {
				continue
			}
			facts, err := s.Directory.ResolvePersonaAudienceMember(ctx, member.HomeTenantID, member.SubjectID)
			if err != nil {
				if member.HomeTenantID == principal.TenantID && member.SubjectID == principal.SubjectID {
					return nil, fmt.Errorf("%w: resolve current principal %s: %v", ErrPersonaAudienceDirectoryFactsMissing, key, err)
				}
				omitted.member("directory_facts_missing")
				continue
			}
			if facts.SubjectID != member.SubjectID || !nonemptyFacts(facts.Roles) || !nonemptyFacts(facts.Populations) || strings.TrimSpace(facts.OrganizationScope) == "" {
				if member.HomeTenantID == principal.TenantID && member.SubjectID == principal.SubjectID {
					return nil, fmt.Errorf("%w: current principal %s in tenant %s", ErrPersonaAudienceDirectoryFactsMissing, member.SubjectID, member.HomeTenantID)
				}
				omitted.member("directory_facts_incomplete")
				continue
			}
			facts.Roles = append([]string(nil), facts.Roles...)
			facts.Populations = append([]string(nil), facts.Populations...)
			out = append(out, facts)
		}
		if members.NextCursor == "" {
			break
		}
		page.Cursor = members.NextCursor
	}
	if _, ok := seen[principal.TenantID+"\x00"+principal.SubjectID]; !ok {
		return nil, fmt.Errorf("%w: current principal membership missing", ErrPersonaAudienceDirectoryFactsMissing)
	}
	return out, nil
}

func (s *DatabasePersonaAudienceSource) isPersonaChatIdentity(ctx context.Context, tenantID, homeTenantID, subjectID string) (bool, error) {
	if s.Installations == nil || tenantID != homeTenantID {
		return false, nil
	}
	store, err := s.Installations.ForTenant(ctx, values.TenantId(tenantID))
	if err != nil || store == nil {
		return false, fmt.Errorf("%w: scope persona identities", errPersonaAudienceSourceUnavailable)
	}
	identity, err := store.LookupPersonaChatIdentity(ctx, subjectID)
	if errors.Is(err, agentpersonastore.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: resolve persona identity: %v", errPersonaAudienceSourceUnavailable, err)
	}
	if identity.TenantID.String() != tenantID || identity.AgentID != subjectID || strings.TrimSpace(identity.PersonaID) == "" {
		return false, fmt.Errorf("%w: invalid persona identity", errPersonaAudienceSourceUnavailable)
	}
	return identity.Active, nil
}

func nonemptyFacts(items []string) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			return false
		}
	}
	return true
}

// ListCurrentPersonaInstallations lists active placements whose exact persona
// version remains published. Publication never silently upgrades an installation.
func (s *DatabasePersonaAudienceSource) ListCurrentPersonaInstallations(ctx context.Context, tenantID, conversationID string) ([]PersonaAudienceInstallation, error) {
	verified, err := s.verify(ctx, tenantID, "")
	if err != nil || s.Installations == nil {
		return nil, errPersonaAudienceSourceUnavailable
	}
	if strings.TrimSpace(conversationID) == "" {
		return nil, errPersonaAudienceSourceUnavailable
	}
	store, err := s.Installations.ForTenant(ctx, values.TenantId(verified.Tenant()))
	if err != nil {
		return nil, fmt.Errorf("scope persona installation store: %w", err)
	}
	if store == nil {
		return nil, errPersonaAudienceSourceUnavailable
	}
	published, err := store.ListPublished(ctx)
	if err != nil {
		return nil, fmt.Errorf("list published persona versions: %w", err)
	}
	type publishedKey struct {
		persona string
		version int64
	}
	current := make(map[publishedKey]bool, len(published))
	for _, version := range published {
		if version.TenantID.String() != tenantID || strings.TrimSpace(version.PersonaID) == "" || version.Version <= 0 {
			return nil, errPersonaAudienceSourceUnavailable
		}
		current[publishedKey{version.PersonaID, version.Version}] = true
	}
	active, err := store.ListActiveByConversation(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list active persona installations for %q: %w", conversationID, err)
	}
	out := make([]PersonaAudienceInstallation, 0, len(active))
	for _, item := range active {
		if item.ConversationID != conversationID || !current[publishedKey{item.PersonaID, item.PersonaVersion}] || item.InstallationID == "" || item.PersonaID == "" || item.PersonaVersion <= 0 {
			continue
		}
		out = append(out, PersonaAudienceInstallation{Tuple: agentpersonastore.AvailableInstallation{PersonaID: item.PersonaID, PersonaVersion: item.PersonaVersion, InstallationID: item.InstallationID, ConversationID: item.ConversationID}, Active: true, CurrentVersion: true, AudienceRoles: append([]string(nil), item.Audience.Roles...), AudiencePopulations: append([]string(nil), item.Audience.Populations...), AudienceScopes: append([]string(nil), item.Audience.OrganizationScope...)})
	}
	return out, nil
}

func (s *DatabasePersonaAudienceSource) verify(ctx context.Context, tenantID, subjectID string) (*trust.Principal, error) {
	if s == nil || ctx == nil || strings.TrimSpace(tenantID) == "" {
		return nil, errPersonaAudienceSourceUnavailable
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant().String() != tenantID || (subjectID != "" && verified.Subject() != subjectID) {
		return nil, errPersonaAudienceSourceUnavailable
	}
	return verified, nil
}
