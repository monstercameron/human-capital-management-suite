package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

var (
	// ErrAgentModelGatewayNotConfigured marks a gateway missing a required
	// policy, router, dispatcher, adapter catalog, or pricing schedule.
	ErrAgentModelGatewayNotConfigured = errors.New("application: agent model gateway is not configured")
	// ErrAgentModelGatewayTenant marks a missing or mismatched tenant binding.
	ErrAgentModelGatewayTenant = errors.New("application: agent model gateway tenant binding is invalid")
	// ErrAgentModelGatewayPin marks a missing or mismatched immutable model pin.
	ErrAgentModelGatewayPin = errors.New("application: agent model gateway model pin is invalid")
	// ErrAgentModelGatewayLease marks a missing destination-scoped credential lease.
	ErrAgentModelGatewayLease = errors.New("application: agent model gateway credential lease is invalid")
	// ErrAgentModelGatewayPricing marks absent or mismatched authoritative pricing.
	ErrAgentModelGatewayPricing = errors.New("application: agent model gateway pricing is unavailable")
	// ErrAgentModelGatewayAdapter marks a missing adapter for the selected identity.
	ErrAgentModelGatewayAdapter = errors.New("application: agent model gateway adapter is unavailable")
)

// AgentModelGatewayConfig supplies every dependency needed for production
// model dispatch. No dependency has a default because defaults could widen
// provider, tenant, credential, or cost authority.
type AgentModelGatewayConfig struct {
	Router        *agentmodel.Router
	Egress        *agentegress.ProviderDispatcher
	Adapters      map[agentmodel.ModelSelection]agentmodel.ModelAdapter
	Pricing       *agentmodel.PricingSchedule
	Budget        agentmodel.Budget
	Resources     AgentModelResourceAdmission
	LeaseBindings *ModelLeaseSource
}

// AgentModelResourceRequest is derived from trusted dispatch state after the
// router selects the exact pinned provider. The resource authority must resolve
// the task's current user and execution lane before issuing capacity.
type AgentModelResourceRequest struct{ TenantID, UserID, TaskID, ProviderID, ModelProfileID string }
type AgentModelResourceAdmission interface {
	AcquireAgentModelResources(context.Context, AgentModelResourceRequest) (func(), error)
}

// AgentModelGateway composes routing, pricing, egress and one adapter call.
// It does not create provider sessions, execute tool proposals, or retain
// provider state as HCM run state.
type AgentModelGateway struct {
	router        *agentmodel.Router
	egress        *agentegress.ProviderDispatcher
	adapters      map[agentmodel.ModelSelection]agentmodel.ModelAdapter
	pricing       *agentmodel.PricingSchedule
	budget        agentmodel.Budget
	resources     AgentModelResourceAdmission
	leaseBindings *ModelLeaseSource
}

// NewAgentModelGateway validates the production composition. A nil or empty
// input is refused so local fakes and live provider defaults cannot be
// selected accidentally by an application caller.
func NewAgentModelGateway(cfg AgentModelGatewayConfig) (*AgentModelGateway, error) {
	if cfg.Router == nil || cfg.Egress == nil || cfg.Pricing == nil || cfg.Pricing.Digest == "" || cfg.Budget == nil || len(cfg.Adapters) == 0 {
		return nil, ErrAgentModelGatewayNotConfigured
	}
	adapters := make(map[agentmodel.ModelSelection]agentmodel.ModelAdapter, len(cfg.Adapters))
	for selection, adapter := range cfg.Adapters {
		if strings.TrimSpace(selection.ProfileID) == "" || strings.TrimSpace(selection.ProfileDigest) == "" || strings.TrimSpace(selection.Identity.ProviderID) == "" || strings.TrimSpace(selection.Identity.ModelID) == "" || strings.TrimSpace(selection.Identity.Version) == "" || adapter == nil {
			return nil, fmt.Errorf("%w: adapter identity is incomplete", ErrAgentModelGatewayNotConfigured)
		}
		identified, ok := adapter.(interface {
			Identity() agentmodel.ModelIdentity
		})
		if !ok || identified.Identity() != selection.Identity {
			return nil, fmt.Errorf("%w: adapter identity does not match its pinned selection", ErrAgentModelGatewayNotConfigured)
		}
		adapters[selection] = adapter
	}
	return &AgentModelGateway{router: cfg.Router, egress: cfg.Egress, adapters: adapters, pricing: cfg.Pricing, budget: cfg.Budget, resources: cfg.Resources, leaseBindings: cfg.LeaseBindings}, nil
}

