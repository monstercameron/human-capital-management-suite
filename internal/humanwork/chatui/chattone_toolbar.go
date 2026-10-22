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
	return html.Div(html.Props{Class: "chattone", Data: data, Dir: agentReplyDirection(p.Locale), Hidden: p.Pending},
		html.Div(html.Props{Class: "chattone-toolbar-choices"}, chattoneTrigger(p, text)),
		chattoneMenu(p, text),
	)
}

// ChattonePreviewProps is what the line above the composer's text says about an
// applied rewrite: the style it used, the text as typed and the text now in the
// field (which the person may have edited since).
type ChattonePreviewProps struct {
	Locale, Target, StyleName, Original, Rewritten string
	// Edited is set once the field no longer holds what the model wrote.
	Edited bool
}

// RenderChattonePreview is the line that stays above the composer's text after a
// rewrite is applied: "Rewritten: Professional", the Undo and See changes text
// buttons, and the layer See changes opens (the text as typed beside the text
// now in the field). It stays until the draft is sent, undone or cleared. The
// layer is closed when drawn; the browser opens it against its button.
func RenderChattonePreview(p ChattonePreviewProps) ui.Node {
	if p.Original == "" {
		return nil
	}
	text := func(key string) string { return ChattoneText(p.Locale, key) }
	label := []ui.Node{icon("sparkles"), html.Span(html.Props{Class: "chattone-chip-text", Text: text("rewritten")})}
	if p.StyleName != "" {
		label = append(label, html.Span(html.Props{Class: "chattone-chip-style", Dir: "auto", Text: p.StyleName}))
	}
	if p.Edited {
		label = append(label, html.Span(html.Props{Class: "chattone-chip-edited", Text: text("edited_tag")}))
	}
	diffID := p.Target + "-chattone-options"
	return html.Div(html.Props{Class: "chattone-chip", Dir: agentReplyDirection(p.Locale), Role: "group", Aria: map[string]string{"label": text("rewrite")}},
		html.Span(html.Props{Class: "chattone-chip-label"}, label...),
		html.Button(html.Props{Type: "button", Class: "chattone-chip-action chattone-undo", Data: map[string]string{"chattone-action": "undo"}, Text: text("undo_chip"), Aria: map[string]string{"label": text("undo")}}),
		html.Button(html.Props{Type: "button", Class: "chattone-chip-action", Data: map[string]string{"chattone-action": "changes"}, Text: text("changes"), Aria: map[string]string{"expanded": "false", "haspopup": "dialog", "controls": diffID}}),
		anchoredChatLayer(html.Props{ID: diffID, Class: "chattone-options", Hidden: true, Role: "dialog", Data: map[string]string{"chattone-diff": "true", "chattone-for": p.Target}, Aria: map[string]string{"label": text("changes")}}, "writing-style",
			html.Div(html.Props{Class: "chattone-diff", Role: "region", Aria: map[string]string{"label": text("changes")}},
				html.P(html.Props{Text: text("before")}), html.Del(html.Props{Dir: "auto", Text: p.Original}),
				html.P(html.Props{Text: text("after")}), html.Ins(html.Props{Dir: "auto", Text: p.Rewritten}))))
}

// chattoneChipSlot is where the browser draws what a rewrite leaves behind, in
// the composer directly above the text: the status line (working, or why a
// rewrite did not happen) and the line RenderChattonePreview draws. It is empty,
// and takes no room, until there is something to say. The status is a live
// region, so it exists before it has words.
func chattoneChipSlot(m Model, target string) ui.Node {
	if m.ChatFeatures != nil && !m.ChatFeatures.WritingStyles {
		return nil
	}
	return html.Div(html.Props{ID: target + "-chattone-chip", Class: "chattone-chip-slot", Dir: agentReplyDirection(m.Locale), Data: map[string]string{"chattone-chip": target}},
		html.P(html.Props{ID: target + "-chattone-status", Class: "chattone-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}),
		html.Div(html.Props{Class: "chattone-chip-region"}))
}
