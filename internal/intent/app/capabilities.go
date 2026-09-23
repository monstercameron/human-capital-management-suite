package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/capability/authority"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
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
	// IntentID identifies the simulated intent. It is carried so the
	// SIMULATE branch can bind the governed simulation contract to the
	// intent it describes; empty on paths that never resolve one.
	IntentID string
	// Simulations are the resolve-time governed simassign/simcomp results
	// for a position-bound proposal. Nil selects the legacy direct
	// simulation: the handler still answers, but assembles no governed
	// contract.
	Simulations *PromotionSimulations
	// ControlSnapshotDigest is the control snapshot the simulation runs
	// under, cited by the governed contract's revalidation section.
	ControlSnapshotDigest string
	// RevalidationRule is the intent definition's revalidation rule the
	// governed contract reruns at execution time.
	RevalidationRule string
}

// promotionAnswer is what it returns. Exactly one half is populated, selected
// by the requested mode; Contract carries the governed simulation contract
// when the SIMULATE branch assembled one.
type promotionAnswer struct {
	Preflight  promotion.PreflightResult
	Simulation promotion.SimulationResult
	Contract   simcontract.SimulationResult
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
		answer := promotionAnswer{Preflight: result.Preflight, Simulation: result}
		if call.Simulations != nil {
			contract, err := assemblePromotionContract(promotionContractInput{
				IntentID:              call.IntentID,
				Simulations:           call.Simulations,
				Preflight:             result.Preflight,
				ControlSnapshotDigest: call.ControlSnapshotDigest,
				RevalidationRule:      call.RevalidationRule,
			})
			if err != nil {
				return nil, err
			}
			answer.Contract = contract
		}
		return answer, nil
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
// The decision itself is computed by the capability authority engine
// (internal/capability/authority): the intersection of the caller's granted
// scopes, the capability's scope and the field policy, over server-verified
// delegation, with step-up or dual approval gating high-risk writes. This
// adapter passes no server-resolved authority yet — no durable role,
// delegation, scope or step-up store is reachable from these call sites, so
// the engine evaluates the credential's own authority exactly as before for
// humans, while machine-kind calls and high-risk writes now fail closed
// until INTAPI-001 resolves their grants. Wiring a store only adds fields
// to the Authority value; no call site changes shape.
func authorize(principal *trust.Principal, purpose string, def capability.Definition) capability.Authorization {
	return authority.Authorize(principal, purpose, def, authority.Authority{}, time.Now().UTC())
}
