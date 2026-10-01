package application

import statuscontract "github.com/monstercameron/human-capital-management-suite/internal/experience/status"

// ServedStatusSurface exposes the reviewed service-status projection through
// the application boundary used by hcmnext serve. The caller supplies the
// authenticated tenant request and the authoritative records; the status
// package remains responsible for filtering, freshness and deterministic
// ordering.
type ServedStatusSurface struct {
	Present        func(statuscontract.Request, []statuscontract.Service, []statuscontract.Incident, []statuscontract.Advisory) (statuscontract.Snapshot, error)
	RegistryMatrix func() []statuscontract.RegistryEntry
	Lookup         func(string) (statuscontract.RegistryEntry, bool)
}

// NewServedStatusSurface returns the tenant-scoped status and advisory
// projection reachable from a composed serving application.
func NewServedStatusSurface() ServedStatusSurface {
	return ServedStatusSurface{
		Present:        statuscontract.Present,
		RegistryMatrix: statuscontract.RegistryMatrix,
		Lookup:         statuscontract.Lookup,
	}
}

// Status returns the status surface exposed by a composed application. A nil
// application has no reachable capabilities.
func (a *App) Status() ServedStatusSurface {
	if a == nil {
		return ServedStatusSurface{}
	}
	return NewServedStatusSurface()
}
