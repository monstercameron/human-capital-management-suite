package application

import "github.com/monstercameron/human-capital-management-suite/internal/operations/wedge"

// ServedWedgeSurface exposes the Gate A wedge-evidence contracts through the
// application boundary used by hcmnext serve. The wedge package remains the
// semantic owner; this surface only carries its value-owned operations into
// the composed application.
type ServedWedgeSurface struct {
	ValidateAdoption   func(wedge.AdoptionMetric) error
	EvaluateAdoption   func(wedge.AdoptionMetric) (wedge.AdoptionResult, error)
	SortBypassTaxonomy func(wedge.BypassTaxonomy) wedge.BypassTaxonomy
}

// NewServedWedgeSurface returns the wedge-evidence capabilities reachable from
// the served application. It creates no process-wide or tenant-wide state.
func NewServedWedgeSurface() ServedWedgeSurface {
	return ServedWedgeSurface{
		ValidateAdoption:   wedge.AdoptionMetric.Validate,
		EvaluateAdoption:   wedge.AdoptionMetric.Evaluate,
		SortBypassTaxonomy: wedge.SortBypassTaxonomy,
	}
}

// Wedge returns the wedge-evidence capabilities exposed by a composed
// application. A nil application has no served capabilities.
func (a *App) Wedge() ServedWedgeSurface {
	if a == nil {
		return ServedWedgeSurface{}
	}
	return NewServedWedgeSurface()
}
