package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

type ChatSearchSavedReader interface {
	SearchSaved(context.Context, chat.Principal, string, string) ([]chat.SavedItem, error)
}

func RegisterChatSearchSaved(registry *chatsearch.Registry, reader ChatSearchSavedReader) error {
	if registry == nil || reader == nil {
		return chatsearch.ErrInvalid
	}
	search := func(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
		items, err := reader.SearchSaved(ctx, chat.Principal{TenantID: q.Actor.HomeTenantID, SubjectID: q.Actor.PersonID}, q.Actor.TenantID, "")
		if err != nil {
			return nil, err
		}
		out := []chatsearch.Row{}
		for _, item := range items {
			if item.HomeTenantID != q.Actor.HomeTenantID || item.PersonID != q.Actor.PersonID || item.Post == nil || item.Post.Deleted || item.Post.TenantID != q.Actor.TenantID {
				continue
			}
			p := item.Post
			row := chatsearch.Row{Kind: chatsearch.Saved, ID: item.ConversationID + ":" + item.PostID, TenantID: p.TenantID, Text: item.Note + "\n" + p.Body, AuthorID: p.AuthorID, OwnerID: item.PersonID, Private: true, At: item.CreatedAt, InThread: p.ParentID != "", Target: chatsearch.Target{ConversationID: item.ConversationID, MessageID: item.PostID, ThreadID: p.ParentID, Sequence: p.Sequence}}
			if chatsearch.Match(row, q) {
				out = append(out, row)
			}
		}
		return chatsearch.WindowRows(out, q), nil
	}
	return registry.RegisterSource(chatsearch.Saved, chatsearch.SourceFuncs{SearchRows: search, OpenRow: func(ctx context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
		rows, err := search(ctx, chatsearch.Request{Actor: a, At: time.Now().UTC(), OpenIDs: []string{row.ID}})
		for _, current := range rows {
			if current.ID == row.ID && current.Text == row.Text && current.Target == row.Target {
				return true, err
			}
		}
		return false, err
	}})
}
