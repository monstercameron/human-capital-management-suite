package application

import (
	"context"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATMOD-005: the Permissions tab of the Moderation page. A workspace
// administrator reads the stored answers here and changes one with
// POST /api/chat/moderation/permissions, which existed before the page did.

// ChatModerationPermissionReader reads the stored permission rows. The store
// answers only a current workspace administrator, so a successful read is also
// the check that the person may see the tab.
type ChatModerationPermissionReader interface {
	ModerationPermissionRows(context.Context, chat.Principal, string, string) ([]chat.ModerationPermissionRow, error)
}

// chatmod005ConversationLister is the part of the chat service the tab uses to
// offer the administrator their channels.
type chatmod005ConversationLister interface {
	ListConversations(context.Context, chat.ListConversationsRequest) (chat.ListConversationsResponse, error)
}

func (s *chatremoveRoutedStore) ModerationPermissionRows(ctx context.Context, p chat.Principal, t, cid string) ([]chat.ModerationPermissionRow, error) {
	reader, ok := s.Permissions.(ChatModerationPermissionReader)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return reader.ModerationPermissionRows(ctx, p, t, cid)
}

// chatmod005Permissions builds the tab's table for the workspace or for one
// channel. ok is false for anyone who is not a workspace administrator, and
// when the rows cannot be read.
func (h ChatModerationPageHTTP) chatmod005Permissions(ctx context.Context, p chat.Principal, conversation, role string) (chatui.ModerationPermissionsModel, bool) {
	reader, ok := h.HTTP.Permissions.(ChatModerationPermissionReader)
	if !ok {
		return chatui.ModerationPermissionsModel{}, false
	}
	model := chatui.ModerationPermissionsModel{ExtraRole: strings.TrimSpace(role)}
	if len(model.ExtraRole) > 120 {
		model.ExtraRole = ""
	}
	// The channels the administrator is in: a permission in a private channel
	// takes being a member of it, so those are the ones they can set.
	if lister, ok := h.Context.(chatmod005ConversationLister); ok {
		page := chat.Page{PageSize: 200}
		for range 5 {
			listed, err := lister.ListConversations(ctx, chat.ListConversationsRequest{Principal: p, TenantID: p.TenantID, Page: page})
			if err != nil {
				break
			}
			for _, c := range listed.Conversations {
				if c.Kind != chat.PublicChannel && c.Kind != chat.PrivateChannel {
					continue
				}
				model.Channels = append(model.Channels, chatui.ModerationChannelChoice{ID: c.ID, Name: c.Name})
				if c.ID == conversation {
					model.ConversationID, model.ConversationName = c.ID, c.Name
				}
			}
			if listed.NextCursor == "" {
				break
			}
			page.Cursor = listed.NextCursor
		}
		sort.SliceStable(model.Channels, func(i, j int) bool {
			return strings.ToLower(model.Channels[i].Name) < strings.ToLower(model.Channels[j].Name)
		})
	}
	rows, err := reader.ModerationPermissionRows(ctx, p, p.TenantID, model.ConversationID)
	if err != nil {
		return chatui.ModerationPermissionsModel{}, false
	}
	model.Rows = rows
	return model, true
}
