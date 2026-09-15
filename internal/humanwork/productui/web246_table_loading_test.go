package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WEB_246(t *testing.T) {
	markup := renderWEB246Table(t, ResolveProductLocale(DefaultProductLocale), true)
	for _, want := range []string{
		`class="data-table-scroll is-busy"`,
		`aria-busy="true"`,
		`class="data-table-loader"`,
		`class="data-table-spinner"`,
		`data-row-id="worker-1"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("busy data table missing %q: %s", want, markup)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		`.data-table-loader-anchor{display:flex;height:0`,
		`.data-table-loader{align-items:center`,
		`.data-table-spinner{border:2px solid var(--line)`,
		`.data-table-scroll.is-busy .data-table-body{opacity:0.64`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("busy data-table styling missing %q", want)
		}
	}
}

func TestTodo_WEB_246_Browser(t *testing.T) {
	view := testView(PagePeople)
	view.RefreshingRegion = RefreshRegionPeopleDirectory
	markup, err := ui.RenderToString(BuildRefreshing(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="people-directory-table-viewport"`,
		`class="data-table-scroll is-busy"`,
		`class="data-table-loader"`,
		"Avery Patel",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("People warm refresh missing %q", want)
		}
	}
	if strings.Contains(markup, "loading-proxy") {
		t.Fatal("People table refresh replaced resolved rows with a page loading proxy")
	}
}

func TestTodo_WEB_246_Accessibility(t *testing.T) {
	markup := renderWEB246Table(t, ResolveProductLocale(DefaultProductLocale), true)
	for _, want := range []string{
		`role="region"`,
		`aria-label="Employees"`,
		`aria-busy="true"`,
		`role="status"`,
		`aria-live="polite"`,
		`aria-atomic="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("busy table accessibility contract missing %q", want)
		}
	}
	if strings.Contains(markup, `aria-hidden="true"><span class="data-table-loader-label"`) {
		t.Fatal("busy-table status text was hidden from assistive technology")
	}
}

func TestTodo_WEB_246_I18N(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			context := ResolveProductLocale(locale)
			label := context.Text("table.loading")
			if label == "" || label == "table.loading" {
				t.Fatalf("%s did not resolve table.loading", locale)
			}
			markup := renderWEB246Table(t, context, true)
			if !strings.Contains(markup, label) {
				t.Fatalf("%s busy table did not render localized status %q", locale, label)
			}
		})
	}
}

func TestTodo_WEB_246_Performance(t *testing.T) {
	start := time.Now()
	for range 100 {
		_ = renderWEB246Table(t, ResolveProductLocale(DefaultProductLocale), true)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("100 busy table renders took %s, want <=250ms", elapsed)
	}
	css := Stylesheet()
	for _, want := range []string{
		`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .data-table-spinner`,
		`@media (prefers-reduced-motion:no-preference)`,
		`:root[data-hcm-motion-preference="reduce"] .data-table-scroll.is-busy .data-table-body`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("busy table motion contract missing %q", want)
		}
	}
}

func TestTodo_WEB_246_Regression(t *testing.T) {
	ready := renderWEB246Table(t, ResolveProductLocale(DefaultProductLocale), false)
	busy := renderWEB246Table(t, ResolveProductLocale(DefaultProductLocale), true)
	for _, markup := range []string{ready, busy} {
		for _, want := range []string{`id="employees"`, `id="employees-viewport"`, `data-row-id="worker-1"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("stable table geometry missing %q", want)
			}
		}
	}
	if strings.Contains(ready, `aria-busy="true"`) || strings.Contains(ready, "data-table-loader") {
		t.Fatal("resolved table retained busy-only semantics")
	}
}

func renderWEB246Table(t *testing.T, locale LocaleContext, busy bool) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(DataTable, DataTableProps{
		ID: "employees", Caption: "Employees", AriaLabel: "Employees", Busy: busy, BusyLabel: locale.Text("table.loading"),
		Columns: []DataTableColumnProps{{ID: "name", Label: "Name"}},
		Rows:    []DataTableRowProps{{ID: "worker-1", Cells: []DataTableCellProps{{ColumnID: "name", Text: "Rafael Torres"}}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}
