package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// personaAudienceMemberSource is the narrow read an authority check needs:
// one subject's own membership of one conversation. A source that offers it
// is asked for exactly that, instead of every conversation the subject is in
// with every member of each projected through the directory.
type personaAudienceMemberSource interface {
	CurrentPersonaAudienceMember(ctx context.Context, tenantID, conversationID, subjectID string) (PersonaAudienceMember, error)
}

// CurrentPersonaAudienceMember resolves the verified subject's current
// membership of one conversation and its directory facts. It applies the same
// checks as ListCurrentPersonaAudience to that one member: the subject must be
// the verified principal, an active member exactly once, with complete facts.
func (s *DatabasePersonaAudienceSource) CurrentPersonaAudienceMember(ctx context.Context, tenantID, conversationID, subjectID string) (PersonaAudienceMember, error) {
	verified, err := s.verify(ctx, tenantID, subjectID)
	if err != nil {
		return PersonaAudienceMember{}, err
	}
	if s.Chat == nil || s.Directory == nil || strings.TrimSpace(conversationID) == "" {
		return PersonaAudienceMember{}, errPersonaAudienceSourceUnavailable
	}
	principal := chat.Principal{TenantID: string(verified.Tenant()), SubjectID: verified.Subject()}
	page := chat.Page{PageSize: 200}
	matches := 0
	for {
		members, err := s.Chat.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: principal, TenantID: tenantID, ConversationID: conversationID, Page: page})
		if err != nil {
			return PersonaAudienceMember{}, fmt.Errorf("list current chat members for %q: %w", conversationID, err)
		}
		for _, member := range members.Memberships {
			if member.TenantID != tenantID || member.ConversationID != conversationID {
				continue
			}
			if member.HomeTenantID == principal.TenantID && member.SubjectID == principal.SubjectID {
				matches++
			}
		}
		if members.NextCursor == "" {
			break
		}
		page.Cursor = members.NextCursor
	}
	if matches != 1 {
		return PersonaAudienceMember{}, fmt.Errorf("%w: current principal membership missing", ErrPersonaAudienceDirectoryFactsMissing)
	}
	facts, err := s.Directory.ResolvePersonaAudienceMember(ctx, principal.TenantID, principal.SubjectID)
	if err != nil {
		return PersonaAudienceMember{}, fmt.Errorf("%w: resolve current principal: %v", ErrPersonaAudienceDirectoryFactsMissing, err)
	}
	if facts.SubjectID != principal.SubjectID || !nonemptyFacts(facts.Roles) || !nonemptyFacts(facts.Populations) || strings.TrimSpace(facts.OrganizationScope) == "" {
		return PersonaAudienceMember{}, fmt.Errorf("%w: current principal %s in tenant %s", ErrPersonaAudienceDirectoryFactsMissing, principal.SubjectID, principal.TenantID)
	}
	facts.Roles = append([]string(nil), facts.Roles...)
	facts.Populations = append([]string(nil), facts.Populations...)
	return facts, nil
}

var _ personaAudienceMemberSource = (*DatabasePersonaAudienceSource)(nil)
