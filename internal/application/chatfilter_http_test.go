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

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatfilterHTTPStore struct {
	chatfilter.Store
	defs   []chatfilter.Definition
	writes int
}

func (s *chatfilterHTTPStore) Enablements(context.Context, string) ([]chatfilter.Enablement, error) {
	return []chatfilter.Enablement{}, nil
}

func (s *chatfilterHTTPStore) Definitions(context.Context, string) ([]chatfilter.Definition, error) {
	return s.defs, nil
}
func (s *chatfilterHTTPStore) CreateVersion(_ context.Context, _ string, d chatfilter.Definition) error {
	s.defs = append(s.defs, d)
	s.writes++
	return nil
}
func (s *chatfilterHTTPStore) PutEnablement(context.Context, string, chatfilter.Enablement) error {
	s.writes++
	return nil
}
func (s *chatfilterHTTPStore) Hits(context.Context, string) ([]chatfilter.Record, error) {
	return []chatfilter.Record{}, nil
}

type chatfilterHTTPAuthority struct{ denied bool }

func (a chatfilterHTTPAuthority) AuthorizeFilters(context.Context, chatfilter.Actor, string) error {
	if a.denied {
		return chatfilter.ErrDenied
	}
	return nil
}
func (a chatfilterHTTPAuthority) CanReadFilterConversation(context.Context, chatfilter.Actor, string) bool {
	return false
}

type chatfilterHTTPFacts struct{ Roles []string }

func (f chatfilterHTTPFacts) ResolveChatFacts(_ context.Context, tenant, subject string, _ time.Time) (chatpolicy.Principal, error) {
	roles := f.Roles
	if roles == nil {
		roles = []string{"hcm_admin"}
	}
	return chatpolicy.Principal{ID: subject, Tenant: tenant, Active: true, AuthorityRevision: 1, Roles: roles}, nil
}
func filterHTTPContext(t *testing.T) context.Context {
	t.Helper()
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "admin", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "filter-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture-digest"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(t.Context(), p)
}
func TestTodo_CHATMOD_003(t *testing.T) {
	store := &chatfilterHTTPStore{}
	service := &chatfilter.Service{Store: store, Registry: chatfilter.NewRegistry(), Authority: chatfilterHTTPAuthority{}}
	handler := ChatFilterHandler{Service: service}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, ChatFiltersPath+path, strings.NewReader(body)).WithContext(filterHTTPContext(t))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	d := chatfilter.Definition{ID: "project", Name: "Project rule", Version: "1.0.0", Kind: "words", Match: []string{"quartz"}, Action: "block"}
	body, _ := json.Marshal(chatFilterRequest{Definition: d, Sample: "quartz"})
	for _, path := range []string{"/try", "/versions"} {
		w := request("POST", path, string(body))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if path == "/try" && (!strings.Contains(w.Body.String(), `"Action":"block"`) || store.writes != 0) {
			t.Fatal("Try it performed a write")
		}
	}
	for _, path := range []string{"", "/hits", "/enablements"} {
		w := request("GET", path, "")
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
	}
	enable, _ := json.Marshal(chatFilterRequest{Enablement: chatfilter.Enablement{RuleID: d.ID}})
	for _, path := range []string{"/enable", "/disable"} {
		w := request("POST", path, string(enable))
		if w.Code != 200 {
			t.Fatal(path, w.Body.String())
		}
	}
	for _, body := range []string{`{"unknown":true}`, `{} {}`, strings.Repeat(" ", 64<<10) + `{}`} {
		w := request("POST", "/versions", body)
		if w.Code != 400 {
			t.Fatalf("unbounded/invalid body %d", w.Code)
		}
	}
	w := request("DELETE", "", "")
	if w.Code != 405 || w.Header().Get("Allow") != "GET, POST" {
		t.Fatal("method guard")
	}
	if request("GET", "/missing", "").Code != 404 {
		t.Fatal("unknown path")
	}
	if request("GET", "/hits?before=invalid", "").Code != 400 {
		t.Fatal("invalid hit cursor accepted")
	}
}

type chatfilterMembership struct{ membership chat.Membership }

