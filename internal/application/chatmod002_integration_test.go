package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// modFacts gives each subject the roles a workspace would: alice administers
// it, everyone else is an employee. The server resolves authority from these,
// never from the request.
type modFacts map[string][]string

func (f modFacts) ResolveChatFacts(_ context.Context, tenant, subject string, _ time.Time) (chatpolicy.Principal, error) {
	roles := f[subject]
	if roles == nil {
		roles = []string{"employee"}
	}
	return chatpolicy.Principal{ID: subject, Tenant: tenant, Active: true, Roles: roles, AuthorityRevision: 1}, nil
}

// modHarness is the real composed chat runtime over the shared test PostgreSQL
// with the served filter handler in front of it.
type modHarness struct {
	t       *testing.T
	dep     composedChat
	handler http.Handler
	bearer  map[string]string
	db      *pgtest.DB
	admit   transport.Config
	room    string
	rooms   map[string]string
}

func newModHarness(t *testing.T) *modHarness {
	t.Helper()
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
	t.Cleanup(core.Close)
	facts := modFacts{"alice": {"hcm_admin"}}
	dep, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "chatmod-integration-cursor-key"}, time.Now, facts, core, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dep.close)
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte("chatmod-integration-key-32-bytes!"), Issuer: "chatmod", Audience: "chat", Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	h := &modHarness{t: t, dep: dep, db: chatDB, bearer: map[string]string{}, rooms: map[string]string{}}
	for _, subject := range []string{"alice", "bob", "carol"} {
		token, err := verifier.Issue(trust.Claims{Issuer: "chatmod", Audience: "chat", Tenant: "host", Subject: subject, SubjectKind: "human", AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "chatmod-" + subject, IssuedAtUnix: time.Now().Add(-time.Minute).Unix(), ExpiresAtUnix: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		h.bearer[subject] = "Bearer " + token
	}
	h.admit = transport.Config{Verifier: verifier}
	h.handler = OverlayChatFilters(http.NotFoundHandler(), dep.filters, h.admit)
	h.room = h.createRoom("bob", "general")
	h.rooms["general"] = h.room
	return h
}

func (h *modHarness) ctx(subject string) context.Context {
	h.t.Helper()
	at := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "chatmod-session-" + subject, IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "chatmod-digest-" + subject})
	if err != nil {
		h.t.Fatal(err)
	}
	return transport.WithInvocation(trust.WithPrincipal(context.Background(), p), &transport.Invocation{})
}

func (h *modHarness) principal(subject string) chatcore.Principal {
	return chatcore.Principal{TenantID: "host", SubjectID: subject}
}

func (h *modHarness) createRoom(owner, name string) string {
	h.t.Helper()
	room, err := h.dep.service.CreateConversation(h.ctx(owner), chatcore.CreateConversationRequest{Principal: h.principal(owner), TenantID: "host", Kind: chatcore.PublicChannel, Name: name, IdempotencyKey: "chatmod-room-" + name})
	if err != nil {
		h.t.Fatalf("create %s: %v", name, err)
	}
	for _, member := range []string{"alice", "carol"} {
		if member == owner {
			continue
		}
		if _, err = h.dep.service.AddMembership(h.ctx(owner), chatcore.AddMembershipRequest{Principal: h.principal(owner), Membership: chatcore.Membership{ConversationID: room.ID, TenantID: "host", HomeTenantID: "host", SubjectID: member, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory}}); err != nil {
			h.t.Fatalf("add %s to %s: %v", member, name, err)
		}
	}
	return room.ID
}

var modKey int

func (h *modHarness) send(subject, room, body, parent string) (chatcore.Post, error) {
	modKey++
	return h.dep.service.SendPost(h.ctx(subject), chatcore.SendPostRequest{Principal: h.principal(subject), TenantID: "host", ConversationID: room, ParentID: parent, Body: body, IdempotencyKey: fmt.Sprintf("chatmod-send-%d", modKey)})
}

