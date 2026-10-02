package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentIconCatalogVersions decorates the authorized catalog with identity-owned
// icons. Version changes never synthesize a new icon on a read.
type AgentIconCatalogVersions struct {
	Base  PersonaCatalogVersionReader
	Store *agentpersonastore.Store
}

func (r AgentIconCatalogVersions) ListPersonaAdminVersionHistory(ctx context.Context, tenant values.TenantId) (map[string][]productui.PersonaAdminVersionHistory, error) {
	if ctx == nil {
		return nil, ErrPersonaCatalogDenied
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Tenant() != tenant || p.SubjectKind() != trust.SubjectKindHuman {
		return nil, ErrPersonaCatalogDenied
	}
	if reader, ok := r.Base.(interface {
		ListPersonaAdminVersionHistory(context.Context, values.TenantId) (map[string][]productui.PersonaAdminVersionHistory, error)
	}); ok {
		return reader.ListPersonaAdminVersionHistory(ctx, tenant)
	}
	return nil, nil
}

func (r AgentIconCatalogVersions) ListPersonaCatalogVersions(ctx context.Context, tenant values.TenantId) ([]PersonaCatalogVersion, error) {
	if ctx == nil || r.Base == nil || r.Store == nil {
		return nil, ErrPersonaCatalogDenied
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Tenant() != tenant || p.SubjectKind() != trust.SubjectKindHuman {
		return nil, ErrPersonaCatalogDenied
	}
	versions, err := r.Base.ListPersonaCatalogVersions(ctx, tenant)
	if err != nil {
		return nil, err
	}
	scoped, err := r.Store.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := append([]PersonaCatalogVersion(nil), versions...)
	for i := range out {
		icon, err := scoped.GetIcon(ctx, out[i].Profile.Profile.PersonaID)
		if errors.Is(err, agentpersonastore.ErrNotFound) {
			continue
		}
		if err != nil {
			continue
		}
		out[i].Icon, out[i].IconRevision = icon.Value, icon.Revision
	}
	return out, nil
}

// AgentIconProjection attaches a stored icon after the caller has admitted the
// agent reference or author. It never discovers or widens agent visibility.
type AgentIconProjection struct{ Store *agentpersonastore.Store }

func (r AgentIconProjection) read(ctx context.Context, tenant values.TenantId, id string) (agentpersonastore.PersonaIcon, error) {
	if ctx == nil || r.Store == nil {
		return agentpersonastore.PersonaIcon{}, ErrPersonaCatalogDenied
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Tenant() != tenant {
		return agentpersonastore.PersonaIcon{}, ErrPersonaCatalogDenied
	}
	scoped, err := r.Store.ForTenant(ctx, tenant)
	if err != nil {
		return agentpersonastore.PersonaIcon{}, err
	}
	icon, err := scoped.GetIcon(ctx, id)
	if errors.Is(err, agentpersonastore.ErrNotFound) {
		return agentpersonastore.PersonaIcon{}, nil
	}
	return icon, err
}

func (r AgentIconProjection) Mention(ctx context.Context, tenant values.TenantId, personaID string, mention *chatui.ResolvedPersonaMention) error {
	if mention == nil || mention.Reference.TenantID != tenant.String() || mention.Reference.Kind != "AGENT_MENTION" {
		return ErrPersonaCatalogDenied
	}
	icon, err := r.read(ctx, tenant, personaID)
	if err != nil {
		return err
	}
	mention.Icon, mention.IconRevision = icon.Value, icon.Revision
	return nil
}

func (r AgentIconProjection) Actor(ctx context.Context, tenant values.TenantId, actor *chatui.PersonaActor) error {
	if actor == nil || !actor.Trusted || actor.PersonaID == "" || actor.AgentID == "" {
		return ErrPersonaCatalogDenied
	}
	icon, err := r.read(ctx, tenant, actor.PersonaID)
	if err != nil {
		return err
	}
	actor.Icon, actor.IconRevision = icon.Value, icon.Revision
	return nil
}

func (r AgentIconProjection) Agent(ctx context.Context, tenant values.TenantId, personaID string, agent *productui.AgentSummary) error {
	if agent == nil {
		return ErrPersonaCatalogDenied
	}
	icon, err := r.read(ctx, tenant, personaID)
	if err != nil {
		return err
	}
	agent.Icon, agent.IconRevision = icon.Value, icon.Revision
	return nil
}
