package application

import "fmt"

// harborcareDemoTenant is the tenant a plain local run signs in to (devprofile
// serves it); ironridge-demo stays the default for the agent demo commands.
const (
	harborcareDemoTenant = "harborcare-demo"
	harborcareDemoAdmin  = "hc-050-rafael-torres"
)

// localAgentDemoTenants is the closed allowlist of workspaces the local agent
// demo may prepare, clean up and evaluate. Each has its own seeded HCM
// administrator. A tenant outside this list is refused everywhere, so the
// destructive cleanup and seeding can never reach a real tenant.
var localAgentDemoTenants = []string{localAgentDemoTenant, harborcareDemoTenant}

// localAgentDemoAdminFor returns the seeded administrator of an allowlisted
// demo workspace, or "" for every other tenant.
func localAgentDemoAdminFor(tenant string) string {
	switch tenant {
	case localAgentDemoTenant:
		return localAgentDemoAdmin
	case harborcareDemoTenant:
		return harborcareDemoAdmin
	}
	return ""
}

// isLocalAgentDemoTenant reports whether tenant is an allowlisted demo workspace.
func isLocalAgentDemoTenant(tenant string) bool { return localAgentDemoAdminFor(tenant) != "" }

// localAgentDemoTenantChoices names the allowlist for operator messages.
func localAgentDemoTenantChoices() string {
	return fmt.Sprintf("%s or %s", localAgentDemoTenants[0], localAgentDemoTenants[1])
}

// localAgentDemoPreparationCommand is the command that prepares tenant's agent demo.
func localAgentDemoPreparationCommand(tenant string) string {
	if !isLocalAgentDemoTenant(tenant) {
		tenant = localAgentDemoTenant
	}
	return "go run ./cmd/migrate agent-demo -profile local-dev -tenant " + tenant
}

// servedLocalAgentDemoTenant picks the demo workspace a local run serves:
// ironridge-demo when it is served (the long-standing default), otherwise the
// first served allowlisted tenant. It returns "" when none is served.
func servedLocalAgentDemoTenant(served []string) string {
	first := ""
	for _, tenant := range served {
		if tenant == localAgentDemoTenant {
			return tenant
		}
		if first == "" && isLocalAgentDemoTenant(tenant) {
			first = tenant
		}
	}
	return first
}

// localAdminEvaluationTenantAllowed is the local administrator evaluation's
// guard: the local-dev profile and an allowlisted demo workspace only.
func localAdminEvaluationTenantAllowed(profile, tenant string) bool {
	return profile == ServeProfileLocalDev && isLocalAgentDemoTenant(tenant)
}
