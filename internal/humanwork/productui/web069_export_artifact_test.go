package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-069: export and artifact visibility. Search honors record
// verdicts, but the work surfaces that present decision artifacts — the
// My Work queue with its proposal facts, the history extract with its
// summaries and digests, the insights and home counts — render every
// projected instance regardless of verdicts. An undisclosable journey
// keeps its rows, its compensation facts, and its share of every
// count. These extractable listings are what any export draws from, so
// the admitted-population rule must govern them: once the server
// speaks, only disclosable instances may appear.
func TestTodo_WEB_069(t *testing.T) {
	home := testView(PageHome)
	both := []string{"intent-1", "intent-2"}
	admitted := func(view View) []string {
		work := admittedWork(view)
		if len(work) == 0 {
			return nil
		}
		ids := make([]string, 0, len(work))
		for _, item := range work {
			ids = append(ids, item.ID)
		}
		return ids
	}
	for _, population := range []struct {
		name     string
		verdicts map[string]AuthorizedRecord
		want     []string
	}{
		{"silent server keeps the instances", nil, both},
		{"person-only verdicts leave journeys ungated", map[string]AuthorizedRecord{
			"worker-avery": {ID: "worker-avery", Disclosable: true},
		}, both},
		{"governed population drops undisclosable instances", map[string]AuthorizedRecord{
			"intent-1": {ID: "intent-1", Disclosable: true},
			"intent-2": {ID: "intent-2", Disclosable: false, DenialReason: "No record access."},
		}, []string{"intent-1"}},
		{"instances without a verdict drop once governed", map[string]AuthorizedRecord{
			"intent-1": {ID: "intent-1", Disclosable: true},
		}, []string{"intent-1"}},
		{"fully denied population is empty", map[string]AuthorizedRecord{
			"intent-1": {ID: "intent-1", Disclosable: false},
			"intent-2": {ID: "intent-2", Disclosable: false},
		}, nil},
	} {
		view := home
		view.RecordVerdicts = population.verdicts
		if got := admitted(view); !reflect.DeepEqual(got, population.want) {
			t.Fatalf("%s = %q, want %q", population.name, got, population.want)
		}
	}
	work := append([]WorkItem(nil), home.Work...)
	governed := View{Work: work, RecordVerdicts: map[string]AuthorizedRecord{"intent-1": {ID: "intent-1", Disclosable: true}}}
	_ = admittedWork(governed)
	if !reflect.DeepEqual(work, home.Work) {
		t.Fatal("work limiting mutates its inputs")
	}

	// The governed work queue drops the denied instance's row.
	doc, err := Render(web069View(PageWork, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	body := web063BodyText(t, doc)
	if strings.Contains(body, "DES2 G6 → DES3 G7") {
		t.Fatal("governed work queue keeps the denied instance")
	}
	if !strings.Contains(body, "ENG2 G6 → ENG3 G7") {
		t.Fatal("governed work queue loses the admitted instance")
	}

	// The governed history extract drops the denied artifact.
	historyDoc, err := Render(web069View(PageHistory, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(web063BodyText(t, historyDoc), "DES2 G6 → DES3 G7") {
		t.Fatal("governed history keeps the denied artifact")
	}
}

// Golden: the work-limit matrix digest (variant → admitted IDs).
func TestTodo_WEB_069_Golden(t *testing.T) {
	home := testView(PageHome)
	variants := map[string]map[string]AuthorizedRecord{
		"silent": nil,
		"all": {"intent-1": {ID: "intent-1", Disclosable: true},
			"intent-2": {ID: "intent-2", Disclosable: true}},
		"terminal-hidden": {"intent-1": {ID: "intent-1", Disclosable: true},
			"intent-2": {ID: "intent-2", Disclosable: false}},
		"open-only": {"intent-1": {ID: "intent-1", Disclosable: true}},
		"none": {"intent-1": {ID: "intent-1", Disclosable: false},
			"intent-2": {ID: "intent-2", Disclosable: false}},
	}
	var builder strings.Builder
	for _, name := range []string{"silent", "all", "terminal-hidden", "open-only", "none"} {
		view := home
		view.RecordVerdicts = variants[name]
		ids := make([]string, 0)
		for _, item := range admittedWork(view) {
			ids = append(ids, item.ID)
		}
		builder.WriteString(name)
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(ids, ","))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "34a25cbbae13177d4fb980e63c1084b8b87206d039116df7078689fba82d0a44"
	if got != want {
		t.Fatalf("work matrix digest = %s, want %s", got, want)
	}
}

// Browser: the governed queue parses with exactly the admitted rows,
// and no positive tabindex stops.
func TestTodo_WEB_069_Browser(t *testing.T) {
	governed := web069View(PageWork, "en-US")
	governed.WorkFilter = "review"
	silent := testView(PageWork)
	silent.WorkFilter = "review"
	for _, browser := range []struct {
		name string
		view View
		rows int
	}{
		{"governed", governed, 1},
		{"silent", silent, 1},
	} {
		doc, err := Render(browser.view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if count := countClassTokens(root, "work-row"); count != browser.rows {
			t.Fatalf("%s work queue renders %d rows, want %d", browser.name, count, browser.rows)
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
			t.Fatalf("%s work queue carries %d positive tabindex stops", browser.name, positive)
		}
	}
}

// Conformance: no unresolved copy in three locales, governed
// all-disclosable renders identically to a silent server on every work
// surface, determinism.
func TestTodo_WEB_069_Conformance(t *testing.T) {
	pages := []PageID{PageWork, PageHistory, PageInsights, PageHome}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, page := range pages {
			doc, err := Render(web069View(page, locale))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(doc, "⟦") {
				t.Fatalf("%s %s governed render leaks an unresolved key", locale, page)
			}
			silent := testView(page)
			silent.Locale = ResolveProductLocale(locale)
			silent = ApplyLocale(silent, silent.Locale)
			silentDoc, err := Render(silent)
			if err != nil {
				t.Fatal(err)
			}
			allowed := silent
			allowed.RecordVerdicts = map[string]AuthorizedRecord{
				"intent-1": {ID: "intent-1", Disclosable: true},
				"intent-2": {ID: "intent-2", Disclosable: true},
			}
			allowedDoc, err := Render(allowed)
			if err != nil {
				t.Fatal(err)
			}
			if silentDoc != allowedDoc {
				t.Fatalf("%s %s all-disclosable render differs from the silent server", locale, page)
			}
		}
	}
	view := web069View(PageWork, "en-US")
	if !reflect.DeepEqual(admittedWork(view), admittedWork(view)) {
		t.Fatal("work limiting is nondeterministic")
	}
}

// Security: denied journey artifacts never reach the markup in any
// catalog locale; admitted instances are never over-withheld.
func TestTodo_WEB_069_Security(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, page := range []PageID{PageWork, PageHistory} {
			doc, err := Render(web069View(page, locale))
			if err != nil {
				t.Fatal(err)
			}
			body := web063BodyText(t, doc)
			if strings.Contains(body, "DES2 G6 → DES3 G7") {
				t.Fatalf("%s %s keeps the denied artifact", locale, page)
			}
		}
		workDoc, err := Render(web069View(PageWork, locale))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(web063BodyText(t, workDoc), "ENG2 G6 → ENG3 G7") {
			t.Fatalf("%s work queue over-withholds the admitted instance", locale)
		}
	}
}

func web069View(page PageID, locale string) View {
	view := testView(page)
	view.Locale = ResolveProductLocale(locale)
	view = ApplyLocale(view, view.Locale)
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"intent-1": {ID: "intent-1", Disclosable: true},
		"intent-2": {ID: "intent-2", Disclosable: false, DenialReason: "No record access."},
	}
	return view
}