func (h *modHarness) list(subject, room string) []chatcore.Post {
	h.t.Helper()
	page, err := h.dep.service.ListPosts(h.ctx(subject), chatcore.ListPostsRequest{Principal: h.principal(subject), TenantID: "host", ConversationID: room, Page: chatcore.Page{PageSize: 100}})
	if err != nil {
		h.t.Fatalf("list as %s: %v", subject, err)
	}
	return page.Posts
}

func (h *modHarness) call(subject, method, path, body string) (int, string) {
	h.t.Helper()
	r := httptest.NewRequest(method, ChatFiltersPath+path, bytes.NewReader([]byte(body)))
	r.Header.Set("Authorization", h.bearer[subject])
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func modJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (h *modHarness) enable(subject, rule, channel, action string, want int) {
	h.t.Helper()
	code, body := h.call(subject, "POST", "/enable", modJSON(h.t, map[string]any{"Enablement": map[string]any{"RuleID": rule, "Channel": channel, "Action": action}}))
	if code != want {
		h.t.Fatalf("enable %s in %q as %s answered %d %s, want %d", rule, channel, subject, code, body, want)
	}
}

func (h *modHarness) disable(subject, rule, channel string, want int) {
	h.t.Helper()
	code, body := h.call(subject, "POST", "/disable", modJSON(h.t, map[string]any{"Enablement": map[string]any{"RuleID": rule, "Channel": channel}}))
	if code != want {
		h.t.Fatalf("disable %s in %q as %s answered %d %s, want %d", rule, channel, subject, code, body, want)
	}
}

func modBlocked(t *testing.T, err error, term string, body string) {
	t.Helper()
	var blocked *chatfilter.BlockedError
	if !errors.As(err, &blocked) || !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatalf("want a blocked refusal naming the term, got %v", err)
	}
	spans := append([]chatfilter.Span{blocked.Span}, blocked.Spans...)
	for _, span := range spans {
		if span.End <= len(body) && span.Start >= 0 && span.Start < span.End && strings.EqualFold(body[span.Start:span.End], term) {
			return
		}
	}
	t.Fatalf("the refusal does not name %q in %q: %+v", term, body, blocked)
}

