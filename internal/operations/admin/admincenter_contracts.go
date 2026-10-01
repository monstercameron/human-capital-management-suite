package admin

import (
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/diagnosticsession"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/evidenceexport"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/incidentrepair"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/inspector"
)

// CenterContract describes an operator-center contract available to the
// composed admin surface. The catalog is intentionally metadata-only: the
// operation packages retain ownership of validation, redaction, and side
// effects, while transports can discover the contracts without importing
// their internals or reaching a store directly.
type CenterContract struct {
	ID                string
	Version           string
	RedactionRequired bool
	ReadOnly          bool
}

// AdminCenterContracts is the composition-root catalog for the governed
// admin-center contracts. Keeping the catalog here makes the contracts part
// of the shipped admin dependency closure; it does not create a second
// implementation or bypass the operation packages' ports.
func AdminCenterContracts() []CenterContract {
	diagnostic := diagnosticsession.Registry()
	export := evidenceexport.Registry()
	incident := incidentrepair.Registry()
	return []CenterContract{
		{ID: "ADMIN-002", Version: "hcmnext.admincenter.workflow-inspector/1", RedactionRequired: true, ReadOnly: true},
		{ID: incident.ID, Version: incident.Version, RedactionRequired: incident.RedactionRequired, ReadOnly: false},
		{ID: diagnostic.ID, Version: diagnostic.Version, RedactionRequired: diagnostic.RedactionRequired, ReadOnly: true},
		{ID: export.ID, Version: export.Version, RedactionRequired: export.RedactionRequired, ReadOnly: true},
	}
}

// Keep the inspector package on the catalog's semantic path. ADMIN-002 is a
// projection rather than a registry-shaped operation, so its stable contract
// identity is carried alongside the other center metadata above.
var _ = inspector.BuildWorkflowView
