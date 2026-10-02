package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

type chattoneFactsFixture struct{ admins map[string]bool }

func (f chattoneFactsFixture) ResolveChatFacts(_ context.Context, tenant, subject string, _ time.Time) (chatpolicy.Principal, error) {
	p := chatpolicy.Principal{ID: subject, Tenant: tenant, Active: true, AuthorityRevision: 1}
	if f.admins[subject] {
		p.Roles = []string{"hcm_admin"}
	}
	return p, nil
}

type chattoneFilterAuthority struct{}

func (chattoneFilterAuthority) AuthorizeFilters(context.Context, chatfilter.Actor, string) error {
	return nil
}
func (chattoneFilterAuthority) CanReadFilterConversation(context.Context, chatfilter.Actor, string) bool {
	return true
}

type chattoneServedStack struct {
	t         *testing.T
	handler   http.Handler
	bearer    map[string]string
	service   *ChattoneService
	provider  *chattoneProvider
	ledger    *agentbudget.Ledger
	audit     *chattoneAuditSpy
	chat      *chat.Service
	filters   *chatfilter.Service
	member    chat.Membership
	owner     chat.Principal
	store     *chatstore.Store
	admission transport.Config
	compose   func() (*ChattoneService, string, error)
}

const (
	chattoneTenant     = "tenant-a"
	chattoneRoom       = "room"
	chattoneDraftAngry = "You idiot, the deploy really broke prod again. @dana needs to fix 42 files by 2026-10-01. Please do it NOW."
)

var chattoneRoomPosts = []string{"Standup moved to 10", "Keep replies short please", "Thanks all, shipping the patch"}

// newChattoneServedStack builds the served writing-style service the way the
// serve composition does (composeServedChattone), over a real chat service,
// real content filters and the real reader selection on the shared test
// PostgreSQL, a real governed gateway, and a deterministic stand-in for the
// provider's HTTP API. userDailySteps is the platform's per-person daily step
// ceiling the shared budget ledger enforces.
func newChattoneServedStack(t *testing.T, userDailySteps int64) *chattoneServedStack {
	return newChattoneServedStackFor(t, userDailySteps, false)
}

// newChattoneServedStackFor with local set composes under the local development
// profile (in a scratch working directory, reading the dedicated deployment path
// the profile reads), which is the only profile that turns a workspace on.
func newChattoneServedStackFor(t *testing.T, userDailySteps int64, local bool) *chattoneServedStack {
	return newChattoneServedStackWith(t, userDailySteps, local, 0)
}

