package application

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const issue3828Loopback = "postgres://local@127.0.0.1:5432/hcm?sslmode=disable"

func issue3828Config(tenant string) LocalAgentDemoConfig {
	l := issue3828Loopback
	return LocalAgentDemoConfig{Profile: ServeProfileLocalDev, Tenant: tenant, DatabaseURL: l, AgentDatabaseURL: l, ChatDatabaseURL: l, DocumentDatabaseURL: l, ReviewAuthorityDatabaseURL: l, PolicyAuthorityDatabaseURL: l, RouteAuthorityDatabaseURL: l, EvaluationProvisionerDatabaseURL: l}
}

func TestIssue3828_AgentDemoAcceptsBothDemoTenants(t *testing.T) {
	for _, tenant := range []string{"ironridge-demo", "harborcare-demo"} {
		if err := ValidateLocalAgentDemoConfig(issue3828Config(tenant)); err != nil {
			t.Fatalf("%s refused: %v", tenant, err)
		}
		if err := ValidateLocalAgentDemoCleanupConfig(LocalAgentDemoCleanupConfig{Profile: ServeProfileLocalDev, Tenant: tenant, DatabaseURL: issue3828Loopback, ChatDatabaseURL: issue3828Loopback}); err != nil {
			t.Fatalf("cleanup for %s refused: %v", tenant, err)
		}
	}
}

func TestIssue3828_AgentDemoRefusesEveryOtherTenant_Security(t *testing.T) {
	for _, tenant := range []string{"", "acme", "acme-demo", "Harborcare-Demo", "harborcare-demo ", "harborcare-demo,ironridge-demo"} {
		err := ValidateLocalAgentDemoConfig(issue3828Config(tenant))
		if err == nil {
			t.Fatalf("tenant %q accepted for preparation", tenant)
		}
		if !strings.Contains(err.Error(), "ironridge-demo") || !strings.Contains(err.Error(), "harborcare-demo") {
			t.Fatalf("refusal %q does not name the allowed workspaces", err)
		}
		if ValidateLocalAgentDemoCleanupConfig(LocalAgentDemoCleanupConfig{Profile: ServeProfileLocalDev, Tenant: tenant, DatabaseURL: issue3828Loopback, ChatDatabaseURL: issue3828Loopback}) == nil {
			t.Fatalf("tenant %q accepted for cleanup", tenant)
		}
		if isLocalAgentDemoTenant(tenant) || localAgentDemoAdminFor(tenant) != "" {
			t.Fatalf("tenant %q is in the allowlist", tenant)
		}
	}
	bad := issue3828Config("harborcare-demo")
	bad.Profile = ServeProfileStandard
	if ValidateLocalAgentDemoConfig(bad) == nil {
		t.Fatal("harborcare accepted outside the local-dev profile")
	}
	bad = issue3828Config("harborcare-demo")
	bad.ChatDatabaseURL = "postgres://x@db.internal:5432/hcm"
	if ValidateLocalAgentDemoConfig(bad) == nil {
		t.Fatal("harborcare accepted with a remote database")
	}
}

func TestIssue3828_EachDemoTenantHasItsOwnAdministrator(t *testing.T) {
	if got := localAgentDemoAdminFor("harborcare-demo"); got != "hc-050-rafael-torres" {
		t.Fatalf("harborcare administrator = %q", got)
	}
	if got := localAgentDemoAdminFor("ironridge-demo"); got != "ir-001-walt-brennan" {
		t.Fatalf("ironridge administrator = %q", got)
	}
}

func TestIssue3828_RemediationNamesTheServedDemoTenant(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	env := func(string) string { return "configured" }
	cases := []struct {
		name   string
		cfg    ServeConfig
		wantIn string
	}{
		{"default", ServeConfig{}, "-tenant ironridge-demo"},
		{"harborcare only", ServeConfig{Tenant: "harborcare-demo"}, "-tenant harborcare-demo"},
		{"both served keeps the default", ServeConfig{Tenant: "harborcare-demo", Tenants: "ironridge-demo"}, "-tenant ironridge-demo"},
		{"unrelated tenant falls back", ServeConfig{Tenant: "acme"}, "-tenant ironridge-demo"},
	}
	for _, tc := range cases {
		cfg := tc.cfg
		cfg.Profile, cfg.AgentModelConfigFile = ServeProfileLocalDev, missing
		if reason := personaServedProviderUnavailableReason(cfg, env); !strings.Contains(reason, tc.wantIn) {
			t.Fatalf("%s: reason %q lacks %q", tc.name, reason, tc.wantIn)
		}
	}
}

func TestIssue3828_LocalAdminEvaluationRunsForHarborcare(t *testing.T) {
	if got := servedLocalAgentDemoTenant([]string{"harborcare-demo"}); got != "harborcare-demo" {
		t.Fatalf("served harborcare picked %q", got)
	}
	if got := servedLocalAgentDemoTenant([]string{"harborcare-demo", "ironridge-demo"}); got != "ironridge-demo" {
		t.Fatalf("both served picked %q", got)
	}
	if got := servedLocalAgentDemoTenant([]string{"acme"}); got != "" {
		t.Fatalf("unrelated tenant picked %q", got)
	}
	// The runner's own guard accepts a demo tenant and refuses others before it
	// touches any key or store.
	var w *personaServeWiring
	if _, err := w.prepareLocalAdminEvaluation(nil, ServeProfileLocalDev, "acme", time.Now); err == nil {
		t.Fatal("unrelated tenant accepted")
	}
	if !localAdminEvaluationTenantAllowed(ServeProfileLocalDev, "harborcare-demo") || localAdminEvaluationTenantAllowed(ServeProfileStandard, "harborcare-demo") || localAdminEvaluationTenantAllowed(ServeProfileLocalDev, "acme") {
		t.Fatal("evaluation tenant guard is wrong")
	}
}

func TestIssue3828_CleanupAuthorsServeBothDemoWorkspaces_Security(t *testing.T) {
	authors := localAgentDemoCleanupAuthors{now: func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }, organization: "org:harborcare-demo:people-ops"}
	admin, err := authors.DemoPostAuthor(context.Background(), "harborcare-demo", "hc-050-rafael-torres")
	if err != nil || admin == nil || admin.Tenant().String() != "harborcare-demo" {
		t.Fatalf("harborcare administrator author = %v, %v", admin, err)
	}
	if _, err := authors.DemoPostAuthor(context.Background(), "harborcare-demo", "ir-001-walt-brennan"); err == nil {
		t.Fatal("another workspace's administrator accepted as an author")
	}
	if _, err := authors.DemoPostAuthor(context.Background(), "ironridge-demo", "hc-050-rafael-torres"); err == nil {
		t.Fatal("harborcare administrator accepted in ironridge")
	}
	if _, err := authors.DemoPostAuthor(context.Background(), "acme", "hc-050-rafael-torres"); err == nil {
		t.Fatal("unrelated tenant accepted")
	}
}
