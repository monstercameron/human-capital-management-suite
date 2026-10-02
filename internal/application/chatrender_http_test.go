package application

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

type chatrenderHTTPFixture struct {
	authorized, called int
	deny               bool
	scope              chatstore.RenderingScope
	rendering          chatrender.Rendering
	settings           chatrender.Preference
}

func (f *chatrenderHTTPFixture) AuthorizeRendering(_ context.Context, s chatstore.RenderingScope, _ string) error {
	f.authorized++
	f.scope = s
	if f.deny {
		return chatrender.ErrDenied
	}
	return nil
}
func (f *chatrenderHTTPFixture) ListRenderings(context.Context, chatstore.RenderingScope, []string) ([]chatrender.Rendering, error) {
	f.called++
	return []chatrender.Rendering{f.rendering}, nil
}
func (f *chatrenderHTTPFixture) RequestRendering(_ context.Context, _ chatstore.RenderingScope, r chatrender.Rendering) error {
	f.called++
	f.rendering = r
	return nil
}
func (f *chatrenderHTTPFixture) LanguageSettings(context.Context, chatstore.RenderingScope, string) (chatrender.Preference, error) {
	f.called++
	return f.settings, nil
}
func (f *chatrenderHTTPFixture) PutLanguageSettings(_ context.Context, _ chatstore.RenderingScope, p chatrender.Preference) error {
	f.called++
	f.settings = p
	return nil
}
func (f *chatrenderHTTPFixture) ReportRendering(context.Context, chatstore.RenderingScope, chatrender.Rendering, string) error {
	f.called++
	return nil
}
func (f *chatrenderHTTPFixture) CorrectRevisionLanguage(context.Context, chatstore.RenderingScope, string, uint64, string) error {
	f.called++
	return nil
}
func (f *chatrenderHTTPFixture) ConversationLanguages(context.Context, chatstore.RenderingScope) (map[string]int, error) {
	f.called++
	return map[string]int{"de": 2}, nil
}
func (f *chatrenderHTTPFixture) ReadRenderingSelection(context.Context, chatstore.RenderingScope, string) (chatrender.Rendering, chatrender.Mark, error) {
	f.called++
	return f.rendering, chatrender.Mark{State: "ready"}, nil
}
func chatrenderRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "render-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-digest"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(trust.WithPrincipal(context.Background(), p))
	request.Header.Set("Content-Type", "application/json")
	return request
}
func TestTodo_CHATRENDER_001(t *testing.T) {
	fixture := &chatrenderHTTPFixture{rendering: chatrender.Rendering{Text: "safe view"}, settings: chatrender.DefaultPreference("de")}
	handler := ChatRenderingHandler{Port: fixture}
	for _, test := range []struct{ action, method, body string }{{"list?message=post", "GET", ""}, {"request", "POST", `{"rendering":{"message":"post","revision":1,"tone":"as-written","language":"de","tenant":"forged"}}`}, {"report", "POST", `{"rendering":{"message":"post"},"reason":"meaning"}`}, {"selection?message=post", "GET", ""}, {"languages", "GET", ""}, {"correct-language", "POST", `{"message":"post","revision":1,"language":"de"}`}} {
		separator := "?"
		if strings.Contains(test.action, "?") {
			separator = "&"
		}
		w := httptest.NewRecorder()
		before := fixture.called
		handler.ServeHTTP(w, chatrenderRequest(t, test.method, ChatRenderingPath+"/"+test.action+separator+"conversation=room", test.body))
		if w.Code != http.StatusOK || fixture.called != before+1 || fixture.scope.Principal.SubjectID != "alice" || fixture.scope.Tenant != "tenant-a" || fixture.scope.Conversation != "room" {
			t.Fatal(test.action, w.Code, w.Body.String(), fixture)
		}
	}
	if fixture.rendering.Tenant != "tenant-a" {
		t.Fatal("JSON forged tenant accepted", fixture.rendering)
	}
	nextCalls := 0
	overlay := OverlayChatRenderings(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { nextCalls++; w.WriteHeader(204) }), nil, transport.Config{})
	w := httptest.NewRecorder()
	overlay.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != 204 || nextCalls != 1 {
		t.Fatal(w.Code, nextCalls)
	}
	overlay = OverlayChatRenderings(nil, nil, transport.Config{})
	w = httptest.NewRecorder()
	overlay.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	overlay.ServeHTTP(w, httptest.NewRequest("GET", ChatRenderingPath+"/list?conversation=room", nil))
	if w.Code == 200 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestTodo_CHATRENDER_001_Security(t *testing.T) {
	fixture := &chatrenderHTTPFixture{deny: true}
	handler := ChatRenderingHandler{Port: fixture}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, chatrenderRequest(t, "POST", ChatRenderingPath+"/request?conversation=room", `{"rendering":{"message":"post"}}`))
	if w.Code != 403 || fixture.called != 0 || fixture.authorized != 1 {
		t.Fatal(w.Code, fixture)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", ChatRenderingPath+"/list?conversation=room", nil))
	if w.Code != 403 || fixture.authorized != 1 {
		t.Fatal(w.Code, fixture)
	}
	fixture.deny = false
	for _, body := range []string{`{"unexpected":true}`, `{} {}`, strings.Repeat("x", 17000)} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, chatrenderRequest(t, "POST", ChatRenderingPath+"/request?conversation=room", body))
		if w.Code != 400 || fixture.called != 0 {
			t.Fatal(w.Code, fixture)
		}
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, chatrenderRequest(t, "GET", ChatRenderingPath+"/list", ""))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, chatrenderRequest(t, "DELETE", ChatRenderingPath+"/list?conversation=room", ""))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, chatrenderRequest(t, "GET", ChatRenderingPath+"/unknown?conversation=room", ""))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	// CHATBUG-012: a read of an unwired service is a typed 200 "not available";
	// a write to it still fails.
	ChatRenderingHandler{}.ServeHTTP(w, chatrenderRequest(t, "GET", ChatRenderingPath+"/list?conversation=room", ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	ChatRenderingHandler{}.ServeHTTP(w, chatrenderRequest(t, "POST", ChatRenderingPath+"/request?conversation=room", `{}`))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	var surface *ChatRenderingSurface
	if surface.AuthorizeRendering(context.Background(), chatstore.RenderingScope{}, "selection") != chatrender.ErrUnavailable {
		t.Fatal("unwired policy allowed")
	}
	if _, _, err := surface.ReadRenderingSelection(context.Background(), chatstore.RenderingScope{}, "post"); err != chatrender.ErrUnavailable {
		t.Fatal(err)
	}
}
func TestTodo_CHATLANG_002(t *testing.T) {
	f := &chatrenderHTTPFixture{settings: chatrender.DefaultPreference("ar")}
	handler := ChatRenderingHandler{Port: f}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, chatrenderRequest(t, "GET", ChatRenderingPath+"/settings", ""))
	var pref chatrender.Preference
	if err := json.Unmarshal(w.Body.Bytes(), &pref); err != nil || w.Code != 200 || pref.ReadingLanguage != "ar" {
		t.Fatal(w.Code, pref, err)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, chatrenderRequest(t, "PUT", ChatRenderingPath+"/settings", `{"settings":{"tone":"as-written","reading_language":"de","translate":true,"further_languages":["fr"],"source_overrides":{"es":false}}}`))
	if w.Code != 200 || f.settings.ReadingLanguage != "de" || !f.settings.Translate || f.scope.Principal.SubjectID != "alice" {
		t.Fatal(w.Code, f)
	}
}