// TestTodo_CHATMOD_002_Integration drives CHATMOD-002 end to end: the served
// filter handler for the administrator's switches and the really composed chat
// service for the people typing, over the shared test PostgreSQL.
func TestTodo_CHATMOD_002_Integration(t *testing.T) {
	h := newModHarness(t)
	random := h.createRoom("bob", "random")

	// Off by default: nothing is filtered until a workspace turns a list on.
	if _, err := h.send("bob", h.room, "well damn it", ""); err != nil {
		t.Fatalf("a list that was never enabled blocked a message: %v", err)
	}

	// The switches belong to the people who may manage filters.
	h.enable("carol", "builtin-en-profanity", "", "", http.StatusForbidden)
	h.enable("bob", "builtin-en-profanity", "", "", http.StatusForbidden) // a channel manager cannot act for the workspace
	h.enable("alice", "builtin-en-profanity", "", "block", http.StatusOK)

	// Blocked: not sent, the author told which word, nothing stored.
	before := len(h.list("carol", h.room))
	_, err := h.send("carol", h.room, "what the Damn!", "")
	modBlocked(t, err, "Damn", "what the Damn!")
	if len(h.list("carol", h.room)) != before {
		t.Fatal("a blocked message was stored")
	}
	if _, err = h.send("carol", random, "oh DAMN", ""); err == nil {
		t.Fatal("a workspace-wide switch did not apply in another channel")
	}

	// A thread reply is a message, and so is an edit.
	root, err := h.send("carol", h.room, "a clean root", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.send("carol", h.room, "damn reply", root.ID)
	modBlocked(t, err, "damn", "damn reply")
	_, err = h.dep.service.EditPost(h.ctx("carol"), chatcore.EditPostRequest{Principal: h.principal("carol"), TenantID: "host", ConversationID: h.room, PostID: root.ID, Body: "a d.a.m.n root", ExpectedRevision: root.Revision})
	modBlocked(t, err, "d.a.m.n", "a d.a.m.n root")

	// Innocent words that contain a listed one are not caught.
	for _, ok := range []string{"a classy assessment", "Scunthorpe is lovely", "the assistant passed"} {
		if _, err = h.send("carol", h.room, ok, ""); err != nil {
			t.Fatalf("%q was refused: %v", ok, err)
		}
	}

	// Per-channel override, switching a workspace-enabled list OFF for one
	// channel (the channel's manager may), then back to the workspace's setting.
	h.disable("bob", "builtin-en-profanity", h.room, http.StatusOK)
	if _, err = h.send("carol", h.room, "damn it, the override is off here", ""); err != nil {
		t.Fatalf("channel override off did not apply: %v", err)
	}
	if _, err = h.send("carol", random, "damn it, still on there", ""); err == nil {
		t.Fatal("a channel override leaked into another channel")
	}
	h.disable("carol", "builtin-en-profanity", h.room, http.StatusForbidden)

	// And the other direction: a list the workspace never enabled, switched ON
	// for one channel only.
	h.enable("bob", "builtin-de-profanity", h.room, "block", http.StatusOK)
	if _, err = h.send("carol", h.room, "na verdammt", ""); err == nil {
		t.Fatal("a channel override ON did not apply")
	}
	if _, err = h.send("carol", random, "na verdammt", ""); err != nil {
		t.Fatalf("a channel override ON applied to another channel: %v", err)
	}
	h.disable("bob", "builtin-de-profanity", h.room, http.StatusOK)

	// Mask: sent, readers and the author see the mask, the original is stored.
	h.enable("alice", "builtin-en-profanity", "", "mask", http.StatusOK)
	h.enable("bob", "builtin-en-profanity", h.room, "mask", http.StatusOK)
	sent, err := h.send("carol", h.room, "oh damn, again", "")
	if err != nil {
		t.Fatalf("a masked message was refused: %v", err)
	}
	if sent.Body != "oh [removed word], again" {
		t.Fatalf("the author was told %q", sent.Body)
	}
	for _, who := range []string{"bob", "carol", "alice"} {
		var found bool
		for _, post := range h.list(who, h.room) {
			if post.ID == sent.ID {
				found = true
				if post.Body != "oh [removed word], again" || strings.Contains(post.Body, "damn") {
					t.Fatalf("%s reads %q", who, post.Body)
				}
			}
		}
		if !found {
			t.Fatalf("%s cannot read the masked message", who)
		}
	}
	var stored string
	if err = h.db.SQL.QueryRowContext(context.Background(), `SELECT body FROM chat_post WHERE id=$1`, sent.ID).Scan(&stored); err != nil || stored != "oh damn, again" {
		t.Fatalf("the original must be stored unchanged: %q %v", stored, err)
	}

	// Flag: delivered exactly as sent, and a hit is recorded for review.
	h.enable("bob", "builtin-en-profanity", h.room, "flag", http.StatusOK)
	flagged, err := h.send("carol", h.room, "damn, flagged", "")
	if err != nil || flagged.Body != "damn, flagged" {
		t.Fatalf("a flagged message must be delivered as sent: %q %v", flagged.Body, err)
	}
	for _, post := range h.list("bob", h.room) {
		if post.ID == flagged.ID && post.Body != "damn, flagged" {
			t.Fatalf("a flagged message was altered for readers: %q", post.Body)
		}
	}
	code, body := h.call("bob", "GET", "/hits?channel="+url.QueryEscape(h.room), "")
	if code != http.StatusOK || !strings.Contains(body, `"Action":"flag"`) || !strings.Contains(body, "sha256:") || strings.Contains(strings.ToLower(body), `"damn`) {
		t.Fatalf("hits answered %d %s", code, body)
	}

	// Direct messages follow only hard rules: the built-in lists never apply.
	dm, err := h.dep.service.CreateConversation(h.ctx("carol"), chatcore.CreateConversationRequest{Principal: h.principal("carol"), TenantID: "host", Kind: chatcore.Direct, IdempotencyKey: "chatmod-dm", Members: []chatcore.MemberRef{{TenantID: "host", SubjectID: "bob"}}})
	if err != nil {
		t.Fatalf("direct conversation: %v", err)
	}
	h.enable("alice", "builtin-en-profanity", "", "block", http.StatusOK)
	if _, err = h.send("carol", dm.ID, "damn it, privately", ""); err != nil {
		t.Fatalf("a direct message was filtered by a built-in list: %v", err)
	}
}

// TestTodo_CHATMOD_002_Surfaces: every other place a person's text can enter is
// judged by the same filters: channel names, to-do items, poll text and widget
// text, and an agent's output.
func TestTodo_CHATMOD_002_Surfaces(t *testing.T) {
	h := newModHarness(t)
	h.enable("alice", "builtin-en-profanity", "", "block", http.StatusOK)

	_, err := h.dep.service.CreateConversation(h.ctx("bob"), chatcore.CreateConversationRequest{Principal: h.principal("bob"), TenantID: "host", Kind: chatcore.PublicChannel, Name: "damn-channel", IdempotencyKey: "chatmod-bad-name"})
	if !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatalf("a channel name bypassed the filters: %v", err)
	}

	todo := func(text string) error {
		_, err := h.dep.extensions.MutateChannelTodo(h.ctx("bob"), h.principal("bob"), "host", h.room, 1, chatcore.ChannelTodoMutation{Operation: "ADD", Text: text})
		return err
	}
	if err = todo("fix the damn build"); !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatalf("a to-do bypassed the filters: %v", err)
	}
	if err = todo("fix the build"); err != nil {
		t.Fatalf("a clean to-do was refused: %v", err)
	}
	_, err = h.dep.extensions.MutateChannelPoll(h.ctx("bob"), h.principal("bob"), "host", h.room, 1, chatcore.ChannelPollMutation{Operation: "CREATE", Question: "lunch?", Options: []string{"pizza", "damn salad"}})
	if !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatalf("a poll option bypassed the filters: %v", err)
	}
	_, err = h.dep.extensions.MutateChannelWidget(h.ctx("bob"), h.principal("bob"), "host", h.room, 1, chatcore.ChannelWidgetMutation{Kind: "TEAM", Operation: "SET_PURPOSE", Purpose: "a damn good team"})
	if !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatalf("a widget purpose bypassed the filters: %v", err)
	}

	// An agent's public answer and a scheduled announcement are written by the
	// store with a trusted author. The guard gives them the same filters.
	policy := &chatcore.FilterContentPolicy{Filters: h.dep.filters, Conversations: chatstore.NewAdapter(h.dep.store)}
	guard := chatmod002TrustedGuard(policy)
	if err = guard(h.ctx("bob"), "host", h.room, "assistant", "an answer with damn in it"); !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatalf("an agent answer bypassed the filters: %v", err)
	}
	if err = guard(h.ctx("bob"), "host", h.room, "assistant", "a clean answer"); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_CHATMOD_003_Integration: an administrator's own filters through the
