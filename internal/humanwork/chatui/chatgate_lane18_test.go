package chatui

import (
	"encoding/json"
	stdhtml "html"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

var chatgateLocales = []string{"en-US", "de-DE", "ar"}

func chatgateBrowseModel(locale string) Model {
	return Model{State: StateReady, Locale: locale, Direction: direction(locale), CurrentUser: "walt", CurrentTenantID: "t", ShowBrowse: true,
		ChatFeatures: &ChatFeatures{Gates: true},
		Browse: []Conversation{
			{ID: "gated", Name: "payroll-help", Kind: PublicChannel, MemberCount: 12},
			{ID: "one", Name: "single-question", Kind: PublicChannel},
			{ID: "open", Name: "random", Kind: PublicChannel},
			{ID: "mine", Name: "already-in", Kind: PublicChannel, Joined: true},
		},
		GateJoins: map[string]GateJoin{
			"gated": {Questions: 3, Purpose: "So the right person answers", ChannelPurpose: "Payroll questions", Mode: "review"},
			"one":   {Questions: 1, Mode: "automatic"},
			"mine":  {Questions: 2, Mode: "rule"},
		},
		Callbacks: Callbacks{JoinConversation: func(string) {}, SelectConversation: func(string) {}},
	}
}

// TestTodo_CHATGATE_005_Browser_Browse: a gated channel in Browse says how many
// questions joining takes, what the channel is for and what happens to a
// request; an ungated one still says Join.
func TestTodo_CHATGATE_005_Browser_Browse(t *testing.T) {
	if got := chatgateJoinLabel(chatgateBrowseModel("en-US"), Conversation{ID: "gated"}); got != "Answer 3 questions to join" {
		t.Fatalf("English label is %q", got)
	}
	if got := GateText("en-US", "browseReview"); got != "An administrator reads your answers before you are let in." {
		t.Fatalf("English note is %q", got)
	}
	for _, locale := range chatgateLocales {
		m := chatgateBrowseModel(locale)
		gate := chatgateLocale(locale)
		for id, want := range map[string]string{"gated": gateJoinTitle(gate, 3), "one": GateText(gate, "joinOne"), "open": m.t(KeyJoin)} {
			if got := chatgateJoinLabel(m, Conversation{ID: id}); got != want || got == "" {
				t.Errorf("%s: the join button of %s reads %q, want %q", locale, id, got, want)
			}
		}
		note := chatremoveMarkup(t, chatgateBrowseNote(m, Conversation{ID: "gated"}))
		note = stdhtml.UnescapeString(note)
		for _, want := range []string{"Payroll questions", "So the right person answers", GateText(gate, "browseReview")} {
			if !strings.Contains(note, want) {
				t.Errorf("%s: the note misses %q: %s", locale, want, note)
			}
		}
		if got := chatremoveMarkup(t, chatgateBrowseNote(m, Conversation{ID: "one"})); !strings.Contains(stdhtml.UnescapeString(got), GateText(gate, "browseAutomatic")) {
			t.Errorf("%s: an automatic gate does not say answers admit at once: %s", locale, got)
		}
		// Nothing for a channel with no gate, and nothing for one already joined.
		if chatgateBrowseNote(m, Conversation{ID: "open"}) != nil || chatgateBrowseNote(m, Conversation{ID: "mine", Joined: true}) != nil {
			t.Errorf("%s: a note is drawn where no gate stands between the person and the channel", locale)
		}
		// The list was not read, or gates are off: a plain Join, no note.
		unread := m
		unread.GateJoins = nil
		off := m
		off.ChatFeatures = &ChatFeatures{}
		for name, other := range map[string]Model{"unread": unread, "off": off} {
			if got := chatgateJoinLabel(other, Conversation{ID: "gated"}); got != other.t(KeyJoin) || chatgateBrowseNote(other, Conversation{ID: "gated"}) != nil {
				t.Errorf("%s %s: the row speaks of a gate the page does not know of: %q", locale, name, got)
			}
		}
	}
	if !strings.Contains(Stylesheet, ".chat-workspace .chatgate-browse-note{") {
		t.Fatal("the note has no styles in the page's stylesheet")
	}
}

// TestTodo_CHATGATE_005_Browser_Banner: a member who must answer the channel's
// questions again sees one line above the conversation with the date and a
// button that opens the form; nobody else sees it.
func TestTodo_CHATGATE_005_Browser_Banner(t *testing.T) {
	by := time.Date(time.Now().Year(), 11, 3, 0, 0, 0, 0, time.UTC)
	for _, locale := range chatgateLocales {
		m := Model{State: StateReady, Locale: locale, Direction: direction(locale), SelectedID: "room", CurrentUser: "walt", ChatFeatures: &ChatFeatures{Gates: true},
			Conversations: []Conversation{{ID: "room", Name: "payroll-help", Kind: PublicChannel, Joined: true}},
			GateJoins:     map[string]GateJoin{"room": {Questions: 2, AnswerAgain: true, AnswerBy: by}, "other": {Questions: 1}}}
		banner := chatgateBanner(m)
		if banner == nil {
			t.Fatalf("%s: no banner for a member who must answer again", locale)
		}
		markup := stdhtml.UnescapeString(chatremoveMarkup(t, banner))
		gate := chatgateLocale(locale)
		day := formatDay(locale, by, false)
		if !strings.Contains(markup, day) || !strings.Contains(markup, GateText(gate, "answerNow")) || !strings.Contains(markup, `role="status"`) {
			t.Fatalf("%s: the banner does not give the date %q and a way to answer: %s", locale, day, markup)
		}
		buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" })
		if len(buttons) != 1 || chatPolishAttr(buttons[0], "data-gate-open") != "room" || chatPolishAttr(buttons[0], "type") != "button" {
			t.Fatalf("%s: the banner's button does not open this channel's form: %s", locale, markup)
		}
		if strings.Contains(markup, "%s") || strings.Contains(markup, "0001") {
			t.Fatalf("%s: the banner prints a placeholder or an empty date: %s", locale, markup)
		}
		// On the page it is above the messages, once.
		page := stdhtml.UnescapeString(chatremoveMarkup(t, Build(m)))
		if strings.Count(page, `class="chatgate-banner"`) != 1 || strings.Index(page, `class="chatgate-banner"`) > strings.Index(page, `class="chat-composer"`) {
			t.Fatalf("%s: the banner is not drawn once above the composer", locale)
		}
		// No date known: the sentence without one.
		undated := m
		undated.GateJoins = map[string]GateJoin{"room": {Questions: 2, AnswerAgain: true}}
		if got := stdhtml.UnescapeString(chatremoveMarkup(t, chatgateBanner(undated))); !strings.Contains(got, GateText(gate, "changedSoon")) {
			t.Fatalf("%s: the undated banner: %s", locale, got)
		}
		// Not for a member whose answers are current, another conversation, or
		// with gates off.
		current := m
		current.GateJoins = map[string]GateJoin{"room": {Questions: 2}}
		elsewhere := m
		elsewhere.SelectedID = "other"
		off := m
		off.ChatFeatures = &ChatFeatures{}
		for name, other := range map[string]Model{"current": current, "elsewhere": elsewhere, "off": off} {
			if chatgateBanner(other) != nil {
				t.Errorf("%s: a banner is drawn for %s", locale, name)
			}
		}
	}
	if !strings.Contains(Stylesheet, ".chat-workspace .chatgate-banner{") {
		t.Fatal("the banner has no styles in the page's stylesheet")
	}
}

// TestTodo_CHATGATE_005_Validation: the form's own check finds what the service
// would refuse, question by question, before anything is sent.
func TestTodo_CHATGATE_005_Validation(t *testing.T) {
	field := func(id, kind string, required bool, options ...string) chatgate.Field {
		return chatgate.Field{ID: id, Kind: kind, KindVersion: "1.0.0", Label: id, Purpose: "p", DataClass: "INTERNAL", Required: required, Options: options, RetentionDays: 30, Visibility: chatgate.Visibility{Administrators: true}}
	}
	d := chatgate.Definition{Mode: "review", Purpose: "p", Fields: []chatgate.Field{
		field("why", "short_text", true), field("note", "long_text", false), field("team", "single_choice", true, "Payroll", "Finance"),
		field("skills", "multiple_choice", true, "go", "sql"), field("start", "date", false), field("remote", "boolean", true),
		field("ack", "acknowledgement", true), field("future", "not_built_yet", true),
	}}
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	good := map[string]json.RawMessage{"why": raw("a reason"), "team": raw("Payroll"), "skills": raw([]string{"go"}), "start": raw("2026-10-02"), "remote": raw(false), "ack": raw(true), "future": raw("x")}
	if problems := GateAnswerProblems(d, good); len(problems) != 0 {
		t.Fatalf("good answers are refused: %v", problems)
	}
	// What the check passes, the service's own validation passes too.
	registry := chatgate.NewRegistry()
	for _, f := range d.Fields[:7] {
		kind, _ := registry.Kind(f.Kind, f.KindVersion)
		if value, ok := good[f.ID]; ok && kind.Validate(f, value) != nil {
			t.Fatalf("the service refuses %s=%s that the form accepted", f.ID, value)
		}
	}
	bad := map[string]json.RawMessage{"why": raw("   "), "note": raw(strings.Repeat("x", 501)), "team": raw("Legal"), "skills": raw([]string{}), "start": raw("next week"), "remote": raw(false), "ack": raw(false)}
	problems := GateAnswerProblems(d, bad)
	want := map[string]string{"why": "requiredError", "note": "invalid", "team": "invalid", "skills": "requiredError", "start": "invalid", "ack": "requiredError", "future": "requiredError"}
	for id, key := range want {
		if problems[id] != key {
			t.Errorf("%s: %q, want %q", id, problems[id], key)
		}
	}
	// A yes-or-no question left at "no" is an answer, and an optional question
	// left empty is not a problem.
	if _, flagged := problems["remote"]; flagged || len(problems) != len(want) {
		t.Fatalf("problems=%v", problems)
	}
	for _, locale := range chatgateLocales {
		for _, key := range []string{"requiredError", "invalid", "fixAnswers"} {
			if GateText(locale, key) == "" {
				t.Errorf("%s: %q has no words", locale, key)
			}
		}
	}
	if GateText("en-US", "requiredError") != "Answer this question to continue." {
		t.Fatalf("English sentence is %q", GateText("en-US", "requiredError"))
	}
	// The form draws each sentence under its question and marks the question.
	v := GateView{Locale: "en-US", Conversation: "room", Gate: chatgate.Gate{Versions: []chatgate.Definition{{Version: chatgate.Version{Major: 1}, Mode: "review", Purpose: "p", Fields: d.Fields[:1]}}, Current: "1.0.0", State: "active"},
		FieldErrors: map[string]string{"why": GateText("en-US", "requiredError")}}
	markup := stdhtml.UnescapeString(gateMarkup(t, v))
	inputs := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-gate-field") == "why" })
	lines := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "gate-field-why-error" })
	if len(inputs) != 1 || chatPolishAttr(inputs[0], "aria-invalid") != "true" || !strings.Contains(chatPolishAttr(inputs[0], "aria-describedby"), "gate-field-why-error") || len(lines) != 1 || chatPolishAttr(lines[0], "role") != "alert" || !strings.Contains(markup, "Answer this question to continue.") {
		t.Fatalf("the question is not marked with its sentence: %s", markup)
	}
}

