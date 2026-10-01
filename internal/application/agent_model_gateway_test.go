package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

func TestTodo_AGENT_019_020_024_GatewayRequiresStableInputs(t *testing.T) {
	if _, err := NewAgentModelGateway(AgentModelGatewayConfig{}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("empty gateway error = %v, want ErrAgentModelGatewayNotConfigured", err)
	}
	if _, err := NewAgentModelGateway(AgentModelGatewayConfig{Pricing: &agentmodel.PricingSchedule{}}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("partial gateway error = %v, want ErrAgentModelGatewayNotConfigured", err)
	}
}

func TestTodo_AGENT_019_020_024_GatewayRefusesUnboundDispatch(t *testing.T) {
	var gateway *AgentModelGateway
	_, err := gateway.Dispatch(context.Background(), AgentModelGatewayRequest{TenantID: "tenant-1"})
	if !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("nil gateway error = %v, want ErrAgentModelGatewayNotConfigured", err)
	}

	// This request demonstrates the shape required at the integration splice;
	// it remains refused before any router or provider dependency can run.
	req := AgentModelGatewayRequest{
		TenantID: "tenant-1",
		Dispatch: agentegress.ProviderDispatchRequest{Outbound: agentegress.OutboundRequest{Tenant: "other-tenant"}},
	}
	if err := validateGatewayLease(req.Dispatch.Lease, req.TenantID, req.Dispatch.Outbound); err == nil {
		t.Fatal("unbound lease accepted, want fail-closed refusal")
	}
}
