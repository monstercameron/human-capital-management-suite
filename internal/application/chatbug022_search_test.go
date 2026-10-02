package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

type chatbug022Port struct {
	err error
}

func (p chatbug022Port) Search(context.Context, chatsearch.Request) (chatsearch.Response, error) {
	return chatsearch.Response{}, p.err
}

// CHATBUG-022: searching Chat answered 503 whenever any one source failed or
// was slow, and listed every kind nobody had registered as "unavailable". Here
// the real message sources run on PostgreSQL, a second source is down and a
// third never answers: the request still answers 200 with the messages, within
// the source deadline, and the body says nothing about causes.
func TestTodo_CHATBUG_022(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv("HCMNEXT_AGENT_DEBUG_CAUSES", "1")

	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	now := time.Now().UTC()
	service := chat.NewService(store, func() time.Time { return now })
	service.SetAuthority(servedPersonaChatAuthority{})
	alice := chat.Principal{TenantID: "tenant", SubjectID: "alice"}
	if _, err := service.CreateConversation(ctx, chat.CreateConversationRequest{Principal: alice, TenantID: "tenant", ConversationID: "general", Kind: chat.PublicChannel, Name: "General"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SendPost(ctx, chat.SendPostRequest{Principal: alice, TenantID: "tenant", ConversationID: "general", Body: "The holiday schedule is posted", IdempotencyKey: "holiday"}); err != nil {
		t.Fatal(err)
	}
	registry, err := NewChatSearchRendering(raw, service, func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	registry.SourceTimeout = 400 * time.Millisecond
	ok := func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }
	down := chatsearch.SourceFuncs{SearchRows: func(context.Context, chatsearch.Request) ([]chatsearch.Row, error) {
		return nil, errors.New("saved store: connection refused to db.internal:5432")
	}, OpenRow: ok}
	hung := chatsearch.SourceFuncs{SearchRows: func(c context.Context, _ chatsearch.Request) ([]chatsearch.Row, error) {
		<-c.Done()
		return nil, c.Err()
	}, OpenRow: ok}
	if err := registry.RegisterSource(chatsearch.Saved, down); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterSource(chatsearch.Reminder, hung); err != nil {
		t.Fatal(err)
	}
	h := chatsearchTestHTTP(t, registry, chatsearch.NewRecent())

	started := time.Now()
	w := chatsearchHTTPCall(h, http.MethodPost, ChatSearchPath, `{"Query":"holiday","DisplayQuery":"holiday","Mode":"keyword","Limit":20}`, "fixture")
	elapsed := time.Since(started)
	if w.Code != http.StatusOK {
		t.Fatalf("one source down must not fail the search: %d %s", w.Code, w.Body)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("a hung source delayed the answer by %s", elapsed)
	}
	var response chatsearch.Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range response.Groups {
		for _, row := range group.Rows {
			found = found || row.Kind == chatsearch.Message && strings.Contains(row.Text, "holiday schedule")
		}
	}
	if !found {
		t.Fatalf("the message from the working source is missing: %s", w.Body)
	}
	// The people source of this fixture has no reference reader and answers
	// "unavailable" too: three registered sources are named, and none of the
	// fifteen kinds that have no source at all.
	named := map[chatsearch.Kind]bool{}
	for _, kind := range response.Unavailable {
		named[kind] = true
	}
	if len(named) != 3 || !named[chatsearch.Saved] || !named[chatsearch.Reminder] || !named[chatsearch.Person] {
		t.Fatalf("only the registered sources that failed are named: %v %s", response.Unavailable, logged.String())
	}
	if strings.Contains(w.Body.String(), "connection refused") || strings.Contains(w.Body.String(), "Failures") {
		t.Fatalf("a cause reached the response body: %s", w.Body)
	}
	if !strings.Contains(logged.String(), "hcmnext.chat_search_source_unavailable") || !strings.Contains(logged.String(), "connection refused") {
		t.Fatalf("the failing source's cause was not logged under the opt-in: %s", logged.String())
	}

	// The semantic path has no model files here: it answers from keywords.
	w = chatsearchHTTPCall(h, http.MethodPost, ChatSearchPath, `{"Query":"holiday","Mode":"meaning","Limit":20}`, "fixture")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"Mode":"keyword"`) {
		t.Fatalf("meaning mode did not degrade silently: %d %s", w.Code, w.Body)
	}
}

// The 503 path names its cause in the log for a developer cell and never in
// the body; without the opt-in the log stays quiet.
func TestTodo_CHATBUG_022_Cause(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cause := errors.New("search store exploded: secret-host:5432")
	h := chatsearchTestHTTP(t, chatbug022Port{err: cause}, chatsearch.NewRecent())
	t.Setenv("HCMNEXT_AGENT_DEBUG_CAUSES", "")
	w := chatsearchHTTPCall(h, http.MethodPost, ChatSearchPath, `{"Query":"holiday"}`, "fixture")
	if w.Code != http.StatusServiceUnavailable || logged.Len() != 0 {
		t.Fatalf("without the opt-in: %d, logged %q", w.Code, logged.String())
	}
	t.Setenv("HCMNEXT_AGENT_DEBUG_CAUSES", "1")
	w = chatsearchHTTPCall(h, http.MethodPost, ChatSearchPath, `{"Query":"holiday"}`, "fixture")
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secret-host") {
		t.Fatalf("503 body %d %s", w.Code, w.Body)
	}
	if !strings.Contains(logged.String(), "hcmnext.chat_search_unavailable") || !strings.Contains(logged.String(), "secret-host:5432") {
		t.Fatalf("503 cause not logged under the opt-in: %q", logged.String())
	}
}

// The ordinary message source must keep answering for what the reader may
// read: a member of the private channel finds the colleague's message, a
// non-member does not, and a row whose rendering fails is withheld on its own
// instead of taking the whole message source (and every other result) down.
func TestTodo_CHATBUG_022_Messages(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	now := time.Now().UTC()
	service := chat.NewService(store, func() time.Time { return now })
	service.SetAuthority(servedPersonaChatAuthority{})
	danny := chat.Principal{TenantID: "tenant", SubjectID: "danny"}
	if _, err := service.CreateConversation(ctx, chat.CreateConversationRequest{Principal: danny, TenantID: "tenant", ConversationID: "incident-review", Kind: chat.PrivateChannel, Name: "incident-review"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMembership(ctx, chat.AddMembershipRequest{Principal: danny, Membership: chat.Membership{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "incident-review", SubjectID: "alice", HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	for key, body := range map[string]string{"ok": "Quick reminder that the survey closes tonight.", "bad": "The survey rendering FAILS for this one"} {
		if _, err := service.SendPost(ctx, chat.SendPostRequest{Principal: danny, TenantID: "tenant", ConversationID: "incident-review", Body: body, IdempotencyKey: key}); err != nil {
			t.Fatal(err)
		}
	}
	allow := func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }
	render := func(_ context.Context, _ chatsearch.Actor, row chatsearch.Row) (string, error) {
		if strings.Contains(row.Text, "FAILS") {
			return "", errors.New("rendering unavailable for this message")
		}
		return row.Text, nil
	}
	registry, err := NewChatSearchRendering(raw, service, allow, render)
	if err != nil {
		t.Fatal(err)
	}
	h := chatsearchTestHTTP(t, registry, chatsearch.NewRecent())
	w := chatsearchHTTPCall(h, http.MethodPost, ChatSearchPath, `{"Query":"survey","DisplayQuery":"survey","Mode":"keyword","Limit":20}`, "fixture")
	if w.Code != http.StatusOK {
		t.Fatalf("member search answered %d %s", w.Code, w.Body)
	}
	var response chatsearch.Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	texts := []string{}
	for _, group := range response.Groups {
		for _, row := range group.Rows {
			if row.Kind == chatsearch.Message {
				texts = append(texts, row.Text)
			}
		}
	}
	if len(texts) != 1 || texts[0] != "Quick reminder that the survey closes tonight." {
		t.Fatalf("a member of the private channel must find the colleague's message and only that: %v in %s", texts, w.Body)
	}
	for _, kind := range response.Unavailable {
		if kind == chatsearch.Message {
			t.Fatalf("the message source reported itself unavailable: %v", response.Unavailable)
		}
	}
	outsider := chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "eve"}
	got, err := registry.Search(ctx, chatsearch.Request{Actor: outsider, Query: "survey", Filters: chatsearch.Filters{Kind: chatsearch.Message}, At: time.Now().UTC()})
	if err != nil || len(got.Groups) != 0 {
		t.Fatalf("a non-member must find nothing: %+v %v", got, err)
	}
}

// CHATBUG-022 (speed): a keyword search over what one person may read must not
// cost a database or policy call per post. Twenty thousand posts across five
// rooms, the reader a member of four: the message source answers well inside
// 300 ms and returns exactly the readable matches, none from the fifth room.
func TestTodo_CHATBUG_022_Performance(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	now := time.Now().UTC()
	service := chat.NewService(store, func() time.Time { return now })
	service.SetAuthority(servedPersonaChatAuthority{})
	danny := chat.Principal{TenantID: "tenant", SubjectID: "danny"}
	for i := 0; i < 5; i++ {
		room := "room-" + string(rune('0'+i))
		if _, err := service.CreateConversation(ctx, chat.CreateConversationRequest{Principal: danny, TenantID: "tenant", ConversationID: room, Kind: chat.PrivateChannel, Name: room}); err != nil {
			t.Fatal(err)
		}
		if i < 4 {
			if _, err := service.AddMembership(ctx, chat.AddMembershipRequest{Principal: danny, Membership: chat.Membership{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: room, SubjectID: "alice", HistoryVisibility: chat.FullHistory}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	const posts = 20000
	if err := raw.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,created_at,updated_at)
 SELECT 'perf-'||g,'tenant','room-'||(g%5),'danny','tenant',(g/5)+1000,
  'routine update number '||g||CASE WHEN g%997=0 THEN ' - the survey closes tonight' ELSE '' END,
  now()-(g||' seconds')::interval, now() FROM generate_series(1,$1::int) g`, posts)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := 0
	for g := 1; g <= posts; g++ {
		if g%997 == 0 && g%5 != 4 {
			want++
		}
	}
	allow := func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }
	registry, err := NewChatSearchRendering(raw, service, allow, nil)
	if err != nil {
		t.Fatal(err)
	}
	registry.SourceTimeout = time.Minute // the 300 ms bound below is the assertion, not the deadline
	alice := chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "alice"}
	request := chatsearch.Request{Actor: alice, Query: "survey", Filters: chatsearch.Filters{Kind: chatsearch.Message}, At: time.Now().UTC(), Limit: 50}
	if _, err := registry.Search(ctx, request); err != nil { // warm the plan and the connection
		t.Fatal(err)
	}
	calls := 0
	counting := func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { calls++; return true, nil }
	phase := time.Now()
	direct, err := raw.SearchChatContent(ctx, request, chatsearch.Message, counting, "")
	t.Logf("store search alone: %d rows, %d authority calls, %s", len(direct), calls, time.Since(phase))
	recheck := chatsearch.Request{Actor: alice, At: time.Now().UTC(), Limit: len(direct), OpenIDs: []string{}, OpenMessageIDs: []string{}}
	for _, row := range direct {
		recheck.OpenIDs = append(recheck.OpenIDs, row.ID)
		recheck.OpenMessageIDs = append(recheck.OpenMessageIDs, row.Target.MessageID)
	}
	calls, phase = 0, time.Now()
	again, err := raw.SearchChatContent(ctx, recheck, chatsearch.Message, counting, "")
	t.Logf("recheck alone: %d rows, %d authority calls, %s", len(again), calls, time.Since(phase))
	// Best of five: the shared test database is also used by other work, and a
	// busy moment is not the search's cost.
	var got chatsearch.Response
	elapsed := time.Hour
	for i := 0; i < 5; i++ {
		started := time.Now()
		got, err = registry.Search(ctx, request)
		took := time.Since(started)
		t.Logf("run %d: %s", i+1, took)
		if err != nil || len(got.Unavailable) != 0 {
			t.Fatalf("message source failed: %v %v", err, got.Unavailable)
		}
		if took < elapsed {
			elapsed = took
		}
	}
	n := 0
	for _, group := range got.Groups {
		for _, row := range group.Rows {
			if strings.HasPrefix(row.Target.ConversationID, "room-4") {
				t.Fatalf("a row from a room the reader is not in: %+v", row)
			}
			n++
		}
	}
	t.Logf("message source: %d rows from %d posts in %s", n, posts, elapsed)
	if n != want {
		t.Fatalf("readable matches = %d, want %d", n, want)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("message source took %s, want under 300ms", elapsed)
	}
}
