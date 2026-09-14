package journey

import (
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The live path is the product path, so it is tested here rather than left
// to a browser. Everything below runs natively: GWC's native slice
// implements UseState and UseEffect as immediate no-ops and drops event
// handlers from the SSR output, which means these tests can assert two
// distinct things -- that wiring a callback changes the markup in the ways
// that matter (a link stops being a bare link, a submit becomes a button),
// and that the callbacks themselves compute the right values.

// ----------------------------------------------------------------------
// Store
// ----------------------------------------------------------------------

func TestStoreNotifiesEverySubscriberOnEveryChange(t *testing.T) {
	s := NewStore(SampleListPage())
	var first, second int
	stopFirst := s.Subscribe(func() { first++ })
	s.Subscribe(func() { second++ })

	s.Set(SampleDetailPage())
	s.Update(func(p *Page) { p.Title = "changed" })
	s.SetValue("propose-job", "FIN-ANALYST4")

	if first != 3 || second != 3 {
		t.Fatalf("subscribers saw %d and %d changes, want 3 each", first, second)
	}
	stopFirst()
	s.Set(SampleListPage())
	if first != 3 {
		t.Error("an unsubscribed listener was still called")
	}
	if second != 4 {
		t.Error("a live listener stopped being called")
	}
	if s.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount = %d, want 1 after one unsubscribe", s.SubscriberCount())
	}
}

func TestStoreSetValueSeedsTheValueMap(t *testing.T) {
	s := NewStore(Page{})
	s.SetValue("a", "1")
	s.SetValue("b", "2")
	if got := s.Page().Values["a"]; got != "1" {
		t.Errorf("Values[a] = %q, want 1", got)
	}
	values := s.Values()
	values["a"] = "tampered"
	if s.Page().Values["a"] != "1" {
		t.Error("Values() returned the live map rather than a copy")
	}
}

func TestStoreUpdateIgnoresANilMutator(t *testing.T) {
	s := NewStore(SampleListPage())
	calls := 0
	s.Subscribe(func() { calls++ })
	s.Update(nil)
	if calls != 0 {
		t.Error("a nil mutator still notified subscribers")
	}
}

// TestStoreIsSafeUnderConcurrentWriters matters because an RPC answer and a
// keystroke can land on different goroutines even on wasm: the streaming
// callback is not the render loop.
func TestStoreIsSafeUnderConcurrentWriters(t *testing.T) {
	s := NewStore(Page{})
	s.Subscribe(func() { _ = s.Page().Title })

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.SetValue("f", "v")
			s.Update(func(p *Page) { p.Title = "t" })
			_ = s.Values()
		}(i)
	}
	wg.Wait()
	if s.Page().Title != "t" {
		t.Errorf("Title = %q after concurrent updates", s.Page().Title)
	}
}

// ----------------------------------------------------------------------
// The mounted component
// ----------------------------------------------------------------------

func TestLiveComponentRendersTheStoresCurrentPage(t *testing.T) {
	s := NewStore(SampleListPage())
	out := renderNode(t, LiveComponent(s))
	if !strings.Contains(out, "Promotion journeys") {
		t.Errorf("the list page did not render through the component:\n%s", firstN(out, 400))
	}

	s.Set(SampleDetailPage())
	out = renderNode(t, LiveComponent(s))
	if !strings.Contains(out, `id="journey-heading"`) {
		t.Error("the component did not follow the store to the detail page")
	}
}

func TestLiveComponentToleratesANilStore(t *testing.T) {
	out := renderNode(t, LiveComponent(nil))
	if !strings.Contains(out, `id="main-content"`) {
		t.Error("a nil store should still render the chrome, not panic")
	}
}

// ----------------------------------------------------------------------
// Wire
// ----------------------------------------------------------------------

