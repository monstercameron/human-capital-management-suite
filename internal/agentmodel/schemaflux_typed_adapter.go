package agentmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	schemaflux "github.com/monstercameron/schemaflux"
)

// SchemaFluxTypedAdapter generates an owned structured business type. The
// request's approved output contract must match SchemaFlux's Go-derived schema.
// It carries no authority or model defaults and each invocation owns its client.
type SchemaFluxTypedAdapter[T any] struct {
	*SchemaFluxAdapter
}

func NewSchemaFluxTypedAdapter[T any](provider ModelAdapter, selection ModelSelection, pricing *PricingSchedule) (*SchemaFluxTypedAdapter[T], error) {
	base, err := NewSchemaFluxAdapter(provider, selection, pricing)
	if err != nil {
		return nil, err
	}
	var value T
	typeOf := reflect.TypeOf(value)
	if typeOf == nil || typeOf.Kind() != reflect.Struct {
		return nil, ErrNotConfigured
	}
	return &SchemaFluxTypedAdapter[T]{SchemaFluxAdapter: base}, nil
}

func (a *SchemaFluxTypedAdapter[T]) Capabilities() AdapterCapabilities {
	if a == nil || a.SchemaFluxAdapter == nil || a.adapter == nil {
		return AdapterCapabilities{}
	}
	return AdapterCapabilities{ContractVersions: []int{ContractVersion}, Features: []ModelFeature{FeatureStructuredJSON}, OutputModes: []OutputMode{OutputSchema}}
}

func (a *SchemaFluxTypedAdapter[T]) Invoke(ctx context.Context, req ModelRequest) (ModelResult, error) {
	if a == nil || a.SchemaFluxAdapter == nil || a.adapter == nil || ctx == nil {
		return ModelResult{}, ErrNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return ModelResult{ContractVersion: ContractVersion, Provider: a.Identity(), Finish: FinishCancelled}, err
	}
	if err := ValidateModelRequest(req); err != nil {
		return ModelResult{}, err
	}
	if refusal := CheckCapabilities(req, a.Capabilities()); refusal != nil {
		return ModelResult{}, refusal
	}
	if req.ModelProfile != a.selection.ProfileID || req.Output.Mode != OutputSchema || len(req.Tools) != 0 {
		return ModelResult{}, ErrInvalidModelRequest
	}
	provider := &typedInferenceProvider{adapter: a.adapter, request: req, identity: a.selection.Identity, validate: validateTypedDispatchOutput[T], bindSchema: true}
	client := schemaflux.NewClient("").WithProviderInstance(provider).WithRetries(0)
	generated, err := schemaflux.Generating[T]("Return only the requested business material matching the approved schema. Authority, approval and provenance are supplied by HCM, never by this model.").Strict().Model(a.selection.Identity.ModelID).RunResult(client.Context(ctx))
	result := provider.result
	if result.Usage.TotalTokens > 0 {
		cost, costErr := a.pricing.Cost(a.selection, result.Usage)
		if costErr != nil {
			return ModelResult{}, costErr
		}
		result.Usage.CostMicros = cost
	}
	if err != nil {
		if ctx.Err() != nil {
			return ModelResult{ContractVersion: ContractVersion, Provider: a.Identity(), Finish: FinishCancelled, Usage: result.Usage}, ctx.Err()
		}
		if result.Refusal != nil {
			if validateErr := ValidateModelResult(req, result, a.Capabilities()); validateErr != nil {
				return ModelResult{}, validateErr
			}
			return result, nil
		}
		if result.Failure == nil {
			result = ModelResult{ContractVersion: ContractVersion, Provider: a.Identity(), Usage: result.Usage, Failure: &ModelFailure{Code: FailureInvalid, Message: "typed model generation failed"}}
		}
		return result, fmt.Errorf("agentmodel: typed generation failed")
	}
	structured, err := json.Marshal(generated.Value)
	if err != nil {
		return ModelResult{}, ErrInvalidModelResult
	}
	result.Text, result.Structured, result.ToolProposals = "", structured, nil
	if err := ValidateModelResult(req, result, a.Capabilities()); err != nil {
		return ModelResult{}, err
	}
	return result, nil
}

// Object key order is immaterial, while required lists and all constraints stay
// exact. No schema clause may be dropped when switching to the typed provider.
func sameTypedSchema(approved, generated json.RawMessage) bool {
	var left, right any
	return json.Unmarshal(approved, &left) == nil && json.Unmarshal(generated, &right) == nil && reflect.DeepEqual(left, right)
}
