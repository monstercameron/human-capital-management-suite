package productui

import (
	"strings"
	"testing"
)

func TestSupportDestinationsRespectResolvedPagePermissions(t *testing.T) {
	view := testView(PageHelp)
	view.EffectivePermissions = []RolePagePermission{
		{Page: PageKnowledgeSearch, View: true},
		{Page: PageHRServiceRequest, View: true},
		{Page: PageConfidentialCase, View: false},
		{Page: PageCaseStatus, View: true},
	}
	destinations := authorizedSupportDestinations(view)
	if len(destinations) != 3 {
		t.Fatalf("authorized destinations = %d, want 3", len(destinations))
	}
	for _, destination := range destinations {
		if strings.Contains(destination.Href, "/confidential-case") {
			t.Fatal("denied confidential case route was exposed")
		}
		if strings.TrimSpace(destination.Title) == "" || strings.TrimSpace(destination.Description) == "" {
			t.Fatalf("destination lacks an explanatory label: %+v", destination)
		}
	}
}

func TestHelpDoesNotContradictItsSupportDestinations(t *testing.T) {
	for _, detail := range []string{
		"Contact HR. Support tickets cannot be submitted from this workspace.",
		"Kontakt HR. Supportanfragen können in diesem Arbeitsbereich nicht eingereicht werden.",
		"تواصل مع الموارد البشرية. لا يمكن إرسال تذاكر الدعم من مساحة العمل هذه.",
	} {
		if got := trimRetiredSupportBoundary(detail); strings.Contains(strings.ToLower(got), "support ticket") || strings.Contains(got, "Supportanfragen") || strings.Contains(got, "تذاكر الدعم") {
			t.Fatalf("retired support boundary remained in %q", got)
		}
	}
}

func TestKnowledgeSearchRendersAccessibleProgressiveSearch(t *testing.T) {
	view := testView(PageKnowledgeSearch)
	view.Query = "pay statement"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="search"`, `name="q"`, `type="search"`, `value="pay statement"`, "Authorized knowledge search is not published yet"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("knowledge search missing %q", want)
		}
	}
}

func TestHRServiceRequestDoesNotPutDetailsInTheURL(t *testing.T) {
	doc, err := Render(testView(PageHRServiceRequest))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="support-request`) || !strings.Contains(doc, `method="post"`) {
		t.Fatal("support request must use a POST form")
	}
	if !strings.Contains(doc, `name="category"`) || !strings.Contains(doc, `name="details"`) {
		t.Fatal("support request omitted its category or details field")
	}
}

func TestInsightsUsesInsufficientDataInsteadOfUnsupportedZeroes(t *testing.T) {
	view := testView(PageInsights)
	view.Work = nil
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Not reported", "Not enough visible requests to report a count", "Period", "Last updated", "Includes", "a missing count is not zero"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("insufficient-data insights missing %q", want)
		}
	}
	if strings.Contains(doc, ">0<") {
		t.Fatal("insufficient-data insights present zero as a measured result")
	}
}
