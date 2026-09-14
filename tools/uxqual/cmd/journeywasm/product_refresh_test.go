package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

func TestTodo_PROMOUX_010_Regression_KeepOnlyCurrentJourneyDuringRefresh(t *testing.T) {
	for _, tc := range []struct {
		name, active, requested string
		last, next              productui.PageID
		want                    bool
	}{
		{name: "same journey mutation", last: productui.PageJourneys, next: productui.PageJourneys, active: "/journeys/j-1", requested: "/journeys/j-1", want: true},
		{name: "different journey", last: productui.PageJourneys, next: productui.PageJourneys, active: "/journeys/j-1", requested: "/journeys/j-2"},
		{name: "journey to list", last: productui.PageJourneys, next: productui.PageJourneys, active: "/journeys/j-1", requested: "/journeys"},
		{name: "cold journey", last: productui.PageJourneys, next: productui.PageJourneys, requested: "/journeys/j-1"},
		{name: "ordinary same-page filter", last: productui.PagePeople, next: productui.PagePeople, want: true},
		{name: "different page", last: productui.PagePeople, next: productui.PageJourneys, active: "/journeys/j-1", requested: "/journeys/j-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := keepResolvedPageDuringLoad(tc.last, tc.next, tc.active, tc.requested); got != tc.want {
				t.Fatalf("keepResolvedPageDuringLoad(%q, %q, %q, %q) = %t, want %t", tc.last, tc.next, tc.active, tc.requested, got, tc.want)
			}
		})
	}
}

func TestTodo_PROMOUX_011_Regression_DoesNotPaintPreviousSubjectDuringWarmLoad(t *testing.T) {
	last := productui.View{Page: productui.PagePerson, SelectedPerson: "worker-a"}
	requested := productclient.State{Page: productui.PagePerson, Request: productui.PageRequest{Page: productui.PagePerson, SelectedPerson: "worker-b"}}
	if keepResolvedProductViewDuringLoad(last, requested, "", "") {
		t.Fatal("person A projection was retained while loading person B")
	}
	requested.Request.SelectedPerson = "worker-a"
	if !keepResolvedProductViewDuringLoad(last, requested, "", "") {
		t.Fatal("same-person refresh lost its warm projection")
	}
}

func TestTodo_PROMOUX_011_Regression_DoesNotPaintPreviousHistorySubjectFilter(t *testing.T) {
	last := productui.View{Page: productui.PageHistory, HistoryPerson: "worker-a"}
	requested := productclient.State{Page: productui.PageHistory, Request: productui.PageRequest{Page: productui.PageHistory, HistoryPerson: "worker-b"}}
	if keepResolvedProductViewDuringLoad(last, requested, "", "") {
		t.Fatal("history for worker A was retained while loading worker B")
	}
}

func TestTodo_UXAUDIT_012_Regression_FencesOrganizationSubjectDuringWarmLoad(t *testing.T) {
	for _, page := range []productui.PageID{
		productui.PageOrganization, productui.PageOrgExplorer, productui.PageOrgOutline, productui.PageOrgResponsive,
	} {
		t.Run(string(page), func(t *testing.T) {
			last := productui.View{Page: page, SelectedPerson: "worker-a"}
			requested := productclient.State{Page: page, Request: productui.PageRequest{Page: page, SelectedPerson: "worker-b"}}
			if keepResolvedProductViewDuringLoad(last, requested, "", "") {
				t.Fatal("worker A organization context was retained while loading worker B")
			}
			requested.Request.SelectedPerson = ""
			if keepResolvedProductViewDuringLoad(last, requested, "", "") {
				t.Fatal("worker A organization context was retained after person selection was cleared")
			}
		})
	}
}

func TestTodo_UXAUDIT_012_Regression_AllowsSameOrganizationSubjectRefresh(t *testing.T) {
	last := productui.View{Page: productui.PageOrganization, SelectedPerson: " worker-a "}
	requested := productclient.State{Page: productui.PageOrganization, Request: productui.PageRequest{Page: productui.PageOrganization, SelectedPerson: "worker-a"}}
	if !keepResolvedProductViewDuringLoad(last, requested, "", "") {
		t.Fatal("same organization subject lost its stable warm projection")
	}
}
