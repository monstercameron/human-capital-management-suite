package chatui

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func unresolvedAgent(model Model, body string) (ResolvedPersonaMention, int, int, bool) {
	if model.selected().Agent {
		return ResolvedPersonaMention{}, 0, 0, false
	}
	for start, r := range body {
		previous, _ := utf8.DecodeLastRuneInString(body[:start])
		if r != '@' || (start > 0 && !unicode.IsSpace(previous)) {
			continue
		}
		rest := body[start+1:]
		end := start + 1
		for _, letter := range rest {
			if !unicode.IsLetter(letter) {
				break
			}
			end += utf8.RuneLen(letter)
		}
		if end == start+1 {
			continue
		}
		query := body[start+1 : end]
		exact := false
		for _, person := range mentionIndex(model) {
			exact = exact || mentionNameAt(rest, person.name)
		}
		best, score := ResolvedPersonaMention{}, 3
		for _, candidate := range model.ResolvedPersonaMentions {
			if !validResolvedPersonaReference(candidate.Reference, model.SelectedID) || candidate.Reference.TenantID != model.CurrentTenantID {
				continue
			}
			exact = exact || mentionNameAt(rest, candidate.Reference.Display) || mentionNameAt(rest, strings.TrimPrefix(candidate.Handle, "@"))
			next := mentionTextScore(query, candidate.Reference.Display, candidate.Handle, candidate.Reference.ID)
			if next < score {
				best, score = candidate, next
			}
		}
		if !exact && score < 3 {
			return best, start, end, true
		}
	}
	return ResolvedPersonaMention{}, 0, 0, false
}

func mentionNameAt(rest, name string) bool {
	if name == "" || len(rest) < len(name) || !strings.EqualFold(rest[:len(name)], name) {
		return false
	}
	if len(rest) == len(name) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(rest[len(name):])
	return !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_' && next != '-'
}

func composerUnresolvedMention(model Model, selectedName string) ui.Node {
	if selectedName != "" {
		return nil
	}
	agent, _, _, ok := unresolvedAgent(model, model.Draft)
	if !ok {
		return nil
	}
	return html.Div(html.Props{Class: "composer-unresolved-mention", Role: "status"},
		html.Button(html.Props{Class: "button secondary", Type: "button", Data: map[string]string{"action": "agent-suggest-mention", "id": agent.Reference.ID}, Text: agentUXChat4Format(model, "chat.agent.suggest_mention", map[string]string{"name": agent.Reference.Display})}),
		html.P(html.Props{Text: agentUXChat4Text(model, "chat.agent.unresolved_plain")}))
}

func unresolvedMessageRecovery(model Model, message Message) ui.Node {
	if model.CurrentUser == "" || message.AuthorID != model.CurrentUser || len(message.PersonaReferences) > 0 || !chatux006HintNow(model, message) {
		return nil
	}
	agent, start, end, ok := unresolvedAgent(model, message.Body)
	if !ok || !uniqueUnresolvedAgent(model, message.Body[start+1:end]) {
		return nil
	}
	return html.Div(html.Props{Class: "composer-unresolved-mention", Data: map[string]string{"recipient-only": "true"}},
		html.P(html.Props{Text: agentUXChat4Format(model, "chat.agent.not_mentioned", map[string]string{"name": agent.Reference.Display})}),
		html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: model.Callbacks.SendMessageWithReferences == nil, Data: map[string]string{"action": "agent-suggest-mention", "id": agent.Reference.ID, "extra": message.ID}, Text: agentUXChat4Format(model, "chat.agent.ask_named", map[string]string{"name": agent.Reference.Display})}))
}

func selectUnresolvedAgent(model Model, mentions mentionStore, id, postID string) {
	body := domValue("chat-composer")
	if postID != "" {
		body = ""
		for _, post := range model.Messages {
			if post.ID == postID && post.AuthorID == model.CurrentUser && len(post.PersonaReferences) == 0 {
				body = post.Body
				break
			}
		}
	}
	agent, start, end, ok := unresolvedAgent(model, body)
	if !ok || agent.Reference.ID != id {
		return
	}
	question := strings.TrimSpace(body[:start] + strings.TrimLeftFunc(body[end:], unicode.IsSpace))
	if postID != "" {
		if !uniqueUnresolvedAgent(model, body[start+1:end]) {
			return
		}
		if question != "" && model.Callbacks.SendMessageWithReferences != nil {
			model.Callbacks.SendMessageWithReferences(model.SelectedID, bodyWithAgentMentions(question, []ChatReference{agent.Reference}), []ChatReference{agent.Reference})
		}
		return
	}
	mentions.AddPersonaToken("chat-composer", model.SelectedID, agent.Reference)
	mentions.Set(mentionState{})
	replaceComposerText("chat-composer", question, len(utf16.Encode([]rune(question))))
}

