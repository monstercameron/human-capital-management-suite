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

// integrate2SearchRowAuthority is the per-row check of the served search: the
// person may be shown a result that sits on a message only if they may read
// that message now.
func integrate2SearchRowAuthority(reader chat.ConversationService) chatstore.ChatSearchAuthority {
	return func(ctx context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
		if row.ID == "" {
			return true, nil
		}
		// A cited document has its own grants, and no document authority is
		// composed here, so a source title is never found on a message's say-so.
		if row.Kind == chatsearch.SourceTitle {
			return false, nil
		}
		// CHATSEARCH-002: an attachment is served to whoever may read its
		// conversation (ChatExtensions.AuthorizeMedia) and is shown on its message,
		// so its name is found by the people who may read that message, which is
		// the check below. Every file used to be withheld. A file that names no
		// message has nothing to be checked against and stays withheld.
		if row.Kind == chatsearch.File && row.Target.MessageID == "" {
			return false, nil
		}
		if row.Target.MessageID == "" {
			return true, nil
		}
		_, err := integrate2ReadPost(ctx, reader, chat.Principal{TenantID: a.HomeTenantID, SubjectID: a.PersonID}, a.TenantID, row.Target.ConversationID, row.Target.MessageID)
		if errors.Is(err, chat.ErrNotFound) || errors.Is(err, chat.ErrPermissionDenied) {
			return false, nil
		}
		return err == nil, err
	}
}

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
	var rows []chatsearch.Row
	var sentences map[string][]string
	var err error
	if timed, ok := s.Source.(integrate2VoiceSentences); ok {
		rows, sentences, err = timed.SearchSentences(ctx, candidates)
	} else {
		rows, err = s.Source.Search(ctx, candidates)
	}
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
			row.Target.Sentence, err = s.sentence(ctx, post, principal, sentences[row.ID], q.Query)
			if err != nil {
				return nil, err
			}
			out = append(out, row)
		}
	}
	return chatsearch.WindowRows(out, q), nil
}

// integrate2VoiceSentences is a voice source that also hands over the timed
// sentences of each transcript it found.
type integrate2VoiceSentences interface {
	SearchSentences(context.Context, chatsearch.Request) ([]chatsearch.Row, map[string][]string, error)
}

// sentence is the sentence a voice result seeks to (CHATSEARCH-002): the first
// one that still holds the searched words in what this reader is shown. A
// sentence is masked like the transcript it is part of, so a word a filter
// hides from the reader never decides where their result opens. Only the
// sentences that hold a searched word as spoken are put through the filters.
func (s integrate2VoiceSource) sentence(ctx context.Context, post chat.Post, reader chat.Principal, spoken []string, query string) (int, error) {
	if chatsearch.Sentence(spoken, query) == 0 {
		return 0, nil
	}
	shown := make([]string, len(spoken))
	for i, text := range spoken {
		if chatsearch.Sentence([]string{text}, query) == 0 {
			continue
		}
		post.Body = text
		masked, err := s.Filter.MaskedBody(ctx, post, reader)
		if err != nil {
			return 0, err
		}
		shown[i] = masked
	}
	return chatsearch.Sentence(shown, query), nil
}

func (s integrate2VoiceSource) CanOpen(ctx context.Context, actor chatsearch.Actor, row chatsearch.Row) (bool, error) {
	rows, err := s.Search(ctx, chatsearch.Request{Actor: actor, At: time.Now(), OpenMessageID: row.Target.MessageID, OpenIDs: []string{row.ID}, Filters: chatsearch.Filters{Conversation: row.Target.ConversationID, Kind: row.Kind}})
	for _, current := range rows {
		// The recheck carries no words, so it names no sentence.
		if current.ID == row.ID && current.Text == row.Text && chatsearch.SameTarget(current.Target, row.Target) {
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
