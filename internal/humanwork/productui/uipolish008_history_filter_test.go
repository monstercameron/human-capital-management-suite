package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_008_HistoryMobileFiltersUseIntrinsicControlHeight(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, `@media (max-width:760px){.history-filter-controls{display:grid;grid-template-columns:minmax(0,1fr);}}`) {
		t.Fatal("History filter controls must switch from flex-basis rows to a single intrinsic-height grid column on mobile")
	}
	if strings.Contains(css, `@media (max-width:760px){.history-filter-controls{display:flex;flex-direction:column;}}`) {
		t.Fatal("History mobile filters still inherit 150px/280px flex-basis as control height")
	}
}

func TestTodo_UIPOLISH_008_HistoryResponsiveRowsKeepColumnContext(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowHistoryItem, WorkflowHistoryItemProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("ar")},
		Person:    "Jane", Type: "ترقية", Summary: "ENG-SWE3 → ENG-MGR1", Outcome: "مسجل", Tone: "success",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="history-mobile-label"`, `>التغيير</span>`,
		`aria-label="التغيير · ENG-SWE3 → ENG-MGR1"`, `aria-label="النتيجة · مسجل"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("responsive History row lost context %q: %s", want, markup)
		}
	}
	css := Stylesheet()
	if !strings.Contains(css, `@media (max-width:1080px){.history-mobile-label{display:block;`) {
		t.Fatal("responsive History row does not expose the change label when column headers hide")
	}
}
