package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrAgentPageCatalogBindingUnavailable identifies an Agents page binding
// that lacks either the existing task client or the authoritative persona
// discovery sources.
var ErrAgentPageCatalogBindingUnavailable = errors.New("application: agents page catalog binding unavailable")

// AgentPageCatalogBinding decorates an existing Agents page client with the
// viewer's current available persona catalog. The wrapped client remains the
// owner-scoped source for tasks and threads; this binding only replaces the
// persona summaries after the verified request principal has been checked.
type AgentPageCatalogBinding struct {
	Tasks   productui.AgentClient
	Catalog *AgentUserCatalog
	// Icons reads each listed agent's stored icon, so the Agents page draws the
	// icon Chat and Agent setup draw. It is optional: without it the page draws
	// a fallback for every agent.
	Icons AgentIconProjection
}

// NewAgentPageCatalogBinding constructs a fail-closed Agents page binding.
// Personas must be backed by current audience and installation authority;
// skills must be backed by the current AGENT2-005 discovery authority.
func NewAgentPageCatalogBinding(tasks productui.AgentClient, personas AvailablePersonaReader, skills AgentSkillDiscoverer) (*AgentPageCatalogBinding, error) {
	if tasks == nil || personas == nil || skills == nil {
		return nil, ErrAgentPageCatalogBindingUnavailable
	}
	return &AgentPageCatalogBinding{
		Tasks:   tasks,
		Catalog: &AgentUserCatalog{Personas: personas, Skills: skills},
	}, nil
}

var _ productui.AgentClient = (*AgentPageCatalogBinding)(nil)

// Snapshot preserves the wrapped client's owner-scoped task and thread data,
// then adds only personas authorized for the verified tenant and subject.
// Missing or detached trust, audience, installation, or skill authority
// returns an error instead of rendering a partial or broad catalog.
func (b *AgentPageCatalogBinding) Snapshot(ctx context.Context, req productui.AgentSnapshotRequest) (productui.AgentSnapshot, error) {
	if b == nil || b.Tasks == nil || b.Catalog == nil || ctx == nil {
		return productui.AgentSnapshot{}, ErrAgentPageCatalogBindingUnavailable
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman ||
		verified.Tenant().String() != req.TenantID || verified.Subject() != req.Principal {
		return productui.AgentSnapshot{}, ErrAgentPageCatalogBindingUnavailable
	}
	snapshot, err := b.Tasks.Snapshot(ctx, req)
	if err != nil {
		return productui.AgentSnapshot{}, err
	}
	agents, err := b.Catalog.List(ctx, verified)
	if err != nil {
		return productui.AgentSnapshot{}, err
	}
	snapshot.Agents = cloneAgentSummariesForPage(agents)
	agentUX074AttachStoredIcons(ctx, b.Icons, verified, snapshot.Agents)
	return snapshot, nil
}

func cloneAgentSummariesForPage(in []productui.AgentSummary) []productui.AgentSummary {
	out := make([]productui.AgentSummary, len(in))
	for i, item := range in {
		out[i] = item
		out[i].Skills = append([]string(nil), item.Skills...)
	}
	return out
}
