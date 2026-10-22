package application

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENTUX_053_CleanupCommand_Security(t *testing.T) {
	loopback := "postgres://local@127.0.0.1:5432/hcm?sslmode=disable"
	valid := LocalAgentDemoCleanupConfig{Profile: ServeProfileLocalDev, Tenant: localAgentDemoTenant, DatabaseURL: loopback, ChatDatabaseURL: loopback}
	if err := ValidateLocalAgentDemoCleanupConfig(valid); err != nil {
		t.Fatalf("local config refused: %v", err)
	}
	for name, mutate := range map[string]func(*LocalAgentDemoCleanupConfig){
		"production profile": func(c *LocalAgentDemoCleanupConfig) { c.Profile = ServeProfileStandard },
		"other tenant":       func(c *LocalAgentDemoCleanupConfig) { c.Tenant = "harborcare-demo" },
		"remote chat":        func(c *LocalAgentDemoCleanupConfig) { c.ChatDatabaseURL = "postgres://chat@db.internal:5432/chat" },
		"remote core":        func(c *LocalAgentDemoCleanupConfig) { c.DatabaseURL = "postgres://core@db.internal:5432/core" },
	} {
		changed := valid
		mutate(&changed)
		if err := ValidateLocalAgentDemoCleanupConfig(changed); err == nil {
			t.Fatalf("%s accepted", name)
		}
		if _, err := RunLocalAgentDemoCleanup(t.Context(), changed); err == nil {
			t.Fatalf("%s ran", name)
		}
	}
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	authors := localAgentDemoCleanupAuthors{now: func() time.Time { return now }, organization: "organization"}
	admin, err := authors.DemoPostAuthor(t.Context(), localAgentDemoTenant, localAgentDemoAdmin)
	if err != nil || admin.SubjectKind() != trust.SubjectKindHuman || !admin.HasRole("hcm_admin") {
		t.Fatalf("administrator author: %v %v", admin, err)
	}
	agent, err := authors.DemoPostAuthor(t.Context(), localAgentDemoTenant, localAgentDemoAgentID)
	if err != nil || agent.SubjectKind() != trust.SubjectKindAgent || agent.HasRole("hcm_admin") || len(agent.Roles()) != 0 {
		t.Fatalf("agent author borrows roles: %v %v", agent, err)
	}
	for _, bad := range []struct{ tenant, subject string }{{"harborcare-demo", localAgentDemoAdmin}, {localAgentDemoTenant, "ir-008-curtis-bell"}, {localAgentDemoTenant, "assistant"}} {
		if _, err := authors.DemoPostAuthor(t.Context(), bad.tenant, bad.subject); err == nil {
			t.Fatalf("author %+v accepted", bad)
		}
	}
}

// The owner decides when the old test posts go: nothing the preparation runs
// reaches the cleanup.
func TestTodo_AGENTUX_053_PreparationNeverDeletes(t *testing.T) {
	for _, name := range []string{"agentux_runtime_prepare.go", "agentux_general_prepare.go", "agentux049_prepare_tail.go", "agentux049_prepare_entry.go", "agentux049_placements.go", "agentux053_general_history.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"AgentUXDemoGeneralCleanup", "RunLocalAgentDemoCleanup", "DeletePost", "tombstone", "DELETE FROM"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("%s mentions %q: the preparation must never delete", name, forbidden)
			}
		}
	}
}
