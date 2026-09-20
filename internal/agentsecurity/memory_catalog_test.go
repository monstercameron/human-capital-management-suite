package agentsecurity

import (
	"context"
	"strings"
	"sync"
)

// IntentDefinition is one discoverable definition a test catalog may target.
// Test-only fixture: production code resolves through the DefinitionCatalog
// read port over the live intent catalog instead of this stand-in.
type IntentDefinition struct {
	ID                 string
	Version            string
	RequiresReview     bool
	RequiresSimulation bool
	MaxBulk            int
}

// DefinitionRegistry is an in-memory DefinitionCatalog for tests. Only
// registered definitions exist: model output can never invent one.
type DefinitionRegistry struct {
	mu          sync.Mutex
	definitions map[string]IntentDefinition
}

// NewDefinitionRegistry starts an empty test catalog.
func NewDefinitionRegistry() *DefinitionRegistry {
	return &DefinitionRegistry{definitions: make(map[string]IntentDefinition)}
}

// Register publishes one definition. Duplicates and hollow entries refuse.
func (r *DefinitionRegistry) Register(definition IntentDefinition) error {
	if r == nil {
		return refusal(RefusalInvalid, "definitions", "nil registry")
	}
	if strings.TrimSpace(definition.ID) == "" || strings.TrimSpace(definition.Version) == "" {
		return refusal(RefusalInvalid, "definition", "definition id and version are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.definitions[definition.ID]; dup {
		return refusal(RefusalInvalid, "definition", "definition is already registered")
	}
	r.definitions[definition.ID] = definition
	return nil
}

// LookupDefinition resolves one registered definition against the test catalog.
func (r *DefinitionRegistry) LookupDefinition(_ context.Context, id string) (CatalogDefinition, error) {
	if r == nil {
		return CatalogDefinition{}, ErrUnknownDefinition
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	definition, ok := r.definitions[id]
	if !ok {
		return CatalogDefinition{}, ErrUnknownDefinition
	}
	return CatalogDefinition{
		ID:                 definition.ID,
		Version:            definition.Version,
		RequiresReview:     definition.RequiresReview,
		RequiresSimulation: definition.RequiresSimulation,
		MaxBulk:            definition.MaxBulk,
	}, nil
}
