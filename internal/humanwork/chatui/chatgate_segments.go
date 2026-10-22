package chatui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// CHATGATE-009, 011 and 013 as the gate page draws them: the rules that turn an
// answer into a label or a segment, who has them, the choices that let content
// be addressed to a segment, how the gate is running, and what a person reads of
// their own labels and segments. They are sections of the gate's own page and
// use its builder, buttons and fields; the details panel's rows are drawn by
// chatux027_section.go and the gate page does not draw those.

// GateSegmentRows is how many rule rows the editor shows beyond the rules that
// exist: room to add, so adding a rule is never a separate step.
const GateSegmentRows = 2

// gateLiveDefinition is the version in force, or the first published one.
func gateLiveDefinition(v GateView) chatgate.Definition {
	for _, d := range v.Gate.Versions {
		if d.Version.String() == v.Gate.Current {
			return d
		}
	}
	return chatgate.Definition{}
}

// GateSegmentFields are the questions a rule may use: shown to members and naming
// the segments consumer, of a kind with a fixed set of answers.
func GateSegmentFields(v GateView) []chatgate.Field {
	out := []chatgate.Field{}
	for _, f := range gateLiveDefinition(v).Fields {
		usable := false
		for _, kind := range []string{"single_choice", "multiple_choice", "boolean", "team", "location"} {
			usable = usable || f.Kind == kind
		}
		consumer := false
		for _, id := range f.Visibility.Consumers {
			consumer = consumer || id == chatgate.ConsumerSegments
		}
		if usable && f.Visibility.Members && consumer {
			out = append(out, f)
		}
	}
	return out
}

// GateDuration says a wait in the largest whole unit that reads plainly.
func GateDuration(locale string, d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf(GateText(locale, "days"), int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf(GateText(locale, "hours"), int(d/time.Hour))
	default:
		return fmt.Sprintf(GateText(locale, "minutes"), int(d/time.Minute))
	}
}

func gateSegmentInput(v GateView, id, label, value string) ui.Node {
	return html.Label(html.Props{For: id}, html.Span(html.Props{Text: GateText(v.Locale, label)}), html.Input(html.Props{ID: id, Type: "text", Value: value}))
}

// renderGateSegments is the administrator's block of rules, counts and levels.
func renderGateSegments(v GateView) ui.Node {
	if v.Segments == nil {
		return nil
	}
	t := func(k string) string { return GateText(v.Locale, k) }
	nodes := []ui.Node{html.H2(html.Props{Text: t("segTitle")}), html.P(html.Props{Text: t("segIntro")}), html.P(html.Props{Class: "chatgate-hint", Text: t("segHint")})}
	fields := GateSegmentFields(v)
	if len(fields) == 0 {
		nodes = append(nodes, html.P(html.Props{Role: "status", ID: "gate-seg-nofield", Text: t("segNoField")}))
	} else {
		rules := v.Segments.Rules.Rules
		for i := 0; i < len(rules)+GateSegmentRows; i++ {
			var rule chatgate.SegmentRule
			if i < len(rules) {
				rule = rules[i]
			}
			prefix := "gate-seg-" + strconv.Itoa(i) + "-"
			options := []ui.Node{html.Option(html.Props{Value: "", Text: t("choose")})}
			for _, f := range fields {
				options = append(options, html.Option(html.Props{Value: f.ID, Text: f.Label, Selected: f.ID == rule.Field}))
			}
			nodes = append(nodes, html.Fieldset(html.Props{Data: map[string]string{"gate-segment-rule": strconv.Itoa(i)}},
				html.Legend(html.Props{Text: fmt.Sprintf("%s %d", t("segTitle"), i+1)}),
				html.Label(html.Props{For: prefix + "field"}, html.Span(html.Props{Text: t("segQuestion")}), html.Select(html.Props{ID: prefix + "field"}, options...)),
				gateSegmentInput(v, prefix+"value", "segAnswer", rule.Value),
				gateSegmentInput(v, prefix+"label", "segLabel", rule.Label),
				gateSegmentInput(v, prefix+"segment", "segSegment", rule.Segment)))
		}
		nodes = append(nodes, html.P(html.Props{Class: "chatgate-hint", Text: t("segBlankRule")}), gateSegmentInput(v, "gate-seg-reason", "segReason", ""), html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "segments-save", "segSave", "", true)))
	}
	if len(v.Segments.Rules.Rules) == 0 {
		nodes = append(nodes, html.P(html.Props{ID: "gate-seg-empty", Text: t("segEmpty")}))
	}
	// Who is in them: counts only.
	counts := []ui.Node{html.H3(html.Props{Text: t("segCounts")})}
	for _, group := range [][]chatgate.SegmentSummary{v.Segments.Labels, v.Segments.Segments} {
		for _, s := range group {
			counts = append(counts, html.P(html.Props{Data: map[string]string{"gate-segment-count": s.Name}, Text: s.Name + " · " + fmt.Sprintf(t("segPeople"), s.People)}))
		}
	}
	if len(counts) > 1 {
		nodes = append(nodes, html.Section(html.Props{}, counts...))
	}
	// The history of the rules, newest first, each version one step back.
	history := append([]chatgate.SegmentRuleSet(nil), v.Segments.History...)
	sort.Slice(history, func(i, j int) bool { return history[i].Version > history[j].Version })
	for _, old := range history {
		nodes = append(nodes, html.Div(html.Props{Class: "chatgate-actions", Data: map[string]string{"gate-segment-version": strconv.Itoa(old.Version)}},
			html.P(html.Props{Text: fmt.Sprintf(t("segVersion"), old.Version, old.Reason)}),
			gateButton(v, "segments-revert", "segUndo", strconv.Itoa(old.Version), false)))
	}
	// The two levels of addressing, off until an administrator turns them on.
	levels := []ui.Node{html.H3(html.Props{Text: t("segLevels")}),
		html.Label(html.Props{For: "gate-levels-address"}, html.Input(html.Props{ID: "gate-levels-address", Type: "checkbox", Checked: v.Segments.Addressing}), html.Span(html.Props{Text: t("segAddress")})),
		html.Label(html.Props{For: "gate-levels-restrict"}, html.Input(html.Props{ID: "gate-levels-restrict", Type: "checkbox", Checked: v.Segments.Restricted}), html.Span(html.Props{Text: t("segRestrict")})),
		html.P(html.Props{Class: "chatgate-hint", Text: t("segRestrictN")})}
	if v.RestrictedMessages > 0 {
		levels = append(levels, html.P(html.Props{Role: "status", ID: "gate-restricted-count", Text: fmt.Sprintf(t("segRestricted"), v.RestrictedMessages)}))
	}
	levels = append(levels, html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "levels-save", "segLevelsSave", "", false)))
	nodes = append(nodes, html.Section(html.Props{}, levels...))
	return html.Section(html.Props{Data: map[string]string{"gate-segments": "true"}}, nodes...)
}

