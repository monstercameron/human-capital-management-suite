package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/organization"
)

func TestTodo_REV_076_01(t *testing.T) {
	view := testView(PageOrganization)
	view.People = []Person{{ID: "worker-1", Name: "Alex Worker", Team: "Untrusted Team", Location: "Untrusted Location"}}
	view.OrganizationTenant = "tenant-test"
	view.OrganizationAsOf = "2026-09-24"
	view.OrganizationGraph = &organization.Snapshot{Tenant: "tenant-test"}
	// No authorizer is supplied, so the wired graph is invalid for disclosure.
	markup, err := ui.RenderToString(organizationPageWithCopy(view, "Organization", "", false))
	if err != nil {
		t.Fatal(err)
	}
	for _, untrusted := range []string{"Untrusted Team", "Untrusted Location"} {
		if strings.Contains(markup, untrusted) {
			t.Fatalf("invalid organization graph fell back to legacy value %q: %s", untrusted, markup)
		}
	}
}
