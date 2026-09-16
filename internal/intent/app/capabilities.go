package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// bootstrapCapabilityVersion is the version every BOOTSTRAP capability is
// published at. Capability identity is (ID, Version); "latest" is not an
// identity, so the version is named rather than looked up.
const bootstrapCapabilityVersion uint32 = 1

// promotionMode selects which promotion answer a capability invocation wants.
type promotionMode string

const (
	promotionModePreflight promotionMode = "PREFLIGHT"
	promotionModeSimulate  promotionMode = "SIMULATE"
)

// promotionCall is the payload the promote_worker capability handler consumes.
type promotionCall struct {
	Mode    promotionMode
	Request promotion.PreflightRequest
}

// promotionAnswer is what it returns. Exactly one half is populated, selected
// by the requested mode.
type promotionAnswer struct {
	Preflight  promotion.PreflightResult
	Simulation promotion.SimulationResult
}

// ErrCapabilityUnbound is returned by a bootstrap capability that this cell
// publishes for discovery but does not implement. internal/domains/dataops,
// repair and intelligence own those answers; a cell that has not been given
// them refuses loudly rather than returning an echo that looks like a result.
var ErrCapabilityUnbound = errors.New("app: capability has no domain binding in this cell")

// domainHandlers binds the P1A capability table to the domain packages that
// own each answer.
type domainHandlers struct {
	workers people.WorkerFacts
	bands   rewards.PayBandCatalog
	// history and observations are the two sides of every cross-system
	// diagnostic: what this platform masters, and what the incumbent reports.
	history      dataops.FieldHistory
	observations dataops.ObservationReader
	// transactions is the recorded chronology explain_transaction rebuilds an
	// authorized account of.
	transactions intelligence.TransactionHistory
}

// handlerFor returns the handler bound to one capability id.
func (h *domainHandlers) handlerFor(id string) capability.Handler {
	switch id {
	case people.ExplainWorkerStateIntentType:
		return h.explainWorkerState
	case promotion.IntentType:
		return h.promoteWorker
	case rewards.SimulateCompensationIntentType:
		return h.simulateCompensation
	case rewards.EvaluatePayBandIntentType:
		return h.evaluatePayBandPosition
	case dataops.DetectDriftIntentType:
		return h.detectDrift
	case repair.CreateRepairPlanIntentType:
		return h.createRepairPlan
	case repair.SimulateRepairIntentType:
		return h.simulateRepair
	case intelligence.ExplainTransactionIntentType:
		return h.explainTransaction
	default:
		return func(context.Context, any) (any, error) {
			return nil, fmt.Errorf("%w: %s", ErrCapabilityUnbound, id)
		}
	}
}

func (h *domainHandlers) explainWorkerState(ctx context.Context, payload any) (any, error) {
	req, ok := payload.(people.ExplainWorkerStateRequest)
	if !ok {
		return nil, fmt.Errorf("app: explain_worker_state expects a people.ExplainWorkerStateRequest, got %T", payload)
	}
	return people.ExplainWorkerState(ctx, h.workers, req)
}

func (h *domainHandlers) promoteWorker(ctx context.Context, payload any) (any, error) {
	call, ok := payload.(promotionCall)
	if !ok {
		return nil, fmt.Errorf("app: promote_worker expects a promotionCall, got %T", payload)
	}
	switch call.Mode {
	case promotionModePreflight:
		result, err := promotion.PreflightPromotion(ctx, h.bands, call.Request)
		if err != nil {
			return nil, err
		}
		return promotionAnswer{Preflight: result}, nil
	case promotionModeSimulate:
		result, err := promotion.SimulatePromotion(ctx, h.bands, call.Request)
		if err != nil {
			return nil, err
		}
		return promotionAnswer{Preflight: result.Preflight, Simulation: result}, nil
	default:
		return nil, fmt.Errorf("app: promote_worker has no mode %q", call.Mode)
	}
}

func (h *domainHandlers) simulateCompensation(ctx context.Context, payload any) (any, error) {
	in, ok := payload.(rewards.SimulateCompensationInput)
	if !ok {
		return nil, fmt.Errorf("app: simulate_compensation expects a rewards.SimulateCompensationInput, got %T", payload)
	}
	return rewards.SimulateCompensation(ctx, h.bands, in)
}

func (h *domainHandlers) evaluatePayBandPosition(ctx context.Context, payload any) (any, error) {
	in, ok := payload.(PayBandInputs)
	if !ok {
		return nil, fmt.Errorf("app: evaluate_pay_band_position expects a PayBandInputs, got %T", payload)
	}
	return rewards.EvaluatePayBandPosition(ctx, h.bands, in.Query, in.Amount)
}

// newCapabilityRegistry republishes the compiled-in BOOTSTRAP capability table
// with this cell's real handlers bound to it.
//
// The definitions are taken from capability.NewBootstrapRegistry rather than
// restated here: the BOOTSTRAP table is the published contract (effect class,
// authorization scope, legal basis, entitlement, risk class), and a second
// copy of it in this package would be a copy that drifts. What this cell
// supplies is the missing half - the handler that answers - which the
// bootstrap registry deliberately leaves as an echo.
func newCapabilityRegistry(h *domainHandlers) (*capability.Registry, error) {
	published, err := capability.NewBootstrapRegistry()
	if err != nil {
		return nil, fmt.Errorf("app: read the bootstrap capability table: %w", err)
	}
	bound := capability.NewRegistry()
	for _, rec := range published.List() {
		if err := bound.Register(rec.Definition, h.handlerFor(rec.Definition.ID)); err != nil {
			return nil, fmt.Errorf("app: bind capability %s: %w", rec.Definition.Key(), err)
		}
	}
	return bound, nil
}

// capabilityKeyFor is the capability one intent definition is answered by. The
// P1A capability table is keyed by intent type id, so the mapping is identity
// rather than a lookup table that could disagree with the registry.
func capabilityKeyFor(ref intent.Ref) capability.Key {
	return capability.Key{ID: ref.TypeID, Version: bootstrapCapabilityVersion}
}

// authorize turns the admitted invocation into the already-made authorization
// decision the gateway requires.
//
// This is not an authorization engine and does not pretend to be one: the
// policy subsystems (GOVERN-001..003, TRUST-011) are not built, and inventing a
// weak second evaluator here is exactly the drift the capability package's own
// doc warns about. What the decision does encode is real and checkable: the
// request reached a verified principal, that principal is authorized for the
// purpose the invocation resolved, and the granted scope is the exact
// AuthZScopeRef the capability publishes - never a wildcard.
func authorize(principal *trust.Principal, purpose string, def capability.Definition) capability.Authorization {
	decision := capability.Authorization{
		Decision:   capability.Deny,
		SubjectRef: "",
		Reason:     "no verified principal",
	}
	if principal == nil {
		return decision
	}
	decision.SubjectRef = principal.Subject()
	decision.Tenant = principal.Tenant().String()
	if purpose != "" && !principal.AuthorizesPurpose(purpose) {
		decision.Reason = "the principal is not authorized for the resolved purpose of processing"
		return decision
	}
	decision.Decision = capability.Allow
	decision.Reason = ""
	decision.Scopes = []string{def.AuthZScopeRef}
	return decision
}
