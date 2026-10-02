package chatui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// CHATSEARCH-002: the workspace search box asks Chat too. What Chat answers is
// drawn by the workspace's own list, so each result is handed over as plain
// facts: what it is, one line of it, where it is, and the address that opens
// it at the exact place.

// ChatSearchWorkspaceResult is one Chat result as the workspace search lists it.
type ChatSearchWorkspaceResult struct {
	// ID is unique among the results of one answer.
	ID string
	// Kind is the registry's kind, for the icon.
	Kind chatsearch.Kind
	// KindLabel is the kind in the reader's language ("Message", "Agent").
	KindLabel string
	// Text is one line of the result, with the searched words in it.
	Text string
	// Where says what it is in: "#general", a person's name, or the kind of
	// result when the conversation cannot be named.
	Where string
	// Href opens it.
	Href string
}

// chatsearch002WorkspaceLine is how much of a result the workspace list shows.
const chatsearch002WorkspaceLine = 96

// ChatSearchWorkspaceResults turns Chat's answer into at most limit rows for
// the workspace search. A message that several sources returned is listed
// once. People are left out: the workspace search already lists the people
// directory. A result that names nothing to open is left out too.
func ChatSearchWorkspaceResults(m Model, response chatsearch.Response, words string, limit int) []ChatSearchWorkspaceResult {
	groups, _ := chatsearchDedupe(response.Groups)
	parsed, _ := chatsearch.Parse(words)
	out := []ChatSearchWorkspaceResult{}
	for _, group := range groups {
		for _, row := range group.Rows {
			if len(out) >= limit {
				return out
			}
			if row.Kind == chatsearch.Person || row.ID == "" {
				continue
			}
			href := ChatSearchRowHref(row)
			if href == "" {
				continue
			}
			text := chatDisplayText(row.Text)
			if ChatSearchAgentHref(row) != "" {
				// An agent is listed by its name; its purpose is not the line.
				text, _, _ = strings.Cut(text, "\n")
			} else {
				text = searchSnippet(text, parsed.Text, chatsearch002WorkspaceLine)
			}
			if text = strings.TrimSpace(text); text == "" {
				continue
			}
			// A conversation the page cannot name is replaced by the kind of
			// result, never by an identifier.
			where := chatux010GroupName(m.Locale, row.Kind)
			if c, ok := chatsearchConversation(m, row); ok {
				if name := displayName(m, c); name != "" {
					where = name
					if c.Kind == PublicChannel || c.Kind == PrivateChannel {
						where = "#" + name
					}
				}
			}
			out = append(out, ChatSearchWorkspaceResult{ID: string(row.Kind) + ":" + row.ID, Kind: row.Kind, KindLabel: chatsearchKind(m.Locale, row.Kind), Text: text, Where: where, Href: href})
		}
	}
	return out
}

// ChatSearchRowHref is the address that opens a result from outside Chat: the
// page of an agent record, or the conversation with the message, the thread
// and the sentence the result names. It is "" when the result names nothing an
// address can open.
func ChatSearchRowHref(row chatsearch.Row) string {
	if href := ChatSearchAgentHref(row); href != "" {
		return href
	}
	target := row.Target
	if target.ConversationID == "" {
		return ""
	}
	href := ChannelReferenceURL(target.ConversationID)
	extra := url.Values{}
	switch {
	case target.ThreadID != "" && target.ThreadSequence > 0:
		// A reply opens its thread, with the message the thread hangs on in view.
		extra.Set("message", target.ThreadID)
		extra.Set("at", strconv.FormatUint(target.ThreadSequence, 10))
		extra.Set("thread", "1")
	case target.MessageID != "" && target.Sequence > 0:
		extra.Set("message", target.MessageID)
		extra.Set("at", strconv.FormatUint(target.Sequence, 10))
		if row.Kind == chatsearch.Voice && target.Sentence > 0 {
			extra.Set("sentence", strconv.Itoa(target.Sentence))
		}
	}
	if len(extra) == 0 {
		return href
	}
	return href + "&" + extra.Encode()
}

// ChatSearchAddress is what a "#channel=" address asks for beyond the
// conversation: the message to open at, whether to open its thread, and the
// sentence of a voice message. ok is false when the address names no message.
type ChatSearchAddress struct {
	MessageID string
	Sequence  uint64
	Thread    bool
	Sentence  int
}

// ParseChatSearchAddress reads the parts ChatSearchRowHref writes after the
// conversation. Anything malformed is no message at all: the conversation
// still opens.
func ParseChatSearchAddress(hash string) (ChatSearchAddress, bool) {
	if !strings.HasPrefix(hash, "#channel=") {
		return ChatSearchAddress{}, false
	}
	_, extra, found := strings.Cut(hash[1:], "&")
	if !found {
		return ChatSearchAddress{}, false
	}
	params, err := url.ParseQuery(extra)
	if err != nil {
		return ChatSearchAddress{}, false
	}
	address := ChatSearchAddress{MessageID: params.Get("message"), Thread: params.Get("thread") == "1"}
	if address.MessageID == "" || len(address.MessageID) > 256 {
		return ChatSearchAddress{}, false
	}
	address.Sequence, err = strconv.ParseUint(params.Get("at"), 10, 63)
	if err != nil || address.Sequence == 0 {
		return ChatSearchAddress{}, false
	}
	if raw := params.Get("sentence"); raw != "" {
		sentence, err := strconv.Atoi(raw)
		if err != nil || sentence < 1 || sentence > 10000 {
			return ChatSearchAddress{}, false
		}
		address.Sentence = sentence
	}
	return address, true
}
