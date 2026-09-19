package journey

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The People panel is the page's entry point: a reader who cannot create an
// employee, see them and pick them never reaches the proposal form at all.
// These tests hold that path together -- the table's columns, the selection
// marker, the two provenances, the row action's two forms (a link without a
// client and a button with one), the form's POST, the empty state and the
// wiring -- and they run the same way every other test in this package does,
// against markup rather than a simulated DOM.

// ----------------------------------------------------------------------
// The table
// ----------------------------------------------------------------------

var peopleRowPattern = regexp.MustCompile(`(?s)<tr\b[^>]*class="jn-people-row".*?</tr>`)

// peopleRows returns each rendered People row, in document order.
func peopleRows(page string) []string { return peopleRowPattern.FindAllString(page, -1) }

func TestPeopleTableRendersEveryWorkerWithEveryColumn(t *testing.T) {
	p := SampleListPage()
	out := mustRender(t, p)

	rows := peopleRows(out)
	if len(rows) != len(p.List.People.Workers) {
		t.Fatalf("rendered %d people rows for %d workers", len(rows), len(p.List.People.Workers))
	}
	for i, c := range p.List.People.Workers {
		row := rows[i]
		columns := map[string]string{
			"name":      c.Name,
			"number":    c.Number,
			"title":     c.Title,
			"org unit":  c.OrgUnit,
			"location":  c.Location,
			"pay":       c.PayLine,
			"hire date": c.HireDate,
			"source":    c.SourceLabel,
			"job line":  c.JobCode + " · " + c.Grade,
			"journeys":  ">" + strconv.Itoa(c.OpenJourneys) + "<",
		}
		for what, want := range columns {
			if !strings.Contains(row, want) {
				t.Errorf("row %d (%s) is missing its %s (%q)", i, c.Name, what, want)
			}
		}
		if !strings.Contains(row, `class="jn-num jn-people-pay"`) {
			t.Errorf("row %d does not render pay in the tabular column", i)
		}
	}

	// The header names every column, and the action column is named for
	// assistive technology rather than left as a bare empty cell.
	for _, want := range []string{
		">Employee<", ">Job code and grade<", ">Org unit<", ">Location<",
		">Base pay<", ">Hired<", ">Source<", ">Journeys<", ">Row actions<",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the people table has no %q column header", want)
		}
	}
	if !strings.Contains(out, `<caption class="jn-visually-hidden">`) {
		t.Error("the people table has no caption; a screen reader would meet an unnamed table")
	}
}

func TestPeopleTableWindowsLargeWorkforcesAndKeepsSelectionVisible(t *testing.T) {
	workers := make([]WorkerCard, peoplePreviewLimit+5)
	for index := range workers {
		workers[index] = WorkerCard{Ref: fmt.Sprintf("worker-%02d", index), Name: fmt.Sprintf("Worker %02d", index)}
	}
	view := PeopleView{Workers: workers, SelectedRef: workers[len(workers)-1].Ref, DirectoryLink: NavLink{Label: "Open the full People directory", Href: "/workspace/app/people"}}
	out := renderNode(t, peopleTable(view))
	rows := peopleRows(out)
	if len(rows) != peoplePreviewLimit+1 {
		t.Fatalf("rendered %d preview rows, want %d", len(rows), peoplePreviewLimit+1)
	}
	if !strings.Contains(out, workers[len(workers)-1].Name) || !strings.Contains(out, "Showing 21 of 25 employees") || !strings.Contains(out, "/workspace/app/people") {
		t.Fatalf("window did not retain the selected worker or directory handoff: %s", out)
	}
}

