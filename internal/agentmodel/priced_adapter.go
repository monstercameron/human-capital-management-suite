package agentmodel

import "context"

// PricedAdapter attaches approved token cost to a provider's normalized result.
// Typed generation remains in the owning SchemaFlux gateway above this adapter.
type PricedAdapter struct {
	adapter   ModelAdapter
	selection ModelSelection
	pricing   *PricingSchedule
}

func NewPricedAdapter(adapter ModelAdapter, selection ModelSelection, pricing *PricingSchedule) (*PricedAdapter, error) {
	typed, err := NewSchemaFluxAdapter(adapter, selection, pricing)
	if err != nil {
		return nil, err
	}
	return &PricedAdapter{adapter: typed.adapter, selection: typed.selection, pricing: typed.pricing}, nil
}
func (a *PricedAdapter) Identity() ModelIdentity {
	if a == nil {
		return ModelIdentity{}
	}
	return a.selection.Identity
}
func (a *PricedAdapter) Capabilities() AdapterCapabilities {
	if a == nil || a.adapter == nil {
		return AdapterCapabilities{}
	}
	return a.adapter.Capabilities()
}
func (a *PricedAdapter) Invoke(ctx context.Context, request ModelRequest) (ModelResult, error) {
	if a == nil || a.adapter == nil || a.pricing == nil {
		return ModelResult{}, ErrNotConfigured
	}
	if request.ModelProfile != a.selection.ProfileID {
		return ModelResult{}, ErrInvalidModelRequest
	}
	if err := a.pricing.Validate(ctx, a.selection, request.Limits); err != nil {
		return ModelResult{}, err
	}
	result, err := a.adapter.Invoke(ctx, request)
	if result.Usage.TotalTokens > 0 {
		cost, costErr := a.pricing.Cost(a.selection, result.Usage)
		if costErr != nil {
			return ModelResult{}, costErr
		}
		result.Usage.CostMicros = cost
	}
	return result, err
}
