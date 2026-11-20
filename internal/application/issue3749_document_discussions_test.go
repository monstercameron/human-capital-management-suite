package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3749Refs struct{ refs []chatstore.PostReference }

func (r issue3749Refs) ListPostReferencesByTarget(_ context.Context, _, kind, target string, _ int) ([]chatstore.PostReference, error) {
	var out []chatstore.PostReference
	for _, ref := range r.refs {
		if ref.Kind == kind && ref.TargetID == target {
			out = append(out, ref)
		}
	}
	return out, nil
}

type issue3749Docs map[string]bool

func (d issue3749Docs) CanRead(_ context.Context, _, id, kind, reader string) error {
	if kind != "person" || reader == "" || !d[id] {
		return documenthubstore.ErrDenied
	}
	return nil
}

// issue3749Rooms answers per conversation and per message; a conversation key
// ending in "/" is the conversation itself.
type issue3749Rooms struct {
	readable map[string]chat.Post
	rooms    map[string]chat.Conversation
	fail     bool
}

func (r issue3749Rooms) ReadAuthorizedReference(_ context.Context, p chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	if r.fail {
		return chat.Conversation{}, nil, context.DeadlineExceeded
	}
	room, ok := r.rooms[conversation]
	if !ok || p.TenantID != tenant {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	if post == "" {
		return room, nil, nil
	}
	found, ok := r.readable[conversation+"/"+post]
	if !ok {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	return room, &found, nil
}

func issue3749Ref(conversation, post string, minute int) chatstore.PostReference {
	return chatstore.PostReference{TenantID: "tenant-a", PostID: post, ConversationID: conversation, Kind: chatstore.PostReferenceDocument, TargetID: "doc-1", PostedAt: time.Date(2026, 11, 20, 9, minute, 0, 0, time.UTC)}
}

func issue3749Service(rooms issue3749Rooms, refs []chatstore.PostReference, docs issue3749Docs) *DocumentDiscussions {
	return &DocumentDiscussions{Chat: rooms, Refs: issue3749Refs{refs}, Docs: docs}
}

func issue3749Fixture() issue3749Rooms {
	return issue3749Rooms{
		rooms: map[string]chat.Conversation{
			"general": {ID: "general", Kind: chat.PublicChannel, Name: "general"},
			"dm":      {ID: "dm", Kind: chat.Direct, Name: "dana, lee"},
		},
		readable: map[string]chat.Post{
			"general/m1": {ID: "m1", ConversationID: "general", Sequence: 7, Body: "see doc:doc-1"},
			"general/m2": {ID: "m2", ConversationID: "general", Sequence: 9, Body: "again doc:doc-1"},
			"dm/m3":      {ID: "m3", ConversationID: "dm", Sequence: 2, Body: "doc:doc-1"},
		},
	}
}

// The document lists the conversations that mention it, newest first, one row
// per conversation with the count and a link to the latest message.
func TestIssue3749_ADocumentListsTheConversationsThatMentionIt_Integration(t *testing.T) {
	refs := []chatstore.PostReference{issue3749Ref("dm", "m3", 30), issue3749Ref("general", "m2", 20), issue3749Ref("general", "m1", 10)}
	got, err := issue3749Service(issue3749Fixture(), refs, issue3749Docs{"doc-1": true}).List(context.Background(), issue3790Principal(t, "dana"), "doc-1")
	if err != nil || got.Incomplete || len(got.Items) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	dm, general := got.Items[0], got.Items[1]
	if !dm.Direct || dm.Name != "" || dm.Messages != 1 || dm.Href != "/workspace/app/chat#channel=dm&message=m3&at=2" {
		t.Fatalf("dm %+v", dm)
	}
	if general.Name != "general" || general.Direct || general.Messages != 2 || general.Href != "/workspace/app/chat#channel=general&message=m2&at=9" || general.LatestAt != "2026-11-20T09:20:00Z" {
		t.Fatalf("general %+v", general)
	}
}

// A conversation the reader cannot open, and a message in an open one that the
// reader cannot read, leave no row, no name, no count and no link.
func TestIssue3749_LockedConversationsAndMessagesAreWithheldEntirely_Security(t *testing.T) {
	rooms := issue3749Fixture()
	rooms.rooms["board"] = chat.Conversation{ID: "board", Kind: chat.PrivateChannel, Name: "board-secret"}
	delete(rooms.rooms, "board") // the reader is not a member: reading it is denied
	refs := []chatstore.PostReference{issue3749Ref("board", "b1", 40), issue3749Ref("general", "hidden", 30), issue3749Ref("general", "m1", 10)}
	got, err := issue3749Service(rooms, refs, issue3749Docs{"doc-1": true}).List(context.Background(), issue3790Principal(t, "dana"), "doc-1")
	if err != nil || len(got.Items) != 1 || got.Items[0].ConversationID != "general" || got.Items[0].Messages != 1 || got.Incomplete {
		t.Fatalf("%+v %v", got, err)
	}
	body, _ := json.Marshal(got)
	for _, leak := range []string{"board", "hidden", "b1"} {
		if strings.Contains(string(body), leak) {
			t.Fatalf("%q leaked: %s", leak, body)
		}
	}
	// Only locked conversations: an empty, complete answer, not an error.
	got, err = issue3749Service(rooms, refs[:1], issue3749Docs{"doc-1": true}).List(context.Background(), issue3790Principal(t, "dana"), "doc-1")
	if err != nil || len(got.Items) != 0 || got.Incomplete {
		t.Fatalf("locked only %+v %v", got, err)
	}
}

// A document the reader cannot read answers not found before any conversation
// is read, so the answer never says whether the document is discussed.
func TestIssue3749_ADocumentTheReaderCannotReadIsNotFound_Security(t *testing.T) {
	refs := []chatstore.PostReference{issue3749Ref("general", "m1", 10)}
	svc := issue3749Service(issue3749Fixture(), refs, issue3749Docs{})
	if _, err := svc.List(context.Background(), issue3790Principal(t, "dana"), "doc-1"); !errors.Is(err, ErrChatWorkNotFound) {
		t.Fatalf("err %v", err)
	}
	if _, err := svc.List(context.Background(), issue3790Principal(t, "dana"), " doc-1"); !errors.Is(err, ErrChatWorkInvalid) {
		t.Fatalf("blank-padded id err %v", err)
	}
}

// A conversation that cannot be checked is reported as incomplete, never as absent.
func TestIssue3749_AConversationThatCannotBeCheckedIsSaidNotHidden(t *testing.T) {
	rooms := issue3749Fixture()
	rooms.fail = true
	got, err := issue3749Service(rooms, []chatstore.PostReference{issue3749Ref("general", "m1", 10)}, issue3749Docs{"doc-1": true}).List(context.Background(), issue3790Principal(t, "dana"), "doc-1")
	if err != nil || len(got.Items) != 0 || !got.Incomplete {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestIssue3749_TheRouteIsHumansOnlyNoStoreAndReadOnly_Security(t *testing.T) {
	svc := issue3749Service(issue3749Fixture(), []chatstore.PostReference{issue3749Ref("general", "m1", 10)}, issue3749Docs{"doc-1": true})
	handler := DocumentDiscussionsHandler{Discussions: svc}
	request := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, nil)
		r = r.WithContext(trust.WithPrincipal(r.Context(), issue3790Principal(t, "dana")))
		handler.ServeHTTP(w, r)
		return w
	}
	ok := request(http.MethodGet, "/api/chat/documents/doc-1/discussions")
	if ok.Code != http.StatusOK || ok.Header().Get("Cache-Control") != "no-store" || !strings.Contains(ok.Body.String(), `"general"`) {
		t.Fatalf("get %d %s", ok.Code, ok.Body)
	}
	if w := request(http.MethodPost, "/api/chat/documents/doc-1/discussions"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post %d", w.Code)
	}
	if w := request(http.MethodGet, "/api/chat/documents/doc-9/discussions"); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "general") {
		t.Fatalf("unreadable %d %s", w.Code, w.Body)
	}
	if w := request(http.MethodGet, "/api/chat/documents/a/b/discussions"); w.Code != http.StatusNotFound {
		t.Fatalf("nested path %d", w.Code)
	}
	anon := httptest.NewRecorder()
	handler.ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/api/chat/documents/doc-1/discussions", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous %d", anon.Code)
	}
}

// A message the reader may open but sees masked or replaced by a stand-in no
// longer names the document in the reader's view, so it is withheld.
func TestIssue3749_AMessageMaskedForTheReaderIsWithheld_Security(t *testing.T) {
	rooms := issue3749Fixture()
	rooms.readable["general/m2"] = chat.Post{ID: "m2", ConversationID: "general", Sequence: 9, Body: "[hidden by policy]"}
	refs := []chatstore.PostReference{issue3749Ref("general", "m2", 20), issue3749Ref("general", "m1", 10)}
	got, err := issue3749Service(rooms, refs, issue3749Docs{"doc-1": true}).List(context.Background(), issue3790Principal(t, "dana"), "doc-1")
	if err != nil || len(got.Items) != 1 || got.Items[0].Messages != 1 || !strings.Contains(got.Items[0].Href, "message=m1") {
		t.Fatalf("%+v %v", got, err)
	}
	if got, _ = issue3749Service(rooms, refs[:1], issue3749Docs{"doc-1": true}).List(context.Background(), issue3790Principal(t, "dana"), "doc-1"); len(got.Items) != 0 {
		t.Fatalf("a masked message produced a row: %+v", got)
	}
}
