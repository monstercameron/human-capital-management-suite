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

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// TestTodo_CHATBUG_004 drives POST /api/chat/search through the served handler
// assembly over a really composed chat runtime: a signed-in member finds a
// message and a channel they can read, and a person who is not a member of a
// private channel finds nothing from it.
func TestTodo_CHATBUG_004(t *testing.T) {
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
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "chatbug-004-search-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer deployment.close()
	if deployment.search.Port == nil {
		t.Fatal("composed chat has no search port: POST /api/chat/search would answer 503")
	}
	authenticate := func(subject string) context.Context {
		at := time.Now().UTC()
		principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "test-session-" + subject, IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "test-verified-" + subject})
		if err != nil {
			t.Fatal(err)
		}
		return trust.WithPrincipal(ctx, principal)
	}
	aliceCtx := authenticate("alice")
	alice := chatcore.Principal{TenantID: "host", SubjectID: "alice"}
	holiday, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PublicChannel, Name: "holiday-planning", IdempotencyKey: "chatbug-004-public"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: holiday.ID, Body: "The holiday schedule is posted", IdempotencyKey: "chatbug-004-post"}); err != nil {
		t.Fatal(err)
	}
	secret, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PrivateChannel, Name: "closed-room", IdempotencyKey: "chatbug-004-private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: secret.ID, Body: "holiday bonus figures", IdempotencyKey: "chatbug-004-private-post"}); err != nil {
		t.Fatal(err)
	}

	bearerFor := func(subject string) (http.Handler, string) {
		admission, bearer := integrate1Admission(t, "host", subject, time.Now)
		return (&agentServedAssembly{ChatSearch: deployment.search}).Overlay(http.NotFoundHandler(), admission), bearer
	}
	search := func(subject, query string) (int, chatsearch.Response) {
		h, bearer := bearerFor(subject)
		r := httptest.NewRequest(http.MethodPost, ChatSearchPath, strings.NewReader(`{"Query":"`+query+`","DisplayQuery":"`+query+`","Mode":"keyword","Limit":20}`))
		r.Header.Set("Authorization", bearer)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var out chatsearch.Response
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode %q: %v", w.Body.String(), err)
			}
		} else {
			t.Logf("search %s %q: %d %s", subject, query, w.Code, w.Body.String())
		}
		return w.Code, out
	}
	rowsOf := func(res chatsearch.Response, kind chatsearch.Kind) []chatsearch.Row {
		for _, g := range res.Groups {
			if g.Kind == kind {
				return g.Rows
			}
		}
		return nil
	}

	code, res := search("alice", "holiday")
	if code != http.StatusOK {
		t.Fatalf("member search answered %d", code)
	}
	messages := rowsOf(res, chatsearch.Message)
	if len(messages) < 2 {
		t.Fatalf("member found %d messages, want the public and the private one: %+v", len(messages), res.Groups)
	}
	channels := rowsOf(res, chatsearch.Conversation)
	if len(channels) != 1 || !strings.Contains(channels[0].Text, "holiday-planning") {
		t.Fatalf("member did not find the readable channel: %+v", res.Groups)
	}

	code, res = search("mallory", "holiday")
	if code != http.StatusOK {
		t.Fatalf("outsider search answered %d", code)
	}
	for _, g := range res.Groups {
		for _, row := range g.Rows {
			if row.Target.ConversationID == secret.ID {
				t.Fatalf("outsider found a row in a private channel: %+v", row)
			}
		}
	}

	h, _ := bearerFor("alice")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatSearchPath, strings.NewReader(`{"Query":"holiday"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unadmitted search answered %d", w.Code)
	}
}
