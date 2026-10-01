package productui

import (
	"net/url"
	"slices"
	"testing"
)

func TestWorkflowReferenceRouteStateIsTypedAndScoped(t *testing.T) {
	profile, _, ok := PageProfiles(PageWorkflowDesigner)
	if !ok || profile != RouteProfileWorkflow {
		t.Fatalf("workflow designer route profile = %q, ok=%t", profile, ok)
	}
	if !slices.Contains(profile.QueryKeys(), "workflow_references") {
		t.Fatal("workflow designer route does not admit the reference toggle")
	}
	if !profile.ValidControlledValues(url.Values{"workflow_references": {"1"}}) {
		t.Fatal("workflow designer rejected enabled reference toggle")
	}
	if profile.ValidControlledValues(url.Values{"workflow_references": {"true"}}) {
		t.Fatal("workflow designer accepted a noncanonical reference-toggle value")
	}

	request := PageRequest{WorkflowID: "workflows.clock", WorkflowShowReferences: true}
	provided := map[string]bool{"workflow": true, "workflow_references": true}
	canonical := profile.CanonicalValues(request, provided)
	if canonical.Get("workflow") != request.WorkflowID || canonical.Get("workflow_references") != "1" {
		t.Fatalf("canonical workflow route = %v", canonical)
	}
	canonical = profile.CanonicalValues(PageRequest{WorkflowID: request.WorkflowID}, provided)
	if _, present := canonical["workflow_references"]; present {
		t.Fatalf("unchecked references remained in canonical route: %v", canonical)
	}

	view := ApplyRequest(NewView(PageWorkflowDesigner, "Ironridge", "Walt", "admin"), request)
	address := url.Values{}
	profile.AddressValues(address, view)
	if address.Get("workflow_references") != "1" {
		t.Fatalf("workflow shell address lost reference visibility: %v", address)
	}

	request.WorkflowShowReferences = false
	view = ApplyRequest(NewView(PageWorkflowDesigner, "Ironridge", "Walt", "admin"), request)
	address = url.Values{}
	profile.AddressValues(address, view)
	if _, present := address["workflow_references"]; present {
		t.Fatalf("hidden references were serialized into shell address: %v", address)
	}
}
