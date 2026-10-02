package main

import (
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatPinProjection is a pin as the details panel shows it: the pinned
// message, and who pinned it and when (CHATUX-019).
func chatPinProjection(pin *chatv1.Pin, directory map[string]string) chatui.ChannelPin {
	post := pin.GetPost()
	projected := chatui.ChannelPin{
		PostID: pin.GetPostId(), Author: chatDisplayName(directory, post.GetAuthorId()),
		Body: post.GetBody(), Sequence: post.GetSequence(), Revision: post.GetRevision(),
	}
	if by := pin.GetPinnedBy(); by != "" {
		projected.PinnedBy = chatDisplayName(directory, by)
	}
	if at := pin.GetCreatedAt(); at != nil && at.IsValid() {
		projected.PinnedAt = at.AsTime().In(time.Local)
	}
	return projected
}
