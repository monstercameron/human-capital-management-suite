package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-072: cross-surface authorization noninterference. Work
// verdicts are population-scoped since WEB-069, but people verdicts are
// not: any non-empty verdict map empties the workforce directory, so a
// map addressing only journeys — saying nothing about workers —
// invents authority over the directory and wipes its rows, facets, and
// counts. Silence must be population-scoped symmetrically: each
// population gates only on verdicts addressing it, and authorization
// for one surface neither leaks into nor breaks another.
func TestTodo_WEB_072(t *testing.T) {
	home := testView(PageHome)
	people := func(view View) []string {
		out := make([]string, 0)
		for _, person := range admittedPeople(view) {
			out = append(out, person.ID)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	work := func(view View) []string {
		out := make([]string, 0)
		for _, item := range admittedWork(view) {
			out = append(out, item.ID)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	allPeople := []string{"worker-jordan", "worker-avery", "worker-elena"}
	bothWork := []string{"intent-1", "intent-2"}
	personAddressed := map[string]AuthorizedRecord{
		"worker-avery":  {ID: "worker-avery", Disclosable: true},
		"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
		"worker-elena":  {ID: "worker-elena", Disclosable: true},
	}
	workAddressed := map[string]AuthorizedRecord{
		"intent-1": {ID: "intent-1", Disclosable: true},
		"intent-2": {ID: "intent-2", Disclosable: false, DenialReason: "No record access."},
	}
	mixed := map[string]AuthorizedRecord{
		"worker-avery":  {ID: "worker-avery", Disclosable: true},
		"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
		"worker-elena":  {ID: "worker-elena", Disclosable: true},
		"intent-1":      {ID: "intent-1", Disclosable: true},
		"intent-2":      {ID: "intent-2", Disclosable: false, DenialReason: "No record access."},
	}
	for _, cross := range []struct {
		name     string
		verdicts map[string]AuthorizedRecord
		people   []string
		work     []string
	}{
		{"silent server interferes with nothing", nil, allPeople, bothWork},
		{"person verdicts gate people only", personAddressed, []string{"worker-avery", "worker-elena"}, bothWork},
		{"work verdicts gate journeys only", workAddressed, allPeople, []string{"intent-1"}},
		{"mixed maps gate each population independently", mixed, []string{"worker-avery", "worker-elena"}, []string{"intent-1"}},
	} {
		view := home
		view.RecordVerdicts = cross.verdicts
		if got := people(view); !reflect.DeepEqual(got, cross.people) {
			t.Fatalf("%s directory = %q, want %q", cross.name, got, cross.people)
		}
		if got := work(view); !reflect.DeepEqual(got, cross.work) {
			t.Fatalf("%s journeys = %q, want %q", cross.name, got, cross.work)
		}
	}

	// A work-addressed map renders the full directory: three rows and
	// the unadmitted-by-nobody Jordan Lee.
	peopleView := testView(PagePeople)
	peopleView.RecordVerdicts = workAddressed
	doc, err := Render(peopleView)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if count := countClassTokens(root, "people-row"); count != 3 {
		t.Fatalf("work-addressed directory renders %d rows, want 3", count)
	}
	if body := web063BodyText(t, doc); !strings.Contains(body, "Jordan Lee") {
		t.Fatal("work-addressed directory loses Jordan Lee")
	}

	// A person-addressed map preserves the admitted journey set, while the
	// review filter still excludes the terminal item.
	workView := testView(PageWork)
	workView.WorkFilter = "review"
	workView.RecordVerdicts = personAddressed
	workDoc, err := Render(workView)
	if err != nil {
		t.Fatal(err)
	}
	workRoot, err := xhtml.Parse(strings.NewReader(workDoc))
	if err != nil {
		t.Fatal(err)
	}
	if count := countClassTokens(workRoot, "work-row"); count != 1 {
		t.Fatalf("person-addressed queue renders %d rows, want 1", count)
	}
}

// Golden: the noninterference matrix digest (variant → people, work).
func TestTodo_WEB_072_Golden(t *testing.T) {
	home := testView(PageHome)
	variants := map[string]map[string]AuthorizedRecord{
		"silent": nil,
		"person": {"worker-avery": {ID: "worker-avery", Disclosable: true},
			"worker-jordan": {ID: "worker-jordan", Disclosable: false},
			"worker-elena":  {ID: "worker-elena", Disclosable: true}},
		"work": {"intent-1": {ID: "intent-1", Disclosable: true},
			"intent-2": {ID: "intent-2", Disclosable: false}},
		"mixed": {"worker-avery": {ID: "worker-avery", Disclosable: true},
			"worker-jordan": {ID: "worker-jordan", Disclosable: false},
			"worker-elena":  {ID: "worker-elena", Disclosable: true},
			"intent-1":      {ID: "intent-1", Disclosable: true},
			"intent-2":      {ID: "intent-2", Disclosable: false}},
	}
	var builder strings.Builder
	for _, name := range []string{"silent", "person", "work", "mixed"} {
		view := home
		view.RecordVerdicts = variants[name]
		people := make([]string, 0)
		for _, person := range admittedPeople(view) {
			people = append(people, person.ID)
		}
		ids := make([]string, 0)
		for _, item := range admittedWork(view) {
			ids = append(ids, item.ID)
		}
		builder.WriteString(name)
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(people, ","))
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(ids, ","))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "91f69d1ef21408365478d9495755f520e3c23ec0f8f88576970550a1c6bb46d4"
	if got != want {
		t.Fatalf("noninterference matrix digest = %s, want %s", got, want)
	}
}

// Browser: mixed-governed directory and queue parse with exactly the
// admitted rows and no positive tabindex stops.
func TestTodo_WEB_072_Browser(t *testing.T) {
	mixed := map[string]AuthorizedRecord{
		"worker-avery":  {ID: "worker-avery", Disclosable: true},
		"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
		"worker-elena":  {ID: "worker-elena", Disclosable: true},
		"intent-1":      {ID: "intent-1", Disclosable: true},
		"intent-2":      {ID: "intent-2", Disclosable: false, DenialReason: "No record access."},
	}
	peopleView := testView(PagePeople)
	peopleView.RecordVerdicts = mixed
	workView := testView(PageWork)
	workView.WorkFilter = "review"
	workView.RecordVerdicts = mixed
	for _, browser := range []struct {
		name  string
		view  View
		class string
		rows  int
	}{
		{"directory", peopleView, "people-row", 2},
		{"queue", workView, "work-row", 1},
	} {
		doc, err := Render(browser.view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if count := countClassTokens(root, browser.class); count != browser.rows {
			t.Fatalf("mixed-governed %s renders %d rows, want %d", browser.name, count, browser.rows)
		}
		var positive int
		var walk func(node *xhtml.Node)
		walk = func(node *xhtml.Node) {
			if node.Type == xhtml.ElementNode {
				for _, attr := range node.Attr {
					if attr.Key == "tabindex" && strings.TrimSpace(attr.Val) != "" && attr.Val != "0" && !strings.HasPrefix(attr.Val, "-") {
						positive++
					}
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(root)
		if positive != 0 {
			t.Fatalf("mixed-governed %s carries %d positive tabindex stops", browser.name, positive)
		}
	}
}

// Conformance: no unresolved copy in three locales under a mixed map,
// determinism on both populations.
func TestTodo_WEB_072_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, page := range []PageID{PagePeople, PageWork} {
			view := testView(page)
			view.Locale = ResolveProductLocale(locale)
			view = ApplyLocale(view, view.Locale)
			view.RecordVerdicts = map[string]AuthorizedRecord{
				"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
				"intent-2":      {ID: "intent-2", Disclosable: false, DenialReason: "No record access."},
			}
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(doc, "⟦") {
				t.Fatalf("%s %s mixed render leaks an unresolved key", locale, page)
			}
		}
	}
	view := testView(PageHome)
	view.RecordVerdicts = map[string]AuthorizedRecord{"intent-2": {ID: "intent-2", Disclosable: false}}
	if !reflect.DeepEqual(admittedPeople(view), admittedPeople(view)) || !reflect.DeepEqual(admittedWork(view), admittedWork(view)) {
		t.Fatal("population limiting is nondeterministic")
	}
}

// Security: each population withholds exactly its denied records in
// every catalog locale — denied workers leave the directory while
// their journeys stay, denied journeys leave the queue while their
// workers stay.
func TestTodo_WEB_072_Security(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		peopleView := testView(PagePeople)
		peopleView.Locale = ResolveProductLocale(locale)
		peopleView = ApplyLocale(peopleView, peopleView.Locale)
		peopleView.RecordVerdicts = map[string]AuthorizedRecord{
			"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
			"intent-2":      {ID: "intent-2", Disclosable: false, DenialReason: "No record access."},
		}
		doc, err := Render(peopleView)
		if err != nil {
			t.Fatal(err)
		}
		if body := web063BodyText(t, doc); strings.Contains(body, "Jordan Lee") {
			t.Fatalf("%s directory keeps the denied worker", locale)
		}
		workView := testView(PageWork)
		workView.Locale = ResolveProductLocale(locale)
		workView = ApplyLocale(workView, workView.Locale)
		workView.WorkFilter = "review"
		workView.RecordVerdicts = map[string]AuthorizedRecord{
			"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
			"intent-1":      {ID: "intent-1", Disclosable: true},
			"intent-2":      {ID: "intent-2", Disclosable: false, DenialReason: "No record access."},
		}
		workDoc, err := Render(workView)
		if err != nil {
			t.Fatal(err)
		}
		workBody := web063BodyText(t, workDoc)
		if strings.Contains(workBody, "DES2 G6 → DES3 G7") {
			t.Fatalf("%s queue keeps the denied journey", locale)
		}
		if !strings.Contains(workBody, "ENG2 G6 → ENG3 G7") {
			t.Fatalf("%s queue loses the admitted journey", locale)
		}
	}
}
