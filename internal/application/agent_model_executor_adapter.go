package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

var (
	// ErrAgentModelExecutorNotConfigured marks an adapter without its gateway.
	ErrAgentModelExecutorNotConfigured = errors.New("application: agent model executor adapter is not configured")
	// ErrAgentModelExecutorBinding marks model inputs not derived from one trusted task.
	ErrAgentModelExecutorBinding = errors.New("application: agent model executor task binding is invalid")
)

// AgentModelExecutorRequest is the trusted-run projection consumed by the
// executor adapter. The caller must resolve every field from durable run state;
// this adapter never fills model, profile, budget, tenant, or agent defaults.
type AgentModelExecutorRequest struct {
	Task         TrustedModelTask
	StepID       string
	ToolResultClass trustdlp.DataClass
	Route        agentmodel.RouteRequest
	Model        agentmodel.ModelRequest
	Outbound     agentegress.OutboundRequest
	FieldSources map[string]string
	Lease        lease.CredentialLease
}

// AgentModelExecutorResult preserves the route and normalized provider result
// while keeping provider wire details behind the agentmodel contract.
type AgentModelExecutorResult struct {
	Route  agentmodel.RouteRecord
	Result agentmodel.ModelResult
}

// AgentModelExecutorAdapter is the only application bridge from trusted agent
// run state to AgentModelGateway. It has no legacy SchemaFlux fallback.
type AgentModelExecutorAdapter struct {
	gateway *AgentModelGateway
}

// NewAgentModelExecutorAdapter requires a fully composed production gateway.
func NewAgentModelExecutorAdapter(gateway *AgentModelGateway) (*AgentModelExecutorAdapter, error) {
	if gateway == nil {
		return nil, ErrAgentModelExecutorNotConfigured
	}
	return &AgentModelExecutorAdapter{gateway: gateway}, nil
}

// Execute dispatches one trusted task model step through the pinned gateway.
// Missing or mismatched task, model, route, budget, tenant, agent, or lease
// inputs are refused before route recording or provider dispatch.
func (a *AgentModelExecutorAdapter) Execute(ctx context.Context, req AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	if a == nil || a.gateway == nil {
		return AgentModelExecutorResult{}, ErrAgentModelExecutorNotConfigured
	}
	if err := validateExecutorRequest(req); err != nil {
		return AgentModelExecutorResult{}, err
	}
	result, err := a.gateway.Dispatch(ctx, AgentModelGatewayRequest{
		TenantID: req.Task.TenantID,
		Route:    req.Route,
		Dispatch: agentegress.ProviderDispatchRequest{
			Model: req.Model, Outbound: req.Outbound, FieldSources: req.FieldSources, Lease: req.Lease,
		},
	})
	if err != nil {
		return AgentModelExecutorResult{}, err
	}
	return AgentModelExecutorResult{Route: result.Route, Result: result.Dispatch.Model}, nil
}

func validateExecutorRequest(req AgentModelExecutorRequest) error {
	if err := validateTrustedTask(req.Task); err != nil {
		return fmt.Errorf("%w: %v", ErrAgentModelExecutorBinding, err)
	}
	stepID := req.StepID
	if stepID == "" {
		stepID = req.Task.TaskID
	}
	if strings.TrimSpace(stepID) != stepID || stepID == "" || req.Route.TraceID != stepID ||
		req.Model.TraceID != stepID || req.Outbound.TaskID != req.Task.TaskID || req.Outbound.Tenant != req.Task.TenantID {
		return ErrAgentModelExecutorBinding
	}
	if req.Route.Pin.AgentVersionDigest == "" || req.Route.Pin.AgentVersionDigest != req.Task.AgentID ||
		req.Route.Task.AgentVersionDigest != req.Task.AgentID || req.Route.Task.ID == "" || req.Route.Pin.TaskProfileID != req.Route.Task.ID {
		return ErrAgentModelExecutorBinding
	}
	if req.Model.TaskProfile != req.Route.Task.ID || req.Model.ModelProfile == "" || req.Model.Limits.MaxCostMicros <= 0 ||
		req.Route.Task.MaxCostMicros <= 0 || req.Model.Limits.MaxCostMicros != req.Route.Task.MaxCostMicros {
		return ErrAgentModelExecutorBinding
	}
	if strings.TrimSpace(req.Outbound.Purpose) == "" || strings.TrimSpace(req.Outbound.Region) == "" || req.Outbound.Profile.Kind != agentegress.TargetModel {
		return ErrAgentModelExecutorBinding
	}
	if req.Lease.Tenant != req.Task.TenantID || req.Lease.Purpose != req.Outbound.Purpose || req.Lease.Destination != req.Outbound.Profile.ID {
		return ErrAgentModelExecutorBinding
	}
	return nil
}