// TestTodo_CHATGATE_006_Browser_Rule: the rule editor offers questions and the
// directory's facts, lists the known answers as tick boxes, lets everyone else
// be declined or reviewed, and says the rule back in a sentence.
func TestTodo_CHATGATE_006_Browser_Rule(t *testing.T) {
	d := chatgate.Definition{Version: chatgate.Version{Major: 1}, Mode: "rule", Purpose: "Discuss payroll", Fields: []chatgate.Field{
		{ID: "team", Kind: "single_choice", KindVersion: "1.0.0", Label: "Which team are you in?", Purpose: "Routing", DataClass: "INTERNAL", Required: true, RetentionDays: 30, Options: []string{"Payroll", "Finance", "Legal"}, Visibility: chatgate.Visibility{Administrators: true}},
		{ID: "why", Kind: "short_text", KindVersion: "1.0.0", Label: "Why?", Purpose: "Context", DataClass: "INTERNAL", RetentionDays: 30, Visibility: chatgate.Visibility{Administrators: true}},
		{ID: "remote", Kind: "boolean", KindVersion: "1.0.0", Label: "Do you work remotely?", Purpose: "Context", DataClass: "INTERNAL", RetentionDays: 30, Visibility: chatgate.Visibility{Administrators: true}},
	}}
	v := GateView{Locale: "en-US", Directory: map[string][]GateChoice{"team": {{ID: "People", Label: "People"}, {ID: "Payroll", Label: "Payroll"}}}}

	// Applying an edit writes the "in" list and, when asked, its negation.
	declined, err := GateRuleApply(d, GateRuleEdit{Subject: "team", Values: []string{"Payroll", " Finance ", "Payroll", ""}, Reason: " Your team runs payroll ", Else: "declined"})
	if err != nil || len(declined.Rules) != 2 {
		t.Fatalf("rules=%+v err=%v", declined.Rules, err)
	}
	first, second := declined.Rules[0], declined.Rules[1]
	if first.When.Operator != "in" || first.When.Field != "team" || first.When.Fact != "" || strings.Join(first.When.Values, ",") != "Payroll,Finance" || first.Outcome != "admitted" || first.Reason != "Your team runs payroll" {
		t.Fatalf("the admitting rule: %+v", first)
	}
	if second.When.Operator != "not" || len(second.When.Children) != 1 || second.When.Children[0].Operator != "in" || second.Outcome != "declined" {
		t.Fatalf("the rule for everyone else: %+v", second)
	}
	reviewed, err := GateRuleApply(d, GateRuleEdit{Subject: GateRuleFactPrefix + "team", Values: []string{"Payroll", "Finance"}, Reason: "Your team", Else: "review"})
	if err != nil || len(reviewed.Rules) != 1 || reviewed.Rules[0].When.Fact != "team" || reviewed.Rules[0].When.Field != "" {
		t.Fatalf("a rule on a fact: %+v %v", reviewed.Rules, err)
	}
	// Both are rules the service accepts.
	registry := chatgate.NewRegistry()
	for name, def := range map[string]chatgate.Definition{"declined": declined, "review": reviewed} {
		if err := registry.ValidateDefinition(def, chatgate.Policy{Ceiling: "INTERNAL"}); err != nil {
			t.Fatalf("the service refuses the %s rule: %v", name, err)
		}
	}
	// Reading a rule back gives the edit that wrote it.
	if got := GateRuleOf(declined); got.Subject != "team" || strings.Join(got.Values, ",") != "Payroll,Finance" || got.Else != "declined" || got.Reason != "Your team runs payroll" {
		t.Fatalf("read back: %+v", got)
	}
	if got := GateRuleOf(reviewed); got.Subject != GateRuleFactPrefix+"team" || got.Else != "review" {
		t.Fatalf("read back a fact rule: %+v", got)
	}
	// Refused before the round trip: no subject, an unknown one, no answers, too
	// many, no reason.
	many := make([]string, GateRuleMaxValues+1)
	for i := range many {
		many[i] = "value-" + string(rune('a'+i))
	}
	for name, edit := range map[string]GateRuleEdit{
		"no subject":      {Values: []string{"x"}, Reason: "r"},
		"unknown subject": {Subject: "nothing", Values: []string{"x"}, Reason: "r"},
		"unknown fact":    {Subject: GateRuleFactPrefix + "salary", Values: []string{"x"}, Reason: "r"},
		"no answers":      {Subject: "team", Values: []string{" "}, Reason: "r"},
		"too many":        {Subject: "team", Values: many, Reason: "r"},
		"no reason":       {Subject: "team", Values: []string{"Payroll"}, Reason: "  "},
	} {
		if _, err := GateRuleApply(d, edit); err == nil {
			t.Errorf("%s: the rule was accepted", name)
		}
	}
	// The unfinished draft still draws: the subject is kept, no answers yet.
	draft := GateRuleDraft(d, GateRuleEdit{Subject: GateRuleFactPrefix + "team"})
	if got := GateRuleOf(draft); got.Subject != GateRuleFactPrefix+"team" || len(got.Values) != 0 {
		t.Fatalf("draft: %+v", got)
	}

	// The sentence.
	for _, tc := range []struct {
		def  chatgate.Definition
		want string
	}{
		{declined, "Admit people whose Which team are you in? is Payroll or Finance. Everyone else is not admitted."},
		{reviewed, "Admit people whose team is Payroll or Finance. Everyone else waits for an administrator."},
	} {
		if got := GateRulePreview("en-US", tc.def); got != tc.want {
			t.Errorf("preview %q, want %q", got, tc.want)
		}
	}
	three, _ := GateRuleApply(d, GateRuleEdit{Subject: "team", Values: []string{"Payroll", "Finance", "Legal"}, Reason: "r", Else: "review"})
	if got := GateRulePreview("en-US", three); !strings.Contains(got, "Payroll, Finance or Legal.") {
		t.Errorf("three answers read %q", got)
	}
	yes, _ := GateRuleApply(d, GateRuleEdit{Subject: "remote", Values: []string{"true"}, Reason: "r", Else: "review"})
	if got := GateRulePreview("en-US", yes); !strings.Contains(got, "is Yes.") {
		t.Errorf("a yes-or-no rule reads %q", got)
	}
	if got := GateRulePreview("en-US", d); got != GateText("en-US", "preview") {
		t.Errorf("no rule reads %q", got)
	}
	if got := GateRulePreview("de-DE", reviewed); !strings.Contains(got, "Payroll oder Finance") {
		t.Errorf("German reads %q", got)
	}

	// Known answers: a choice question's options, yes and no, the directory's.
	if got := GateRuleChoices(v, d, "team"); len(got) != 3 || got[2].ID != "Legal" {
		t.Fatalf("a choice question's answers: %+v", got)
	}
	if got := GateRuleChoices(v, d, "remote"); len(got) != 2 || got[0].ID != "true" || got[0].Label != "Yes" {
		t.Fatalf("a yes-or-no question's answers: %+v", got)
	}
	if got := GateRuleChoices(v, d, GateRuleFactPrefix+"team"); len(got) != 2 || got[0].ID != "People" {
		t.Fatalf("the directory's teams: %+v", got)
	}
	if got := GateRuleChoices(v, d, "why"); len(got) != 0 {
		t.Fatalf("a free-text question has known answers: %+v", got)
	}

	// The builder.
	for _, locale := range chatgateLocales {
		view := v
		view.Locale, view.Administrator, view.Conversation = locale, true, "room"
		draft := declined
		// An answer the options no longer hold stays in the rule, as text.
		draft.Rules[0].When.Values = []string{"Payroll", "Finance", "Treasury"}
		view.Gate = chatgate.Gate{Versions: []chatgate.Definition{d}, Current: "1.0.0", State: "active", Draft: &draft}
		markup := stdhtml.UnescapeString(gateMarkup(t, view))
		subjects := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "option" && n.Parent != nil && chatPolishAttr(n.Parent, "id") == "gate-rule-field"
		})
		values := map[string]bool{}
		for _, option := range subjects {
			values[chatPolishAttr(option, "value")] = true
		}
		for _, want := range []string{"team", "why", "remote", GateRuleFactPrefix + "team", GateRuleFactPrefix + "location"} {
			if !values[want] {
				t.Errorf("%s: the rule's subject does not offer %q", locale, want)
			}
		}
		boxes := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "input" && strings.HasPrefix(chatPolishAttr(n, "id"), "gate-rule-choice-")
		})
		if len(boxes) != 3 {
			t.Fatalf("%s: %d tick boxes for the question's three options", locale, len(boxes))
		}
		ticked := []string{}
		for _, box := range boxes {
			if chatmodHasAttr(box, "checked") {
				ticked = append(ticked, chatPolishAttr(box, "value"))
			}
			id := chatPolishAttr(box, "id")
			if labels := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "label" && chatPolishAttr(n, "for") == id }); len(labels) != 1 {
				t.Errorf("%s: tick box %s has %d labels", locale, id, len(labels))
			}
		}
		if strings.Join(ticked, ",") != "Payroll,Finance" {
			t.Errorf("%s: ticked %v, want the rule's known answers", locale, ticked)
		}
		others := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "textarea" && chatPolishAttr(n, "id") == "gate-rule-values" })
		if len(others) != 1 || others[0].FirstChild == nil || strings.TrimSpace(others[0].FirstChild.Data) != "Treasury" {
			t.Errorf("%s: an answer that is not among the options is not kept as text", locale)
		}
		for _, id := range []string{"gate-rule-field", "gate-rule-values", "gate-rule-else", "gate-rule-reason"} {
			if !strings.Contains(markup, `for="`+id+`"`) {
				t.Errorf("%s: %s has no label", locale, id)
			}
		}
		visible := chatmodVisible(markup)
		for _, key := range []string{"ruleChoices", "ruleOther", "ruleLimit", "ruleElse", "ruleElse_declined", "ruleElse_review", "ruleReason", "ruleFact_team", "ruleFact_location"} {
			if want := GateText(locale, key); want == "" || !strings.Contains(visible, want) {
				t.Errorf("%s: the rule editor does not show %q (%q)", locale, key, want)
			}
		}
		if want := GateRulePreview(locale, draft); !strings.Contains(visible, want) || strings.Contains(want, "%") {
			t.Errorf("%s: the rule is not said back: %q", locale, want)
		}
		// The publish step names the version and what it means.
		version, meaning := GatePublishMeaning(view)
		if version == "" || !strings.Contains(visible, meaning) || !strings.Contains(meaning, version) || strings.Contains(meaning, "%") {
			t.Errorf("%s: the publish step does not say what %q means: %q", locale, version, meaning)
		}
	}
}

