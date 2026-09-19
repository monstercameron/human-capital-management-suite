package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_UXLIVE_003_Browser is the rendering half of UXLIVE-003. The
// projection fix (internal/intent/app) stops this particular 299-character
// value reaching a cell; this keeps the table readable whatever value
// arrives, because the Change column going off-screen is a layout defect
// the table should not have in the first place.
func TestTodo_UXLIVE_003_Browser(t *testing.T) {
	rows := []ComparisonRow{
		{Label: "Job code", Current: "OPS-HRBP2", Proposed: "OPS-HRBP3", Changed: true},
		{Label: "Position", Current: "—", Proposed: strings.Repeat("Z", 299), Changed: true},
		{Label: "Base pay", Current: "USD 93,000.00", Proposed: "USD 98,500.00", Delta: "+USD 5,500.00 (+5.9%)", Changed: true},
	}
	markup, err := ui.RenderToString(comparisonTableLocale("en-US", rows))
	if err != nil {
		t.Fatalf("render comparison table: %v", err)
	}
	if !strings.Contains(markup, "jn-compare") {
		t.Fatalf("comparison table carries no width-bounded class:\n%s", markup)
	}
	if !strings.Contains(markup, "Change") {
		t.Fatalf("comparison table lost its Change column:\n%s", markup)
	}

	sheet := Stylesheet()
	fixed := cssRule(t, sheet, ".jn-compare")
	if !strings.Contains(fixed, "table-layout:fixed") && !strings.Contains(fixed, "table-layout: fixed") {
		t.Fatalf(".jn-compare does not bound its column widths: %q", fixed)
	}
	cells := cssRule(t, sheet, ".jn-compare :is(td,th)")
	if !strings.Contains(cells, "anywhere") {
		t.Fatalf(".jn-compare cells cannot wrap a long value: %q", cells)
	}
	// Measured live: the change cell holds white-space:nowrap from
	// `.jn-table td.jn-change`, which outranks a cell-level rule and alone
	// kept 41px of the table off its wrapper.
	if !strings.Contains(cells, "white-space:normal") && !strings.Contains(cells, "white-space: normal") {
		t.Fatalf(".jn-compare cells are still allowed to refuse wrapping: %q", cells)
	}
	change := cssRule(t, sheet, ".jn-compare.jn-table :is(td,th).jn-change")
	if !strings.Contains(change, "white-space:normal") && !strings.Contains(change, "white-space: normal") {
		t.Fatalf("the change cell can still refuse to wrap: %q", change)
	}
}
