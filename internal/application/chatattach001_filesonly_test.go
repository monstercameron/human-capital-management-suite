package application

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/pressly/goose/v3"
)

// chatattach001Served is the chat service on the test database with one
// private channel of two managers, and the composition the media overlay is
// built from.
func chatattach001Served(t *testing.T) (composedChat, *chat.Service, *chatstore.Adapter) {
	t.Helper()
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
	return composedChat{core: core, service: core, store: raw, extensions: &ChatExtensions{Conversations: core}}, core, store
}

func chatattach001Get(t *testing.T, edge http.Handler, method, path string, bearer bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if bearer {
		r.Header.Set("Authorization", "Bearer valid")
	}
	w := httptest.NewRecorder()
	edge.ServeHTTP(w, r)
	return w
}

// TestTodo_CHATATTACH_001_Integration_FilesOnly: a message can be its files. A
// post with an admitted file and no text is stored and read back, in the
// conversation and as a reply; a post with neither is still refused, and so is
// one whose only "file" is not an admitted file of the conversation.
func TestTodo_CHATATTACH_001_Integration_FilesOnly(t *testing.T) {
	ctx := context.Background()
	runtime, core, store := chatattach001Served(t)
	cfg := ChatMediaConfig{Authorize: runtime.extensions.AuthorizeMedia}.WithDefaults(t.TempDir(), "")
	edge, err := chatattach001Overlay(http.NotFoundHandler(), cfg, chatattach001Admission(t, "member"), ServeProfileLocalDev, runtime)
	if err != nil {
		t.Fatal(err)
	}
	upload := func(content string) chatmedia.Reference {
		t.Helper()
		w := httptest.NewRecorder()
		edge.ServeHTTP(w, chatattach001UploadRequest(t, []byte(content)))
		var ref chatmedia.Reference
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ref) != nil || ref.State != chatmedia.StateAdmitted {
			t.Fatalf("upload: %d %s", w.Code, w.Body.String())
		}
		return ref
	}
	request := chatattach001UploadRequest(t, []byte("x"))
	callCtx, _, denial := transport.Admit(ctx, chatattach001Admission(t, "member"), transport.AdmissionRequest{Metadata: transport.MapMetadata(request.Header), Method: request.URL.Path, Kind: transport.KindHTTPEdge})
	if denial != nil {
		t.Fatal(denial)
	}
	member := chat.Principal{TenantID: "tenant", SubjectID: "member"}
	file := func(ref chatmedia.Reference) chat.Reference {
		return chat.Reference{Kind: chat.MediaAttachment, TenantID: "tenant", ConversationID: "room", ID: ref.ArtifactID, Display: "note.txt", ContentType: "text/plain", ByteSize: uint64(ref.Size)}
	}
	send := func(key, body, parent string, refs ...chat.Reference) (chat.Post, error) {
		return core.SendPost(callCtx, chat.SendPostRequest{Principal: member, TenantID: "tenant", ConversationID: "room", Body: body, ParentID: parent, IdempotencyKey: key, References: refs})
	}

	first := upload("first file")
	post, err := send("files-only", "", "", file(first))
	if err != nil {
		t.Fatalf("a message of one file and no text was refused: %v", err)
	}
	persisted, err := store.GetPost(ctx, "tenant", "room", post.ID)
	if err != nil || persisted.Body != "" || len(persisted.References) != 1 || persisted.References[0].ID != first.ArtifactID {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
	// Spaces are not text: the stored body is empty, not spaces.
	second := upload("second file!")
	if spaced, err := send("files-spaces", "   \n ", "", file(second)); err != nil || spaced.Body != "" {
		t.Fatalf("a message of a file and blank text: %+v %v", spaced, err)
	}
	// A reply can be its files too, and stays under its parent.
	third := upload("third file!!")
	reply, err := send("files-reply", "", post.ID, file(third))
	if err != nil || reply.ParentID != post.ID || len(reply.References) != 1 {
		t.Fatalf("a reply of one file and no text: %+v %v", reply, err)
	}
	page, err := core.ListPosts(callCtx, chat.ListPostsRequest{Principal: member, TenantID: "tenant", ConversationID: "room", Page: chat.Page{PageSize: 50}})
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]chat.Post{}
	for _, p := range page.Posts {
		listed[p.ID] = p
	}
	if got, ok := listed[post.ID]; !ok || got.Body != "" || len(got.References) != 1 {
		t.Fatalf("the reader's page does not hold the files-only message: %+v", page.Posts)
	}

	// Still refused: nothing at all, a mention with no text, and a "file" the
	// conversation does not have.
	if _, err = send("nothing", "", ""); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a post with neither text nor a file: %v", err)
	}
	if _, err = send("mention-only", "  ", "", chat.Reference{Kind: chat.PersonMention, TenantID: "tenant", ConversationID: "room", ID: "bob", Display: "Bob"}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a post of a mention and no text: %v", err)
	}
	forged := file(first)
	forged.ID = "not-an-artifact"
	if _, err = send("forged", "", "", forged); err == nil {
		t.Fatal("a post whose only file is not an admitted file was accepted")
	}
	// A file of another conversation is not this conversation's file.
	foreign := file(first)
	foreign.ConversationID = "another-room"
	if _, err = send("foreign", "", "", foreign); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a post whose only file belongs to another conversation: %v", err)
	}
}

