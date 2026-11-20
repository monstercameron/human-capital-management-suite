package chatui

import (
	stdhtml "html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3854Rule(t *testing.T, selector string) string {
	t.Helper()
	i := strings.Index(Stylesheet, selector)
	if i < 0 {
		t.Fatalf("the stylesheet has no rule for %q", selector)
	}
	rest := Stylesheet[i:]
	j := strings.Index(rest, "}")
	if j < 0 {
		t.Fatalf("rule for %q is not closed", selector)
	}
	return rest[:j]
}

// TestIssue3854_PermissionCellsNameTheirColumn: when the matrix reflows into
// one card per role, every cell says which permission it is for, in the
// reader's language, so a switch is never separated from its column heading.
func TestIssue3854_PermissionCellsNameTheirColumn(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		model := ModerationPermissionsModel{}
		markup := stdhtml.UnescapeString(chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: locale, State: StateReady, Tab: "permissions", CanAssign: true, Permissions: &model})))
		labels := regexp.MustCompile(`<td[^>]*data-label="([^"]+)"`).FindAllStringSubmatch(markup, -1)
		if want := len(chat.ModerationPermissions) * 3; len(labels) != want {
			t.Fatalf("%s: %d cells carry a column label, want %d: %s", locale, len(labels), want, markup)
		}
		for _, permission := range chat.ModerationPermissions {
			if want := chatremoveText(locale, moderationPermissionKeys[permission]); !strings.Contains(markup, `data-label="`+want+`"`) {
				t.Fatalf("%s: no cell is labelled %q", locale, want)
			}
		}
	}
}

// TestIssue3854_PermissionMatrixReflowsOnPhones: below the phone breakpoint a
// row is a card (block rows, a label beside each switch) and words are never
// broken into characters; wider than that the table keeps readable columns
// inside its scroll region.
func TestIssue3854_PermissionMatrixReflowsOnPhones(t *testing.T) {
	if !strings.Contains(Stylesheet, "@media(max-width:640px){.chatremove .chatmod005-perm-table") {
		t.Fatal("the permission matrix has no narrow-screen layout")
	}
	i := strings.Index(Stylesheet, "@media(max-width:640px){.chatremove .chatmod005-perm-table")
	narrow := Stylesheet[i : i+strings.Index(Stylesheet[i:], "/*end-3854*/")]
	for _, want := range []string{"display:block", "attr(data-label)", "grid-template-columns", "tbody td:last-child{border-block-end:0}"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("the narrow layout lacks %q: %s", want, narrow)
		}
	}
	if rule := issue3854Rule(t, ".chatremove .chatmod005-perm-table :is(th,td){"); !strings.Contains(rule, "overflow-wrap:normal") {
		t.Errorf("words in the matrix may still break mid-word: %s", rule)
	}
	if rule := issue3854Rule(t, ".chatremove .chatmod005-perm-table{"); !strings.Contains(rule, "min-inline-size") {
		t.Errorf("the wide table has no readable minimum width: %s", rule)
	}
}

// TestIssue3856_ModerationTextHasTheContentInset: the no-results line and the
// Permissions explanation sit at the same 16px inset as the tabs, the search
// field and the cards, set with logical properties so it holds in RTL, and the
// rule outranks the muted-text reset that zeroed it.
func TestIssue3856_ModerationTextHasTheContentInset(t *testing.T) {
	rule := issue3854Rule(t, ":root .chatremove.chatmod005-page>:not(.chatmod005-heading){")
	if !strings.Contains(rule, "margin-inline:16px") {
		t.Fatalf("the page children have no inline inset: %s", rule)
	}
	if strings.Contains(rule, "margin-left") || strings.Contains(rule, "margin-right") {
		t.Fatalf("the inset is not logical, so RTL would be wrong: %s", rule)
	}
	// The no-results and intro text are direct children of the page, so the rule applies to them.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: locale, State: StateReady, Tab: "resolved", Query: "zzz"}))
		if !strings.Contains(markup, `class="chatmod005-empty"`) {
			t.Fatalf("%s: no-results text is missing: %s", locale, markup)
		}
	}
}
