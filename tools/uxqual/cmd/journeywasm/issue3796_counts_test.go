package main

import (
	"errors"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3796Delivery(id, body string) *chatv1.EphemeralDelivery {
	return &chatv1.EphemeralDelivery{Id: id, Body: body, OnlyVisibleToYou: true}
}

func TestIssue3796_NoticeDecodesBothKindsAndRefusesOthers(t *testing.T) {
	n, ok := chatCountsNoticeFrom(issue3796Delivery(chatrecipient.UnreadChangedDeliveryID, `{"h":"t1","c":"room","u":4,"m":2}`))
	if !ok || n.Read || n.Host != "t1" || n.Conversation != "room" || n.Unread != 4 || n.Mentions != 2 {
		t.Fatalf("unread notice = %+v ok=%v", n, ok)
	}
	n, ok = chatCountsNoticeFrom(issue3796Delivery(chatrecipient.ReadChangedDeliveryID, `{"c":"room"}`))
	if !ok || !n.Read || n.Unread != 0 {
		t.Fatalf("read notice = %+v ok=%v", n, ok)
	}
	for name, d := range map[string]*chatv1.EphemeralDelivery{
		"nil":             nil,
		"sidebar notice":  issue3796Delivery(chatrecipient.SidebarChangedDeliveryID, "3"),
		"garbage body":    issue3796Delivery(chatrecipient.UnreadChangedDeliveryID, "nope"),
		"no conversation": issue3796Delivery(chatrecipient.UnreadChangedDeliveryID, `{"u":3}`),
		"a private reply": issue3796Delivery("agent-reply", `{"c":"room","u":3}`),
	} {
		if got, ok := chatCountsNoticeFrom(d); ok {
			t.Fatalf("%s decoded as %+v", name, got)
		}
	}
	huge, _ := chatCountsNoticeFrom(issue3796Delivery(chatrecipient.UnreadChangedDeliveryID, `{"c":"r","u":18446744073709551615}`))
	if huge.Unread <= 0 {
		t.Fatalf("a huge count overflowed to %d", huge.Unread)
	}
}

func TestIssue3796_NoticeUpdatesEveryCopyOfTheRoomRowAndOnlyThatRoom(t *testing.T) {
	model := &chatui.Model{
		Conversations: []chatui.Conversation{{ID: "a", Unread: 1}, {ID: "b", Unread: 5, Mentions: 1}},
		Sections:      []chatui.SidebarSection{{Chats: []chatui.Conversation{{ID: "b", Unread: 5, Mentions: 1}, {ID: "a"}}}},
	}
	if !applyChatCountsNotice(model, chatCountsNotice{Conversation: "b", Unread: 0, Mentions: 0, Read: true}) {
		t.Fatal("a listed room was not found")
	}
	if model.Conversations[1].Unread != 0 || model.Conversations[1].Mentions != 0 || model.Sections[0].Chats[0].Unread != 0 || model.Sections[0].Chats[0].Mentions != 0 {
		t.Fatalf("room b not cleared everywhere: %+v / %+v", model.Conversations[1], model.Sections[0].Chats[0])
	}
	if model.Conversations[0].Unread != 1 || model.Sections[0].Chats[1].Unread != 0 {
		t.Fatalf("another room moved: %+v", model.Conversations[0])
	}
	applyChatCountsNotice(model, chatCountsNotice{Conversation: "a", Unread: 7, Mentions: 2})
	if model.Conversations[0].Unread != 7 || model.Conversations[0].Mentions != 2 || model.Sections[0].Chats[1].Unread != 7 {
		t.Fatalf("room a = %+v / %+v", model.Conversations[0], model.Sections[0].Chats[1])
	}
	if applyChatCountsNotice(model, chatCountsNotice{Conversation: "unlisted", Unread: 1}) {
		t.Fatal("an unlisted room reported as found")
	}
}

type issue3796Stream struct {
	messages []*chatv1.WatchConversationResponse
}

func (s *issue3796Stream) Recv() (*chatv1.WatchConversationResponse, error) {
	if len(s.messages) == 0 {
		return nil, errors.New("end")
	}
	m := s.messages[0]
	s.messages = s.messages[1:]
	return m, nil
}

func TestIssue3796_StreamWrapperReportsNoticesAndPassesEverythingOn(t *testing.T) {
	unread := &chatv1.WatchConversationResponse{EphemeralDelivery: issue3796Delivery(chatrecipient.UnreadChangedDeliveryID, `{"c":"room","u":2}`)}
	other := &chatv1.WatchConversationResponse{EphemeralDelivery: issue3796Delivery("agent-reply", "hello")}
	var seen []chatCountsNotice
	wrapped := &chatCountsNoticeStream{next: &issue3796Stream{messages: []*chatv1.WatchConversationResponse{unread, other}}, onNotice: func(n chatCountsNotice) { seen = append(seen, n) }}
	for _, want := range []*chatv1.WatchConversationResponse{unread, other} {
		got, err := wrapped.Recv()
		if err != nil || got != want {
			t.Fatalf("message not passed on unchanged: %v %v", got, err)
		}
	}
	if len(seen) != 1 || seen[0].Conversation != "room" || seen[0].Unread != 2 {
		t.Fatalf("notices = %+v", seen)
	}
	if _, err := wrapped.Recv(); err == nil {
		t.Fatal("the end of the stream was hidden")
	}
}

func TestIssue3796_NoticeRecordMatchesTheRecipientCache(t *testing.T) {
	rec := chatCountsNoticeRecord(chatCountsNotice{Host: "t1", Conversation: "room", Unread: 3, Mentions: 1}, "home", "me")
	if rec.GetConversationId() != "room" || rec.GetTenantId() != "t1" || rec.GetHomeTenantId() != "home" || rec.GetSubjectId() != "me" || rec.GetUnreadCount() != 3 || rec.GetMentionCount() != 1 {
		t.Fatalf("record = %+v", rec)
	}
}