// TestPeopleTableScrollsInsideItsOwnContainer is the 320px reflow contract:
// the table keeps its natural width and scrolls in its own box, and that box
// is reachable from the keyboard, which is the only way its content is
// reachable for someone not using a mouse.
func TestPeopleTableScrollsInsideItsOwnContainer(t *testing.T) {
	out := mustRender(t, SampleListPage())
	wrap := regexp.MustCompile(`<div[^>]*class="jn-tablewrap jn-peoplewrap"[^>]*>`).FindString(out)
	if wrap == "" {
		t.Fatal("the people table is not inside a scroll container")
	}
	// GWC emits the DOM property spelling (tabIndex); HTML attribute names
	// are case-insensitive, so the assertion is too.
	lowered := strings.ToLower(wrap)
	for _, want := range []string{`tabindex="0"`, `role="region"`, `aria-label="people"`} {
		if !strings.Contains(lowered, want) {
			t.Errorf("the scroll container is missing %q: %s", want, wrap)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		".jn-peoplewrap{max-height:34rem;overflow-y:auto",
		".jn-tablewrap{border:1px solid var(--jn-hairline);border-radius:var(--jn-r2);overflow-x:auto;}",
		".jn-table thead th{background-color:var(--jn-surface-muted);color:var(--jn-ink-muted);font-size:0.75rem;font-weight:600;letter-spacing:var(--hcm-tracking-caps,.05em);",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet is missing %q", want)
		}
	}
	if !strings.Contains(css, "position:sticky;text-transform:uppercase;top:0;white-space:nowrap;}") {
		t.Error("the table header does not stick inside the scroll container")
	}
	if !strings.Contains(css, ".jn-people tbody th,.jn-people tbody td{height:2.75rem") {
		t.Error("the people rows are not the 44px (2.75rem) dense-but-airy height")
	}
}

// ----------------------------------------------------------------------
// Selection
// ----------------------------------------------------------------------

func TestSelectedWorkerRowIsMarkedForAssistiveTechnologyAndStyling(t *testing.T) {
	p := SampleListPage()
	out := mustRender(t, p)

	selected := 0
	for i, row := range peopleRows(out) {
		switch {
		case strings.Contains(row, `aria-selected="true"`):
			selected++
			if !strings.Contains(row, `data-selected="true"`) {
				t.Errorf("row %d is selected for assistive technology but carries no styling hook", i)
			}
			// WCAG 1.4.1: the accent rule is not the only thing that says so.
			if !strings.Contains(row, `class="jn-people-selected"`) || !strings.Contains(row, ">Selected<") {
				t.Errorf("row %d is marked selected only by its tint and its ARIA state", i)
			}
		case !strings.Contains(row, `aria-selected="false"`):
			t.Errorf("row %d declares no selection state at all", i)
		}
	}
	if selected != 1 {
		t.Fatalf("%d rows are marked selected, want exactly 1", selected)
	}
	if !strings.Contains(Stylesheet(), `.jn-people tbody tr[data-selected="true"] .jn-people-idcell{box-shadow:inset 3px 0 0 0 var(--jn-accent);}`) {
		t.Error("the selected row has no accent left rule in the stylesheet")
	}
}

// TestSelectionFollowsEitherHalfOfTheContract: a projection may mark the row
// itself or name it through SelectedRef, and either one has to mark exactly
// the row it meant.
func TestSelectionFollowsEitherHalfOfTheContract(t *testing.T) {
	cases := map[string]PeopleView{
		"by the row's own flag": {Workers: []WorkerCard{{Ref: "a", Name: "A"}, {Ref: "b", Name: "B", Selected: true}}},
		"by SelectedRef":        {Workers: []WorkerCard{{Ref: "a", Name: "A"}, {Ref: "b", Name: "B"}}, SelectedRef: "b"},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			if workerSelected(v, v.Workers[0]) {
				t.Error("the wrong row is selected")
			}
			if !workerSelected(v, v.Workers[1]) {
				t.Error("the intended row is not selected")
			}
		})
	}
	// A ref nobody carries selects nobody rather than the first row.
	v := PeopleView{Workers: []WorkerCard{{Ref: "a", Name: "A"}}, SelectedRef: "zzz"}
	if workerSelected(v, v.Workers[0]) {
		t.Error("an unmatched SelectedRef still selected a row")
	}
	// So does an empty ref on an empty-ref card.
	if workerSelected(PeopleView{}, WorkerCard{}) {
		t.Error("two empty refs matched each other")
	}
}

