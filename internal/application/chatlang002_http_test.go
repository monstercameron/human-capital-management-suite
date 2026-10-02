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

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

func chatlangRequest(t *testing.T, subject, method, path, body, locale string) *http.Request {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "lang-" + subject, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-digest"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(trust.WithPrincipal(context.Background(), p))
	request.Header.Set("Content-Type", "application/json")
	if locale != "" {
		request.Header.Set("Accept-Language", locale)
	}
	return request
}

func chatlangServe(t *testing.T, handler ChatRenderingHandler, subject, method, path, body, locale string, into any) int {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, chatlangRequest(t, subject, method, ChatRenderingPath+"/"+path, body, locale))
	if into != nil && w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatalf("%s: %v: %s", path, err, w.Body.String())
		}
	}
	return w.Code
}

// TestTodo_CHATLANG_002_Integration drives the reading settings, the audience
// and the reader's selection through the HTTP handler on the shared test
// PostgreSQL, with the deterministic fixture producer standing in for an
// engine, and renders what the server answered.
func TestTodo_CHATLANG_002_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	store, err := chatstore.New(ctx, chatstore.Config{DSN: u.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	adapter := &chatstore.RenderingAdapter{Adapter: chatstore.NewAdapter(store)}
	c := chat.Conversation{ID: "room", TenantID: "tenant-a", Kind: chat.PrivateChannel, OwnerID: "alice", Revision: 1}
	members := []chat.Membership{
		{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
		{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: c.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory},
	}
	if _, err = adapter.CreateConversation(ctx, c, members, ""); err != nil {
		t.Fatal(err)
	}
	alice := chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}
	post, err := adapter.SendPost(ctx, chat.SendPostRequest{Principal: alice, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "en"}, chat.Post{ID: "post", TenantID: c.TenantID, ConversationID: c.ID, AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "Please read this message with your team today.", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	// The store assigns the message id; every request below names the stored one.
	if post.ID == "" {
		t.Fatal("the stored message has no id")
	}
	message := "&message=" + url.QueryEscape(post.ID)
	correction := func(language string) string {
		body, err := json.Marshal(map[string]any{"message": post.ID, "revision": post.Revision, "language": language})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	original := chatrender.Rendering{Tenant: c.TenantID, Message: post.ID, Revision: post.Revision, Tone: chatrender.AsWritten, Language: "en", SourceLanguage: "en", Text: post.Body}
	governance := &ChatlangGovernance{Store: store}
	governance.BindEngine(ChatlangEngineInfo{Name: "fixture", Ready: true})
	surface := &ChatRenderingSurface{Store: store, Languages: governance,
		Policy: chatrenderPolicyFixture{policy: chatrender.Policy{Original: original, AllowOriginal: true, AllowedKinds: []chatrender.Kind{chatrender.Translate}}}}
	handler := ChatRenderingHandler{Port: surface}
	room := "?conversation=room"

	// Settings: a person who has not chosen reads in the interface language they
	// opened Chat in, with translation on; what they save is what they get back.
	var bob chatrender.Preference
	if code := chatlangServe(t, handler, "bob", "GET", "settings", "", "de-DE", &bob); code != 200 || bob.ReadingLanguage != "de" || !bob.Translate {
		t.Fatalf("default settings: %d %+v", code, bob)
	}
	// The page asks the reader endpoint with no language header: the default is
	// the language remembered from the settings read.
	if code := chatlangServe(t, handler, "bob", "GET", "settings", "", "", &bob); code != 200 || bob.ReadingLanguage != "de" {
		t.Fatalf("remembered language: %d %+v", code, bob)
	}
	saved := `{"settings":{"tone":"as-written","reading_language":"de","translate":true,"further_languages":["fr"],"source_overrides":{"es":false}}}`
	if code := chatlangServe(t, handler, "bob", "PUT", "settings", saved, "", nil); code != 200 {
		t.Fatalf("saving settings: %d", code)
	}
	var again chatrender.Preference
	if code := chatlangServe(t, handler, "bob", "GET", "settings", "", "en-US", &again); code != 200 || again.ReadingLanguage != "de" || !again.Translate || len(again.FurtherLanguages) != 1 || again.FurtherLanguages[0] != "fr" || again.SourceOverrides["es"] {
		t.Fatalf("settings did not round trip: %d %+v", code, again)
	}
	// A conversation of their own choosing overrides, and only there.
	override := `{"settings":{"tone":"as-written","reading_language":"ar","translate":false}}`
	if code := chatlangServe(t, handler, "bob", "PUT", "settings"+room, override, "", nil); code != 200 {
		t.Fatalf("saving a conversation's settings: %d", code)
	}
	var inRoom, elsewhere chatrender.Preference
	chatlangServe(t, handler, "bob", "GET", "settings"+room, "", "", &inRoom)
	chatlangServe(t, handler, "bob", "GET", "settings", "", "", &elsewhere)
	if inRoom.ReadingLanguage != "ar" || inRoom.Translate || elsewhere.ReadingLanguage != "de" {
		t.Fatalf("conversation override: room %+v elsewhere %+v", inRoom, elsewhere)
	}
	for _, bad := range []string{`{"settings":{"tone":"as-written","reading_language":"xx"}}`, `{"settings":{"tone":"x","reading_language":"en"}}`, `{"settings":{"tone":"as-written","reading_language":"und"}}`} {
		if code := chatlangServe(t, handler, "bob", "PUT", "settings", bad, "", nil); code != http.StatusBadRequest {
			t.Fatalf("invalid settings %s answered %d", bad, code)
		}
	}
	// A person's settings are theirs: alice's read shows alice's own.
	var own chatrender.Preference
	chatlangServe(t, handler, "alice", "GET", "settings", "", "en-US", &own)
	if own.ReadingLanguage != "en" || len(own.FurtherLanguages) != 0 {
		t.Fatalf("alice sees bob's settings: %+v", own)
	}
	// Restore bob's personal choice for the rest of the test.
	if code := chatlangServe(t, handler, "bob", "PUT", "settings"+room, `{"settings":{"tone":"as-written","reading_language":"de","translate":true}}`, "", nil); code != 200 {
		t.Fatalf("restoring: %d", code)
	}

	// The audience: who reads a translation of an English message. Off until the
	// workspace turns translation on.
	var audience ChatlangAudienceView
	if code := chatlangServe(t, handler, "alice", "GET", "audience"+room, "", "", &audience); code != 200 || audience.Offered || len(audience.Readers) != 0 || audience.Language != "en" {
		t.Fatalf("audience before translation is on: %d %+v", code, audience)
	}
	if _, err = store.PutChatlangWorkspace(ctx, "tenant-a", "alice", chatlang.Workspace{Enabled: true, ExternalAllowed: true}); err != nil {
		t.Fatal(err)
	}
	governance.forget("tenant-a")
	if code := chatlangServe(t, handler, "alice", "GET", "audience"+room, "", "", &audience); code != 200 || !audience.Offered || audience.Readers["de"] != 1 || len(audience.Readers) != 1 {
		t.Fatalf("audience with translation on: %d %+v", code, audience)
	}
	audience = ChatlangAudienceView{}
	if code := chatlangServe(t, handler, "alice", "GET", "audience"+room+"&language=de", "", "", &audience); code != 200 || audience.Language != "de" || len(audience.Readers) != 0 {
		t.Fatalf("a German message needs no translation for a German reader: %d %+v", code, audience)
	}
	if code := chatlangServe(t, handler, "alice", "GET", "audience"+room+"&language=xx", "", "", nil); code != http.StatusBadRequest {
		t.Fatalf("an unknown language answered %d", code)
	}
	if code := chatlangServe(t, handler, "alice", "GET", "audience", "", "", nil); code != http.StatusBadRequest {
		t.Fatalf("an audience with no conversation answered %d", code)
	}
	// The features answer says translation is on for the workspace.
	p, _ := trust.FromContext(chatlangRequest(t, "alice", "GET", "/", "", "").Context())
	if !chatlangTranslationFeature(trust.WithPrincipal(ctx, p), surface) || chatlangTranslationFeature(ctx, surface) || chatlangTranslationFeature(trust.WithPrincipal(ctx, p), nil) {
		t.Fatal("the translation feature flag is wrong")
	}

	// The reader endpoint: pending first, with what was asked for; then ready
	// once the deterministic producer has made the translation.
	var read map[string]chatui.ReaderSelection
	if code := chatlangServe(t, handler, "bob", "GET", "reader"+room+message, "", "", &read); code != 200 || read[post.ID].Mark.State != "pending" || len(read[post.ID].Mark.Wanted) != 1 || read[post.ID].Mark.Wanted[0] != chatrender.Translate || !read[post.ID].Mark.CanShowOriginal {
		t.Fatalf("first read: %d %+v", code, read)
	}
	job, err := store.ClaimRendering(ctx, "tenant-a", time.Minute)
	if err != nil || job.Request.Language != "de" {
		t.Fatalf("job: %+v %v", job, err)
	}
	produced, err := (chatrender.FixtureProducer{Text: "Bitte lesen Sie diese Nachricht heute mit Ihrem Team.", Language: "de"}).Produce(ctx, job.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteRendering(ctx, job, produced); err != nil {
		t.Fatal(err)
	}
	read = nil
	if code := chatlangServe(t, handler, "bob", "GET", "reader"+room+message, "", "", &read); code != 200 || read[post.ID].Mark.State != "ready" || read[post.ID].Rendering.Language != "de" || read[post.ID].Mark.SourceLanguage != "en" || len(read[post.ID].Mark.Kinds) != 1 || read[post.ID].Mark.Kinds[0] != chatrender.Translate {
		t.Fatalf("second read: %d %+v", code, read)
	}
	// Alice reads her own message as written, with no mark.
	var own1 map[string]chatui.ReaderSelection
	if code := chatlangServe(t, handler, "alice", "GET", "reader"+room+message, "", "", &own1); code != 200 || own1[post.ID].Mark.State != "original" || len(own1[post.ID].Mark.Wanted) != 0 {
		t.Fatalf("the writer's read: %d %+v", code, own1)
	}

	// What the server answered is what the page renders: the translation, marked,
	// in the reader's language, with the original one press away.
	selection := read[post.ID]
	selection.Revision = post.Revision
	model := chatui.Model{State: chatui.StateReady, Locale: "de-DE", SelectedID: "room", CurrentUser: "bob", CurrentTenantID: "tenant-a",
		ChatFeatures:     &chatui.ChatFeatures{Renderings: true, Translating: true},
		Conversations:    []chatui.Conversation{{ID: "room", Kind: chatui.PrivateChannel, Name: "room", Joined: true}},
		Messages:         []chatui.Message{{ID: post.ID, Revision: post.Revision, AuthorID: "alice", Author: "Alice", Body: post.Body, Sequence: 1}},
		ReaderSelections: map[string]chatui.ReaderSelection{post.ID: selection},
		Chatlang:         chatui.ChatlangModel{AudienceRoom: "room", Audience: chatui.ChatlangAudience{Language: "de", Offered: true, Readers: map[string]int{"en": 2}}}}
	page, err := ui.RenderToString(chatui.Build(model))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bitte lesen Sie diese Nachricht heute mit Ihrem Team.", "Übersetzt aus Englisch", "Original anzeigen", `lang="de"`, "2 Personen lesen dies auf Englisch"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page misses %q", want)
		}
	}
	if strings.Contains(page, "chatlang-original-text") {
		t.Fatal("the original is shown before anyone asked")
	}

	// The writer corrects a wrong detection; nobody else may, and the correction
	// drops the translation made from the wrong guess.
	if code := chatlangServe(t, handler, "bob", "POST", "correct-language"+room, correction("fr"), "", nil); code != http.StatusForbidden {
		t.Fatalf("a reader changed the language of another person's message: %d", code)
	}
	if code := chatlangServe(t, handler, "alice", "POST", "correct-language"+room, correction("zz"), "", nil); code != http.StatusBadRequest {
		t.Fatalf("an unknown language was accepted: %d", code)
	}
	if code := chatlangServe(t, handler, "alice", "POST", "correct-language"+room, correction("fr"), "", nil); code != 200 {
		t.Fatalf("the writer's correction: %d", code)
	}
	detection, err := store.RevisionLanguage(ctx, chatstore.RenderingScope{Principal: alice, Tenant: "tenant-a", Conversation: "room"}, post.ID, post.Revision)
	if err != nil || detection.Language != "fr" || !detection.Corrected {
		t.Fatalf("the correction was not recorded: %+v %v", detection, err)
	}

	// A person outside the conversation reads nothing about it.
	for _, path := range []string{"audience" + room, "languages" + room} {
		if code := chatlangServe(t, handler, "mallory", "GET", path, "", "", nil); code != http.StatusForbidden {
			t.Fatalf("an outsider read %s: %d", path, code)
		}
	}
}
