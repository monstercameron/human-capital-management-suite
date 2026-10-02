package application

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type AgentAnnouncementCatalogNames struct {
	Versions      PersonaCatalogVersionReader
	Installations PersonaCatalogInstallationReader
	Authority     AgentAnnouncementInstallationAuthority
	OwnerNames    func(context.Context, string, []string) (map[string]string, error)
}

func (s AgentAnnouncementCatalogNames) AgentAnnouncementChoices(ctx context.Context, actor AgentAnnouncementActor) ([]productui.AgentAnnouncementAgent, error) {
	if s.Versions == nil || s.Installations == nil || s.Authority == nil || ctx == nil {
		return nil, ErrAgentAnnouncementUnavailable
	}
	versions, err := s.Versions.ListPersonaCatalogVersions(ctx, values.TenantId(actor.TenantID))
	if err != nil {
		return nil, err
	}
	placements, err := s.Installations.ListPersonaCatalogInstallations(ctx, values.TenantId(actor.TenantID))
	if err != nil {
		return nil, err
	}
	byPersona := map[string]productui.AgentAnnouncementAgent{}
	for _, placement := range placements {
		if !placement.Active || placement.ConversationKind != "PUBLIC_CHANNEL" || placement.ID == "" || strings.TrimSpace(placement.Conversation) == "" {
			continue
		}
		allowed, err := s.Authority.ManageAgentInstallation(ctx, actor, placement.ID, placement.PersonaID, placement.ConversationID)
		if err != nil || !allowed {
			continue
		}
		name := ""
		for _, version := range versions {
			profile := version.Profile.Profile
			if profile.PersonaID == placement.PersonaID && profile.Version == placement.PersonaVersion {
				name = profile.DisplayName
				break
			}
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		choice := byPersona[placement.PersonaID]
		choice.PersonaID, choice.Name = placement.PersonaID, name
		choice.Conversations = append(choice.Conversations, productui.AgentAnnouncementConversation{ID: placement.ConversationID, Name: placement.Conversation, InstallationID: placement.ID})
		byPersona[placement.PersonaID] = choice
	}
	choices := make([]productui.AgentAnnouncementAgent, 0, len(byPersona))
	for _, choice := range byPersona {
		sort.Slice(choice.Conversations, func(i, j int) bool { return choice.Conversations[i].Name < choice.Conversations[j].Name })
		choices = append(choices, choice)
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Name < choices[j].Name })
	return choices, nil
}

func (s AgentAnnouncementCatalogNames) AgentAnnouncementNames(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement) (string, string, string, error) {
	if s.Versions == nil || s.Installations == nil || s.OwnerNames == nil || record.TenantID != actor.TenantUUID || record.OwnerID != actor.SubjectID {
		return "", "", "", ErrAgentAnnouncementDenied
	}
	versions, err := s.Versions.ListPersonaCatalogVersions(ctx, values.TenantId(actor.TenantID))
	if err != nil {
		return "", "", "", err
	}
	placements, err := s.Installations.ListPersonaCatalogInstallations(ctx, values.TenantId(actor.TenantID))
	if err != nil {
		return "", "", "", err
	}
	agentName, conversationName := "", ""
	for _, placement := range placements {
		if placement.ID == record.InstallationID && placement.PersonaID == record.PersonaID && placement.ConversationID == record.ConversationID {
			conversationName = placement.Conversation
			for _, version := range versions {
				p := version.Profile.Profile
				if p.PersonaID == record.PersonaID && p.Version == placement.PersonaVersion {
					agentName = p.DisplayName
					break
				}
			}
			break
		}
	}
	// The installation an announcement was made with is retired when the agent
	// is installed again at a newer version. The announcement is still the
	// owner's: name it from the agent as it is installed in that conversation now.
	if strings.TrimSpace(agentName) == "" || strings.TrimSpace(conversationName) == "" {
		for _, placement := range placements {
			if !placement.Active || placement.PersonaID != record.PersonaID || placement.ConversationID != record.ConversationID {
				continue
			}
			conversationName = placement.Conversation
			for _, version := range versions {
				p := version.Profile.Profile
				if p.PersonaID == record.PersonaID && p.Version == placement.PersonaVersion {
					agentName = p.DisplayName
					break
				}
			}
			break
		}
	}
	names, err := s.OwnerNames(ctx, actor.TenantID, []string{record.OwnerID})
	if err != nil {
		return "", "", "", err
	}
	if strings.TrimSpace(agentName) == "" || strings.TrimSpace(conversationName) == "" {
		return "", "", names[record.OwnerID], errors.Join(ErrAgentAnnouncementUnavailable, ErrAgentAnnouncementAgentGone)
	}
	if strings.TrimSpace(names[record.OwnerID]) == "" {
		return "", "", "", ErrAgentAnnouncementUnavailable
	}
	return agentName, conversationName, names[record.OwnerID], nil
}

var _ AgentAnnouncementNameSource = AgentAnnouncementCatalogNames{}
