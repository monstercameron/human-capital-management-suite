package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func issue3959View(allowed bool, navigated *[]string) productui.View {
	view := productui.NewView(productui.PageHome, "t", "p", "s")
	if allowed {
		view.EffectivePermissions = []productui.RolePagePermission{{Page: productui.PageReportCatalog, View: true}}
	}
	view.Navigate = func(href string) { *navigated = append(*navigated, href) }
	return view
}

func TestIssue3959_AnalysisIsOfferedOnlyToAViewerWhoMayOpenAnalytics(t *testing.T) {
	var nav []string
	s := &analysisBrowser{refresh: func() {}, focus: func(string) {}}
	if m := s.project(chatui.Model{SelectedID: "c1"}, issue3959View(false, &nav)); m.Analysis.Enabled || m.Analysis.Open != nil {
		t.Fatalf("offered without the right: %+v", m.Analysis)
	}
	if m := s.project(chatui.Model{SelectedID: "c1"}, issue3959View(true, &nav)); !m.Analysis.Enabled || m.Analysis.Open == nil {
		t.Fatalf("not offered with the right: %+v", m.Analysis)
	}
}

func TestIssue3959_AnalysisStateIsPrivatePerConversationAndHandsOffTokens(t *testing.T) {
	var nav []string
	view := issue3959View(true, &nav)
	refreshes := 0
	s := &analysisBrowser{refresh: func() { refreshes++ }, focus: func(string) {}}
	m := s.project(chatui.Model{SelectedID: "c1"}, view)
	m.Analysis.Open("ask")
	m = s.project(chatui.Model{SelectedID: "c1"}, view)
	m.Analysis.Metric("attrition")
	m.Analysis.Choose("period", "last_quarter")
	m = s.project(chatui.Model{SelectedID: "c1"}, view)
	if m.Analysis.PostID != "ask" || !m.Analysis.Metrics["attrition"] || m.Analysis.Period != "last_quarter" {
		t.Fatalf("state = %+v", m.Analysis)
	}
	m.Analysis.HandOff("c1", "ask", chatui.AnalysisRequest{Metrics: []string{"attrition"}, Period: "last_quarter"})
	if len(nav) != 1 || !strings.HasPrefix(nav[0], "/workspace/app/reports/catalog?") || !strings.Contains(nav[0], "metric=attrition") {
		t.Fatalf("navigation = %v", nav)
	}
	// Another conversation starts clean; coming back does not restore the draft.
	if other := s.project(chatui.Model{SelectedID: "c2"}, view); other.Analysis.PostID != "" || len(other.Analysis.Metrics) != 0 {
		t.Fatalf("leaked into another conversation: %+v", other.Analysis)
	}
	if back := s.project(chatui.Model{SelectedID: "c1"}, view); back.Analysis.PostID != "" {
		t.Fatalf("restored after leaving: %+v", back.Analysis)
	}
	// Choosing another message starts with nothing chosen; close forgets it all.
	back := s.project(chatui.Model{SelectedID: "c1"}, view)
	back.Analysis.Open("one")
	back.Analysis.Metric("headcount")
	back = s.project(chatui.Model{SelectedID: "c1"}, view)
	back.Analysis.Open("two")
	if again := s.project(chatui.Model{SelectedID: "c1"}, view); again.Analysis.PostID != "two" || len(again.Analysis.Metrics) != 0 {
		t.Fatalf("second message: %+v", again.Analysis)
	}
	again := s.project(chatui.Model{SelectedID: "c1"}, view)
	again.Analysis.Close()
	if closed := s.project(chatui.Model{SelectedID: "c1"}, view); closed.Analysis.PostID != "" || refreshes == 0 {
		t.Fatalf("close: %+v", closed.Analysis)
	}
}
