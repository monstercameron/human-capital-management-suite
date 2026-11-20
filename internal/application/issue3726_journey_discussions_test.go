package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3726Rooms map[string]chat.Conversation

func (r issue3726Rooms) ReadAuthorizedReference(_ context.Context, p chat.Principal, tenant, conversation, _ string) (chat.Conversation, *chat.Post, error) {
	room, ok := r[conversation]
	if !ok || p.TenantID != tenant {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	return room, nil, nil
}

type issue3726Store struct {
	links    []chatstore.WorkLink
	recorded []chatstore.WorkLink
}

func (s *issue3726Store) ListWorkLinksBySource(_ context.Context, _, kind, source string, _ int) ([]chatstore.WorkLink, error) {
	var out []chatstore.WorkLink
	for _, l := range s.links {
		if l.Kind == kind && l.SourceID == source {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *issue3726Store) LinkWork(_ context.Context, l chatstore.WorkLink) error {
	s.recorded = append(s.recorded, l)
	return nil
}

func issue3726Service(rooms issue3726Rooms, store *issue3726Store, journeys ...string) *JourneyDiscussions {
	byID := map[string]*intentsv1.IntentInstance{}
	for _, id := range journeys {
		byID[id] = &intentsv1.IntentInstance{IntentId: id}
	}
	return &JourneyDiscussions{Chat: rooms, Links: store, Intents: &issue3791Intents{byID: byID}, Now: func() time.Time { return time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC) }}
}

func issue3726Link(conversation string) chatstore.WorkLink {
	return chatstore.WorkLink{TenantID: "tenant-a", ConversationID: conversation, Kind: chatstore.WorkLinkJourneyShare, SourceID: "j1", ProposedByID: "dana", LinkedAt: time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)}
}

// A reader sees the conversations they can open, with a link to each; a direct
// message is named by the page, not by a stored title.
func TestIssue3726_DiscussedInListsTheConversationsTheReaderCanOpen_Integration(t *testing.T) {
	rooms := issue3726Rooms{
		"general": {ID: "general", Kind: chat.PublicChannel, Name: "general"},
		"dm":      {ID: "dm", Kind: chat.Direct, Name: "dana, lee"},
	}
	store := &issue3726Store{links: []chatstore.WorkLink{issue3726Link("general"), issue3726Link("dm")}}
	got, err := issue3726Service(rooms, store, "j1").List(context.Background(), issue3790Principal(t, "dana"), "j1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Incomplete {
		t.Fatalf("items %+v", got)
	}
	if first := got.Items[0]; first.Name != "general" || first.Direct || first.Href != "/workspace/app/chat#channel=general" || first.SharedAt != "2026-11-19T08:00:00Z" {
		t.Fatalf("first %+v", first)
	}
	if dm := got.Items[1]; !dm.Direct || dm.Name != "" {
		t.Fatalf("a direct message carried a stored name: %+v", dm)
	}
}

// A conversation the reader cannot open leaves no row, no name and no count.
func TestIssue3726_DiscussedInHidesConversationsTheReaderCannotOpen_Security(t *testing.T) {
	rooms := issue3726Rooms{"general": {ID: "general", Kind: chat.PublicChannel, Name: "general"}}
	store := &issue3726Store{links: []chatstore.WorkLink{issue3726Link("board-only"), issue3726Link("general")}}
	got, err := issue3726Service(rooms, store, "j1").List(context.Background(), issue3790Principal(t, "dana"), "j1")
	if err != nil || len(got.Items) != 1 || got.Items[0].ConversationID != "general" {
		t.Fatalf("%+v %v", got, err)
	}
	body, _ := json.Marshal(got)
	if strings.Contains(string(body), "board-only") {
		t.Fatalf("a restricted conversation leaked: %s", body)
	}
	store.links = []chatstore.WorkLink{issue3726Link("board-only")}
	if got, err = issue3726Service(rooms, store, "j1").List(context.Background(), issue3790Principal(t, "dana"), "j1"); err != nil || len(got.Items) != 0 || got.Incomplete {
		t.Fatalf("restricted-only %+v %v", got, err)
	}
}

// A journey the reader cannot open answers not found before any conversation is read.
func TestIssue3726_DiscussedInOfAJourneyTheReaderCannotOpenIsNotFound_Security(t *testing.T) {
	rooms := &issue3790Chat{readable: map[string]string{"general/": ""}}
	store := &issue3726Store{links: []chatstore.WorkLink{issue3726Link("general")}}
	svc := issue3726Service(nil, store)
	svc.Chat = rooms
	if _, err := svc.List(context.Background(), issue3790Principal(t, "dana"), "j1"); err != ErrChatWorkNotFound {
		t.Fatalf("err %v", err)
	}
	if rooms.reads != 0 {
		t.Fatalf("read %d conversations for a journey the reader cannot open", rooms.reads)
	}
	if err := svc.Record(context.Background(), issue3790Principal(t, "dana"), "j1", "general"); err != ErrChatWorkNotFound || len(store.recorded) != 0 {
		t.Fatalf("recorded for an unreadable journey: %v %+v", err, store.recorded)
	}
}

type issue3726Failing struct{}

func (issue3726Failing) ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error) {
	return chat.Conversation{}, nil, context.DeadlineExceeded
}