// served handler, versioned, scoped, tried before saving, and judged on the
// real composed chat.
func TestTodo_CHATMOD_003_Integration(t *testing.T) {
	h := newModHarness(t)
	random := h.createRoom("bob", "random")
	custom := func(id, version string, kind string, match []string, action string, channels []string) string {
		return modJSON(t, map[string]any{"Definition": chatfilter.Definition{ID: id, Name: "Project code names", Version: version, Kind: kind, Match: match, Action: action, Channels: channels}})
	}

	// Try it: what a filter would do, before anything is saved.
	code, body := h.call("alice", "POST", "/try", modJSON(t, map[string]any{"Definition": chatfilter.Definition{ID: "phoenix", Name: "Project code names", Version: "1.0.0", Kind: "words", Match: []string{"project phoenix"}, Action: "mask"}, "Channel": h.room, "Sample": "about Project  PHOENIX today"}))
	if code != http.StatusOK || !strings.Contains(body, `"Action":"mask"`) || !strings.Contains(body, "about [removed word] today") {
		t.Fatalf("try answered %d %s", code, body)
	}
	if code, body = h.call("alice", "GET", "", ""); code != http.StatusOK || strings.Contains(body, "phoenix") {
		t.Fatalf("trying a filter saved it: %d %s", code, body)
	}

	// A pattern that could backtrack is refused when saved, with a typed error.
	for _, bad := range []string{"(a+)+$", "(?=x)y", `(a)\1`, ".*secret", strings.Repeat("a", 300)} {
		code, body = h.call("alice", "POST", "/versions", custom("bad", "1.0.0", "pattern", []string{bad}, "block", nil))
		if code != http.StatusBadRequest || !strings.Contains(body, "invalid_filter") {
			t.Fatalf("pattern %q was accepted or mis-reported: %d %s", bad, code, body)
		}
	}

	// A workspace administrator creates a workspace-wide filter; versions are monotonic.
	if code, body = h.call("alice", "POST", "/versions", custom("phoenix", "1.0.0", "words", []string{"project phoenix", "ACME-77"}, "block", nil)); code != http.StatusOK {
		t.Fatalf("create: %d %s", code, body)
	}
	if code, _ = h.call("alice", "POST", "/versions", custom("phoenix", "1.0.0", "words", []string{"x"}, "block", nil)); code != http.StatusConflict {
		t.Fatalf("a shipped version was replaced: %d", code)
	}
	h.enable("alice", "phoenix", "", "", http.StatusOK)
	_, err := h.send("carol", h.room, "news on Project Phoenix", "")
	modBlocked(t, err, "Project Phoenix", "news on Project Phoenix")
	if _, err = h.send("carol", random, "acme-77 is due", ""); err == nil {
		t.Fatal("a workspace filter did not apply in every channel")
	}
	// A new version changes the terms for new judgements; the hit records which version judged.
	if code, body = h.call("alice", "POST", "/versions", custom("phoenix", "1.1.0", "words", []string{"project phoenix"}, "block", nil)); code != http.StatusOK {
		t.Fatalf("new version: %d %s", code, body)
	}
	if _, err = h.send("carol", random, "acme-77 is fine now", ""); err != nil {
		t.Fatalf("version 1.1.0 still judged by 1.0.0: %v", err)
	}
	code, body = h.call("alice", "GET", "/hits", "")
	if code != http.StatusOK || !strings.Contains(body, `"Version":"1.0.0"`) {
		t.Fatalf("hits do not record the judging version: %d %s", code, body)
	}

	// A channel manager may write a filter for his or her channel only, and can
	// never weaken the workspace's.
	if code, body = h.call("bob", "POST", "/versions", custom("room-words", "1.0.0", "words", []string{"mercury"}, "block", []string{h.room})); code != http.StatusOK {
		t.Fatalf("channel filter: %d %s", code, body)
	}
	h.enable("bob", "room-words", h.room, "", http.StatusOK)
	if _, err = h.send("carol", h.room, "mercury rising", ""); err == nil {
		t.Fatal("the channel's own filter did not apply")
	}
	if _, err = h.send("carol", random, "mercury rising", ""); err != nil {
		t.Fatalf("a channel filter applied to another channel: %v", err)
	}
	if code, _ = h.call("bob", "POST", "/versions", custom("everywhere", "1.0.0", "words", []string{"venus"}, "block", nil)); code != http.StatusForbidden {
		t.Fatalf("a channel manager wrote a workspace filter: %d", code)
	}
	h.disable("bob", "phoenix", h.room, http.StatusForbidden)
	if _, err = h.send("carol", h.room, "Project Phoenix again", ""); err == nil {
		t.Fatal("a channel manager switched off a workspace filter")
	}

	// Strictest action wins when two filters match the same text.
	if code, body = h.call("alice", "POST", "/versions", custom("soft", "1.0.0", "words", []string{"phoenix"}, "flag", nil)); code != http.StatusOK {
		t.Fatalf("soft: %d %s", code, body)
	}
	h.enable("alice", "soft", "", "", http.StatusOK)
	if _, err = h.send("carol", h.room, "Project Phoenix once more", ""); err == nil {
		t.Fatal("a flag outranked a block")
	}

	// Exemptions: a named role is not judged.
	exempt := modJSON(t, map[string]any{"Definition": chatfilter.Definition{ID: "exempt", Name: "Exempt rule", Version: "1.0.0", Kind: "words", Match: []string{"saturn"}, Action: "block", ExemptRoles: []string{"hcm_admin"}}})
	if code, body = h.call("alice", "POST", "/versions", exempt); code != http.StatusOK {
		t.Fatalf("exempt: %d %s", code, body)
	}
	h.enable("alice", "exempt", "", "", http.StatusOK)
	if _, err = h.send("alice", h.room, "saturn", ""); err != nil {
		t.Fatalf("an exempt administrator was judged: %v", err)
	}
	if _, err = h.send("carol", h.room, "saturn", ""); err == nil {
		t.Fatal("a non-exempt person was not judged")
	}

	// Dry run: for a week the filter only records what it would have done.
	if code, body = h.call("alice", "POST", "/versions", custom("dry", "1.0.0", "words", []string{"neptune"}, "block", nil)); code != http.StatusOK {
		t.Fatalf("dry: %d %s", code, body)
	}
	if code, body = h.call("alice", "POST", "/enable", modJSON(t, map[string]any{"Enablement": map[string]any{"RuleID": "dry"}, "DryRun": true})); code != http.StatusOK {
		t.Fatalf("dry enable: %d %s", code, body)
	}
	if _, err = h.send("carol", h.room, "neptune is far", ""); err != nil {
		t.Fatalf("a filter in dry run acted: %v", err)
	}
	if code, body = h.call("alice", "GET", "/hits?q=dry", ""); code != http.StatusOK || !strings.Contains(body, `"DryRun":true`) || !strings.Contains(body, `"RuleID":"dry"`) {
		t.Fatalf("the dry run was not recorded: %d %s", code, body)
	}

	// Searchable by those who may manage filters, closed to everyone else.
	if code, body = h.call("alice", "GET", "?q=phoenix", ""); code != http.StatusOK || !strings.Contains(body, "Project code names") {
		t.Fatalf("search: %d %s", code, body)
	}
	if code, _ = h.call("carol", "GET", "?q=phoenix", ""); code != http.StatusForbidden {
		t.Fatalf("a member searched the filters: %d", code)
	}
}