// TestTodo_CHATGATE_006_Publish: the version the publish step names follows
// what changed, and its sentence says what that means for current members.
func TestTodo_CHATGATE_006_Publish(t *testing.T) {
	base := chatgateViewFixture()
	base.Administrator = true
	old := base.Gate.Versions[0]
	draft := func(change func(*chatgate.Definition)) GateView {
		v := base
		d := old
		d.Fields = append([]chatgate.Field(nil), old.Fields...)
		change(&d)
		v.Gate.Draft = &d
		return v
	}
	for _, tc := range []struct {
		name    string
		view    GateView
		version string
		key     string
	}{
		{"wording", draft(func(d *chatgate.Definition) { d.Fields[0].Label = "Your team" }), "1.0.1", "publishPatch"},
		{"an optional question", draft(func(d *chatgate.Definition) {
			d.Fields = append(d.Fields, chatgate.Field{ID: "note", Kind: "short_text", KindVersion: "1.0.0", Label: "Note", Purpose: "p", DataClass: "INTERNAL", RetentionDays: 30, Visibility: chatgate.Visibility{Administrators: true}})
		}), "1.1.0", "publishMinor"},
		{"a required question", draft(func(d *chatgate.Definition) {
			d.Fields = append(d.Fields, chatgate.Field{ID: "must", Kind: "short_text", KindVersion: "1.0.0", Label: "Must", Purpose: "p", DataClass: "INTERNAL", Required: true, RetentionDays: 30, Visibility: chatgate.Visibility{Administrators: true}})
		}), "2.0.0", "publishMajor"},
	} {
		version, meaning := GatePublishMeaning(tc.view)
		if version != tc.version || meaning != strings.Replace(GateText("en-US", tc.key), "%s", tc.version, 1) {
			t.Errorf("%s: %q %q, want %s and the %s sentence", tc.name, version, meaning, tc.version, tc.key)
		}
	}
	first := GateView{Locale: "en-US", Administrator: true, Gate: chatgate.Gate{Draft: &old}}
	if version, meaning := GatePublishMeaning(first); version != "1.0.0" || !strings.Contains(meaning, "turns the gate on") {
		t.Fatalf("a first version: %q %q", version, meaning)
	}
}

