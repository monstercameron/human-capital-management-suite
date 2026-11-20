package chatui

import "strings"

// ISSUE-3937: the search icon in a conversation's header starts a search in that
// conversation. The scope is the same "in:" filter the box already understands,
// written into the sidebar's own search box where the person can see it, edit it
// and delete it; the sidebar's own entry stays global.

const (
	keyIssue3937Hint           = "chat.issue3937.scope_hint"
	keyIssue3937InConversation = "chat.issue3937.in_conversation"
)

var issue3937Copy = map[string]map[string]string{
	"en-US": {keyIssue3937Hint: "Searches only in {scope}. Delete the in: filter in the search box to search all of Chat.", keyIssue3937InConversation: "In a conversation"},
	"de-DE": {keyIssue3937Hint: "Sucht nur in {scope}. Lösche den Filter „in:“ im Suchfeld, um den gesamten Chat zu durchsuchen.", keyIssue3937InConversation: "In einer Unterhaltung"},
	"ar":    {keyIssue3937Hint: "يبحث في {scope} فقط. احذف عامل التصفية in: من مربع البحث للبحث في كل الدردشة.", keyIssue3937InConversation: "في محادثة"},
}

// issue3937ScopeLabel is how the open conversation is named to a person: a
// channel with its "#", a direct or group chat by its people. It is empty while
// the conversation has no name to show, in which case nothing is scoped.
func issue3937ScopeLabel(m Model, c Conversation) string {
	name := strings.TrimSpace(displayName(m, c))
	if name == "" {
		return ""
	}
	if channelNamed(c.Kind) {
		return "#" + name
	}
	return name
}

// issue3937ScopeToken is the text after "in:" that reads back as c.
func issue3937ScopeToken(m Model, c Conversation) string {
	name := strings.TrimSpace(displayName(m, c))
	if name == "" {
		return ""
	}
	if channelNamed(c.Kind) || strings.Contains(name, `"`) {
		return searchInToken(name)
	}
	if strings.ContainsAny(name, " \t") {
		return `"` + name + `"`
	}
	return name
}

// issue3937OpenConversation is the conversation the header belongs to, and false
// when none is open or it is not one of the viewer's.
func issue3937OpenConversation(m Model) (Conversation, bool) {
	if m.SelectedID == "" {
		return Conversation{}, false
	}
	for _, c := range m.Conversations {
		if c.ID == m.SelectedID {
			return c, true
		}
	}
	if m.PreviewConversation != nil && m.PreviewConversation.ID == m.SelectedID {
		return *m.PreviewConversation, true
	}
	return Conversation{}, false
}

// issue3937ScopedQuery is what the search box holds after the header icon is
// pressed: the words already typed are kept and the "in:" filter names the open
// conversation. A filter naming another conversation is replaced, so a scope
// never carries into a room the person has left. With no open conversation the
// box is left as it is, and the search stays global.
func issue3937ScopedQuery(m Model, current string) string {
	c, ok := issue3937OpenConversation(m)
	token := issue3937ScopeToken(m, c)
	if !ok || token == "" {
		return current
	}
	rest := current
	if id, from, to := issue3937FindInFilter(m, current); id != "" {
		if id == c.ID {
			return current
		}
		rest = current[:from] + current[to:]
	}
	rest = strings.Join(strings.Fields(rest), " ")
	if rest == "" {
		return "in:" + token + " "
	}
	return "in:" + token + " " + rest
}

// issue3937FindInFilter locates the first "in:" filter that names a conversation
// the viewer has, as the byte range it occupies in query.
func issue3937FindInFilter(m Model, query string) (id string, from, to int) {
	lower := strings.ToLower(query)
	for at := 0; at < len(lower); {
		i := strings.Index(lower[at:], "in:")
		if i < 0 {
			break
		}
		i += at
		at = i + len("in:")
		if i != 0 && query[i-1] != ' ' {
			continue
		}
		name := strings.TrimPrefix(query[i+len("in:"):], "#")
		hash := len(query[i+len("in:"):]) - len(name)
		if found, used := channelFilterTarget(m, name); found != "" {
			return found, i, i + len("in:") + hash + used
		}
	}
	return "", 0, 0
}

// issue3937ScopeHint says, for assistive technology, where the header's search
// will look and how to widen it. It is empty when the icon searches everywhere.
func issue3937ScopeHint(m Model, c Conversation) string {
	label := issue3937ScopeLabel(m, c)
	if label == "" || issue3937ScopeToken(m, c) == "" {
		return ""
	}
	return laneTextf(m, issue3937Copy, keyIssue3937Hint, map[string]string{"scope": label})
}

// issue3937ConversationByID is the conversation a resolved "in:" filter names,
// from the page's own model.
func issue3937ConversationByID(v ChatSearchView, id string) (Model, Conversation, bool) {
	if v.Context == nil || id == "" {
		return Model{}, Conversation{}, false
	}
	m := *v.Context
	for _, c := range m.Conversations {
		if c.ID == id {
			return m, c, true
		}
	}
	return Model{}, Conversation{}, false
}

// issue3937InChipLabel is the text of the removable scope chip after the search
// ran. The filter holds the conversation's id once names are resolved; a person
// reads its name, and a direct or group chat is not called a channel. An id the
// page does not know is shown as it came.
func issue3937InChipLabel(v ChatSearchView, locale, channelWord, value string) string {
	m, c, ok := issue3937ConversationByID(v, value)
	if !ok {
		return channelWord + " " + value
	}
	m.Locale = locale
	label := issue3937ScopeLabel(m, c)
	word := channelWord
	if !channelNamed(c.Kind) {
		word = laneText(m, issue3937Copy, keyIssue3937InConversation)
	}
	if label == "" {
		return word
	}
	return word + " " + label
}

// issue3937InFieldValue is what the "In a channel" field of Refine search holds:
// the conversation's name rather than its id.
func issue3937InFieldValue(v ChatSearchView, value string) string {
	m, c, ok := issue3937ConversationByID(v, value)
	if !ok {
		return value
	}
	if name := strings.TrimSpace(displayName(m, c)); name != "" {
		return name
	}
	return value
}