// TestTodo_CHATMOD_002_Stream: the live feed a reader's browser holds open is
// the other way a message reaches a person. A masked message arrives masked,
// and the stored body is untouched.
func TestTodo_CHATMOD_002_Stream(t *testing.T) {
	h := newModHarness(t)
	h.enable("alice", "builtin-en-profanity", "", "mask", http.StatusOK)
	ctx, cancel := context.WithTimeout(h.ctx("alice"), 30*time.Second)
	defer cancel()
	events, err := h.dep.service.WatchConversation(ctx, chatcore.WatchConversationRequest{Principal: h.principal("alice"), TenantID: "host", ConversationID: h.room})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := h.send("carol", h.room, "this is damn live", "")
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("the stream ended before the message arrived")
			}
			if event.Event.Post == nil || event.Event.Post.ID != sent.ID {
				continue
			}
			if event.Event.Post.Body != "this is [removed word] live" {
				t.Fatalf("the live event carried %q", event.Event.Post.Body)
			}
			return
		case <-ctx.Done():
			t.Fatal("no live event arrived")
		}
	}
}

type modGateRecorder struct{ calls int }

func (r *modGateRecorder) GateRequest(context.Context, ChatgateRequest) (ChatgateReply, error) {
	r.calls++
	return ChatgateReply{}, nil
}

