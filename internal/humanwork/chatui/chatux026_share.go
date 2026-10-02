package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-026: making a private answer public is the one thing on the card a
// person cannot quietly take back, and it used to happen on one press. The
// first press now asks: it says what is posted, who will be able to read it and
// where it goes, with Share and Cancel. Once shared, the card stops calling
// itself private: its header reads "Shared with #channel", and its action row
// leads to the shared copy and lets the person who asked remove it, which
// returns the card to private.

// chatux026Shared reports whether the answer of this run is now a message in
// the channel (or is being taken back from it).
func chatux026Shared(model Model, invocationID string) bool {
	status := model.AgentShare[invocationID].Status
	return invocationID != "" && (status == AgentShareShared || status == AgentShareRemoving)
}

// chatux026Asking reports whether the card of this run is asking whether to
// share. A request already on its way, a shared answer and a refusal have
// nothing left to ask.
func chatux026Asking(model Model, local localUI, invocationID string) bool {
	if invocationID == "" || local.agentShareAsk != invocationID || model.Callbacks.ShareAgentAnswer == nil {
		return false
	}
	switch model.AgentShare[invocationID].Status {
	case AgentShareSharing, AgentShareShared, AgentShareRemoving, AgentShareRefused:
		return false
	}
	return true
}

// chatux026Visibility is the note at the end of the card's header and how the
// stylesheet and tests name it: who can see the answer.
func chatux026Visibility(model Model, invocationID string) (note, visibility string) {
	if chatux026Shared(model, invocationID) {
		return chatux026Format(model, "shared_with", map[string]string{"channel": chatux003ChannelName(model)}), "shared"
	}
	return personaProgressText(model, "chat.agent.only_visible", "Only visible to you"), "private"
}

// chatux026Audience says who will be able to read a shared answer: everyone in
// the channel, with the number of people when the page knows it.
func chatux026Audience(model Model) string {
	values := map[string]string{"channel": chatux003ChannelName(model)}
	switch count := model.selected().MemberCount; {
	case count == 1:
		return chatux026Format(model, "ask.who.one", values)
	case count > 1:
		values["n"] = model.n(count)
		return chatux026Format(model, "ask.who.many", values)
	}
	return chatux026Format(model, "ask.who", values)
}

// chatux026Confirm is the question the first press of "Share to channel" asks.
// It takes the whole width of the action row, under the other actions.
func chatux026Confirm(model Model, invocationID string) ui.Node {
	title := chatux026Text(model, "ask.title")
	fact := func(label, value string) []ui.Node {
		return []ui.Node{html.Tag("dt", html.Props{Text: chatux026Text(model, label)}), html.Tag("dd", html.Props{Dir: "auto", Text: value})}
	}
	facts := fact("ask.what.label", chatux026Text(model, "ask.what"))
	facts = append(facts, fact("ask.who.label", chatux026Audience(model))...)
	facts = append(facts, fact("ask.where.label", chatux026Text(model, "ask.where"))...)
	return html.Div(html.Props{Class: "agent-share-confirm", Role: "group", Aria: map[string]string{"label": title}, Data: map[string]string{"agent-share-ask": invocationID}},
		html.P(html.Props{Class: "agent-share-confirm-title", Role: "status", Text: title}),
		html.Tag("dl", html.Props{}, facts...),
		html.Div(html.Props{Class: "agent-share-confirm-actions"},
			html.Button(html.Props{Class: "button small agent-share-go", Type: "button", Data: map[string]string{"action": "agent-share-confirm", "id": invocationID}, Text: chatux026Text(model, "ask.share")}),
			html.Button(html.Props{Class: "button secondary small agent-share-cancel", Type: "button", Data: map[string]string{"action": "agent-share-cancel", "id": invocationID}, Text: agentReplyFallback(model.Locale, KeyCancel, "Cancel")})))
}

