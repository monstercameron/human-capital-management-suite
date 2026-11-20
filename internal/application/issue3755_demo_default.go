package application

import "fmt"

// LocalAgentDemoDefaultTenant is the workspace agent-demo prepares when no
// tenant is named: the standard Harborcare demo a plain local run signs in to,
// so one command is enough to get an agent placed in Chat. It is on the closed
// demo allowlist, so the default can never reach a real tenant.
func LocalAgentDemoDefaultTenant() string { return harborcareDemoTenant }

// LocalAgentDemoWhereToLook tells the person who ran agent-demo, in plain
// words, where the prepared agent now is in Chat and what to try. It names the
// workspace and its signed-in administrator, and says nothing for a tenant
// that is not an allowlisted demo workspace.
func LocalAgentDemoWhereToLook(tenant string) string {
	admin := localAgentDemoAdminFor(tenant)
	if admin == "" {
		return ""
	}
	return fmt.Sprintf("Where to look in %s, signed in as %s: Policy Helper is placed in #general and in your direct message with it. Type @ to mention it, or /ask in either place, then watch the working indicator, the answer with its sources, and a failure card when the model cannot answer.", tenant, admin)
}
