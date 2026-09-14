package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-063: record-level page authorization. The person and
// self-service routes must enforce the WEB-061 verdicts: an undisclosable
// record — or a record without a verdict once verdicts exist — renders
// the no-data unavailable recovery instead of the profile, and a
// disclosable record renders every fact through its field verdict, so a
// denied salary never reaches the page. A silent server keeps the current
// pages byte-for-byte: full-allow verdicts render identically to no
// verdicts at all.
func TestTodo_WEB_063(t *testing.T) {
	avery := web063Person("worker-avery")
	salary := money(ResolveProductLocale("en-US"), avery.BasePay)
	bonus := percentage(ResolveProductLocale("en-US"), avery.BonusTarget)

	governed := testView(PagePerson)
	governed.RecordVerdicts = map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name":         {Effect: PresentationAllow},
			"base_pay":     {Effect: PresentationDenied, Reason: "Not for this purpose."},
			"bonus_target": {Effect: PresentationDenied, Reason: "Not for this purpose."},
			"manager":      {Effect: PresentationRedact},
			"legal_name":   {Effect: PresentationWithheld},
		}},
		"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
	}
	doc, err := Render(governed)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"Unavailable", "Redacted", "Withheld"} {
		if !strings.Contains(doc, marker) {
			t.Fatalf("governed profile shows no %q marker", marker)
		}
	}
	for _, leaked := range []string{salary, bonus} {
		if leaked != "" && strings.Contains(web063BodyText(t, doc), leaked) {
			t.Fatalf("governed profile leaks %q", leaked)
		}
	}
	if !strings.Contains(doc, "Avery Patel") {
		t.Fatal("governed profile loses the allowed name")
	}

	// An undisclosable record renders the no-data recovery: no hero, no
	// facts, no names.
	hidden := testView(PagePerson)
	hidden.SelectedPerson = "worker-jordan"
	hidden.RecordVerdicts = governed.RecordVerdicts
	hiddenDoc, err := Render(hidden)
	if err != nil {
		t.Fatal(err)
	}
	hiddenRoot, err := xhtml.Parse(strings.NewReader(hiddenDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findClassNode(hiddenRoot, "person-hero") != nil {
		t.Fatal("undisclosable record renders a profile hero")
	}
	if findClassNode(hiddenRoot, "empty-state") == nil {
		t.Fatal("undisclosable record renders no unavailable recovery")
	}
	if strings.Contains(textContent(hiddenRoot), "Jordan Lee") {
		t.Fatal("undisclosable record leaks its name")
	}

	// The self-service route obeys the same verdicts for the viewer.
	self := testView(PageMyself)
	self.Viewer.PersonID = "worker-avery"
	self.RecordVerdicts = governed.RecordVerdicts
	selfDoc, err := Render(self)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(selfDoc, "Avery Patel") {
		t.Fatal("governed self-service loses the viewer")
	}
	closed := testView(PageMyself)
	closed.Viewer.PersonID = "worker-avery"
	closed.RecordVerdicts = map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: false, DenialReason: "No record access."},
	}
	closedDoc, err := Render(closed)
	if err != nil {
		t.Fatal(err)
	}
	closedRoot, err := xhtml.Parse(strings.NewReader(closedDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findClassNode(closedRoot, "person-hero") != nil {
		t.Fatal("undisclosable viewer renders a profile hero")
	}
	if strings.Contains(textContent(closedRoot), "Avery Patel") {
		t.Fatal("undisclosable viewer leaks its name")
	}
}

// web063BodyText returns the rendered body text without style and script
// subtrees, so leak assertions cannot false-positive on the inlined
// stylesheet (which contains strings like "12%" in color-mix rules).
func web063BodyText(t *testing.T, doc string) string {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	var builder strings.Builder
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && (node.Data == "style" || node.Data == "script") {
			return
		}
		if node.Type == xhtml.TextNode {
			builder.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return builder.String()
}

func web063Person(id string) Person {
	for _, person := range testView(PageHome).People {
		if person.ID == id {
			return person
		}
	}
	return Person{}
}

// Golden: the governed person profile for a fixed projection.
func TestTodo_WEB_063_Golden(t *testing.T) {
	view := testView(PagePerson)
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name":         {Effect: PresentationAllow},
			"base_pay":     {Effect: PresentationDenied, Reason: "Not for this purpose."},
			"bonus_target": {Effect: PresentationDenied, Reason: "Not for this purpose."},
			"manager":      {Effect: PresentationRedact},
			"legal_name":   {Effect: PresentationWithheld},
		}},
	}
	person, ok := exactPerson(view)
	if !ok {
		t.Fatal("fixture resolves no person")
	}
	profile := personProfileProps(view, person, PagePerson)
	node, err := ui.RenderToString(ui.CreateElement(PersonPage, PersonPageProps{I18nProps: I18nProps{Locale: view.Locale}, Profile: &profile}))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))

	// PROMOUX-012 re-pin: the profile gained its Active workflows section
	// (empty here, Avery's only journey is terminal). Rendering the same
	// profile with that section omitted reproduces the previous pin
	// cc4de793..., so nothing else in the governed profile changed.
	if got := hex.EncodeToString(digest[:]); got != "9528cf9f75fbdc65c12bc01bfbcdd67b1a8321397cf4690e47914774053b787c" {
		t.Fatalf("governed profile golden mismatch: %s\n%s", got, node)
	}
}

