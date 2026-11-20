package chatui

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"reflect"
	"strings"
	"testing"
	"time"
)

func issue3792Model(locale string) (Model, *[]string) {
	m := composerToolsModel(locale, false)
	var opened []string
	m.Decisions = DecisionsState{Enabled: true,
		Record:   func(text, parent string) { opened = append(opened, text+"|"+parent) },
		OpenList: func() { opened = append(opened, "list") }, Count: 2}
	return m, &opened
}

func issue3792Names(m Model) []string {
	state := composerCommandMenuNext(m, composerCommandMenu{}, "chat-composer", "/", 1)
	return composerCommandShown(m, state, "chat-composer")
}

func TestIssue3792_DecideIsInTheCommandListOnlyWhereItWorks(t *testing.T) {
	m, _ := issue3792Model("en-US")
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	if got := issue3792Names(m); !reflect.DeepEqual(got, []string{"poll", "todo", "decide", "giphy", "location", "ask"}) {
		t.Fatalf("/ lists %v", got)
	}
	state := composerCommandMenuNext(m, composerCommandMenu{}, "chat-composer", "/", 1)
	markup := renderNode(t, composerCommandMenuView(m, state, "chat-composer", 1))
	for _, want := range []string{">/decide<", "Record a decision the conversation reached"} {
		if !strings.Contains(markup, want) {
			t.Errorf("the list lacks %q: %s", want, markup)
		}
	}
	de, _ := issue3792Model("de-DE")
	state = composerCommandMenuNext(de, composerCommandMenu{}, "chat-composer", "/", 1)
	if markup = renderNode(t, composerCommandMenuView(de, state, "chat-composer", 1)); !strings.Contains(markup, "Eine in der Unterhaltung getroffene Entscheidung festhalten") {
		t.Errorf("no German description: %s", markup)
	}
	ar, _ := issue3792Model("ar")
	state = composerCommandMenuNext(ar, composerCommandMenu{}, "chat-composer", "/", 1)
	if markup = renderNode(t, composerCommandMenuView(ar, state, "chat-composer", 1)); !strings.Contains(markup, "تسجيل قرار توصلت إليه المحادثة") {
		t.Errorf("no Arabic description: %s", markup)
	}
	// Not offered when the server cannot record, with an agent, or in a room another company hosts.
	off := m
	off.Decisions.Enabled = false
	for _, name := range issue3792Names(off) {
		if name == "decide" {
			t.Fatal("decide offered while the server cannot record decisions")
		}
	}
	agent := chat4Fixture("en-US", "sent", true)
	agent.Decisions = m.Decisions
	for i := range agent.Conversations {
		agent.Conversations[i].Agent = true
	}
	if decisionsAvailable(agent) {
		t.Fatal("decide offered in a conversation with an agent")
	}
}

func TestIssue3792_DecideOpensTheDialogWithTheWordsAndConsumesTheLine(t *testing.T) {
	m, opened := issue3792Model("en-US")
	cleared := 0
	rt := composerCommandRuntime{Model: m, ClearDraft: func() { cleared++ }, Notice: func(string) {}}
	outcome, _ := defaultComposerCommands().dispatch(rt, "/decide   Freeze promotion changes until after calibration ")
	if outcome != composerCommandRan || cleared != 1 || !reflect.DeepEqual(*opened, []string{"Freeze promotion changes until after calibration|"}) {
		t.Fatalf("outcome=%v cleared=%d opened=%v", outcome, cleared, *opened)
	}
	// An empty /decide opens an empty form; nothing is recorded by opening.
	*opened = nil
	if outcome, _ = defaultComposerCommands().dispatch(rt, "/decide"); outcome != composerCommandRan || !reflect.DeepEqual(*opened, []string{"|"}) {
		t.Fatalf("empty: %v %v", outcome, *opened)
	}
	// In a thread's reply box the decision belongs to that thread.
	*opened = nil
	thread := m
	thread.ThreadParent = &Message{ID: "root-1"}
	rt = composerCommandRuntime{Model: thread, Target: "thread-composer", ClearDraft: func() {}, Notice: func(string) {}}
	composerCommandsFor("thread-composer").dispatch(rt, "/decide Ship Friday")
	if !reflect.DeepEqual(*opened, []string{"Ship Friday|root-1"}) {
		t.Fatalf("thread: %v", *opened)
	}
	// A server that does not record decisions never sends a line into the room.
	off := m
	off.Decisions.Enabled = false
	rt = composerCommandRuntime{Model: off, ClearDraft: func() {}, Notice: func(string) {}}
	if outcome, _ = defaultComposerCommands().dispatch(rt, "/decide x"); outcome != composerCommandUnknown {
		t.Fatalf("unavailable decide: %v", outcome)
	}
}

