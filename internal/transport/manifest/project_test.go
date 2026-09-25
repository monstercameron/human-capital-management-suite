package manifest

import (
	"strings"
	"testing"
)

func TestTodo_PM_026_Conformance(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	count := 0
	for _, endpoint := range m.Endpoints {
		if !strings.HasPrefix(endpoint.EndpointID, "hcmnext.project.v1.ProjectService/") {
			continue
		}
		count++
		if endpoint.Disposition != DispositionServed || endpoint.DispositionReason == "" {
			t.Errorf("%s: expected a reviewed served disposition, got %s (%q)", endpoint.EndpointID, endpoint.Disposition, endpoint.DispositionReason)
		}
		if endpoint.OwnerDomain != "PROJECT" || len(endpoint.CapabilityRefs) != 1 || !strings.HasPrefix(endpoint.CapabilityRefs[0], "hcmnext.projectaccess.") {
			t.Errorf("%s: missing project owner or projectaccess capability reference: owner=%q refs=%v", endpoint.EndpointID, endpoint.OwnerDomain, endpoint.CapabilityRefs)
		}
		if endpoint.HTTPMethod != "POST" || endpoint.HTTPPathTemplate != "/"+endpoint.EndpointID || endpoint.HTTPBodyBinding != "*" {
			t.Errorf("%s: not bound to its canonical Connect JSON route: %s %s body=%q", endpoint.EndpointID, endpoint.HTTPMethod, endpoint.HTTPPathTemplate, endpoint.HTTPBodyBinding)
		}
		if len(endpoint.RequiredFieldPaths) == 0 {
			t.Errorf("%s: missing reviewed request presence requirements", endpoint.EndpointID)
		}
		if endpoint.Phase != "PHASE_3" || endpoint.ClassificationRef != "CONFIDENTIAL_HR" || endpoint.AuthzAction == "" {
			t.Errorf("%s: incomplete project security metadata: phase=%q classification=%q authz=%q", endpoint.EndpointID, endpoint.Phase, endpoint.ClassificationRef, endpoint.AuthzAction)
		}
		if endpoint.IdempotencyClass == IdempotencyKey && endpoint.IdempotencyKeySource != "request.idempotency_key" {
			t.Errorf("%s: write retry lacks request idempotency key source %q", endpoint.EndpointID, endpoint.IdempotencyKeySource)
		}
	}
	if count != 38 {
		t.Fatalf("expected all 38 current ProjectService RPCs, got %d", count)
	}
}

func TestTodo_PM_026_Security(t *testing.T) {
	for procedure, paths := range requiredFieldPaths {
		if !strings.HasPrefix(procedure, "/hcmnext.project.v1.ProjectService/") {
			continue
		}
		if len(paths) == 0 {
			t.Errorf("%s: empty required field rule", procedure)
		}
		for _, path := range paths {
			if path == "scope" {
				continue
			}
			if strings.TrimSpace(path) != path || path == "" {
				t.Errorf("%s: invalid required field path %q", procedure, path)
			}
		}
	}
}