func TestProposalHeadingNamesTheSelectedWorker(t *testing.T) {
	p := SampleListPage()
	if got := selectedWorkerName(p.List.People); got != "Omar Reyes" {
		t.Fatalf("selectedWorkerName = %q, want Omar Reyes", got)
	}
	out := mustRender(t, p)
	if !strings.Contains(out, `<h2 id="propose-heading">Propose a promotion for Omar Reyes</h2>`) {
		t.Error("the proposal panel does not name the selected employee")
	}

	// Nobody selected, no People view at all, and a ref nobody carries all
	// fall back to the plain wording rather than claiming a selection.
	for name, mutate := range map[string]func(*Page){
		"no selection": func(p *Page) { p.List.People.SelectedRef = "" },
		"no people":    func(p *Page) { p.List.People = nil },
		"unknown ref":  func(p *Page) { p.List.People.SelectedRef = "worker:NOBODY" },
	} {
		t.Run(name, func(t *testing.T) {
			p := SampleListPage()
			mutate(&p)
			out := mustRender(t, p)
			if !strings.Contains(out, `<h2 id="propose-heading">Propose a promotion</h2>`) {
				t.Error("the proposal heading did not fall back to its plain wording")
			}
		})
	}
}

// ----------------------------------------------------------------------
// Provenance
// ----------------------------------------------------------------------

func TestCreatedWorkersCarryTheirChipAndSparkAndCorpusOnesDoNot(t *testing.T) {
	p := SampleListPage()
	out := mustRender(t, p)

	var created, corpus int
	for i, row := range peopleRows(out) {
		c := p.List.People.Workers[i]
		switch c.Source {
		case sourceCreated:
			created++
			if !strings.Contains(row, `data-source="CREATED"`) || !strings.Contains(row, `data-tone="info"`) {
				t.Errorf("row %d (%s) does not carry the CREATED chip", i, c.Name)
			}
			if !strings.Contains(row, "M10 3.5 11.5 8") {
				t.Errorf("row %d (%s) has no spark glyph on its chip", i, c.Name)
			}
		default:
			corpus++
			if !strings.Contains(row, `data-source="CORPUS"`) {
				t.Errorf("row %d (%s) does not carry the CORPUS chip", i, c.Name)
			}
			if strings.Contains(row, "M10 3.5 11.5 8") {
				t.Errorf("row %d (%s) drew the new-record spark", i, c.Name)
			}
		}
		// Neither chip is ever a bare color.
		if !strings.Contains(row, c.SourceLabel) {
			t.Errorf("row %d (%s) has a source chip with no word in it", i, c.Name)
		}
	}
	if created == 0 || corpus == 0 {
		t.Fatalf("the fixture covers %d created and %d corpus workers; both must be non-zero", created, corpus)
	}
}

func TestSourceVocabularyIsClosedAndAlwaysLabelled(t *testing.T) {
	cases := map[string]string{
		sourceCorpus: sourceCorpus, sourceCreated: sourceCreated,
		"": sourceCorpus, "IMPORTED": sourceCorpus, "created": sourceCorpus,
	}
	for in, want := range cases {
		if got := sourceOf(in); got != want {
			t.Errorf("sourceOf(%q) = %q, want %q", in, got, want)
		}
	}
	// A chip whose label the projection forgot still says a word.
	for _, source := range []string{sourceCorpus, sourceCreated, "nonsense"} {
		out := renderNode(t, sourceChip(WorkerCard{Source: source}))
		if !strings.Contains(out, sourceWord(sourceOf(source))) {
			t.Errorf("sourceChip(%q) rendered without a fallback label: %s", source, out)
		}
	}
}

