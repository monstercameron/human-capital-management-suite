package participants

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
)

// NewDefaultSpecificationOwnerRegistry compiles the authoritative stable
// BusinessIntent references and their declared semantic owners into the
// experience-plane lookup used by flow stages.
func NewDefaultSpecificationOwnerRegistry() (*SpecificationOwnerRegistry, error) {
	owners := make(map[string]string)
	for _, definition := range definitions.All() {
		ref := definition.Ref.String()
		if ref == "" || definition.OwnerDomain == "" {
			return nil, fmt.Errorf("participants: intent %q has no accountable specification owner", ref)
		}
		if previous, exists := owners[ref]; exists && previous != definition.OwnerDomain {
			return nil, fmt.Errorf("participants: intent %q has conflicting specification owners", ref)
		}
		owners[ref] = definition.OwnerDomain
	}
	return NewSpecificationOwnerRegistry(owners)
}
