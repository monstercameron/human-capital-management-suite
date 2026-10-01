package main

import (
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/agentportable"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

var errAgentRolloutInput = errors.New("agent rollout: invalid input")

func portableDefinitionMappingFields(data []byte) ([]productui.AgentPortableDestination, error) {
	definition, err := agentportable.Parse(data)
	if err != nil {
		return nil, err
	}
	var out []productui.AgentPortableDestination
	add := func(kind agentportable.ReferenceKind, id string) {
		out = append(out, productui.AgentPortableDestination{ID: string(kind) + ":" + id, Label: string(kind) + " · " + id, Kind: string(kind), SourceID: id})
	}
	for _, ref := range definition.SourceCeiling {
		add(agentportable.SourceReference, ref.ID)
	}
	for _, ref := range definition.ToolCeiling {
		add(agentportable.CapabilityReference, ref.ID)
	}
	add(agentportable.ModelPolicyReference, definition.ModelPolicy.ID)
	add(agentportable.OutputSchemaReference, definition.OutputSchema.ID)
	for _, ref := range definition.EvaluationRefs {
		add(agentportable.EvaluationReference, ref.ID)
	}
	return out, nil
}
