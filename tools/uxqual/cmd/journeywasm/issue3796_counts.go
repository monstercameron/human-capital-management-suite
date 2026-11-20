package main

import (
	"encoding/json"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3796: one person-scoped live channel. The server tells every tab the
// person has open, on whatever room it watches, that a room's counts moved (a
// post arrived) or that the read position moved (read in another tab). The
// notice carries a room id and two numbers, never content. The tab applies
// them to the sidebar at once; the slow periodic read stays as reconciliation.

// chatCountsNotice is one decoded count notice.
type chatCountsNotice struct {
	// Read is true for a read-position notice, false for a new-post notice.
	Read         bool
	Host         string
	Conversation string
	Unread       int
	Mentions     int
}

// chatCountsNoticeFrom decodes a count notice from a watch delivery, and
// reports false for any other delivery.
func chatCountsNoticeFrom(delivery *chatv1.EphemeralDelivery) (chatCountsNotice, bool) {
	if delivery == nil {
		return chatCountsNotice{}, false
	}
	read := false
	switch delivery.GetId() {
	case chatrecipient.UnreadChangedDeliveryID:
	case chatrecipient.ReadChangedDeliveryID:
		read = true
	default:
		return chatCountsNotice{}, false
	}
	var body struct {
		Host         string `json:"h"`
		Conversation string `json:"c"`
		Unread       uint64 `json:"u"`
		Mentions     uint64 `json:"m"`
	}
	if json.Unmarshal([]byte(delivery.GetBody()), &body) != nil || body.Conversation == "" {
		return chatCountsNotice{}, false
	}
	const ceiling = 1 << 30
	return chatCountsNotice{Read: read, Host: body.Host, Conversation: body.Conversation, Unread: int(min(body.Unread, ceiling)), Mentions: int(min(body.Mentions, ceiling))}, true
}

// applyChatCountsNotice puts a notice's counts on the room's sidebar row (both
// the flat list and the sections hold a copy). It reports whether the room is
// one the sidebar shows; a room the tab does not list is left to the next read.
func applyChatCountsNotice(model *chatui.Model, n chatCountsNotice) bool {
	found := false
	for i := range model.Conversations {
		if model.Conversations[i].ID == n.Conversation {
			model.Conversations[i].Unread, model.Conversations[i].Mentions = n.Unread, n.Mentions
			found = true
		}
	}
	for s := range model.Sections {
		for i := range model.Sections[s].Chats {
			if model.Sections[s].Chats[i].ID == n.Conversation {
				model.Sections[s].Chats[i].Unread, model.Sections[s].Chats[i].Mentions = n.Unread, n.Mentions
			}
		}
	}
	return found
}

// chatCountsNoticeRecord is the server-count record the recipient cache keeps,
// so a repaint from the cache shows the same numbers the notice did.
func chatCountsNoticeRecord(n chatCountsNotice, tenant, subject string) *chatv1.ChatCounts {
	return &chatv1.ChatCounts{TenantId: n.Host, ConversationId: n.Conversation, SubjectId: subject, HomeTenantId: tenant, UnreadCount: uint64(n.Unread), MentionCount: uint64(n.Mentions)}
}

// chatCountsNoticeStream hands every count notice to onNotice and passes every
// message on unchanged, as the sidebar notice stream does.
type chatCountsNoticeStream struct {
	next     chatEventStream
	onNotice func(chatCountsNotice)
}

func (s *chatCountsNoticeStream) Recv() (*chatv1.WatchConversationResponse, error) {
	message, err := s.next.Recv()
	if err == nil && message != nil && s.onNotice != nil {
		if notice, ok := chatCountsNoticeFrom(message.GetEphemeralDelivery()); ok {
			s.onNotice(notice)
		}
	}
	return message, err
}
