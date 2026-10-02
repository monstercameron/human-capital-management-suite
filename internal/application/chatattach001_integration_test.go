package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	mediahttp "github.com/monstercameron/human-capital-management-suite/internal/transport/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

func chatattach001Admission(t *testing.T, subject string) transport.Config {
	t.Helper()
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant"), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "attachment-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return transport.Config{Verifier: mediaVerifier{principal: p}}
}

func chatattach001UploadRequest(t *testing.T, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/media/upload?host_tenant_id=tenant&conversation_id=room", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("Authorization", "Bearer valid")
	return r
}

func TestTodo_CHATATTACH_001_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	// Index-only scaling migration 34 waits on unrelated shared-server readers.
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true), goose.WithExcludeVersions([]int64{34}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)
	store := chatstore.NewAdapter(raw)
	members := []chat.Membership{}
	for _, subject := range []string{"member", "bob"} {
		members = append(members, chat.Membership{TenantID: "tenant", ConversationID: "room", HomeTenantID: "tenant", SubjectID: subject, Role: chat.Manager, HistoryVisibility: chat.FullHistory})
	}
	if _, err := store.CreateConversation(ctx, chat.Conversation{ID: "room", TenantID: "tenant", Kind: chat.PrivateChannel, Name: "Attachments", OwnerID: "member", Revision: 1}, members, "attachment-room"); err != nil {
		t.Fatal(err)
	}
	core := chat.NewService(store, func() time.Time { return time.Now().UTC() })
	core.SetAuthority(servedPersonaChatAuthority{})
	runtime := composedChat{core: core, service: core, store: raw, extensions: &ChatExtensions{Conversations: core}}
	root := t.TempDir()
	cfg := ChatMediaConfig{Authorize: runtime.extensions.AuthorizeMedia}.WithDefaults(root, "")
	edge, err := chatattach001Overlay(http.NotFoundHandler(), cfg, chatattach001Admission(t, "member"), ServeProfileLocalDev, runtime)
	if err != nil {
		t.Fatal(err)
	}
	request := chatattach001UploadRequest(t, []byte("hello123"))
	w := httptest.NewRecorder()
	edge.ServeHTTP(w, request)
	if w.Code != 200 {
		t.Fatalf("served upload: %d %s", w.Code, w.Body.String())
	}
	var ref chatmedia.Reference
	if err := json.Unmarshal(w.Body.Bytes(), &ref); err != nil {
		t.Fatal(err)
	}
	if ref.ArtifactID == "" || ref.TenantID != "tenant" || ref.ConversationID != "room" || ref.MediaType != "text/plain" || ref.Size != 8 || ref.State != chatmedia.StateAdmitted {
		t.Fatalf("reference=%+v", ref)
	}
	callCtx, _, denial := transport.Admit(ctx, chatattach001Admission(t, "member"), transport.AdmissionRequest{Metadata: transport.MapMetadata(request.Header), Method: request.URL.Path, Kind: transport.KindHTTPEdge})
	if denial != nil {
		t.Fatal(denial)
	}
	post, err := core.SendPost(callCtx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant", SubjectID: "member"}, TenantID: "tenant", ConversationID: "room", Body: "Here is the file", IdempotencyKey: "attachment-post", References: []chat.Reference{{Kind: chat.MediaAttachment, TenantID: "tenant", ConversationID: "room", ID: ref.ArtifactID, Display: "note.txt", ContentType: "text/plain", ByteSize: 8}}})
	if err != nil {
		t.Fatalf("post with attachment: %v", err)
	}
	persisted, err := store.GetPost(ctx, "tenant", "room", post.ID)
	if err != nil || len(persisted.References) != 1 || persisted.References[0].ID != ref.ArtifactID || persisted.References[0].ByteSize != 8 {
		t.Fatalf("persisted post=%+v err=%v", persisted, err)
	}
	t.Run("download", func(t *testing.T) {
		path := "/v1/chat/media/" + ref.ArtifactID
		query := "?host_tenant_id=tenant&conversation_id=room"
		grantRequest := httptest.NewRequest(http.MethodGet, path+"/grant"+query, nil)
		grantRequest.Header.Set("Authorization", "Bearer valid")
		w := httptest.NewRecorder()
		edge.ServeHTTP(w, grantRequest)
		var grant struct {
			Token string `json:"grant"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &grant) != nil || grant.Token == "" {
			t.Fatalf("download grant=%d %s", w.Code, w.Body.String())
		}
		read := httptest.NewRequest(http.MethodGet, path+query, nil)
		read.Header.Set("Authorization", "Bearer valid")
		read.Header.Set("X-Chat-Media-Grant", grant.Token)
		w = httptest.NewRecorder()
		edge.ServeHTTP(w, read)
		if w.Code != 200 || w.Body.String() != "hello123" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("protected bytes=%d %q", w.Code, w.Body.String())
		}
	})
	t.Run("type", func(t *testing.T) {
		w := httptest.NewRecorder()
		edge.ServeHTTP(w, chatattach001UploadRequest(t, []byte{'M', 'Z', 0, 1, 3}))
		if w.Code != 415 {
			t.Fatalf("type status=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("size", func(t *testing.T) {
		w := httptest.NewRecorder()
		edge.ServeHTTP(w, chatattach001UploadRequest(t, bytes.Repeat([]byte("a"), int(chatmedia.Chatattach001MaxBytes)+1)))
		if w.Code != 413 {
			t.Fatalf("size status=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("post-permission", func(t *testing.T) {
		denied, err := chatattach001Overlay(http.NotFoundHandler(), cfg, chatattach001Admission(t, "outsider"), ServeProfileLocalDev, runtime)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		denied.ServeHTTP(w, chatattach001UploadRequest(t, []byte("outsider")))
		if w.Code != 403 {
			t.Fatalf("post authority status=%d %s", w.Code, w.Body.String())
		}
	})
	// The quota port below uses the same PostgreSQL authority and retention
	// callback, with small configured limits so the test is bounded.
	fs, err := chatmedia.NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	media := chatmedia.New(chatmedia.Config{Store: fs, Scanner: chatmedia.Chatattach001Scanner{}, Authorize: cfg.Authorize})
	uploads := &chatmedia.Chatattach001Uploads{Media: media, Root: root, Authorize: chatattach001PostAuthority(runtime), Linked: chatattach001Linked(raw), PersonBytes: 12, ConversationBytes: 17}
	h := &chatattach001HTTP{uploads: uploads, principal: cfg.Principal, reads: mediahttp.NewHandler(media, cfg.Principal), readAuthorize: cfg.Authorize, slots: make(chan struct{}, 4)}
	invoke := func(subject string, content []byte) *httptest.ResponseRecorder {
		r := chatattach001UploadRequest(t, content)
		admitted, _, denial := transport.Admit(ctx, chatattach001Admission(t, subject), transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denial != nil {
			t.Fatal(denial)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r.WithContext(admitted))
		return w
	}
	t.Run("person-quota", func(t *testing.T) {
		w := invoke("member", []byte("morefiles"))
		if w.Code != 429 {
			t.Fatalf("person quota=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("conversation-quota", func(t *testing.T) {
		w := invoke("bob", []byte("morefiles"))
		if w.Code != 200 {
			t.Fatalf("second uploader=%d %s", w.Code, w.Body.String())
		}
		w = invoke("bob", []byte("z"))
		if w.Code != 429 {
			t.Fatalf("conversation quota=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("retention", func(t *testing.T) {
		if err := uploads.Retain(ctx, "tenant"); err != nil {
			t.Fatal(err)
		}
		if err := raw.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM chat_post WHERE tenant_id=$1 AND id=$2`, "tenant", post.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := uploads.Retain(ctx, "tenant"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, ref.ArtifactID+".bytes")); !os.IsNotExist(err) {
			t.Fatalf("message purge retained attachment: %v", err)
		}
	})
}

