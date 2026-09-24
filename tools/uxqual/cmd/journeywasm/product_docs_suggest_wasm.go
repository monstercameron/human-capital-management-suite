//go:build js && wasm

package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// docsSuggestSources caches the people directory and the channel listing
// the editor's "@" and "#" lists search, per viewer, for a minute. Both are
// the same authorized reads chat makes: the worker directory and the
// policy-filtered discoverable conversation listing.
type docsSuggestSources struct {
	mu       sync.Mutex
	identity string
	people   []chatui.SearchPerson
	peopleAt time.Time
	rooms    []chatui.Conversation
	roomsAt  time.Time
}

var docsSuggestCache = &docsSuggestSources{}

func (s *docsSuggestSources) cached(identity string, now time.Time) (people []chatui.SearchPerson, peopleOK bool, rooms []chatui.Conversation, roomsOK bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.identity != identity {
		*s = docsSuggestSources{identity: identity}
	}
	return s.people, now.Sub(s.peopleAt) < time.Minute, s.rooms, now.Sub(s.roomsAt) < time.Minute
}

func (s *docsSuggestSources) store(identity string, people []chatui.SearchPerson, rooms []chatui.Conversation, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.identity != identity {
		return
	}
	if people != nil {
		s.people, s.peopleAt = people, now
	}
	if rooms != nil {
		s.rooms, s.roomsAt = rooms, now
	}
}

// wireDocsReferenceSuggest answers the split editor's autocomplete.
func wireDocsReferenceSuggest(view *productui.View, service documentv1.DocumentServiceClient, cfg journeyclient.Config) {
	locale := view.Locale.Resolved
	view.SuggestDocsReferences = func(kind, query string, done func([]productui.DocsReferenceSuggestion)) {
		active := chatBrowser.config(cfg)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var rows []productui.DocsReferenceSuggestion
			switch kind {
			case productui.DocsSuggestPeople:
				rows = docsSuggestPeople(docsSuggestDirectory(ctx, active), query)
			case productui.DocsSuggestChannels:
				rows = docsSuggestChannels(docsSuggestRooms(ctx, active), query, productui.DocsChatText(locale, "suggest_members"))
			case productui.DocsSuggestDocs:
				rows = docsSuggestDocuments(chatRPCContext(ctx, active), service, query)
			}
			ui.PostAsync(func() { done(rows) })
		}()
	}
}

func docsSuggestDirectory(ctx context.Context, cfg journeyclient.Config) []chatui.SearchPerson {
	identity := cfg.Tenant + "\x00" + cfg.Subject
	people, fresh, _, _ := docsSuggestCache.cached(identity, time.Now())
	if fresh || chatWorkers == nil {
		return people
	}
	result, err := chatWorkers.ListWorkers(chatRPCContext(ctx, cfg), &journeyv1.ListWorkersRequest{})
	if err != nil {
		return people
	}
	people = chatSearchDirectoryFromWorkers(result.GetWorkers())
	docsSuggestCache.store(identity, people, nil, time.Now())
	return people
}

func docsSuggestRooms(ctx context.Context, cfg journeyclient.Config) []chatui.Conversation {
	identity := cfg.Tenant + "\x00" + cfg.Subject
	_, _, rooms, fresh := docsSuggestCache.cached(identity, time.Now())
	client := chatBrowser.conversationClient()
	if fresh || client == nil {
		return rooms
	}
	var listed []chatui.Conversation
	cursor := ""
	for page := 0; page < 5; page++ {
		result, err := client.ListConversations(chatRPCContext(ctx, cfg), &chatv1.ListConversationsRequest{TenantId: cfg.Tenant, Cursor: cursor, PageSize: 100, IncludeDiscoverable: true})
		if err != nil {
			return rooms
		}
		for _, conversation := range result.GetConversations() {
			if conversation != nil && conversation.GetTenantId() == cfg.Tenant && !conversation.GetArchived() {
				listed = append(listed, chatConversation(conversation))
			}
		}
		if result.GetNextCursor() == "" || result.GetNextCursor() == cursor {
			break
		}
		cursor = result.GetNextCursor()
	}
	docsSuggestCache.store(identity, nil, listed, time.Now())
	return listed
}

// docsSuggestDocuments searches the documents the viewer can open; the
// service never lists one they cannot.
func docsSuggestDocuments(ctx context.Context, service documentv1.DocumentServiceClient, query string) []productui.DocsReferenceSuggestion {
	if service == nil {
		return nil
	}
	request := &documentv1.ListDocumentsRequest{PageSize: 8, Query: strings.TrimSpace(query)}
	if request.Query != "" {
		request.SearchMode = "contains"
	}
	response, err := service.ListDocuments(ctx, request)
	if err != nil {
		return nil
	}
	names := chatDirectorySnapshot()
	rows := make([]productui.DocsReferenceSuggestion, 0, len(response.GetDocuments()))
	for _, document := range response.GetDocuments() {
		id, title := document.GetDocumentId(), strings.TrimSpace(document.GetTitle())
		if id == "" || title == "" {
			continue
		}
		rows = append(rows, productui.DocsReferenceSuggestion{ID: id, Label: title, Detail: names[document.GetOwnerId()], Insert: productui.DocsSuggestDocInsert(id, title)})
	}
	return rows
}
