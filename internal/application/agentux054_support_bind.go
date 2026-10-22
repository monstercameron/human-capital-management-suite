package application

import (
	"path/filepath"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// localAgentDemoSupportAgent is the chat identity Support Desk posts its alerts
// under: the handle its persona is registered with.
const localAgentDemoSupportAgent = "support-desk"

// localAgentDemoObjectRoot is where the local cell keeps the support desk's
// stored emails when no artifact root is configured.
const localAgentDemoObjectRoot = ".artifacts/lanes/agent-dev"

// BindAgentUXSupportDesk mounts the owner's "Simulate a customer email" at
// /api/agent-controls/support-inbox/simulate (AGENTUX-054). It is for local
// development only: any other profile mounts nothing, and the path is answered
// by the agent controls as before. The inbox, the stored objects, the owner
// check and the rules classifier are the product's own; the ticket's project and
// the incident channel come from targets, and a cell that has not composed them
// stores the email and answers that the desk is unavailable instead of
// promising a ticket.
func (s *agentServedAssembly) BindAgentUXSupportDesk(profile string, agents *agentstore.Store, chat *chatstore.Store, targets AgentUXDemoSupportTargetSource, projects AgentUXDemoSupportProject, now func() time.Time) error {
	if s == nil || profile != ServeProfileLocalDev || agents == nil || now == nil {
		return nil
	}
	inbox, err := agentstore.NewSupportInboxStore(agents)
	if err != nil {
		return err
	}
	objects := AgentUXDemoLocalSupportObjects{Root: filepath.FromSlash(localAgentDemoObjectRoot)}
	desk := &AgentUXDemoSupportDesk{Inbox: inbox, Journal: inbox, Objects: objects, Planner: AgentUXDemoLocalSupportPlanner{}, Targets: targets, Projects: projects, Now: now}
	if chat != nil {
		desk.Alerts = AgentUXDemoChatSupportAlerts{Chat: chat, Agent: localAgentDemoSupportAgent}
	}
	s.agentUX.SupportInbox = AgentUXDemoInboxSurface{Inbox: inbox, Objects: objects, Queue: AgentUXDemoSupportQueue{Desk: desk}, Owners: AgentUXDemoSupportOwner{}, TenantUUID: tenantKeyMapper[values.TenantId](pgstore.TenantID), Now: now}
	return nil
}