// TestTodo_CHATGATE_005_Withdraw: the consequence of withdrawing is stated to
// the person it applies to, a member admitted on a required answer; anyone
// else is told only that their answers are deleted.
func TestTodo_CHATGATE_005_Withdraw(t *testing.T) {
	v := chatgateViewFixture()
	v.ConfirmWithdrawal = true
	for _, tc := range []struct {
		name       string
		submission *chatgate.Submission
		required   bool
		leaves     bool
	}{
		{"a member with a required answer", &chatgate.Submission{Status: "admitted", Version: "1.0.0"}, true, true},
		{"a member whose answers were all optional", &chatgate.Submission{Status: "admitted", Version: "1.0.0"}, false, false},
		{"a person waiting for review", &chatgate.Submission{Status: "review", Version: "1.0.0"}, true, false},
		{"nobody", nil, true, false},
	} {
		view := v
		view.Gate.Versions = append([]chatgate.Definition(nil), v.Gate.Versions...)
		view.Gate.Versions[0].Fields = append([]chatgate.Field(nil), v.Gate.Versions[0].Fields...)
		view.Gate.Versions[0].Fields[0].Required = tc.required
		view.Submission = tc.submission
		markup := stdhtml.UnescapeString(gateMarkup(t, view))
		if got := strings.Contains(markup, GateText("en-US", "consequence")); got != tc.leaves {
			t.Errorf("%s: told they leave the channel=%v, want %v", tc.name, got, tc.leaves)
		}
		if got := strings.Contains(markup, GateText("en-US", "withdrawPlain")); got == tc.leaves {
			t.Errorf("%s: told only that answers are deleted=%v", tc.name, got)
		}
	}
}

