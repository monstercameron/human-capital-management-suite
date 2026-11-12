package productui

import (
	"strings"
	"testing"
)

// TestIssue3427_WithheldManagerIsNotTopOfOrganization proves that a self
// subtree whose manager relationship the viewer may not see is an explained
// root, so Myself never asserts "None - top of organization" for it. The
// genuine top-level worker (ROOT disposition) keeps the top-of-organization
// fact.
func TestIssue3427_WithheldManagerIsNotTopOfOrganization(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	const anaWorkerID = "22222222-2222-4222-8222-222222222222"
	const rootWorkerID = "11111111-1111-4111-8111-111111111111"

	withheld := testView(PageMyself)
	withheld.Viewer = ViewerProfile{PersonID: "ana", Name: "Ana Flores", Initials: "AF"}
	// The self-service projection carries no manager endpoint for a withheld
	// relationship, so ManagerID is empty while the disposition is WITHHELD.
	withheld.People = []Person{{ID: "ana", WorkerID: anaWorkerID, Name: "Ana Flores", ManagerRelationship: OrganizationRelationshipWithheld}}
	tree := myselfOwnershipSubtree(withheld, "ana")
	if len(tree) != 1 {
		t.Fatalf("expected one self subtree root, got %d", len(tree))
	}
	if tree[0].Explanation == "" {
		t.Fatalf("withheld manager rendered as a genuine top of the organization: %+v", tree[0])
	}

	profile := myselfManagerProfile(locale)
	got := myselfProfileWithTopOrganizationFact(profile, tree, locale.Text("organization.top_of_organization"))
	if manager := got.Organization.Facts[0]; manager.Value == locale.Text("organization.top_of_organization") || manager.Status == WorkerFactPresent {
		t.Fatalf("withheld relationship was projected as top of organization: %+v", manager)
	}

	root := testView(PageMyself)
	root.Viewer = ViewerProfile{PersonID: "root", Name: "Root Lead", Initials: "RL"}
	root.People = []Person{{ID: "root", WorkerID: rootWorkerID, Name: "Root Lead", ManagerRelationship: OrganizationRelationshipRoot}}
	tree = myselfOwnershipSubtree(root, "root")
	if len(tree) != 1 || tree[0].Explanation != "" {
		t.Fatalf("genuine top-level worker lost its root status: %+v", tree)
	}
	got = myselfProfileWithTopOrganizationFact(myselfManagerProfile(locale), tree, locale.Text("organization.top_of_organization"))
	if manager := got.Organization.Facts[0]; manager.Value != locale.Text("organization.top_of_organization") || manager.Status != WorkerFactPresent {
		t.Fatalf("genuine top-level worker lost the top-of-organization fact: %+v", manager)
	}
}

// TestIssue3427_OrphanManagerIsNotTopOfOrganization covers the ORPHAN
// disposition: an unattached relationship is undetermined, not a root.
func TestIssue3427_OrphanManagerIsNotTopOfOrganization(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	view := testView(PageMyself)
	view.Viewer = ViewerProfile{PersonID: "ana", Name: "Ana Flores", Initials: "AF"}
	view.People = []Person{{ID: "ana", WorkerID: "22222222-2222-4222-8222-222222222222", Name: "Ana Flores", ManagerRelationship: OrganizationRelationshipOrphan}}
	tree := myselfOwnershipSubtree(view, "ana")
	if len(tree) != 1 || tree[0].Explanation == "" {
		t.Fatalf("orphan relationship rendered without an explanation: %+v", tree)
	}
	if !strings.Contains(tree[0].Explanation, "could not be confirmed") {
		t.Fatalf("orphan relationship explanation is not the undetermined copy: %q", tree[0].Explanation)
	}
	got := myselfProfileWithTopOrganizationFact(myselfManagerProfile(locale), tree, locale.Text("organization.top_of_organization"))
	if manager := got.Organization.Facts[0]; manager.Value == locale.Text("organization.top_of_organization") {
		t.Fatalf("orphan relationship projected as top of organization: %+v", manager)
	}
}

func myselfManagerProfile(locale LocaleContext) PersonProfileProps {
	return PersonProfileProps{
		Organization: EmploymentDetailsProps{
			I18nProps: I18nProps{Locale: locale},
			Facts:     []ProfileFactProps{{Label: locale.Text("person.manager"), Value: locale.Text("common.not_reported"), Status: WorkerFactMissing}},
		},
	}
}
