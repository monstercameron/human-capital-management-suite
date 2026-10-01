package journey

import (
	"fmt"
	"strings"
)

// ServingContractID identifies the renderer contract used by the served
// Promotion journey client.
const ServingContractID = "hcmnext.conformance.promotion-journey-renderer/v1"

// ValidateServingContract renders each production journey state through the
// same Document entry point used by the browser client. It has no network or
// persistence effects; the workspace composition calls it before publishing
// the journey route.
func ValidateServingContract() error {
	pages := []Page{SampleListPage(), SampleDetailPage(), SampleCompletedDetailPage()}
	for i, page := range pages {
		doc, err := Document(page)
		if err != nil {
			return fmt.Errorf("journey: serving document %d: %w", i, err)
		}
		if !strings.Contains(doc, "<html lang=\"en-US\" dir=\"ltr\">") || !strings.Contains(doc, "<style>") {
			return fmt.Errorf("journey: serving document %d lacks canonical document metadata", i)
		}
	}
	return nil
}
