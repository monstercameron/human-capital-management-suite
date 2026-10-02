package chatui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// CHATGATE-006: the rule editor's "in" lists, the publish step and the form's
// own checks.
//
// A rule admits people whose answer to one question, or whose team or location
// in the people directory, is one of a list. The editor could only name a
// question, took the list as free text even when the possible answers were
// known, and always declined everyone else. It now also offers the two facts
// of the directory, lists the known answers as tick boxes (a question's
// options, the directory's teams or locations) with a box for others, and lets
// the administrator choose what happens to everyone else. The sentence under
// it reads as one would say it: "Admit people whose team is Payroll or
// Finance. Everyone else waits for an administrator."

// GateRuleFactPrefix marks a rule's subject as a fact of the people directory
// rather than a question. A question's identifier cannot contain a colon.
const GateRuleFactPrefix = "fact:"

// gateRuleFacts are the facts a rule may test, in the order they are offered.
// "person" is a fact too, and a rule about one person is not a rule.
var gateRuleFacts = []string{"team", "location"}

// GateRuleMaxValues is how many answers an "in" list holds; the service
// refuses more.
const GateRuleMaxValues = 20

// GateRuleEdit is the rule as the editor shows it: whose answer or fact is
// tested, the list that admits, the reason people are given, and what happens
// to everyone else ("declined" or "review").
type GateRuleEdit struct {
	Subject string
	Values  []string
	Reason  string
	Else    string
}

// GateRuleOf reads the editor's rule out of a definition. A definition written
// by another client keeps its rules until this editor saves one.
func GateRuleOf(d chatgate.Definition) GateRuleEdit {
	edit := GateRuleEdit{Else: "declined"}
	if len(d.Rules) == 0 {
		return edit
	}
	first := d.Rules[0]
	edit.Values, edit.Reason = append([]string(nil), first.When.Values...), first.Reason
	edit.Subject = first.When.Field
	if first.When.Fact != "" {
		edit.Subject = GateRuleFactPrefix + first.When.Fact
	}
	// With no second rule a request that does not match waits for a person.
	edit.Else = "review"
	if len(d.Rules) > 1 && d.Rules[1].Outcome == "declined" {
		edit.Else = "declined"
	}
	return edit
}

// GateRuleApply writes the editor's rule into a definition: the "in" list that
// admits and, when everyone else is declined, its negation. It refuses a rule
// the service would refuse, so the administrator is told before the round trip.
func GateRuleApply(d chatgate.Definition, edit GateRuleEdit) (chatgate.Definition, error) {
	fact, isFact := strings.CutPrefix(edit.Subject, GateRuleFactPrefix)
	known := false
	if isFact {
		for _, name := range gateRuleFacts {
			known = known || name == fact
		}
	} else {
		for _, f := range d.Fields {
			known = known || (f.ID == edit.Subject && edit.Subject != "")
		}
	}
	values := gateRuleDistinct(edit.Values)
	if !known || len(values) == 0 || len(values) > GateRuleMaxValues || strings.TrimSpace(edit.Reason) == "" {
		return d, chatgate.ErrInvalid
	}
	return GateRuleDraft(d, edit), nil
}

// GateRuleDraft writes the editor's rule into a definition as it stands, also
// when it is not finished: the builder draws itself from the draft, and a rule
// whose subject was just chosen has no answers yet. Only GateRuleApply's result
// is sent to the service.
func GateRuleDraft(d chatgate.Definition, edit GateRuleEdit) chatgate.Definition {
	when := chatgate.Expression{Operator: "in", Values: gateRuleDistinct(edit.Values)}
	if fact, ok := strings.CutPrefix(edit.Subject, GateRuleFactPrefix); ok {
		when.Fact = fact
	} else {
		when.Field = edit.Subject
	}
	reason := strings.TrimSpace(edit.Reason)
	d.Rules = []chatgate.Rule{{When: when, Outcome: "admitted", Reason: reason}}
	if edit.Else != "review" {
		d.Rules = append(d.Rules, chatgate.Rule{When: chatgate.Expression{Operator: "not", Children: []chatgate.Expression{when}}, Outcome: "declined", Reason: reason})
	}
	return d
}

