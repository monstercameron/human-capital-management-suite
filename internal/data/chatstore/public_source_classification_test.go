package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestTodo_AGENTP_012_PublicSourceClassification(t *testing.T) {
	store, owner, _ := publicAudienceFixture(t)
	ctx := context.Background()
	post, err := store.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: owner.TenantID, ConversationID: "persona-reply", IdempotencyKey: "classified-source"}, chat.Post{AuthorID: owner.SubjectID, Body: "policy question"})
	if err != nil {
		t.Fatal(err)
	}
	digest := publicChatBodyDigest(post.Body)
	if _, err = store.Store.PublicChatDisclosureClass(ctx, owner.TenantID, "persona-reply", post.ID, digest); err == nil {
		t.Fatal("unclassified source admitted")
	}
	before, err := store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Store.PutPublicChatPostClassification(ctx, owner.TenantID, "persona-reply", post.ID, digest, dlp.ClassInternal); err != nil {
		t.Fatal(err)
	}
	class, err := store.Store.PublicChatDisclosureClass(ctx, owner.TenantID, "persona-reply", post.ID, digest)
	if err != nil || class != dlp.ClassInternal {
		t.Fatalf("class=%s %v", class, err)
	}
	after, err := store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil || after.Revision <= before.Revision {
		t.Fatalf("classification did not fence %+v %v", after, err)
	}
	if err = store.Store.PutPublicChatPostClassification(ctx, owner.TenantID, "persona-reply", post.ID, digest, dlp.ClassInternal); err != nil {
		t.Fatal(err)
	}
	replayed, err := store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil || replayed.Revision != after.Revision {
		t.Fatalf("classification replay changed audience before=%d after=%d err=%v", after.Revision, replayed.Revision, err)
	}
	if err = store.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, "persona-reply", owner.TenantID, owner.SubjectID, post.ID, publicChatBodyDigest(post.Body), "body", "", "INTERNAL"); err != nil {
		t.Fatal(err)
	}
	if err = store.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, "persona-reply", owner.TenantID, owner.SubjectID, post.ID, publicChatBodyDigest(post.Body), "body", "", "PUBLIC"); err == nil {
		t.Fatal("caller downgraded current source class")
	}
	if err = store.Store.PutPublicChatPostClassification(ctx, owner.TenantID, "persona-reply", post.ID, publicChatBodyDigest("forged"), dlp.ClassPublic); !errors.Is(err, ErrAudienceChanged) {
		t.Fatalf("wrong bytes classification=%v", err)
	}
	if err = store.Store.RunTenantTx(ctx, owner.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET body='salary amount' WHERE tenant_id=$1 AND id=$2`, owner.TenantID, post.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Store.PublicChatDisclosureClass(ctx, owner.TenantID, "persona-reply", post.ID, digest); !errors.Is(err, ErrAudienceChanged) {
		t.Fatalf("edited source classification=%v", err)
	}
	if err = store.Store.PutPublicChatPostClassification(ctx, owner.TenantID, "persona-reply", post.ID, publicChatBodyDigest("salary amount"), dlp.ClassCompensation); err != nil {
		t.Fatal(err)
	}
	class, err = store.Store.PublicChatDisclosureClass(ctx, owner.TenantID, "persona-reply", post.ID, publicChatBodyDigest("salary amount"))
	if err != nil || class != dlp.ClassCompensation {
		t.Fatalf("actual restricted class=%s %v", class, err)
	}
	if err = store.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, "persona-reply", owner.TenantID, owner.SubjectID, post.ID, publicChatBodyDigest(post.Body), "body", "", "INTERNAL"); err == nil {
		t.Fatal("sensitive classification downgraded")
	}
}

func TestTodo_AGENTP_012_PrivateSourceClassification_Integration(t *testing.T) {
	store := adapterDB(t)
	ctx := context.Background()
	owner := chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	for _, kind := range []chat.ConversationKind{chat.PrivateChannel, chat.Direct, chat.Group} {
		t.Run(string(kind), func(t *testing.T) {
			room := "classified-" + string(kind)
			if _, err := store.CreateConversation(ctx, chat.Conversation{ID: room, TenantID: owner.TenantID, Kind: kind, OwnerID: owner.SubjectID, Revision: 1}, []chat.Membership{{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: room, SubjectID: owner.SubjectID, Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
				t.Fatal(err)
			}
			post, err := store.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: owner.TenantID, ConversationID: room, IdempotencyKey: room}, chat.Post{AuthorID: owner.SubjectID, AuthorHomeTenantID: owner.TenantID, Body: "policy question"})
			if err != nil {
				t.Fatal(err)
			}
			digest := publicChatBodyDigest(post.Body)
			if err = store.Store.PutPublicChatPostClassification(ctx, owner.TenantID, room, post.ID, digest, dlp.ClassInternal); err != nil {
				t.Fatal(err)
			}
			class, err := store.Store.ReadableChatDisclosureClass(ctx, owner.TenantID, owner.TenantID, owner.SubjectID, room, post.ID, digest)
			if err != nil || class != dlp.ClassInternal {
				t.Fatalf("private member class=%s err=%v", class, err)
			}
			for _, identity := range []struct{ host, home, subject string }{{owner.TenantID, owner.TenantID, "outsider"}, {owner.TenantID, "foreign", owner.SubjectID}, {"foreign", owner.TenantID, owner.SubjectID}} {
				if _, err = store.Store.ReadableChatDisclosureClass(ctx, identity.host, identity.home, identity.subject, room, post.ID, digest); err == nil {
					t.Fatalf("foreign or nonmember read admitted %+v", identity)
				}
			}
			if err = store.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, room, owner.TenantID, owner.SubjectID, post.ID, digest, "body", "", "INTERNAL"); err == nil {
				t.Fatal("private source acquired public disclosure authority")
			}
			if _, err = store.PutMembership(ctx, owner, chat.Membership{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: room, SubjectID: "late", Role: chat.Member, HistoryVisibility: chat.FromJoin}); err != nil {
				t.Fatal(err)
			}
			if _, err = store.Store.ReadableChatDisclosureClass(ctx, owner.TenantID, owner.TenantID, "late", room, post.ID, digest); !errors.Is(err, ErrNotMember) {
				t.Fatalf("from-join old source readable=%v", err)
			}
			if err = store.Store.RunTenantTx(ctx, owner.TenantID, func(tx dbport.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE chat_membership SET state='left' WHERE tenant_id=$1 AND conversation_id=$2 AND member_id=$3`, owner.TenantID, room, owner.SubjectID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err = store.Store.ReadableChatDisclosureClass(ctx, owner.TenantID, owner.TenantID, owner.SubjectID, room, post.ID, digest); !errors.Is(err, ErrNotMember) {
				t.Fatalf("removed member still reads=%v", err)
			}
			if err = store.Store.RunTenantTx(ctx, owner.TenantID, func(tx dbport.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE chat_post SET body='salary amount' WHERE tenant_id=$1 AND id=$2`, owner.TenantID, post.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err = store.Store.PublicChatDisclosureClass(ctx, owner.TenantID, room, post.ID, digest); !errors.Is(err, ErrAudienceChanged) {
				t.Fatalf("private edit accepted stale bytes=%v", err)
			}
		})
	}
}
