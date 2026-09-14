package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

// TestTodo_UXAUDIT_012 proves the production refresh gate across every
// subject-bearing route.  These cases exercise the same predicate used by the
// WASM router's Loading callback, rather than a mock component abstraction.
func TestTodo_UXAUDIT_012(t *testing.T) {
	for _, tc := range []struct {
		name       string
		page       productui.PageID
		last       productui.View
		request    productui.PageRequest
		active     string
		wantWarm   bool
		wantPerson string
	}{
		{name: "person identity switch", page: productui.PagePerson, last: productui.View{Page: productui.PagePerson, SelectedPerson: "worker-a"}, request: productui.PageRequest{Page: productui.PagePerson, SelectedPerson: "worker-b"}},
		{name: "person same identity", page: productui.PagePerson, last: productui.View{Page: productui.PagePerson, SelectedPerson: "worker-a"}, request: productui.PageRequest{Page: productui.PagePerson, SelectedPerson: " worker-a "}, wantWarm: true, wantPerson: "worker-a"},
		{name: "journey identity switch", page: productui.PageJourneys, last: productui.View{Page: productui.PageJourneys}, request: productui.PageRequest{Page: productui.PageJourneys, JourneyID: "journey-b"}, active: journeyclient.DetailHref("journey-a")},
		{name: "journey same identity", page: productui.PageJourneys, last: productui.View{Page: productui.PageJourneys}, request: productui.PageRequest{Page: productui.PageJourneys, JourneyID: "journey-a"}, active: journeyclient.DetailHref("journey-a"), wantWarm: true},
		{name: "work identity switch", page: productui.PageWork, last: productui.View{Page: productui.PageWork, SelectedWork: "journey-a"}, request: productui.PageRequest{Page: productui.PageWork, SelectedWork: "journey-b"}},
		{name: "history subject switch", page: productui.PageHistory, last: productui.View{Page: productui.PageHistory, HistoryPerson: "worker-a"}, request: productui.PageRequest{Page: productui.PageHistory, HistoryPerson: "worker-b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := productclient.State{Page: tc.page, Request: tc.request}
			warm := keepResolvedProductViewDuringLoad(tc.last, state, tc.active, productclient.JourneyFragment(tc.request))
			if warm != tc.wantWarm {
				t.Fatalf("warm decision = %t, want %t", warm, tc.wantWarm)
			}
			baseline := productBaselineForLoad(tc.last, state, tc.active, productclient.JourneyFragment(tc.request))
			if tc.wantWarm {
				if baseline.SelectedPerson != tc.wantPerson && tc.page == productui.PagePerson {
					t.Fatalf("same-subject baseline selected person = %q, want %q", baseline.SelectedPerson, tc.wantPerson)
				}
				return
			}
			if baseline.SelectedPerson != "" || baseline.SelectedWork != "" || baseline.HistoryPerson != "" || baseline.JourneyID != "" || baseline.JourneyWorker != "" || baseline.JourneyMode != "" {
				t.Fatalf("identity-switch baseline retained route-owned state: %+v", baseline)
			}
		})
	}
}

// TestTodo_UXAUDIT_012_Fault proves a fault-safe refresh cannot opt back into
// the stale projection: an unknown requested identity gets a cleared
// baseline, leaving the renderer to show its destination-shaped loading or
// error state instead of the prior subject's data.
func TestTodo_UXAUDIT_012_Fault(t *testing.T) {
	last := productui.View{Page: productui.PageOrganization, SelectedPerson: "worker-a", SelectedWork: "journey-a"}
	requested := productclient.State{Page: productui.PageOrganization, Request: productui.PageRequest{Page: productui.PageOrganization, SelectedPerson: "worker-b"}}
	if keepResolvedProductViewDuringLoad(last, requested, "", "") {
		t.Fatal("faulted identity switch was classified as a warm refresh")
	}
	baseline := productBaselineForLoad(last, requested, "", "")
	if baseline.SelectedPerson != "" || baseline.SelectedWork != "" {
		t.Fatalf("fault-safe baseline retained stale route state: %+v", baseline)
	}
}