// chatux026ShareControls is what the action row holds for sharing: the control
// to share, the question it asks, what came of it, or, once shared, the way to
// the shared copy and the way to remove it.
func chatux026ShareControls(model Model, local localUI, reason, name, invocationID, threadID string) []ui.Node {
	if invocationID == "" || model.selected().Kind != PublicChannel {
		return nil
	}
	state := model.AgentShare[invocationID]
	if chatux026Shared(model, invocationID) {
		// CHATUX-028: the way to the shared copy and the way to take it back are
		// items of the card's "…" menu, not buttons of the row; the row says only
		// what is happening to the copy.
		var nodes []ui.Node
		note := func(key string) ui.Node {
			return html.Span(html.Props{Class: "agent-reply-share-note", Role: "status", Dir: "auto", Text: chatux026Text(model, key)})
		}
		if state.Status == AgentShareRemoving {
			nodes = append(nodes, note("removing"))
		}
		if state.RemoveFailed {
			nodes = append(nodes, note("remove.failed"))
		}
		return nodes
	}
	if chatux026Asking(model, local, invocationID) {
		return []ui.Node{chatux026Confirm(model, invocationID)}
	}
	return chatux003ShareControls(model, reason, name, invocationID)
}

// chatux026SharedMenuItems are the items a shared answer adds to the card's "…"
// menu (CHATUX-028): the way to the shared copy, and, for the person who asked,
// the way to take it back. Nothing for an answer that is not shared.
func chatux026SharedMenuItems(model Model, invocationID, threadID string) []ui.Node {
	if invocationID == "" || model.selected().Kind != PublicChannel || !chatux026Shared(model, invocationID) {
		return nil
	}
	state := model.AgentShare[invocationID]
	view := chatux026Text(model, "view")
	items := []ui.Node{html.Button(html.Props{Class: "menu-item agent-share-view", Type: "button", Role: "menuitem", Disabled: model.Callbacks.OpenThread == nil || threadID == "", Data: map[string]string{"action": "agent-share-view", "id": threadID, "extra": state.PostID}},
		icon("reply"), html.Span(html.Props{Text: view}))}
	// The card is drawn for the person who asked and nobody else, so the one who
	// may remove the shared copy is the one reading it.
	if model.Callbacks.RemoveSharedAgentAnswer != nil && state.PostID != "" {
		removing := state.Status == AgentShareRemoving
		label := chatux026Text(model, "remove")
		if removing {
			label = chatux026Text(model, "removing")
		}
		items = append(items, html.Button(html.Props{Class: "menu-item agent-share-remove", Type: "button", Role: "menuitem", Disabled: removing, Data: map[string]string{"action": "agent-share-remove", "id": invocationID}, Aria: map[string]string{"busy": boolString(removing)}},
			icon("trash"), html.Span(html.Props{Text: label})))
	}
	return items
}

// chatux026Step decides what one press on a share control does. asking is the
// run whose card is asking now. It returns the run that asks afterwards and the
// effect to carry out: "share" (send the request), "view" (open the thread at
// the copy), "remove" (take the copy back), "ask" or "cancel" (only the
// question changes) or "" (the press is not a share control). A press that
// cannot do anything now is taken and has no effect.
func chatux026Step(model Model, asking, action, id string) (next, effect string, taken bool) {
	state := model.AgentShare[id]
	switch action {
	case "agent-share":
		if id == "" || model.Callbacks.ShareAgentAnswer == nil || state.Status == AgentShareSharing || state.Status == AgentShareRefused || chatux026Shared(model, id) {
			return asking, "", true
		}
		return id, "ask", true
	case "agent-share-cancel":
		return "", "cancel", true
	case "agent-share-confirm":
		if id == "" || asking != id {
			// A confirmation with no question before it shares nothing.
			return "", "", true
		}
		return "", "share", true
	case "agent-share-view":
		if id == "" || model.Callbacks.OpenThread == nil {
			return asking, "", true
		}
		return asking, "view", true
	case "agent-share-remove":
		if state.Status != AgentShareShared || state.PostID == "" || model.Callbacks.RemoveSharedAgentAnswer == nil {
			return asking, "", true
		}
		return asking, "remove", true
	}
	return asking, "", false
}

