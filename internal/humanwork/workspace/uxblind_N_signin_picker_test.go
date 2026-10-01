package workspace

import (
	"strings"
	"testing"
)

func TestTodo_UXBLIND_053(t *testing.T) {
	h := newDirectoryHandler(t)
	body := getLoginPage(t, h, "Evelyn")

	if !strings.Contains(body, `action="`+PathLogin+`#directory-summary"`) {
		t.Fatal("employee search does not return the browser to the result region")
	}
	if !strings.Contains(body, `<p id="directory-summary" class="directory-summary" role="status" aria-live="polite" tabindex="-1" autofocus>`) {
		t.Fatal("filtered result count is not a focusable live result target")
	}
	if got := strings.Count(body, "Sign in as Evelyn Morgan"); got != 1 {
		t.Fatalf("filtered employee appears %d times, want once", got)
	}
	if strings.Contains(body, `class="org-count">0 match`) {
		t.Fatal("an empty organization group is still rendered")
	}
	if !strings.Contains(body, `class="org-unit"`) || !strings.Contains(body, `class="org-unit-name">Clinical Operations`) {
		t.Fatal("the matching organization path disappeared with the duplicate people")
	}
}

func TestTodo_UXBLIND_053_Browser(t *testing.T) {
	h := newDirectoryHandler(t)
	body := getLoginPage(t, h, "Evelyn")

	// The login surface deliberately has a no-script CSP. This component-level
	// browser proof pins the native navigation contract that keeps the result
	// region in view after a GET form submission.
	if !strings.Contains(body, `method="get"`) || !strings.Contains(body, `href="`+PathLogin+`">Clear filters</a>`) {
		t.Fatal("the browser search contract is not a native, recoverable GET")
	}
	if strings.Contains(body, `onclick=`) || strings.Contains(body, `<script`) {
		t.Fatal("the picker added a script-only browser path")
	}
	if strings.Count(body, `<ul class="directory-results">`) != 1 {
		t.Fatal("the browser result surface is not a single list")
	}
}

func TestTodo_UXBLIND_053_Accessibility(t *testing.T) {
	h := newDirectoryHandler(t)
	body := getLoginPage(t, h, "Evelyn")

	for _, want := range []string{
		`<div class="directory-field"><label for="directory-query">Find an employee</label>`,
		`<div class="directory-field directory-role-field"><label for="directory-role">Role</label>`,
		`id="directory-summary"`,
		`role="status"`,
		`tabindex="-1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("accessible picker markup missing %q", want)
		}
	}

	sheet := loginStylesheet()
	for _, want := range []string{
		`.org-unit > details > summary::-webkit-details-marker`,
		`.org-unit > details > summary::before`,
		`.directory-summary:focus-visible`,
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("picker stylesheet missing accessible affordance %q", want)
		}
	}

	companies := []DevCompany{
		{Key: "harborcare-demo", Name: "HarborCare", Description: "Care", Headcount: 3},
		{Key: "ironridge-demo", Name: "IronRidge", Description: "Technology", Headcount: 2},
	}
	companyBody := companySelector(companies, companies[0], "en-US")
	if !strings.Contains(companyBody, `aria-current="true" aria-label="HarborCare (Selected)"`) {
		t.Fatal("selected company link has no accessible name")
	}
	if !strings.Contains(companyBody, `aria-label="Switch to IronRidge"`) {
		t.Fatal("unselected company link lost its accessible name")
	}
}
