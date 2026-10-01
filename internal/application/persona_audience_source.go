package application

import (
	"context"
	"errors"
	"fmt"
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
// currently a member of, projecting all members through the trusted directory.
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
	for {
		rooms, err := s.Chat.ListConversations(ctx, chat.ListConversationsRequest{Principal: principal, TenantID: tenantID, Page: page})
		if err != nil {
			return nil, fmt.Errorf("list current chat conversations: %w", err)
		}
		for _, room := range rooms.Conversations {
			if room.TenantID != tenantID || strings.TrimSpace(room.ID) == "" || room.Archived {
				return nil, fmt.Errorf("%w: invalid current conversation %q", errPersonaAudienceSourceUnavailable, room.ID)
			}
			if _, exists := seenRooms[room.ID]; exists {
				return nil, fmt.Errorf("%w: duplicate current conversation %q", errPersonaAudienceSourceUnavailable, room.ID)
			}
			seenRooms[room.ID] = struct{}{}
			members, err := s.listMembers(ctx, tenantID, room.ID, principal)
			if err != nil {
				return nil, err
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

func (s *DatabasePersonaAudienceSource) listMembers(ctx context.Context, tenantID, conversationID string, principal chat.Principal) ([]PersonaAudienceMember, error) {
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
				return nil, fmt.Errorf("%w: invalid current membership in %q", errPersonaAudienceSourceUnavailable, conversationID)
			}
			key := member.HomeTenantID + "\x00" + member.SubjectID
			if _, ok := seen[key]; ok {
				return nil, fmt.Errorf("%w: duplicate membership in %q", errPersonaAudienceSourceUnavailable, conversationID)
			}
			seen[key] = struct{}{}
			facts, err := s.Directory.ResolvePersonaAudienceMember(ctx, member.HomeTenantID, member.SubjectID)
			if err != nil {
				return nil, fmt.Errorf("%w: resolve %s: %v", ErrPersonaAudienceDirectoryFactsMissing, key, err)
			}
			if facts.SubjectID != member.SubjectID || !nonemptyFacts(facts.Roles) || !nonemptyFacts(facts.Populations) || strings.TrimSpace(facts.OrganizationScope) == "" {
				return nil, fmt.Errorf("%w: subject %s in tenant %s", ErrPersonaAudienceDirectoryFactsMissing, member.SubjectID, member.HomeTenantID)
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
	return out, nil
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