// TestWorkerToneReachesTheRowWithoutCarryingMeaningAlone: Tone is a styling
// hook. It has to reach the markup, and it must not be the only thing that
// says anything.
func TestWorkerToneReachesTheRowAsADataAttribute(t *testing.T) {
	for _, tone := range []string{toneInfo, toneSuccess, toneWarning, toneDanger, toneNeutral, "chartreuse"} {
		v := PeopleView{Workers: []WorkerCard{{Ref: "a", Name: "A", Tone: tone}}}
		out := renderNode(t, workerRow(v, v.Workers[0]))
		if !strings.Contains(out, `data-tone="`+toneOf(tone)+`"`) {
			t.Errorf("tone %q did not reach the row as data-tone=%q: %s", tone, toneOf(tone), out)
		}
		if strings.Contains(out, "chartreuse") {
			t.Error("an unrecognised tone reached the markup")
		}
	}
	if !strings.Contains(Stylesheet(), `.jn-people tbody tr[data-tone="warning"] .jn-people-idcell`) {
		t.Error("the stylesheet declares no rule for a toned row")
	}
}

// ----------------------------------------------------------------------
// The row action and the live wiring
// ----------------------------------------------------------------------

// TestWorkerRowActionIsALinkWithoutAClientAndAButtonWithOne mirrors
// TestLiveFormsSubmitThroughTheClientNotTheBrowser for the People table: the
// whole create-see-pick-propose flow works with scripting off, and the same
// semantic controls gain callbacks once a client is wired.
func TestWorkerRowActionIsALinkWithoutAClientAndAButtonWithOne(t *testing.T) {
	p := SampleListPage()
	plain := mustRender(t, p)
	for _, c := range p.List.People.Workers {
		if !strings.Contains(plain, `href="`+c.ProposeHref+`"`) {
			t.Errorf("row %q offers no plain link to propose a promotion", c.Name)
		}
		if !strings.Contains(plain, "Propose promotion") {
			t.Fatal("no row offers the propose action at all")
		}
	}
	if strings.Contains(plain, `class="jn-people-idbox jn-people-pick"`) {
		t.Error("the SSR page rendered a selection button no browser could make work")
	}

	s := NewStore(Page{})
	livened := mustRender(t, Wire(s, SampleListPage(), func(string) {}, func(string, map[string]string) {}))
	for _, c := range SampleListPage().List.People.Workers {
		if strings.Contains(livened, `href="`+c.ProposeHref+`"`) {
			t.Errorf("row %q still renders a browser navigation once the client owns it", c.Name)
		}
	}
	if !strings.Contains(livened, `class="jn-people-idbox jn-people-pick"`) {
		t.Error("the live page has no selection control")
	}
	if !strings.Contains(livened, `aria-pressed="true"`) {
		t.Error("the live selection control does not announce which row is picked")
	}
	// Handlers never reach the markup. The live form keeps a real submit
	// control so native constraint validation runs before its callback.
	if strings.Contains(strings.ToLower(livened), "onclick=") {
		t.Error("a click handler leaked into the markup")
	}
	if !strings.Contains(livened, `type="submit"`) {
		t.Error("the live page bypasses native form submission and validation")
	}
}

// TestPeopleCallbacksAreOnlyCreatedWhenTheContractCarriesThem keeps the hook
// discipline visible: ui.UseEvent is positional on wasm, so the renderer
// must not manufacture a handler for a nil callback.
func TestPeopleCallbacksAreOnlyCreatedWhenTheContractCarriesThem(t *testing.T) {
	out := renderNode(t, workerActionCell(WorkerCard{Name: "A"}))
	if strings.Contains(out, "<a") || strings.Contains(out, "<button") {
		t.Errorf("a card with neither OnPropose nor ProposeHref rendered an affordance: %s", out)
	}
	if !strings.Contains(out, "no action available") {
		t.Errorf("the inert action cell says nothing at all: %s", out)
	}
}

