package productui

import (
	"net/url"
	"testing"
)

func TestTodo_WEB_243(t *testing.T) {
	cases := []struct {
		route             string
		profile           RouteProfile
		data              DataProfile
		journeys, workers bool
	}{
		{"/workspace/app/home", RouteProfileHome, DataProfileJourneysWorkers, true, true},
		{"/workspace/app/people", RouteProfilePeople, DataProfileJourneysWorkers, true, true},
		{"/workspace/app/person", RouteProfilePerson, DataProfileJourneysWorkers, true, true},
		{"/workspace/app/organization", RouteProfileOrganization, DataProfileWorkers, false, true},
		{"/workspace/app/organization/outline", RouteProfileOrgOutline, DataProfileWorkers, false, true},
		{"/workspace/app/settings", RouteProfileSupport, DataProfileNone, false, false},
	}
	for _, test := range cases {
		module, ok := LookupRouteModule(test.route)
		if !ok {
			t.Fatalf("route %q is not registered", test.route)
		}
		if module.RouteProfile != test.profile || module.DataProfile != test.data {
			t.Errorf("%s profiles = %q/%q, want %q/%q", test.route, module.RouteProfile, module.DataProfile, test.profile, test.data)
		}
		requirements := module.DataProfile.Requirements()
		if requirements.Journeys != test.journeys || requirements.Workers != test.workers {
			t.Errorf("%s requirements = %+v", test.route, requirements)
		}
	}
}

func TestTodo_WEB_243_Property(t *testing.T) {
	profile := RouteProfilePerson.StateProfile()
	profile.QueryKeys[0] = "corrupted"
	if RouteProfilePerson.QueryKeys()[0] == "corrupted" {
		t.Fatal("route profile query keys escaped as mutable state")
	}
	values := url.Values{"org_view": {"flat"}}
	if !RouteProfileWork.ValidControlledValues(values) {
		t.Fatal("profile validation rejected unrelated values")
	}
}

func TestTodo_WEB_243_Conformance(t *testing.T) {
	for _, module := range PageModules() {
		if !validRouteProfile(module.RouteProfile) || !validDataProfile(module.DataProfile) {
			t.Fatalf("module %q has non-reusable profiles %q/%q", module.Definition.ID, module.RouteProfile, module.DataProfile)
		}
		keys := module.RouteProfile.QueryKeys()
		seen := map[string]bool{}
		for _, key := range keys {
			if seen[key] {
				t.Fatalf("module %q profile repeats query key %q", module.Definition.ID, key)
			}
			seen[key] = true
		}
	}
}

func TestTodo_WEB_243_Performance(t *testing.T) {
	allocations := testing.AllocsPerRun(100, func() {
		module, ok := LookupPageModule(PagePerson)
		if !ok || module.Definition.ID != PagePerson {
			t.Fatal("profile lookup failed")
		}
	})
	if allocations > 16 {
		t.Fatalf("profile lookup allocations = %.0f, want <= 16", allocations)
	}
}

func TestTodo_WEB_243_Regression(t *testing.T) {
	if got := RouteProfileOrgOutline.CanonicalValues(PageRequest{Page: PageOrgOutline}, map[string]bool{}).Get("org_view"); got != "tree" {
		t.Fatalf("outline canonical org_view = %q, want tree", got)
	}
	if got := RouteProfilePerson.CanonicalValues(PageRequest{SelectedPerson: "worker-a", WorkflowQuery: "promotion"}, map[string]bool{"person": true, "workflow_q": true}); got.Encode() != "person=worker-a&workflow_q=promotion" {
		t.Fatalf("person canonical values = %q", got.Encode())
	}
	view := testView(PagePerson)
	view.PeoplePage = 3
	view.PeoplePageSize = 100
	view.PeopleEligibleOnly = true
	view.HistoryPage = 4
	view.HistoryPageSize = 10
	values := url.Values{}
	RouteProfilePerson.AddressValues(values, view)
	for key, want := range map[string]string{
		"person": "worker-avery", "page": "3", "page_size": "100", "eligible": "1",
		"history_page": "4", "history_page_size": "10",
	} {
		if got := values.Get(key); got != want {
			t.Errorf("person address %s = %q, want %q", key, got, want)
		}
	}
}

func TestRouteProfileHistoryOutcomeRoundTrip(t *testing.T) {
	statuses := append([]string{""}, JourneyStatusFilterValues()...)
	// The compatibility /history, Myself and Person views still render these
	// terminal outcomes through WorkflowHistoryFilter.
	statuses = append(statuses, "completed", "rejected", "failed")
	sorts := []string{"started", "updated", "status", "person", "change", "closed", "outcome"}
	for _, status := range statuses {
		statusName := status
		if statusName == "" {
			statusName = "all"
		}
		for _, sort := range sorts {
			sort := sort
			t.Run(statusName+"-"+sort, func(t *testing.T) {
				request := PageRequest{
					HistoryOutcome: status, HistoryQuery: "Promotion",
					HistoryRequester: "Avery Patel", HistorySort: sort,
				}
				provided := map[string]bool{
					"outcome": true, "history_q": true, "history_requester": true, "history_sort": true,
				}
				encoded := RouteProfileHistory.CanonicalValues(request, provided).Encode()
				values, err := url.ParseQuery(encoded)
				if err != nil {
					t.Fatalf("canonical history query %q is invalid: %v", encoded, err)
				}
				if !RouteProfileHistory.ValidControlledValues(values) {
					t.Fatalf("rendered outcome/sort %q/%q was rejected after URL round trip: %q", status, sort, encoded)
				}
				for key, want := range map[string]string{
					"outcome": status, "history_q": "Promotion",
					"history_requester": "Avery Patel", "history_sort": sort,
				} {
					if got := values.Get(key); got != want {
						t.Errorf("round-tripped %s = %q, want %q", key, got, want)
					}
				}
				values.Set("outcome", "arbitrary-status")
				if RouteProfileHistory.ValidControlledValues(values) {
					t.Fatal("route profile accepted an unrendered history outcome")
				}
				values.Set("outcome", status)
				values.Set("history_sort", "arbitrary-sort")
				if RouteProfileHistory.ValidControlledValues(values) {
					t.Fatal("route profile accepted an unrendered history sort")
				}
			})
		}
	}
}