// renderGateOperating is how the gate is running: the metrics, the settings for a
// queue that waits too long, and the people who were in the channel before it.
func renderGateOperating(v GateView) ui.Node {
	if v.Metrics == nil && v.Ops == nil {
		return nil
	}
	t := func(k string) string { return GateText(v.Locale, k) }
	nodes := []ui.Node{}
	if m := v.Metrics; m != nil {
		nodes = append(nodes, html.H2(html.Props{Text: t("metTitle")}),
			html.P(html.Props{Text: fmt.Sprintf(t("metStarted"), m.Started)}), html.P(html.Props{Text: fmt.Sprintf(t("metSubmitted"), m.Submitted)}),
			html.P(html.Props{Text: fmt.Sprintf(t("metAdmitted"), m.Admitted)}), html.P(html.Props{Text: fmt.Sprintf(t("metDeclined"), m.Declined)}), html.P(html.Props{Text: fmt.Sprintf(t("metWaiting"), m.Waiting)}))
		if m.MedianDecision > 0 {
			nodes = append(nodes, html.P(html.Props{Text: fmt.Sprintf(t("metMedian"), GateDuration(v.Locale, m.MedianDecision))}))
		}
		if m.OldestWaiting > 0 {
			nodes = append(nodes, html.P(html.Props{Text: fmt.Sprintf(t("metOldest"), GateDuration(v.Locale, m.OldestWaiting))}))
		}
		labels := map[string]string{}
		for _, f := range gateDefinition(v).Fields {
			labels[f.ID] = f.Label
		}
		for _, q := range m.DropOff {
			if q.Dropped > 0 && labels[q.Field] != "" {
				nodes = append(nodes, html.P(html.Props{Class: "chatgate-hint", Text: fmt.Sprintf(t("metDrop"), labels[q.Field], q.Dropped)}))
			}
		}
	}
	if o := v.Ops; o != nil {
		review, escalate := "", ""
		if o.ReviewAfter > 0 {
			review, escalate = strconv.Itoa(int(o.ReviewAfter/time.Hour)), strconv.Itoa(int(o.EscalateAfter/time.Hour))
		}
		nodes = append(nodes, html.H2(html.Props{Text: t("opsTitle")}),
			html.Label(html.Props{For: "gate-ops-review"}, html.Span(html.Props{Text: t("opsReview")}), html.Input(html.Props{ID: "gate-ops-review", Type: "number", Value: review})),
			html.Label(html.Props{For: "gate-ops-escalate"}, html.Span(html.Props{Text: t("opsEscalate")}), html.Input(html.Props{ID: "gate-ops-escalate", Type: "number", Value: escalate})),
			html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "ops-save", "opsSave", "", false)))
		by := ""
		if !o.Existing.AnswerBy.IsZero() {
			by = o.Existing.AnswerBy.Format("2006-01-02")
		}
		nodes = append(nodes, html.H3(html.Props{Text: t("exTitle")}), html.P(html.Props{Class: "chatgate-hint", Text: t("exStays")}),
			html.Label(html.Props{For: "gate-existing-mode"}, html.Span(html.Props{Text: t("exTitle")}), html.Select(html.Props{ID: "gate-existing-mode"},
				html.Option(html.Props{Value: chatgate.ExistingNewOnly, Text: t("exNew"), Selected: o.Existing.Mode != chatgate.ExistingAsk}),
				html.Option(html.Props{Value: chatgate.ExistingAsk, Text: t("exAsk"), Selected: o.Existing.Mode == chatgate.ExistingAsk}))),
			html.Label(html.Props{For: "gate-existing-by"}, html.Span(html.Props{Text: t("exBy")}), html.Input(html.Props{ID: "gate-existing-by", Type: "date", Value: by})),
			html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "existing-save", "exSave", "", false)))
		nodes = append(nodes, renderGateOverride(v)...)
		if len(v.Unanswered) > 0 && o.Existing.Mode == chatgate.ExistingAsk {
			people := []ui.Node{}
			for _, p := range v.Unanswered {
				name := v.Names[p]
				if name == "" {
					name = p
				}
				people = append(people, html.Li(html.Props{Text: name}))
			}
			nodes = append(nodes, html.P(html.Props{Role: "status", ID: "gate-unanswered", Text: fmt.Sprintf(t("exUnanswered"), len(v.Unanswered))}), html.Ul(html.Props{}, people...),
				html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "remove-unanswered", "exRemove", "", false)))
		}
	}
	return html.Section(html.Props{Data: map[string]string{"gate-operating": "true"}}, nodes...)
}

