package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// chatlangFacts makes one person a workspace administrator and everyone else an
// employee, so administration can be refused to the others.
type chatlangFacts struct{ admin string }

func (f chatlangFacts) ResolveChatFacts(_ context.Context, tenant, subject string, _ time.Time) (chatpolicy.Principal, error) {
	role := "employee"
	if subject == f.admin {
		role = "hcm_admin"
	}
	return chatpolicy.Principal{ID: subject, Tenant: tenant, Active: true, Roles: []string{role}, AuthorityRevision: 1}, nil
}

// chatlangRig is a really composed Chat over the shared test PostgreSQL with
// the deterministic engine bound. No paid model is reachable from it.
type chatlangRig struct {
	t          *testing.T
	chat       composedChat
	db         *sql.DB
	engine     *chatlang.FixtureEngine
	runtime    *ChatlangRuntime
	governance *ChatlangGovernance
	room       string
}

func newChatlangRig(t *testing.T) *chatlangRig {
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
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "chatlang-003-integration-key"}, time.Now, chatlangFacts{admin: "alice"}, core, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(deployment.close)
	rig := &chatlangRig{t: t, chat: deployment, db: chatDB.SQL, engine: &chatlang.FixtureEngine{}, governance: deployment.renderings.Languages}
	rig.governance.BindEngine(ChatlangEngineInfo{Name: "fixture", Ready: true, External: true})
	rig.runtime, err = NewChatlangRuntime(deployment.store, rig.governance, rig.engine, ChatlangFilterScreen{Filters: deployment.filters}, time.Now, []string{"host"})
	if err != nil {
		t.Fatal(err)
	}
	room, err := deployment.service.CreateConversation(rig.as("alice"), chatcore.CreateConversationRequest{Principal: rig.person("alice"), TenantID: "host", Kind: chatcore.PublicChannel, Name: "ops", IdempotencyKey: "chatlang-room"})
	if err != nil {
		t.Fatal(err)
	}
	rig.room = room.ID
	for _, member := range []string{"bruno", "carla", "dora"} {
		if _, err := deployment.service.AddMembership(rig.as("alice"), chatcore.AddMembershipRequest{Principal: rig.person("alice"), Membership: chatcore.Membership{ConversationID: room.ID, TenantID: "host", HomeTenantID: "host", SubjectID: member, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory}}); err != nil {
			t.Fatal(err)
		}
	}
	return rig
}

