package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

type ChatSearchPeopleReader interface {
	ListConversations(context.Context, chat.ListConversationsRequest) (chat.ListConversationsResponse, error)
	SuggestReferences(context.Context, chat.SuggestReferencesRequest) ([]chat.ReferenceCandidate, error)
}

// RegisterChatSearchPeople uses current authorized directory facts, never the
// display-name snapshot on a historical mention. The reference reader owns
// policy and tenant visibility before any name is matched.
func RegisterChatSearchPeople(registry *chatsearch.Registry, reader ChatSearchPeopleReader) error {
	if registry == nil || reader == nil {
		return chatsearch.ErrInvalid
	}
	search := func(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
		p := chat.Principal{TenantID: q.Actor.HomeTenantID, SubjectID: q.Actor.PersonID}
		out := []chatsearch.Row{}
		seen := map[string]bool{}
		cursor := ""
		for {
			rooms, err := reader.ListConversations(ctx, chat.ListConversationsRequest{Principal: p, TenantID: q.Actor.TenantID, Page: chat.Page{PageSize: 200, Cursor: cursor}})
			if err != nil {
				return nil, err
			}
			for _, room := range rooms.Conversations {
				if q.Filters.Conversation != "" && q.Filters.Conversation != room.ID {
					continue
				}
				people, err := reader.SuggestReferences(ctx, chat.SuggestReferencesRequest{Principal: p, TenantID: q.Actor.TenantID, ConversationID: room.ID, Kind: chat.PersonMention})
				if err != nil {
					return nil, err
				}
				for _, person := range people {
					if !person.Eligible || seen[person.TenantID+":"+person.ID] {
						continue
					}
					row := chatsearch.Row{Kind: chatsearch.Person, ID: person.TenantID + ":" + person.ID, TenantID: q.Actor.TenantID, Text: person.Display, AuthorID: person.ID, Target: chatsearch.Target{ConversationID: room.ID, ItemID: person.ID}}
					if chatsearch.Match(row, q) {
						out = append(out, row)
						seen[row.ID] = true
					}
				}
			}
			if rooms.NextCursor == "" {
				return chatsearch.WindowRows(out, q), nil
			}
			if rooms.NextCursor == cursor {
				return nil, chatsearch.ErrInvalid
			}
			cursor = rooms.NextCursor
		}
	}
	return registry.RegisterSource(chatsearch.Person, chatsearch.SourceFuncs{SearchRows: search, OpenRow: func(ctx context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
		rows, err := search(ctx, chatsearch.Request{Actor: a, Filters: chatsearch.Filters{Conversation: row.Target.ConversationID}, At: time.Now().UTC(), OpenIDs: []string{row.ID}})
		for _, current := range rows {
			if current.ID == row.ID && current.Text == row.Text {
				return true, err
			}
		}
		return false, err
	}})
}
