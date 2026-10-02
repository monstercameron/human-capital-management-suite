package chatui

import "strings"

// CHATBUG-052. The browser tab names what the page shows: the open
// conversation, the Moderation page, the search results with their words, or
// the Saved panel. The browser client puts this in front of the page's own
// title ("#random · Chat").

// Chat page kinds that take part in the address and the tab title.
const (
	ChatPageModeration = "moderation"
	ChatPageSaved      = "saved"
	ChatPageSearch     = "search"
)

// ChatTabTitle is what the tab names for page ("" for the conversation), and
// "" when there is nothing to name yet.
func ChatTabTitle(m Model, page, query string) string {
	switch page {
	case ChatPageModeration:
		key := "moderation"
		if !m.Moderation.Moderator {
			key = "notices_title"
		}
		return chatremoveText(m.Locale, key)
	case ChatPageSaved:
		return SavedMessagesCopy(m.Locale).Saved
	case ChatPageSearch:
		return chatsearchText(m.Locale, "search") + ": " + strings.TrimSpace(query)
	}
	if m.SelectedID == "" {
		return ""
	}
	c := m.selected()
	name := strings.TrimSpace(displayName(m, c))
	if name == "" {
		return ""
	}
	if c.Kind == PublicChannel {
		return "#" + name
	}
	return name
}