func TestWireBindsThePeopleCallbacks(t *testing.T) {
	s := NewStore(Page{})
	var navigated []string
	var submitted []string
	var lastValues map[string]string

	page := Wire(s, SampleListPage(),
		func(href string) { navigated = append(navigated, href) },
		func(id string, values map[string]string) {
			submitted = append(submitted, id)
			lastValues = values
		})

	people := page.List.People
	if people == nil {
		t.Fatal("Wire dropped the People view")
	}
	for _, c := range people.Workers {
		if c.OnSelect == nil || c.OnPropose == nil {
			t.Fatalf("worker %q was left unwired", c.Ref)
		}
	}
	if people.Form.OnSubmit == nil {
		t.Fatal("the New employee form was left unwired")
	}

	// Each closure has to capture its own row, not the loop variable.
	people.Workers[2].OnSelect()
	if len(navigated) != 1 || navigated[0] != selectWorkerHref(people.Workers[2].Ref) {
		t.Errorf("selection navigated to %v, want the third row's route", navigated)
	}

	people.Workers[3].OnPropose()
	if len(submitted) != 1 || submitted[0] != "propose-for" {
		t.Errorf("the row action submitted as %v, want [propose-for]", submitted)
	}
	if lastValues["worker_ref"] != people.Workers[3].Ref {
		t.Errorf("propose-for carried worker_ref %q, want %q", lastValues["worker_ref"], people.Workers[3].Ref)
	}
	if len(lastValues) != 1 {
		t.Errorf("propose-for carried %v, want only worker_ref", lastValues)
	}

	people.Form.OnSubmit(map[string]string{"legal_name": "Rosa Iglesias"})
	if len(submitted) != 2 || submitted[1] != "create-worker" {
		t.Errorf("the New employee form submitted as %v, want create-worker second", submitted)
	}
	if lastValues["legal_name"] != "Rosa Iglesias" {
		t.Error("the New employee form's values were not passed through")
	}
}

// TestWithSelectWorkerOverridesTheNavigationFallback is the point of the
// option: a client that owns selection as state rather than as a route says
// so without Wire's four-argument call changing for anyone else.
func TestWithSelectWorkerOverridesTheNavigationFallback(t *testing.T) {
	s := NewStore(Page{})
	var picked []string
	var navigated []string
	page := Wire(s, SampleListPage(),
		func(href string) { navigated = append(navigated, href) },
		func(string, map[string]string) {},
		WithSelectWorker(func(ref string) { picked = append(picked, ref) }),
		nil, // a nil option is ignored rather than panicking
	)
	page.List.People.Workers[1].OnSelect()
	if len(picked) != 1 || picked[0] != page.List.People.Workers[1].Ref {
		t.Errorf("the select callback saw %v, want the second row's ref", picked)
	}
	if len(navigated) != 0 {
		t.Errorf("the option did not displace the navigation fallback: %v", navigated)
	}
}

// TestWireLeavesPeopleAloneWithoutTheCallbacksItNeeds: no nav and no select
// option means no selection control, which is better than a control that
// does nothing.
func TestWireLeavesPeopleAloneWithoutTheCallbacksItNeeds(t *testing.T) {
	s := NewStore(Page{})
	page := Wire(s, SampleListPage(), nil, nil)
	for _, c := range page.List.People.Workers {
		if c.OnSelect != nil {
			t.Errorf("worker %q got a selection callback with nothing to call", c.Ref)
		}
		if c.OnPropose != nil {
			t.Errorf("worker %q got a propose callback with no submit", c.Ref)
		}
	}
	if page.List.People.Form.OnSubmit != nil {
		t.Error("the New employee form was wired with no submit callback")
	}
	// And a page with no People view at all does not panic.
	p := SampleListPage()
	p.List.People = nil
	if got := Wire(s, p, func(string) {}, func(string, map[string]string) {}); got.List.People != nil {
		t.Error("Wire invented a People view")
	}
}

// ----------------------------------------------------------------------
// The New employee form
// ----------------------------------------------------------------------

func TestWorkerFormPostsEveryFieldToItsRoute(t *testing.T) {
	p := SampleListPage()
	out := mustRender(t, p)
	f := p.List.People.Form
	form := formWithAction(t, out, f.Action)

	if !strings.Contains(form, `method="post"`) {
		t.Error("the New employee form is not a POST")
	}
	if !strings.Contains(form, `type="submit"`) {
		t.Error("the New employee form has no browser submit button")
	}
	if !strings.Contains(form, ">Add employee</button>") {
		t.Error("the New employee form's submit is not labelled")
	}
	assertHiddenInputs(t, form, f.Hidden)

	for _, field := range f.Fields {
		if !strings.Contains(form, `id="`+field.ID+`"`) {
			t.Errorf("field %q did not render", field.ID)
		}
		if !strings.Contains(form, `name="`+field.Name+`"`) {
			t.Errorf("field %q did not carry its submitted name %q", field.ID, field.Name)
		}
		if !strings.Contains(form, `<label class="jn-label" for="`+field.ID+`"`) {
			t.Errorf("field %q has no label bound to it", field.ID)
		}
	}
	// The two adorned money fields keep their adornments.
	for _, want := range []string{`>USD</span>`, `>%</span>`} {
		if !strings.Contains(form, want) {
			t.Errorf("the form lost the adornment %q", want)
		}
	}
}

