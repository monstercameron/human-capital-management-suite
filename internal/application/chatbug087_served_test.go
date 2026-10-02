package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// chatbug087Served is a really composed Chat served through the agent assembly
// over a database the way a separated-role cell runs it: the serving role holds
// only the schema's default table privileges (SELECT, INSERT, UPDATE).
type chatbug087Served struct {
	t       *testing.T
	handler http.Handler
	bearer  string
	room    string
	extra   map[string]string
}

func newChatbug087Served(t *testing.T, runtimeRole bool, facts string) *chatbug087Served {
	t.Helper()
	ctx := context.Background()
	chatDB, coreDB := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
	role := ""
	if runtimeRole {
		role = "chatbug087_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
		chatDB.Exec(t, `CREATE ROLE `+role+` LOGIN`)
		t.Cleanup(func() {
			_ = chatDB.ExecErr(`DROP OWNED BY ` + role)
			_ = chatDB.ExecErr(`DROP ROLE ` + role)
		})
		chatDB.Exec(t, `GRANT USAGE ON SCHEMA `+chatDB.Schema+` TO `+role)
		chatDB.Exec(t, `ALTER DEFAULT PRIVILEGES IN SCHEMA `+chatDB.Schema+` GRANT SELECT, INSERT, UPDATE ON TABLES TO `+role)
		chatDB.Exec(t, `ALTER DEFAULT PRIVILEGES IN SCHEMA `+chatDB.Schema+` GRANT USAGE, SELECT ON SEQUENCES TO `+role)
	}
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, chatDB.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	databaseURL := func(raw, schema, user string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if user != "" {
			parsed.User = url.User(user)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	core, err := pgxadapter.NewPool(ctx, databaseURL(coreDB.URL, coreDB.Schema, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema, role), ChatCursorKey: "chatbug-087-served-key"}, time.Now, chatAdminFacts{role: facts}, core, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(deployment.close)
	rig := &chatlangRig{t: t}
	alice := chatcore.Principal{TenantID: "host", SubjectID: "alice"}
	room, err := deployment.service.CreateConversation(rig.as("alice"), chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PublicChannel, Name: "ops", IdempotencyKey: "chatbug-087-room"})
	if err != nil {
		t.Fatal(err)
	}
	admission, bearer := integrate1Admission(t, "host", "alice", time.Now)
	return &chatbug087Served{t: t, handler: (&agentServedAssembly{Renderings: deployment.renderings}).Overlay(http.NotFoundHandler(), admission), bearer: bearer, room: room.ID}
}

func (s *chatbug087Served) call(method, target string, body any) (int, string) {
	s.t.Helper()
	reader := strings.NewReader("")
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			s.t.Fatal(err)
		}
		reader = strings.NewReader(string(raw))
	}
	r := httptest.NewRequest(method, target, reader)
	r.Header.Set("Authorization", s.bearer)
	r.Header.Set("Accept", "application/json")
	for k, v := range s.extra {
		r.Header.Set(k, v)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// chatbug087Save saves a reading language the way the page does: a PUT of
// {"settings": ...} to the settings route, with or without a conversation.
func chatbug087Save(t *testing.T, s *chatbug087Served, language, conversation string) {
	t.Helper()
	pref := chatrender.DefaultPreference("en")
	pref.ReadingLanguage, pref.Translate = language, true
	target := ChatRenderingPath + "/settings"
	if conversation != "" {
		target += "?conversation=" + url.QueryEscape(conversation)
	}
	if code, body := s.call(http.MethodPut, target, map[string]any{"settings": pref}); code != http.StatusOK {
		t.Fatalf("saving a reading language (conversation %q) answered %d %s", conversation, code, body)
	}
}

// TestTodo_CHATBUG_087_Integration saves the reading language through the real
// route, over a really composed Chat, with the runtime database role: the
// choice is stored, comes back, and a save made after another client saved is
// not refused (the route carries no revision, so there is nothing to go stale).
func TestTodo_CHATBUG_087_Integration(t *testing.T) {
	s := newChatbug087Served(t, true, "hcm_admin")
	chatbug087Save(t, s, "de", "")
	read := func(target string) chatrender.Preference {
		code, body := s.call(http.MethodGet, target, nil)
		var got chatrender.Preference
		if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil {
			t.Fatalf("read %s answered %d %s", target, code, body)
		}
		return got
	}
	if got := read(ChatRenderingPath + "/settings"); got.ReadingLanguage != "de" || !got.Translate {
		t.Fatalf("the saved choice did not come back: %+v", got)
	}
	chatbug087Save(t, s, "fr", s.room)
	if got := read(ChatRenderingPath + "/settings?conversation=" + url.QueryEscape(s.room)); got.ReadingLanguage != "fr" {
		t.Fatalf("the conversation's choice did not come back: %+v", got)
	}
	// Another client of the same person saves, then the first client saves: both
	// are accepted, the later one stands.
	var group sync.WaitGroup
	for _, language := range []string{"es", "pt", "ja", "hi"} {
		group.Add(1)
		go func() {
			defer group.Done()
			chatbug087Save(t, s, language, "")
		}()
	}
	group.Wait()
	chatbug087Save(t, s, "ar", "")
	if got := read(ChatRenderingPath + "/settings"); got.ReadingLanguage != "ar" {
		t.Fatalf("the last save did not stand: %+v", got)
	}

	// The channel's translation setting in Conversation details loads and saves
	// through the same assembly for a person who administers the channel.
	if code, body := s.call(http.MethodGet, ChatlangPath+"/settings?conversation="+url.QueryEscape(s.room), nil); code != http.StatusOK {
		t.Fatalf("the channel translation setting did not load: %d %s", code, body)
	}
	if code, body := s.call(http.MethodPost, ChatlangPath+"/channel", map[string]any{"conversation": s.room, "translation": "on", "external": "inherit"}); code != http.StatusOK {
		t.Fatalf("the channel translation setting did not save: %d %s", code, body)
	}
}

// TestTodo_CHATBUG_087_Integration_Browser: a browser sends an Origin with every
// write. With the browser token the server minted, the save is accepted; without
// it the refusal is the browser-token one (403), which the client answers by
// getting a fresh page credential and token and repeating the save once.
func TestTodo_CHATBUG_087_Integration_Browser(t *testing.T) {
	s := newChatbug087Served(t, true, "employee")
	pref := chatrender.DefaultPreference("en")
	pref.ReadingLanguage = "de"
	s.extra = map[string]string{"Origin": "http://example.com"}
	code, body := s.call(http.MethodPut, ChatRenderingPath+"/settings", map[string]any{"settings": pref})
	if code != http.StatusForbidden || !strings.Contains(body, "browser_csrf_rejected") {
		t.Fatalf("a browser write without the browser token answered %d %s", code, body)
	}
	s.extra["Cookie"] = "hcmnext_browser_csrf=minted-by-the-page"
	if code, body = s.call(http.MethodPut, ChatRenderingPath+"/settings", map[string]any{"settings": pref}); code != http.StatusOK {
		t.Fatalf("a browser write with the browser token answered %d %s", code, body)
	}
	// A tab that outlived its credential presents one the server no longer
	// accepts: every read and write is 401, however fresh the route is. The page
	// used to turn that into "Settings could not be saved" with nothing logged.
	s.bearer = "Bearer a-credential-from-before-the-restart"
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if code, _ := s.call(method, ChatRenderingPath+"/settings", map[string]any{"settings": pref}); code != http.StatusUnauthorized {
			t.Fatalf("%s with a stale credential answered %d, want 401", method, code)
		}
	}
}

