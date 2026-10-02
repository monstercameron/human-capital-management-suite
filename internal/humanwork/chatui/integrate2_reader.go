package chatui

import "strings"
import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"

type ReaderSelection struct {
	Revision  uint64               `json:"revision"`
	Rendering chatrender.Rendering `json:"rendering"`
	Mark      chatrender.Mark      `json:"mark"`
}

// ReaderRendering carries admitted policy without changing the authored body.
type ReaderRendering struct {
	Policy     chatrender.Policy
	Preference chatrender.Preference
	Available  chatrender.Available
}

func ReaderMessageBody(m Model, msg Message) string {
	policy := chatrender.Policy{Original: chatrender.Rendering{Message: msg.ID, Revision: msg.Revision, Tone: chatrender.AsWritten, Text: msg.Body}, AllowOriginal: true}
	preference := chatrender.DefaultPreference(m.Locale)
	available := chatrender.Available{}
	if view, ok := m.ReaderSelections[msg.ID]; ok && ((view.Rendering.Message == msg.ID && (view.Rendering.Revision == msg.Revision || (msg.Revision == 0 && view.Rendering.Revision == 1))) || ((view.Mark.State == "unavailable" || view.Mark.State == "pending") && (view.Revision == msg.Revision || view.Revision == 0))) {
		if view.Mark.State == "pending" || view.Mark.State == "unavailable" {
			// The sentence stands in for the text only when the server's own mark withholds the original from this reader.
			if !view.Mark.CanShowOriginal {
				policy.AllowOriginal = false
				available.Pending = view.Mark.State == "pending"
				available.Failed = !available.Pending
			}
		} else {
			policy.Original = view.Rendering
		}
	}
	if view, ok := m.ReaderRenderings[msg.ID]; ok {
		// A view belongs to the exact authored revision; stale views cannot survive edits.
		if view.Policy.Original.Message == msg.ID && view.Policy.Original.Revision == msg.Revision {
			policy, preference, available = view.Policy, view.Preference, view.Available
		}
	}
	selected, mark := chatrender.SelectForReader(policy, preference, available)
	if mark.State == "pending" || mark.State == "unavailable" {
		return RenderingText(m.Locale, mark.State)
	}
	return selected.Text
}

func readerText(m Model, id, body string, revision uint64) string {
	return ReaderMessageBody(m, Message{ID: id, Body: body, Revision: revision})
}

func readerReplyBody(m Model, msg Message, authoredVisible string) string {
	selected := ReaderMessageBody(m, msg)
	if selected == msg.Body {
		return authoredVisible
	}
	// Only the authored envelope identifies control bytes. Derived text is never parsed as authority.
	if strings.HasPrefix(msg.Body, authoredVisible) {
		selected = strings.TrimSuffix(selected, strings.TrimPrefix(msg.Body, authoredVisible))
	}
	return selected
}
func readerAnnouncementMessage(m Model, msg Message) Message {
	if !msg.PersonaActor.valid() {
		return msg
	}
	original, ok := DecodeAnnouncementMessageBody(msg.Body)
	if !ok {
		return msg
	}
	selected := ReaderMessageBody(m, msg)
	if selected == msg.Body {
		return msg
	}
	original.Text = selected
	encoded, err := AnnouncementMessageBody(original)
	if err == nil {
		msg.Body = encoded
	}
	return msg
}

// AuthoredReaderText strips control bytes only after the caller attests the agent author.
func AuthoredReaderText(body string) string {
	if announcement, ok := DecodeAnnouncementMessageBody(body); ok {
		return announcement.Text
	}
	return parseAgentReplyEnvelope(body).Body
}