func (r *chatlangRig) person(subject string) chatcore.Principal {
	return chatcore.Principal{TenantID: "host", SubjectID: subject}
}
func (r *chatlangRig) as(subject string) context.Context {
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-" + subject, IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "verified-" + subject})
	if err != nil {
		r.t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}
func (r *chatlangRig) scope(subject string) chatstore.RenderingScope {
	return chatstore.RenderingScope{Principal: r.person(subject), Tenant: "host", Conversation: r.room}
}

// reads makes a person read German (or any language) with translation on.
func (r *chatlangRig) reads(subject, language string) {
	r.t.Helper()
	personal := chatstore.RenderingScope{Principal: r.person(subject), Tenant: "host"}
	pref := chatrender.DefaultPreference(language)
	pref.Translate = true
	if err := r.chat.renderings.PutLanguageSettings(r.as(subject), personal, pref); err != nil {
		r.t.Fatal(err)
	}
}

func (r *chatlangRig) enable(w chatlang.Workspace) {
	r.t.Helper()
	w.Enabled, w.ExternalAllowed = true, true
	if _, err := r.governance.PutWorkspace(r.as("alice"), w); err != nil {
		r.t.Fatal(err)
	}
}

func (r *chatlangRig) send(key, body string) chatcore.Post {
	r.t.Helper()
	post, err := r.chat.service.SendPost(r.as("alice"), chatcore.SendPostRequest{Principal: r.person("alice"), TenantID: "host", ConversationID: r.room, Body: body, IdempotencyKey: key})
	if err != nil {
		r.t.Fatal(err)
	}
	return post
}

func (r *chatlangRig) read(subject string, post chatcore.Post) (chatrender.Rendering, chatrender.Mark) {
	r.t.Helper()
	rendering, mark, err := r.chat.renderings.ReadRenderingSelection(r.as(subject), r.scope(subject), post.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	return rendering, mark
}

func (r *chatlangRig) drain() int {
	r.t.Helper()
	n, err := r.runtime.Drain(context.Background(), "host")
	if err != nil {
		r.t.Fatal(err)
	}
	return n
}

func (r *chatlangRig) count(query string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.db.QueryRow(query, args...).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

func (r *chatlangRig) jobs(post chatcore.Post, language string) int {
	return r.count(`SELECT count(*) FROM chatrender_job WHERE post_id=$1 AND language=$2`, post.ID, language)
}
func (r *chatlangRig) jobState(post chatcore.Post, language string) string {
	var state string
	if err := r.db.QueryRow(`SELECT state FROM chatrender_job WHERE post_id=$1 AND language=$2 ORDER BY revision DESC LIMIT 1`, post.ID, language).Scan(&state); err != nil {
		r.t.Fatal(err)
	}
	return state
}

const englishSentence = "Please review the quarterly report with the team today."

func TestTodo_CHATLANG_003_Integration(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	rig.reads("dora", "de")

	post := rig.send("one", englishSentence)
	// A message in English with two German readers is requested at once, as one
	// job for German, not two.
	if rig.jobs(post, "de") != 1 || rig.jobs(post, "en") != 0 {
		t.Fatalf("expected one eager job for de, got de=%d", rig.jobs(post, "de"))
	}
	if rig.drain() != 1 {
		t.Fatal("expected one job to run")
	}
	calls := rig.engine.Calls()
	if len(calls) != 1 || calls[0].Source != "en" || calls[0].Target != "de" {
		t.Fatalf("engine calls %+v", calls)
	}
	bruno, mark := rig.read("bruno", post)
	if mark.State != "ready" || bruno.Text != "[de] "+englishSentence || bruno.Language != "de" || bruno.SourceLanguage != "en" {
		t.Fatalf("the German reader's projection: %+v %+v", bruno, mark)
	}
	if bruno.Producer.InstructionDigest != chatlang.InstructionDigest() || bruno.Producer.GlossaryVersion != "glossary-v0" || bruno.Producer.Provider != "fixture" || !strings.HasPrefix(bruno.CostReference, "chatlang:host:"+post.ID) {
		t.Fatalf("the rendering must say what produced it: %+v", bruno.Producer)
	}
	dora, mark := rig.read("dora", post)
	if mark.State != "ready" || dora.Text != bruno.Text || len(rig.engine.Calls()) != 1 {
		t.Fatalf("the second German reader shares the first translation: %+v %+v calls=%d", dora, mark, len(rig.engine.Calls()))
	}
	carla, mark := rig.read("carla", post)
	if mark.State != "original" || carla.Text != englishSentence || len(rig.engine.Calls()) != 1 || rig.jobs(post, "en") != 0 {
		t.Fatalf("a reader of the source language reads the original and costs nothing: %+v %+v", carla, mark)
	}
	// The original is the record: it is exactly what was written.
	stored, err := rig.chat.service.ListPosts(rig.as("alice"), chatcore.ListPostsRequest{Principal: rig.person("alice"), TenantID: "host", ConversationID: rig.room, Page: chatcore.Page{PageSize: 20}})
	if err != nil || len(stored.Posts) != 1 || stored.Posts[0].Body != englishSentence {
		t.Fatalf("the original changed: %+v %v", stored.Posts, err)
	}
	if rig.count(`SELECT count(*) FROM chat_post_revision WHERE post_id=$1 AND body=$2`, post.ID, englishSentence) != 1 || rig.count(`SELECT count(*) FROM chatlang_usage WHERE post_id=$1 AND outcome='translated'`, post.ID) != 1 {
		t.Fatal("the usage line or the record is wrong")
	}
	spent, limit, err := rig.governance.Budget(context.Background(), "host")
	if err != nil || spent != 0 || limit != chatlang.DefaultMonthlyBudgetMicros {
		t.Fatalf("budget %d %d %v", spent, limit, err)
	}
	// The reader projection through the list endpoint carries the translation too.
	listed, err := rig.chat.renderings.ListRenderings(rig.as("bruno"), rig.scope("bruno"), []string{post.ID})
	if err != nil || len(listed) != 1 || listed[0].Language != "de" {
		t.Fatalf("list %+v %v", listed, err)
	}

	// An edit is a new revision with fresh renderings; the old one is not reused.
	edited, err := rig.chat.service.EditPost(rig.as("alice"), chatcore.EditPostRequest{Principal: rig.person("alice"), TenantID: "host", ConversationID: rig.room, PostID: post.ID, Body: "Please ship the final report with the team tomorrow.", ExpectedRevision: post.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Revision != post.Revision+1 || rig.count(`SELECT count(*) FROM chatrender_job WHERE post_id=$1 AND revision=$2 AND language='de'`, post.ID, edited.Revision) != 1 {
		t.Fatalf("the edit did not request a fresh translation: %+v", edited)
	}
	rig.drain()
	bruno, mark = rig.read("bruno", edited)
	if mark.State != "ready" || bruno.Revision != edited.Revision || bruno.Text != "[de] Please ship the final report with the team tomorrow." || len(rig.engine.Calls()) != 2 {
		t.Fatalf("after the edit: %+v %+v calls=%d", bruno, mark, len(rig.engine.Calls()))
	}
}

func TestTodo_CHATLANG_003_Context(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	rig.send("a", "Are the quarterly numbers ready for the team today?")
	rig.drain()
	second := rig.send("b", "Yes, they are ready and the team has them.")
	rig.drain()
	calls := rig.engine.Calls()
	if len(calls) != 2 || len(calls[1].Context) != 1 || !strings.Contains(calls[1].Context[0], "quarterly numbers") {
		t.Fatalf("a short reply is translated with the earlier message as context: %+v", calls)
	}
	bruno, _ := rig.read("bruno", second)
	if strings.Contains(bruno.Text, "quarterly") {
		t.Fatalf("context leaked into the output: %q", bruno.Text)
	}
}

func TestTodo_CHATLANG_003_Glossary(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	for _, term := range []chatlang.Term{{Source: "Acme Cloud"}, {Source: "time off", Language: "de", Target: "Urlaub"}} {
		if _, err := rig.governance.AddTerm(rig.as("alice"), term); err != nil {
			t.Fatal(err)
		}
	}
	post := rig.send("g", "Please book time off in Acme Cloud with @carla before 2026-10-01 at 14:30.")
	rig.drain()
	text := rig.engine.Calls()[0].Text
	for _, hidden := range []string{"Acme Cloud", "time off", "@carla", "2026-10-01", "14:30"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("%q was sent to the engine in %q", hidden, text)
		}
	}
	got, mark := rig.read("bruno", post)
	if mark.State != "ready" || got.Text != "[de] Please book Urlaub in Acme Cloud with @carla before 2026-10-01 at 14:30." || got.Producer.GlossaryVersion != "glossary-v2" {
		t.Fatalf("the glossary and placeholders survive translation: %+v %+v", got, mark)
	}
}

func TestTodo_CHATLANG_003_PlaceholderFailure(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	// An engine that loses the mention: both attempts are discarded.
	rig.engine.Mutate = func(_ chatlang.Request, out string) string {
		return strings.Join(strings.Fields(strings.NewReplacer("⟦", " ", "⟧", " ").Replace(out)), " ")
	}
	post := rig.send("p", "Please ask @carla to review the report today.")
	rig.drain()
	if len(rig.engine.Calls()) != chatlangAttempts || rig.jobState(post, "de") != "failed" {
		t.Fatalf("calls=%d state=%s", len(rig.engine.Calls()), rig.jobState(post, "de"))
	}
	got, mark := rig.read("bruno", post)
	if mark.State != "fallback" || got.Text != "Please ask @carla to review the report today." {
		t.Fatalf("a rendering that fails the placeholder check is discarded and the original shown: %+v %+v", got, mark)
	}
	if rig.count(`SELECT count(*) FROM chatrender_rendering WHERE post_id=$1`, post.ID) != 0 || rig.count(`SELECT count(*) FROM chatlang_usage WHERE post_id=$1 AND outcome='discarded'`, post.ID) != 2 {
		t.Fatal("a failed rendering must not be stored and each paid attempt must have a line")
	}
	// Reading again does not ask the engine again.
	rig.read("bruno", post)
	if rig.drain() != 0 || len(rig.engine.Calls()) != chatlangAttempts {
		t.Fatal("a discarded translation was retried")
	}
}

func TestTodo_CHATLANG_003_Fault(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	rig.engine.Fail = chatlang.ErrUnavailable
	post := rig.send("f", englishSentence)
	if err := rig.runtime.Worker.RunOne(context.Background(), "host", time.Minute); !errors.Is(err, chatlang.ErrUnavailable) {
		t.Fatalf("an engine timeout fails the job: %v", err)
	}
	if rig.jobState(post, "de") != "queued" || rig.count(`SELECT count(*) FROM chatlang_usage WHERE post_id=$1 AND outcome='failed'`, post.ID) != 1 {
		t.Fatalf("a transient failure is requeued and counted: %s", rig.jobState(post, "de"))
	}
	got, mark := rig.read("bruno", post)
	if mark.State == "ready" || (mark.State == "fallback" && got.Text != englishSentence) {
		t.Fatalf("while it is down the reader keeps the original: %+v %+v", got, mark)
	}
	rig.engine.Fail = nil
	rig.drain()
	got, mark = rig.read("bruno", post)
	if mark.State != "ready" || got.Text != "[de] "+englishSentence {
		t.Fatalf("after the engine recovers: %+v %+v", got, mark)
	}
}

func TestTodo_CHATLANG_003_Budget(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{BudgetMicros: 100})
	rig.reads("bruno", "de")
	rig.engine.Cost = 100
	first := rig.send("b1", englishSentence)
	rig.drain()
	if got, mark := rig.read("bruno", first); mark.State != "ready" || got.Text == "" {
		t.Fatalf("within budget: %+v %+v", got, mark)
	}
	spent, limit, _ := rig.governance.Budget(context.Background(), "host")
	if spent != 100 || limit != 100 {
		t.Fatalf("spent %d of %d", spent, limit)
	}
	// At the limit new messages are not requested and a lazy read ends quietly.
	second := rig.send("b2", "Please send the report to the team before the meeting.")
	if rig.jobs(second, "de") != 0 {
		t.Fatal("a job was requested after the limit")
	}
	rig.read("bruno", second)
	rig.drain()
	got, mark := rig.read("bruno", second)
	if mark.State != "fallback" || got.Text != "Please send the report to the team before the meeting." || len(rig.engine.Calls()) != 1 {
		t.Fatalf("at the limit the original is shown and the engine is not called: %+v %+v calls=%d", got, mark, len(rig.engine.Calls()))
	}
	// Raising the limit lets translation continue for new messages.
	rig.enable(chatlang.Workspace{BudgetMicros: 1000})
	third := rig.send("b3", "Please confirm the schedule with the team before tomorrow.")
	rig.drain()
	if got, mark := rig.read("bruno", third); mark.State != "ready" || got.Text == "" {
		t.Fatalf("after raising the limit: %+v %+v", got, mark)
	}
}

func TestTodo_CHATLANG_003_ChannelOffAndBarred(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	// Translation off for the channel: nothing is requested and the reader sees
	// the original as the original, not as a failed translation.
	if err := rig.governance.PutChannel(rig.as("alice"), rig.room, chatlang.Channel{Translation: chatlang.Off}); err != nil {
		t.Fatal(err)
	}
	post := rig.send("off", englishSentence)
	got, mark := rig.read("bruno", post)
	if rig.jobs(post, "de") != 0 || mark.State != "original" || got.Text != englishSentence || len(rig.engine.Calls()) != 0 {
		t.Fatalf("channel off: jobs=%d %+v %+v", rig.jobs(post, "de"), got, mark)
	}
	// A channel that must not use an external engine: with only an external
	// engine there is no translation, and nothing leaves the deployment.
	if err := rig.governance.PutChannel(rig.as("alice"), rig.room, chatlang.Channel{Translation: chatlang.On, External: chatlang.ExternalBarred}); err != nil {
		t.Fatal(err)
	}
	barred := rig.send("barred", "Please review the other report with the team today.")
	got, mark = rig.read("bruno", barred)
	if rig.jobs(barred, "de") != 0 || mark.State != "original" || got.Text != "Please review the other report with the team today." || len(rig.engine.Calls()) != 0 {
		t.Fatalf("channel barred: jobs=%d %+v %+v", rig.jobs(barred, "de"), got, mark)
	}
	// A job that was requested before the channel was barred is stopped by the
	// worker's own recheck, before any engine call.
	if err := rig.governance.PutChannel(rig.as("alice"), rig.room, chatlang.Channel{Translation: chatlang.On}); err != nil {
		t.Fatal(err)
	}
	queued := rig.send("queued", "Please read the third report with the team today.")
	if rig.jobs(queued, "de") != 1 {
		t.Fatal("expected a queued job")
	}
	if err := rig.governance.PutChannel(rig.as("alice"), rig.room, chatlang.Channel{Translation: chatlang.On, External: chatlang.ExternalBarred}); err != nil {
		t.Fatal(err)
	}
	rig.drain()
	if len(rig.engine.Calls()) != 0 || rig.jobState(queued, "de") != "failed" {
		t.Fatalf("a barred channel's queued job reached the engine: calls=%d state=%s", len(rig.engine.Calls()), rig.jobState(queued, "de"))
	}
	// The workspace switch is a ceiling.
	if _, err := rig.governance.PutWorkspace(rig.as("alice"), chatlang.Workspace{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if rig.governance.Allows(context.Background(), "host", rig.room) {
		t.Fatal("translation offered with the workspace off")
	}
}

func TestTodo_CHATLANG_003_Languages(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{Languages: []string{"fr"}})
	rig.reads("bruno", "de")
	post := rig.send("lang", englishSentence)
	got, mark := rig.read("bruno", post)
	rig.drain()
	got2, mark2 := rig.read("bruno", post)
	if rig.jobState(post, "de") != "failed" || len(rig.engine.Calls()) != 0 || got2.Text != englishSentence || mark2.State != "fallback" {
		t.Fatalf("a language that is not offered is not translated: %+v %+v / %+v %+v", got, mark, got2, mark2)
	}
}

func TestTodo_CHATLANG_003_Security(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	// Text the workspace filters mask is never sent to an engine.
	rig.chatFilter("secret")
	post := rig.send("s", "Please keep the secret report away from the team today.")
	rig.drain()
	if len(rig.engine.Calls()) != 0 {
		t.Fatalf("masked text reached the engine: %+v", rig.engine.Calls())
	}
	if got, _ := rig.read("bruno", post); strings.Contains(got.Text, "secret") {
		t.Fatalf("masked text shown to a reader: %q", got.Text)
	}
	// Someone who is not a member of the channel can neither read nor request.
	outsider := chatstore.RenderingScope{Principal: rig.person("mallory"), Tenant: "host", Conversation: rig.room}
	if _, _, err := rig.chat.renderings.ReadRenderingSelection(rig.as("mallory"), outsider, post.ID); err == nil {
		t.Fatal("an outsider read a rendering")
	}
}

// chatFilter turns on a workspace filter that masks one word for readers.
func (r *chatlangRig) chatFilter(word string) {
	r.t.Helper()
	actor := chatfilter.Actor{Tenant: "host", Subject: "alice"}
	definition := chatfilter.Definition{ID: "mask-" + word, Name: "Restricted word", Version: "1.0.0", Kind: "words", Match: []string{word}, Action: "mask"}
	if err := r.chat.filters.CreateVersion(r.as("alice"), actor, definition); err != nil {
		r.t.Fatal(err)
	}
	if err := r.chat.filters.Enable(r.as("alice"), actor, chatfilter.Enablement{RuleID: definition.ID, Enabled: true}, false); err != nil {
		r.t.Fatal(err)
	}
}

// TestTodo_CHATLANG_003_ReaderEndpoint reads the German rendering the way the
// page does, through the served reader endpoint, as the German reader, and as
// an English reader and an outsider who must not get it.
func TestTodo_CHATLANG_003_ReaderEndpoint(t *testing.T) {
	rig := newChatlangRig(t)
	rig.enable(chatlang.Workspace{})
	rig.reads("bruno", "de")
	post := rig.send("endpoint", englishSentence)
	rig.drain()
	read := func(subject string) (int, map[string]struct {
		Rendering chatrender.Rendering
		Mark      chatrender.Mark
	}) {
		admission, bearer := integrate1Admission(t, "host", subject, time.Now)
		handler := (&agentServedAssembly{Renderings: rig.chat.renderings}).Overlay(http.NotFoundHandler(), admission)
		request := httptest.NewRequest(http.MethodGet, ChatRenderingPath+"/reader?conversation="+rig.room+"&message="+post.ID, nil)
		request.Header.Set("Authorization", bearer)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var out map[string]struct {
			Rendering chatrender.Rendering
			Mark      chatrender.Mark
		}
		if response.Code == http.StatusOK {
			if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
				t.Fatalf("%v: %s", err, response.Body.String())
			}
		}
		return response.Code, out
	}
	code, out := read("bruno")
	if got := out[post.ID]; code != 200 || got.Mark.State != "ready" || got.Rendering.Text != "[de] "+englishSentence || got.Rendering.Language != "de" || got.Mark.SourceLanguage != "en" || !got.Mark.CanShowOriginal {
		t.Fatalf("German reader: %d %+v", code, out)
	}
	if code, out = read("carla"); code != 200 || out[post.ID].Mark.State != "original" || out[post.ID].Rendering.Text != englishSentence {
		t.Fatalf("English reader: %d %+v", code, out)
	}
	if code, _ = read("mallory"); code == http.StatusOK {
		t.Fatal("an outsider read a message of a channel they are not in")
	}
}
