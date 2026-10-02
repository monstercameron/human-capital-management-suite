package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// personaRailChat is the chat service's conversation listing, which the
// persona surface's own narrow chat port does not carry.
type personaRailChat interface {
	ListConversations(context.Context, chat.ListConversationsRequest) (chat.ListConversationsResponse, error)
}

const (
	personaRailPageSize = 100
	personaRailMaxPages = 20
)

var _ transport.AgentIconRailSource = AgentIconChatDirectory{}

// RailAgentIcons lists the caller's direct conversations with agents, each with
// the agent's name, its own description and its stored icon, so the sidebar can
// draw every agent row with its own icon on first paint. Each conversation is
// one the caller is in (the listing is the caller's), and an agent appears only
// when that conversation's own directory would admit it: a current reference to
// a published persona version the caller may use.
func (s AgentIconChatDirectory) RailAgentIcons(ctx context.Context) (transport.AgentIconRail, error) {
	if ctx == nil {
		return transport.AgentIconRail{}, personachat.ErrUnauthenticated
	}
	if s.Base == nil || s.Store == nil || s.Base.References == nil || s.Base.Personas == nil {
		return transport.AgentIconRail{}, personachat.ErrUnavailable
	}
	p, ok := personaSurfacePrincipal(ctx)
	if !ok {
		return transport.AgentIconRail{}, personachat.ErrUnauthenticated
	}
	lister, ok := s.Base.Chat.(personaRailChat)
	if !ok {
		return transport.AgentIconRail{}, personachat.ErrUnavailable
	}
	at := time.Now().UTC()
	if s.Base.Now != nil {
		at = s.Base.Now().UTC()
	}
	if !p.ExpiresAt().After(at) {
		return transport.AgentIconRail{}, personachat.ErrUnauthenticated
	}
	tenant := p.Tenant().String()
	principal := chat.Principal{TenantID: tenant, SubjectID: p.Subject()}
	rooms := []chat.Conversation{}
	seenCursor := map[string]bool{}
	page := chat.Page{PageSize: personaRailPageSize}
	for i := 0; i < personaRailMaxPages; i++ {
		listed, err := lister.ListConversations(ctx, chat.ListConversationsRequest{Principal: principal, TenantID: tenant, Page: page})
		if err != nil {
			return transport.AgentIconRail{}, surfaceChatError(err)
		}
		for _, room := range listed.Conversations {
			if room.Kind == chat.Direct && room.TenantID == tenant && !room.Archived {
				rooms = append(rooms, room)
			}
		}
		if listed.NextCursor == "" || seenCursor[listed.NextCursor] {
			break
		}
		seenCursor[listed.NextCursor], page.Cursor = true, listed.NextCursor
	}
	out := transport.AgentIconRail{Agents: []transport.AgentIconRailEntry{}}
	if len(rooms) == 0 {
		return out, nil
	}
	profiles, err := s.Base.Personas.ListAvailable(ctx, p)
	if err != nil {
		return transport.AgentIconRail{}, personachat.ErrUnavailable
	}
	scoped, err := s.Store.ForTenant(ctx, p.Tenant())
	if err != nil {
		return transport.AgentIconRail{}, personachat.ErrUnavailable
	}
	for _, room := range rooms {
		candidates, err := s.Base.References.ListPersonaReferenceCandidates(ctx, principal, tenant, room.ID, "")
		if err != nil {
			continue
		}
		for _, candidate := range candidates {
			if candidate.Kind != chat.AgentMention || !candidate.Eligible || candidate.TenantID != tenant || candidate.ConversationID != room.ID {
				continue
			}
			facts, err := s.Base.References.LookupPersonaReference(ctx, tenant, room.ID, candidate.ID)
			if err != nil || !currentPersonaReference(facts, tenant, room.ID, candidate.ID, at) {
				continue
			}
			for _, version := range profiles {
				profile := version.Profile
				if version.Verify() != nil || profile.PersonaID != facts.PersonaID || uint64(profile.Version) != facts.PersonaVersion || candidate.Display != profile.DisplayName {
					continue
				}
				icon, err := scoped.GetIcon(ctx, facts.PersonaID)
				if err != nil && !errors.Is(err, agentpersonastore.ErrNotFound) {
					icon = agentpersonastore.PersonaIcon{}
				}
				out.Agents = append(out.Agents, transport.AgentIconRailEntry{ConversationID: room.ID, AgentID: candidate.ID, Name: profile.DisplayName, Purpose: strings.TrimSpace(profile.Purpose), Icon: icon.Value, IconRevision: icon.Revision})
				break
			}
			// A direct conversation is with one agent.
			if len(out.Agents) > 0 && out.Agents[len(out.Agents)-1].ConversationID == room.ID {
				break
			}
		}
	}
	return out, nil
}
