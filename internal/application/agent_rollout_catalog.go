package application

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	agentrollout "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/rollout"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

type AgentVersionRolloutCatalog struct {
	Versions       []AgentVersionRolloutCatalogVersion      `json:"versions"`
	Installations  []AgentVersionRolloutCatalogInstallation `json:"installations"`
	NotListedCount int                                      `json:"not_listed_count,omitempty"`
}
type AgentVersionRolloutCatalogVersion struct {
	PersonaID       string `json:"persona_id"`
	Name            string `json:"name"`
	Version         int64  `json:"version"`
	ProfileDigest   string `json:"profile_digest"`
	ManifestID      string `json:"manifest_id"`
	ManifestVersion uint64 `json:"manifest_version"`
	PublishedAt     string `json:"published_at"`
	Current         bool   `json:"current"`
}
type AgentVersionRolloutCatalogInstallation struct {
	ID             string                          `json:"id"`
	PersonaID      string                          `json:"persona_id"`
	ConversationID string                          `json:"conversation_id"`
	Version        int64                           `json:"version"`
	Revision       int64                           `json:"revision"`
	Policy         agentpersonastore.ChannelPolicy `json:"policy"`
	Name           string                          `json:"name,omitempty"`
	Kind           string                          `json:"kind,omitempty"`
	MemberCount    uint32                          `json:"member_count,omitempty"`
	Visible        bool                            `json:"visible"`
	UpdateBlocked  bool                            `json:"update_blocked,omitempty"`
}
type AgentVersionRolloutCatalogTenant interface {
	ListPublished(context.Context) ([]agentpersonastore.PersonaVersion, error)
	ListVersionRolloutInstallations(context.Context, string) ([]agentpersonastore.PersonaInstallation, error)
}

type AgentVersionRolloutAgentIdentityTenant interface {
	LookupPersonaChatIdentity(context.Context, string) (agentpersonastore.PersonaChatIdentity, error)
}

func (s *AgentVersionRolloutService) catalog(ctx context.Context, store AgentVersionRolloutTenant, actor PersonaAdminCommandActor, r AgentVersionRolloutCommand) (AgentVersionRolloutReceipt, error) {
	if r.RolloutID != "" || r.Digest != "" || r.Revision != 0 || r.TargetVersion != 0 || len(r.InstallationIDs) != 0 || len(r.CanaryIDs) != 0 || r.BatchLimit != 0 {
		return AgentVersionRolloutReceipt{}, agentrollout.ErrInvalid
	}
	reader, ok := store.(AgentVersionRolloutCatalogTenant)
	if !ok {
		return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
	}
	published, err := reader.ListPublished(ctx)
	if err != nil {
		return AgentVersionRolloutReceipt{}, err
	}
	catalog := &AgentVersionRolloutCatalog{Versions: []AgentVersionRolloutCatalogVersion{}, Installations: []AgentVersionRolloutCatalogInstallation{}}
	personas := map[string]bool{}
	for _, row := range published {
		if row.TenantID != actor.Tenant {
			return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
		}
		if r.PersonaID != "" && r.PersonaID != row.PersonaID {
			continue
		}
		var p agentpersona.PersonaProfile
		if json.Unmarshal(row.Profile, &p) != nil {
			return AgentVersionRolloutReceipt{}, ErrPersonaDraftInvalid
		}
		catalog.Versions = append(catalog.Versions, AgentVersionRolloutCatalogVersion{PersonaID: row.PersonaID, Name: row.DisplayName, Version: row.Version, ProfileDigest: row.ContentDigest, ManifestID: p.Manifest.ID, ManifestVersion: uint64(p.Manifest.Version), PublishedAt: row.CreatedAt.UTC().Format("2006-01-02")})
		personas[row.PersonaID] = true
	}
	ids := make([]string, 0, len(personas))
	for id := range personas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		installations, err := reader.ListVersionRolloutInstallations(ctx, id)
		if err != nil {
			return AgentVersionRolloutReceipt{}, err
		}
		for _, in := range installations {
			if in.TenantID != actor.Tenant || in.PersonaID != id {
				return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
			}
			facts, err := s.Placement.ResolvePersonaAdminPlacement(ctx, actor, in.ConversationID)
			if err != nil || facts.ManagerID != actor.Subject || facts.Tenant != actor.Tenant || facts.ConversationID != in.ConversationID {
				catalog.NotListedCount++
				continue
			}
			conversation := s.rolloutConversation(ctx, reader, actor, in.ConversationID)
			catalog.Installations = append(catalog.Installations, AgentVersionRolloutCatalogInstallation{ID: in.InstallationID, PersonaID: in.PersonaID, ConversationID: in.ConversationID, Version: in.PersonaVersion, Revision: in.Revision, Policy: in.ChannelPolicy, Name: conversation.Name, Kind: conversation.Kind, MemberCount: conversation.MemberCount, Visible: conversation.Visible})
		}
	}
	for index := range catalog.Versions {
		for _, installation := range catalog.Installations {
			if installation.PersonaID == catalog.Versions[index].PersonaID && installation.Version == catalog.Versions[index].Version {
				catalog.Versions[index].Current = true
				break
			}
		}
	}
	return AgentVersionRolloutReceipt{Catalog: catalog}, nil
}

