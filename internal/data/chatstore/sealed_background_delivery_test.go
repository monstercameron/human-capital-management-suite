package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type sealedBackgroundRefusingOwner struct{ calls int }

func (s *sealedBackgroundRefusingOwner) AuthorizeSealedBackgroundPersonaReply(context.Context, agentsecurity.FinalOutputPersistence) (SealedBackgroundPersonaAuthority, error) {
	s.calls++
	return SealedBackgroundPersonaAuthority{}, chat.ErrPermissionDenied
}
func (s *sealedBackgroundRefusingOwner) RenderSealedBackgroundPersonaReply(agentsecurity.FinalOutputPersistence) (string, error) {
	return "unexpected", nil
}

func TestSealedBackgroundPersonaDeliveryRefusesCallerFabricatedOutputBeforeEffects(t *testing.T) {
	s, _ := chatFixture(t)
	owner := &sealedBackgroundRefusingOwner{}
	if _, err := NewSealedBackgroundPersonaDelivery(nil, owner, owner, time.Now); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("missing persistence owner: %v", err)
	}
	native, err := NewSealedBackgroundPersonaDelivery(s, owner, owner, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = native.CommitSealedBackgroundPersonaReply(context.Background(), agentsecurity.FinalOutputPersistence{}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("fabricated sealed output: %v", err)
	}
	if owner.calls != 0 {
		t.Fatalf("unsealed output reached authority: %d", owner.calls)
	}
	if err = s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		for _, query := range []string{`SELECT count(*) FROM chat_post`, `SELECT count(*) FROM chat_ephemeral_post`, `SELECT count(*) FROM chat_outbox`} {
			var count int
			if e := tx.QueryRow(context.Background(), query).Scan(&count); e != nil {
				return e
			}
			if count != 0 {
				t.Errorf("fabricated output produced rows: %s = %d", query, count)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if sealedBackgroundAuthorityMatches(SealedBackgroundPersonaAuthority{}, agentsecurity.FinalOutputIdentity{}, time.Now()) {
		t.Fatal("caller supplied zero worker identity authorized delivery")
	}
}

func TestSealedBackgroundPersonaDeliveryCurrentReadsShareNativeFence(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "native-private", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "alice", Role: "manager", State: "active"}})
	if err := s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		_, e := tx.Exec(context.Background(), `INSERT INTO chat_channel_policy(tenant_id,conversation_id,revision,role_mode,classification) VALUES('tenant-a','native-private',1,1,'T0')`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	p, err := s.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "native-private", AuthorID: "alice", Body: "question", ClientKey: "native-source"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err = tenant(ctx, tx, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if err = fenceContextWrite(ctx, tx, "tenant-a", "native-private"); err != nil {
		t.Fatal(err)
	}
	nativeContext := context.WithValue(ctx, sealedBackgroundTransactionKey{}, sealedBackgroundTransaction{store: s, tenantID: "tenant-a", tx: tx})
	if _, ok := sealedBackgroundTxFromContext(nativeContext, &Store{}, "tenant-a"); ok {
		t.Fatal("native transaction crossed store ownership")
	}
	if _, ok := sealedBackgroundTxFromContext(nativeContext, s, "tenant-b"); ok {
		t.Fatal("native transaction crossed tenant scope")
	}
	audience, err := s.CaptureAudienceSnapshot(nativeContext, "tenant-a", "native-private")
	if err != nil || len(audience.Members) != 1 {
		t.Fatalf("current audience blocked by its native fence: %+v %v", audience, err)
	}
	thread, err := NewAdapter(s).CaptureBackgroundThreadSnapshot(nativeContext, chat.BackgroundThreadSnapshotRequest{TenantID: "tenant-a", ReaderID: "alice", ConversationID: "native-private", ThreadID: p.ID, InvokingPostID: p.ID, Limit: chat.MaxThreadSnapshotPosts})
	if err != nil || len(thread.Posts) != 1 || thread.Posts[0].ID != p.ID {
		t.Fatalf("current thread blocked by its native fence: %+v %v", thread, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CaptureAudienceSnapshot(context.Background(), "tenant-a", "native-private"); err != nil {
		t.Fatalf("native reader committed or consumed caller transaction: %v", err)
	}
}
