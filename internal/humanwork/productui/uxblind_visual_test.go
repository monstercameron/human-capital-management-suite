package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestUXBlindDirectoryDisambiguatesPreferredNames(t *testing.T) {
	row := peopleDataTableRow(PeopleRowProps{Name: "Rafael", WorkerNumber: "HC-21050"})
	markup, err := ui.RenderToString(row.Cells[0].Children[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Rafael", "HC-21050", `class="people-identity"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("employee identity missing %q", want)
		}
	}
}

func TestLegacyUXBLIND_011(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.people-page{gap:calc(var(--hcm-space-3) * var(--hcm-density));`,
		`padding-top:calc(var(--hcm-space-2) * var(--hcm-density));`,
		`padding-left:calc(var(--hcm-space-2) * var(--hcm-density));`,
		`@media (max-width:1050px){.people-table .data-table-row{`,
		`gap:calc(var(--hcm-space-1) * var(--hcm-density));`,
		`padding:calc(var(--hcm-space-2) * var(--hcm-density));`,
		`@media (max-width:1050px){.people-table .data-table-cell{`,
		`gap:calc(var(--hcm-space-2) * var(--hcm-density));`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("directory responsive spacing is missing %q", want)
		}
	}
}

func TestLegacyUXBLIND_019(t *testing.T) {
	css := Stylesheet()
	want := `@media (min-width:761px){.sidebar .nav-copy>.nav-label{hyphens:none;overflow-wrap:normal;white-space:normal;word-break:normal;}`
	if !strings.Contains(css, want) {
		t.Fatalf("sidebar labels are not constrained to word-boundary wrapping: %q", want)
	}
	if strings.Contains(css, `.sidebar .nav-copy>.nav-label{overflow-wrap:anywhere`) {
		t.Fatal("sidebar labels still permit arbitrary mid-word wrapping")
	}
}

func TestLegacyUXBLIND_026(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.global-search .global-search-input{`,
		`padding-inline:48px 14px;`,
		`.global-search-glyph{`,
		`inset-inline-start:15px;`,
		`width:20px;`,
		`text-align:center;`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("global search spacing is missing %q", want)
		}
	}
}

func TestUXBlindHeaderAndWorkflowStacking(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.page-head>.breadcrumbs{flex-basis:100%;min-width:0;}`,
		`.header-navigation-tools{`,
		`.header-navigation-tools>.global-search{flex:1 1 0;min-width:0;}`,
		`.people-table td:last-child:has(.popover-root[open]){z-index:8;}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("missing layout regression contract %q", want)
		}
	}
	if strings.Contains(css, "padding-inline:10px 14px 10px 48px") {
		t.Fatal("invalid four-value logical padding returned")
	}
}
