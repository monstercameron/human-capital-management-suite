package chatui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

type GateChoice struct{ ID, Label, Version string }

func gateJoinTitle(locale string, n int) string {
	if n == 1 {
		return GateText(locale, "joinOne")
	}
	return fmt.Sprintf(GateText(locale, "join"), n)
}

type GateView struct {
	Reviewers                                                  []string
	ConsumerNames                                              map[string]string
	Locale, Conversation, ChannelPurpose, State, Error, Notice string
	Gate                                                       chatgate.Gate
	Submission                                                 *chatgate.Submission
	Answers                                                    map[string]json.RawMessage
	FieldErrors                                                map[string]string
	Directory                                                  map[string][]GateChoice
	Controls                                                   map[string]chatgate.Control
	Administrator, ExportAllowed, ConfirmWithdrawal, Held      bool
	Submissions                                                []chatgate.Submission
	VisibleAnswers                                             map[string]map[string]json.RawMessage
	Names                                                      map[string]string
	Reads                                                      []chatgate.ReadAudit
}

func gateButton(v GateView, action, label, id string, primary bool) ui.Node {
	class := ""
	if primary {
		class = "primary"
	}
	return html.Button(html.Props{Type: "button", Class: class, Text: GateText(v.Locale, label), Data: map[string]string{"gate-action": action, "id": id}})
}
func RenderGate(v GateView) ui.Node {
	t := func(k string) string { return GateText(v.Locale, k) }
	dir := "ltr"
	if v.Locale == "ar" {
		dir = "rtl"
	}
	children := []ui.Node{html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "close", "close", "", false))}
	if v.State == "loading" {
		children = append(children, html.P(html.Props{Role: "status", Text: t("loading")}))
	} else if v.Error != "" {
		children = append(children, html.P(html.Props{Role: "alert", Text: t("error")}), gateButton(v, "retry", "retry", "", true))
	} else {
		if v.Notice != "" {
			children = append(children, html.P(html.Props{Role: "status", Text: v.Notice}))
		}
		if v.Gate.State == "paused" || v.Gate.State == "retired" {
			children = append(children, html.P(html.Props{Role: "status", Text: t(v.Gate.State)}))
		}
		if v.Administrator {
			children = append(children, renderGateBuilder(v), renderGateQueue(v), renderGateAnswers(v))
		} else {
			children = append(children, renderGateApplicant(v))
		}
	}
	return html.Section(html.Props{Class: "chatgate", Lang: v.Locale, Dir: dir, Data: map[string]string{"conversation": v.Conversation}, Aria: map[string]string{"label": t("gate")}}, children...)
}
func gateDefinition(v GateView) chatgate.Definition {
	if v.Administrator && v.Gate.Draft != nil {
		return *v.Gate.Draft
	}
	for _, d := range v.Gate.Versions {
		if d.Version.String() == v.Gate.Current {
			return d
		}
	}
	return chatgate.Definition{Mode: "review"}
}
func renderGateApplicant(v GateView) ui.Node {
	t := func(k string) string { return GateText(v.Locale, k) }
	d := gateDefinition(v)
	nodes := []ui.Node{html.H2(html.Props{Text: gateJoinTitle(v.Locale, len(d.Fields))}), html.P(html.Props{Text: v.ChannelPurpose}), html.P(html.Props{Text: d.Purpose})}
	if v.Submission != nil {
		switch v.Submission.Status {
		case "admitted":
			nodes = append(nodes, html.P(html.Props{Role: "status", Text: t("joined")}))
		case "review":
			nodes = append(nodes, html.P(html.Props{Role: "status", Text: t("waiting")}), html.P(html.Props{Text: t("since") + v.Submission.SubmittedAt.Format("2006-01-02 15:04 MST")}), gateButton(v, "withdraw", "withdraw", v.Submission.ID, false))
			if len(v.Reviewers) > 0 {
				nodes = append(nodes, html.P(html.Props{Text: t("admin") + ": " + strings.Join(v.Reviewers, ", ")}))
			}
		case "declined":
			nodes = append(nodes, html.P(html.Props{Role: "status", Text: t("declined")}), html.P(html.Props{Text: v.Submission.Reason}), html.P(html.Props{Text: t("next")}))
		}
		old, _ := chatgate.ParseVersion(v.Submission.Version)
		live, _ := chatgate.ParseVersion(v.Gate.Current)
		if old.Major > 0 && old.Major < live.Major && !d.AnswerBy.IsZero() {
			nodes = append(nodes, html.P(html.Props{Role: "status", Text: fmt.Sprintf(t("changed"), d.AnswerBy.Format("2006-01-02"))}))
		}
	}
	if v.Held {
		nodes = append(nodes, html.P(html.Props{Text: t("hold")}))
	}
	if len(v.Answers) > 0 {
		items := []ui.Node{html.H3(html.Props{Text: t("answers")})}
		old := d
		if v.Submission != nil {
			for _, version := range v.Gate.Versions {
				if version.Version.String() == v.Submission.Version {
					old = version
				}
			}
		}
		for _, f := range old.Fields {
			if value, ok := v.Answers[f.ID]; ok {
				items = append(items, html.P(html.Props{Text: f.Label + ": " + string(value)}))
			}
		}
		nodes = append(nodes, html.Section(html.Props{}, items...))
	}
	if v.ConfirmWithdrawal {
		nodes = append(nodes, html.P(html.Props{Role: "alert", Text: t("consequence")}), gateButton(v, "confirm-withdraw", "confirm", "", true))
	}
	fields := []ui.Node{}
	for _, f := range d.Fields {
		fields = append(fields, renderGateField(v, f))
	}
	fields = append(fields, html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "submit", "submit", "", true), gateButton(v, "save", "save", "", false)))
	if len(d.Fields) > 0 && v.Gate.State != "paused" && v.Gate.State != "retired" {
		nodes = append(nodes, html.Form(html.Props{ID: "gate-answer-form"}, fields...))
	}
	if v.Submission != nil && v.Submission.Status == "admitted" {
		nodes = append(nodes, gateButton(v, "withdraw", "withdraw", v.Submission.ID, false))
	}
	audit := []ui.Node{html.H3(html.Props{Text: t("readers")})}
	for _, a := range v.Reads {
		name := v.Names[a.Reader]
		if name == "" {
			name = t("admin")
		}
		audit = append(audit, html.P(html.Props{Text: name + " · " + a.Purpose + " · " + a.At.Format("2006-01-02")}))
	}
	nodes = append(nodes, html.Section(html.Props{}, audit...))
	return html.Div(html.Props{}, nodes...)
}
func renderGateField(v GateView, f chatgate.Field) ui.Node {
	t := func(k string) string { return GateText(v.Locale, k) }
	registry := chatgate.NewRegistry()
	control, known := v.Controls[f.ID]
	if !known {
		if kind, ok := registry.Kind(f.Kind, f.KindVersion); ok {
			control = kind.Render(f, v.Locale)
			known = true
		}
	}
	if !known {
		return html.P(html.Props{Role: "alert", Text: t("unknown")})
	}
	f.Kind = control.Type
	if len(control.Options) > 0 {
		f.Options = control.Options
	}
	id := "gate-field-" + f.ID
	requirement := t("optional")
	if f.Required {
		requirement = t("required")
	}
	audience := []string{}
	if f.Visibility.Administrators {
		audience = append(audience, t("admin"))
	}
	if f.Visibility.Members {
		audience = append(audience, t("members"))
	}
	if len(f.Visibility.Consumers) > 0 {
		for _, id := range f.Visibility.Consumers {
			label := v.ConsumerNames[id]
			if label == "" {
				label = t("consumer")
			}
			audience = append(audience, label)
		}
	}
	nodes := []ui.Node{html.Label(html.Props{For: id, Text: f.Label + " · " + requirement}), html.P(html.Props{ID: id + "-hint", Class: "chatgate-hint", Text: f.Help + " " + t("purpose") + f.Purpose + " · " + t("visibility") + strings.Join(audience, ", ") + " · " + fmt.Sprintf(t("retention"), f.RetentionDays)})}
	props := html.Props{ID: id, Name: f.ID, Required: f.Required, Aria: map[string]string{"describedby": id + "-hint " + id + "-error", "invalid": strconv.FormatBool(v.FieldErrors[f.ID] != "")}, Data: map[string]string{"gate-field": f.ID, "kind": f.Kind}}
	var value string
	_ = json.Unmarshal(v.Answers[f.ID], &value)
	switch f.Kind {
	case "long_text":
		props.Rows = 4
		props.MaxLength = 500
		nodes = append(nodes, html.Textarea(props))
	case "single_choice", "person", "team", "location":
		options := []ui.Node{html.Option(html.Props{Value: "", Text: t("choose")})}
		choices := []GateChoice{}
		if f.Kind == "single_choice" {
			for _, o := range f.Options {
				choices = append(choices, GateChoice{ID: o, Label: o})
			}
		} else {
			choices = v.Directory[f.Kind]
		}
		if len(choices) == 0 {
			nodes = append(nodes, html.P(html.Props{Role: "alert", Text: t("unavailable")}))
			props.Disabled = true
		}
		for _, o := range choices {
			options = append(options, html.Option(html.Props{Value: o.ID, Text: o.Label, Selected: o.ID == value}))
		}
		nodes = append(nodes, html.Select(props, options...))
	case "multiple_choice":
		var selected []string
		_ = json.Unmarshal(v.Answers[f.ID], &selected)
		for i, o := range f.Options {
			p := props
			p.ID = id + "-" + strconv.Itoa(i)
			p.Type = "checkbox"
			p.Required = false
			p.Value = o
			p.Checked = false
			for _, x := range selected {
				p.Checked = p.Checked || x == o
			}
			nodes = append(nodes, html.Label(html.Props{For: p.ID}, html.Input(p), html.Span(html.Props{Text: o})))
		}
	case "boolean", "acknowledgement":
		props.Type = "checkbox"
		props.Required = f.Required && f.Kind == "acknowledgement"
		var checked bool
		_ = json.Unmarshal(v.Answers[f.ID], &checked)
		props.Checked = checked
		nodes = append(nodes, html.Input(props))
		if f.Kind == "acknowledgement" {
			nodes = append(nodes, html.A(html.Props{Href: "/documents/" + url.PathEscape(f.DocumentID) + "?version=" + url.QueryEscape(f.DocumentVersion), Text: t("ack")}))
		}
	default:
		props.Type = "text"
		if f.Kind == "date" {
			props.Type = "date"
		}
		props.MaxLength = 200
		nodes = append(nodes, html.Input(props))
	}
	nodes = append(nodes, html.P(html.Props{ID: id + "-error", Class: "chatgate-error", Role: "alert", Text: v.FieldErrors[f.ID]}))
	return html.Div(html.Props{}, nodes...)
}
func renderGateQueue(v GateView) ui.Node {
	t := func(k string) string { return GateText(v.Locale, k) }
	nodes := []ui.Node{html.H2(html.Props{Text: t("queue")})}
	n := 0
	for _, sub := range v.Submissions {
		if sub.Status != "review" {
			continue
		}
		n++
		name := v.Names[sub.Person]
		if name == "" {
			name = t("select")
		}
		items := []ui.Node{html.Label(html.Props{}, html.Input(html.Props{Type: "checkbox", Data: map[string]string{"gate-select": sub.ID}, Aria: map[string]string{"label": t("select") + " " + name}}), html.Span(html.Props{Text: name})), html.P(html.Props{Text: t("since") + sub.SubmittedAt.Format("2006-01-02 15:04 MST")})}
		for id, value := range v.VisibleAnswers[sub.Person] {
			label := t("label")
			for _, f := range gateDefinition(v).Fields {
				if f.ID == id {
					label = f.Label
				}
			}
			items = append(items, html.P(html.Props{Text: label + ": " + string(value)}))
		}
		items = append(items, html.Label(html.Props{For: "gate-reason-" + sub.ID, Text: t("reason")}), html.Input(html.Props{ID: "gate-reason-" + sub.ID, Type: "text", Required: true}), html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "admit", "admit", sub.ID, true), gateButton(v, "decline", "decline", sub.ID, false)))
		nodes = append(nodes, html.Fieldset(html.Props{}, items...))
	}
	if n == 0 {
		nodes = append(nodes, html.P(html.Props{Text: t("empty")}))
	}
	nodes = append(nodes, html.Label(html.Props{For: "gate-bulk-reason", Text: t("reason")}), html.Input(html.Props{ID: "gate-bulk-reason", Type: "text"}), html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "bulk-admit", "admitSelected", "", false), gateButton(v, "bulk-decline", "declineSelected", "", false)))
	return html.Section(html.Props{}, nodes...)
}
func renderGateAnswers(v GateView) ui.Node {
	t := func(k string) string { return GateText(v.Locale, k) }
	rows := []ui.Node{}
	for person, answers := range v.VisibleAnswers {
		name := v.Names[person]
		if name == "" {
			name = t("answers")
		}
		ids := []string{}
		for id := range answers {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			label := t("label")
			for _, f := range gateDefinition(v).Fields {
				if f.ID == id {
					label = f.Label
				}
			}
			rows = append(rows, html.Tr(html.Props{Data: map[string]string{"gate-answer-row": "true"}}, html.Td(html.Props{Text: name}), html.Td(html.Props{Text: label}), html.Td(html.Props{Text: string(answers[id])})))
		}
	}
	nodes := []ui.Node{html.H2(html.Props{Text: t("answerView")}), html.Label(html.Props{For: "gate-filter", Text: t("filter")}), html.Input(html.Props{ID: "gate-filter", Type: "search"}), html.P(html.Props{ID: "gate-answer-count", Role: "status", Text: fmt.Sprintf(t("count"), len(rows))}), html.Div(html.Props{Class: "chatgate-table-wrap"}, html.Table(html.Props{}, html.Tbody(html.Props{}, rows...)))}
	if v.ExportAllowed {
		nodes = append(nodes, gateButton(v, "export", "export", "", false))
	}
	counts := GateAnswerCounts(v)
	fieldIDs := []string{}
	for id := range counts {
		fieldIDs = append(fieldIDs, id)
	}
	sort.Strings(fieldIDs)
	for _, id := range fieldIDs {
		label := t("label")
		for _, field := range gateDefinition(v).Fields {
			if field.ID == id {
				label = field.Label
			}
		}
		values := []string{}
		for value := range counts[id] {
			values = append(values, value)
		}
		sort.Strings(values)
		for _, value := range values {
			nodes = append(nodes, html.P(html.Props{Text: label + ": " + value + " · " + fmt.Sprintf(t("count"), counts[id][value])}))
		}
	}
	return html.Section(html.Props{}, nodes...)
}

// GateAnswerCounts accepts only the service's field-filtered projection; no
// hidden field can contribute even a count to the administrator's table.
func GateAnswerCounts(v GateView) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, answers := range v.VisibleAnswers {
		for field, value := range answers {
			label := string(value)
			var text string
			if json.Unmarshal(value, &text) == nil {
				label = text
			}
			if out[field] == nil {
				out[field] = map[string]int{}
			}
			out[field][label]++
		}
	}
	return out
}
func chatgateDetailsSection(m Model) ui.Node {
	if m.ChatFeatures != nil && !m.ChatFeatures.Gates {
		return nil
	}
	c := m.selected()
	if c.Kind == DirectMessage {
		return nil
	}
	label := GateText(m.Locale, "answers")
	if c.OwnerID == m.CurrentUser {
		label = GateText(m.Locale, "gate")
	}
	return html.Section(html.Props{Class: "details-section"}, html.A(html.Props{Href: "#gate=" + url.QueryEscape(c.ID), Text: label, Data: map[string]string{"gate-open": c.ID}}))
}
