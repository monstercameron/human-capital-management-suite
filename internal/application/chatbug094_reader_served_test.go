package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_094: a German or Arabic reader waits a minute for a channel
// because one rendering read answers 503 and holds the list. Through the
// served reader: a post with no rendering yet is answered as pending or as
// written, never 503 (even when its revision row predates detection); a post
// that does not exist is 404; a batch answers what it can and leaves out what
// is gone.
func TestTodo_CHATBUG_094(t *testing.T) {
	ctx := context.Background()
	now := time.Now
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)
	adapter := chatstore.NewAdapter(raw)
	service := chat.NewService(adapter, now)
	service.SetAuthority(servedPersonaChatAuthority{})
	owner := chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}
	if _, err := service.CreateConversation(ctx, chat.CreateConversationRequest{Principal: owner, TenantID: "tenant-a", ConversationID: "room", Kind: chat.PublicChannel, Name: "general"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMembership(ctx, chat.AddMembershipRequest{Principal: owner, Membership: chat.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "room", SubjectID: "alice", HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	post, err := service.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: "tenant-a", ConversationID: "room", Body: "Standup moved to ten in the morning", IdempotencyKey: "chatbug094-a"})
	if err != nil {
		t.Fatal(err)
	}
	// A post that predates detection keeps no revision row; the reader must
	// still answer it as written, never 503.
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('legacy-post','tenant-a','room','owner','tenant-a',100,'The quarterly report is ready for review')`); err != nil {
		t.Fatal(err)
	}
	legacyID := "legacy-post"
	governance := &ChatlangGovernance{Store: raw}
	governance.BindEngine(ChatlangEngineInfo{Name: "fixture", Ready: true})
	if _, err := raw.PutChatlangWorkspace(ctx, "tenant-a", "owner", chatlang.Workspace{Enabled: true, ExternalAllowed: true}); err != nil {
		t.Fatal(err)
	}
	surface := &ChatRenderingSurface{Store: raw, Languages: governance, Policy: integrate2RenderingPolicy{Store: raw, Chat: service, Languages: governance}}
	alice := chatlangRequest(t, "alice", http.MethodGet, "/", "", "").Context()
	pref := chatrender.DefaultPreference("de")
	pref.Translate = true
	if err := surface.PutLanguageSettings(alice, chatstore.RenderingScope{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, Tenant: "tenant-a", Conversation: "room"}, pref); err != nil {
		t.Fatal(err)
	}
	handler := ChatRenderingHandler{Port: surface}
	get := func(t *testing.T, query string) (int, map[string]chatui.ReaderSelection) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, chatlangRequest(t, "alice", http.MethodGet, ChatRenderingPath+"/reader?"+query, "", "de-DE"))
		var out map[string]chatui.ReaderSelection
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatalf("reader answer is not selections: %v: %s", err, w.Body.String())
			}
		}
		return w.Code, out
	}

	// An ordinary English post with no rendering yet is answered as pending or
	// as written, never 503. Pending carries the wanted translation and the
	// page draws the list body it already has until the rendering arrives.
	code, out := get(t, "conversation=room&message="+post.ID)
	if code != http.StatusOK {
		t.Fatalf("a post with no rendering yet answered %d, want 200", code)
	}
	selection, ok := out[post.ID]
	if !ok || (selection.Mark.State != "pending" && selection.Mark.State != "ready" && selection.Mark.State != "original" && selection.Mark.State != "fallback") {
		t.Fatalf("a post with no rendering yet is in no drawable state: %+v", out)
	}
	if selection.Mark.State == "pending" && (!selection.Mark.CanShowOriginal || len(selection.Mark.Wanted) == 0) {
		t.Fatalf("a pending answer names nothing to wait for: %+v", selection.Mark)
	}

	// So is a post that predates detection.
	if code, out := get(t, "conversation=room&message="+legacyID); code != http.StatusOK {
		t.Fatalf("a post with no revision row answered %d, want 200", code)
	} else if pending := out[legacyID]; pending.Mark.State == "pending" && (!pending.Mark.CanShowOriginal || len(pending.Mark.Wanted) == 0) {
		t.Fatalf("a legacy post's pending answer names nothing to wait for: %+v", pending.Mark)
	}

	// A post that does not exist is 404 and carries no retryable outage.
	if code, _ := get(t, "conversation=room&message=no-such-post"); code != http.StatusNotFound {
		t.Fatalf("a missing post answered %d, want 404", code)
	}

	// A batch answers what it can and leaves out what is gone.
	code, out = get(t, "conversation=room&message="+post.ID+"&message=no-such-post")
	if code != http.StatusOK {
		t.Fatalf("a batch with one missing post answered %d, want 200", code)
	}
	if _, ok := out[post.ID]; !ok {
		t.Fatalf("a readable post is missing from the batch: %+v", out)
	}
	if _, ok := out["no-such-post"]; ok {
		t.Fatalf("a missing post was answered: %+v", out)
	}
}
