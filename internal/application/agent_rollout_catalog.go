package application

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	agentrollout "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/rollout"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

type AgentVersionRolloutCatalog struct {
	Versions      []AgentVersionRolloutCatalogVersion      `json:"versions"`
	Installations []AgentVersionRolloutCatalogInstallation `json:"installations"`
}
type AgentVersionRolloutCatalogVersion struct {
	PersonaID       string `json:"persona_id"`
	Name            string `json:"name"`
	Version         int64  `json:"version"`
	ProfileDigest   string `json:"profile_digest"`
	ManifestID      string `json:"manifest_id"`
	ManifestVersion uint64 `json:"manifest_version"`
}
type AgentVersionRolloutCatalogInstallation struct {
	ID             string                          `json:"id"`
	PersonaID      string                          `json:"persona_id"`
	ConversationID string                          `json:"conversation_id"`
	Version        int64                           `json:"version"`
	Revision       int64                           `json:"revision"`
	Policy         agentpersonastore.ChannelPolicy `json:"policy"`
}
type AgentVersionRolloutCatalogTenant interface {
	ListPublished(context.Context) ([]agentpersonastore.PersonaVersion, error)
	ListVersionRolloutInstallations(context.Context, string) ([]agentpersonastore.PersonaInstallation, error)
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
		catalog.Versions = append(catalog.Versions, AgentVersionRolloutCatalogVersion{PersonaID: row.PersonaID, Name: row.DisplayName, Version: row.Version, ProfileDigest: row.ContentDigest, ManifestID: p.Manifest.ID, ManifestVersion: uint64(p.Manifest.Version)})
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
				continue
			}
			catalog.Installations = append(catalog.Installations, AgentVersionRolloutCatalogInstallation{ID: in.InstallationID, PersonaID: in.PersonaID, ConversationID: in.ConversationID, Version: in.PersonaVersion, Revision: in.Revision, Policy: in.ChannelPolicy})
		}
	}
	return AgentVersionRolloutReceipt{Catalog: catalog}, nil
}
