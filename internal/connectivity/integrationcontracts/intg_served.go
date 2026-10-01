// Package integrationcontracts describes the schema and mapping capabilities
// that the served IntegrationService makes visible to the connector test
// bench. It is deliberately a projection of the underlying contracts: it does
// not publish a snapshot, infer a mapping, or promote a diff.
package integrationcontracts

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/mapping"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot/diff"
)

const (
	SchemaDiscovery = "integrations.schemas.discover"
	SchemaDiff      = "integrations.schemas.diff"
	MappingValidate = "integrations.mappings.validate"
)

// Contract is the immutable vocabulary the served test bench can report.
// SnapshotStates and Impacts are copied on construction so callers cannot
// mutate the composition's contract through a returned slice.
type Contract struct {
	Name           string
	SnapshotStates []schemasnapshot.State
	Impacts        []diff.Impact
	Identity       string
}

// Contracts returns the served schema/mapping contract set in stable order.
func Contracts() []Contract {
	return []Contract{
		{Name: SchemaDiscovery, SnapshotStates: schemasnapshot.States()},
		{Name: SchemaDiff, Impacts: []diff.Impact{diff.ImpactBreaking, diff.ImpactAdditive, diff.ImpactCosmetic}},
		{Name: MappingValidate, Identity: mapping.IdentityTransformation},
	}
}

// Validate rejects an incomplete served contract manifest. This is a
// composition check, not a promotion check: provider-specific schema bodies
// and declared consumer mappings are still required before diff or mapping
// work can run.
func Validate(contracts []Contract) error {
	if len(contracts) != 3 {
		return fmt.Errorf("integration contracts: got %d contracts, want 3", len(contracts))
	}
	seen := make(map[string]struct{}, len(contracts))
	for _, contract := range contracts {
		if contract.Name == "" {
			return fmt.Errorf("integration contracts: contract name is required")
		}
		if _, ok := seen[contract.Name]; ok {
			return fmt.Errorf("integration contracts: duplicate contract %q", contract.Name)
		}
		seen[contract.Name] = struct{}{}
	}
	for _, name := range []string{SchemaDiscovery, SchemaDiff, MappingValidate} {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("integration contracts: required contract %q is missing", name)
		}
	}
	for _, contract := range contracts {
		switch contract.Name {
		case SchemaDiscovery:
			states := schemasnapshot.States()
			if len(contract.SnapshotStates) != len(states) {
				return fmt.Errorf("integration contracts: schema discovery must expose all snapshot states")
			}
			for i := range states {
				if contract.SnapshotStates[i] != states[i] {
					return fmt.Errorf("integration contracts: schema discovery state %d is %q, want %q", i, contract.SnapshotStates[i], states[i])
				}
			}
		case SchemaDiff:
			impacts := []diff.Impact{diff.ImpactBreaking, diff.ImpactAdditive, diff.ImpactCosmetic}
			if len(contract.Impacts) != len(impacts) {
				return fmt.Errorf("integration contracts: schema diff must expose all typed impacts")
			}
			for i := range impacts {
				if contract.Impacts[i] != impacts[i] {
					return fmt.Errorf("integration contracts: schema diff impact %d is %q, want %q", i, contract.Impacts[i], impacts[i])
				}
			}
		case MappingValidate:
			if contract.Identity != mapping.IdentityTransformation {
				return fmt.Errorf("integration contracts: mapping validation must expose bounded identity transform")
			}
		}
	}
	return nil
}
