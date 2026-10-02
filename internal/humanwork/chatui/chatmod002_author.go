package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATMOD-002: the author's side of language filtering.
//
//   - A message a filter BLOCKS never reaches the conversation. The text stays in
//     its field and one plain line under the field names the words; the line is
//     role="alert", dir="auto", and goes away when the text changes.
//   - A message a filter MASKS reads, for its author too, with "[removed word]"
//     where the word was; the author also gets one quiet note under the message.
//   - A message a filter FLAGS is delivered as sent and nothing here shows it.

// ModAuthorMaskToken is the literal the server puts where it hid a word.
const ModAuthorMaskToken = "[removed word]"

// AuthorBlocked is one refusal the author has not yet answered by editing. Text
// is what was refused: the line stays only while the field still holds it.
type AuthorBlocked struct {
	// Surface is ModAuthorSurfaceMessage or ModAuthorSurfaceSaved.
	Surface string
	// Words are the offending words when the server's offsets named them.
	Words []string
	// Text is the refused text.
	Text string
	// Stamp identifies this refusal, so one dismissed locally is not mistaken
	// for the next one.
	Stamp int64
}

// ModAuthorKeyComposer is the key for the main composer of a conversation.
func ModAuthorKeyComposer(conversationID string) string { return "composer:" + conversationID }

// ModAuthorKeyReply is the key for the thread composer under one root message.
func ModAuthorKeyReply(parentID string) string { return "reply:" + parentID }

// ModAuthorKeyEdit is the key for the inline edit form of one message.
func ModAuthorKeyEdit(postID string) string { return "edit:" + postID }

// WithAuthorBlocked returns blocked with one refusal set (or removed when entry
// is nil). The map is copied, never mutated, so a render in progress keeps the
// map it started with.
func WithAuthorBlocked(blocked map[string]AuthorBlocked, key string, entry *AuthorBlocked) map[string]AuthorBlocked {
	out := make(map[string]AuthorBlocked, len(blocked)+1)
	for k, v := range blocked {
		if k != key {
			out[k] = v
		}
	}
	if entry != nil {
		out[key] = *entry
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// modAuthorVisible is the refusal for key while the author has not yet edited.
func modAuthorVisible(m Model, local localUI, key string) (AuthorBlocked, bool) {
	entry, ok := m.AuthorBlocked[key]
	if !ok {
		return AuthorBlocked{}, false
	}
	if hidden, ok := local.modAuthorHidden[key]; ok && hidden == entry.Stamp {
		return AuthorBlocked{}, false
	}
	return entry, true
}

// modAuthorLine is the slot under a field. It is always one element, so the
// field above it never moves when the line comes or goes.
func modAuthorLine(m Model, local localUI, key, id string) ui.Node {
	entry, ok := modAuthorVisible(m, local, key)
	if !ok {
		return html.Div(html.Props{Class: "chatmod002-slot"})
	}
	return html.Div(html.Props{Class: "chatmod002-slot"},
		html.P(html.Props{ID: id, Class: "composer-notice chatmod002-blocked", Role: "alert", Dir: "auto", Text: modAuthorSentence(m, entry.Surface, entry.Words)}))
}

// modAuthorDescribe points the field at the line while the line is drawn.
func modAuthorDescribe(m Model, local localUI, key, id string, aria map[string]string) map[string]string {
	if _, ok := modAuthorVisible(m, local, key); !ok {
		return aria
	}
	if aria == nil {
		aria = map[string]string{}
	}
	if existing := strings.TrimSpace(aria["describedby"]); existing != "" {
		aria["describedby"] = existing + " " + id
	} else {
		aria["describedby"] = id
	}
	return aria
}

// modAuthorTyped hides the line once the field no longer holds the refused text.
// Hiding is local to this page and needs no round trip; the client drops the
// refusal itself when it is told the draft changed.
func modAuthorTyped(m Model, local localStore, key, value string) {
	entry, ok := modAuthorVisible(m, local.get(), key)
	if !ok || value == entry.Text || strings.TrimSpace(value) == strings.TrimSpace(entry.Text) {
		return
	}
	local.update(func(u *localUI) {
		if u.modAuthorHidden == nil {
			u.modAuthorHidden = map[string]int64{}
		}
		u.modAuthorHidden[key] = entry.Stamp
	})
}

// modAuthorMaskedNote is the note under an author's own message that readers see
// with a word hidden. It is secondary text, not an alert.
func modAuthorMaskedNote(m Model, msg Message, own bool) ui.Node {
	if !own || !strings.Contains(msg.Body, ModAuthorMaskToken) {
		return nil
	}
	return html.P(html.Props{Class: "chatmod002-masked-note", Dir: "auto", Text: modAuthorText(m, "modauthor_masked")})
}

// modAuthorInline renders plain text, drawing each removed-word token as a chip
// whose text is "removed word" in the viewer's language, so it is visible, themed
// and read out by a screen reader instead of being blank.
func modAuthorInline(m Model, text string) []ui.Node {
	if !strings.Contains(text, ModAuthorMaskToken) {
		return mentionReferenceBody(m, text)
	}
	var nodes []ui.Node
	for i, part := range strings.Split(text, ModAuthorMaskToken) {
		if i > 0 {
			nodes = append(nodes, html.Span(html.Props{Class: "chatfilter-removed", Text: chatfilterText(m, "removed")}))
		}
		if part != "" {
			nodes = append(nodes, mentionReferenceBody(m, part)...)
		}
	}
	return nodes
}

// ModAuthorErrorBlocked is the error code a to-do or poll surface records when a
// language filter refused the text it tried to save.
const ModAuthorErrorBlocked = "blocked"

// modAuthorErrorText is the sentence for that code, or "" for any other code.
func modAuthorErrorText(m Model, code string) string {
	if code != ModAuthorErrorBlocked {
		return ""
	}
	return modAuthorSentence(m, ModAuthorSurfaceSaved, nil)
}

// modAuthorErrorOr is the filter sentence for a refusal code, else other.
func modAuthorErrorOr(m Model, code, other string) string {
	if text := modAuthorErrorText(m, code); text != "" {
		return text
	}
	return other
}

// ChatMod002Styles is the line's slot, the masked note and the removed-word chip.
const ChatMod002Styles = `.chat-workspace .chatmod002-slot:empty{display:none}` +
	`.chat-workspace .chatmod002-blocked{overflow-wrap:anywhere}` +
	`.chat-workspace .chatmod002-masked-note{margin:2px 0 0;font-size:.75rem;line-height:1.4;color:var(--hcm-color-text-muted)}` +
	`.chat-workspace .chatfilter-removed{display:inline-block;padding:0 6px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text-muted);font-style:italic;font-size:.9em;line-height:1.5;vertical-align:baseline}`
