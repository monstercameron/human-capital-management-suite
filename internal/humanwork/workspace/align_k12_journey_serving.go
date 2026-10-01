package workspace

import (
	"fmt"

	journey "github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// validateJourneyRendererServingContract keeps the browser journey renderer
// on the served-command dependency path. The workspace owns route admission;
// the renderer owns the document tree and its accessibility/localization
// contract.
func validateJourneyRendererServingContract() error {
	if err := journey.ValidateServingContract(); err != nil {
		return fmt.Errorf("workspace: journey renderer serving contract: %w", err)
	}
	return nil
}