// TestTodo_CHATGATE_006_Accessibility_Answers: the answers table has column
// headings and lists people by name, the same way on every read.
func TestTodo_CHATGATE_006_Accessibility_Answers(t *testing.T) {
	v := chatgateViewFixture()
	v.Administrator = true
	v.Names = map[string]string{"w3": "Carla", "w1": "Alex", "w2": "Bea"}
	v.VisibleAnswers = map[string]map[string]json.RawMessage{"w3": {"team": json.RawMessage(`"Finance"`)}, "w1": {"team": json.RawMessage(`"Payroll"`)}, "w2": {"team": json.RawMessage(`"Payroll"`)}}
	for range 5 {
		markup := stdhtml.UnescapeString(gateMarkup(t, v))
		heads := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "th" && chatPolishAttr(n, "scope") == "col" })
		if len(heads) != 3 {
			t.Fatalf("%d column headings, want Person, Question, Answer", len(heads))
		}
		rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "tr" && chatPolishAttr(n, "data-gate-answer-row") == "true" })
		var names []string
		for _, row := range rows {
			if row.FirstChild != nil && row.FirstChild.FirstChild != nil {
				names = append(names, row.FirstChild.FirstChild.Data)
			}
		}
		if strings.Join(names, ",") != "Alex,Bea,Carla" {
			t.Fatalf("rows in the order %v", names)
		}
	}
}
