package manifest

import (
	"strings"
	"testing"
)

func TestWorkOrderManifestConformance(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	count := 0
	for _, endpoint := range m.Endpoints {
		if !strings.HasPrefix(endpoint.EndpointID, "hcmnext.workorder.v1.WorkOrderService/") {
			continue
		}
		count++
		if endpoint.Disposition != DispositionServed || endpoint.DispositionReason == "" {
			t.Errorf("%s: missing served disposition: %s (%q)", endpoint.EndpointID, endpoint.Disposition, endpoint.DispositionReason)
		}
		if endpoint.OwnerDomain != "WORKORDER" || len(endpoint.CapabilityRefs) != 1 || !strings.HasPrefix(endpoint.CapabilityRefs[0], "hcmnext.workorderaccess.") {
			t.Errorf("%s: owner/capability binding = %q %v", endpoint.EndpointID, endpoint.OwnerDomain, endpoint.CapabilityRefs)
		}
		if endpoint.HTTPMethod != "POST" || endpoint.HTTPPathTemplate != "/"+endpoint.EndpointID || endpoint.HTTPBodyBinding != "*" {
			t.Errorf("%s: invalid Connect route %s %s body=%q", endpoint.EndpointID, endpoint.HTTPMethod, endpoint.HTTPPathTemplate, endpoint.HTTPBodyBinding)
		}
		if endpoint.ClassificationRef != "CONFIDENTIAL_HR" || endpoint.AuthzAction == "" || endpoint.Phase != "PHASE_1" {
			t.Errorf("%s: incomplete security metadata: classification=%q authz=%q phase=%q", endpoint.EndpointID, endpoint.ClassificationRef, endpoint.AuthzAction, endpoint.Phase)
		}
		if endpoint.IdempotencyClass == IdempotencyKey && endpoint.IdempotencyKeySource != "request.idempotency_key" {
			t.Errorf("%s: missing idempotency key source", endpoint.EndpointID)
		}
		if len(endpoint.RequiredFieldPaths) == 0 {
			t.Errorf("%s: missing required request field policy", endpoint.EndpointID)
		}
	}
	if count != 12 {
		t.Fatalf("expected all 12 WorkOrderService RPCs, got %d", count)
	}
}
