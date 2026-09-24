package application

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Document chat references. The Markdown syntax, shared with the reader
// (productui/docs_chat_refs.go) and the editor's autocomplete:
//
//	#channel-name               a channel the reader can see, by name
//	[#label](channel:<id>)      a channel by conversation ID (autocomplete)
//	@handle or @<subject-id>    a person; handle is the display name,
//	                            lowercased, words joined by "."
//	[@Name](person:<id>)        a person by subject ID
//	/workspace/app/chat#share=<token>  a chat message permalink
//
// Everything is plain Markdown text, so it round-trips through both panes
// of the split editor unchanged.
const (
	maxDocumentChannelRefs = 32
	maxDocumentPersonRefs  = 32
	maxDocumentMessageRefs = 10
	maxQuotedMessageRunes  = 600
)

var (
	docChannelLinkPattern = regexp.MustCompile(`\]\(channel:([A-Za-z0-9._~:-]{1,256})\)`)
	docPersonLinkPattern  = regexp.MustCompile(`\]\(person:([A-Za-z0-9._~:@-]{1,256})\)`)
	docChannelPattern     = regexp.MustCompile(`(?m)(?:^|[^\p{L}\p{N}_&#/\[])#([\p{L}\p{N}][\p{L}\p{N}_-]{0,79})`)
	docPersonPattern      = regexp.MustCompile(`(?m)(?:^|[^\p{L}\p{N}_.@/\[])@([\p{L}\p{N}](?:[\p{L}\p{N}._-]{0,126}[\p{L}\p{N}])?)`)
	docSharePattern       = regexp.MustCompile(`/(?:workspace/app/chat#share=|chat/share/)([A-Za-z0-9_-]+)`)
)

// documentChatRefs is what a version's Markdown names, as keys.
type documentChatRefs struct {
	channels []string // "id:<id>" or "name:<normalized>"
	people   []string // lowercased handle or subject ID
	messages []string // share tokens
}

// DocumentChannelName normalizes a channel name for "#name" matching:
// lowercase, with runs of spaces, "_" and "-" as one "-".
func DocumentChannelName(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsSpace(r) || r == '_' || r == '-' {
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
			continue
		}
		b.WriteRune(r)
		dash = false
	}
	return strings.TrimRight(b.String(), "-")
}

// DocumentPersonHandle is the "@handle" for a display name: lowercase
// letters and digits, words joined by ".".
func DocumentPersonHandle(name string) string {
	var b strings.Builder
	dot := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dot = false
			continue
		}
		if !dot && b.Len() > 0 {
			b.WriteByte('.')
			dot = true
		}
	}
	return strings.TrimRight(b.String(), ".")
}

func extractDocumentChatRefs(markdown string) documentChatRefs {
	if len(markdown) > 1<<20 {
		markdown = markdown[:1<<20]
	}
	var out documentChatRefs
	seen := map[string]bool{}
	add := func(list *[]string, key string, limit int) {
		if key == "" || seen[key] || len(*list) >= limit {
			return
		}
		seen[key] = true
		*list = append(*list, key)
	}
	for _, m := range docChannelLinkPattern.FindAllStringSubmatch(markdown, -1) {
		add(&out.channels, "id:"+m[1], maxDocumentChannelRefs)
	}
	for _, m := range docChannelPattern.FindAllStringSubmatch(markdown, -1) {
		add(&out.channels, "name:"+DocumentChannelName(m[1]), maxDocumentChannelRefs)
	}
	for _, m := range docPersonLinkPattern.FindAllStringSubmatch(markdown, -1) {
		add(&out.people, "p:"+strings.ToLower(m[1]), maxDocumentPersonRefs)
	}
	for _, m := range docPersonPattern.FindAllStringSubmatch(markdown, -1) {
		add(&out.people, "p:"+strings.ToLower(m[1]), maxDocumentPersonRefs)
	}
	for _, m := range docSharePattern.FindAllStringSubmatch(markdown, -1) {
		add(&out.messages, "m:"+m[1], maxDocumentMessageRefs)
	}
	for i := range out.people {
		out.people[i] = strings.TrimPrefix(out.people[i], "p:")
	}
	for i := range out.messages {
		out.messages[i] = strings.TrimPrefix(out.messages[i], "m:")
	}
	return out
}

// documentChatReader is the slice of the composed chat service a document
// read needs; every call is authorized as the reader.
type documentChatReader interface {
	ListConversations(context.Context, chatcore.ListConversationsRequest) (chatcore.ListConversationsResponse, error)
	ResolveShareLink(context.Context, chatcore.Principal, string) (chatcore.Conversation, *chatcore.Post, error)
}

// documentPerson is one directory row for "@" resolution.
type documentPerson struct{ ID, Name string }

