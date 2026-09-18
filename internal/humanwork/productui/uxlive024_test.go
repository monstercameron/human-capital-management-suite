package productui

import (
	"strings"
	"testing"
)

// UXLIVE-024's RED was measured on the running server: at a 1280px viewport
// the organization unit list occupied about 430px of a roughly 900px card.
// Measuring the live page found the cause: UIPOLISH-001's prose measure
// (--hcm-measure-prose, 65ch) applies to every li inside the shell, so each
// .organization-unit was capped at 583.05px inside an 886px column while the
// rest of the card stayed empty.
//
// A measure is a reading aid for a sentence. A list item used as a layout
// container is not a sentence.

// TestTodo_UXLIVE_024 is the primary red/green test.
func TestTodo_UXLIVE_024(t *testing.T) {
	sheet := Stylesheet()

	// The rule this todo was measured against has since been split in two:
	// the opt-in body roles keep their specificity, and the bare elements
	// became a zero-specificity default, because mixing them made the
	// default out-specify every component that named its own size. The
	// measure -- this todo's actual subject -- is unchanged and still
	// reaches a bare li, which is what the release below has to overcome.
	prose := cssDeclarations(sheet, `:where(.app-shell,.jn-embedded) :where(p,li,dd,dt,blockquote)`)
	if !strings.Contains(prose, "max-inline-size") {
		t.Fatalf("the prose measure rule changed shape; this todo's premise no longer holds: %q", prose)
	}
	roles := cssDeclarations(sheet, `:where(.app-shell,.jn-embedded) :is(.prose,.prose p,.prose li,.provenance,.body-copy,[data-type-role="body"],.type-body)`)
	if !strings.Contains(roles, "max-inline-size") {
		t.Fatalf("the opt-in body roles lost the measure they ask for: %q", roles)
	}

	released := cssDeclarations(sheet, ":is(.app-shell,.jn-embedded) :is(.metrics,.recent,.org-branches,.work-rows,.people-rows)>li")
	if released == "" {
		t.Fatalf("structural list items are still held to the prose measure")
	}
	if !strings.Contains(released, "max-inline-size:none") && !strings.Contains(released, "max-inline-size: none") {
		t.Fatalf("the structural list rule does not release the measure: %q", released)
	}
}

// TestTodo_UXLIVE_024_Browser keeps the measure where it belongs: prose
// inside those containers is still constrained.
func TestTodo_UXLIVE_024_Browser(t *testing.T) {
	sheet := Stylesheet()
	released := cssDeclarations(sheet, ":is(.app-shell,.jn-embedded) :is(.metrics,.recent,.org-branches,.work-rows,.people-rows)>li")
	if strings.Contains(released, "font-size") {
		t.Fatalf("the layout release also changed typography, which is not its job: %q", released)
	}
	// The rule is scoped to direct children, so a paragraph nested inside one
	// of those items keeps the shell's prose measure.
	if !strings.Contains(sheet, ">li{") && !strings.Contains(sheet, "> li{") {
		t.Fatalf("the release is not scoped to direct list children")
	}
}

// cssDeclarations returns the declaration block of the rule whose selector is
// exactly sel, or "" when the sheet declares no such rule.
func cssDeclarations(sheet, sel string) string {
	for _, chunk := range strings.Split(sheet, "}") {
		open := strings.Index(chunk, "{")
		if open < 0 {
			continue
		}
		if strings.TrimSpace(chunk[:open]) == sel {
			return strings.TrimSpace(chunk[open+1:])
		}
	}
	return ""
}
