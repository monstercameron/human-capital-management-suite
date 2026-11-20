package application

import (
	"strings"
	"testing"
)

func TestIssue3755_DefaultTenantIsTheStandardDemoAndIsAllowlisted_Security(t *testing.T) {
	tenant := LocalAgentDemoDefaultTenant()
	if tenant != "harborcare-demo" {
		t.Fatalf("default tenant = %q, want the standard Harborcare demo", tenant)
	}
	if err := ValidateLocalAgentDemoConfig(issue3828Config(tenant)); err != nil {
		t.Fatalf("the default tenant must be accepted: %v", err)
	}
	if err := ValidateLocalAgentDemoConfig(issue3828Config("")); err == nil {
		t.Fatal("the application layer still refuses an empty tenant; only the command defaults it")
	}
}

func TestIssue3755_WhereToLookNamesPlacementsAndEntryPoints(t *testing.T) {
	line := LocalAgentDemoWhereToLook("harborcare-demo")
	for _, want := range []string{"harborcare-demo", "hc-050-rafael-torres", "#general", "direct message", "@", "/ask", "working indicator", "sources", "failure"} {
		if !strings.Contains(line, want) {
			t.Fatalf("guidance %q lacks %q", line, want)
		}
	}
	if LocalAgentDemoWhereToLook("acme-demo") != "" {
		t.Fatal("a tenant outside the demo allowlist gets no guidance")
	}
}