// chatux026HandleAction applies chatux026Step to the workspace for one press,
// and reports whether the press was a share control.
func chatux026HandleAction(e ui.Event, model Model, local localStore) bool {
	action, id, _ := eventAction(e)
	asking := local.get().agentShareAsk
	next, effect, taken := chatux026Step(model, asking, action, id)
	if !taken {
		return false
	}
	e.PreventDefault()
	if next != asking {
		local.update(func(u *localUI) { u.agentShareAsk = next })
	}
	switch effect {
	case "ask":
		// The pressed item or mark is replaced by the question: the caret goes to
		// Cancel, the answer that changes nothing. A menu it came from closes.
		chatux028CloseMenu(model)
		chatux026Focus(`.agent-share-confirm [data-action="agent-share-cancel"]`)
	case "cancel":
		chatux026Focus(`[data-action="agent-share"][data-id="` + chatux026Quoted(id) + `"]`)
	case "share":
		chatux003ShareClick(model, local.get(), id)
	case "view":
		chatux028CloseMenu(model)
		model.Callbacks.OpenThread(id)
	case "remove":
		chatux028CloseMenu(model)
		model.Callbacks.RemoveSharedAgentAnswer(id)
	}
	return true
}

// chatux026Text is the wording of the question and of the shared card, in the
// three languages the product speaks. {channel} is the conversation the answer
// was asked in and {n} the number of people in it.
func chatux026Text(model Model, key string) string {
	copy := map[string][3]string{
		"shared_with":     {"Shared with {channel}", "Geteilt mit {channel}", "تمت المشاركة مع {channel}"},
		"view":            {"View shared answer", "Geteilte Antwort ansehen", "عرض الإجابة المشارَكة"},
		"remove":          {"Remove shared answer", "Geteilte Antwort entfernen", "إزالة الإجابة المشارَكة"},
		"removing":        {"Removing…", "Wird entfernt…", "جارٍ الإزالة…"},
		"remove.failed":   {"Could not remove the shared answer. Try again.", "Die geteilte Antwort konnte nicht entfernt werden. Versuchen Sie es erneut.", "تعذرت إزالة الإجابة المشارَكة. حاول مرة أخرى."},
		"ask.title":       {"Share this answer with the channel?", "Diese Antwort mit dem Kanal teilen?", "هل تريد مشاركة هذه الإجابة مع القناة؟"},
		"ask.what.label":  {"What is posted", "Was veröffentlicht wird", "ما الذي سيُنشر"},
		"ask.what":        {"This answer and its sources", "Diese Antwort und ihre Quellen", "هذه الإجابة ومصادرها"},
		"ask.who.label":   {"Who can read it", "Wer es lesen kann", "من يمكنه قراءتها"},
		"ask.who":         {"Everyone in {channel}", "Alle in {channel}", "الجميع في {channel}"},
		"ask.who.one":     {"Everyone in {channel}, 1 person", "Alle in {channel}, 1 Person", "الجميع في {channel}، عضو واحد"},
		"ask.who.many":    {"Everyone in {channel}, {n} people", "Alle in {channel}, {n} Personen", "الجميع في {channel}، {n} من الأعضاء"},
		"ask.where.label": {"Where", "Wo", "أين"},
		"ask.where":       {"Under your question, as a message from you", "Unter Ihrer Frage, als Nachricht von Ihnen", "تحت سؤالك، كرسالة منك"},
		"ask.share":       {"Share", "Teilen", "مشاركة"},
	}
	return chatbug039Text(key, copy[key][chatbug039LocaleIndex(model.Locale)], copy[key][0])
}

func chatux026Format(model Model, key string, values map[string]string) string {
	text := chatux026Text(model, key)
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

// chatux026Quoted makes an id safe inside a quoted attribute selector.
func chatux026Quoted(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

// chatUX026Styles lays the question out as a quiet block across the action row.
const chatUX026Styles = `.agent-share-confirm{flex:1 0 100%;box-sizing:border-box;display:grid;gap:8px;min-width:0;padding:10px 12px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--soft)}` +
	`.agent-share-confirm-title{margin:0;font-size:.875rem;font-weight:600}` +
	`.agent-share-confirm dl{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:2px 12px;margin:0;font-size:.8125rem;line-height:1.4}` +
	`.agent-share-confirm dt{color:var(--muted)}.agent-share-confirm dd{margin:0;min-width:0;overflow-wrap:anywhere}` +
	`.agent-share-confirm-actions{display:flex;flex-wrap:wrap;gap:8px}` +
	`@media(max-width:480px){.agent-share-confirm dl{grid-template-columns:minmax(0,1fr)}.agent-share-confirm dd{margin-block-end:4px}}`