func TestWireBindsEveryCallbackTheRendererUnderstands(t *testing.T) {
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

	if page.OnFieldChange == nil {
		t.Fatal("Wire left the field-change callback unbound")
	}
	for _, l := range page.Nav {
		if l.OnNavigate == nil {
			t.Fatalf("nav link %q was left unwired", l.Label)
		}
	}
	for _, j := range page.List.Journeys {
		if j.OnOpen == nil {
			t.Fatalf("journey %q was left unwired", j.IntentID)
		}
	}
	if page.List.Form.OnSubmit == nil {
		t.Fatal("the proposal form was left unwired")
	}

	// Each closure has to capture its own row, not the loop variable.
	page.Nav[1].OnNavigate()
	page.List.Journeys[2].OnOpen()
	if len(navigated) != 2 || navigated[0] != page.Nav[1].Href || navigated[1] != page.List.Journeys[2].Href {
		t.Errorf("navigation callbacks captured the wrong rows: %v", navigated)
	}

	page.List.Form.OnSubmit(map[string]string{"k": "v"})
	if len(submitted) != 1 || submitted[0] != "propose" {
		t.Errorf("submit callbacks fired as %v, want [propose]", submitted)
	}
	if lastValues["k"] != "v" {
		t.Error("the submitted values were not passed through")
	}
}

func TestWireBindsGroupedJourneyCards(t *testing.T) {
	page := SampleListPage()
	first := page.List.Journeys[0]
	second := page.List.Journeys[1]
	page.List.Groups = []JourneySubjectGroup{
		{Subject: "Jane", Journeys: []JourneyCard{first}},
		{Subject: "Priya", Journeys: []JourneyCard{second}},
	}
	var navigated []string
	page = Wire(NewStore(page), page, func(href string) { navigated = append(navigated, href) }, nil)
	for group := range page.List.Groups {
		for _, card := range page.List.Groups[group].Journeys {
			if card.OnOpen == nil {
				t.Fatalf("group %q card %q has no live navigation", page.List.Groups[group].Subject, card.IntentID)
			}
			card.OnOpen()
		}
	}
	if len(navigated) != 2 || navigated[0] != first.Href || navigated[1] != second.Href {
		t.Fatalf("grouped navigation = %q, want [%q %q]", navigated, first.Href, second.Href)
	}
}

func TestWireBindsOneCallbackPerAction(t *testing.T) {
	s := NewStore(Page{})
	var fired []string
	page := Wire(s, SampleDetailPage(), nil, func(id string, _ map[string]string) {
		fired = append(fired, id)
	})
	if len(page.Detail.Actions) < 2 {
		t.Fatal("the fixture needs at least two actions for this to mean anything")
	}
	for _, a := range page.Detail.Actions {
		if a.OnSubmit == nil {
			t.Fatalf("action %q was left unwired", a.ID)
		}
		a.OnSubmit(nil)
	}
	want := make([]string, 0, len(page.Detail.Actions))
	for _, a := range page.Detail.Actions {
		want = append(want, a.ID)
	}
	if strings.Join(fired, ",") != strings.Join(want, ",") {
		t.Errorf("actions fired as %v, want %v -- a closure captured the loop variable", fired, want)
	}
}

func TestWireBindsFocusedProposalSubmissionAndSoftwareNavigation(t *testing.T) {
	s := NewStore(Page{})
	var navigated string
	var submitted string
	p := Page{Proposal: &ProposalView{
		JourneysLink: NavLink{Href: "#/journeys"},
		Form:         ProposalForm{Fields: []Field{{ID: "worker", Name: "worker_ref", Kind: fieldKindHidden, Value: "jane-doe"}}},
	}}
	p = Wire(s, p, func(href string) { navigated = href }, func(id string, _ map[string]string) { submitted = id })
	if p.Proposal.Form.OnSubmit == nil || p.Proposal.JourneysLink.OnNavigate == nil {
		t.Fatal("focused proposal callbacks were not wired")
	}
	p.Proposal.Form.OnSubmit(nil)
	p.Proposal.JourneysLink.OnNavigate()
	if submitted != actionPropose || navigated != "#/journeys" {
		t.Fatalf("focused callbacks submitted %q and navigated %q", submitted, navigated)
	}
}

func TestWireOnANilStoreIsANoOp(t *testing.T) {
	page := Wire(nil, SampleListPage(), func(string) {}, func(string, map[string]string) {})
	if page.OnFieldChange != nil {
		t.Error("Wire bound callbacks to a nil store")
	}
}

// ----------------------------------------------------------------------
// What the wiring does to the markup
// ----------------------------------------------------------------------

