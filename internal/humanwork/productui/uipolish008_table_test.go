package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_008(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowHistory, WorkflowHistoryProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Title:     "History", Density: HistoryDensityCompact,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="surface workflow-history history-density-compact"`) {
		t.Fatalf("compact history density did not produce its closed class: %s", markup)
	}
	unknown, err := ui.RenderToString(ui.CreateElement(WorkflowHistory, WorkflowHistoryProps{Density: HistoryDensity("wide")}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unknown, "history-density-") {
		t.Fatal("unknown history density escaped the closed vocabulary")
	}
	css := uipolish008TableStylesheet()
	for _, want := range []string{
		`@media (max-width:420px){.data-table .data-table-cell{gap:8px;overflow-wrap:anywhere;padding:2px;}}`,
		`@media (max-width:420px){.data-table-head{flex-wrap:wrap;overflow-x:visible;}}`,
		`.workflow-history.history-density-compact .history-row{gap:12px;padding-bottom:12px;padding-left:16px;padding-right:16px;padding-top:12px;}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("density stylesheet missing %q: %s", want, css)
		}
	}
}

func TestTodo_UIPOLISH_008_PersistedDensityReachesHistoryPage(t *testing.T) {
	view := testView(PageHistory)
	view.Appearance = DefaultCustomerTheme()
	view.Appearance.Density = "compact"
	markup, err := ui.RenderToString(historyPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="surface workflow-history history-density-compact"`) {
		t.Fatalf("persisted compact density did not reach the production history render: %s", markup)
	}

	view.Appearance.Density = "untrusted"
	markup, err = ui.RenderToString(historyPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "history-density-untrusted") {
		t.Fatalf("untrusted persisted density reached the history class boundary: %s", markup)
	}
}

func TestTodo_UIPOLISH_008_ProductionTableDensity(t *testing.T) {
	for _, density := range []string{"compact", "comfortable", "spacious"} {
		theme := DefaultCustomerTheme()
		theme.Density = density
		if got := CustomerThemeAttributes(theme)["data-hcm-density"]; got != density {
			t.Errorf("%s density became %q at the browser boundary", density, got)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		`:root[data-hcm-density="compact"]`,
		`:root[data-hcm-density="spacious"]`,
		`.data-table-cell{padding-bottom:calc(3px + var(--hcm-space-1) * var(--hcm-density))`,
		`min-height:44px`,
		`data-table-scroll`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("production table density missing %q", want)
		}
	}
	if strings.Contains(uipolish008TableStylesheet(), `display:none`) || strings.Contains(uipolish008TableStylesheet(), `order:`) {
		t.Fatal("density changed field visibility or reading order")
	}
}
