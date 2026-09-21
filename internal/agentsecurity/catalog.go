package agentsecurity

import (
	"context"
	"errors"
)

// ErrUnknownDefinition refuses a definition id the live catalog does not
// publish. The compiler treats it as an undiscoverable definition, never as
// an empty default.
var ErrUnknownDefinition = errors.New("agentsecurity: definition is not discoverable")

// CatalogDefinition is the compiler's view of one live catalog definition.
// It carries the review, simulation and bulk gates the compiler enforces plus
// the schema, capability, governance, side-effect and risk constraints the
// served IntentDefinition catalog publishes for the same id and version, so a
// compiled draft can never drift from what the platform actually serves.
type CatalogDefinition struct {
	ID                 string
	Version            string
	RequiresReview     bool
	RequiresSimulation bool
	MaxBulk            int
	InputSchemaRef     string
	ResultSchemaRef    string
	CapabilityRefs     []string
	GovernanceRefs     []string
	SideEffectProfile  string
	RiskClass          string
}

// DefinitionCatalog is the read port the ActionCompiler resolves definitions
// through. Implementations must resolve against the platform's live,
// versioned definition catalog: an id with no published, invocable version
// reports ErrUnknownDefinition and the compiler refuses the action.
type DefinitionCatalog interface {
	LookupDefinition(ctx context.Context, id string) (CatalogDefinition, error)
}
