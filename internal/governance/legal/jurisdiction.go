package legal

import (
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/jurisdiction"
)

// Jurisdiction is the country/state/locality tuple that a legal rule pack is
// published against. The type and its behaviour live in
// internal/kernel/jurisdiction, a pure leaf that the browser client may link;
// this alias keeps every legal caller on the same type.
type Jurisdiction = jurisdiction.Jurisdiction

// Jurisdiction errors. All are matchable with errors.Is.
var (
	ErrJurisdictionCountry  = jurisdiction.ErrJurisdictionCountry
	ErrJurisdictionState    = jurisdiction.ErrJurisdictionState
	ErrJurisdictionLocality = jurisdiction.ErrJurisdictionLocality
)
