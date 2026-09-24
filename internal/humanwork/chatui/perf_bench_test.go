package chatui

import (
	"strconv"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// benchModel is a busy channel as the demo seed shapes it: 23 rooms, a
// 60-person directory, 60 loaded messages with mentions, lists and reactions.
func benchModel() Model {
	m := Model{State: StateReady, SelectedID: "room-0", CurrentUser: "hc-050", CurrentUserName: "Rafael Torres"}
	names := []string{"Camila Morales", "Zuri Mensah", "Mina Sato", "Marcus Reed", "Hugo Dubois", "Jasmine Price", "Dominic Collins", "Anika Desai", "Nia Brooks", "Rosa Santos"}
	for i := 0; i < 60; i++ {
		id := "hc-" + strconv.Itoa(100+i)
		name := names[i%len(names)] + " " + strconv.Itoa(i)
		m.SearchDirectory = append(m.SearchDirectory, SearchPerson{ID: id, Name: name})
		if i < 20 {
			m.Members = append(m.Members, Member{ID: id, Name: name})
		}
	}
	for i := 0; i < 23; i++ {
		kind := PublicChannel
		if i >= 13 {
			kind = DirectMessage
		}
		m.Conversations = append(m.Conversations, Conversation{ID: "room-" + strconv.Itoa(i), Name: "room-" + strconv.Itoa(i), Kind: kind, Unread: i % 3})
	}
	start := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 60; i++ {
		author := m.SearchDirectory[i%20]
		body := "@" + m.SearchDirectory[(i+1)%20].Name + " Can we keep this thread for decisions and take the debate to a call?"
		if i%4 == 0 {
			body += "\n\nOpen questions before Friday:\n- who signs the exception\n- whether the band table is final\n- how we tell the affected managers"
		}
		m.Messages = append(m.Messages, Message{ID: "p" + strconv.Itoa(i), AuthorID: author.ID, Author: author.Name, Body: body, SentAt: start.Add(time.Duration(i) * 40 * time.Minute), TimeLabel: "9:22 PM",
			Replies: i % 5, Chips: []ReactionChip{{Emoji: "👍", Count: 2}, {Emoji: "🎉", Count: 1}}})
	}
	return m
}

func BenchmarkWorkspaceRender(b *testing.B) {
	m := benchModel()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ui.RenderToString(Build(m)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMessageBodyWithMentions(b *testing.B) {
	m := benchModel()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, msg := range m.Messages {
			_ = markdownMessageBody(m, msg.Body)
		}
	}
}

func BenchmarkMentionCandidatesPerKeystroke(b *testing.B) {
	m := benchModel()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = mentionCandidates(m, "ca")
	}
}