// AgentModelGatewayRequest binds a tenant-owned model request to one pinned
// route and one already-minted provider credential lease.
type AgentModelGatewayRequest struct {
	TenantID string
	Route    agentmodel.RouteRequest
	Dispatch agentegress.ProviderDispatchRequest
}

// AgentModelGatewayResult contains the recorded route and normalized provider
// dispatch evidence. Provider wire responses remain inside agentmodel.
type AgentModelGatewayResult struct {
	Route    agentmodel.RouteRecord
	Dispatch agentegress.DispatchResult
}

// Dispatch routes and sends one model request after all stable policy inputs
// are present. A missing pin, tenant, pricing schedule, or lease produces no
// adapter call; the egress dispatcher additionally enforces lease single use.
func (g *AgentModelGateway) Dispatch(ctx context.Context, req AgentModelGatewayRequest) (AgentModelGatewayResult, error) {
	if g == nil || g.router == nil || g.egress == nil || g.pricing == nil || g.pricing.Digest == "" || g.budget == nil || len(g.adapters) == 0 {
		return AgentModelGatewayResult{}, ErrAgentModelGatewayNotConfigured
	}
	if ctx == nil {
		return AgentModelGatewayResult{}, fmt.Errorf("%w: context is required", ErrAgentModelGatewayTenant)
	}
	if err := ctx.Err(); err != nil {
		return AgentModelGatewayResult{}, err
	}
	if strings.TrimSpace(req.TenantID) == "" || req.Dispatch.Outbound.Tenant != req.TenantID {
		return AgentModelGatewayResult{}, ErrAgentModelGatewayTenant
	}
	if strings.TrimSpace(req.Route.TraceID) == "" || strings.TrimSpace(req.Route.Pin.AgentVersionDigest) == "" ||
		req.Route.Pin.Primary == (agentmodel.ModelSelection{}) || strings.TrimSpace(req.Route.Pin.TaskProfileID) == "" {
		return AgentModelGatewayResult{}, ErrAgentModelGatewayPin
	}
	if req.Dispatch.Model.Output.Mode == agentmodel.OutputSchema {
		digest := sha256.Sum256(req.Dispatch.Model.Output.Schema)
		if req.Route.Pin.OutputSchemaDigest != "sha256:"+hex.EncodeToString(digest[:]) {
			return AgentModelGatewayResult{}, ErrAgentModelGatewayPin
		}
	}
	if err := validateGatewayLease(req.Dispatch.Lease, req.TenantID, req.Dispatch.Outbound); err != nil {
		return AgentModelGatewayResult{}, err
	}
	if g.leaseBindings != nil {
		if err := g.leaseBindings.ValidateTaskBoundModelLease(ctx, req.TenantID, req.Dispatch.Outbound.TaskID, req.Route.Pin.AgentVersionDigest, req.Route.Pin.Primary.Identity.ProviderID, req.Dispatch.Lease); err != nil {
			return AgentModelGatewayResult{}, ErrAgentModelGatewayLease
		}
	}
	ctx = context.WithValue(ctx, openAIModelDispatchContextKey{}, openAIModelDispatchBinding{tenant: req.TenantID, runID: req.Dispatch.Outbound.TaskID, stepID: req.Route.TraceID})
	route, err := g.router.Route(ctx, req.Route)
	if err != nil {
		return AgentModelGatewayResult{}, err
	}
	if route.Selected == (agentmodel.ModelSelection{}) {
		return AgentModelGatewayResult{}, ErrAgentModelGatewayPin
	}
	if req.Dispatch.Model.ModelProfile != route.Selected.ProfileID || req.Dispatch.Model.TraceID != req.Route.TraceID {
		return AgentModelGatewayResult{}, ErrAgentModelGatewayPin
	}
	adapter, ok := g.adapters[route.Selected]
	if !ok || adapter == nil {
		return AgentModelGatewayResult{}, ErrAgentModelGatewayAdapter
	}
	if err := g.pricing.Validate(ctx, route.Selected, req.Dispatch.Model.Limits); err != nil {
		return AgentModelGatewayResult{}, fmt.Errorf("%w: %w", ErrAgentModelGatewayPricing, err)
	}
	deadline := time.Now().Add(req.Route.Task.MaxLatency)
	if req.Dispatch.Model.Deadline.Before(deadline) {
		deadline = req.Dispatch.Model.Deadline
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if g.resources != nil {
		release, err := g.resources.AcquireAgentModelResources(ctx, AgentModelResourceRequest{TenantID: req.TenantID, UserID: req.Dispatch.Outbound.Principal, TaskID: req.Dispatch.Outbound.TaskID, ProviderID: route.Selected.Identity.ProviderID, ModelProfileID: route.Selected.ProfileID})
		if err != nil {
			return AgentModelGatewayResult{}, err
		}
		if release == nil {
			return AgentModelGatewayResult{}, ErrAgentModelGatewayNotConfigured
		}
		defer release()
	}
	limits := req.Dispatch.Model.Limits
	if limits.MaxInputTokens < 0 || limits.MaxOutputTokens < 0 || limits.MaxInputTokens > math.MaxInt64-limits.MaxOutputTokens {
		return AgentModelGatewayResult{}, fmt.Errorf("%w: token budget is not representable", agentmodel.ErrBudgetFailed)
	}
	// Reserve the wall-clock time this call can actually use: the call is
	// cancelled at the deadline above, so it cannot consume the task's whole
	// contractual latency when less time than that is left.
	wallClock, err := agentModelReservationWallClock(deadline, req.Route.Task.MaxLatency, time.Now())
	if err != nil {
		return AgentModelGatewayResult{}, fmt.Errorf("%w: model deadline has passed", agentmodel.ErrBudgetFailed)
	}
	reservation, err := g.budget.Reserve(ctx, agentbudget.Request{TaskID: req.Dispatch.Outbound.TaskID, StepID: req.Route.TraceID, Fingerprint: req.Route.TraceID, Estimate: agentbudget.Usage{Steps: 1, Tokens: limits.MaxInputTokens + limits.MaxOutputTokens, SpendMicros: limits.MaxCostMicros, WallClock: wallClock}})
	if err != nil {
		return AgentModelGatewayResult{}, fmt.Errorf("%w: %w", agentmodel.ErrBudgetFailed, err)
	}
	started := time.Now()
	dispatched, dispatchErr := g.egress.Dispatch(ctx, req.Dispatch, adapter)
	// A refusal or normalized provider failure may still include billed tokens.
	// Charge authoritative observed usage before returning that outcome.
	if dispatched.LeaseID != "" || (dispatchErr == nil && dispatched.Model.Failure == nil && dispatched.Model.Refusal == nil) {
		if dispatched.Model.Usage.TotalTokens > 0 || (dispatchErr == nil && dispatched.Model.Failure == nil && dispatched.Model.Refusal == nil) {
			if _, err := g.pricing.Reconcile(route.Selected, dispatched.Model.Usage); err != nil {
				_ = reservation.Fail()
				return AgentModelGatewayResult{}, fmt.Errorf("%w: %w", ErrAgentModelGatewayPricing, err)
			}
		} else if dispatched.Model.Usage.CostMicros != 0 {
			_ = reservation.Fail()
			return AgentModelGatewayResult{}, ErrAgentModelGatewayPricing
		}
		usage := agentbudget.Usage{Steps: 1, Tokens: dispatched.Model.Usage.TotalTokens, SpendMicros: dispatched.Model.Usage.CostMicros, WallClock: time.Since(started)}
		if err := reservation.Settle(usage); err != nil {
			return AgentModelGatewayResult{}, fmt.Errorf("%w: %w", agentmodel.ErrBudgetFailed, err)
		}
	} else if err := reservation.Fail(); err != nil {
		return AgentModelGatewayResult{}, fmt.Errorf("%w: %w", agentmodel.ErrBudgetFailed, err)
	}
	if dispatchErr != nil {
		return AgentModelGatewayResult{}, dispatchErr
	}
	return AgentModelGatewayResult{Route: route, Dispatch: dispatched}, nil
}

func agentModelReservationWallClock(deadline time.Time, taskLatency time.Duration, now time.Time) (time.Duration, error) {
	left := deadline.Sub(now)
	if left <= 0 || taskLatency <= 0 {
		return 0, agentmodel.ErrBudgetFailed
	}
	if left < taskLatency {
		return left, nil
	}
	return taskLatency, nil
}

func validateGatewayLease(value lease.CredentialLease, tenant string, outbound agentegress.OutboundRequest) error {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.CustodyLeaseID) == "" || strings.TrimSpace(value.Workload) == "" ||
		strings.TrimSpace(value.Nonce) == "" || value.Operation != custody.Decrypt || strings.TrimSpace(value.Tenant) == "" || value.Tenant != tenant ||
		value.Destination != outbound.Profile.ID || value.Purpose != outbound.Purpose || value.ExpiresAt.IsZero() || value.IssuedAt.IsZero() || !value.ExpiresAt.After(value.IssuedAt) || value.Handle.Validate() != nil {
		return ErrAgentModelGatewayLease
	}
	return nil
}