type chatrenderPolicyFixture struct {
	policy chatrender.Policy
	deny   bool
}
type chatrenderLocaleFixture struct{}

func (chatrenderLocaleFixture) DefaultConversationLanguages(context.Context, chatstore.RenderingScope) (map[string]int, error) {
	return map[string]int{"en": 1}, nil
}

func (f chatrenderPolicyFixture) AuthorizeRendering(context.Context, chatstore.RenderingScope, string) error {
	if f.deny {
		return chatrender.ErrDenied
	}
	return nil
}
func (f chatrenderPolicyFixture) RenderingPolicy(context.Context, chatstore.RenderingScope, string) (chatrender.Policy, error) {
	return f.policy, nil
}
func TestTodo_CHATRENDER_001_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	store, err := chatstore.New(ctx, chatstore.Config{DSN: u.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	adapter := &chatstore.RenderingAdapter{Adapter: chatstore.NewAdapter(store)}
	scope := chatstore.RenderingScope{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, Tenant: "tenant-a", Conversation: "room"}
	c := chat.Conversation{ID: "room", TenantID: scope.Tenant, Kind: chat.PrivateChannel, OwnerID: "alice", Revision: 1}
	if _, err = adapter.CreateConversation(ctx, c, []chat.Membership{{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	post, err := adapter.SendPost(ctx, chat.SendPostRequest{Principal: scope.Principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "test"}, chat.Post{ID: "post", TenantID: c.TenantID, ConversationID: c.ID, AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "Bitte lesen wir die Nachricht heute mit dem Team.", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	original := chatrender.Rendering{Tenant: scope.Tenant, Message: post.ID, Revision: post.Revision, Tone: chatrender.AsWritten, Language: "de", SourceLanguage: "de", Text: post.Body}
	surface := &ChatRenderingSurface{Store: store, InterfaceLocale: "en", Policy: chatrenderPolicyFixture{policy: chatrender.Policy{Original: original, AllowOriginal: true, AllowedKinds: []chatrender.Kind{chatrender.Translate}}}}
	surface.Locales = chatrenderLocaleFixture{}
	counts, err := surface.ConversationLanguages(ctx, scope)
	if err != nil || counts["en"] != 1 || counts["und"] != 0 {
		t.Fatal(counts, err)
	}
	pref := chatrender.DefaultPreference("en")
	pref.Translate = true
	if err = surface.PutLanguageSettings(ctx, scope, pref); err != nil {
		t.Fatal(err)
	}
	r, mark, err := surface.ReadRenderingSelection(ctx, scope, post.ID)
	if err != nil || r.Text != "" || mark.State != "pending" {
		t.Fatal(r, mark, err)
	}
	job, err := store.ClaimRendering(ctx, scope.Tenant, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r, err = (chatrender.FixtureProducer{Text: "This is the translated message.", Language: "en"}).Produce(ctx, job.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteRendering(ctx, job, r); err != nil {
		t.Fatal(err)
	}
	r, mark, err = surface.ReadRenderingSelection(ctx, scope, post.ID)
	if err != nil || r.Text != "This is the translated message." || mark.State != "ready" {
		t.Fatal(r, mark, err)
	}
	listed, err := surface.ListRenderings(ctx, scope, []string{post.ID})
	if err != nil || len(listed) != 1 {
		t.Fatal(listed, err)
	}
	searched, err := surface.SearchRenderings(ctx, scope, "translated")
	if err != nil || len(searched) != 1 {
		t.Fatal(searched, err)
	}
	surface.Policy = chatrenderPolicyFixture{policy: chatrender.Policy{Original: original, RequireMask: true, AllowedKinds: []chatrender.Kind{chatrender.Mask, chatrender.Translate}}}
	listed, err = surface.ListRenderings(ctx, scope, []string{post.ID})
	if err != nil || len(listed) != 0 {
		t.Fatal("forbidden rendering listed", listed, err)
	}
	searched, err = surface.SearchRenderings(ctx, scope, "translated")
	if err != nil || len(searched) != 0 {
		t.Fatal("forbidden rendering searched", searched, err)
	}
	r, mark, err = surface.ReadRenderingSelection(ctx, scope, post.ID)
	if err != nil || r.Text != "" || mark.State != "pending" {
		t.Fatal("mask upgrade", r, mark, err)
	}
	job, err = store.ClaimRendering(ctx, scope.Tenant, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r, err = (chatrender.FixtureProducer{Text: "A neutral view.", Language: "en"}).Produce(ctx, job.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteRendering(ctx, job, r); err != nil {
		t.Fatal(err)
	}
	r, mark, err = surface.ReadRenderingSelection(ctx, scope, post.ID)
	if err != nil || r.Text != "A neutral view." || mark.CanShowOriginal || len(mark.Kinds) != 2 || mark.Kinds[0] != chatrender.Mask {
		t.Fatal(r, mark, err)
	}
	surface.Policy = chatrenderPolicyFixture{deny: true}
	if _, _, err = surface.ReadRenderingSelection(ctx, scope, post.ID); err != chatrender.ErrDenied {
		t.Fatal(err)
	}
	original.Tenant = "other"
	surface.Policy = chatrenderPolicyFixture{policy: chatrender.Policy{Original: original}}
	if _, _, err = surface.ReadRenderingSelection(ctx, scope, post.ID); err != chat.ErrPermissionDenied {
		t.Fatal(err)
	}
}