func TestIssue3726_AConversationThatCannotBeCheckedIsSaidNotHidden(t *testing.T) {
	store := &issue3726Store{links: []chatstore.WorkLink{issue3726Link("general")}}
	svc := issue3726Service(nil, store, "j1")
	svc.Chat = issue3726Failing{}
	got, err := svc.List(context.Background(), issue3790Principal(t, "dana"), "j1")
	if err != nil || len(got.Items) != 0 || !got.Incomplete {
		t.Fatalf("%+v %v", got, err)
	}
}

// Recording needs both the journey and the conversation; it writes the sharer's id.
func TestIssue3726_RecordingAShareChecksBothSides_Security(t *testing.T) {
	rooms := issue3726Rooms{"general": {ID: "general", Kind: chat.PublicChannel, Name: "general"}}
	store := &issue3726Store{}
	svc := issue3726Service(rooms, store, "j1")
	ctx := context.Background()
	if err := svc.Record(ctx, issue3790Principal(t, "dana"), "j1", "general"); err != nil {
		t.Fatal(err)
	}
	if len(store.recorded) != 1 || store.recorded[0].Kind != chatstore.WorkLinkJourneyShare || store.recorded[0].SourceID != "j1" || store.recorded[0].ConversationID != "general" || store.recorded[0].ProposedByID != "dana" || store.recorded[0].TenantID != "tenant-a" {
		t.Fatalf("recorded %+v", store.recorded)
	}
	store.recorded = nil
	if err := svc.Record(ctx, issue3790Principal(t, "dana"), "j1", "board-only"); err != ErrChatWorkNotFound || len(store.recorded) != 0 {
		t.Fatalf("recorded a conversation the sharer cannot open: %v %+v", err, store.recorded)
	}
	if err := svc.Record(ctx, issue3790Principal(t, "dana"), "j1", " "); err != ErrChatWorkInvalid {
		t.Fatalf("blank conversation: %v", err)
	}
}

func issue3726Serve(t *testing.T, method, target, body string, port JourneyDiscussionsPort, p *trust.Principal) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if p != nil {
		req = req.WithContext(trust.WithPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	JourneyDiscussionsHandler{Discussions: port}.ServeHTTP(rec, req)
	return rec
}

func TestIssue3726_RoutesAnswerWithTheStatusesTheClientReads_Integration(t *testing.T) {
	rooms := issue3726Rooms{"general": {ID: "general", Kind: chat.PublicChannel, Name: "general"}}
	store := &issue3726Store{links: []chatstore.WorkLink{issue3726Link("general")}}
	svc := issue3726Service(rooms, store, "j1")
	dana := issue3790Principal(t, "dana")
	rec := issue3726Serve(t, http.MethodGet, "/api/chat/journeys/j1/discussions", "", svc, dana)
	var got JourneyDiscussionsResult
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || len(got.Items) != 1 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("list %d %s", rec.Code, rec.Body)
	}
	if rec = issue3726Serve(t, http.MethodPost, "/api/chat/journeys/j1/discussions", `{"conversation_id":"general"}`, svc, dana); rec.Code != http.StatusNoContent || len(store.recorded) != 1 {
		t.Fatalf("record %d %s", rec.Code, rec.Body)
	}
	if rec = issue3726Serve(t, http.MethodPost, "/api/chat/journeys/j1/discussions", `{"conversation_id":"general","extra":1}`, svc, dana); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field %d", rec.Code)
	}
	if rec = issue3726Serve(t, http.MethodGet, "/api/chat/journeys/other/discussions", "", svc, dana); rec.Code != http.StatusNotFound {
		t.Fatalf("unreadable journey %d", rec.Code)
	}
	if rec = issue3726Serve(t, http.MethodDelete, "/api/chat/journeys/j1/discussions", "", svc, dana); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method %d", rec.Code)
	}
	if rec = issue3726Serve(t, http.MethodGet, "/api/chat/journeys/j1/discussions", "", svc, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous %d", rec.Code)
	}
	if rec = issue3726Serve(t, http.MethodGet, "/api/chat/journeys/j1/discussions", "", nil, dana); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no service %d", rec.Code)
	}
}
