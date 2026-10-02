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

func gateEditorInput(v GateView, id, label, kind string) ui.Node {
	return html.Label(html.Props{For: id}, html.Span(html.Props{Text: GateText(v.Locale, label)}), html.Input(html.Props{ID: id, Type: kind}))
}
func renderGateBuilder(v GateView) ui.Node {
	t := func(k string) string { return GateText(v.Locale, k) }
	d := gateDefinition(v)
	nodes := []ui.Node{html.H2(html.Props{Text: t("gate")}), gateEditorInput(v, "gate-purpose", "why", "text"), html.Label(html.Props{For: "gate-mode", Text: t("mode")})}
	modes := []ui.Node{}
	for _, mode := range []string{"automatic", "rule", "review"} {
		modes = append(modes, html.Option(html.Props{Value: mode, Selected: mode == d.Mode, Text: t(mode)}))
	}
	nodes = append(nodes, html.Select(html.Props{ID: "gate-mode"}, modes...))
	registry := chatgate.NewRegistry()
	sampleOptions := []ui.Node{html.Option(html.Props{Value: "", Text: t("sample")})}
	for _, person := range v.Directory["person"] {
		sampleOptions = append(sampleOptions, html.Option(html.Props{Value: person.ID, Text: person.Label}))
	}
	nodes = append(nodes, html.Label(html.Props{For: "gate-sample-person", Text: t("sample")}), html.Select(html.Props{ID: "gate-sample-person"}, sampleOptions...))
	for i, f := range d.Fields {
		id := strconv.Itoa(i)
		prefix := "gate-editor-" + id + "-"
		items := []ui.Node{html.Legend(html.Props{Text: f.Label}), gateEditorInput(v, prefix+"label", "label", "text"), gateEditorInput(v, prefix+"help", "help", "text"), gateEditorInput(v, prefix+"purpose", "why", "text"), gateEditorInput(v, prefix+"days", "days", "number"), html.Label(html.Props{For: prefix + "kind", Text: t("kind")})}
		kinds := []ui.Node{}
		for _, kind := range []string{"short_text", "long_text", "single_choice", "multiple_choice", "boolean", "date", "person", "team", "location", "acknowledgement"} {
			k, _ := registry.Kind(kind, "1.0.0")
			kinds = append(kinds, html.Option(html.Props{Value: kind, Selected: f.Kind == kind, Text: k.Names[v.Locale]}))
		}
		items = append(items, html.Select(html.Props{ID: prefix + "kind"}, kinds...), html.Label(html.Props{For: prefix + "options", Text: t("options")}), html.Textarea(html.Props{ID: prefix + "options", Rows: 3}, ui.Text(strings.Join(f.Options, "\n"))))
		items = append(items, html.Label(html.Props{For: prefix + "class", Text: t("class")}), html.Select(html.Props{ID: prefix + "class"}, html.Option(html.Props{Value: "PUBLIC", Text: t("PUBLIC"), Selected: f.DataClass == "PUBLIC"}), html.Option(html.Props{Value: "INTERNAL", Text: t("INTERNAL"), Selected: f.DataClass == "INTERNAL"})))
		if len(v.ConsumerNames) > 0 {
			options := []ui.Node{}
			for consumer, name := range v.ConsumerNames {
				selected := false
				for _, id := range f.Visibility.Consumers {
					selected = selected || id == consumer
				}
				options = append(options, html.Option(html.Props{Value: consumer, Text: name, Selected: selected}))
			}
			items = append(items, html.Label(html.Props{For: prefix + "consumers", Text: t("consumer")}), html.Select(html.Props{ID: prefix + "consumers", Multiple: true}, options...))
		}
		if f.Kind == "acknowledgement" {
			docs := []ui.Node{html.Option(html.Props{Value: "", Text: t("choose")})}
			for _, doc := range v.Directory["document"] {
				value, _ := json.Marshal([]string{doc.ID, doc.Version})
				docs = append(docs, html.Option(html.Props{Value: string(value), Text: doc.Label, Selected: f.DocumentID == doc.ID && f.DocumentVersion == doc.Version}))
			}
			items = append(items, html.Label(html.Props{For: prefix + "document", Text: t("document")}), html.Select(html.Props{ID: prefix + "document"}, docs...))
		}
		for _, choice := range []struct {
			key, label string
			checked    bool
		}{{"required", "required", f.Required}, {"administrators", "admin", f.Visibility.Administrators}, {"members", "members", f.Visibility.Members}} {
			items = append(items, html.Label(html.Props{For: prefix + choice.key}, html.Input(html.Props{ID: prefix + choice.key, Type: "checkbox", Checked: choice.checked}), html.Span(html.Props{Text: t(choice.label)})))
		}
		items = append(items, html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "question-up", "up", id, false), gateButton(v, "question-down", "down", id, false), gateButton(v, "question-remove", "remove", id, false)))
		nodes = append(nodes, html.Fieldset(html.Props{Data: map[string]string{"gate-editor": id}}, items...))
	}
	nodes = append(nodes, gateButton(v, "question-add", "add", "", false), html.Fieldset(html.Props{}, html.Legend(html.Props{Text: t("rule")}), gateRuleFieldControl(v, d), html.Label(html.Props{For: "gate-rule-values", Text: t("ruleValue")}), html.Textarea(html.Props{ID: "gate-rule-values", Rows: 3}), gateEditorInput(v, "gate-rule-reason", "ruleReason", "text"), html.P(html.Props{ID: "gate-rule-preview", Text: GateRulePreview(v.Locale, d)})), gateEditorInput(v, "gate-answer-by", "date", "date"), html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "define", "apply", "", true), gateButton(v, "try", "try", "", false)), gateEditorInput(v, "gate-version", "version", "text"), html.P(html.Props{Text: t("meaning")}), html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "publish", "publish", "", true), gateButton(v, "pause", "pause", "", false), gateButton(v, "retire", "retire", "", false)), chatPolishDisclosure(html.Props{}, chatPolishDisclosureLabel(html.Props{Text: t("sample")}), renderGateApplicant(GateView{Locale: v.Locale, Gate: chatgate.Gate{Versions: []chatgate.Definition{d}, Current: d.Version.String()}, Directory: v.Directory})))
	return html.Section(html.Props{}, nodes...)
}
func gateRuleFieldControl(v GateView, d chatgate.Definition) ui.Node {
	options := []ui.Node{html.Option(html.Props{Value: "", Text: GateText(v.Locale, "choose")})}
	for _, f := range d.Fields {
		options = append(options, html.Option(html.Props{Value: f.ID, Text: f.Label}))
	}
	return html.Label(html.Props{For: "gate-rule-field"}, html.Span(html.Props{Text: GateText(v.Locale, "ruleField")}), html.Select(html.Props{ID: "gate-rule-field"}, options...))
}
func GateRulePreview(locale string, d chatgate.Definition) string {
	if len(d.Rules) == 0 {
		return GateText(locale, "preview")
	}
	rule := d.Rules[0]
	label := GateText(locale, "label")
	for _, f := range d.Fields {
		if f.ID == rule.When.Field {
			label = f.Label
		}
	}
	return fmt.Sprintf(GateText(locale, "rulePreview"), label, strings.Join(rule.When.Values, " / "))
}

