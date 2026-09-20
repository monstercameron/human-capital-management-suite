package productui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// UXLIVE-029's RED: typing "Amara" quickly into global search rendered the
// field as "Amaa" once the debounced results rerendered. GWC writes `value`
// whenever the rendered prop differs from the previous render's, so a render
// resolving after the next keystroke put an older query back into the field.
// The fix makes the field browser-owned (its value attribute is the mount
// seed and never changes) and moves query, generation and results into
// globalSearchController.

func uxlive029Items() []GlobalSearchItem {
	return []GlobalSearchItem{
		{ID: "person:amara", Kind: "person", KindLabel: "Person", Label: "Amara Okafor", Href: "/workspace/app/person?person=amara"},
		{ID: "person:amal", Kind: "person", KindLabel: "Person", Label: "Amal Haddad", Href: "/workspace/app/person?person=amal"},
		{ID: "page:people", Kind: "page", KindLabel: "Page", Label: "People", Href: "/workspace/app/people"},
	}
}

// typeInto records each keystroke's field contents, as OnInput does.
func typeInto(controller *globalSearchController, values ...string) []uint64 {
	generations := make([]uint64, 0, len(values))
	for _, value := range values {
		generations = append(generations, controller.Edit(value))
	}
	return generations
}

func uxlive029SearchInput(t *testing.T, markup string) *xhtml.Node {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var input *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if input == nil && node.Data == "input" && attr(node, "id") == "global-search-input" {
			input = node
		}
	})
	if input == nil {
		t.Fatal("global search rendered no input")
	}
	return input
}

func uxlive029HasAttr(node *xhtml.Node, name string) bool {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return true
		}
	}
	return false
}

// TestTodo_UXLIVE_029 is the PRIMARY: every keystroke survives, and a results
// answer only lands for its own exact query generation.
func TestTodo_UXLIVE_029(t *testing.T) {
	items := uxlive029Items()
	controller := newGlobalSearchController("")
	generations := typeInto(controller, "A", "Am", "Ama", "Amar", "Amara")

	// The debounced answer for "Ama" arrives after "Amara" was typed.
	if controller.Resolve(generations[2], "Ama", SearchGlobalItems(items, "Ama", globalSearchLimit)) {
		t.Fatal("an answer for a superseded generation was accepted")
	}
	if got := controller.Query(); got != "Amara" {
		t.Fatalf("query = %q after a stale answer, want Amara", got)
	}
	// A forged answer claiming the current generation for another query.
	if controller.Resolve(generations[4], "Amaa", nil) {
		t.Fatal("an answer for a different query was accepted under the current generation")
	}
	if !controller.Resolve(generations[4], "Amara", SearchGlobalItems(items, "Amara", globalSearchLimit)) {
		t.Fatal("the current generation's answer was refused")
	}
	results, query, current := controller.Results()
	if !current || query != "Amara" || len(results) == 0 || results[0].ID != "person:amara" {
		t.Fatalf("results = %+v for %q (current %v), want Amara first", results, query, current)
	}
	if got := controller.Query(); got != "Amara" {
		t.Fatalf("resolving results changed the query to %q", got)
	}

	// The rendered control is not controlled: without a seed it carries no
	// value attribute a rerender could write back.
	props := GlobalSearchProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Items: items}
	markup, err := ui.RenderToString(ui.CreateElement(GlobalSearch, props))
	if err != nil {
		t.Fatal(err)
	}
	if input := uxlive029SearchInput(t, markup); uxlive029HasAttr(input, "value") {
		t.Fatalf("unseeded global search input carries a controlled value: %s", markup)
	}
	// A seeded field (a shared ?q= address) renders the seed and its answer.
	props.InitialQuery = "Amara"
	markup, err = ui.RenderToString(ui.CreateElement(GlobalSearch, props))
	if err != nil {
		t.Fatal(err)
	}
	if got := attr(uxlive029SearchInput(t, markup), "value"); got != "Amara" {
		t.Fatalf("seeded value = %q, want Amara", got)
	}
	if !strings.Contains(markup, "Amara Okafor") || strings.Contains(markup, `aria-busy="true"`) {
		t.Fatalf("seeded search did not render its settled answer:\n%s", markup)
	}
	if globalSearchDebounceDelay != 250*time.Millisecond {
		t.Fatalf("debounce = %v, want 250ms", globalSearchDebounceDelay)
	}
}

