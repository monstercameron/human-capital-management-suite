package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

type sealedPublicRefusingOwner struct{ calls int }

func (s *sealedPublicRefusingOwner) AuthorizeSealedPublicPersonaReply(context.Context, agentsecurity.FinalOutputPersistence) (workload.Identity, error) {
	s.calls++
	return workload.Identity{}, chat.ErrPermissionDenied
}

func TestTodo_AGENTP_012_SealedPublicReplyRefusesFabricatedOutputBeforeEffects(t *testing.T) {
	store, _ := chatFixture(t)
	owner := &sealedPublicRefusingOwner{}
	if _, err := NewSealedPublicPersonaDelivery(nil, owner, time.Now); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("missing native owner accepted: %v", err)
	}
	delivery, err := NewSealedPublicPersonaDelivery(store, owner, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := delivery.CommitPersonaReply(context.Background(), chat.PersonaReplyCommitRequest{}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("unsealed commit accepted: %v", err)
	}
	if _, err := delivery.CommitSealedPersonaReply(context.Background(), agentsecurity.FinalOutputPersistence{}, chat.PersonaReplyCommitRequest{}); !errors.Is(err, chat.ErrPermissionDenied) || owner.calls != 0 {
		t.Fatalf("fabricated output reached owner: %v calls=%d", err, owner.calls)
	}
	if err := store.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		for _, query := range []string{`SELECT count(*) FROM chat_post`, `SELECT count(*) FROM chat_outbox`} {
			var count int
			if err := tx.QueryRow(context.Background(), query).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatalf("fabricated public output created an effect: %s count=%d", query, count)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_AGENTP_012_PublicAuthorRefusesPrivateHistory(t *testing.T) {
	store, _ := personaReplyFixture(t)
	ctx := context.Background()
	if err := store.Store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_conversation SET kind='PRIVATE_CHANNEL' WHERE tenant_id='tenant-a' AND id='persona-reply'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	revision, err := store.AudienceRevision(ctx, "tenant-a", "persona-reply")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Store.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", ConversationID: "persona-reply", HomeTenantID: "tenant-a", AuthorID: "persona", Body: "must not enter private history", ClientKey: "invalid-shared-route", TrustedAuthor: true, ExpectedAudienceRevision: revision}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("public persona writer entered private history: %v", err)
	}
	if err := store.Store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_post`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("refused route wrote %d posts", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
