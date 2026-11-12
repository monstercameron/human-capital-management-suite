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

// TestIssue3567_WorkflowCatalogListStateIsAddressBacked pins the catalog's
// search, status, sort and page parameters through the route profile: they
// must be admitted, serialized from the request and kept in the shell address
// so a reload, Back or a chip click does not drop them.
func TestIssue3567_WorkflowCatalogListStateIsAddressBacked(t *testing.T) {
	profile, _, ok := PageProfiles(PageWorkflowDesigner)
	if !ok {
		t.Fatal("workflow designer route profile is missing")
	}
	for _, key := range []string{workflowListQueryKey, workflowListStatusKey, workflowListSortKey, workflowListPageKey} {
		if !slices.Contains(profile.QueryKeys(), key) {
			t.Fatalf("workflow designer route does not admit %q", key)
		}
	}

	request := PageRequest{WorkflowID: "workflows.clock", WorkflowQuery: "clock", WorkflowListStatus: workflowListStatusDraft, WorkflowListSort: workflowListSortName, WorkflowListPage: 2}
	provided := map[string]bool{"workflow": true, workflowListQueryKey: true, workflowListStatusKey: true, workflowListSortKey: true, workflowListPageKey: true}
	canonical := profile.CanonicalValues(request, provided)
	for key, want := range map[string]string{workflowListQueryKey: "clock", workflowListStatusKey: workflowListStatusDraft, workflowListSortKey: workflowListSortName, workflowListPageKey: "2"} {
		if got := canonical.Get(key); got != want {
			t.Fatalf("canonical %s = %q, want %q (values %v)", key, got, want, canonical)
		}
	}

	view := ApplyRequest(NewView(PageWorkflowDesigner, "Ironridge", "Walt", "admin"), request)
	address := url.Values{}
	profile.AddressValues(address, view)
	for key, want := range map[string]string{workflowListQueryKey: "clock", workflowListStatusKey: workflowListStatusDraft, workflowListSortKey: workflowListSortName, workflowListPageKey: "2"} {
		if got := address.Get(key); got != want {
			t.Fatalf("shell address %s = %q, want %q (values %v)", key, got, want, address)
		}
	}

	defaults := profile.CanonicalValues(PageRequest{WorkflowID: request.WorkflowID, WorkflowListStatus: workflowListStatusAll, WorkflowListSort: workflowListSortRecent, WorkflowListPage: 1}, provided)
	if defaults.Get(workflowListStatusKey) != "" {
		t.Fatalf("default status was serialized as %q", defaults.Get(workflowListStatusKey))
	}
	if defaults.Get(workflowListPageKey) != "" {
		t.Fatalf("first page was serialized into the address: %v", defaults)
	}
}
