package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-070: filter real-time events by current authority. The
// attention notification center — the shell's real-time event surface
// — counts every open work item regardless of record verdicts: a
// denied journey keeps its share of the attention count even though it
// appears in no listing. The count must follow the current authority:
// only admitted open instances count, while a silent server keeps the
// current count and page-visibility keeps its honest restricted state.
func TestTodo_WEB_070(t *testing.T) {
	for _, count := range []struct {
		name     string
		verdicts map[string]AuthorizedRecord
		want     int64
	}{
		{"silent server keeps the count", nil, 1},
		{"denied open instance leaves the count", map[string]AuthorizedRecord{
			"intent-1": {ID: "intent-1", Disclosable: false, DenialReason: "No record access."},
			"intent-2": {ID: "intent-2", Disclosable: true},
		}, 0},
		{"denied terminal instance keeps the count", map[string]AuthorizedRecord{
			"intent-1": {ID: "intent-1", Disclosable: true},
			"intent-2": {ID: "intent-2", Disclosable: false, DenialReason: "No record access."},
		}, 1},
		{"person-only verdicts keep the count", map[string]AuthorizedRecord{
			"worker-avery": {ID: "worker-avery", Disclosable: true},
		}, 1},
	} {
		view := testView(PageWork)
		view.RecordVerdicts = count.verdicts
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		center := findNotificationCenter(root)
		if center == nil {
			t.Fatalf("%s renders no notification center", count.name)
		}
		// UXBLIND-056: the bell announces unread notifications; the
		// authority-filtered open count now lives in the work stream itself.
		if got := int64(len(OpenWorkItems(admittedWork(view)))); got != count.want {
			t.Fatalf("%s admitted open work = %d, want %d", count.name, got, count.want)
		}
		want := view.Locale.Text("shell.notifications") + ", " + view.Locale.Plural("notifications.unread_count", 0)
		if label := attr(findSummary(center), "aria-label"); label != want {
			t.Fatalf("%s announces %q, want %q", count.name, label, want)
		}
	}
}

// Golden: the attention-count matrix digest (variant → count).
func TestTodo_WEB_070_Golden(t *testing.T) {
	variants := map[string]map[string]AuthorizedRecord{
		"silent":        nil,
		"open-hidden":   {"intent-1": {ID: "intent-1", Disclosable: false}, "intent-2": {ID: "intent-2", Disclosable: true}},
		"all-admitted":  {"intent-1": {ID: "intent-1", Disclosable: true}, "intent-2": {ID: "intent-2", Disclosable: true}},
		"person-only":   {"worker-avery": {ID: "worker-avery", Disclosable: true}},
		"terminal-only": {"intent-2": {ID: "intent-2", Disclosable: true}},
	}
	var builder strings.Builder
	for _, name := range []string{"silent", "open-hidden", "all-admitted", "person-only", "terminal-only"} {
		view := testView(PageWork)
		view.RecordVerdicts = variants[name]
		builder.WriteString(name)
		builder.WriteString("\x00")
		builder.WriteString(openAdmittedCount(view))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "9fb63cfe662a17b24fe999edaacbfa5e49419fa7726fd0cecf37a6118bf6faf8"
	if got != want {
		t.Fatalf("attention matrix digest = %s, want %s", got, want)
	}
}

// openAdmittedCount is the authority-filtered attention count: admitted
// open instances only.
func openAdmittedCount(view View) string {
	return strconv.Itoa(len(OpenWorkItems(admittedWork(view))))
}

// Browser: the governed shell parses with its honest center and no
// positive tabindex stops.
func TestTodo_WEB_070_Browser(t *testing.T) {
	view := testView(PageWork)
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"intent-1": {ID: "intent-1", Disclosable: false, DenialReason: "No record access."},
		"intent-2": {ID: "intent-2", Disclosable: true},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if findNotificationCenter(root) == nil {
		t.Fatal("governed shell loses the notification center")
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
		t.Fatalf("governed shell carries %d positive tabindex stops", positive)
	}
}

// Conformance: no unresolved copy in three locales, governed
// all-admitted renders identically to a silent server, determinism.
func TestTodo_WEB_070_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageWork)
		view.Locale = ResolveProductLocale(locale)
		view = ApplyLocale(view, view.Locale)
		view.RecordVerdicts = map[string]AuthorizedRecord{
			"intent-1": {ID: "intent-1", Disclosable: false, DenialReason: "No record access."},
			"intent-2": {ID: "intent-2", Disclosable: true},
		}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("%s governed shell leaks an unresolved key", locale)
		}
		silent := testView(PageWork)
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
			t.Fatalf("%s all-admitted shell differs from the silent server", locale)
		}
	}
	first, second := renderNotificationSubtree(t, testView(PageWork)), renderNotificationSubtree(t, testView(PageWork))
	if first != second {
		t.Fatal("notification center is not deterministic for one view")
	}
}

// Security: the denied instance keeps no share of the attention count
// in any catalog locale; the center itself never disappears.
func TestTodo_WEB_070_Security(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageWork)
		view.Locale = ResolveProductLocale(locale)
		view = ApplyLocale(view, view.Locale)
		view.RecordVerdicts = map[string]AuthorizedRecord{
			"intent-1": {ID: "intent-1", Disclosable: false, DenialReason: "No record access."},
			"intent-2": {ID: "intent-2", Disclosable: true},
		}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		center := findNotificationCenter(root)
		if center == nil {
			t.Fatalf("%s governed shell loses the notification center", locale)
		}
		lc := ResolveProductLocale(locale)
		// UXBLIND-056: the bell announces unread notifications; the denied
		// instance keeps no share of the admitted open work.
		if got := len(OpenWorkItems(admittedWork(view))); got != 0 {
			t.Fatalf("%s denied instance keeps admitted open work: %d", locale, got)
		}
		want := lc.Text("shell.notifications") + ", " + lc.Plural("notifications.unread_count", 0)
		if label := attr(findSummary(center), "aria-label"); label != want {
			t.Fatalf("%s denied instance keeps attention share: %q, want %q", locale, label, want)
		}
	}
}
