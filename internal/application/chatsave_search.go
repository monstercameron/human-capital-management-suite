package application

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

type SavedMessagesSearchPort interface {
	SearchSaved(context.Context, chat.Principal, string) ([]chat.SavedItem, error)
}

type chatsaveSearchSource struct{ reader SavedMessagesSearchPort }

func (s chatsaveSearchSource) rows(ctx context.Context, actor chatsearch.Actor) ([]chatsearch.Row, error) {
	p := chat.Principal{TenantID: actor.HomeTenantID, SubjectID: actor.PersonID}
	if err := chat.ValidateSavedOwner(ctx, p, actor.TenantID); err != nil {
		return nil, err
	}
	items, err := s.reader.SearchSaved(ctx, p, "")
	if err != nil {
		return nil, err
	}
	rows := []chatsearch.Row{}
	for _, item := range items {
		if item.HomeTenantID != p.TenantID || item.PersonID != p.SubjectID {
			continue
		}
		identity, _ := json.Marshal([]string{item.TenantID, item.ConversationID, item.PostID})
		id := base64.RawURLEncoding.EncodeToString(identity)
		row := chatsearch.Row{Kind: chatsearch.Saved, ID: id, TenantID: actor.TenantID, OwnerID: item.PersonID, Private: true, Text: item.Note, At: item.CreatedAt, Target: chatsearch.Target{ItemID: id}}
		if item.Availability == "readable" && item.Post != nil && !item.Post.Deleted && item.Post.TenantID == item.TenantID && item.Post.ConversationID == item.ConversationID && item.Post.ID == item.PostID {
			row.Text += "\n" + item.Post.Body
			row.AuthorID = item.Post.AuthorID
			row.InThread = item.Post.ParentID != ""
			row.Target.ConversationID = item.ConversationID
			row.Target.MessageID = item.PostID
			row.Target.ThreadID = item.Post.ParentID
			row.Target.Sequence = item.Post.Sequence
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s chatsaveSearchSource) Search(ctx context.Context, req chatsearch.Request) ([]chatsearch.Row, error) {
	rows, err := s.rows(ctx, req.Actor)
	if err != nil {
		return nil, err
	}
	result := []chatsearch.Row{}
	for _, row := range rows {
		if chatsearch.Match(row, req) {
			result = append(result, row)
		}
	}
	return result, nil
}

func (s chatsaveSearchSource) CanOpen(ctx context.Context, actor chatsearch.Actor, row chatsearch.Row) (bool, error) {
	rows, err := s.rows(ctx, actor)
	if err != nil {
		return false, err
	}
	for _, current := range rows {
		if current.ID == row.ID && current.Text == row.Text && current.Target == row.Target {
			return true, nil
		}
	}
	return false, nil
}

// RegisterSavedMessagesSearch registers the private list with the shared search
// port. Notes remain searchable when source-message access has been revoked.
func RegisterSavedMessagesSearch(registry *chatsearch.Registry, reader SavedMessagesSearchPort) error {
	if registry == nil || reader == nil {
		return chatsearch.ErrInvalid
	}
	return registry.RegisterSource(chatsearch.Saved, chatsaveSearchSource{reader: reader})
}

var _ chatsearch.Source = chatsaveSearchSource{}
