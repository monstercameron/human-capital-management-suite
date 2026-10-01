package manifest

import (
	"strings"
	"testing"
)

func TestTodo_PM_066(t *testing.T) {
	m := AdvancedProjectEndpoints()
	if err := ValidateAdvancedProjectManifest(m); err != nil {
		t.Fatal(err)
	}
	seen := map[AdvancedBoardCapability]bool{}
	for _, endpoint := range m.Endpoints {
		seen[endpoint.Capability] = true
		if endpoint.Version != AdvancedProjectManifestVersion || !strings.HasPrefix(endpoint.GRPCProcedure, "/hcmnext.project.v1.ProjectService/") || !strings.HasPrefix(endpoint.HTTPPath, "/v1/projects/") {
			t.Fatalf("advanced endpoint is not versioned and dual-transport: %+v", endpoint)
		}
	}
	for _, capability := range []AdvancedBoardCapability{AdvancedCapabilityWIP, AdvancedCapabilityCycles, AdvancedCapabilityDependencies, AdvancedCapabilityMilestones, AdvancedCapabilityMigrations, AdvancedCapabilityImports} {
		if !seen[capability] {
			t.Fatalf("advanced capability %q has no manifest operation", capability)
		}
	}
}

func TestTodo_PM_066_Conformance(t *testing.T) {
	m := AdvancedProjectEndpoints()
	if len(m.Endpoints) != 15 {
		t.Fatalf("advanced endpoint count = %d, want 15", len(m.Endpoints))
	}
	for i := 1; i < len(m.Endpoints); i++ {
		if m.Endpoints[i-1].GRPCProcedure >= m.Endpoints[i].GRPCProcedure {
			t.Fatalf("advanced endpoints are not stable sorted at %d: %q >= %q", i, m.Endpoints[i-1].GRPCProcedure, m.Endpoints[i].GRPCProcedure)
		}
	}
	if err := ValidateAdvancedProjectManifest(AdvancedProjectManifest{Version: m.Version, Endpoints: append([]AdvancedProjectEndpoint(nil), m.Endpoints[:len(m.Endpoints)-1]...)}); err != nil {
		t.Fatalf("partial capability manifest should remain structurally valid: %v", err)
	}
	bad := m
	bad.Endpoints = append([]AdvancedProjectEndpoint(nil), m.Endpoints...)
	bad.Endpoints[0].CapabilityRef = "hcmnext.intents.execute"
	if err := ValidateAdvancedProjectManifest(bad); err == nil {
		t.Fatal("non-project capability crossed advanced project manifest boundary")
	}
}

func TestTodo_PM_066_Security(t *testing.T) {
	m := AdvancedProjectEndpoints()
	for _, endpoint := range m.Endpoints {
		if endpoint.ClassificationRef != "CONFIDENTIAL_HR" || !strings.HasPrefix(endpoint.CapabilityRef, "hcmnext.project.") {
			t.Fatalf("endpoint lacks project classification/capability guard: %+v", endpoint)
		}
		if endpoint.ExpectedRevision && endpoint.Name != "PreviewMigration" && endpoint.Name != "PreviewImport" && !endpoint.IdempotencyRequired {
			t.Fatalf("mutating advanced endpoint lacks idempotency fence: %+v", endpoint)
		}
		if endpoint.PaginationPolicy == "OPAQUE_CURSOR_BOUNDED" && endpoint.HTTPMethod != "GET" {
			t.Fatalf("paged advanced endpoint is not a bounded read: %+v", endpoint)
		}
	}
}

func TestTodo_PM_066_Integration(t *testing.T) {
	first := AdvancedProjectEndpoints()
	second := AdvancedProjectEndpoints()
	if first.Version != second.Version || len(first.Endpoints) != len(second.Endpoints) {
		t.Fatalf("manifest is not deterministic: first=%+v second=%+v", first, second)
	}
	for i := range first.Endpoints {
		if first.Endpoints[i] != second.Endpoints[i] {
			t.Fatalf("manifest endpoint %d changed between renders: first=%+v second=%+v", i, first.Endpoints[i], second.Endpoints[i])
		}
	}
}