func gateRuleDistinct(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

// GateRuleChoices is the answers a rule's subject is known to take: a choice
// question's options, yes and no for a yes-or-no question, the directory's
// teams or locations for a fact or for a team or location question. Empty for
// a subject whose answers are free text.
func GateRuleChoices(v GateView, d chatgate.Definition, subject string) []GateChoice {
	if fact, ok := strings.CutPrefix(subject, GateRuleFactPrefix); ok {
		return v.Directory[fact]
	}
	for _, f := range d.Fields {
		if f.ID != subject {
			continue
		}
		switch f.Kind {
		case "single_choice":
			out := make([]GateChoice, 0, len(f.Options))
			for _, option := range f.Options {
				out = append(out, GateChoice{ID: option, Label: option})
			}
			return out
		case "boolean":
			return []GateChoice{{ID: "true", Label: GateText(v.Locale, "yes")}, {ID: "false", Label: GateText(v.Locale, "no")}}
		case "team", "location", "person":
			return v.Directory[f.Kind]
		}
	}
	return nil
}

// gateRuleSubjectLabel names a rule's subject in a sentence: the question as it
// is asked, or "team" and "location".
func gateRuleSubjectLabel(locale string, d chatgate.Definition, subject string) string {
	if fact, ok := strings.CutPrefix(subject, GateRuleFactPrefix); ok {
		return GateText(locale, "fact_"+fact)
	}
	for _, f := range d.Fields {
		if f.ID == subject && f.Label != "" {
			return f.Label
		}
	}
	return GateText(locale, "label")
}

// gateRuleList writes a list as it is said: "Payroll", "Payroll or Finance",
// "Payroll, Finance or Legal".
func gateRuleList(locale string, values []string) string {
	or := GateText(locale, "or")
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	}
	return strings.Join(values[:len(values)-1], GateText(locale, "comma")) + or + values[len(values)-1]
}

// GateRulePreview says the rule in one or two plain sentences.
func GateRulePreview(locale string, d chatgate.Definition) string {
	edit := GateRuleOf(d)
	if len(d.Rules) == 0 || len(edit.Values) == 0 {
		return GateText(locale, "preview")
	}
	// The list shows answers as people read them: yes and no, not true and false.
	shown := make([]string, 0, len(edit.Values))
	for _, value := range edit.Values {
		shown = append(shown, gateRuleValueLabel(locale, d, edit.Subject, value))
	}
	sentence := fmt.Sprintf(GateText(locale, "rulePreview"), gateRuleSubjectLabel(locale, d, edit.Subject), gateRuleList(locale, shown))
	return sentence + GateText(locale, "sentenceEnd") + " " + GateText(locale, "rulePreviewElse_"+edit.Else)
}

func gateRuleValueLabel(locale string, d chatgate.Definition, subject, value string) string {
	for _, f := range d.Fields {
		if f.ID == subject && f.Kind == "boolean" {
			if value == "true" {
				return GateText(locale, "yes")
			}
			if value == "false" {
				return GateText(locale, "no")
			}
		}
	}
	return value
}

// gateRuleEditor is the rule's fieldset in the builder.
func gateRuleEditor(v GateView, d chatgate.Definition) ui.Node {
	t := func(k string) string { return GateText(v.Locale, k) }
	edit := GateRuleOf(d)
	subjects := []ui.Node{html.Option(html.Props{Value: "", Text: t("choose")})}
	for _, f := range d.Fields {
		label := f.Label
		if label == "" {
			label = t("label")
		}
		subjects = append(subjects, html.Option(html.Props{Value: f.ID, Text: label, Selected: edit.Subject == f.ID}))
	}
	for _, fact := range gateRuleFacts {
		subjects = append(subjects, html.Option(html.Props{Value: GateRuleFactPrefix + fact, Text: t("ruleFact_" + fact), Selected: edit.Subject == GateRuleFactPrefix+fact}))
	}
	nodes := []ui.Node{html.Legend(html.Props{Text: t("rule")}),
		html.Label(html.Props{For: "gate-rule-field"}, html.Span(html.Props{Text: t("ruleField")}), html.Select(html.Props{ID: "gate-rule-field", Aria: map[string]string{"describedby": "gate-rule-preview"}}, subjects...))}
	choices := GateRuleChoices(v, d, edit.Subject)
	known := map[string]bool{}
	if len(choices) > 0 {
		boxes := []ui.Node{html.Legend(html.Props{Text: t("ruleChoices")})}
		for i, choice := range choices {
			known[choice.ID] = true
			id := "gate-rule-choice-" + strconv.Itoa(i)
			checked := false
			for _, value := range edit.Values {
				checked = checked || value == choice.ID
			}
			boxes = append(boxes, html.Label(html.Props{For: id}, html.Input(html.Props{ID: id, Type: "checkbox", Value: choice.ID, Checked: checked}), html.Span(html.Props{Dir: "auto", Text: choice.Label})))
		}
		nodes = append(nodes, html.Fieldset(html.Props{Class: "chatgate-rule-choices"}, boxes...))
	}
	// Answers the lists above do not hold (free text, or a team that is no
	// longer in the directory) are typed, one per line.
	label := "ruleValue"
	if len(choices) > 0 {
		label = "ruleOther"
	}
	var other []string
	for _, value := range edit.Values {
		if !known[value] {
			other = append(other, value)
		}
	}
	nodes = append(nodes, html.Label(html.Props{For: "gate-rule-values", Text: t(label)}),
		html.Textarea(html.Props{ID: "gate-rule-values", Rows: 3, Aria: map[string]string{"describedby": "gate-rule-limit"}}, ui.Text(strings.Join(other, "\n"))),
		html.P(html.Props{ID: "gate-rule-limit", Class: "chatgate-hint", Text: t("ruleLimit")}),
		html.Label(html.Props{For: "gate-rule-else", Text: t("ruleElse")}),
		html.Select(html.Props{ID: "gate-rule-else"},
			html.Option(html.Props{Value: "declined", Text: t("ruleElse_declined"), Selected: edit.Else != "review"}),
			html.Option(html.Props{Value: "review", Text: t("ruleElse_review"), Selected: edit.Else == "review"})),
		gateEditorInput(v, "gate-rule-reason", "ruleReason", "text"),
		html.P(html.Props{ID: "gate-rule-preview", Class: "chatgate-rule-preview", Role: "status", Dir: "auto", Text: GateRulePreview(v.Locale, d)}))
	return html.Fieldset(html.Props{Class: "chatgate-rule"}, nodes...)
}

