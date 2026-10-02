package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// AgentIconChatDirectory joins stored identities only after the ordinary chat
// surface has filtered mentions and admitted the room's visible post authors.
type AgentIconChatDirectory struct {
	Base  *PersonaChatSurface
	Store *agentpersonastore.Store
}

func (s AgentIconChatDirectory) DirectoryAgentIcons(ctx context.Context, conversation string) (transport.AgentIconDirectory, error) {
	if ctx == nil {
		return transport.AgentIconDirectory{}, personachat.ErrUnauthenticated
	}
	if s.Base == nil || s.Store == nil {
		return transport.AgentIconDirectory{}, personachat.ErrUnavailable
	}
	directory, err := s.Base.Directory(ctx, conversation)
	if err != nil {
		return transport.AgentIconDirectory{}, err
	}
	p, room, err := s.Base.member(ctx, conversation)
	if err != nil {
		return transport.AgentIconDirectory{}, err
	}
	scoped, err := s.Store.ForTenant(ctx, p.Tenant())
	if err != nil {
		return transport.AgentIconDirectory{}, personachat.ErrUnavailable
	}
	out := transport.AgentIconDirectory{Personas: make([]transport.AgentIconMentionProfile, 0, len(directory.Personas)), PostActors: make([]transport.AgentIconPostActor, 0, len(directory.PostActors))}
	at := time.Now().UTC()
	if s.Base.Now != nil {
		at = s.Base.Now().UTC()
	}
	for _, profile := range directory.Personas {
		facts, err := s.Base.References.LookupPersonaReference(ctx, p.Tenant().String(), room.ID, profile.Reference.ID)
		if err != nil || !currentPersonaReference(facts, p.Tenant().String(), room.ID, profile.Reference.ID, at) {
			continue
		}
		icon, err := scoped.GetIcon(ctx, facts.PersonaID)
		if err != nil && !errors.Is(err, agentpersonastore.ErrNotFound) {
			icon = agentpersonastore.PersonaIcon{}
		}
		out.Personas = append(out.Personas, transport.AgentIconMentionProfile{Profile: profile, Icon: icon.Value, IconRevision: icon.Revision})
	}
	for _, actor := range directory.PostActors {
		icon, err := scoped.GetIcon(ctx, actor.PersonaID)
		if err != nil && !errors.Is(err, agentpersonastore.ErrNotFound) {
			icon = agentpersonastore.PersonaIcon{}
		}
		out.PostActors = append(out.PostActors, transport.AgentIconPostActor{PostActor: actor, Icon: icon.Value, IconRevision: icon.Revision})
	}
	return out, nil
}