type modGateRoutes struct{}

func (modGateRoutes) ChatWriteContext(ctx context.Context, _, _ string) (context.Context, error) {
	return ctx, nil
}

// TestTodo_CHATMOD_002_Gate: the free text of a gate submission and a
// reviewer's reason is judged before the gate sees it, and a refusal names the
// answer field and reaches the browser as a typed, field-level 422.
func TestTodo_CHATMOD_002_Gate(t *testing.T) {
	h := newModHarness(t)
	h.enable("alice", "builtin-en-profanity", "", "block", http.StatusOK)
	recorder := &modGateRecorder{}
	surface := integrate2GateSurface{ChatgateSurface: recorder, Routes: modGateRoutes{}, Filter: &chatcore.FilterContentPolicy{Filters: h.dep.filters}}
	answers := map[string]json.RawMessage{"why": json.RawMessage(`"I like pizza"`), "count": json.RawMessage(`42`), "multi": json.RawMessage(`["fine","a damn answer"]`)}
	_, err := surface.GateRequest(h.ctx("carol"), ChatgateRequest{Action: "submit", Conversation: h.room, Answers: answers})
	var field chatgate.FieldError
	if !errors.As(err, &field) || field.Field != "multi" || !errors.Is(err, chatfilter.ErrBlocked) || recorder.calls != 0 {
		t.Fatalf("a gate answer bypassed the filters: %v (calls %d)", err, recorder.calls)
	}
	w := httptest.NewRecorder()
	chatgateHTTPError(w, err)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"error":"content_blocked"`) || !strings.Contains(w.Body.String(), `"multi":"content_blocked"`) {
		t.Fatalf("the browser was told %d %s", w.Code, w.Body.String())
	}
	if _, err = surface.GateRequest(h.ctx("alice"), ChatgateRequest{Action: "decline", Conversation: h.room, Reason: "damn it, no"}); !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatalf("a reviewer's reason bypassed the filters: %v", err)
	}
	answers["multi"] = json.RawMessage(`["fine"]`)
	if _, err = surface.GateRequest(h.ctx("carol"), ChatgateRequest{Action: "submit", Conversation: h.room, Answers: answers}); err != nil || recorder.calls != 1 {
		t.Fatalf("clean answers were refused: %v (calls %d)", err, recorder.calls)
	}
	// A draft is not published to anyone: it is not judged.
	answers["why"] = json.RawMessage(`"damn"`)
	if _, err = surface.GateRequest(h.ctx("carol"), ChatgateRequest{Action: "save", Conversation: h.room, Answers: answers}); err != nil {
		t.Fatalf("a draft was judged: %v", err)
	}
}

// TestTodo_CHATMOD_002_Features: the served features endpoint tells the client
// whether filters exist, so the Filters section appears only where the server
// can answer for it.
func TestTodo_CHATMOD_002_Features(t *testing.T) {
	h := newModHarness(t)
	for name, filters := range map[string]*chatfilter.Service{"composed": h.dep.filters, "absent": nil} {
		assembly := &agentServedAssembly{Filters: filters}
		r := httptest.NewRequest(http.MethodGet, integrate2FeaturesPath, nil)
		r.Header.Set("Authorization", h.bearer["alice"])
		w := httptest.NewRecorder()
		assembly.overlayChatFeatures(http.NotFoundHandler(), h.admit).ServeHTTP(w, r)
		var features struct{ Filters bool }
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &features) != nil || features.Filters != (filters != nil) {
			t.Fatalf("%s: features answered %d %s", name, w.Code, w.Body.String())
		}
	}
}
