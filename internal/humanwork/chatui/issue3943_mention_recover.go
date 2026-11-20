package chatui

import "strings"

// Issue 3943. Who a draft's @mention names is kept in the mounted workspace,
// apart from the draft text. A draft restored after a reload came back with the
// text and none of that, so a person who is not in the conversation lost the
// note that they would not be notified, and a member lost the reference that
// notifies them.
//
// recoverDraftMentions works the identity out again from what the page can see
// once it has the member list: the open draft, the members and the directory.
// It runs while the workspace renders, before the render reads the store, and
// asks for no second render. It only adds. A mention the person picked in this
// session is already in the store and is left alone, and a name is recognised
// only as a whole, finished "@Full Name" of exactly one person, so text that
// merely resembles a mention is never promoted. While the member list has not
// loaded nothing is guessed: the person might be a member.
func (s mentionStore) recoverDraftMentions(m Model) {
	const target = "chat-composer"
	if s.box == nil || m.SelectedID == "" || !strings.Contains(m.Draft, "@") || len(m.Members) == 0 {
		return
	}
	if !s.inOpenConversation(personaDraftMention{ConversationID: m.SelectedID}) {
		return
	}
	tracked := func(id, name string) bool {
		name = strings.ToLower(strings.TrimSpace(name))
		for _, list := range [][]personaDraftMention{s.box.personas, s.box.outsiders} {
			for _, item := range list {
				if item.Target != target || item.ConversationID != m.SelectedID || item.Reference.Kind != "PERSON_MENTION" {
					continue
				}
				if item.Reference.ID == id || (name != "" && strings.ToLower(strings.TrimSpace(item.Reference.Display)) == name) {
					return true
				}
			}
		}
		return false
	}
	add := func(id, tenant, name string, member bool) {
		item := personaDraftMention{Target: target, ConversationID: m.SelectedID,
			Reference: ChatReference{Kind: "PERSON_MENTION", TenantID: tenant, ID: id, Display: name, ConversationID: m.SelectedID}}
		if member {
			s.box.personas = append(s.box.personas, item)
		} else {
			s.box.outsiders = append(s.box.outsiders, item)
		}
	}
	for _, member := range m.Members {
		name := strings.TrimSpace(member.Name)
		if member.Agent || member.ID == "" || member.ID == m.CurrentUser || name == "" || name == member.ID {
			continue
		}
		if chatbug071NamedIn(m.Draft, name) && !tracked(member.ID, name) {
			tenant := member.HomeTenantID
			if tenant == "" {
				tenant = m.CurrentTenantID
			}
			add(member.ID, tenant, name, true)
		}
	}
	byName := map[string][]SearchPerson{}
	for _, person := range m.SearchDirectory {
		if key := strings.ToLower(strings.TrimSpace(person.Name)); key != "" {
			byName[key] = append(byName[key], person)
		}
	}
	for key, people := range byName {
		person := people[0]
		name := strings.TrimSpace(person.Name)
		if len(people) != 1 || person.ID == "" || person.ID == m.CurrentUser || name == person.ID || !chatbug071NamedIn(m.Draft, name) {
			continue
		}
		if _, isMember := chatbug071Member(m, ChatReference{ID: person.ID, Display: name}); isMember || tracked(person.ID, key) {
			continue
		}
		add(person.ID, m.CurrentTenantID, name, false)
	}
}
