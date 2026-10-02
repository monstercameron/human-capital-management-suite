package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-070: a channel's administrator can require every agent answer in the
// channel to be private. The requirement is one row of Conversation details,
// under Manage channel, in the shape of every other setting there: the label,
// the current value and, opened, one switch with the sentence that says what it
// does. The server decides who may change it; the row is drawn only for a person
// it said may.

// ChannelAgentPrivacyState is the requirement as the server last reported it for
// one conversation.
type ChannelAgentPrivacyState struct {
	// ConversationID is the conversation the state is for; a state for another
	// conversation is not shown.
	ConversationID string
	// Known is true when the server reported a requirement for the conversation: it
	// has agents and a policy to hold the requirement in.
	Known bool
	// Private is true when private answers are required.
	Private bool
	// CanChange is true for a person who manages the channel.
	CanChange bool
	// Saving is true while a change is on its way; Failed when the last one was not saved.
	Saving, Failed bool
}

const agentux070ChannelRowID = "agent-privacy"

// agentux070ChannelRow is the row, or nothing when the viewer has no setting to
// change here: no agents in the conversation, or not a manager of the channel.
func agentux070ChannelRow(m Model, h handlers, c Conversation) ui.Node {
	state := m.ChannelAgentPrivacy
	if !state.Known || !state.CanChange || state.ConversationID != m.SelectedID || c.Kind == DirectMessage || len(chatux005Agents(m)) == 0 {
		return nil
	}
	value := agentux070ChannelText(m, "value.public")
	if state.Private {
		value = agentux070ChannelText(m, "value.private")
	}
	next := "1"
	if state.Private {
		next = "0"
	}
	label := agentux070ChannelText(m, "switch")
	body := []ui.Node{
		chatux027Help(agentux070ChannelText(m, "help")),
		html.Button(html.Props{Class: "composer-private-switch agent-channel-private-switch", Type: "button", Role: "switch", Disabled: state.Saving || m.Callbacks.SetChannelAgentPrivacy == nil,
			Data: map[string]string{"action": "agent-channel-private", "id": next}, Aria: map[string]string{"checked": boolString(state.Private), "busy": boolString(state.Saving)}, Text: label}),
	}
	if state.Saving {
		body = append(body, html.P(html.Props{Class: "manage-sec-help", Role: "status", Text: agentux070ChannelText(m, "saving")}))
	}
	if state.Failed {
		body = append(body, html.P(html.Props{Class: "manage-sec-error", Role: "alert", Text: agentux070ChannelText(m, "failed")}))
	}
	open := chatux027IsOpen(h.local, chatux027ManageGroup, agentux070ChannelRowID, state.Failed)
	return chatux027Section(m, sectionSpec{ID: agentux070ChannelRowID, Group: chatux027ManageGroup, Class: "manage-agent-privacy", Label: agentux070ChannelText(m, "label"),
		Value: ui.Text(value), Body: html.Div(html.Props{Class: "manage-sec-form"}, body...), Open: open, Failed: state.Failed})
}

// agentux070ChannelPrivacyClick turns the requirement on or off. It reports
// whether the press was on the switch.
func agentux070ChannelPrivacyClick(e ui.MouseEvent, m Model) bool {
	action, id, _ := eventAction(e)
	if action != "agent-channel-private" {
		return false
	}
	e.PreventDefault()
	agentux070ChannelChoice(m, id)
	return true
}

// agentux070ChannelChoice applies one press of the switch: "1" requires private
// answers and "0" stops requiring them. It reports whether the choice was passed
// on; one that cannot do anything now (a save under way, a person who may not
// change it) is dropped.
func agentux070ChannelChoice(m Model, choice string) bool {
	state := m.ChannelAgentPrivacy
	if m.Callbacks.SetChannelAgentPrivacy == nil || !state.Known || !state.CanChange || state.Saving || (choice != "1" && choice != "0") {
		return false
	}
	m.Callbacks.SetChannelAgentPrivacy(choice == "1")
	return true
}

func agentux070ChannelText(m Model, key string) string {
	copy := map[string][3]string{
		"group":         {"Agents", "Agenten", "الوكلاء"},
		"label":         {"Agent answers", "Antworten von Agenten", "إجابات الوكلاء"},
		"value.public":  {"Visible to the channel", "Für den Kanal sichtbar", "مرئية للقناة"},
		"value.private": {"Must be private", "Müssen privat sein", "يجب أن تكون خاصة"},
		"switch":        {"Agent answers here must be private", "Antworten von Agenten hier müssen privat sein", "يجب أن تكون إجابات الوكلاء هنا خاصة"},
		"help":          {"When this is on, an agent's answer here is shown only to the person who asked, whatever the agent or the question says. When it is off, answers are shown to the channel unless the agent, the question or the sources keep them private.", "Wenn dies aktiv ist, sieht nur die fragende Person die Antwort eines Agenten hier, unabhängig vom Agenten oder von der Frage. Wenn nicht, sehen alle im Kanal die Antworten, sofern Agent, Frage oder Quellen sie nicht privat halten.", "عند تفعيل هذا الخيار، تظهر إجابة الوكيل هنا لمن طرح السؤال فقط، مهما كان الوكيل أو السؤال. وعند إيقافه تظهر الإجابات للقناة ما لم يبقها الوكيل أو السؤال أو المصادر خاصة."},
		"saving":        {"Saving…", "Wird gespeichert…", "جارٍ الحفظ…"},
		"failed":        {"Could not save this setting. Try again.", "Diese Einstellung konnte nicht gespeichert werden. Versuchen Sie es erneut.", "تعذر حفظ هذا الإعداد. حاول مرة أخرى."},
	}
	return chatbug039Text("agentux070.channel."+key, copy[key][chatbug039LocaleIndex(m.Locale)], copy[key][0])
}
