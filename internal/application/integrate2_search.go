package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

func integrate2RegisterFilters(registry *chatsearch.Registry, service *chatfilter.Service) error {
	search := func(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
		if q.Actor.TenantID != q.Actor.HomeTenantID {
			return nil, nil
		}
		actor := chatfilter.Actor{Tenant: q.Actor.TenantID, Subject: q.Actor.PersonID, Channel: q.Filters.Conversation}
		definitions, err := service.SearchFilters(ctx, actor, "")
		if errors.Is(err, chatfilter.ErrDenied) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		rows := []chatsearch.Row{}
		for _, definition := range definitions {
			row := chatsearch.Row{Kind: chatsearch.FilterDefinition, ID: definition.ID, TenantID: actor.Tenant, Text: strings.Join([]string{definition.Name, definition.Kind, definition.Action}, " "), Target: chatsearch.Target{ConversationID: actor.Channel, ItemID: definition.ID}}
			if chatsearch.Match(row, q) {
				rows = append(rows, row)
			}
		}
		return chatsearch.WindowRows(rows, q), nil
	}
	return registry.RegisterSource(chatsearch.FilterDefinition, chatsearch.SourceFuncs{SearchRows: search, OpenRow: func(ctx context.Context, actor chatsearch.Actor, row chatsearch.Row) (bool, error) {
		rows, err := search(ctx, chatsearch.Request{Actor: actor, At: time.Now(), Filters: chatsearch.Filters{Conversation: row.Target.ConversationID}, OpenIDs: []string{row.ID}})
		for _, current := range rows {
			if current.ID == row.ID && current.Text == row.Text && current.Target == row.Target {
				return true, err
			}
		}
		return false, err
	}})
}

type integrate2VoiceSource struct {
	Source chatsearch.Source
	Reader chat.ConversationService
	Filter *chat.FilterContentPolicy
}

func (s integrate2VoiceSource) Search(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
	candidates := q
	candidates.Query = ""
	candidates.Filters = chatsearch.Filters{Conversation: q.Filters.Conversation, Kind: q.Filters.Kind}
	rows, err := s.Source.Search(ctx, candidates)
	if err != nil {
		return nil, err
	}
	out := []chatsearch.Row{}
	principal := chat.Principal{TenantID: q.Actor.HomeTenantID, SubjectID: q.Actor.PersonID}
	for _, row := range rows {
		if _, err := s.Reader.GetConversation(ctx, chat.GetConversationRequest{Principal: principal, TenantID: q.Actor.TenantID, ConversationID: row.Target.ConversationID}); err != nil {
			if errors.Is(err, chat.ErrPermissionDenied) || errors.Is(err, chat.ErrNotFound) {
				continue
			}
			return nil, err
		}
		post, err := integrate2ReadPost(ctx, s.Reader, principal, q.Actor.TenantID, row.Target.ConversationID, row.Target.MessageID)
		if errors.Is(err, chat.ErrPermissionDenied) || errors.Is(err, chat.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		post.Body = row.Text
		row.Text, err = s.Filter.MaskedBody(ctx, post, principal)
		if err != nil {
			return nil, err
		}
		if chatsearch.Match(row, q) {
			out = append(out, row)
		}
	}
	return chatsearch.WindowRows(out, q), nil
}
func (s integrate2VoiceSource) CanOpen(ctx context.Context, actor chatsearch.Actor, row chatsearch.Row) (bool, error) {
	rows, err := s.Search(ctx, chatsearch.Request{Actor: actor, At: time.Now(), OpenMessageID: row.Target.MessageID, OpenIDs: []string{row.ID}, Filters: chatsearch.Filters{Conversation: row.Target.ConversationID, Kind: row.Kind}})
	for _, current := range rows {
		if current.ID == row.ID && current.Text == row.Text && current.Target == row.Target {
			return true, err
		}
	}
	return false, err
}
func integrate2RegisterVoice(registry *chatsearch.Registry, adapter *chatstore.Adapter, reader chat.ConversationService, filter *chat.FilterContentPolicy) error {
	authorize := func(ctx context.Context, actor chatsearch.Actor, row chatsearch.Row) (bool, error) {
		p := chat.Principal{TenantID: actor.HomeTenantID, SubjectID: actor.PersonID}
		_, err := reader.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: actor.TenantID, ConversationID: row.Target.ConversationID})
		if err == nil {
			_, err = integrate2ReadPost(ctx, reader, p, actor.TenantID, row.Target.ConversationID, row.Target.MessageID)
		}
		if errors.Is(err, chat.ErrPermissionDenied) || errors.Is(err, chat.ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	}
	for _, kind := range []chatsearch.Kind{chatsearch.Voice, chatsearch.VoiceCorrection} {
		source := &chatstore.ChatvoiceSearchSource{Adapter: adapter, Kind: kind, Authorize: authorize}
		if err := registry.RegisterSource(kind, integrate2VoiceSource{Source: source, Reader: reader, Filter: filter}); err != nil {
			return err
		}
	}
	return nil
}
