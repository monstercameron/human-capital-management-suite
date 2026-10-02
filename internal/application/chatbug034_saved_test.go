package application

import (
	"bytes"
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
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// TestTodo_CHATBUG_034 drives the exact requests the hover bar's bookmark sends
// (POST /api/chat/saved with the host, tab, cursor and limit query the client
// always adds) through the served handler assembly over a really composed chat
// runtime, on a message shaped like the one in the capture: a root post in a
// public channel with replies, reactions and a document reference. The press on
// an already saved message (the client's pressed state can lag the list),
// the press that unsaves, and save, unsave, save again must all answer 200 and
// leave the list matching the last press.
func TestTodo_CHATBUG_034(t *testing.T) {
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
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "chatbug-034-saved-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer deployment.close()
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "test-session-alice-034", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "test-verified-alice-034"})
	if err != nil {
		t.Fatal(err)
	}
	aliceCtx := trust.WithPrincipal(ctx, principal)
	alice := chatcore.Principal{TenantID: "host", SubjectID: "alice"}
	room, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PublicChannel, Name: "random", IdempotencyKey: "chatbug-034-room"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, Body: "Open enrollment runs November 2 to 20. doc:doc-47892b80-d600-4401-8244-e2fa2a31caa7", IdempotencyKey: "chatbug-034-root"})
	if err != nil {
		t.Fatal(err)
	}
	for i, body := range []string{"One reply", "Another reply"} {
		if _, err := deployment.service.SendPost(aliceCtx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, ParentID: root.ID, Body: body, IdempotencyKey: "chatbug-034-reply-" + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	for _, emoji := range []string{"+1", "eyes"} {
		if _, err := deployment.service.AddReaction(aliceCtx, chatcore.AddReactionRequest{Principal: alice, Reaction: chatcore.Reaction{TenantID: "host", ConversationID: room.ID, PostID: root.ID, Emoji: emoji}}); err != nil {
			t.Fatal(err)
		}
	}
	admission, bearer := integrate1Admission(t, "host", "alice", time.Now)
	handler := overlayIntegrate1Chat(http.NotFoundHandler(), deployment, VoiceService{}, admission)
	// press sends what the browser sends: the query carries host, tab, cursor
	// and limit on a command as well as on a read.
	press := func(action, postID string) (int, string) {
		raw, err := json.Marshal(SavedMessageCommand{Action: action, ConversationID: room.ID, PostID: postID})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, SavedMessagesPath+"?"+url.Values{"host": {"host"}, "tab": {"all"}, "cursor": {""}, "limit": {"200"}}.Encode(), bytes.NewReader(raw))
		r.Header.Set("Authorization", bearer)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	saved := func() map[string]chatcore.SavedItem {
		r := httptest.NewRequest(http.MethodGet, SavedMessagesPath+"?"+url.Values{"host": {"host"}, "tab": {"all"}, "cursor": {""}, "limit": {"200"}}.Encode(), nil)
		r.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("the list answered %d %s", w.Code, w.Body.String())
		}
		var page chatcore.SavedPage
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		out := map[string]chatcore.SavedItem{}
		for _, item := range page.Items {
			out[item.PostID] = item
		}
		return out
	}
	step := func(name, action, postID string, wantSaved bool) {
		t.Helper()
		if code, body := press(action, postID); code != http.StatusOK {
			t.Fatalf("%s: %s answered %d %s", name, action, code, body)
		}
		item, listed := saved()[postID]
		if listed != wantSaved {
			t.Fatalf("%s: listed=%v want %v", name, listed, wantSaved)
		}
		if listed && (item.Availability != "readable" || item.Post == nil || item.Post.Body == "") {
			t.Fatalf("%s: the saved row is not readable with its text: %+v", name, item)
		}
		if listed && postID == root.ID && !strings.Contains(item.Post.Body, "doc:doc-47892b80") {
			t.Fatalf("%s: the saved message lost its document reference: %+v", name, item.Post)
		}
	}
	step("first save", "save", root.ID, true)
	// The press on a message the list already holds, from a hover bar whose
	// pressed state had not caught up: a repeated save, not an error.
	step("save pressed again", "save", root.ID, true)
	step("unsave", "remove", root.ID, false)
	step("unsave pressed again", "remove", root.ID, false)
	step("save again", "save", root.ID, true)
	step("unsave once more", "remove", root.ID, false)
	step("save once more", "save", root.ID, true)
	// A reply in the thread pane saves the same way.
	replies, err := deployment.service.ListPosts(aliceCtx, chatcore.ListPostsRequest{Principal: alice, TenantID: "host", ConversationID: room.ID, Page: chatcore.Page{PageSize: 50}})
	if err != nil {
		t.Fatal(err)
	}
	for _, post := range replies.Posts {
		if post.ParentID == root.ID {
			step("save reply", "save", post.ID, true)
			step("unsave reply", "remove", post.ID, false)
			break
		}
	}
	if _, listed := saved()[root.ID]; !listed {
		t.Fatal("the root message lost its save while a reply was saved and removed")
	}
}
