package chatmedia_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type chatattach001PostgresAuthority struct{ store chat.Store }

func (a chatattach001PostgresAuthority) Authorize(ctx context.Context, p chat.Principal, c chat.Conversation, _ chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	m, err := a.store.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	if err != nil || m.LeftAt != nil {
		return chatpolicy.Input{}, chat.ErrPermissionDenied
	}
	return chatpolicy.Input{Principal: chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true}, Channel: chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: !c.Archived, Revision: c.Revision}, Membership: chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: 1, JoinedAt: now.Add(-time.Hour)}, HasMembership: true, Now: now}, nil
}

type chatattach001PostgresDirectory struct{ media *chatmedia.Service }

func (d chatattach001PostgresDirectory) MediaArtifact(ctx context.Context, tenant, room, id string) (chat.MediaFacts, error) {
	r, err := d.media.Chatattach001Describe(ctx, tenant, room, id)
	return chat.MediaFacts{ContentType: string(r.MediaType), ByteSize: uint64(r.Size), Admitted: r.State == chatmedia.StateAdmitted}, err
}

// This package's integration proof remains runnable independently of other
// lanes' application tests. Upload authority and post persistence reach the
// real shared PostgreSQL through an isolated test schema.
func TestTodo_CHATATTACH_001_Integration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	// The index-only scaling migration waits on unrelated sessions' snapshots.
	// This attachment test needs the schema and policies, not scaling indexes.
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true), goose.WithExcludeVersions([]int64{34}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("search_path", db.Schema)
	dsn.RawQuery = query.Encode()
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: dsn.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)
	store := chatstore.NewAdapter(raw)
	members := []chat.Membership{}
	for _, person := range []string{"alice", "bob"} {
		members = append(members, chat.Membership{TenantID: "tenant", ConversationID: "room", HomeTenantID: "tenant", SubjectID: person, Role: chat.Manager, HistoryVisibility: chat.FullHistory})
	}
	if _, err := store.CreateConversation(ctx, chat.Conversation{ID: "room", TenantID: "tenant", Kind: chat.PrivateChannel, Name: "Files", OwnerID: "alice", Revision: 1}, members, "files"); err != nil {
		t.Fatal(err)
	}
	core := chat.NewService(store, time.Now)
	core.SetAuthority(chatattach001PostgresAuthority{store: store})
	authorize := func(ctx context.Context, r chatmedia.AccessRequest) error {
		m, err := store.GetMembership(ctx, r.TenantID, r.ConversationID, r.TenantID, r.PrincipalID)
		if err != nil || m.LeftAt != nil {
			return chatmedia.ErrUnauthorized
		}
		return nil
	}
	root := t.TempDir()
	files, err := chatmedia.NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	media := chatmedia.New(chatmedia.Config{Store: files, Scanner: chatmedia.Chatattach001Scanner{}, Authorize: authorize})
	core.SetMediaDirectory(chatattach001PostgresDirectory{media: media})
	uploads := &chatmedia.Chatattach001Uploads{Media: media, Root: root, Authorize: authorize, PersonBytes: 12, ConversationBytes: 17}
	uploads.Linked = func(ctx context.Context, r chatmedia.Reference) (bool, error) {
		needle, _ := json.Marshal([]map[string]string{{"Kind": "MEDIA", "ID": r.ArtifactID}})
		var linked bool
		err := raw.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND references_json @> $3::jsonb)`, r.TenantID, r.ConversationID, string(needle)).Scan(&linked)
		})
		return linked, err
	}
	r := chatmedia.UploadRequest{TenantID: "tenant", ConversationID: "room", PrincipalID: "alice", Filename: "note.txt", Content: []byte("hello123")}
	ref, err := uploads.Upload(ctx, r)
	if err != nil || ref.State != chatmedia.StateAdmitted {
		t.Fatalf("upload=%+v err=%v", ref, err)
	}
	post, err := core.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant", SubjectID: "alice"}, TenantID: "tenant", ConversationID: "room", Body: "Attached file", IdempotencyKey: "attachment-post", References: []chat.Reference{{Kind: chat.MediaAttachment, TenantID: "tenant", ConversationID: "room", ID: ref.ArtifactID, Display: "note.txt", ContentType: "text/plain", ByteSize: 8}}})
	if err != nil {
		t.Fatalf("post with attachment=%v", err)
	}
	stored, err := store.GetPost(ctx, "tenant", "room", post.ID)
	if err != nil || len(stored.References) != 1 || stored.References[0].ID != ref.ArtifactID {
		t.Fatalf("persisted=%+v err=%v", stored, err)
	}
	t.Run("type", func(t *testing.T) {
		denied := r
		denied.Content = []byte{'M', 'Z', 0, 0}
		if _, err := uploads.Upload(ctx, denied); !errors.Is(err, chatmedia.ErrUnsupported) {
			t.Fatalf("type refusal=%v", err)
		}
	})
	t.Run("size", func(t *testing.T) {
		denied := r
		denied.Content = bytes.Repeat([]byte("a"), int(chatmedia.Chatattach001MaxBytes)+1)
		if _, err := uploads.Upload(ctx, denied); !errors.Is(err, chatmedia.ErrChatattach001Size) {
			t.Fatalf("size refusal=%v", err)
		}
	})
	t.Run("permission", func(t *testing.T) {
		denied := r
		denied.PrincipalID = "outsider"
		if _, err := uploads.Upload(ctx, denied); !errors.Is(err, chatmedia.ErrUnauthorized) {
			t.Fatalf("permission refusal=%v", err)
		}
	})
	t.Run("person-quota", func(t *testing.T) {
		denied := r
		denied.Content = []byte("morefiles")
		if _, err := uploads.Upload(ctx, denied); !errors.Is(err, chatmedia.ErrChatattach001Quota) {
			t.Fatalf("person quota=%v", err)
		}
	})
	t.Run("conversation-quota", func(t *testing.T) {
		other := r
		other.PrincipalID = "bob"
		other.Content = []byte("morefiles")
		if _, err := uploads.Upload(ctx, other); err != nil {
			t.Fatal(err)
		}
		other.Content = []byte("z")
		if _, err := uploads.Upload(ctx, other); !errors.Is(err, chatmedia.ErrChatattach001Quota) {
			t.Fatalf("conversation quota=%v", err)
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
		if _, err := media.Chatattach001Describe(ctx, "tenant", "room", ref.ArtifactID); err == nil {
			t.Fatal("purged message retained file")
		}
	})
}