// GatePublishMeaning is the version the builder suggests for the draft and one
// sentence saying what publishing it means for the people already in the
// channel. The version is computed from what changed, as the service requires.
func GatePublishMeaning(v GateView) (version, sentence string) {
	d := gateDefinition(v)
	for _, old := range v.Gate.Versions {
		if old.Version.String() != v.Gate.Current {
			continue
		}
		next := chatgate.RequiredBump(old, d)
		key := "publishPatch"
		switch {
		case next.Major > old.Version.Major:
			key = "publishMajor"
		case next.Minor > old.Version.Minor:
			key = "publishMinor"
		}
		return next.String(), fmt.Sprintf(GateText(v.Locale, key), next.String())
	}
	return "1.0.0", fmt.Sprintf(GateText(v.Locale, "publishFirst"), "1.0.0")
}

// GateAnswerProblems checks a form's answers the way the service will, so a
// person is told what is wrong before the answers are sent: a required
// question left empty, an answer that is not one of the choices, a date that
// is not a date. It answers the copy key for each question that is wrong
// ("requiredError" or "invalid"). What only the server knows (whether a named
// person or team exists) is still checked there.
func GateAnswerProblems(d chatgate.Definition, answers map[string]json.RawMessage) map[string]string {
	registry := chatgate.NewRegistry()
	problems := map[string]string{}
	for _, f := range d.Fields {
		value, answered := answers[f.ID]
		if !answered || gateAnswerEmpty(f, value) {
			if f.Required {
				problems[f.ID] = "requiredError"
			}
			continue
		}
		kind, ok := registry.Kind(f.Kind, f.KindVersion)
		if !ok {
			// A kind this build does not know is left to the server.
			continue
		}
		if kind.Validate(f, value) != nil {
			problems[f.ID] = "invalid"
		}
	}
	return problems
}

// gateAnswerEmpty reports whether an answer says nothing: an empty text, no
// choice ticked, an acknowledgement or a required yes left unticked.
func gateAnswerEmpty(f chatgate.Field, value json.RawMessage) bool {
	switch f.Kind {
	case "multiple_choice":
		var list []string
		return json.Unmarshal(value, &list) == nil && len(list) == 0
	case "acknowledgement":
		return string(value) != "true"
	case "boolean":
		return false
	}
	var text string
	return json.Unmarshal(value, &text) == nil && strings.TrimSpace(text) == ""
}

// chatgateBuilderStyles: the rule's tick boxes sit in a wrapping row, and the
// sentence that says the rule back is set apart from the fields.
const chatgateBuilderStyles = `.chatgate .chatgate-rule-choices{display:flex;flex-wrap:wrap;gap:.25rem 1rem}.chatgate .chatgate-rule-choices legend{inline-size:100%}.chatgate .chatgate-rule-choices label{display:inline-flex;align-items:center;gap:.25rem;margin-block:0;min-block-size:44px}` +
	`.chatgate-rule-preview,.chatgate-publish-meaning{margin-block:.5rem;padding:.5rem .75rem;border-inline-start:3px solid var(--hcm-color-brand-primary);background:var(--hcm-color-brand-soft);border-radius:var(--hcm-radius-control)}` +
	`.chatgate thead th{font-weight:650;border-block-end:1px solid var(--hcm-color-border)}`
