package productui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_008_MergedUX(t *testing.T) {
	view := testView(PagePeople)
	markup, err := ui.RenderToString(ui.CreateElement(PeopleFilter, PeopleFilterProps{
		I18nProps: I18nProps{Locale: view.Locale}, Query: "Avery", Action: "/workspace/app/people", PageSize: 50,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `name="page_size"`) || !strings.Contains(markup, `value="50"`) {
		t.Fatalf("filter dropped selected page size: %s", markup)
	}
}

func TestTodo_UXAUDIT_008_Browser_MergedUX(t *testing.T) {
	view := testView(PagePeople)
	view.People = latencyPeople(100)
	view.PeoplePageSize = 100
	IndexPeople(view.People)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(doc, `class="data-table-row people-row-item people-row"`); got != 100 {
		t.Fatalf("desktop 100-row page rendered %d employee rows", got)
	}
	for _, want := range []string{`id="people-directory-table-viewport"`, `name="page_size"`, `value="100"`, `class="people-filter"`, `class="people-pager"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("People page omitted %q", want)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		`.page-head[data-hcm-page="people"]{margin-bottom:16px;}`,
		`.people-page{gap:10px;}`,
		`.people-page .people-filter{padding-bottom:12px;padding-top:12px;}`,
		`.people-page .people-table .data-table-cell{padding-bottom:8px;padding-top:8px;}`,
		`@media (min-width:1200px){.people-page .directory-tools>div{align-items:baseline;display:flex;gap:10px;}}`,
		`@media (min-width:1200px){.people-page .people-filter{align-items:center;grid-template-columns:1fr;}}`,
		`@media (min-width:1200px){.people-page .people-filter>label{display:none;}}`,
	} {
		if !strings.Contains(css, want) {
			selector := strings.SplitN(want, "{", 2)[0]
			near := "absent"
			if start := strings.Index(css, selector+"{"); start >= 0 {
				end := strings.IndexByte(css[start:], '}')
				if end >= 0 {
					near = css[start : start+end+1]
				}
			}
			t.Errorf("compact People viewport lacks %q; rendered %q", want, near)
		}
	}
}

func TestTodo_UXAUDIT_008_Regression_MergedUX(t *testing.T) {
	local := PeopleDirectoryProps{
		InputKey: "same", Refreshing: false,
		Pagination: PeoplePaginationProps{Page: 2, PageCount: 3, PageSize: PageSizeControlProps{Value: 50}},
	}
	incoming := local
	incoming.Refreshing = true
	kept, reset := reconcilePeopleDirectoryState(incoming, peopleDirectoryState{InputKey: "same", Current: local})
	if reset || !kept.Current.Refreshing {
		t.Fatalf("refresh state was not applied: reset=%t state=%+v", reset, kept.Current)
	}
	if kept.Current.Pagination.Page != 2 || kept.Current.Pagination.PageSize.Value != 50 {
		t.Fatalf("local pagination was discarded: %+v", kept.Current.Pagination)
	}
}

func TestTodo_UXAUDIT_008_Accessibility_MergedUX(t *testing.T) {
	view := testView(PagePeople)
	row := PeopleRowProps{
		I18nProps: I18nProps{Locale: view.Locale}, ID: "worker-1", Name: "Avery Patel", Href: "/workspace/app/person?person=worker-1",
		WorkflowsUnavailableReason: view.Locale.Text("workflow.no_promotion_path"),
	}
	markup, err := ui.RenderToString(ui.CreateElement(PeopleTable, PeopleTableProps{
		I18nProps: I18nProps{Locale: view.Locale}, Rows: []PeopleRowProps{row},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "No eligible promotion role") {
		t.Fatalf("eligibility reason missing: %s", markup)
	}
	if strings.Contains(markup, "Start Promotion") {
		t.Fatalf("unavailable workflow became actionable: %s", markup)
	}
	if !strings.Contains(markup, `aria-label="Why workflows are unavailable for Avery Patel: No eligible promotion role`) ||
		!strings.Contains(markup, `<summary`) ||
		!strings.Contains(markup, `>Unavailable<svg`) ||
		!strings.Contains(markup, `class="people-workflow-unavailable-reason"`) {
		t.Fatalf("compact row lost its named explanation disclosure: %s", markup)
	}
}

func TestTodo_UXAUDIT_008_Density(t *testing.T) {
	view := testView(PagePeople)
	const count = 100
	rows := make([]PeopleRowProps, count)
	for i := range rows {
		rows[i] = PeopleRowProps{
			ID: "worker-" + strconv.Itoa(i), Name: "Avery Patel", Role: "Accountant",
			WorkflowsUnavailableReason: view.Locale.Text("workflow.no_promotion_path"),
		}
	}
	markup, err := ui.RenderToString(ui.CreateElement(PeopleTable, PeopleTableProps{
		I18nProps: I18nProps{Locale: view.Locale}, Rows: rows,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(markup, `class="popover-root people-workflow-menu people-unavailable-menu"`); got != count {
		t.Fatalf("100-row directory has %d compact unavailable controls, want %d", got, count)
	}
	if got := strings.Count(markup, `>Unavailable<svg`); got != count {
		t.Fatalf("100-row directory repeats full explanations in visible action cells: compact summaries=%d", got)
	}
}

func TestTodo_UXAUDIT_008_Layout(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.people-directory,.people-directory .data-table-scroll{max-height:none;overflow:visible;}`,
		`@media (min-width:1051px){.people-workflow-menu>.people-workflow-options{`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("People page scroll/menu rule missing %q", want)
		}
	}
	if !strings.Contains(css, `.people-workflow-menu>.people-workflow-options{margin-block-start:0;min-width:0;position:absolute;`) {
		t.Fatal("People row menu must float without stretching its table row")
	}
}

func TestTodo_UXAUDIT_008_I18N_UnavailableDisclosure(t *testing.T) {
	for _, tc := range []struct{ locale, label string }{
		{"en-US", "Unavailable"},
		{"de-DE", "Nicht verfügbar"},
		{"ar", "غير متاح"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			locale := ResolveProductLocale(tc.locale)
			reason := PromotionAvailabilityReason(locale, PromotionIneligible)
			markup, err := ui.RenderToString(ui.CreateElement(PeopleTable, PeopleTableProps{
				I18nProps: I18nProps{Locale: locale},
				Rows: []PeopleRowProps{{ID: "worker-1", Name: "Avery Patel",
					WorkflowsUnavailableReason: reason}},
			}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, ">"+tc.label+"<svg") || strings.Contains(markup, "people.workflow_unavailable") {
				t.Fatalf("%s disclosure label did not resolve: %s", tc.locale, markup)
			}
			announcement := locale.Text("people.workflow_unavailable_reason_aria", map[string]string{"name": "Avery Patel", "reason": reason})
			if !strings.Contains(markup, `aria-label="`+announcement+`"`) {
				t.Fatalf("%s unavailable action did not announce its reason: %s", tc.locale, markup)
			}
			genericMarkup, err := ui.RenderToString(ui.CreateElement(PeopleTable, PeopleTableProps{
				I18nProps: I18nProps{Locale: locale},
				Rows:      []PeopleRowProps{{ID: "worker-2", Name: "Avery Patel"}},
			}))
			if err != nil {
				t.Fatal(err)
			}
			genericAnnouncement := locale.Text("people.workflow_unavailable_generic_aria", map[string]string{"name": "Avery Patel"})
			if !strings.Contains(genericMarkup, `aria-label="`+genericAnnouncement+`"`) {
				t.Fatalf("%s generic action falsely claimed a specific reason: %s", tc.locale, genericMarkup)
			}
		})
	}
}

func BenchmarkPeopleUnavailableActions100Rows(b *testing.B) {
	locale := ResolveProductLocale("en-US")
	rows := make([]PeopleRowProps, 100)
	for index := range rows {
		rows[index] = PeopleRowProps{
			ID:                         "worker-" + strconv.Itoa(index),
			Name:                       "Avery Patel",
			WorkflowsUnavailableReason: PromotionAvailabilityReason(locale, PromotionWithheld),
		}
	}
	props := PeopleTableProps{I18nProps: I18nProps{Locale: locale}, Rows: rows}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := ui.RenderToString(ui.CreateElement(PeopleTable, props)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTodo_UXAUDIT_008_I18N_CompactSearchHint(t *testing.T) {
	for _, tc := range []struct{ locale, hint string }{
		{"en-US", "Name, role, or worker ID"},
		{"de-DE", "Name, Rolle oder ID"},
		{"ar", "الاسم أو الدور أو المعرّف"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			markup, err := ui.RenderToString(ui.CreateElement(PeopleFilter, PeopleFilterProps{
				I18nProps: I18nProps{Locale: ResolveProductLocale(tc.locale)},
			}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, `placeholder="`+tc.hint+`"`) {
				t.Fatalf("%s People search hint is not localized or compact: %s", tc.locale, markup)
			}
		})
	}
}