// newChattoneServedStackWith also sets the per-person daily operation ceiling
// the durable ledger enforces (zero keeps the product default).
func newChattoneServedStackWith(t *testing.T, userDailySteps int64, local bool, dailyOperations int) *chattoneServedStack {
	t.Helper()
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
	owner := chat.Principal{TenantID: chattoneTenant, SubjectID: "owner"}
	if _, err := service.CreateConversation(ctx, chat.CreateConversationRequest{Principal: owner, TenantID: chattoneTenant, ConversationID: chattoneRoom, Kind: chat.PublicChannel, Name: "team-room"}); err != nil {
		t.Fatal(err)
	}
	var member chat.Membership
	for _, person := range []string{"alice", "admin"} {
		m, err := service.AddMembership(ctx, chat.AddMembershipRequest{Principal: owner, Membership: chat.Membership{TenantID: chattoneTenant, HomeTenantID: chattoneTenant, ConversationID: chattoneRoom, SubjectID: person, HistoryVisibility: chat.FullHistory}})
		if err != nil {
			t.Fatal(err)
		}
		if person == "alice" {
			member = m
		}
	}
	for i, body := range chattoneRoomPosts {
		if _, err := service.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: chattoneTenant, ConversationID: chattoneRoom, Body: body, IdempotencyKey: "post-" + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	facts := chattoneFactsFixture{admins: map[string]bool{"admin": true}}
	filters := NewChatFilterService(raw, chattoneFilterAuthority{}, now)
	renderings := &ChatRenderingSurface{Store: raw, Policy: integrate2RenderingPolicy{Store: raw, Chat: service, Filter: &chat.FilterContentPolicy{Filters: filters, Conversations: adapter, AuthorIdentity: chatFilterIdentity{facts: facts, now: now}}}}

	provider := newChattoneProvider(t)
	ledger, err := agentbudget.New(chattoneBudgetPolicy(userDailySteps))
	if err != nil {
		t.Fatal(err)
	}
	spy := &chattoneAuditSpy{Store: agentaudit.NewMemoryStore()}
	dep := chattoneTestDeployment(t, provider)
	raw2, err := json.Marshal(dep)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "chat-writing-style-deployment.json")
	if err := os.WriteFile(path, raw2, 0o600); err != nil {
		t.Fatal(err)
	}
	material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.Join(t.TempDir(), "signing.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := func(key string) string {
		switch key {
		case EnvChatWritingStyleModelConfigFile:
			return path
		case "MODEL_API_KEY":
			return "test-only-provider-key"
		}
		return ""
	}
	cfg := ServeConfig{Profile: ServeProfileStandard, CellID: dep.Worker.Cell, PersonaOutputSigningSeed: material.OutputSeed, PersonaWorkloadSigningSeed: material.WorkloadSeed}
	if local {
		t.Chdir(t.TempDir())
		localPath := filepath.FromSlash(localChattoneModelDeploymentPath)
		if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(localPath, raw2, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg = ServeConfig{Profile: ServeProfileLocalDev, CellID: "local-cell", Tenant: localAgentDemoTenant}
		env = func(key string) string {
			if key == "MODEL_API_KEY" {
				return "test-only-provider-key"
			}
			return ""
		}
	}
	compose := func() (*ChattoneService, string, error) {
		return composeServedChattone(ctx, chattoneServeInput{Config: cfg, Runtime: &agentRuntime{Budget: ledger, Audit: spy}, Chat: composedChat{service: service, renderings: renderings, filters: filters, store: raw},
			Facts: facts, Env: env, Now: now, DailyOperations: dailyOperations})
	}
	svc, reason, err := compose()
	if err != nil || svc == nil {
		t.Fatalf("composition: %v %s", err, reason)
	}
	admission, aliceBearer := integrate1Admission(t, chattoneTenant, "alice", now)
	_, adminBearer := integrate1Admission(t, chattoneTenant, "admin", now)
	_, otherBearer := integrate1Admission(t, chattoneTenant, "bystander", now)
	assembly := &agentServedAssembly{WritingStyles: svc}
	return &chattoneServedStack{t: t, handler: assembly.Overlay(http.NotFoundHandler(), admission), bearer: map[string]string{"alice": aliceBearer, "admin": adminBearer, "bystander": otherBearer},
		service: svc, provider: provider, ledger: ledger, audit: spy, chat: service, filters: filters, member: member, owner: owner, store: raw, admission: admission, compose: compose}
}

// restart composes the service again over the same database, as a new process
// would, and serves it on a fresh handler.
func (s *chattoneServedStack) restart() *chattoneServedStack {
	s.t.Helper()
	svc, reason, err := s.compose()
	if err != nil || svc == nil {
		s.t.Fatalf("recomposition: %v %s", err, reason)
	}
	next := *s
	next.service = svc
	next.handler = (&agentServedAssembly{WritingStyles: svc}).Overlay(http.NotFoundHandler(), s.admission)
	return &next
}

// usageLines counts the durable outcome lines (not the reservations) written for
// the workspace.
func (s *chattoneServedStack) usageLines() (lines, reservations int) {
	s.t.Helper()
	err := s.store.RunTenantTx(context.Background(), chattoneTenant, func(tx dbport.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM chattone_usage WHERE operation IN ('rewrite','meaning')`).Scan(&lines); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM chattone_usage WHERE operation='reserve'`).Scan(&reservations)
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return lines, reservations
}

func (s *chattoneServedStack) call(person, method, path, body string) (int, string) {
	s.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if person != "" {
		req.Header.Set("Authorization", s.bearer[person])
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func (s *chattoneServedStack) features(person string) chatui.ChatFeatures {
	s.t.Helper()
	code, body := s.call(person, http.MethodGet, integrate2FeaturesPath, "")
	var f chatui.ChatFeatures
	if code != 200 || json.Unmarshal([]byte(body), &f) != nil {
		s.t.Fatalf("features: %d %s", code, body)
	}
	return f
}

func (s *chattoneServedStack) rewrite(person, draft, style string) (int, chattoneRewriteReply) {
	s.t.Helper()
	payload, _ := json.Marshal(map[string]string{"conversation_id": chattoneRoom, "draft": draft, "style_id": style})
	code, body := s.call(person, http.MethodPost, ChattonePath+"/rewrite", string(payload))
	var reply chattoneRewriteReply
	_ = json.Unmarshal([]byte(body), &reply)
	reply.raw = body
	return code, reply
}

type chattoneRewriteReply struct {
	Draft   string `json:"draft"`
	Enabled bool   `json:"enabled"`
	Error   string `json:"error"`
	raw     string
}

func (s *chattoneServedStack) suggestion(person string) (int, ChattoneReply) {
	s.t.Helper()
	code, body := s.call(person, http.MethodGet, ChattonePath+"/suggestion?conversation_id="+chattoneRoom, "")
	var reply ChattoneReply
	_ = json.Unmarshal([]byte(body), &reply)
	return code, reply
}

func chattoneFakeContext(t *testing.T, req chattoneProviderRequest) (draft string, context []string) {
	t.Helper()
	raw := strings.TrimSuffix(strings.TrimPrefix(req.User, "<untrusted_data>\n"), "\n</untrusted_data>")
	var data struct {
		Draft           string   `json:"draft"`
		RegisterContext []string `json:"register_context"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatalf("provider data %q: %v", req.User, err)
	}
	return data.Draft, data.RegisterContext
}

func TestTodo_CHATTONE_004_ServedIntegration(t *testing.T) {
	s := newChattoneServedStack(t, 200)
	if got := s.features("alice"); got.WritingStyles {
		t.Fatal("a workspace nobody has turned the controls on for is offered them")
	}
	if code, reply := s.suggestion("alice"); code != 200 || reply.Enabled || reply.Suggestion != nil || len(reply.Styles) != 0 {
		t.Fatalf("suggestion while off: %d %+v", code, reply)
	}
	if code, reply := s.rewrite("alice", chattoneDraftAngry, "professional"); code != 403 || reply.Error != "disabled" || s.provider.calls() != 0 {
		t.Fatalf("rewrite while off: %d %s calls %d", code, reply.raw, s.provider.calls())
	}
	if err := ChattoneEnableTenant(s.service.Rewrite.Registry, chattoneTenant); err != nil {
		t.Fatal(err)
	}

	t.Run("controls are offered, with the style best for this audience", func(t *testing.T) {
		if got := s.features("alice"); !got.WritingStyles {
			t.Fatal("an enabled workspace is not offered the controls")
		}
		code, reply := s.suggestion("alice")
		if code != 200 || !reply.Enabled || reply.Suggestion == nil || len(reply.Styles) != 3 {
			t.Fatalf("suggestion: %d %+v", code, reply)
		}
		// Three short messages from two people: short and direct wins.
		if reply.Suggestion.StyleID != "concise" || reply.Suggestion.ReasonKey != "short" || reply.Suggestion.Reason == "" || !reply.Suggestion.ExpiresAt.After(time.Now()) {
			t.Fatalf("suggestion %+v", reply.Suggestion)
		}
		for _, style := range reply.Styles {
			if style.Instruction != "" {
				t.Fatal("a style's instruction is public")
			}
		}
		if s.provider.calls() != 0 {
			t.Fatal("reading a suggestion called the model")
		}
	})

	t.Run("rewrite in each of the three styles", func(t *testing.T) {
		cleaned := "the deploy broke prod again. @dana needs to fix 42 files by 2026-10-01. Please do it NOW."
		for _, tc := range []struct{ style, want, instruction string }{
			{"professional", "Hello, " + cleaned + " Thank you.", "neutral, courteous"},
			{"friendly", "Hi! " + cleaned + " Thanks so much!", "warm, positive"},
			{"concise", strings.ReplaceAll(cleaned, "Please ", ""), "shorter, direct"},
		} {
			before := s.provider.calls()
			code, reply := s.rewrite("alice", chattoneDraftAngry, tc.style)
			if code != 200 || reply.Draft != tc.want || !reply.Enabled {
				t.Fatalf("%s: %d %s", tc.style, code, reply.raw)
			}
			if s.provider.calls() != before+1 {
				t.Fatalf("%s: model calls %d", tc.style, s.provider.calls()-before)
			}
			sent := s.provider.last()
			draft, register := chattoneFakeContext(t, sent)
			if !strings.Contains(sent.System, tc.instruction) || sent.Authorization != "Bearer test-only-provider-key" || sent.Store {
				t.Fatalf("%s: instruction or credential: %q", tc.style, sent.System)
			}
			// The writer's facts never left as text: mentions, dates and links are placeholders.
			if strings.Contains(draft, "@dana") || strings.Contains(draft, "2026-10-01") || !strings.Contains(draft, "⟦HCM:") {
				t.Fatalf("%s: protected spans reached the provider: %q", tc.style, draft)
			}
			// Only this conversation's recent messages, oldest first, for register.
			if strings.Join(register, "|") != strings.Join(chattoneRoomPosts, "|") {
				t.Fatalf("%s: register context %q", tc.style, register)
			}
		}
		// Each press took one place (the model call) and wrote its outcome lines to
		// the chat store (a rewrite and a meaning check), not to memory.
		if lines, places := s.usageLines(); lines != 6 || places != 3 {
			t.Fatalf("durable usage: %d lines %d places", lines, places)
		}
		// Nothing was sent: the room still holds only the three posts.
		page, err := s.chat.ListPosts(context.Background(), chat.ListPostsRequest{Principal: chat.Principal{TenantID: chattoneTenant, SubjectID: "alice"}, TenantID: chattoneTenant, ConversationID: chattoneRoom, Page: chat.Page{PageSize: 50}})
		if err != nil || len(page.Posts) != len(chattoneRoomPosts) {
			t.Fatalf("a preview created or changed a post: %d %v", len(page.Posts), err)
		}
		// Cost was recorded in the shared ledger against alice's day.
		var used agentbudget.Usage
		for _, task := range s.ledger.Snapshot().Tasks {
			if task.ID == ChattoneBudgetTaskID(chatrewrite.Identity{Tenant: chattoneTenant, Person: "alice", Conversation: chattoneRoom}, time.Now()) {
				used = task.Used
			}
		}
		if used.Steps != 3 || used.SpendMicros != 360 || used.Tokens != 90 {
			t.Fatalf("recorded usage %+v", used)
		}
		s.audit.mu.Lock()
		routes := len(s.audit.entries)
		s.audit.mu.Unlock()
		if routes != 3 {
			t.Fatalf("route records %d", routes)
		}
	})

	t.Run("a rewrite that changes the meaning is refused, not offered", func(t *testing.T) {
		before := s.provider.calls()
		code, reply := s.rewrite("alice", "No, I will not approve FLIP the 42 budget lines for @dana.", "professional")
		if code != 422 || reply.Error != "preservation" || reply.Draft != "" || s.provider.calls() != before+2 {
			t.Fatalf("meaning change: %d %s calls %d", code, reply.raw, s.provider.calls()-before)
		}
	})

	t.Run("a secret the model could read is refused before the model; a number is never read", func(t *testing.T) {
		before := s.provider.calls()
		code, reply := s.rewrite("alice", "use sk-abcdefghijklmnopqrstuv for the deploy and please fix it now", "professional")
		if code != 422 || reply.Error != "policy" || reply.Draft != "" || s.provider.calls() != before {
			t.Fatalf("secret in a draft: %d %s", code, reply.raw)
		}
		// Identifiers made of digits are protected spans: they are replaced by
		// placeholders before the draft leaves and restored in the preview, so the
		// provider never receives them.
		code, reply = s.rewrite("alice", "my id is 123-45-6789 so please fix the record now", "professional")
		if code != 200 || !strings.Contains(reply.Draft, "123-45-6789") || s.provider.calls() != before+1 {
			t.Fatalf("protected identifier: %d %s", code, reply.raw)
		}
		if sent, _ := chattoneFakeContext(t, s.provider.last()); strings.Contains(sent, "123-45-6789") {
			t.Fatalf("an identifier reached the provider: %q", sent)
		}
	})

	t.Run("the workspace's hard filters apply to the draft, and a draft leaves no hit", func(t *testing.T) {
		actor := chatfilter.Actor{Tenant: chattoneTenant, Subject: "admin"}
		ctx := context.Background()
		if err := s.filters.CreateVersion(ctx, actor, chatfilter.Definition{ID: "project-rule", Name: "Project rule", Version: "1.0.0", Kind: "words", Match: []string{"quartz"}, Action: "block", Hard: true}); err != nil {
			t.Fatal(err)
		}
		if err := s.filters.Enable(ctx, actor, chatfilter.Enablement{RuleID: "project-rule", Enabled: true}, false); err != nil {
			t.Fatal(err)
		}
		before := s.provider.calls()
		code, reply := s.rewrite("alice", "the quartz deploy broke prod again so please fix it now", "professional")
		if code != 422 || reply.Error != "policy" || s.provider.calls() != before {
			t.Fatalf("blocked draft: %d %s", code, reply.raw)
		}
		if hits, err := s.filters.Store.Hits(ctx, chattoneTenant); err != nil || len(hits) != 0 {
			t.Fatalf("a draft that was never sent left %d filter hits (%v)", len(hits), err)
		}
		if code, reply := s.rewrite("alice", "the deploy broke prod again so please fix it now", "professional"); code != 200 || reply.Draft == "" {
			t.Fatalf("an unblocked draft was refused: %d %s", code, reply.raw)
		}
	})

	t.Run("only a member who is signed in may ask", func(t *testing.T) {
		before := s.provider.calls()
		if code, _ := s.call("", http.MethodGet, ChattonePath+"/suggestion?conversation_id="+chattoneRoom, ""); code != 401 {
			t.Fatalf("unauthenticated: %d", code)
		}
		payload := `{"conversation_id":"` + chattoneRoom + `","draft":"Please fix the broken deploy now","style_id":"professional"}`
		if code, body := s.call("bystander", http.MethodPost, ChattonePath+"/rewrite", payload); code != 403 {
			t.Fatalf("a person outside the room: %d %s", code, body)
		}
		if code, body := s.call("bystander", http.MethodGet, ChattonePath+"/suggestion?conversation_id="+chattoneRoom, ""); code != 403 {
			t.Fatalf("a suggestion for a person outside the room: %d %s", code, body)
		}
		if s.provider.calls() != before {
			t.Fatal("a refused request reached the model")
		}
	})

	t.Run("only an administrator changes the styles, and house styles are used", func(t *testing.T) {
		body := `{"conversation_id":"` + chattoneRoom + `","enabled":true,"styles":[` +
			`{"id":"professional","label":"Boardroom","instruction":"Write as if for a board paper.","register":"professional"},` +
			`{"id":"friendly","label":"Friendly","instruction":"Use warm, positive language.","register":"friendly"},` +
			`{"id":"concise","label":"Concise","instruction":"Use shorter, direct language, keeping the same content.","register":"concise"}]}`
		if code, out := s.call("alice", http.MethodPost, ChattonePath+"/styles", body); code != 403 {
			t.Fatalf("a member changed the styles: %d %s", code, out)
		}
		if code, out := s.call("admin", http.MethodPost, ChattonePath+"/styles", body); code != 200 {
			t.Fatalf("administrator: %d %s", code, out)
		}
		code, reply := s.suggestion("alice")
		labels := []string{}
		for _, style := range reply.Styles {
			labels = append(labels, style.Label)
		}
		if code != 200 || strings.Join(labels, ",") != "Boardroom,Friendly,Concise" {
			t.Fatalf("styles after the change: %d %v", code, labels)
		}
		if code, reply := s.rewrite("alice", "Please fix the broken deploy pipeline now", "professional"); code != 200 || reply.Draft == "" || !strings.Contains(s.provider.last().System, "Write as if for a board paper.") {
			t.Fatalf("house style not used: %d %s", code, reply.raw)
		}
	})

	t.Run("a workspace that turns it off has no controls and no model calls", func(t *testing.T) {
		if err := s.service.Rewrite.Registry.Configure(chattoneTenant, false, chatrewrite.DefaultStyles()); err != nil {
			t.Fatal(err)
		}
		before := s.provider.calls()
		if got := s.features("alice"); got.WritingStyles {
			t.Fatal("the features endpoint still offers the controls")
		}
		if code, reply := s.suggestion("alice"); code != 200 || reply.Enabled || len(reply.Styles) != 0 {
			t.Fatalf("suggestion after off: %d %+v", code, reply)
		}
		if code, reply := s.rewrite("alice", chattoneDraftAngry, "professional"); code != 403 || reply.Error != "disabled" || s.provider.calls() != before {
			t.Fatalf("rewrite after off: %d %s", code, reply.raw)
		}
		if err := s.service.Rewrite.Registry.Configure(chattoneTenant, true, chatrewrite.DefaultStyles()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a person who leaves the room is refused before the model", func(t *testing.T) {
		if _, err := s.chat.RemoveMembership(context.Background(), chat.RemoveMembershipRequest{Principal: s.owner, TenantID: chattoneTenant, HomeTenantID: chattoneTenant, ConversationID: chattoneRoom, SubjectID: "alice", ExpectedRevision: s.member.Revision}); err != nil {
			t.Fatal(err)
		}
		before := s.provider.calls()
		if code, reply := s.rewrite("alice", chattoneDraftAngry, "professional"); code != 403 || s.provider.calls() != before {
			t.Fatalf("revoked member: %d %s", code, reply.raw)
		}
		if got := s.features("alice"); !got.WritingStyles {
			t.Fatal("the workspace feature flag depends on a room")
		}
	})
}

// TestTodo_CHATTONE_004_ServedBudgetExhausted drives the same stack until the
// shared budget says no: the writer is told plainly (429, "limit"), no draft is
// offered, and the provider is not called again.
func TestTodo_CHATTONE_004_ServedBudgetExhausted(t *testing.T) {
	s := newChattoneServedStack(t, 2)
	if err := ChattoneEnableTenant(s.service.Rewrite.Registry, chattoneTenant); err != nil {
		t.Fatal(err)
	}
	if code, reply := s.rewrite("alice", chattoneDraftAngry, "professional"); code != 200 || reply.Draft == "" {
		t.Fatalf("first rewrite: %d %s", code, reply.raw)
	}
	if code, reply := s.rewrite("alice", chattoneDraftAngry, "friendly"); code != 200 || reply.Draft == "" {
		t.Fatalf("second rewrite: %d %s", code, reply.raw)
	}
	code, reply := s.rewrite("alice", chattoneDraftAngry, "concise")
	if code != 429 || reply.Error != "limit" || reply.Draft != "" || s.provider.calls() != 2 {
		t.Fatalf("a spent budget: %d %s calls %d", code, reply.raw, s.provider.calls())
	}
	// Another person has their own day; the ceiling is per person.
	if code, reply := s.rewrite("admin", chattoneDraftAngry, "professional"); code != 200 || reply.Draft == "" {
		t.Fatalf("a different person was held to alice's day: %d %s", code, reply.raw)
	}
}

// TestTodo_CHATTONE_004_ServedLocalDev: under the local development profile,
// and only there, the composition root turns the development tenant on. Every
// other workspace stays off until its own administrator turns it on, and the
// standard profile never enables anyone.
func TestTodo_CHATTONE_004_ServedLocalDev(t *testing.T) {
	standard := newChattoneServedStack(t, 200)
	if standard.service.Rewrite.Registry.Configured(localAgentDemoTenant) {
		t.Fatal("the standard profile turned the development tenant on")
	}
	s := newChattoneServedStackFor(t, 200, true)
	styles, enabled := s.service.Rewrite.Registry.Styles(localAgentDemoTenant)
	if !enabled || len(styles) != 3 || !s.service.Rewrite.Registry.Configured(localAgentDemoTenant) {
		t.Fatalf("the development tenant is not on: %v %v", enabled, styles)
	}
	if _, other := s.service.Rewrite.Registry.Styles(chattoneTenant); other {
		t.Fatal("a workspace other than the development tenant was turned on")
	}
	if got := s.features("alice"); got.WritingStyles {
		t.Fatal("a session of another workspace is offered the controls")
	}
	// An administrator's choice for the development tenant is never overwritten
	// by the composition root's default.
	if err := s.service.Rewrite.Registry.Configure(localAgentDemoTenant, false, chatrewrite.DefaultStyles()); err != nil {
		t.Fatal(err)
	}
	if err := ChattoneEnableTenant(s.service.Rewrite.Registry, localAgentDemoTenant); err != nil {
		t.Fatal(err)
	}
	if _, enabled := s.service.Rewrite.Registry.Styles(localAgentDemoTenant); enabled {
		t.Fatal("enabling the development tenant overwrote an administrator's off")
	}
}

// TestTodo_CHATTONE_004_ServedPersistence: what an administrator chooses and
// what a person has used survive a restart, because both live in the chat
// store, and a restarted service reads the setting before it serves anyone.
func TestTodo_CHATTONE_004_ServedPersistence(t *testing.T) {
	s := newChattoneServedStackWith(t, 200, false, 3)
	if err := ChattoneEnableTenant(s.service.Rewrite.Registry, chattoneTenant); err != nil {
		t.Fatal(err)
	}
	house := `{"conversation_id":"` + chattoneRoom + `","enabled":true,"styles":[` +
		`{"id":"professional","label":"Boardroom","instruction":"Write as if for a board paper.","register":"professional"},` +
		`{"id":"concise","label":"Concise","instruction":"Use shorter, direct language, keeping the same content.","register":"concise"}]}`
	if code, out := s.call("admin", http.MethodPost, ChattonePath+"/styles", house); code != 200 {
		t.Fatalf("administrator: %d %s", code, out)
	}
	if code, out := s.call("admin", http.MethodPost, ChattonePath+"/styles", `{"conversation_id":"`+chattoneRoom+`","enabled":true,"styles":[]}`); code != 400 {
		t.Fatalf("an empty registry was saved: %d %s", code, out)
	}
	saved, err := s.store.LoadWritingStyleSetting(context.Background(), chattoneTenant)
	if err != nil || !saved.Found || !saved.Enabled || !strings.Contains(string(saved.Styles), "Write as if for a board paper.") {
		t.Fatalf("setting not persisted: %+v %v", saved, err)
	}

	// A new process: the house styles are there before anyone has asked, and a
	// workspace the administrator turned off stays off.
	again := s.restart()
	if code, reply := again.suggestion("alice"); code != 200 || !reply.Enabled || len(reply.Styles) != 2 || reply.Styles[0].Label != "Boardroom" {
		t.Fatalf("after restart: %d %+v", code, reply)
	}
	if code, reply := again.rewrite("alice", "Please fix the broken deploy pipeline now", "professional"); code != 200 || reply.Draft == "" || !strings.Contains(again.provider.last().System, "Write as if for a board paper.") {
		t.Fatalf("house style lost: %d %s", code, reply.raw)
	}

	off := `{"conversation_id":"` + chattoneRoom + `","enabled":false,"styles":[{"id":"professional","label":"Professional","instruction":"Use neutral, courteous language; remove heat.","register":"professional"}]}`
	if code, out := again.call("admin", http.MethodPost, ChattonePath+"/styles", off); code != 200 {
		t.Fatalf("turn off: %d %s", code, out)
	}
	cold := again.restart()
	// The composition root would enable a development workspace again; a saved
	// "off" must win over that default.
	if err := ChattoneEnableTenant(cold.service.Rewrite.Registry, chattoneTenant); err != nil {
		t.Fatal(err)
	}
	if got := cold.features("alice"); got.WritingStyles {
		t.Fatal("a restart turned a workspace back on")
	}
	before := cold.provider.calls()
	if code, reply := cold.rewrite("alice", chattoneDraftAngry, "professional"); code != 403 || reply.Error != "disabled" || cold.provider.calls() != before {
		t.Fatalf("rewrite after a saved off: %d %s", code, reply.raw)
	}

	// The per-person day: three places are three rewrites (one is already
	// spent on the house style above). The count is
	// durable, so a restarted service does not give the day back.
	on := `{"conversation_id":"` + chattoneRoom + `","enabled":true,"styles":[{"id":"professional","label":"Professional","instruction":"Use neutral, courteous language; remove heat.","register":"professional"}]}`
	if code, out := cold.call("admin", http.MethodPost, ChattonePath+"/styles", on); code != 200 {
		t.Fatalf("turn on: %d %s", code, out)
	}
	for i := 0; i < 2; i++ {
		if code, reply := cold.rewrite("alice", "Please fix the broken deploy pipeline now", "professional"); code != 200 || reply.Draft == "" {
			t.Fatalf("rewrite %d: %d %s", i, code, reply.raw)
		}
	}
	calls := cold.provider.calls()
	if code, reply := cold.rewrite("alice", "Please fix the broken deploy pipeline now", "professional"); code != 429 || reply.Error != "limit" || cold.provider.calls() != calls {
		t.Fatalf("a spent day: %d %s", code, reply.raw)
	}
	restarted := cold.restart()
	if code, reply := restarted.rewrite("alice", "Please fix the broken deploy pipeline now", "professional"); code != 429 || reply.Error != "limit" || restarted.provider.calls() != calls {
		t.Fatalf("a restart gave the day back: %d %s", code, reply.raw)
	}
	// Another person's day is their own.
	if code, reply := restarted.rewrite("admin", "Please fix the broken deploy pipeline now", "professional"); code != 200 || reply.Draft == "" {
		t.Fatalf("a different person was held to alice's day: %d %s", code, reply.raw)
	}
	if lines, places := restarted.usageLines(); lines != 8 || places != 4 {
		t.Fatalf("usage: %d lines %d places (alice three rewrites, admin one)", lines, places)
	}
}