// TestLiveFormsSubmitThroughTheClientNotTheBrowser: live forms retain a real
// submit control so browser constraint validation runs consistently for a
// pointer click and Enter. The form handler prevents the fallback POST only
// after the browser admits the submission.
func TestLiveFormsSubmitThroughTheClientNotTheBrowser(t *testing.T) {
	plain := mustRender(t, SampleListPage())
	if !strings.Contains(plain, `type="submit"`) {
		t.Error("without a callback the proposal button should be a real submit")
	}

	s := NewStore(Page{})
	livePage := Wire(s, SampleListPage(), nil, func(string, map[string]string) {})
	out := mustRender(t, livePage)
	if !strings.Contains(out, `type="submit"`) {
		t.Error("a live form bypasses native form submission and validation")
	}
	// The route survives, so a client that fails to boot degrades to a POST.
	if !strings.Contains(out, `action="`+SampleListPage().List.Form.Action+`"`) {
		t.Error("the live form dropped its fallback route")
	}
	if !strings.Contains(out, `method="post"`) {
		t.Error("the live form dropped its fallback method")
	}
}

// TestControlledFieldsRenderTheClientsValue: on the live path the value
// comes from Page.Values, which is what makes a keystroke survive a
// re-render triggered by an unrelated RPC answer.
func TestControlledFieldsRenderTheClientsValue(t *testing.T) {
	l := live{
		values:        map[string]string{"f": "typed by the user"},
		onFieldChange: func(string, string) {},
	}
	out := renderNode(t, fieldNode(l, Field{ID: "f", Name: "n", Label: "L", Kind: fieldKindText, Value: "from the engine"}, false))
	if !strings.Contains(out, `value="typed by the user"`) {
		t.Errorf("the control did not render the client's value:\n%s", out)
	}
	if strings.Contains(out, "from the engine") {
		t.Error("the control fell back to the engine's value while the client had one")
	}

	// A field the client has not touched keeps the engine's value, so the
	// first live render matches the shell exactly.
	out = renderNode(t, fieldNode(l, Field{ID: "other", Name: "n", Label: "L", Kind: fieldKindText, Value: "from the engine"}, false))
	if !strings.Contains(out, `value="from the engine"`) {
		t.Errorf("an untouched field lost its seeded value:\n%s", out)
	}
}

func TestControlledSelectSelectsByValueNotByFlag(t *testing.T) {
	f := Field{ID: "f", Name: "n", Label: "L", Kind: fieldKindSelect, Options: []Option{
		{Value: "a", Label: "A", Selected: true},
		{Value: "b", Label: "B"},
	}}
	l := live{values: map[string]string{"f": "b"}, onFieldChange: func(string, string) {}}
	out := renderNode(t, fieldNode(l, f, false))
	if !strings.Contains(out, `selected value="b"`) {
		t.Errorf("the select did not follow the client's value:\n%s", out)
	}
	if strings.Contains(out, `selected value="a"`) {
		t.Error("the stale Selected flag still won")
	}
}