// TestTodo_CHATATTACH_001_Security_Availability: the page is told whether the
// server takes uploads. Where no scanner is composed the answer is no, the
// upload route still fails closed, and a files-only post cannot be sent
// because no file can be admitted.
func TestTodo_CHATATTACH_001_Security_Availability(t *testing.T) {
	runtime, core, _ := chatattach001Served(t)
	cfg := ChatMediaConfig{Authorize: runtime.extensions.AuthorizeMedia}.WithDefaults(t.TempDir(), "")
	admission := chatattach001Admission(t, "member")
	composed, err := chatattach001Overlay(http.NotFoundHandler(), cfg, admission, ServeProfileLocalDev, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if w := chatattach001Get(t, composed, http.MethodGet, ChatAttachmentsAvailabilityPath, true); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"uploads":true}` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("where uploads are composed: %d %q", w.Code, w.Body.String())
	}

	// The standard profile with no scanner: a fresh service that was never
	// given a media directory, as the served product is there.
	bare := chat.NewService(chatstore.NewAdapter(runtime.store), func() time.Time { return time.Now().UTC() })
	bare.SetAuthority(servedPersonaChatAuthority{})
	standard := composedChat{core: bare, service: bare, store: runtime.store, extensions: &ChatExtensions{Conversations: bare}}
	closed, err := chatattach001Overlay(http.NotFoundHandler(), cfg, admission, ServeProfileStandard, standard)
	if err != nil {
		t.Fatal(err)
	}
	if w := chatattach001Get(t, closed, http.MethodGet, ChatAttachmentsAvailabilityPath, true); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"uploads":false}` {
		t.Fatalf("where no scanner is composed: %d %q", w.Code, w.Body.String())
	}
	w := httptest.NewRecorder()
	closed.ServeHTTP(w, chatattach001UploadRequest(t, []byte("hello123")))
	if w.Code < 400 {
		t.Fatalf("an upload was admitted without a scanner: %d %s", w.Code, w.Body.String())
	}
	request := chatattach001UploadRequest(t, []byte("x"))
	callCtx, _, denial := transport.Admit(context.Background(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(request.Header), Method: request.URL.Path, Kind: transport.KindHTTPEdge})
	if denial != nil {
		t.Fatal(denial)
	}
	_, err = bare.SendPost(callCtx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant", SubjectID: "member"}, TenantID: "tenant", ConversationID: "room", IdempotencyKey: "no-media",
		References: []chat.Reference{{Kind: chat.MediaAttachment, TenantID: "tenant", ConversationID: "room", ID: "anything", Display: "note.txt"}}})
	if !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a files-only post where no file can be admitted: %v", err)
	}
	// The composed service is a different one: composing uploads for it gave
	// the bare one nothing.
	if core == bare {
		t.Fatal("the fixture did not separate the two services")
	}

	// The answer is for people who are signed in, by GET, and every other path
	// goes on to the media routes.
	for _, edge := range []http.Handler{composed, closed} {
		if w := chatattach001Get(t, edge, http.MethodGet, ChatAttachmentsAvailabilityPath, false); w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
			t.Fatalf("without a bearer: %d %s", w.Code, w.Body.String())
		}
		if w := chatattach001Get(t, edge, http.MethodPost, ChatAttachmentsAvailabilityPath, true); w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodGet {
			t.Fatalf("by POST: %d", w.Code)
		}
		if w := chatattach001Get(t, edge, http.MethodGet, "/somewhere/else", true); w.Code != http.StatusNotFound {
			t.Fatalf("another path: %d", w.Code)
		}
	}
}
