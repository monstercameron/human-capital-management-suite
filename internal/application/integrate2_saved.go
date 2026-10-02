package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

type integrate2Saved struct {
	SavedMessagesPort
	Rendering ChatRenderingPort
}

func (s integrate2Saved) readerItems(ctx context.Context, p chat.Principal, items []chat.SavedItem) []chat.SavedItem {
	out := append([]chat.SavedItem(nil), items...)
	for i := range out {
		item := &out[i]
		if item.Availability != "readable" || item.Post == nil {
			continue
		}
		selected, mark, err := s.Rendering.ReadRenderingSelection(ctx, chatstore.RenderingScope{Principal: p, Tenant: item.TenantID, Conversation: item.ConversationID}, item.PostID)
		if err != nil || mark.State == "pending" || mark.State == "unavailable" {
			item.Post = nil
			item.Availability = "unavailable"
			continue
		}
		post := *item.Post
		post.Body = selected.Text
		item.Post = &post
	}
	return out
}
func (s integrate2Saved) ListSaved(ctx context.Context, r chat.SavedListRequest) (chat.SavedPage, error) {
	page, err := s.SavedMessagesPort.ListSaved(ctx, r)
	if err != nil {
		return page, err
	}
	page.Items = s.readerItems(ctx, r.Principal, page.Items)
	return page, nil
}
func (s integrate2Saved) SearchSaved(ctx context.Context, p chat.Principal, query string) ([]chat.SavedItem, error) {
	// Match only after reader selection. The private note remains authored text.
	items, err := s.SavedMessagesPort.SearchSaved(ctx, p, "")
	if err != nil {
		return nil, err
	}
	items = s.readerItems(ctx, p, items)
	return integrate2MatchSaved(items, query), nil
}

func integrate2MatchSaved(items []chat.SavedItem, query string) []chat.SavedItem {
	query = strings.ToLower(strings.TrimSpace(query))
	out := []chat.SavedItem{}
	for _, item := range items {
		body := item.Note
		if item.Post != nil && item.Availability == "readable" {
			body += "\n" + item.Post.Body
		}
		if query == "" || strings.Contains(strings.ToLower(body), query) {
			out = append(out, item)
		}
	}
	return out
}
func integrate2SavedPort(runtime composedChat) SavedMessagesPort {
	if runtime.extensions == nil {
		return nil
	}
	core := runtime.extensions.SavedMessages()
	if core == nil {
		return nil
	}
	if runtime.renderings == nil {
		return core
	}
	return integrate2Saved{SavedMessagesPort: core, Rendering: runtime.renderings}
}