func uniqueUnresolvedAgent(model Model, query string) bool {
	ids := map[string]bool{}
	for _, candidate := range model.ResolvedPersonaMentions {
		ref := candidate.Reference
		if validResolvedPersonaReference(ref, model.SelectedID) && ref.TenantID == model.CurrentTenantID && mentionTextScore(query, ref.Display, candidate.Handle, ref.ID) < 3 {
			ids[ref.ID] = true
		}
	}
	return len(ids) == 1
}

func agentThreadFollowUpHint(model Model) string {
	for _, invocation := range model.PersonaInvocations {
		if invocation.PostID == model.ThreadParentID {
			return agentUXChat4Format(model, "chat.agent.thread_follow_up", map[string]string{"name": agentReplyAuthor(model, invocation.PostID, invocation.Projection.AgentName)})
		}
	}
	return personaProgressText(model, "chat.agent.thread_continue", "Reply to continue in this thread")
}

func agentThreadPlaceholder(model Model) string {
	// CHATUX-008: the reply goes to the thread, so the box says so, in a channel
	// and in a direct conversation alike.
	return chatux008Text(model, "reply_in_thread")
}

func openAgentFollowUp(model Model, href string) {
	parsed, err := url.Parse(href)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path != "/workspace/app/chat" || !strings.HasPrefix(parsed.Fragment, "channel=") {
		return
	}
	id, err := url.QueryUnescape(strings.TrimPrefix(parsed.Fragment, "channel="))
	if err != nil || !validChannelReferenceID(id) {
		return
	}
	if model.Callbacks.SelectConversation != nil {
		model.Callbacks.SelectConversation(id)
	} else if model.Callbacks.Navigate != nil {
		model.Callbacks.Navigate(href)
	}
	focusAgentChatComposer()
}

func requestAgentDocumentAccess(model Model) {
	for _, room := range model.Conversations {
		if strings.EqualFold(room.Name, "people-ops") || strings.EqualFold(room.Name, "People Operations") {
			if model.Callbacks.SelectConversation != nil {
				model.Callbacks.SelectConversation(room.ID)
			}
			if model.Callbacks.DraftChanged != nil {
				model.Callbacks.DraftChanged(room.ID, agentUXChat4Text(model, "chat.agent.access_draft"))
			}
			focusAgentChatComposer()
			return
		}
	}
	if model.Callbacks.OpenBrowse != nil {
		model.Callbacks.OpenBrowse()
		if model.Callbacks.FilterBrowse != nil {
			model.Callbacks.FilterBrowse("people")
		}
	}
}

func agentProfileDialog(model Model, id string) ui.Node {
	for _, agent := range model.ResolvedPersonaMentions {
		if agent.Reference.ID == id {
			return html.Div(html.Props{Class: "chat-dialog-backdrop"}, html.Dialog(html.Props{Class: "chat-dialog agent-profile-dialog", Open: true, Role: "dialog", Dir: agentReplyDirection(model.Locale), Aria: map[string]string{"modal": "true", "label": agent.Reference.Display}},
				html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{Text: agent.Reference.Display}), actionButton("icon-button", "agent-profile-close", "", model.t(KeyClose), false, icon("close"))), chatux017AgentSummary(model, agent)))
		}
	}
	return nil
}

func agentMentionKeyboardHint(locale string, details bool) ui.Node {
	var keys []ui.Node
	for _, key := range strings.Split(mentionHintText(locale, details), " · ") {
		keys = append(keys, html.Span(html.Props{Text: key}))
	}
	return html.P(html.Props{Class: "mention-hint kbd-hint", Aria: map[string]string{"hidden": "true"}}, keys...)
}
