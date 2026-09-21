package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// AgentCatalog adapts the service's live intent definition registry to the
// bounded-agent compiler's definition read port, so ActionCompiler resolves
// the same versioned definitions IntentService.GetIntentDefinition serves.
//
// The derivation is deliberately conservative and documented per field:
//   - RequiresReview follows the kernel ApprovalRequired flag.
//   - RequiresSimulation holds while the definition cannot execute: a catalog
//     entry whose allowed modes exclude EXECUTE (for example promote_worker,
//     which P1A simulates and never executes) must stay simulated.
//   - MaxBulk is 1 for every live definition. The catalog declares no bulk
//     fan-out ceiling, so the adapter refuses fan-out until a definition
//     declares one; per-item child intents stay explicit.
type AgentCatalog struct {
	defs *intent.Registry
}

// NewAgentCatalog binds the adapter to the live definition registry.
func NewAgentCatalog(defs *intent.Registry) (*AgentCatalog, error) {
	if defs == nil {
		return nil, fmt.Errorf("app: an intent definition registry is required")
	}
	return &AgentCatalog{defs: defs}, nil
}

// LookupDefinition resolves one published, invocable definition by intent
// type id and projects the compiler's view. An unknown, hollow or retired id
// reports agentsecurity.ErrUnknownDefinition and the compiler refuses.
func (c *AgentCatalog) LookupDefinition(ctx context.Context, id string) (agentsecurity.CatalogDefinition, error) {
	if err := ctx.Err(); err != nil {
		return agentsecurity.CatalogDefinition{}, err
	}
	if c == nil || c.defs == nil {
		return agentsecurity.CatalogDefinition{}, agentsecurity.ErrUnknownDefinition
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return agentsecurity.CatalogDefinition{}, agentsecurity.ErrUnknownDefinition
	}
	var best *intent.Definition
	for _, def := range c.defs.Definitions() {
		if def.Ref.TypeID != id || !def.Maturity.Invocable() {
			continue
		}
		candidate := def
		if best == nil || candidate.Ref.Version > best.Ref.Version {
			best = &candidate
		}
	}
	if best == nil {
		return agentsecurity.CatalogDefinition{}, agentsecurity.ErrUnknownDefinition
	}
	return agentsecurity.CatalogDefinition{
		ID:                 best.Ref.TypeID,
		Version:            fmt.Sprintf("v%d", best.Ref.Version),
		RequiresReview:     best.ApprovalRequired,
		RequiresSimulation: !best.AllowsMode(intent.ModeExecute),
		MaxBulk:            1,
		InputSchemaRef:     best.InputSchema.String(),
		ResultSchemaRef:    best.ResultSchema.String(),
		CapabilityRefs:     slices.Clone(best.RequiredCapabilities),
		GovernanceRefs:     slices.Clone(best.GovernanceRequirements),
		SideEffectProfile:  best.SideEffect.String(),
		RiskClass:          best.RiskClass,
	}, nil
}