func (f chatfilterMembership) GetMembership(context.Context, string, string, string, string) (chat.Membership, error) {
	return f.membership, nil
}
func TestTodo_CHATMOD_003_Authority(t *testing.T) {
	ctx := filterHTTPContext(t)
	a := chatfilter.Actor{Tenant: "tenant-a", Subject: "admin"}
	now := time.Now().UTC()
	authority := ChatFilterAuthority{Facts: chatfilterHTTPFacts{}, Now: func() time.Time { return now }}
	if err := authority.AuthorizeFilters(ctx, a, ""); err != nil {
		t.Fatal("current admin denied", err)
	}
	if authority.CanReadFilterConversation(ctx, a, "private") {
		t.Fatal("admin bought private access")
	}
	if err := authority.AuthorizeFilters(t.Context(), a, ""); !errors.Is(err, chatfilter.ErrDenied) {
		t.Fatal("forged principal allowed")
	}
	conversations := newChatServiceStub()
	authority.Facts = chatfilterHTTPFacts{Roles: []string{}}
	authority.Conversations = conversations
	authority.Membership = chatfilterMembership{membership: chat.Membership{Role: chat.Manager, JoinedAt: &now}}
	if err := authority.AuthorizeFilters(ctx, a, "c"); err != nil {
		t.Fatal("channel manager denied", err)
	}
	if err := authority.AuthorizeFilters(ctx, a, ""); !errors.Is(err, chatfilter.ErrDenied) {
		t.Fatal("channel manager controls workspace")
	}
	authority.Membership = chatfilterMembership{membership: chat.Membership{Role: chat.Member, JoinedAt: &now}}
	if err := authority.AuthorizeFilters(ctx, a, "c"); !errors.Is(err, chatfilter.ErrDenied) {
		t.Fatal("ordinary member managed")
	}
}
func TestTodo_CHATMOD_003_Browser(t *testing.T) {
	service := &chatfilter.Service{Store: &chatfilterHTTPStore{}, Registry: chatfilter.NewRegistry(), Authority: chatfilterHTTPAuthority{}}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		node, err := RenderChatFilterPanel(filterHTTPContext(t), service, chatfilter.Actor{Tenant: "tenant-a", Subject: "admin"}, chatui.Model{Locale: locale})
		if err != nil {
			t.Fatal(err)
		}
		markup, err := ui.RenderToString(node)
		if err != nil || !strings.Contains(markup, "chatfilter-panel") {
			t.Fatal("SSR page absent", err)
		}
	}
	service.Authority = chatfilterHTTPAuthority{denied: true}
	if _, err := RenderChatFilterPanel(t.Context(), service, chatfilter.Actor{Tenant: "tenant-a", Subject: "member"}, chatui.Model{}); !errors.Is(err, chatfilter.ErrDenied) {
		t.Fatal("unauthorized page rendered")
	}
}
func TestTodo_CHATMOD_003_Security(t *testing.T) {
	store := &chatfilterHTTPStore{}
	handler := ChatFilterHandler{Service: &chatfilter.Service{Store: store, Registry: chatfilter.NewRegistry(), Authority: chatfilterHTTPAuthority{denied: true}}}
	r := httptest.NewRequest("POST", ChatFiltersPath+"/versions", strings.NewReader(`{}`)).WithContext(filterHTTPContext(t))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 || store.writes != 0 {
		t.Fatal("authorization followed effect")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", ChatFiltersPath, nil))
	if w.Code != 401 {
		t.Fatal("unauthenticated read")
	}
	nextCalls := 0
	overlay := OverlayChatFilters(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalls++; w.WriteHeader(204) }), nil, transport.Config{})
	w = httptest.NewRecorder()
	overlay.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != 204 || nextCalls != 1 {
		t.Fatal("unrelated route consumed")
	}
}
func TestTodo_CHATMOD_002(t *testing.T) {
	status, body := ChatFilterErrorBody(&chatfilter.BlockedError{RuleName: "Project rule", Span: chatfilter.Span{Start: 6, End: 12}})
	if status != 422 || body.Code != "content_blocked" || body.Span.Start != 6 {
		t.Fatal("blocked draft coordinates lost")
	}
	for _, fixture := range []struct {
		err    error
		status int
	}{{chatfilter.ErrInvalid, 400}, {chatfilter.ErrDenied, 403}, {chatfilter.ErrConflict, 409}, {errors.New("private database error"), 503}} {
		status, body := ChatFilterErrorBody(fixture.err)
		if status != fixture.status || strings.Contains(body.Code, "database") {
			t.Fatal("typed safe error")
		}
	}
	now := time.Now().UTC()
	identity := chatFilterIdentity{facts: chatfilterHTTPFacts{}, now: func() time.Time { return now }}
	in, err := identity.FilterInput(filterHTTPContext(t), chat.Principal{TenantID: "tenant-a", SubjectID: "admin", Roles: []string{"forged"}})
	if err != nil || len(in.Roles) != 1 || in.Roles[0] != "hcm_admin" {
		t.Fatalf("current roles %+v %v", in, err)
	}
	if _, err = identity.FilterInput(t.Context(), chat.Principal{TenantID: "tenant-a", SubjectID: "admin"}); !errors.Is(err, chatfilter.ErrDenied) {
		t.Fatal("missing identity accepted")
	}
	missing := chatFilterIdentity{}
	in, err = missing.FilterInput(filterHTTPContext(t), chat.Principal{TenantID: "tenant-a", SubjectID: "admin"})
	if err != nil || len(in.Roles) != 0 {
		t.Fatal("missing facts granted an exemption or disabled policy", err)
	}
	expired := chatFilterIdentity{now: func() time.Time { return now.Add(24 * time.Hour) }}
	if _, err = expired.FilterInput(filterHTTPContext(t), chat.Principal{TenantID: "tenant-a", SubjectID: "admin"}); !errors.Is(err, chatfilter.ErrDenied) {
		t.Fatal("expired identity admitted")
	}
	service := NewChatFilterService(nil, nil, nil)
	if service.Store == nil || service.Registry == nil || service.Delivery == nil {
		t.Fatal("composition incomplete")
	}
	if newChatFilterPolicy(nil, nil, func() time.Time { return now }) == nil || chatFilterBoundary(chat.NewService(nil, nil)) == nil {
		t.Fatal("served filter hooks missing")
	}
}