// documentChatReferences resolves what markdown names as the reader. A
// chat failure never fails the document read: channels by ID lock, names
// and messages stay unresolved.
func (s documentService) documentChatReferences(ctx context.Context, tenantID, actorID, markdown string) transportdocument.ChatReferences {
	refs := extractDocumentChatRefs(markdown)
	var out transportdocument.ChatReferences
	if len(refs.channels)+len(refs.people)+len(refs.messages) == 0 {
		return out
	}
	var people []documentPerson
	if s.people != nil && (len(refs.people) > 0 || len(refs.messages) > 0) {
		people, _ = s.people(ctx, tenantID)
	}
	out.People = resolveDocumentPeople(refs.people, people)
	principal, ok := documentChatPrincipal(ctx, tenantID, actorID)
	if !ok || s.chat == nil {
		for _, key := range refs.channels {
			if strings.HasPrefix(key, "id:") {
				out.Channels = append(out.Channels, transportdocument.ChannelReference{Key: key, Locked: true})
			}
		}
		for _, token := range refs.messages {
			out.Messages = append(out.Messages, transportdocument.MessageReference{Token: token})
		}
		return out
	}
	if len(refs.channels) > 0 {
		rooms, _ := documentVisibleRooms(ctx, s.chat, principal)
		out.Channels = resolveDocumentChannels(refs.channels, rooms)
	}
	names := map[string]string{}
	for _, p := range people {
		names[p.ID] = p.Name
	}
	for _, token := range refs.messages {
		out.Messages = append(out.Messages, resolveDocumentMessage(ctx, s.chat, principal, token, names))
	}
	return out
}

func documentChatPrincipal(ctx context.Context, tenantID, actorID string) (chatcore.Principal, bool) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Tenant().String() != tenantID || p.Subject() != actorID {
		return chatcore.Principal{}, false
	}
	return chatcore.Principal{TenantID: tenantID, SubjectID: actorID, Roles: p.Roles()}, true
}

// documentVisibleRooms is the reader's policy-filtered listing: rooms they
// are in plus public channels they could join.
func documentVisibleRooms(ctx context.Context, chat documentChatReader, p chatcore.Principal) ([]chatcore.Conversation, error) {
	var rooms []chatcore.Conversation
	cursor := ""
	for page := 0; page < 10; page++ {
		list, err := chat.ListConversations(ctx, chatcore.ListConversationsRequest{Principal: p, TenantID: p.TenantID, Page: chatcore.Page{Cursor: cursor, PageSize: 200}, IncludeDiscoverable: true})
		if err != nil {
			return rooms, err
		}
		rooms = append(rooms, list.Conversations...)
		if list.NextCursor == "" || list.NextCursor == cursor {
			break
		}
		cursor = list.NextCursor
	}
	return rooms, nil
}

// resolveDocumentChannels matches keys against the reader's visible rooms.
// An ID the reader cannot see locks; a "#name" the reader cannot see is
// dropped, so ordinary prose with a "#" is never turned into a chip.
func resolveDocumentChannels(keys []string, rooms []chatcore.Conversation) []transportdocument.ChannelReference {
	byID := map[string]chatcore.Conversation{}
	byName := map[string]chatcore.Conversation{}
	for _, room := range rooms {
		if room.Archived || (room.Kind != chatcore.PublicChannel && room.Kind != chatcore.PrivateChannel) {
			continue
		}
		byID[room.ID] = room
		if name := DocumentChannelName(room.Name); name != "" {
			if _, dup := byName[name]; !dup || room.Joined {
				byName[name] = room
			}
		}
	}
	var out []transportdocument.ChannelReference
	for _, key := range keys {
		var room chatcore.Conversation
		var ok bool
		switch {
		case strings.HasPrefix(key, "id:"):
			room, ok = byID[strings.TrimPrefix(key, "id:")]
			if !ok {
				out = append(out, transportdocument.ChannelReference{Key: key, Locked: true})
				continue
			}
		case strings.HasPrefix(key, "name:"):
			if room, ok = byName[strings.TrimPrefix(key, "name:")]; !ok {
				continue
			}
		default:
			continue
		}
		out = append(out, transportdocument.ChannelReference{Key: key, ConversationID: room.ID, Name: room.Name, MemberCount: int(room.MemberCount), Joined: room.Joined, Private: room.Kind == chatcore.PrivateChannel})
	}
	return out
}

// resolveDocumentPeople matches "@" keys by subject ID or handle. A handle
// two people share resolves to neither.
func resolveDocumentPeople(keys []string, people []documentPerson) []transportdocument.PersonReference {
	if len(keys) == 0 {
		return nil
	}
	byID := map[string]documentPerson{}
	byHandle := map[string]documentPerson{}
	ambiguous := map[string]bool{}
	for _, p := range people {
		byID[strings.ToLower(p.ID)] = p
		handle := DocumentPersonHandle(p.Name)
		if handle == "" {
			continue
		}
		if prior, dup := byHandle[handle]; dup && prior.ID != p.ID {
			ambiguous[handle] = true
		}
		byHandle[handle] = p
	}
	var out []transportdocument.PersonReference
	for _, key := range keys {
		p, ok := byID[key]
		if !ok && !ambiguous[key] {
			p, ok = byHandle[key]
		}
		if ok {
			out = append(out, transportdocument.PersonReference{Key: key, SubjectID: p.ID, DisplayName: p.Name})
		}
	}
	return out
}

