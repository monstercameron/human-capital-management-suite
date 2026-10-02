package application

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatrecordstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatmodServed is the moderation surface as it is served: the real chat store
// and service on the shared test PostgreSQL, the moderation command and queue as
// the composition root builds them (route lease, filter queue, the record
// store's report path), and the HTTP handlers behind the admission edge. Every
// call carries the bearer of the person making it; their roles come from a
// projection the test owns, never from the request.
type chatmodServed struct {
	t         *testing.T
	tenant    string
	raw       *chatstore.Store
	core      *chat.Service
	moderated *chatstore.ModeratedAdapter
	handler   http.Handler
	admission transport.Config
	bearer    map[string]string
	roles     map[string][]string
	names     map[string]string
	post      chat.Post
	ports     *chatremoveRoutedStore
	directory *chatrouting.MemoryDirectory
}

func chatmodSetup(t *testing.T) *chatmodServed {
	t.Helper()
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(t.Context(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)
	f := &chatmodServed{t: t, tenant: "tenant", raw: raw, bearer: map[string]string{}, roles: map[string][]string{}, names: map[string]string{"owner": "Olive Owner", "author": "Alex Author", "member": "Mia Member", "reporter": "Rae Reporter", "admin": "Ada Admin"}}
	f.moderated = chatstore.NewModeratedAdapter(chatstore.NewAdapter(raw))
	f.moderated.CurrentRoles = func(_ context.Context, p chat.Principal) ([]string, error) { return f.roles[p.SubjectID], nil }
	f.core = chat.NewService(f.moderated, time.Now)
	f.core.SetAuthority(servedPersonaChatAuthority{})
	directory := chatrouting.NewMemoryDirectory()
	f.directory = directory
	ports := &chatremoveRoutedStore{ModerationStore: f.moderated, Permissions: f.moderated, Directory: directory, Cache: chatrouting.NewRouteCache(time.Second), Now: time.Now}
	f.ports = ports
	moderation := &chat.ModerationService{Store: ports, Filters: chatmod005FilterQueue{Filters: chatstore.NewFilterModeration(f.moderated), Routes: ports}}
	records := &chatrecords.Service{Repo: chatrecordstore.New(raw), Auth: ChatRecordAuthority{}, Clock: time.Now}
	runtime := composedChat{service: f.core, extensions: &ChatExtensions{Conversations: f.core, Records: records, TodoStore: raw}, moderationStore: f.moderated, moderationPermissions: ports, moderation: moderation}
	for _, subject := range []string{"owner", "author", "member", "reporter", "admin", "outsider"} {
		admission, bearer := integrate1Admission(t, f.tenant, subject, time.Now)
		f.admission, f.bearer[subject] = admission, bearer
	}
	reader := chatremoveNameDirectory{Read: func(_ context.Context, _ string, ids []string) (map[string]string, error) {
		out := map[string]string{}
		for _, id := range ids {
			out[id] = f.names[id]
		}
		return out, nil
	}}
	f.handler = overlayIntegrate1Chat(http.NotFoundHandler(), runtime, VoiceService{}, f.admission, reader)
	f.roles["admin"] = []string{"WORKSPACE_ADMIN"}
	// A public channel the owner made (the creator is its manager) with the author,
	// a plain member and the reporter in it; the workspace administrator and an
	// outsider are not members.
	owner := chat.Principal{TenantID: f.tenant, SubjectID: "owner"}
	if _, err = f.core.CreateConversation(t.Context(), chat.CreateConversationRequest{Principal: owner, TenantID: f.tenant, ConversationID: "room", Kind: chat.PublicChannel, Name: "Room"}); err != nil {
		t.Fatal(err)
	}
	f.route("room")
	for _, id := range []string{"author", "member", "reporter"} {
		if _, err = f.core.AddMembership(f.leased("room"), chat.AddMembershipRequest{Principal: owner, Membership: chat.Membership{TenantID: f.tenant, ConversationID: "room", HomeTenantID: f.tenant, SubjectID: id, Role: chat.Member, HistoryVisibility: chat.FullHistory}}); err != nil {
			t.Fatal(err)
		}
	}
	f.post = f.send("author", "the words to be removed", "first")
	return f
}

func (f *chatmodServed) send(subject, body, key string) chat.Post {
	f.t.Helper()
	post, err := f.core.SendPost(f.leased("room"), chat.SendPostRequest{Principal: chat.Principal{TenantID: f.tenant, SubjectID: subject}, TenantID: f.tenant, ConversationID: "room", Body: body, IdempotencyKey: key})
	if err != nil {
		f.t.Fatal(err)
	}
	return post
}

func (f *chatmodServed) call(subject, method, path string, value any) *httptest.ResponseRecorder {
	f.t.Helper()
	var body bytes.Buffer
	if value != nil {
		if err := json.NewEncoder(&body).Encode(value); err != nil {
			f.t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Content-Type", "application/json")
	if subject != "" {
		request.Header.Set("Authorization", f.bearer[subject])
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

// remove runs the two commands a removal is made of, as the page does: count,
// then apply with the confirmation the count returned.
func (f *chatmodServed) removal(subject, action, reason, note string, ids ...string) *httptest.ResponseRecorder {
	f.t.Helper()
	input := chatremoveInput{Removal: chat.RemovalRequest{Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: ids}, ReasonCode: reason, Note: note, Action: action}}
	preview := f.call(subject, http.MethodPost, ChatModerationPath+"/preview", input)
	if preview.Code != http.StatusOK {
		return preview
	}
	var counted chat.RemovalPreview
	if err := json.Unmarshal(preview.Body.Bytes(), &counted); err != nil {
		f.t.Fatal(err)
	}
	input.Removal.Confirmation, input.Removal.ConfirmedCount = counted.Confirmation, counted.Count
	return f.call(subject, http.MethodPost, ChatModerationPath+"/apply", input)
}

func (f *chatmodServed) listed(subject string) chat.Post {
	f.t.Helper()
	page, err := f.core.ListPosts(f.as(subject), chat.ListPostsRequest{Principal: chat.Principal{TenantID: f.tenant, SubjectID: subject}, TenantID: f.tenant, ConversationID: "room", Page: chat.Page{PageSize: 50}})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, p := range page.Posts {
		if p.ID == f.post.ID {
			return p
		}
	}
	f.t.Fatal("the post is missing from the conversation")
	return chat.Post{}
}

func (f *chatmodServed) rows(query string, args ...any) []string {
	f.t.Helper()
	var out []string
	err := f.raw.RunTenantTx(f.t.Context(), f.tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(f.t.Context(), query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err = rows.Scan(&s); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

type chatmodQueue struct {
	Items []chat.ModerationItem
	Count int
	Names map[string]string
}

func (f *chatmodServed) queue(subject string) chatmodQueue {
	f.t.Helper()
	response := f.call(subject, http.MethodGet, ChatModerationPath, nil)
	if response.Code != http.StatusOK {
		f.t.Fatalf("%s reads the queue: %d %s", subject, response.Code, response.Body.String())
	}
	var q chatmodQueue
	if err := json.Unmarshal(response.Body.Bytes(), &q); err != nil {
		f.t.Fatal(err)
	}
	return q
}

func (f *chatmodServed) summary(subject string) chat.ModerationSummary {
	f.t.Helper()
	response := f.call(subject, http.MethodGet, ChatModerationPath+"/summary", nil)
	if response.Code != http.StatusOK {
		f.t.Fatalf("%s reads the summary: %d %s", subject, response.Code, response.Body.String())
	}
	var s chat.ModerationSummary
	if err := json.Unmarshal(response.Body.Bytes(), &s); err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *chatmodServed) notices(subject string) []chat.ModerationNotice {
	f.t.Helper()
	response := f.call(subject, http.MethodGet, ChatModerationPath+"/notices", nil)
	if response.Code != http.StatusOK {
		f.t.Fatalf("%s reads notices: %d %s", subject, response.Code, response.Body.String())
	}
	var n []chat.ModerationNotice
	if err := json.Unmarshal(response.Body.Bytes(), &n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestTodo_CHATMOD_004_Integration removes a message through the served
// handlers and follows it everywhere a person can see it: the reader's
// projection, the author's private notice, the audit trail, the record that stays
// untouched, and the way back.
func TestTodo_CHATMOD_004_Integration(t *testing.T) {
	f := chatmodSetup(t)
	post := f.post

	// A plain member cannot remove: the server checks the permission at the
	// moment of the command, not the menu.
	if response := f.removal("member", "remove", "spam", "", post.ID); response.Code != http.StatusForbidden {
		t.Fatalf("a plain member's removal: %d %s", response.Code, response.Body.String())
	}
	if response := f.call("member", http.MethodGet, ChatModerationPagePath+"?action=remove&conversation=room&post="+post.ID, nil); response.Code != http.StatusForbidden {
		t.Fatalf("a plain member opens the removal dialog: %d", response.Code)
	}
	if f.listed("member").Deleted {
		t.Fatal("a refused removal changed the message")
	}
	// A reason from the short list is required.
	if response := f.removal("owner", "remove", "because", "", post.ID); response.Code != http.StatusBadRequest {
		t.Fatalf("a reason outside the list: %d", response.Code)
	}

	// The channel's manager removes it, with a reason and a note.
	if response := f.removal("owner", "remove", "harassment", "targeted a colleague", post.ID); response.Code != http.StatusOK {
		t.Fatalf("the manager's removal: %d %s", response.Code, response.Body.String())
	}

	// Readers see "Removed by an administrator" in place of the text; the row,
	// its revisions and the author's id are still there, so the thread is kept.
	for _, reader := range []string{"member", "reporter", "author", "owner"} {
		seen := f.listed(reader)
		if !seen.Deleted || seen.Body != chat.RemovedByAdministrator || len(seen.References) != 0 || strings.Contains(seen.Body, "words") {
			t.Fatalf("%s reads %+v", reader, seen)
		}
	}
	var stored string
	var tombstoned bool
	if err := f.raw.RunTenantTx(t.Context(), f.tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT body,tombstoned FROM chat_post WHERE tenant_id=$1 AND id=$2`, f.tenant, post.ID).Scan(&stored, &tombstoned)
	}); err != nil || stored != post.Body || !tombstoned {
		t.Fatalf("the stored row changed: body=%q tombstoned=%v err=%v", stored, tombstoned, err)
	}

	// Who, when and why are recorded, and so is the audit trail.
	if got := f.rows(`SELECT actor_id||'|'||reason_code||'|'||note FROM chat_admin_removal WHERE tenant_id=$1 AND post_id=$2`, f.tenant, post.ID); len(got) != 1 || got[0] != "owner|harassment|targeted a colleague" {
		t.Fatalf("removal record: %v", got)
	}
	// The decision is in the audit trail with its actor and reason (the chat event projection adds a generic row of its own beside it).
	if got := f.rows(`SELECT actor_id||'|'||action||'|'||reason FROM chat_audit_event WHERE tenant_id=$1 AND target_id=$2 AND action='moderation.remove' AND reason<>action`, f.tenant, post.ID); len(got) != 1 || got[0] != "owner|moderation.remove|harassment — targeted a colleague" {
		t.Fatalf("audit trail: %v", got)
	}
	if got := f.rows(`SELECT actor_id||'|'||action FROM chat_moderation_action WHERE tenant_id=$1 AND case_id=$2`, f.tenant, "removal:"+post.ID); len(got) != 1 || got[0] != "owner|remove" {
		t.Fatalf("action record: %v", got)
	}

	// The author is told privately, with the reason and a way to ask for a review.
	notices := f.notices("author")
	if len(notices) != 1 || notices[0].Outcome != "remove" || !strings.Contains(notices[0].Reason, "harassment") || !strings.Contains(notices[0].Reason, "targeted a colleague") || !notices[0].CanAppeal {
		t.Fatalf("the author's notice: %+v", notices)
	}
	if n := f.notices("member"); len(n) != 0 {
		t.Fatalf("a notice reached a person it was not for: %+v", n)
	}
	authorSummary := f.summary("author")
	if authorSummary.Moderator || authorSummary.Open != 0 || authorSummary.NoticeCount != 1 || len(authorSummary.Notices) != 1 {
		t.Fatalf("the author's summary: %+v", authorSummary)
	}

	// A removal is a decision already made: it is history, not an open item.
	if q := f.queue("owner"); len(q.Items) != 0 {
		t.Fatalf("the removal is open in the queue: %+v", q.Items)
	}
	if s := f.summary("owner"); !s.Moderator || s.Open != 0 {
		t.Fatalf("the manager's summary: %+v", s)
	}

	// Restore is the review permission: the manager does not hold it, a
	// workspace administrator does, and the message comes back exactly.
	if response := f.removal("owner", "restore", "", "", post.ID); response.Code != http.StatusForbidden {
		t.Fatalf("a manager restoring: %d", response.Code)
	}
	if response := f.removal("admin", "restore", "", "reviewed in context", post.ID); response.Code != http.StatusOK {
		t.Fatalf("the administrator's restore: %d %s", response.Code, response.Body.String())
	}
	back := f.listed("member")
	if back.Deleted || back.Body != post.Body || back.Revision <= post.Revision {
		t.Fatalf("the restored message is not as it was: %+v", back)
	}
	restored := f.notices("author")
	if len(restored) != 2 || restored[0].Outcome != "restore" || restored[0].CanAppeal {
		t.Fatalf("the author is not told of the restore: %+v", restored)
	}
	if got := f.rows(`SELECT actor_id||'|'||action FROM chat_audit_event WHERE tenant_id=$1 AND target_id=$2 AND action='moderation.restore' AND reason<>action`, f.tenant, post.ID); len(got) != 1 || got[0] != "admin|moderation.restore" {
		t.Fatalf("the restore is not in the audit trail: %v", got)
	}
}

// TestTodo_CHATMOD_004_Security_Served: the permission is the server's reading of
// the person's current roles and channel, never a claim in the request.
func TestTodo_CHATMOD_004_Security_Served(t *testing.T) {
	f := chatmodSetup(t)
	post := f.post
	// A request cannot name its own role.
	forged := chatremoveInput{Removal: chat.RemovalRequest{Principal: chat.Principal{TenantID: f.tenant, SubjectID: "owner", Roles: []string{"WORKSPACE_ADMIN"}}, Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{post.ID}}, ReasonCode: "spam", Action: "remove"}}
	if response := f.call("member", http.MethodPost, ChatModerationPath+"/preview", forged); response.Code != http.StatusForbidden {
		t.Fatalf("a forged principal in the body: %d", response.Code)
	}
	// Without a credential nothing happens.
	if response := f.call("", http.MethodPost, ChatModerationPath+"/preview", forged); response.Code != http.StatusUnauthorized {
		t.Fatalf("no credential: %d", response.Code)
	}
	// A person who is not a member of the channel and holds no role cannot.
	if response := f.removal("outsider", "remove", "spam", "", post.ID); response.Code != http.StatusForbidden {
		t.Fatalf("an outsider: %d", response.Code)
	}
	// The original text of a removed message is read by the review permission
	// alone, and each read is audited.
	if response := f.removal("owner", "remove", "spam", "", post.ID); response.Code != http.StatusOK {
		t.Fatalf("removal: %d", response.Code)
	}
	read := chatremoveInput{ConversationID: "room", PostID: post.ID, Note: "checking a complaint"}
	if response := f.call("owner", http.MethodPost, ChatModerationPath+"/review", read); response.Code != http.StatusForbidden {
		t.Fatalf("a manager read the removed text: %d", response.Code)
	}
	if response := f.call("member", http.MethodPost, ChatModerationPath+"/review", read); response.Code != http.StatusForbidden {
		t.Fatalf("a member read the removed text: %d", response.Code)
	}
	response := f.call("admin", http.MethodPost, ChatModerationPath+"/review", read)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "the words to be removed") {
		t.Fatalf("the reviewer's read: %d %s", response.Code, response.Body.String())
	}
	if got := f.rows(`SELECT actor_id||'|'||reason FROM chat_audit_event WHERE tenant_id=$1 AND target_id=$2 AND action='moderation.review'`, f.tenant, post.ID); len(got) != 1 || got[0] != "admin|checking a complaint" {
		t.Fatalf("the read is not audited: %v", got)
	}
	// A removal cannot be applied twice, and a stale count is refused.
	input := chatremoveInput{Removal: chat.RemovalRequest{Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{post.ID}}, ReasonCode: "spam", Action: "remove", Confirmation: "stale", ConfirmedCount: 1}}
	if response := f.call("owner", http.MethodPost, ChatModerationPath+"/apply", input); response.Code != http.StatusConflict {
		t.Fatalf("a stale confirmation: %d", response.Code)
	}
}

// leased is the context the routing layer gives a write to one conversation:
// the conversation's current route lease, as the served composition holds it.
func (f *chatmodServed) leased(conversation string) context.Context {
	f.t.Helper()
	ctx, err := f.ports.lease(f.t.Context(), f.tenant, conversation)
	if err != nil {
		f.t.Fatal(err)
	}
	return ctx
}

// route puts a conversation on its shard with an active route, which is what
// makes writes to it need the lease the moderation commands take.
func (f *chatmodServed) route(conversation string) {
	f.t.Helper()
	if err := f.raw.RunTenantTx(f.t.Context(), f.tenant, func(tx dbport.Tx) error {
		_, e := tx.Exec(f.t.Context(), `UPDATE chat_conversation SET route_shard='chat-default',route_epoch=1 WHERE tenant_id=$1 AND id=$2`, f.tenant, conversation)
		return e
	}); err != nil {
		f.t.Fatal(err)
	}
	route, err := f.directory.Reserve(f.t.Context(), chatrouting.ReserveRequest{ConversationID: conversation, HostTenantID: f.tenant, ShardID: "chat-default", IdempotencyKey: "route-" + conversation})
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err = f.directory.Activate(f.t.Context(), conversation, f.tenant, route.Epoch); err != nil {
		f.t.Fatal(err)
	}
}

// as is a context carrying the verified identity of one person, which is what
// the transport hands every read of the chat store.
func (f *chatmodServed) as(subject string) context.Context {
	f.t.Helper()
	now := time.Now()
	verified, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(f.tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "chatmod", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "chatmod"})
	if err != nil {
		f.t.Fatal(err)
	}
	return trust.WithPrincipal(f.t.Context(), verified)
}

// TestTodo_CHATMOD_004_Integration_Pages: the pages the product opens over the
// chat, served by the real handlers. The removal dialog names the author and
// shows the message; the report dialog is private; a removed message is "This
// message was removed" in a person's saved list; the author's page tells them
// why and offers the review.
func TestTodo_CHATMOD_004_Integration_Pages(t *testing.T) {
	f := chatmodSetup(t)
	post := f.post
	if response := f.call("member", http.MethodPost, SavedMessagesPath, SavedMessageCommand{Action: "save", ConversationID: "room", PostID: post.ID}); response.Code != http.StatusOK {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	dialog := func(subject, action, locale string) (int, string) {
		response := f.call(subject, http.MethodGet, ChatModerationPagePath+"?action="+action+"&conversation=room&post="+post.ID+"&locale="+locale, nil)
		return response.Code, response.Body.String()
	}
	code, markup := dialog("owner", "remove", "de-DE")
	if code != http.StatusOK || !strings.Contains(markup, "Alex Author") || !strings.Contains(markup, "the words to be removed") || !strings.Contains(markup, `data-chatremove="quick"`) || !strings.Contains(markup, "Diese Nachricht entfernen?") || strings.Contains(markup, "⟦") || strings.Contains(markup, "<style") {
		t.Fatalf("the removal dialog: %d %s", code, markup)
	}
	if strings.Count(markup, `type="radio"`) != 8 || !strings.Contains(markup, `data-chatremove="preview"`) {
		t.Fatalf("the reason list and the several-messages form: %s", markup)
	}
	code, markup = dialog("reporter", "report", "ar")
	if code != http.StatusOK || !strings.Contains(markup, `data-chatremove="report"`) || !strings.Contains(markup, "Alex Author") || strings.Contains(markup, "data-chatremove=\"preview\"") {
		t.Fatalf("the report dialog: %d %s", code, markup)
	}
	if code, _ = dialog("member", "remove", "en-US"); code != http.StatusForbidden {
		t.Fatalf("a plain member's removal dialog: %d", code)
	}
	if code, _ = dialog("owner", "restore", "en-US"); code != http.StatusForbidden {
		t.Fatalf("a manager's restore dialog: %d", code)
	}
	if code, _ = dialog("owner", "delete", "en-US"); code != http.StatusBadRequest {
		t.Fatalf("an unknown dialog: %d", code)
	}

	if response := f.removal("owner", "remove", "spam", "", post.ID); response.Code != http.StatusOK {
		t.Fatalf("removal: %d", response.Code)
	}
	// Saved: "This message was removed", never the text.
	saved := f.call("member", http.MethodGet, SavedMessagesPath, nil)
	if saved.Code != http.StatusOK || strings.Contains(saved.Body.String(), "the words to be removed") || !strings.Contains(saved.Body.String(), `"removed"`) {
		t.Fatalf("the saved list: %d %s", saved.Code, saved.Body.String())
	}
	// A restore dialog for the reviewer says what restoring does and does not
	// show the removed text.
	code, markup = dialog("admin", "restore", "en-US")
	if code != http.StatusOK || !strings.Contains(markup, "Restore this message?") || strings.Contains(markup, "the words to be removed") || !strings.Contains(markup, `data-removal-action="restore"`) {
		t.Fatalf("the restore dialog: %d %s", code, markup)
	}
	// The author's page: why it was removed and how to ask for a review.
	own := f.call("author", http.MethodGet, ChatModerationPagePath+"?locale=en-US", nil)
	if own.Code != http.StatusOK || !strings.Contains(own.Body.String(), "Your message was removed") || !strings.Contains(own.Body.String(), "Spam or advertising") || !strings.Contains(own.Body.String(), `data-chatremove="appeal"`) || strings.Contains(own.Body.String(), `data-chatremove="search"`) {
		t.Fatalf("the author's page: %d %s", own.Code, own.Body.String())
	}
	// The queue page of a moderator carries the search and the open count.
	mod := f.call("admin", http.MethodGet, ChatModerationPagePath+"?locale=en-US", nil)
	if mod.Code != http.StatusOK || !strings.Contains(mod.Body.String(), `class="side-heading chatmod005-heading"`) || !strings.Contains(mod.Body.String(), `data-chatremove-close="true"`) || !strings.Contains(mod.Body.String(), "Nothing to review.") || strings.Contains(mod.Body.String(), `data-chatremove="filter"`) {
		t.Fatalf("the moderator's page: %d %s", mod.Code, mod.Body.String())
	}
}