// TestCollectMergesFieldsAndHiddenWithHiddenWinning is the security-shaped
// half of the submission: a projected field named after the CSRF token must
// not be able to overwrite it.
func TestCollectMergesFieldsAndHiddenWithHiddenWinning(t *testing.T) {
	l := live{values: map[string]string{"typed": "new"}, onFieldChange: func(string, string) {}}
	got := l.collect(
		map[string]string{"csrf_token": "real", "intent_id": "int_1"},
		[]Field{
			{ID: "typed", Name: "reason", Kind: fieldKindTextarea, Value: "seed"},
			{ID: "untouched", Name: "grade", Kind: fieldKindText, Value: "P3"},
			{ID: "h", Name: "decision", Kind: fieldKindHidden, Value: "approve"},
			{ID: "forged", Name: "csrf_token", Kind: fieldKindText, Value: "forged"},
			{ID: "nameless", Name: "", Kind: fieldKindText, Value: "ignored"},
		})

	want := map[string]string{
		"csrf_token": "real",
		"intent_id":  "int_1",
		"reason":     "new",
		"grade":      "P3",
		"decision":   "approve",
	}
	if len(got) != len(want) {
		t.Fatalf("collected %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("collected[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// TestLiveLinksKeepTheirHref: a link the client handles is still a link.
// Dropping href would break middle-click, "open in new tab" and copy-link,
// and would leave nothing for a screen reader to announce as a destination.
func TestLiveLinksKeepTheirHref(t *testing.T) {
	s := NewStore(Page{})
	page := Wire(s, SampleListPage(), func(string) {}, nil)
	out := mustRender(t, page)
	for _, l := range page.Nav {
		if !strings.Contains(out, `href="`+l.Href+`"`) {
			t.Errorf("live nav link %q lost its href", l.Label)
		}
	}
	for _, j := range page.List.Journeys {
		if !strings.Contains(out, `href="`+j.Href+`"`) {
			t.Errorf("live journey card %q lost its href", j.IntentID)
		}
	}
}

// TestHandlersNeverReachTheMarkup: GWC keeps event handlers out of its SSR
// output, which is what lets every other test in this package assert on
// markup. If that ever changed, an inline handler attribute would be both a
// CSP violation (script-src 'none') and a silent behaviour change.
func TestHandlersNeverReachTheMarkup(t *testing.T) {
	s := NewStore(Page{})
	page := Wire(s, SampleDetailPage(), func(string) {}, func(string, map[string]string) {})
	doc, err := Document(page)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	for _, attr := range []string{"onclick=", "onsubmit=", "oninput=", "onchange="} {
		if strings.Contains(strings.ToLower(doc), attr) {
			t.Errorf("a handler leaked into the markup as %q", attr)
		}
	}
}

// TestLiveAndPlainTreesAgreeOnStructure: the live client and the no-browser
// test path must render the same page, or every assertion made without a
// browser is about a document nobody sees. Event handlers are runtime values
// and deliberately do not alter the serialized structure.
func TestLiveAndPlainTreesAgreeOnStructure(t *testing.T) {
	plain := mustRender(t, SampleDetailPage())
	s := NewStore(Page{})
	livened := mustRender(t, Wire(s, SampleDetailPage(), func(string) {}, func(string, map[string]string) {}))

	for _, marker := range []string{
		`id="journey-heading"`, `id="stages-heading"`, `id="findings-heading"`,
		`id="workflow-heading"`, `id="outcome-heading"`, `id="evidence-heading"`,
		`id="actions-heading"`, `id="timeline-heading"`, `class="jn-band"`, `class="jn-meter"`,
	} {
		if !strings.Contains(plain, marker) || !strings.Contains(livened, marker) {
			t.Errorf("%q is not present on both the plain and the live tree", marker)
		}
	}
}

// TestUseEventIsOnlyCalledWhenThereIsACallback keeps the hook discipline
// visible: GWC's toHandler routes a plain func through ui.UseEvent, which
// is a positional hook on wasm. The renderer must therefore not create a
// handler for a callback the contract left nil, or the hook sequence would
// change with the data.
func TestUseEventIsOnlyCalledWhenThereIsACallback(t *testing.T) {
	if activate(nil).Value() != nil {
		t.Error("activate(nil) produced a handler")
	}
	if clickHandler(nil, nil).Value() != nil {
		t.Error("clickHandler(nil) produced a handler")
	}
	if (live{}).input(Field{ID: "f"}).Value() != nil {
		t.Error("an uncontrolled field produced an input handler")
	}
	if (live{}).submitHandler(nil, nil, nil).Value() != nil {
		t.Error("submitHandler(nil) produced a handler")
	}

	l := live{onFieldChange: func(string, string) {}}
	if l.input(Field{ID: "f"}).Value() == nil {
		t.Error("a controlled field produced no input handler")
	}
	if activate(func() {}).Value() == nil {
		t.Error("activate(fn) produced no handler")
	}
	var got map[string]string
	h := l.submitHandler(func(v map[string]string) { got = v }, map[string]string{"csrf_token": "t"}, nil)
	if h.Value() == nil {
		t.Fatal("submitHandler(fn) produced no handler")
	}
	// Invoking the wrapped function is what proves it carries the right
	// closure; on the native slice the wrapper is the function itself.
	fn, ok := h.Value().(func(ui.FormEvent))
	if !ok {
		t.Fatalf("submit handler has type %T, want func(ui.FormEvent)", h.Value())
	}
	fn(ui.FormEvent{})
	if got["csrf_token"] != "t" {
		t.Errorf("the submit handler carried %v, want the hidden inputs", got)
	}
}
