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
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatsearchHTTPFixture struct {
	calls int
	q     chatsearch.Request
	err   error
}

func (f *chatsearchHTTPFixture) Search(_ context.Context, q chatsearch.Request) (chatsearch.Response, error) {
	f.calls++
	f.q = q
	return chatsearch.Response{Mode: "keyword", CountScope: "page"}, f.err
}

func chatsearchTestHTTP(t *testing.T, port ChatSearchPort, history ChatSearchHistory) http.Handler {
	t.Helper()
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant"), Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "search-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:search-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return OverlayChatSearch(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), ChatSearchHTTP{Port: port, History: history}, transport.Config{Verifier: trust.VerifierFunc(func(_ context.Context, c trust.Credential) (*trust.Principal, error) {
		if c.Token != "fixture" {
			return nil, trust.ErrInvalidCredential
		}
		return p, nil
	})})
}

func chatsearchHTTPCall(h http.Handler, method, path, body, bearer string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestTodo_CHATSEARCH_002(t *testing.T) {
	f := &chatsearchHTTPFixture{}
	history := chatsearch.NewRecent()
	h := chatsearchTestHTTP(t, f, history)
	w := chatsearchHTTPCall(h, "POST", ChatSearchPath, `{"Query":"budget has:file","Limit":20}`, "fixture")
	if w.Code != 200 || f.calls != 1 || f.q.Actor != (chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}) || f.q.At.IsZero() || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("search %d %s %+v", w.Code, w.Body, f.q)
	}
	w = chatsearchHTTPCall(h, "GET", ChatSearchPath+"/recent", "", "fixture")
	var recent []string
	if err := json.Unmarshal(w.Body.Bytes(), &recent); err != nil || len(recent) != 1 || recent[0] != "budget has:file" {
		t.Fatalf("recent %s %v", w.Body, err)
	}
	w = chatsearchHTTPCall(h, "DELETE", ChatSearchPath+"/recent", "", "fixture")
	if w.Code != 204 || len(history.List(f.q.Actor)) != 0 {
		t.Fatal("clear failed")
	}
	if w = chatsearchHTTPCall(h, "GET", "/other", "", ""); w.Code != 204 {
		t.Fatal("unrelated route intercepted")
	}
}

func TestTodo_CHATSEARCH_002_Security(t *testing.T) {
	f := &chatsearchHTTPFixture{}
	h := chatsearchTestHTTP(t, f, chatsearch.NewRecent())
	for _, body := range []string{`{"Actor":{"PersonID":"bob"},"Query":"budget"}`, `{"At":"2026-01-01T00:00:00Z","Query":"budget"}`, `{"OpenIDs":["hidden"],"Query":"budget"}`, `{"BeforeKey":"hidden","Query":"budget"}`, `{"Query":"budget"} {}`, strings.Repeat("x", 65537)} {
		w := chatsearchHTTPCall(h, "POST", ChatSearchPath, body, "fixture")
		if w.Code != 400 || f.calls != 0 {
			t.Fatalf("forged request reached port: %d %d", w.Code, f.calls)
		}
	}
	if w := chatsearchHTTPCall(h, "POST", ChatSearchPath, `{"Query":"budget"}`, ""); w.Code < 400 || f.calls != 0 {
		t.Fatal("anonymous request reached port")
	}
	for _, failure := range []struct {
		err  error
		code string
	}{{chatsearch.ErrInvalid, "invalid"}, {chatsearch.ErrUnavailable, "meaning_unavailable"}, {errors.New("private database detail"), "unavailable"}} {
		f.err = failure.err
		w := chatsearchHTTPCall(h, "POST", ChatSearchPath, `{"Query":"budget"}`, "fixture")
		if !strings.Contains(w.Body.String(), failure.code) || strings.Contains(w.Body.String(), "private database") {
			t.Fatalf("unsafe error %s", w.Body)
		}
	}
	if _, err := NewChatSearch(nil, nil, nil); !errors.Is(err, chatsearch.ErrInvalid) {
		t.Fatal("missing authorities accepted")
	}
}

type chatsearchPeopleFixture struct{ allowed bool }

func (f *chatsearchPeopleFixture) GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error) {
	if !f.allowed {
		return chat.Conversation{}, chat.ErrPermissionDenied
	}
	return chat.Conversation{ID: "room", TenantID: "tenant"}, nil
}

