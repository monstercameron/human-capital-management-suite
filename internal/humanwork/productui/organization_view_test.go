package productui

import (
	"strings"
	"testing"
)

func TestOrganizationViewCanSwitchBetweenFlatAndOwnershipTree(t *testing.T) {
	view := testView(PageOrganization)
	flat, err := Render(ApplyRequest(view, PageRequest{OrganizationView: "flat"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flat, `data-organization-view="flat"`) || !strings.Contains(flat, `org_view=tree`) {
		t.Fatalf("flat view did not expose a software-routable tree toggle")
	}
	for _, want := range []string{`class="organization-unit-disclosure"`, `<summary class="org-node manager">`, "Avery Patel", `class="organization-unit-members"`} {
		if !strings.Contains(flat, want) {
			t.Errorf("expandable flat view missing %q", want)
		}
	}
	tree, err := Render(ApplyRequest(view, PageRequest{OrganizationView: "tree"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="list"`, `data-organization-view="tree"`, `org_view=flat`, "Avery Patel"} {
		if !strings.Contains(tree, want) {
			t.Errorf("tree view missing %q", want)
		}
	}
}

// TestOrganizationReportingDisclosuresAndAmbiguousNames pre-dates UXAUDIT-004
// and pinned the OLD defect: it required nesting by matching Person.Manager
// display-name TEXT, so two people sharing a name ("Alex") made the manager
// ambiguous and blocked nesting, and it required role="tree"/role="treeitem"
// to be ABSENT -- exactly the missing-hierarchy-semantics defect the live UX
// audit found. UXAUDIT-004 replaces name-matching with the authorized,
// WorkerID-keyed org.ResolveManagerRelationships projection, so a shared
// display name is no longer ambiguous at all (there was never a name lookup
// to be ambiguous about), and the tree now IS a real WAI-ARIA tree. This
// case is kept and rewritten, rather than deleted, because its worker-number
// disclosure and "current viewer" assertions remain valid; see
// TestTodo_UXAUDIT_004_Regression and TestTodo_UXAUDIT_004_Accessibility for
// the fuller name-collision and ARIA-tree proofs.
func TestOrganizationReportingDisclosuresAndAmbiguousNames(t *testing.T) {
	alexW01 := Person{ID: "a", WorkerID: uxaudit004ID(101), Name: "Alex", WorkerNumber: "W01"}
	alexW02 := Person{ID: "b", WorkerID: uxaudit004ID(102), Name: "Alex", WorkerNumber: "W02"}
	chris := Person{ID: "c", WorkerID: uxaudit004ID(103), Name: "Chris", ManagerID: alexW02.WorkerID}
	dana := Person{ID: "d", WorkerID: uxaudit004ID(104), Name: "Dana", ManagerID: chris.WorkerID}

	view := testView(PageOrganization)
	view.Viewer.PersonID = chris.ID
	view.People = []Person{alexW01, alexW02, chris, dana}
	nodes := ownershipTree(view)
	if len(nodes) != 2 {
		t.Fatalf("expected exactly the two Alexes as roots (Chris and Dana both nest): %+v", nodes)
	}
	var chrisNode OwnershipNodeProps
	for _, n := range nodes {
		if n.ID == alexW02.ID {
			if len(n.Reports) != 1 || n.Reports[0].ID != chris.ID {
				t.Fatalf("Chris must nest under the specific Alex (W02) named by the authorized relationship, not be blocked by the shared name: %+v", n)
			}
			chrisNode = n.Reports[0]
		} else if n.ID == alexW01.ID && len(n.Reports) != 0 {
			t.Fatalf("the other Alex (W01), whom nobody reports to, must have no reports: %+v", n)
		}
	}
	if len(chrisNode.Reports) != 1 || chrisNode.Reports[0].ID != dana.ID {
		t.Fatalf("Dana must nest under Chris: %+v", chrisNode)
	}

	doc, err := Render(ApplyRequest(view, PageRequest{OrganizationView: "tree"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Direct reports: 1", "W01", "W02", `aria-current="true"`,
		// UXAUDIT-004 GREEN: the reporting-lines view is now a real WAI-ARIA
		// tree, the opposite of this test's pre-fix expectation.
		`role="tree"`, `role="treeitem"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(doc, "Unavailable · Unavailable") {
		t.Errorf("invalid tree output %q", "Unavailable · Unavailable")
	}
}

func TestOrganizationCountLabelsFollowLocale(t *testing.T) {
	for locale, label := range map[string]string{"en-US": "People:", "de-DE": "Personen:", "ar": "الأشخاص:"} {
		t.Run(locale, func(t *testing.T) {
			view := ApplyRequest(testView(PageOrganization), PageRequest{Locale: locale})
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(doc, label) || strings.Contains(doc, "visible workers") {
				t.Fatalf("counts do not follow locale %s", locale)
			}
			if strings.Index(doc, view.Locale.Text("organization.structure_title")) > strings.Index(doc, view.Locale.Text("organization.metadata_title")) {
				t.Fatal("workforce exploration must precede business metadata")
			}
		})
	}
}

// TestOwnershipTreeDoesNotDropCyclesOrOrphans pre-dates UXAUDIT-004, when a
// "cycle" and an "orphan" were made of Person.Manager display-name text. It
// is rewritten against the authorized, WorkerID-keyed relationship
// projection: A and B name each other as their real manager (a genuine
// reporting-line cycle, which organizationHopFacts' per-worker resolution
// alone cannot see -- see breakOwnershipCycles), and C's manager id does not
// belong to anyone in view.People. All three must still be rendered exactly
// once, and neither the cycle nor the orphan may be silently nested.
func TestOwnershipTreeDoesNotDropCyclesOrOrphans(t *testing.T) {
	a := Person{ID: "a", WorkerID: uxaudit004ID(201), Name: "A", Team: "One"}
	b := Person{ID: "b", WorkerID: uxaudit004ID(202), Name: "B", Team: "One"}
	a.ManagerID, b.ManagerID = b.WorkerID, a.WorkerID
	c := Person{ID: "c", WorkerID: uxaudit004ID(203), Name: "C", Team: "Two", ManagerID: uxaudit004ID(999)}

	view := NewView(PageOrganization, "tenant", "principal", "manager")
	view.People = []Person{a, b, c}
	nodes := ownershipTree(view)

	seen := map[string]OwnershipNodeProps{}
	var walk func([]OwnershipNodeProps)
	walk = func(values []OwnershipNodeProps) {
		for _, value := range values {
			seen[value.Name] = value
			walk(value.Reports)
		}
	}
	walk(nodes)
	for _, name := range []string{"A", "B", "C"} {
		node, ok := seen[name]
		if !ok {
			t.Errorf("worker %s was dropped", name)
			continue
		}
		if node.Explanation == "" {
			t.Errorf("worker %s could not be honestly nested and must carry an explanation: %+v", name, node)
		}
		if len(node.Reports) != 0 {
			t.Errorf("worker %s is on a cycle or an orphan and must not gain reports: %+v", name, node)
		}
	}
}