// Browser: the governed profile parses with denial markers and no raw
// compensation values in any catalog locale.
func TestTodo_WEB_063_Browser(t *testing.T) {
	avery := web063Person("worker-avery")
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		lc := ResolveProductLocale(locale)
		salary := money(lc, avery.BasePay)
		view := testView(PagePerson)
		view.Locale = lc
		view = ApplyLocale(view, view.Locale)
		view.RecordVerdicts = map[string]AuthorizedRecord{
			"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
				"name":     {Effect: PresentationAllow},
				"base_pay": {Effect: PresentationDenied, Reason: "Not for this purpose."},
			}},
		}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if salary != "" && strings.Contains(web063BodyText(t, doc), salary) {
			t.Fatalf("%s governed profile leaks the salary %q", locale, salary)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if findClassNode(root, "person-hero") == nil {
			t.Fatalf("%s governed profile loses the hero", locale)
		}
	}
}

// Conformance: full-allow verdicts render identically to a silent server,
// in every locale.
func TestTodo_WEB_063_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		silent := testView(PagePerson)
		silent.Locale = ResolveProductLocale(locale)
		silent = ApplyLocale(silent, silent.Locale)
		silentDoc, err := Render(silent)
		if err != nil {
			t.Fatal(err)
		}
		allowed := testView(PagePerson)
		allowed.Locale = ResolveProductLocale(locale)
		allowed = ApplyLocale(allowed, allowed.Locale)
		allowAll := map[string]AuthorizedField{}
		for _, field := range PersonRecordFields {
			allowAll[field] = AuthorizedField{Effect: PresentationAllow}
		}
		allowed.RecordVerdicts = map[string]AuthorizedRecord{
			"worker-avery":  {ID: "worker-avery", Disclosable: true, Fields: allowAll},
			"worker-jordan": {ID: "worker-jordan", Disclosable: true, Fields: allowAll},
			"worker-elena":  {ID: "worker-elena", Disclosable: true, Fields: allowAll},
		}
		allowedDoc, err := Render(allowed)
		if err != nil {
			t.Fatal(err)
		}
		if silentDoc != allowedDoc {
			t.Fatalf("%s full-allow render differs from the silent server", locale)
		}
		if strings.Contains(allowedDoc, "⟦") {
			t.Fatalf("%s governed render leaks an unresolved key", locale)
		}
	}
}
