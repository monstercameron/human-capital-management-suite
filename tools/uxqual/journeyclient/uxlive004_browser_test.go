package journeyclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// TestTodo_UXLIVE_004_Browser renders the action dialog that the browser
// mounts, proving the typed defaults reach the actual input controls rather
// than stopping at the JourneyCard projection.
func TestTodo_UXLIVE_004_Browser(t *testing.T) {
	head := uxlive004Card(t)
	actions := interventionActions(nil, head, nil, nil, nil)
	markup := mustRender(t, journey.Page{
		Locale: "en-US",
		Detail: &journey.DetailView{Journey: head, Actions: actions},
	})
	for _, name := range []string{NameEditJobCode, NameEditGrade, NameEditBase, NameEditEffective} {
		if !strings.Contains(markup, `name="`+name+`"`) {
			t.Fatalf("rendered edit dialog lost field %s:\n%s", name, markup)
		}
	}
	for _, want := range []string{
		`<option selected value="WRK-MGR">WRK-MGR</option>`,
		`<option selected value="M2">M2</option>`,
		`name="proposed_base"`, `value="98000.00"`,
		`name="effective_date"`, `value="2026-07-01"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("rendered edit dialog did not prefill with %q:\n%s", want, markup)
		}
	}
}