func TestWorkerFormCollectsItsFieldsOnALiveSubmit(t *testing.T) {
	s := NewStore(Page{})
	var got map[string]string
	page := Wire(s, SampleListPage(), nil, func(id string, values map[string]string) {
		if id == "create-worker" {
			got = values
		}
	})
	f := page.List.People.Form
	l := live{onFieldChange: s.SetValue, values: map[string]string{"worker-legal-name": "Typed by the user"}}
	f.OnSubmit(l.collect(f.Hidden, f.Fields))

	if got["csrf_token"] != fixtureCSRF {
		t.Errorf("the live submission dropped the CSRF token: %v", got)
	}
	if got["legal_name"] != "Typed by the user" {
		t.Errorf("legal_name = %q, want the client's value", got["legal_name"])
	}
	if got["position_id"] != "POS-6041" {
		t.Errorf("position_id = %q, want the field's seeded value", got["position_id"])
	}
	for _, name := range []string{"job_code", "grade", "org_unit", "pay_zone", "hire_date", "manager_ref"} {
		if _, ok := got[name]; !ok {
			t.Errorf("the live submission dropped the %q field entirely", name)
		}
	}
	// A select whose selection lives in Option.Selected rather than in
	// Field.Value collects as empty until the reader touches it. That is
	// live.value's existing behaviour, shared with the proposal form, and is
	// pinned here so a change to it is a deliberate one.
	if got["grade"] != "" {
		t.Errorf("grade = %q; live.value no longer falls back to Field.Value for a select", got["grade"])
	}
}

func TestWorkerFormIsRefusedWithAReason(t *testing.T) {
	p := SampleListPage()
	p.List.People.Form.Disabled = true
	p.List.People.Form.DisabledReason = "This cell records no workforce facts."
	out := mustRender(t, p)

	if !strings.Contains(out, `id="worker-form-disabled"`) ||
		!strings.Contains(out, p.List.People.Form.DisabledReason) {
		t.Error("the refused form names no reason")
	}
	if !strings.Contains(out, `aria-describedby="worker-form-disabled"`) {
		t.Error("the disabled submit is not described by its reason")
	}
	form := formWithAction(t, out, p.List.People.Form.Action)
	for _, control := range regexp.MustCompile(`<(input|select|textarea)[^>]*>`).FindAllString(form, -1) {
		if strings.Contains(control, `type="hidden"`) {
			continue
		}
		if !strings.Contains(control, "disabled") {
			t.Errorf("control is still editable while the form is refused: %s", control)
		}
	}
}

// ----------------------------------------------------------------------
// Empty and zero values
// ----------------------------------------------------------------------

func TestPeopleEmptyStateReplacesTheTable(t *testing.T) {
	p := SampleListPage()
	p.List.People.Workers = nil
	p.List.People.Empty = "No employee is readable under this purpose yet."
	out := mustRender(t, p)

	if strings.Contains(out, "jn-people-row") {
		t.Error("the table rendered alongside the empty state")
	}
	if !strings.Contains(out, "jn-empty-inset") || !strings.Contains(out, p.List.People.Empty) {
		t.Error("the people empty state did not render")
	}
	if !strings.Contains(out, ">0 employees<") {
		t.Error("the people count did not fall to zero")
	}
	// The form that fixes the emptiness is still there.
	if !strings.Contains(out, `id="new-employee-heading"`) {
		t.Error("an empty workforce hid the only way to add to it")
	}
	// An empty message the projection forgot still says something.
	if out := renderNode(t, peopleEmptyState("")); !strings.Contains(out, "No employees are available in this view yet.") {
		t.Errorf("the empty state has no fallback message: %s", out)
	}
}

