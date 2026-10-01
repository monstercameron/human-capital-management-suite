package cell

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type agentSettingsStub struct{}

func (agentSettingsStub) AgentsEnabled(context.Context, values.TenantId) (bool, error) {
	return false, nil
}

func (agentSettingsStub) SetAgentsEnabled(context.Context, values.TenantId, bool, string) error {
	return nil
}

func TestRegisterAgentsPublishesOnlyWithASetting(t *testing.T) {
	without := grpc.NewServer()
	registerAgents(without, &app.Cell{})
	registerAgents(nil, &app.Cell{AgentSettings: agentSettingsStub{}})
	registerAgents(without, nil)
	if _, ok := without.GetServiceInfo()["hcmnext.agent.v1.AgentService"]; ok {
		t.Fatal("a cell with no agents setting exposed the agent service")
	}
	with := grpc.NewServer()
	registerAgents(with, &app.Cell{AgentSettings: agentSettingsStub{}})
	if _, ok := with.GetServiceInfo()["hcmnext.agent.v1.AgentService"]; !ok {
		t.Fatal("the agent service was not registered")
	}
}
