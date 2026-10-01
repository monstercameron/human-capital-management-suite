package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providercontract"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerdrift"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerexit"
)

// ServedProviderSurface exposes the selected-provider controls through the
// application package used by the hcmnext serve command. The connectivity
// packages remain the semantic owners; this value only makes their typed,
// deterministic operations reachable from the serving dependency closure.
type ServedProviderSurface struct {
	NewContractFixture      func(providercontract.Topology) (*providercontract.Fixture, error)
	CompileContractEvidence func(context.Context, *providercontract.Fixture) (providercontract.Evidence, error)
	ContractFromTopology    func(providercontract.Topology, string) (providerdrift.Contract, error)
	CompareProviderDrift    func(providerdrift.Contract, providerdrift.Contract) (providerdrift.Report, error)
	ReconcileProviderDrift  func(providerdrift.Report, providerdrift.Review) providerdrift.Report
	ReconcileProviderExit   func(providerexit.Request) (providerexit.Plan, error)
}

// NewServedProviderSurface returns the provider contract, drift and exit
// operations reachable from a composed serving process.
func NewServedProviderSurface() ServedProviderSurface {
	return ServedProviderSurface{
		NewContractFixture:      providercontract.NewFixture,
		CompileContractEvidence: providercontract.CompileEvidence,
		ContractFromTopology:    providerdrift.FromTopology,
		CompareProviderDrift:    providerdrift.Compare,
		ReconcileProviderDrift:  providerdrift.Reconcile,
		ReconcileProviderExit:   providerexit.Reconcile,
	}
}

// Provider returns the selected-provider controls exposed by a composed app.
// A nil application has no served capabilities, matching the other
// application boundary projections.
func (a *App) Provider() ServedProviderSurface {
	if a == nil {
		return ServedProviderSurface{}
	}
	return NewServedProviderSurface()
}
