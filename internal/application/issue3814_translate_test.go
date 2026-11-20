package application

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func issue3814Surface(t *testing.T) (*ChatRenderingSurface, chatstore.RenderingScope, chatrender.Rendering) {
	t.Helper()
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
	scope := chatstore.RenderingScope{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, Tenant: "tenant-a", Conversation: "room"}
	c := chat.Conversation{ID: "room", TenantID: scope.Tenant, Kind: chat.PrivateChannel, OwnerID: "alice", Revision: 1}
	if _, err = adapter.CreateConversation(ctx, c, []chat.Membership{{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	post, err := adapter.SendPost(ctx, chat.SendPostRequest{Principal: scope.Principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "test"}, chat.Post{ID: "post", TenantID: c.TenantID, ConversationID: c.ID, AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "Bitte lesen wir die Nachricht heute mit dem Team.", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	original := chatrender.Rendering{Tenant: scope.Tenant, Message: post.ID, Revision: post.Revision, Tone: chatrender.AsWritten, Language: "de", SourceLanguage: "de", Text: post.Body}
	surface := &ChatRenderingSurface{Store: store, InterfaceLocale: "en", Policy: chatrenderPolicyFixture{policy: chatrender.Policy{Original: original, AllowOriginal: true, AllowedKinds: []chatrender.Kind{chatrender.Translate}}}}
	return surface, scope, original
}

// TestIssue3814_TranslateOnDemand_Integration: a reader whose settings leave a
// German message as written presses Translate; only that read translates it,
// the result is the cached rendering of the message (never the message), and
// the next read without the press is the original again.
func TestIssue3814_TranslateOnDemand_Integration(t *testing.T) {
	surface, scope, original := issue3814Surface(t)
	ctx := context.Background()
	pref := chatrender.DefaultPreference("en")
	pref.Translate = false
	if err := surface.PutLanguageSettings(ctx, scope, pref); err != nil {
		t.Fatal(err)
	}
	r, mark, err := surface.ReadRenderingSelection(ctx, scope, original.Message)
	if err != nil || mark.State != "original" || r.Text == "" || mark.Refused != "" {
		t.Fatalf("a plain read must stay the original: %v %+v %v", r, mark, err)
	}
	asked := chatrender.WithOnDemandTranslation(ctx)
	r, mark, err = surface.ReadRenderingSelection(asked, scope, original.Message)
	if err != nil || mark.State != "pending" || len(mark.Wanted) != 1 || mark.Wanted[0] != chatrender.Translate {
		t.Fatalf("pressing Translate must ask for a translation: %v %+v %v", r, mark, err)
	}
	job, err := surface.Store.ClaimRendering(ctx, scope.Tenant, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	done, err := (chatrender.FixtureProducer{Text: "Please read the message with the team today.", Language: "en"}).Produce(ctx, job.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err = surface.Store.CompleteRendering(ctx, job, done); err != nil {
		t.Fatal(err)
	}
	r, mark, err = surface.ReadRenderingSelection(asked, scope, original.Message)
	if err != nil || mark.State != "ready" || r.Text != "Please read the message with the team today." || !mark.CanShowOriginal || mark.SourceLanguage != "de" {
		t.Fatalf("asked read after the job: %v %+v %v", r, mark, err)
	}
	r, mark, err = surface.ReadRenderingSelection(ctx, scope, original.Message)
	if err != nil || mark.State != "original" || r.Text == "Please read the message with the team today." {
		t.Fatalf("the translation must not become the reader's standing view: %v %+v %v", r, mark, err)
	}
	// Reading German as a further language: the press still translates.
	pref.FurtherLanguages = []string{"de"}
	if err = surface.PutLanguageSettings(ctx, scope, pref); err != nil {
		t.Fatal(err)
	}
	r, mark, err = surface.ReadRenderingSelection(asked, scope, original.Message)
	if err != nil || mark.State != "ready" {
		t.Fatalf("a language the reader also reads is still translated on request: %v %+v %v", r, mark, err)
	}
}

// TestIssue3814_TranslateOnDemand_Security: policy refusals are explicit and
// nothing is queued: masked text, a channel without translation, the reader's
// own message, and a message already in the reading language.
func TestIssue3814_TranslateOnDemand_Security(t *testing.T) {
	surface, scope, original := issue3814Surface(t)
	plain := context.Background()
	ctx := chatrender.WithOnDemandTranslation(plain)
	pref := chatrender.DefaultPreference("en")
	pref.Translate = false
	if err := surface.PutLanguageSettings(plain, scope, pref); err != nil {
		t.Fatal(err)
	}
	for name, policy := range map[string]chatrender.Policy{
		"masked": {Original: original, RequireMask: true, AllowedKinds: []chatrender.Kind{chatrender.Mask, chatrender.Translate}},
		"off":    {Original: original, AllowOriginal: true},
		"own":    {Original: original, AllowOriginal: true, Own: true, AllowedKinds: []chatrender.Kind{chatrender.Translate}},
	} {
		surface.Policy = chatrenderPolicyFixture{policy: policy}
		r, mark, err := surface.ReadRenderingSelection(ctx, scope, original.Message)
		if err != nil || mark.Refused != chatrender.RefusedPolicy || mark.State == "ready" {
			t.Errorf("%s: want a policy refusal, got %v %+v %v", name, r, mark, err)
		}
		if name != "masked" && r.Text != original.Text {
			t.Errorf("%s: the original should be read, got %q", name, r.Text)
		}
		// A masked message may queue its own masking job; no job may translate.
		if job, err := surface.Store.ClaimRendering(plain, scope.Tenant, time.Minute); err == nil && job.Request.Message != "" {
			for _, kind := range job.Request.Kinds {
				if kind == chatrender.Translate {
					t.Errorf("%s: a refused press queued a translation", name)
				}
			}
		}
	}
	// A caller who may not read the conversation (not a member) and a caller from
	// another tenant are refused before anything is read or queued, press or not.
	claimed := func(label string) {
		t.Helper()
		if job, err := surface.Store.ClaimRendering(plain, scope.Tenant, time.Minute); err == nil && job.Request.Message != "" {
			t.Errorf("%s: a refused press queued work: %+v", label, job.Request)
		}
	}
	surface.Policy = chatrenderPolicyFixture{policy: chatrender.Policy{Original: original, AllowOriginal: true, AllowedKinds: []chatrender.Kind{chatrender.Translate}}, deny: true}
	if _, mark, err := surface.ReadRenderingSelection(ctx, scope, original.Message); !errors.Is(err, chatrender.ErrDenied) || mark.State != "" {
		t.Errorf("non-member: want a denial and no answer, got %+v %v", mark, err)
	}
	claimed("non-member")
	surface.Policy = chatrenderPolicyFixture{policy: chatrender.Policy{Original: original, AllowOriginal: true, AllowedKinds: []chatrender.Kind{chatrender.Translate}}}
	other := scope
	other.Tenant, other.Principal.TenantID = "tenant-b", "tenant-b"
	if _, mark, err := surface.ReadRenderingSelection(ctx, other, original.Message); !errors.Is(err, chat.ErrPermissionDenied) || mark.State != "" {
		t.Errorf("other tenant: want a denial and no answer, got %+v %v", mark, err)
	}
	claimed("other tenant")
	// Same language: nothing to translate, said plainly.
	same := original
	same.SourceLanguage, same.Language = "en", "en"
	surface.Policy = chatrenderPolicyFixture{policy: chatrender.Policy{Original: same, AllowOriginal: true, AllowedKinds: []chatrender.Kind{chatrender.Translate}}}
	_, mark, err := surface.ReadRenderingSelection(ctx, scope, original.Message)
	if err != nil || mark.Refused != chatrender.RefusedSameLanguage || mark.State != "original" {
		t.Errorf("same language: %+v %v", mark, err)
	}
}

type issue3814Port struct {
	*chatrenderHTTPFixture
	asked map[string]bool
}

func (p *issue3814Port) ReadRenderingSelection(ctx context.Context, scope chatstore.RenderingScope, id string) (chatrender.Rendering, chatrender.Mark, error) {
	p.asked[id] = chatrender.OnDemandTranslation(ctx)
	return p.chatrenderHTTPFixture.ReadRenderingSelection(ctx, scope, id)
}

// TestIssue3814_ReaderRouteCarriesTheAsk: only the messages named by translate=
// are read as asked for; the others are read as before.
func TestIssue3814_ReaderRouteCarriesTheAsk(t *testing.T) {
	port := &issue3814Port{chatrenderHTTPFixture: &chatrenderHTTPFixture{rendering: chatrender.Rendering{Text: "view"}, settings: chatrender.DefaultPreference("en")}, asked: map[string]bool{}}
	w := httptest.NewRecorder()
	ChatRenderingHandler{Port: port}.ServeHTTP(w, chatrenderRequest(t, http.MethodGet, ChatRenderingPath+"/reader?conversation=room&tenant=tenant-a&message=one&message=two&translate=two", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("reader answered %d: %s", w.Code, w.Body.String())
	}
	if port.asked["one"] || !port.asked["two"] {
		t.Errorf("asked = %v, want only two", port.asked)
	}
}