func TestPeopleCountLabelIsSingularForOne(t *testing.T) {
	cases := map[int]string{0: "0 employees", 1: "1 employee", 2: "2 employees"}
	for n, want := range cases {
		if got := peopleCountLabel(n); got != want {
			t.Errorf("peopleCountLabel(%d) = %q, want %q", n, got, want)
		}
	}
	if got := journeyWord(1); got != "journey" {
		t.Errorf("journeyWord(1) = %q, want journey", got)
	}
	if got := journeyWord(0); got != "journeys" {
		t.Errorf("journeyWord(0) = %q, want journeys", got)
	}
}

// TestJobLineDegradesRatherThanDanglingItsSeparator: a worker with no grade
// should not render "OPS-HRBP2 ·".
func TestJobLineDegradesRatherThanDanglingItsSeparator(t *testing.T) {
	cases := []struct{ code, grade, want string }{
		{"OPS-HRBP2", "P2", "OPS-HRBP2 · P2"},
		{"OPS-HRBP2", "", "OPS-HRBP2"},
		{"", "P2", "P2"},
		{"", "", ""},
	}
	for _, tc := range cases {
		if got := jobLine(WorkerCard{JobCode: tc.code, Grade: tc.grade}); got != tc.want {
			t.Errorf("jobLine(%q, %q) = %q, want %q", tc.code, tc.grade, got, tc.want)
		}
	}
}

func TestPeopleSectionIsAbsentRatherThanEmptyWhenTheProjectionHasNone(t *testing.T) {
	if node := peopleSection(nil); node != nil {
		t.Fatal("peopleSection(nil) rendered something")
	}
	p := SampleListPage()
	p.List.People = nil
	out := mustRender(t, p)
	for _, absent := range []string{`id="people-heading"`, `id="new-employee-heading"`, "jn-people-row"} {
		if strings.Contains(out, absent) {
			t.Errorf("a page with no People view still rendered %q", absent)
		}
	}
	// The rest of the list view is unaffected.
	if !strings.Contains(out, `id="propose-heading"`) || !strings.Contains(out, `id="journeys-heading"`) {
		t.Error("dropping People took the rest of the list view with it")
	}
	if len(formsIn(out)) != 1 {
		t.Errorf("a page with no People view rendered %d forms, want 1", len(formsIn(out)))
	}
}

func TestPeopleZeroValuesRender(t *testing.T) {
	cases := map[string]func() string{
		"zero people view":  func() string { return renderNode(t, peopleSection(&PeopleView{})) },
		"zero worker row":   func() string { return renderNode(t, workerRow(PeopleView{}, WorkerCard{})) },
		"zero source chip":  func() string { return renderNode(t, sourceChip(WorkerCard{})) },
		"zero action cell":  func() string { return renderNode(t, workerActionCell(WorkerCard{})) },
		"zero worker form":  func() string { return renderNode(t, newEmployeeSection(live{}, WorkerForm{})) },
		"zero people table": func() string { return renderNode(t, peopleTable(PeopleView{})) },
	}
	for name, render := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked on a zero value: %v", r)
				}
			}()
			if out := render(); out == "" {
				t.Error("rendered nothing at all")
			}
		})
	}
	// A zero form still offers a labelled submit rather than a bare button.
	if out := renderNode(t, newEmployeeSection(live{}, WorkerForm{})); !strings.Contains(out, ">Add employee</button>") {
		t.Errorf("the zero form's submit has no label: %s", firstN(out, 400))
	}
}

// ----------------------------------------------------------------------
// The fixture
// ----------------------------------------------------------------------

