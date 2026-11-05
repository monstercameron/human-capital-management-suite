package projectui

import (
	"strings"
	"testing"
)

// UXBLIND-057 addendum (Projects > Tickets > Assignee filter): the filter
// dropdowns are plain details disclosures, so the shell's shared popover
// controller ignores them and Escape leaves the menu, its blocking overlay
// and focus behind. Joining the controller's transient-popover contract
// gives them Escape, outside-click and select dismissal with focus recovery.
func TestTodo_UXBLIND_057_TicketsFilter(t *testing.T) {
	list := TicketList{
		Groups: []TicketFilterGroup{{
			Key:   "assignee",
			Label: "Assignee",
			Choices: []FilterChoice{
				{ID: "ana", Label: "Ana Flores", Href: "/tickets?assignee=ana"},
				{ID: "morgan", Label: "Morgan Lee", Href: "/tickets?assignee=morgan", Active: true},
			},
		}},
	}
	markup := renderString(t, TicketFilters(list, TicketCopy(Copy{})))
	for _, want := range []string{
		`data-hcm-transient-popover="tickets-filter"`,
		`data-hcm-popover-surface="true"`,
		`haspopup`,
		`role="menu"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("tickets filter menu missing shared-controller contract %q: %s", want, markup)
		}
	}
	// Selection keeps its filter semantics: every choice stays a link to
	// the address with that choice flipped.
	for _, want := range []string{`href="/tickets?assignee=ana"`, `href="/tickets?assignee=morgan"`, `role="menuitemcheckbox"`, `aria-checked="true"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("tickets filter menu lost filter semantics %q: %s", want, markup)
		}
	}
}

// UXBLIND-057 addendum (board Share disclosure): Escape from Copy for Docs
// or Copy link left Share open with focus on the item, while the card's
// More actions disclosure dismissed. Same contract joins Share to the one
// shared dismissal owner.
func TestTodo_UXBLIND_057_ShareMenu(t *testing.T) {
	markup := renderString(t, ShareMenu(Copy{}, &Share{Href: "/board/1", Title: "Release plan"}))
	for _, want := range []string{
		`data-hcm-transient-popover="project-share"`,
		`data-hcm-popover-surface="true"`,
		`haspopup`,
		`role="menu"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("Share disclosure missing shared-controller contract %q: %s", want, markup)
		}
	}
	// Sharing effects stay separate from dismissal: the items keep their
	// actions and addresses.
	for _, want := range []string{`data-projectui-action="copy-link"`, `data-projectui-action="share-chat"`, `data-projectui-action="copy-docs"`, `role="menuitem"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("Share disclosure lost menu semantics %q: %s", want, markup)
		}
	}
}