// renderGateMySegments is a person's own labels and segments with the reason for
// each, and the plain statement that a label is a name and nothing more.
func renderGateMySegments(v GateView) ui.Node {
	admitted := v.Submission != nil && v.Submission.Status == "admitted"
	if len(v.MySegments) == 0 && !admitted {
		return nil
	}
	t := func(k string) string { return GateText(v.Locale, k) }
	nodes := []ui.Node{html.H3(html.Props{Text: t("mySegs")})}
	if len(v.MySegments) == 0 {
		nodes = append(nodes, html.P(html.Props{Text: t("mySegsNone")}))
	}
	for _, p := range v.MySegments {
		why := t("mySegsAgent")
		if p.Source == "" {
			why = fmt.Sprintf(t("mySegsWhy"), p.Value, p.FieldLabel)
		}
		nodes = append(nodes, html.P(html.Props{Data: map[string]string{"gate-placement": p.Kind}, Text: p.Name + " · " + why}))
	}
	nodes = append(nodes, html.P(html.Props{Class: "chatgate-hint", Text: t("mySegsNote")}))
	return html.Section(html.Props{Data: map[string]string{"gate-my-segments": "true"}}, nodes...)
}

// gateReaderName is how the list of who has read an answer names a reader: a
// person by name, a consumer or an agent by what it is.
func gateReaderName(v GateView, a chatgate.ReadAudit) string {
	t := func(k string) string { return GateText(v.Locale, k) }
	switch {
	case a.Consumer == chatgate.ConsumerSegments:
		return t("readSegments")
	case strings.HasPrefix(a.Consumer, chatgate.ConsumerAgent+":"):
		return t("readAgent")
	}
	if name := v.Names[a.Reader]; name != "" {
		return name
	}
	return t("admin")
}

// renderGateOverride is the administrator's way to add somebody who has not
// answered, for a person invited into the channel first. It is the gate's own
// audited override, named plainly, and asks for a reason.
func renderGateOverride(v GateView) []ui.Node {
	t := func(k string) string { return GateText(v.Locale, k) }
	people := []ui.Node{html.Option(html.Props{Value: "", Text: t("choose")})}
	for _, p := range v.Directory["person"] {
		people = append(people, html.Option(html.Props{Value: p.ID, Text: p.Label}))
	}
	return []ui.Node{html.H3(html.Props{Text: t("ovTitle")}), html.P(html.Props{Class: "chatgate-hint", Text: t("ovHint")}),
		html.Label(html.Props{For: "gate-override-person"}, html.Span(html.Props{Text: t("ovPerson")}), html.Select(html.Props{ID: "gate-override-person"}, people...)),
		gateSegmentInput(v, "gate-override-reason", "ovReason", ""),
		html.Div(html.Props{Class: "chatgate-actions"}, gateButton(v, "override", "ovSave", "", false))}
}