func (f *chatsearchPeopleFixture) ListConversations(_ context.Context, q chat.ListConversationsRequest) (chat.ListConversationsResponse, error) {
	if !f.allowed || q.Principal.SubjectID != "alice" {
		return chat.ListConversationsResponse{}, nil
	}
	return chat.ListConversationsResponse{Conversations: []chat.Conversation{{ID: "room", TenantID: "tenant"}}}, nil
}
func (f *chatsearchPeopleFixture) SuggestReferences(context.Context, chat.SuggestReferencesRequest) ([]chat.ReferenceCandidate, error) {
	return []chat.ReferenceCandidate{{Reference: chat.Reference{Kind: chat.PersonMention, ID: "sam", TenantID: "tenant", Display: "Sam Budget"}, Eligible: true}, {Reference: chat.Reference{ID: "hidden", Display: "Hidden Budget"}}}, nil
}

func TestTodo_CHATSEARCH_001(t *testing.T) {
	r := chatsearch.NewRegistry()
	f := &chatsearchPeopleFixture{true}
	if err := RegisterChatSearchPeople(r, f); err != nil {
		t.Fatal(err)
	}
	composed, err := NewChatSearchRendering(&chatstore.Store{}, f, func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }, nil)
	if err != nil || composed.ValidateStoredKinds([]chatsearch.Kind{chatsearch.Message, chatsearch.Thread, chatsearch.File, chatsearch.Pin, chatsearch.Todo, chatsearch.Poll, chatsearch.AgentAnswer, chatsearch.Conversation, chatsearch.Person}) != nil {
		t.Fatalf("composition missing existing source: %v", err)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "budget", Filters: chatsearch.Filters{Kind: chatsearch.Person}, At: time.Now().UTC()}
	got, err := r.Search(context.Background(), q)
	if err != nil || len(got.Groups) != 1 || got.Groups[0].Count != 1 || got.Groups[0].Rows[0].Target.ItemID != "sam" {
		t.Fatalf("people %+v %v", got, err)
	}
	f.allowed = false
	got, err = r.Search(context.Background(), q)
	if err != nil || len(got.Groups) != 0 {
		t.Fatalf("revoked people %+v %v", got, err)
	}
	if !errors.Is(RegisterChatSearchPeople(nil, f), chatsearch.ErrInvalid) {
		t.Fatal("nil registry accepted")
	}
	// Non-nil persistence without a reader still fails before any store call.
	if _, err = NewChatSearch(&chatstore.Store{}, nil, nil); !errors.Is(err, chatsearch.ErrInvalid) {
		t.Fatal("missing reader accepted")
	}
}

type chatsearchSavedFixture struct{ readable bool }

func (f *chatsearchSavedFixture) SearchSaved(_ context.Context, p chat.Principal, tenant, query string) ([]chat.SavedItem, error) {
	if p.SubjectID != "alice" || tenant != "tenant" || query != "" {
		return nil, chatsearch.ErrInvalid
	}
	item := chat.SavedItem{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice", ConversationID: "room", PostID: "post", Note: "private budget note", CreatedAt: time.Now().UTC()}
	if f.readable {
		item.Post = &chat.Post{ID: "post", TenantID: "tenant", ConversationID: "room", Body: "budget", Sequence: 17}
	}
	other := item
	other.PersonID = "bob"
	return []chat.SavedItem{item, other}, nil
}

func TestTodo_CHATSEARCH_001_Saved(t *testing.T) {
	f := &chatsearchSavedFixture{true}
	r := chatsearch.NewRegistry()
	if err := RegisterChatSearchSaved(r, f); err != nil {
		t.Fatal(err)
	}
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}, Query: "private", Filters: chatsearch.Filters{Kind: chatsearch.Saved}, At: time.Now().UTC()}
	got, err := r.Search(context.Background(), q)
	if err != nil || len(got.Groups) != 1 || got.Groups[0].Count != 1 || got.Groups[0].Rows[0].Target.Sequence != 17 {
		t.Fatalf("saved %+v %v", got, err)
	}
	f.readable = false
	got, err = r.Search(context.Background(), q)
	if err != nil || len(got.Groups) != 0 {
		t.Fatalf("restricted saved note leaked: %+v %v", got, err)
	}
	if !errors.Is(RegisterChatSearchSaved(r, nil), chatsearch.ErrInvalid) {
		t.Fatal("nil saved reader accepted")
	}
}
