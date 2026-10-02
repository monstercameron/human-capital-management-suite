package chatui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-071, the person who is not in the conversation. The mention menu
// offers people from the directory under "Not in this conversation". Choosing
// one wrote "@Sofia Beltran" into the draft and nothing else: the message was
// sent with no reference, nobody was told, and the page did not say so. The
// draft now keeps such a person apart from its references, says under the
// draft that they are not in the conversation and will not be notified, and
// offers to add them where the viewer may add people. Once they are a member
// the mention is a reference like any other.

// ChatBug071Styles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatBug071Styles = `
.chat-workspace .composer-outside-note{display:flex;flex-wrap:wrap;align-items:center;gap:4px 8px;margin:0;padding:2px 8px 4px;font-size:.75rem;color:var(--hcm-color-text-muted)}
.chat-workspace .composer-outside-note p{margin:0;min-width:0;overflow-wrap:anywhere}
.chat-workspace .composer-outside-note .composer-outside-error{flex-basis:100%;color:var(--hcm-color-text)}
`

const (
	keyChatbug071Outside = "chat.bug071.outside"
	keyChatbug071Add     = "chat.bug071.add"
	actionChatbug071Add  = "mention-add-outside"
)

var chatbug071Copy = map[string]map[string]string{
	"en-US": {
		keyChatbug071Outside: "{name} is not in this conversation and will not be notified.",
		keyChatbug071Add:     "Add {name}",
	},
	"de-DE": {
		keyChatbug071Outside: "{name} ist nicht in dieser Unterhaltung und wird nicht benachrichtigt.",
		keyChatbug071Add:     "{name} hinzufügen",
	},
	"ar": {
		keyChatbug071Outside: "{name} ليس في هذه المحادثة ولن يتلقى إشعارًا.",
		keyChatbug071Add:     "إضافة {name}",
	},
}

func chatbug071Text(m Model, key, name string) string {
	text := chatbug039Text(key, chatbug071Copy[chatEmojiLocale(m.Locale)][key], chatbug071Copy["en-US"][key])
	return strings.ReplaceAll(text, "{name}", name)
}

// AddPerson remembers the person the mention menu's choice put into a draft,
// at start to end of the text. Only a member's identifier is one the service
// accepts as a mention, so a member becomes one of the draft's references;
// someone outside the conversation stays plain text, and the draft says so and
// offers to add them.
func (s mentionStore) AddPerson(target, conversationID string, start, end int, person mentionCandidate) {
	if person.Member {
		s.AddPersona(target, conversationID, start, end, ChatReference{Kind: "PERSON_MENTION", TenantID: person.HomeTenantID, ID: person.ID, Display: person.Name, ConversationID: conversationID})
		return
	}
	s.AddOutsider(target, conversationID, start, end, person)
}

// AddOutsider remembers that the draft names a person who is not a member of
// the conversation. It is kept apart from the draft's references: the service
// accepts a mention only of a member.
func (s mentionStore) AddOutsider(target, conversationID string, start, end int, person mentionCandidate) {
	s.box.outsiders = append(s.box.outsiders, personaDraftMention{Target: target, ConversationID: conversationID, Start: start, End: end,
		Reference: ChatReference{Kind: "PERSON_MENTION", TenantID: person.HomeTenantID, ID: person.ID, Display: person.Name, ConversationID: conversationID}})
	s.box.seq++
	s.tick.Set(s.box.seq)
}

// chatbug071NamedIn reports whether text holds "@name" as a finished mention:
// not glued to a word before the "@", and not continued by a letter after the
// name. It looks anywhere in the text, not at the offsets the menu wrote the
// name at, because typing earlier in the draft moves them and the line under
// the draft must follow the name, not the offsets.
func chatbug071NamedIn(text, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	want := "@" + name
	for from := 0; from < len(text); {
		at := strings.Index(text[from:], want)
		if at < 0 {
			return false
		}
		at += from
		from = at + 1
		if at > 0 {
			prev, _ := utf8.DecodeLastRuneInString(text[:at])
			if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' || prev == '@' {
				continue
			}
		}
		if end := at + len(want); end < len(text) {
			next, _ := utf8.DecodeRuneInString(text[end:])
			if unicode.IsLetter(next) || unicode.IsDigit(next) || next == '_' {
				continue
			}
		}
		return true
	}
	return false
}

// reconcileOutsiders drops the people whose name is no longer in the draft, and
// reports whether it dropped any.
func (s mentionStore) reconcileOutsiders(target, value string) bool {
	kept := s.box.outsiders[:0]
	for _, item := range s.box.outsiders {
		if item.Target == target && !chatbug071NamedIn(value, item.Reference.Display) {
			continue
		}
		kept = append(kept, item)
	}
	changed := len(kept) != len(s.box.outsiders)
	s.box.outsiders = kept
	return changed
}

// removeOutsiders forgets a sent or cleared draft's people, and reports whether
// there were any.
func (s mentionStore) removeOutsiders(target, conversationID string) bool {
	kept := s.box.outsiders[:0]
	for _, item := range s.box.outsiders {
		if item.Target == target && item.ConversationID == conversationID {
			continue
		}
		kept = append(kept, item)
	}
	changed := len(kept) != len(s.box.outsiders)
	s.box.outsiders = kept
	return changed
}

// chatbug071Member finds a person among the conversation's members, by
// identifier or, because the directory and the member list can name one person
// under two identifiers, by name.
func chatbug071Member(m Model, reference ChatReference) (Member, bool) {
	name := strings.ToLower(strings.TrimSpace(reference.Display))
	for _, member := range m.Members {
		if member.Agent {
			continue
		}
		if member.ID == reference.ID || (name != "" && strings.ToLower(strings.TrimSpace(member.Name)) == name) {
			return member, true
		}
	}
	return Member{}, false
}

// SettleOutsiders is called while the workspace renders. A person the draft
// names who has since become a member (the viewer pressed Add, or someone else
// added them) moves to the draft's references, so the send carries the mention.
// It returns the people of the open conversation's drafts who are still
// outside. Like ForConversation it changes the store before the render reads
// it and asks for no second render.
//
// The other direction is settled here too. Until the member list has loaded
// the mention menu offers the authors on screen as members, so a person chosen
// in that moment, who wrote here and has since left, was kept as a reference:
// the page showed no note and sent a mention the service refuses. Once the
// list is there, a mentioned person who is not in it is outside.
func (s mentionStore) SettleOutsiders(m Model) []personaDraftMention {
	if s.box == nil {
		return nil
	}
	if len(m.Members) > 0 {
		kept := s.box.personas[:0]
		for _, item := range s.box.personas {
			if item.ConversationID == m.SelectedID && !item.Detached && item.Reference.Kind == "PERSON_MENTION" {
				if _, ok := chatbug071Member(m, item.Reference); !ok {
					s.box.outsiders = append(s.box.outsiders, item)
					continue
				}
			}
			kept = append(kept, item)
		}
		s.box.personas = kept
	}
	if len(s.box.outsiders) == 0 {
		return nil
	}
	var outside []personaDraftMention
	kept := s.box.outsiders[:0]
	for _, item := range s.box.outsiders {
		if item.ConversationID != m.SelectedID {
			kept = append(kept, item)
			continue
		}
		if member, ok := chatbug071Member(m, item.Reference); ok {
			item.Reference.ID = member.ID
			if member.HomeTenantID != "" {
				item.Reference.TenantID = member.HomeTenantID
			}
			s.box.personas = append(s.box.personas, item)
			continue
		}
		kept = append(kept, item)
		outside = append(outside, item)
	}
	s.box.outsiders = kept
	return outside
}

// chatbug071MayAdd reports whether the viewer may add people to the open
// conversation: the rule of the Add people button in the conversation's
// details.
func chatbug071MayAdd(m Model) bool {
	c := m.selected()
	if c.Kind == DirectMessage || m.Callbacks.AddMembers == nil {
		return false
	}
	return c.Kind == PublicChannel || (m.CurrentUser != "" && c.OwnerID == m.CurrentUser)
}

// chatbug071OutsideNote is the line under a draft for each person it names who
// is not in the conversation, with the offer to add them. It is nil when the
// draft names nobody outside.
func chatbug071OutsideNote(m Model, outside []personaDraftMention, target string) ui.Node {
	var rows []ui.Node
	seen := map[string]bool{}
	for _, item := range outside {
		if item.Target != target || item.ConversationID != m.SelectedID || seen[item.Reference.ID] {
			continue
		}
		seen[item.Reference.ID] = true
		name := strings.TrimSpace(item.Reference.Display)
		rows = append(rows, html.P(html.Props{Dir: "auto", Text: chatbug071Text(m, keyChatbug071Outside, name)}))
		if chatbug071MayAdd(m) {
			rows = append(rows, html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.AddMembersPending,
				Data: map[string]string{"action": actionChatbug071Add, "id": item.Reference.ID}, Text: chatbug071Text(m, keyChatbug071Add, name)}))
		}
	}
	if len(rows) == 0 {
		return nil
	}
	// A refused add is reported in the Add people dialog when that is open, and
	// here when the add was asked for from this line.
	if m.AddMembersError != "" && !m.ShowAddMembers {
		rows = append(rows, html.P(html.Props{Class: "composer-outside-error", Role: "alert", Text: m.AddMembersError}))
	}
	return html.Div(html.Props{Class: "composer-outside-note", Role: "status", Aria: map[string]string{"live": "polite"}}, rows...)
}

// chatbug071Add adds the person named under the draft to the open conversation.
func chatbug071Add(m Model, id string) {
	if id != "" && chatbug071MayAdd(m) && !m.AddMembersPending {
		m.Callbacks.AddMembers([]string{id})
	}
}