type agentRolloutConversation struct {
	Name        string
	Kind        string
	MemberCount uint32
	Visible     bool
}

func (s *AgentVersionRolloutService) rolloutConversation(ctx context.Context, store AgentVersionRolloutCatalogTenant, actor PersonaAdminCommandActor, conversationID string) agentRolloutConversation {
	if s == nil || s.Conversations == nil || s.Conversations.Chat == nil || actor.Principal == nil {
		return agentRolloutConversation{}
	}
	principal := chat.Principal{TenantID: actor.Tenant.String(), SubjectID: actor.Subject}
	rooms, _, err := s.Conversations.listRooms(ctx, principal, actor.Tenant)
	if err != nil {
		return agentRolloutConversation{}
	}
	for _, room := range rooms {
		if room.ID != conversationID {
			continue
		}
		out := agentRolloutConversation{Name: strings.TrimSpace(room.Name), Kind: string(room.Kind), MemberCount: room.MemberCount, Visible: true}
		if room.Kind != chat.Direct {
			return out
		}
		members, _, err := s.Conversations.listMembers(ctx, principal, actor.Tenant, room.ID)
		if err != nil || s.Conversations.Directory == nil {
			return agentRolloutConversation{}
		}
		for _, member := range members {
			if member.SubjectID == actor.Subject {
				continue
			}
			if label := rolloutAgentChatIdentityLabel(ctx, store, member); label != "" {
				out.Name = label
				return out
			}
			target, resolveErr := s.Conversations.Directory.ResolvePersonaCatalogTarget(ctx, actor.Tenant, member.SubjectID)
			if resolveErr != nil || target.ID != member.SubjectID || strings.TrimSpace(target.Label) == "" {
				return agentRolloutConversation{}
			}
			out.Name = strings.TrimSpace(target.Label)
			return out
		}
		return agentRolloutConversation{}
	}
	return agentRolloutConversation{}
}

func rolloutAgentChatIdentityLabel(ctx context.Context, store AgentVersionRolloutCatalogTenant, member chat.Membership) string {
	identityStore, ok := store.(AgentVersionRolloutAgentIdentityTenant)
	if !ok || identityStore == nil {
		return ""
	}
	identity, err := identityStore.LookupPersonaChatIdentity(ctx, member.SubjectID)
	if err != nil || !identity.Active || identity.TenantID.String() != member.HomeTenantID {
		return ""
	}
	published, err := store.ListPublished(ctx)
	if err != nil {
		return ""
	}
	for _, version := range published {
		if version.PersonaID == identity.PersonaID && strings.TrimSpace(version.DisplayName) != "" {
			return strings.TrimSpace(version.DisplayName)
		}
	}
	return ""
}
