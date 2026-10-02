package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatsaveHTTPPort struct {
	calls   int
	request chat.SavedRequest
	change  chat.SavedChange
	list    chat.SavedListRequest
	err     error
}

func (f *chatsaveHTTPPort) SaveForLater(_ context.Context, r chat.SavedRequest) (chat.SavedItem, error) {
	f.calls++
	f.request = r
	return chat.SavedItem{PostID: r.PostID}, f.err
}
func (f *chatsaveHTTPPort) Unsave(_ context.Context, r chat.SavedRequest) error {
	f.calls++
	f.request = r
	return f.err
}
func (f *chatsaveHTTPPort) UpdateSaved(_ context.Context, r chat.SavedRequest, c chat.SavedChange) (chat.SavedItem, error) {
	f.calls++
	f.request = r
	f.change = c
	return chat.SavedItem{}, f.err
}
func (f *chatsaveHTTPPort) ListSaved(_ context.Context, r chat.SavedListRequest) (chat.SavedPage, error) {
	f.calls++
	f.list = r
	return chat.SavedPage{Items: []chat.SavedItem{}}, f.err
}
func (f *chatsaveHTTPPort) SearchSaved(_ context.Context, p chat.Principal, query string) ([]chat.SavedItem, error) {
	f.calls++
	f.request = chat.SavedRequest{Principal: p, TenantID: p.TenantID, PostID: query}
	return []chat.SavedItem{}, f.err
}

func chatsaveHTTPContext(t *testing.T, kind trust.SubjectKind) context.Context {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "person", SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "session", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func TestTodo_CHATSAVE_001(t *testing.T) {
	ctx := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	port := &chatsaveHTTPPort{}
	h := SavedMessagesHandler{Service: port}
	for _, action := range []string{"save", "remove", "done", "reopen", "note", "due"} {
		body := `{"action":"` + action + `","conversation_id":"room","post_id":"post","note":"private","set_due":true,"due_at":null}`
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, SavedMessagesPath, strings.NewReader(body)).WithContext(ctx))
		if w.Code != 200 {
			t.Fatalf("%s=%d %s", action, w.Code, w.Body.String())
		}
		if port.request.Principal.SubjectID != "person" || port.request.Principal.TenantID != "tenant-a" || port.request.TenantID != "tenant-a" {
			t.Fatal("request identity not derived from session")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, SavedMessagesPath+"?tab=done&limit=3", nil).WithContext(ctx))
	if w.Code != 200 || port.list.Tab != chat.SavedDone || port.list.Page.PageSize != 3 {
		t.Fatal("list filters")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, SavedMessagesPath+"?query=in%3A%20saved%20private", nil).WithContext(ctx))
	if w.Code != 200 || port.request.PostID != "in: saved private" {
		t.Fatal("search")
	}
	port.err = chat.ErrSavedLimit
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, SavedMessagesPath, strings.NewReader(`{"action":"save","conversation_id":"room","post_id":"post"}`)).WithContext(ctx))
	var response SavedMessagesError
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	if w.Code != 409 || response.Code != "saved_limit" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("typed limit error")
	}
}

func TestTodo_CHATSAVE_001_Security(t *testing.T) {
	port := &chatsaveHTTPPort{}
	h := SavedMessagesHandler{Service: port}
	for _, ctx := range []context.Context{context.Background(), chatsaveHTTPContext(t, trust.SubjectKindAgent), chatsaveHTTPContext(t, trust.SubjectKindService)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, SavedMessagesPath, nil).WithContext(ctx))
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("machine or anonymous accepted: %d", w.Code)
		}
	}
	ctx := chatsaveHTTPContext(t, trust.SubjectKindHuman)
	for _, body := range []string{`{"action":"save","person_id":"victim"}`, `{"action":"save"} {}`, `{"action":"note"}`, `{"action":"due"}`, `{"action":"unknown"}`, strings.Repeat("x", 25<<10)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, SavedMessagesPath, strings.NewReader(body)).WithContext(ctx))
		if w.Code != 400 {
			t.Fatalf("malformed=%d", w.Code)
		}
	}
	if port.calls != 0 {
		t.Fatal("side effect before validation")
	}
	w := httptest.NewRecorder()
	OverlaySavedMessages(nil, port, transport.Config{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, SavedMessagesPath, nil))
	if w.Code == 200 || port.calls != 0 {
		t.Fatal("admission bypass")
	}
	w = httptest.NewRecorder()
	OverlaySavedMessages(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), port, transport.Config{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/elsewhere", nil))
	if w.Code != 204 {
		t.Fatal("fallback changed")
	}
}

type chatsaveCurrentConversation struct {
	chat.ConversationService
	denied bool
}

func (s chatsaveCurrentConversation) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	if s.denied {
		return chat.Conversation{}, chat.ErrPermissionDenied
	}
	return chat.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Kind: chat.PrivateChannel, Revision: 1}, nil
}

func TestTodo_CHATSAVE_001_Composition(t *testing.T) {
	var absent *ChatExtensions
	if absent.SavedMessages() != nil || (&ChatExtensions{}).SavedMessages() != nil {
		t.Fatal("absent composition accepted")
	}
	current := chatsaveCurrentConversation{}
	extension := &ChatExtensions{Conversations: current, TodoStore: &chatstore.Store{}}
	if extension.SavedMessages() == nil {
		t.Fatal("service not composed")
	}
	a := chatsaveAuthority{current: current}
	in, err := a.ReadSavedConversation(context.Background(), chat.GetConversationRequest{Principal: chat.Principal{TenantID: "tenant", SubjectID: "person"}, TenantID: "tenant", ConversationID: "room"})
	if err != nil || in.ID != "room" {
		t.Fatalf("current authority=%+v %v", in, err)
	}
	a.current = chatsaveCurrentConversation{denied: true}
	if _, err = a.ReadSavedConversation(context.Background(), chat.GetConversationRequest{}); err != chat.ErrPermissionDenied {
		t.Fatal("revocation ignored")
	}
	if _, err = a.Authorize(context.Background(), chat.Principal{}, chat.Conversation{}, chatpolicy.ActionRead, time.Now()); err != chat.ErrUnavailable {
		t.Fatal("saved adapter inferred authority")
	}
}