// EditGateQuestion is shared by the native acceptance tests and wasm builder.
func EditGateQuestion(d chatgate.Definition, action string, index int) (chatgate.Definition, error) {
	d.Fields = append([]chatgate.Field(nil), d.Fields...)
	if action == "question-add" {
		if len(d.Fields) >= 12 {
			return d, chatgate.ErrInvalid
		}
		used := map[string]bool{}
		for _, f := range d.Fields {
			used[f.ID] = true
		}
		id := ""
		for n := 1; id == ""; n++ {
			candidate := fmt.Sprintf("question_%d", n)
			if !used[candidate] {
				id = candidate
			}
		}
		d.Fields = append(d.Fields, chatgate.Field{ID: id, Kind: "short_text", KindVersion: "1.0.0", DataClass: "INTERNAL", Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30})
		return d, nil
	}
	if index < 0 || index >= len(d.Fields) {
		return d, chatgate.ErrInvalid
	}
	switch action {
	case "question-remove":
		d.Fields = append(d.Fields[:index], d.Fields[index+1:]...)
	case "question-up":
		if index > 0 {
			d.Fields[index-1], d.Fields[index] = d.Fields[index], d.Fields[index-1]
		}
	case "question-down":
		if index+1 < len(d.Fields) {
			d.Fields[index+1], d.Fields[index] = d.Fields[index], d.Fields[index+1]
		}
	default:
		return d, chatgate.ErrInvalid
	}
	return d, nil
}