func issue3792View(locale string) DecisionsDialogView {
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	return DecisionsDialogView{Locale: locale, Channel: "People ops", State: "ready", CanRecord: true, Items: []DecisionItem{
		{ID: "d2", Text: "Freeze promotion changes until June", Rationale: "Calibration needs one baseline", Owner: "Dana", DecidedAt: at, SourcePostID: "p2", SourceSequence: 12, SupersedesID: "d1", CanReplace: true},
		{ID: "d3", Text: "Hire two analysts", Owner: "Sam", DecidedAt: at, SourcePostID: "p3", SourceSequence: 14},
		{ID: "d1", Text: "Freeze promotion changes", Owner: "Dana", DecidedAt: at.Add(-48 * time.Hour), SourcePostID: "p1", SourceSequence: 5, SupersededByID: "d2", CanReplace: true},
	}}
}

func TestIssue3792_DialogShowsOwnerDateReasonAndKeepsReplacedOnesApart(t *testing.T) {
	markup := renderNode(t, RenderDecisionsDialog(issue3792View("en-US")))
	for _, want := range []string{
		`role="dialog"`, `aria-modal="true"`, `aria-labelledby="decisions-title"`, "Decisions · People ops",
		"informal and does not change company policy", "Informal", "Freeze promotion changes until June", "Why: ", "Calibration needs one baseline",
		"Recorded by Dana on ", "Replaces an earlier decision", "Replaced decisions", "Replaced by a newer decision",
		`data-decisions-goto="p2"`, `data-decisions-seq="12"`, `data-decisions-replace="d2"`, `id="decisions-field-text"`, "Record decision",
		`aria-live="polite"`, `maxLength="500"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("the dialog lacks %q: %s", want, markup)
		}
	}
	// Only a decision in force offers Replace.
	if strings.Contains(markup, `data-decisions-replace="d1"`) {
		t.Error("a replaced decision offers Replace")
	}
	if strings.Contains(markup, `data-decisions-replace="d3"`) {
		t.Error("Replace offered to someone who may not replace it")
	}
	// The replaced decision sits inside the folded group, after those in force.
	if strings.Index(markup, "Replaced decisions") > strings.Index(markup, `data-decision="d1"`) || strings.Index(markup, `data-decision="d3"`) > strings.Index(markup, "Replaced decisions") {
		t.Errorf("replaced decision is not folded away: %s", markup)
	}
}

func TestIssue3792_DialogStatesAndReplacing(t *testing.T) {
	loading := renderNode(t, RenderDecisionsDialog(DecisionsDialogView{Locale: "en-US", State: "loading"}))
	if !strings.Contains(loading, "Loading decisions") || strings.Contains(loading, "<textarea") {
		t.Errorf("loading: %s", loading)
	}
	failed := renderNode(t, RenderDecisionsDialog(DecisionsDialogView{Locale: "en-US", State: "error", Error: "unavailable"}))
	if !strings.Contains(failed, `role="alert"`) || !strings.Contains(failed, "could not be saved right now") || !strings.Contains(failed, "data-decisions-close") {
		t.Errorf("error: %s", failed)
	}
	empty := renderNode(t, RenderDecisionsDialog(DecisionsDialogView{Locale: "en-US", State: "ready", CanRecord: true}))
	if !strings.Contains(empty, "No decisions recorded here yet") || !strings.Contains(empty, "<textarea") {
		t.Errorf("empty: %s", empty)
	}
	readOnly := renderNode(t, RenderDecisionsDialog(DecisionsDialogView{Locale: "en-US", State: "ready", Items: issue3792View("en-US").Items}))
	if strings.Contains(readOnly, "<textarea") || strings.Contains(readOnly, "data-decisions-replace") || !strings.Contains(readOnly, "not record new ones") {
		t.Errorf("a reader who may not record sees the form or Replace: %s", readOnly)
	}
	replacing := issue3792View("en-US")
	replacing.ReplacingID, replacing.Text, replacing.Error = "d2", "Freeze until July", "invalid_argument"
	markup := renderNode(t, RenderDecisionsDialog(replacing))
	for _, want := range []string{"This will replace", "Freeze promotion changes until June", "Record and replace", "data-decisions-keep", `aria-invalid="true"`, `id="decisions-error"`, "Freeze until July"} {
		if !strings.Contains(markup, want) {
			t.Errorf("replacing view lacks %q: %s", want, markup)
		}
	}
	busy := issue3792View("en-US")
	busy.Busy = true
	if markup = renderNode(t, RenderDecisionsDialog(busy)); !strings.Contains(markup, "Recording the decision") || !strings.Contains(markup, `aria-busy="true"`) {
		t.Errorf("busy: %s", markup)
	}
}

func TestIssue3792_CopyInThreeLanguagesAndRTL(t *testing.T) {
	for key := range decisionsCopy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			if strings.TrimSpace(decisionsCopy[locale][key]) == "" {
				t.Errorf("%s has no %q", locale, key)
			}
		}
	}
	for locale, table := range decisionsCopy {
		if len(table) != len(decisionsCopy["en-US"]) {
			t.Errorf("%s has %d entries, English has %d", locale, len(table), len(decisionsCopy["en-US"]))
		}
	}
	if decisionsCopy["de-DE"][dcInformal] == decisionsCopy["en-US"][dcInformal] || decisionsCopy["ar"][dcInformal] == decisionsCopy["en-US"][dcInformal] {
		t.Error("the informal notice is untranslated")
	}
	ar := renderNode(t, RenderDecisionsDialog(issue3792View("ar")))
	if !strings.Contains(ar, `dir="rtl"`) || !strings.Contains(ar, "القرارات") || !strings.Contains(ar, "غير رسمي") {
		t.Errorf("Arabic dialog: %s", ar)
	}
	de := renderNode(t, RenderDecisionsDialog(issue3792View("de-DE")))
	if !strings.Contains(de, "Entscheidung festhalten") || !strings.Contains(de, "Festgehalten von Dana am ") || !strings.Contains(de, "Ersetzte Entscheidungen") {
		t.Errorf("German dialog: %s", de)
	}
	for _, code := range []string{"invalid_argument", "not_found", "permission_denied", "conflict", "text", "unavailable"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if DecisionsErrorText(locale, code) == "" {
				t.Errorf("no %s text for %s", locale, code)
			}
		}
	}
	if !strings.Contains(ChatDecisionsStyles, "max-width:480px") || strings.Contains(ChatDecisionsStyles, "margin-left") || strings.Contains(ChatDecisionsStyles, "padding-right") {
		t.Error("the decision styles are not phone and RTL safe")
	}
}

func TestIssue3792_DetailsRowOpensTheListOnlyWhereItWorks(t *testing.T) {
	m, _ := issue3792Model("en-US")
	markup := renderNode(t, decisionsDetailsSection(m))
	for _, want := range []string{`id="chat-details-decisions"`, `data-decisions-open="list"`, "Decisions · 2", "What this conversation settled"} {
		if !strings.Contains(markup, want) {
			t.Errorf("details row lacks %q: %s", want, markup)
		}
	}
	m.Decisions.Enabled = false
	if decisionsDetailsSection(m) != nil {
		t.Error("a Decisions row with nothing behind it")
	}
}
