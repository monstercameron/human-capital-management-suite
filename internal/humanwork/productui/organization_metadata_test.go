package productui

import (
	"strings"
	"testing"
)

func TestOrganizationPageProjectsAuthorizedBusinessMetadata(t *testing.T) {
	view := testView(PageOrganization)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="business-metadata-title"`, "Business metadata", "tenant-test",
		"Visible workforce", ">3<", "Organization units", ">2<",
		"Operating locations", "New York", "San Francisco", "Toronto",
		"Pay zones", "Access scope", "manager",
		"legal-entity details and reporting lines are not inferred",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("organization metadata missing %q", want)
		}
	}
}

func TestOrganizationMetadataRemainsVisibleWithoutOrganizationGroups(t *testing.T) {
	view := NewView(PageOrganization, "tenant-empty", "manager", "self")
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Business metadata") || !strings.Contains(doc, "No organization members to show") {
		t.Fatal("empty organization view did not preserve business metadata and its honest empty state")
	}
}