func TestTodo_CHATATTACH_001_Security(t *testing.T) {
	calls := 0
	check := chatattach001AttachmentTypes(func(_ context.Context, in chat.ContentInput) ([]string, error) {
		calls++
		return []string{"text/plain"}, nil
	})
	in := chat.ContentInput{}
	for range 10 {
		in.References = append(in.References, chat.Reference{Kind: chat.MediaAttachment})
	}
	if kinds, err := check(context.Background(), in); err != nil || len(kinds) != 1 || calls != 1 {
		t.Fatalf("ten files kinds=%v calls=%d err=%v", kinds, calls, err)
	}
	in.References = append(in.References, chat.Reference{Kind: chat.MediaAttachment})
	if _, err := check(context.Background(), in); !errors.Is(err, chat.ErrInvalidArgument) || calls != 1 {
		t.Fatalf("eleven files calls=%d err=%v", calls, err)
	}
	if _, err := chatattach001AttachmentTypes(nil)(context.Background(), chat.ContentInput{}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatal(err)
	}
	// An unauthorized read must not trigger retention or another handler.
	h := &chatattach001HTTP{principal: func(*http.Request) (string, string, string, bool) { return "tenant", "room", "intruder", true }, readAuthorize: func(context.Context, chatmedia.AccessRequest) error { return chatmedia.ErrUnauthorized }, reads: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unauthorized read reached media") })}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/chat/media/file", nil))
	if w.Code != 403 {
		t.Fatalf("read authority=%d", w.Code)
	}
}