// resolveDocumentMessage opens one permalink through chat's own share-link
// authorization. Anything short of a live post in the reader's tenant is
// unreadable and carries only its token.
func resolveDocumentMessage(ctx context.Context, chat documentChatReader, p chatcore.Principal, token string, names map[string]string) transportdocument.MessageReference {
	ref := transportdocument.MessageReference{Token: token}
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	room, post, err := chat.ResolveShareLink(rctx, p, token)
	if err != nil || post == nil || post.Deleted || room.TenantID != p.TenantID || post.TenantID != p.TenantID || post.ConversationID != room.ID {
		return ref
	}
	body := post.Body
	if utf8.RuneCountInString(body) > maxQuotedMessageRunes {
		body = string([]rune(body)[:maxQuotedMessageRunes]) + "…"
	}
	author := names[post.AuthorID]
	if author == "" {
		author = post.AuthorID
	}
	return transportdocument.MessageReference{Token: token, Readable: true, ConversationID: room.ID, ChannelName: room.Name, PostID: post.ID,
		AuthorID: post.AuthorID, AuthorName: author, Body: body, CreatedAt: post.CreatedAt}
}

// documentPeopleDirectory reads the tenant's worker names for "@"
// resolution, cached per tenant for five minutes.
func documentPeopleDirectory(pool *pgxadapter.Pool) func(context.Context, string) ([]documentPerson, error) {
	if pool == nil {
		return nil
	}
	type entry struct {
		people  []documentPerson
		expires time.Time
	}
	var mu sync.Mutex
	cache := map[string]entry{}
	return func(ctx context.Context, tenantID string) ([]documentPerson, error) {
		mu.Lock()
		if e, ok := cache[tenantID]; ok && time.Now().Before(e.expires) {
			mu.Unlock()
			return e.people, nil
		}
		mu.Unlock()
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		tenantUUID := pgstore.TenantID(tenantID)
		if err := tenancy.WithTenant(ctx, tx, tenantUUID); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (worker_key) worker_key, COALESCE(preferred_name,''), COALESCE(legal_name,'')
			FROM journey_worker WHERE tenant_id=$1 ORDER BY worker_key, revision_sequence DESC LIMIT 20000`, tenantUUID)
		if err != nil {
			return nil, err
		}
		var people []documentPerson
		for rows.Next() {
			var key, preferred, legal string
			if err := rows.Scan(&key, &preferred, &legal); err != nil {
				rows.Close()
				return nil, err
			}
			people = append(people, documentPerson{ID: key, Name: ownerDisplayName(key, preferred, legal)})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		mu.Lock()
		if len(cache) > 64 {
			cache = map[string]entry{}
		}
		cache[tenantID] = entry{people: people, expires: time.Now().Add(5 * time.Minute)}
		mu.Unlock()
		return people, nil
	}
}

// GetDocumentPreviews is the batched unfurl read. Owner names come from
// the directory only for readable rows.
func (s documentService) GetDocumentPreviews(ctx context.Context, tenantID, actorID string, ids []string) ([]transportdocument.Preview, error) {
	rows, err := s.store.DocumentPreviews(ctx, tenantID, actorID, ids)
	if err != nil {
		return nil, err
	}
	var owners []string
	for _, row := range rows {
		if row.Readable && row.OwnerID != "" {
			owners = append(owners, row.OwnerID)
		}
	}
	names := map[string]string{}
	if s.ownerNames != nil && len(owners) > 0 {
		if resolved, err := s.ownerNames(ctx, tenantID, owners); err == nil {
			names = resolved
		}
	}
	out := make([]transportdocument.Preview, 0, len(rows))
	for _, row := range rows {
		if !row.Readable {
			out = append(out, transportdocument.Preview{DocumentID: row.DocumentID})
			continue
		}
		out = append(out, transportdocument.Preview{DocumentID: row.DocumentID, Readable: true, Title: row.Title, OwnerID: row.OwnerID, OwnerName: names[row.OwnerID], Snippet: row.Snippet, UpdatedAt: row.UpdatedAt})
	}
	return out, nil
}

// withDocumentChat lets document reads resolve chat references through the
// composed (routed, audited) chat service. A composition without chat, or
// a chat service without share links, leaves them unresolved.
func withDocumentChat(service transportdocument.Service, chat chatcore.ConversationService) transportdocument.Service {
	ds, ok := service.(documentService)
	if !ok || chat == nil {
		return service
	}
	if reader, ok := chat.(documentChatReader); ok {
		ds.chat = reader
	}
	return ds
}
