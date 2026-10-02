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
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// TestTodo_CHATBUG_005 drives POST and GET /api/chat/saved through the served
// handler assembly over a really composed chat runtime: save, mark done, reopen,
// list by tab and unsave all answer, and the list matches what was saved.
func TestTodo_CHATBUG_005(t *testing.T) {
	ctx := context.Background()
	chatDB, coreDB := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
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
	databaseURL := func(raw, schema string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	core, err := pgxadapter.NewPool(ctx, databaseURL(coreDB.URL, coreDB.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "chatbug-005-saved-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer deployment.close()
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "test-session-alice", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "test-verified-alice"})
	if err != nil {
		t.Fatal(err)
	}
	aliceCtx := trust.WithPrincipal(ctx, principal)
	alice := chatcore.Principal{TenantID: "host", SubjectID: "alice"}
	room, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PublicChannel, Name: "saved-room", IdempotencyKey: "chatbug-005-room"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, Body: "First saved message", IdempotencyKey: "chatbug-005-one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, Body: "Second saved message", IdempotencyKey: "chatbug-005-two"})
	if err != nil {
		t.Fatal(err)
	}
	admission, bearer := integrate1Admission(t, "host", "alice", time.Now)
	handler := overlayIntegrate1Chat(http.NotFoundHandler(), deployment, VoiceService{}, admission)
	post := func(command SavedMessageCommand) (int, string) {
		raw, err := json.Marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, SavedMessagesPath, bytes.NewReader(raw))
		r.Header.Set("Authorization", bearer)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	list := func(tab string) []chatcore.SavedItem {
		r := httptest.NewRequest(http.MethodGet, SavedMessagesPath+"?host=host&tab="+tab+"&limit=200", nil)
		r.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("list %s answered %d %s", tab, w.Code, w.Body.String())
		}
		var page chatcore.SavedPage
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page.Items
	}
	expect := func(step string, wantTodo, wantDone, wantAll int) {
		t.Helper()
		if got := len(list("todo")); got != wantTodo {
			t.Fatalf("%s: to do=%d want %d", step, got, wantTodo)
		}
		if got := len(list("done")); got != wantDone {
			t.Fatalf("%s: done=%d want %d", step, got, wantDone)
		}
		if got := len(list("all")); got != wantAll {
			t.Fatalf("%s: all=%d want %d", step, got, wantAll)
		}
	}
	command := func(action, postID string) SavedMessageCommand {
		return SavedMessageCommand{Action: action, ConversationID: room.ID, PostID: postID}
	}
	step := func(action, postID string) {
		t.Helper()
		if code, body := post(command(action, postID)); code != http.StatusOK {
			t.Fatalf("%s answered %d %s", action, code, body)
		}
	}
	expect("empty", 0, 0, 0)
	step("save", first.ID)
	step("save", second.ID)
	step("save", second.ID)
	expect("saved twice", 2, 0, 2)
	for _, item := range list("all") {
		if item.Availability != "readable" || item.Post == nil || item.Channel != "saved-room" {
			t.Fatalf("saved row is not readable with its channel: %+v", item)
		}
	}
	step("done", first.ID)
	expect("marked done", 1, 1, 2)
	step("reopen", first.ID)
	expect("reopened", 2, 0, 2)
	step("remove", second.ID)
	expect("unsaved", 1, 0, 1)
	step("remove", second.ID)
	expect("unsaved again", 1, 0, 1)
	if code, _ := post(command("save", "no-such-post")); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("saving a message that is not there answered %d", code)
	}
	// A person who reads the public channel without having joined it is told no,
	// not "unavailable", and can still read their own empty list.
	_, bobBearer := integrate1Admission(t, "host", "bob", time.Now)
	bobCall := func(method, target, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		r.Header.Set("Authorization", bobBearer)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := bobCall(http.MethodPost, SavedMessagesPath, `{"action":"save","conversation_id":"`+room.ID+`","post_id":"`+first.ID+`"}`); w.Code != http.StatusForbidden {
		t.Fatalf("non-member save answered %d %s", w.Code, w.Body.String())
	}
	if w := bobCall(http.MethodGet, SavedMessagesPath+"?host=host&tab=all&limit=200", ""); w.Code != http.StatusOK {
		t.Fatalf("non-member list answered %d %s", w.Code, w.Body.String())
	}
}

// TestTodo_CHATBUG_005_Cause proves a saved-list failure is never silently a
// bare 503: with the developer opt-in the cause is logged, without it the log
// stays quiet, and the body never carries the cause either way.
func TestTodo_CHATBUG_005_Cause(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cause := errors.New("database is down")
	for _, want := range []struct {
		err    error
		status int
	}{{chatcore.ErrUnavailable, http.StatusServiceUnavailable}, {cause, http.StatusServiceUnavailable}, {chatcore.ErrNotFound, http.StatusNotFound}, {chatcore.ErrPermissionDenied, http.StatusForbidden}} {
		w := httptest.NewRecorder()
		chatsaveHTTPError(w, want.err)
		if w.Code != want.status || strings.Contains(w.Body.String(), "database") {
			t.Fatalf("%v answered %d %s", want.err, w.Code, w.Body.String())
		}
	}
	t.Setenv("HCMNEXT_AGENT_DEBUG_CAUSES", "")
	chatsaveHTTPError(httptest.NewRecorder(), cause)
	if logged.Len() != 0 {
		t.Fatalf("cause logged without the opt-in: %s", logged.String())
	}
	t.Setenv("HCMNEXT_AGENT_DEBUG_CAUSES", "1")
	chatsaveHTTPError(httptest.NewRecorder(), cause)
	chatsaveHTTPError(httptest.NewRecorder(), chatcore.ErrPermissionDenied)
	if !strings.Contains(logged.String(), "database is down") || strings.Count(logged.String(), "hcmnext.chat_saved_unavailable") != 1 {
		t.Fatalf("an unexpected failure must log its cause once, a typed refusal not at all: %s", logged.String())
	}
}
