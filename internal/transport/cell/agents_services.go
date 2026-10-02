package cell

import (
	"context"

	"google.golang.org/grpc"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	transportagents "github.com/monstercameron/human-capital-management-suite/internal/transport/agents"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// registerAgents mounts the Agents page write service (UXBLIND-122) on the
// already-admitted native gRPC server, so the browser reaches it over the same
// workspace tunnel as every other page. It registers nothing on a cell with no
// agents setting. The administrator decision is the workspace's own durable
// role check for the Chat settings page, not a second policy.
func registerAgents(server *grpc.Server, c *app.Cell) {
	if server == nil || c == nil || c.AgentSettings == nil {
		return
	}
	roles := c.RoleAccess
	var tasks transportagents.TaskReader
	if reader, ok := c.AgentStarter.(transportagents.TaskReader); ok {
		tasks = reader
	}
	transportagents.Register(server, transportagents.Dependencies{
		Settings: c.AgentSettings, Controller: c.AgentController,
		Admin: func(ctx context.Context, principal *trust.Principal) (bool, error) {
			return workspace.CanChangeAgentsSetting(ctx, roles, principal)
		},
		Starter: c.AgentStarter, Tasks: tasks,
	})
}
