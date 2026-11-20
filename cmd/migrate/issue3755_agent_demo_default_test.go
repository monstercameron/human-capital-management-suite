package main

import "testing"

func TestIssue3755_AgentDemoDefaultsToTheStandardDemoTenant(t *testing.T) {
	spec := agentDemoSpec(nil)
	for _, f := range spec.ConfigFields {
		if f.Name == fieldTenant {
			if f.Default != "harborcare-demo" {
				t.Fatalf("agent-demo -tenant default = %q, want harborcare-demo", f.Default)
			}
			return
		}
	}
	t.Fatal("agent-demo has no -tenant field")
}

func TestIssue3755_CleanupStillRequiresAnExplicitTenant_Security(t *testing.T) {
	for _, f := range agentDemoCleanupSpec(nil).ConfigFields {
		if f.Name == fieldTenant && f.Default != "" {
			t.Fatalf("the destructive cleanup must not default its tenant, got %q", f.Default)
		}
	}
}
