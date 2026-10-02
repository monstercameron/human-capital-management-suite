package chatstore

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// TestTodo_CHATMOD_002_TrustedGuard: a server-owned write (an agent's public
// answer, a scheduled announcement) skips the chat service's content policy, so
// the store asks the installed guard before it writes. A refusal writes nothing.
func TestTodo_CHATMOD_002_TrustedGuard(t *testing.T) {
	s, _ := personaReplyFixture(t)
	ctx := context.Background()
	revision, err := s.AudienceRevision(ctx, "tenant-a", "persona-reply")
	if err != nil {
		t.Fatal(err)
	}
	raw := func(key, body string, trusted bool) SendRequest {
		return SendRequest{TenantID: "tenant-a", ConversationID: "persona-reply", HomeTenantID: "tenant-a", AuthorID: "persona", ClientKey: key, Body: body, TrustedAuthor: trusted, ExpectedAudienceRevision: revision}
	}
	count := func() int {
		var n int
		if err := s.Store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM chat_post WHERE tenant_id='tenant-a' AND conversation_id='persona-reply'`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var asked []string
	s.Store.SetTrustedContentGuard(func(_ context.Context, tenant, conversation, author, body string) error {
		asked = append(asked, tenant+"/"+conversation+"/"+author+"/"+body)
		if body == "quartz" {
			return &chatfilter.BlockedError{RuleName: "Project", Span: chatfilter.Span{Start: 0, End: 6}}
		}
		return nil
	})
	if _, err := s.Store.sendPostRaw(ctx, raw("k1", "quartz", true)); !errors.Is(err, chatfilter.ErrBlocked) {
		t.Fatalf("a trusted write bypassed the guard: %v", err)
	}
	if count() != 0 {
		t.Fatal("a refused trusted write was stored")
	}
	if _, err := s.Store.sendPostRaw(ctx, raw("k2", "a clean answer", true)); err != nil {
		t.Fatalf("a clean trusted write was refused: %v", err)
	}
	if count() != 1 || len(asked) != 2 || asked[1] != "tenant-a/persona-reply/persona/a clean answer" {
		t.Fatalf("stored %d, guard asked %q", count(), asked)
	}
	// An ordinary member write is the service's to judge, not the guard's.
	s.Store.SetTrustedContentGuard(func(context.Context, string, string, string, string) error {
		return errors.New("guard must not see ordinary writes")
	})
	if _, err := s.Store.sendPostRaw(ctx, raw("k3", "quartz", false)); err != nil && err.Error() == "guard must not see ordinary writes" {
		t.Fatal("the guard judged an ordinary write")
	}
}