// TestSamplePeopleCoverBothProvenancesAndEveryColumn keeps the fixture
// exercising every branch above: an under-filled People fixture would
// quietly stop testing half the panel.
func TestSamplePeopleCoverBothProvenancesAndEveryColumn(t *testing.T) {
	v := SampleListPage().List.People
	if v == nil {
		t.Fatal("SampleListPage has no People view")
	}
	if len(v.Workers) != 6 {
		t.Errorf("the fixture has %d workers, want the six the brief names", len(v.Workers))
	}
	if v.Note == "" || v.Empty == "" {
		t.Error("the People fixture needs both a note and an empty message")
	}

	names := map[string]bool{}
	refs := map[string]bool{}
	sources := map[string]int{}
	selected := 0
	for _, c := range v.Workers {
		for what, value := range map[string]string{
			"ref": c.Ref, "name": c.Name, "number": c.Number, "title": c.Title,
			"job code": c.JobCode, "grade": c.Grade, "org unit": c.OrgUnit,
			"location": c.Location, "pay": c.PayLine, "hire date": c.HireDate,
			"source label": c.SourceLabel, "propose href": c.ProposeHref,
		} {
			if value == "" {
				t.Errorf("worker %q is missing its %s", c.Name, what)
			}
		}
		if refs[c.Ref] {
			t.Errorf("worker ref %q is used twice", c.Ref)
		}
		refs[c.Ref] = true
		names[c.Name] = true
		sources[sourceOf(c.Source)]++
		if c.Selected {
			selected++
		}
	}
	for _, want := range []string{"Jane Doe", "Omar Reyes", "Lena Park", "Noor Haddad"} {
		if !names[want] {
			t.Errorf("the fixture is missing the corpus employee %q", want)
		}
	}
	if sources[sourceCreated] != 2 {
		t.Errorf("%d workers are CREATED, want the two the brief names", sources[sourceCreated])
	}
	if selected != 1 {
		t.Errorf("%d workers are selected, want exactly 1", selected)
	}
	if v.SelectedRef == "" || !refs[v.SelectedRef] {
		t.Errorf("SelectedRef %q names no worker in the table", v.SelectedRef)
	}
}

func TestSampleWorkerFormCoversEveryControlThePanelDeclares(t *testing.T) {
	f := SampleListPage().List.People.Form
	if f.Action == "" {
		t.Error("the New employee form posts nowhere")
	}
	if f.Hidden["csrf_token"] == "" {
		t.Error("the New employee form carries no CSRF token")
	}
	if f.Submit != "Add employee" {
		t.Errorf("the submit reads %q, want Add employee", f.Submit)
	}

	ids := map[string]bool{}
	kinds := map[string]bool{}
	byName := map[string]Field{}
	for _, field := range f.Fields {
		if field.ID == "" || field.Name == "" || field.Label == "" {
			t.Errorf("field %+v is missing an id, name or label", field)
		}
		if ids[field.ID] {
			t.Errorf("field id %q is used twice; label association would be ambiguous", field.ID)
		}
		ids[field.ID] = true
		kinds[field.Kind] = true
		byName[field.Name] = field
	}
	for _, kind := range []string{fieldKindText, fieldKindNumber, fieldKindDate, fieldKindSelect} {
		if !kinds[kind] {
			t.Errorf("the New employee form has no %q field", kind)
		}
	}
	for _, name := range []string{
		"legal_name", "preferred_name", "job_code", "grade", "org_unit", "position_id",
		"location", "pay_zone", "base_pay", "bonus_target", "hire_date", "manager_ref",
	} {
		if _, ok := byName[name]; !ok {
			t.Errorf("the New employee form has no %q field", name)
		}
	}
	if byName["base_pay"].Prefix != "USD" {
		t.Errorf("base pay carries the prefix %q, want USD", byName["base_pay"].Prefix)
	}
	if byName["bonus_target"].Suffix != "%" {
		t.Errorf("bonus target carries the suffix %q, want %%", byName["bonus_target"].Suffix)
	}
	// No New employee field may collide with a proposal field's id, or the
	// two <label for> associations on one page would be ambiguous.
	for _, field := range SampleListPage().List.Form.Fields {
		if ids[field.ID] {
			t.Errorf("the proposal field id %q is reused by the New employee form", field.ID)
		}
	}
}