// chatbug087FailingPort fails the settings write the way a refused database
// write would.
type chatbug087FailingPort struct {
	*chatrenderHTTPFixture
	err error
}

func (p chatbug087FailingPort) PutLanguageSettings(context.Context, chatstore.RenderingScope, chatrender.Preference) error {
	return p.err
}

// TestTodo_CHATBUG_087: a settings save that fails is written to the server log
// with its cause, its action and the kind of error, never with the person's
// settings; a save that works is logged as ok; a refused credential is logged
// with its status. Before, nothing was logged and the page could only say
// "could not be saved".
func TestTodo_CHATBUG_087(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previous)

	cause := errors.New("ERROR: permission denied for table chat_preference (SQLSTATE 42501)")
	port := chatbug087FailingPort{chatrenderHTTPFixture: &chatrenderHTTPFixture{settings: chatrender.DefaultPreference("de")}, err: cause}
	handler := ChatRenderingHandler{Port: port}
	body := `{"settings":{"tone":"as_written","reading_language":"de","further_languages":null,"translate":true}}`

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, chatrenderRequest(t, http.MethodPut, ChatRenderingPath+"/settings", body))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("a failed save answered %d %s", w.Code, w.Body.String())
	}
	out := logged.String()
	for _, want := range []string{"hcmnext.chat_rendering", "outcome=failed", "method=PUT", "action=settings", "SQLSTATE 42501", "error_type="} {
		if !strings.Contains(out, want) {
			t.Errorf("the failure log lacks %q: %s", want, out)
		}
	}
	if strings.Contains(out, "reading_language") || strings.Contains(out, `"de"`) {
		t.Errorf("the log carries the person's settings: %s", out)
	}

	logged.Reset()
	working := ChatRenderingHandler{Port: &chatrenderHTTPFixture{settings: chatrender.DefaultPreference("de")}}
	w = httptest.NewRecorder()
	working.ServeHTTP(w, chatrenderRequest(t, http.MethodPut, ChatRenderingPath+"/settings", body))
	if w.Code != http.StatusOK || !strings.Contains(logged.String(), "outcome=ok") || !strings.Contains(logged.String(), "method=PUT") {
		t.Fatalf("a save was not logged as ok: %d %q", w.Code, logged.String())
	}
	logged.Reset()
	w = httptest.NewRecorder()
	working.ServeHTTP(w, chatrenderRequest(t, http.MethodGet, ChatRenderingPath+"/settings", ""))
	if w.Code != http.StatusOK || logged.Len() != 0 {
		t.Fatalf("a read was logged: %q", logged.String())
	}

	// A body the route does not accept names why.
	logged.Reset()
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, chatrenderRequest(t, http.MethodPut, ChatRenderingPath+"/settings", `{"settings":{"revision":3}}`))
	if w.Code != http.StatusBadRequest || !strings.Contains(logged.String(), "not the expected shape") {
		t.Fatalf("a body of the wrong shape: %d %q", w.Code, logged.String())
	}

	// A credential the server refuses is logged with its status.
	logged.Reset()
	overlay := OverlayChatRenderings(http.NotFoundHandler(), port, integrate1RefusingAdmission())
	w = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, ChatRenderingPath+"/settings", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer stale-credential")
	request.Header.Set("Content-Type", "application/json")
	overlay.ServeHTTP(w, request)
	if w.Code != http.StatusUnauthorized || !strings.Contains(logged.String(), "admission denied") || !strings.Contains(logged.String(), "401") {
		t.Fatalf("a refused credential: %d %q", w.Code, logged.String())
	}

	// The translation settings are logged the same way.
	logged.Reset()
	chatlangHandler := ChatlangHandler{Surface: chatbug087FailingChatlang{err: cause}}
	request = httptest.NewRequest(http.MethodGet, ChatlangPath+"/settings?conversation=room", nil)
	w = httptest.NewRecorder()
	chatlangHandler.ServeHTTP(w, request)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(logged.String(), "hcmnext.chat_translation_settings") || !strings.Contains(logged.String(), "SQLSTATE 42501") {
		t.Fatalf("a failed translation setting: %d %q", w.Code, logged.String())
	}
}

// integrate1RefusingAdmission is an admission that knows no credential: every
// request is answered 401, as for a bearer the server no longer accepts.
func integrate1RefusingAdmission() transport.Config {
	return transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, trust.ErrNoCredential })}
}

// chatbug087FailingChatlang is a translation surface whose reads fail.
type chatbug087FailingChatlang struct{ err error }

func (s chatbug087FailingChatlang) View(context.Context, string) (ChatlangView, error) {
	return ChatlangView{}, s.err
}
func (s chatbug087FailingChatlang) PutWorkspace(context.Context, chatlang.Workspace) (chatlang.Workspace, error) {
	return chatlang.Workspace{}, s.err
}
func (s chatbug087FailingChatlang) PutChannel(context.Context, string, chatlang.Channel) error {
	return s.err
}
func (s chatbug087FailingChatlang) AddTerm(context.Context, chatlang.Term) (string, error) {
	return "", s.err
}
func (s chatbug087FailingChatlang) RemoveTerm(context.Context, string) error { return s.err }
