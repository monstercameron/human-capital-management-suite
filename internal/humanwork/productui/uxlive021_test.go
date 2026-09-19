package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-021's RED was measured on the running server: the People row menus
// are bare <details> disclosures, so opening the "Workflows" menu on one row
// and the "Unavailable" explanation on another left both panels open and
// overlapping across rows. A single-item "Workflows" menu also took two
// clicks to reach its only action.
//
// Escape and outside dismissal were already handled by the transient-popover
// controller (tools/uxqual/cmd/journeywasm/transient_popover_policy.go); what
// was missing is that opening one closes the other, and that a menu holding
// one action is not a menu.

func uxlive021Popover(t *testing.T, group string) string {
	t.Helper()
	doc, err := ui.RenderToString(TransientPopover(TransientPopoverProps{
		Kind: "people-workflows", Group: group, Class: "people-workflow-menu",
		Trigger:  []ui.Node{ui.Text("Workflows")},
		Children: []ui.Node{ui.Text("Promotion")},
	}))
	if err != nil {
		t.Fatalf("render popover: %v", err)
	}
	return doc
}

// TestTodo_UXLIVE_021 is the primary red/green test: menus in one group are
// mutually exclusive.
func TestTodo_UXLIVE_021(t *testing.T) {
	grouped := uxlive021Popover(t, "people-workflows")
	if !strings.Contains(grouped, `name="people-workflows"`) {
		t.Fatalf("a grouped row menu does not join its exclusive group:\n%s", grouped)
	}

	ungrouped := uxlive021Popover(t, "")
	if strings.Contains(ungrouped, `name="`) {
		t.Fatalf("an ungrouped popover invented a group:\n%s", ungrouped)
	}

	// Every row menu in the directory shares one group, so opening one
	// closes whichever was open.
	doc := uxlive021Directory(t)
	names := regexp.MustCompile(`<details[^>]*name="([^"]+)"`).FindAllStringSubmatch(doc, -1)
	if len(names) < 2 {
		t.Fatalf("the directory renders %d grouped row menus, want at least 2:\n%s", len(names), doc)
	}
	for _, match := range names {
		if match[1] != names[0][1] {
			t.Fatalf("row menus are in different groups (%q and %q), so both can be open at once", names[0][1], match[1])
		}
	}
}

func uxlive021Directory(t *testing.T) string {
	t.Helper()
	view := testView(PagePeople)
	view.People = []Person{
		{ID: "a", Name: "Amara", Initials: "A", Role: "Designer", Team: "Product", Location: "New York", PromotionAvailability: PromotionEligible},
		{ID: "b", Name: "Amina", Initials: "AM", Role: "Chief Executive", Team: "Executive", Location: "Boston", PromotionAvailability: PromotionIneligible},
		{ID: "c", Name: "Andre", Initials: "AN", Role: "Engineer", Team: "Security", Location: "San Francisco", PromotionAvailability: PromotionEligible},
	}
	doc, err := ui.RenderToString(peoplePage(view))
	if err != nil {
		t.Fatalf("render people: %v", err)
	}
	return doc
}

// TestTodo_UXLIVE_021_Browser keeps the dismissal contract the controller
// already provides, so grouping does not replace it.
func TestTodo_UXLIVE_021_Browser(t *testing.T) {
	doc := uxlive021Popover(t, "people-workflows")
	if !strings.Contains(doc, "data-hcm-transient-popover") {
		t.Fatalf("the row menu left the transient-popover controller, which owns Escape and outside dismissal:\n%s", doc)
	}
	if !strings.Contains(doc, "<summary") {
		t.Fatalf("the row menu is no longer a native disclosure:\n%s", doc)
	}
}
