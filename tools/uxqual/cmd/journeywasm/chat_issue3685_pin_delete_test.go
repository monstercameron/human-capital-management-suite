package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestChat_Issue3685_DeletedPinLeavesPinList: deleting a pinned message drops
// it from the client pin list at once, both from the person's own delete
// response (chatDropDeletedRow) and from the stream's delete event
// (applyChatPostDeleted). The pin count and the Pinned list read ChannelPins.
func TestChat_Issue3685_DeletedPinLeavesPinList(t *testing.T) {
	newModel := func() chatui.Model {
		return chatui.Model{
			Messages:    []chatui.Message{{ID: "p1", Body: "keep me out"}, {ID: "p2", Body: "other"}},
			ChannelPins: []chatui.ChannelPin{{PostID: "p1", Body: "keep me out"}, {PostID: "p2", Body: "other"}},
		}
	}

	own := newModel()
	chatDropDeletedRow(&own, "p1")
	if len(own.ChannelPins) != 1 || own.ChannelPins[0].PostID != "p2" {
		t.Fatalf("own delete: ChannelPins=%+v, want only p2", own.ChannelPins)
	}

	streamed := newModel()
	applyChatPostDeleted(&streamed, &chatv1.Post{Id: "p1", Deleted: true})
	if len(streamed.ChannelPins) != 1 || streamed.ChannelPins[0].PostID != "p2" {
		t.Fatalf("stream delete: ChannelPins=%+v, want only p2", streamed.ChannelPins)
	}
}