// TestTodo_UXLIVE_029_Regression covers deletion, IME composition, clearing
// after a choice and out-of-order answers in both directions.
func TestTodo_UXLIVE_029_Regression(t *testing.T) {
	items := uxlive029Items()

	// Deletion: the answer for the longer query must not come back.
	controller := newGlobalSearchController("")
	long := typeInto(controller, "A", "Am", "Ama", "Amar", "Amara")
	short := typeInto(controller, "Amar", "Ama")
	if controller.Resolve(long[4], "Amara", SearchGlobalItems(items, "Amara", globalSearchLimit)) {
		t.Fatal("a pre-deletion answer was accepted")
	}
	if !controller.Resolve(short[1], "Ama", SearchGlobalItems(items, "Ama", globalSearchLimit)) {
		t.Fatal("the post-deletion answer was refused")
	}
	if got := controller.Query(); got != "Ama" {
		t.Fatalf("deletion left %q, want Ama", got)
	}

	// IME composition: intermediate composition strings are the reader's
	// field contents; answers for earlier composition states never replace
	// the composed text.
	ime := newGlobalSearchController("")
	steps := typeInto(ime, "a", "あ", "あま", "あまら", "アマラ")
	for index, value := range []string{"a", "あ", "あま", "あまら"} {
		if ime.Resolve(steps[index], value, nil) {
			t.Fatalf("composition answer for %q was accepted after the text became アマラ", value)
		}
	}
	if got := ime.Query(); got != "アマラ" {
		t.Fatalf("composition query = %q, want アマラ", got)
	}

	// Stale results stay visible and marked not current until answered.
	stale := newGlobalSearchController("")
	first := stale.Edit("Am")
	stale.Resolve(first, "Am", SearchGlobalItems(items, "Am", globalSearchLimit))
	stale.Edit("Amara")
	if results, query, current := stale.Results(); current || query != "Am" || len(results) == 0 {
		t.Fatalf("stale results = %d for %q current=%v; want the Am answer kept and marked stale", len(results), query, current)
	}
	// Repeating the same contents is not a new generation.
	if a, b := stale.Edit("Amara"), stale.Edit("Amara"); a != b {
		t.Fatalf("an unchanged field bumped the generation %d -> %d", a, b)
	}

	// Choosing a destination clears query and results together.
	generation := stale.Clear()
	if stale.Query() != "" {
		t.Fatal("clear left a query")
	}
	if results, _, _ := stale.Results(); len(results) != 0 {
		t.Fatal("clear left results")
	}
	if stale.Resolve(generation-1, "Amara", nil) {
		t.Fatal("an answer from before the clear was accepted")
	}

	// Results handed out are copies; a caller cannot rewrite the answer.
	copyCheck := newGlobalSearchController("x")
	gen, query := copyCheck.Request()
	copyCheck.Resolve(gen, query, []GlobalSearchItem{{ID: "a", Label: "A"}})
	got, _, _ := copyCheck.Results()
	got[0].Label = "mutated"
	if again, _, _ := copyCheck.Results(); again[0].Label != "A" {
		t.Fatal("results were shared with the caller")
	}
}

// TestTodo_UXLIVE_029_Race runs typing and out-of-order answers on separate
// goroutines. Whatever the interleaving, the query is exactly the last
// keystroke and any accepted answer is for that exact query.
func TestTodo_UXLIVE_029_Race(t *testing.T) {
	items := uxlive029Items()
	keystrokes := []string{"A", "Am", "Ama", "Amar", "Amara"}
	for round := 0; round < 50; round++ {
		controller := newGlobalSearchController("")
		var wg sync.WaitGroup
		requests := make(chan struct {
			generation uint64
			query      string
		}, len(keystrokes))
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, key := range keystrokes {
				generation := controller.Edit(key)
				requests <- struct {
					generation uint64
					query      string
				}{generation, key}
			}
			close(requests)
		}()
		for worker := 0; worker < 3; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for request := range requests {
					controller.Resolve(request.generation, request.query, SearchGlobalItems(items, request.query, globalSearchLimit))
				}
			}()
		}
		wg.Wait()
		if got := controller.Query(); got != "Amara" {
			t.Fatalf("round %d: query = %q, want Amara", round, got)
		}
		_, query, current := controller.Results()
		if current && query != "Amara" {
			t.Fatalf("round %d: current results answer %q", round, query)
		}
		if query != "" && !strings.HasPrefix("Amara", query) {
			t.Fatalf("round %d: accepted results for %q, which was never typed", round, query)
		}
	}
}

// TestTodo_UXLIVE_029_Performance: the debounce collapses a burst of
// keystrokes into one results computation, and the field's own echo never
// waits on it (the controller records an edit without searching).
func TestTodo_UXLIVE_029_Performance(t *testing.T) {
	items := make([]GlobalSearchItem, 0, 200)
	for index := 0; index < 200; index++ {
		items = append(items, GlobalSearchItem{ID: "person:" + strings.Repeat("x", index%9+1), Kind: "person", Label: "Worker " + strings.Repeat("m", index%5+1)})
	}
	controller := newGlobalSearchController("")
	var last uint64
	for _, value := range []string{"A", "Am", "Ama", "Amar", "Amara", "Amara ", "Amara O", "Amara Ok"} {
		last = controller.Edit(value)
	}
	// Only the settled query is searched; every earlier generation's
	// computation is skipped rather than run and discarded.
	accepted := 0
	for generation := uint64(1); generation <= last; generation++ {
		current, query := controller.Request()
		if generation == current && controller.Resolve(generation, query, SearchGlobalItems(items, query, globalSearchLimit)) {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("a burst of %d keystrokes ran %d accepted searches, want exactly one", last, accepted)
	}

	// Recording a keystroke is constant work independent of the catalog.
	start := time.Now()
	for index := 0; index < 10000; index++ {
		controller.Edit(strings.Repeat("a", index%7+1))
	}
	if elapsed := time.Since(start); elapsed > globalSearchDebounceDelay {
		t.Fatalf("10000 keystroke records took %v, over the %v debounce budget", elapsed, globalSearchDebounceDelay)
	}
}

// TestTodo_UXLIVE_029_Browser checks the served shell document the browser
// hydrates: the global search field there is browser-owned (no value
// attribute for GWC to diff and write back) and keeps its combobox contract
// and its no-script q submission.
func TestTodo_UXLIVE_029_Browser(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	input := uxlive029SearchInput(t, doc)
	if uxlive029HasAttr(input, "value") {
		t.Fatalf("served global search field is controlled: value=%q", attr(input, "value"))
	}
	if attr(input, "role") != "combobox" || attr(input, "aria-autocomplete") != "list" || attr(input, "autocomplete") != "off" {
		t.Fatalf("served global search field lost its combobox contract: %+v", input.Attr)
	}
	if attr(input, "name") != "q" {
		t.Fatal("served global search field no longer submits q for the no-script fallback")
	}
}
