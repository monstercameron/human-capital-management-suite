package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

type ChattoneToolbarProps struct {
	Locale, Target, Conversation, Draft, Scope string
	Styles                                     []chatrewrite.Style
	Suggestion                                 *chatrewrite.Suggestion
	Enabled, Disabled, Pending                 bool
}

func chattoneToolbar(m Model, target string, disabled bool) ui.Node {
	scope := ""
	draft := m.Draft
	if target == "thread-composer" {
		draft = ""
		scope = m.ThreadParentID
	}
	if m.ChatFeatures != nil && !m.ChatFeatures.WritingStyles {
		return chattoneUnavailable(m.Locale, target, draft, m.ChatFeatures.WritingStylesNote)
	}
	return RenderChattoneToolbar(ChattoneToolbarProps{Locale: m.Locale, Target: target, Conversation: m.SelectedID, Scope: scope, Draft: draft, Styles: chatrewrite.DefaultStyles(), Enabled: true, Disabled: disabled, Pending: true})
}

// chattoneUnavailable says plainly why the writing-style controls are not
// offered, once the draft is long enough for them to matter. The server's
// features answer carries the reason as a code ("not_qualified": no model has
// passed the quality check yet; "workspace_off": an administrator turned them
// off); an answer with no code (not yet arrived, or an older server) shows
// nothing rather than a guess.
func chattoneUnavailable(locale, target, draft, note string) ui.Node {
	switch note {
	case "not_qualified", "workspace_off", "unavailable":
	default:
		return nil
	}
	return html.P(html.Props{ID: target + "-chattone-unavailable", Class: "chattone-unavailable", Role: "status", Dir: agentReplyDirection(locale), Hidden: len(strings.Fields(draft)) < 3,
		Data: map[string]string{"chattone": "unavailable", "reason": note}, Text: ChattoneText(locale, "note_"+note)})
}

func RenderChattoneToolbar(p ChattoneToolbarProps) ui.Node {
	if !p.Enabled {
		return nil
	}
	text := func(key string) string { return ChattoneText(p.Locale, key) }
	data := map[string]string{"chattone": "toolbar", "target": p.Target, "conversation": p.Conversation, "locale": p.Locale, "scope": p.Scope, "ready": boolString(len(strings.Fields(p.Draft)) >= 3), "available": boolString(!p.Pending), "disabled": boolString(p.Disabled)}
	for _, key := range []string{"working", "unavailable", "preservation", "policy", "limit", "invalid", "denied", "disabled", "edited", "notice", "suggested", "undo", "changes", "before", "after"} {
		data["copy-"+key] = text(key)
	}
	buttons := []ui.Node{}
	for index, style := range p.Styles {
		label := style.Label
		if (style.ID == "professional" && label == "Professional") || (style.ID == "friendly" && label == "Friendly") || (style.ID == "concise" && label == "Concise") {
			label = text(style.Register)
		}
		help := text(style.Register + "_help")
		if index < 3 {
			help += " (Alt+Shift+" + itoa(index+1) + ")"
		}
		suggested := p.Suggestion != nil && p.Suggestion.StyleID == style.ID
		reason := ""
		if suggested {
			reason = text(p.Suggestion.ReasonKey)
			help += " " + text("suggested") + ": " + reason
		}
		tooltipID := p.Target + "-chattone-help-" + itoa(index+1)
		aria := map[string]string{"label": label, "describedby": tooltipID, "haspopup": "dialog", "controls": p.Target + "-chattone-options", "expanded": "false"}
		if index < 3 {
			aria["keyshortcuts"] = "Alt+Shift+" + itoa(index+1)
		}
		buttons = append(buttons, html.Span(html.Props{Class: "chattone-choice"},
			html.Button(html.Props{Type: "button", Class: "tool-button chattone-style", Disabled: p.Disabled || p.Pending, Title: label, Data: map[string]string{"chattone-action": "rewrite", "style": style.ID, "help": text(style.Register + "_help"), "index": itoa(index + 1)}, Aria: aria},
				icon("edit"), html.Span(html.Props{Class: "sr-only", Text: label}), html.Span(html.Props{Class: "chattone-suggested", Data: map[string]string{"suggested": boolString(suggested)}, Text: text("suggested")})),
			html.Span(html.Props{ID: tooltipID, Class: "chattone-tooltip", Role: "tooltip", Text: help})))
	}
	panelChildren := []ui.Node{
		html.P(html.Props{Class: "chattone-notice", Text: text("notice")}),
		html.Div(html.Props{ID: p.Target + "-chattone-status", Class: "chattone-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}),
		html.Div(html.Props{Class: "chattone-preview"}),
	}
	return html.Div(html.Props{Class: "chattone", Data: data, Dir: agentReplyDirection(p.Locale), Hidden: p.Pending},
		html.Div(html.Props{Class: "chattone-toolbar-choices", Role: "group", Aria: map[string]string{"label": text("style")}}, buttons...),
		anchoredChatLayer(html.Props{ID: p.Target + "-chattone-options", Class: "chattone-options", Hidden: true, Role: "dialog", Aria: map[string]string{"label": text("style")}}, "writing-style", panelChildren...),
	)
}

type ChattonePreviewProps struct {
	Locale, Original, Rewritten string
	ShowChanges                 bool
}

func RenderChattonePreview(p ChattonePreviewProps) ui.Node {
	if p.Original == "" {
		return nil
	}
	children := []ui.Node{
		html.Button(html.Props{Type: "button", Class: "chattone-undo", Data: map[string]string{"chattone-action": "undo"}, Text: ChattoneText(p.Locale, "undo")}),
		html.Button(html.Props{Type: "button", Data: map[string]string{"chattone-action": "changes"}, Text: ChattoneText(p.Locale, "changes"), Aria: map[string]string{"expanded": boolString(p.ShowChanges)}}),
	}
	if p.ShowChanges {
		children = append(children, html.Div(html.Props{Class: "chattone-diff", Role: "region", Aria: map[string]string{"label": ChattoneText(p.Locale, "changes")}},
			html.P(html.Props{Text: ChattoneText(p.Locale, "before")}), html.Del(html.Props{Dir: "auto", Text: p.Original}),
			html.P(html.Props{Text: ChattoneText(p.Locale, "after")}), html.Ins(html.Props{Dir: "auto", Text: p.Rewritten})))
	}
	return html.Div(html.Props{Class: "chattone-preview-actions"}, children...)
}
