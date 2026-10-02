package chatstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATSCALE_002_Integration_Send(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversation(t, s, "chatscale-send")
	req := SendRequest{TenantID: "chatscale-send", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "chatscale-combined", Body: "Payroll follow-up"}
	post, err := s.sendPostRaw(ctx, req)
	if err != nil || post.Sequence != 1 || post.Revision != 1 {
		t.Fatalf("send=%+v err=%v", post, err)
	}
	replay, err := s.sendPostRaw(ctx, req)
	if err != nil || replay.ID != post.ID || replay.Sequence != post.Sequence {
		t.Fatalf("replay=%+v original=%+v err=%v", replay, post, err)
	}
	if err := s.RunTenantTx(ctx, req.TenantID, func(tx dbport.Tx) error {
		var body, language string
		if err := tx.QueryRow(ctx, `SELECT body,source_language FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND revision=1`, req.TenantID, post.ID).Scan(&body, &language); err != nil {
			return err
		}
		// A plain append moves the head but not the history revision: only edits,
		// removals and deletions invalidate cached badges (migration 00046).
		var head, generation, events, keys int
		if err := tx.QueryRow(ctx, `SELECT post_sequence,chatscale_history_revision,
 (SELECT count(*) FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='post.created'),
 (SELECT count(*) FROM chat_idempotency WHERE tenant_id=$1 AND post_id=$2)
 FROM chat_conversation WHERE tenant_id=$1 AND id='c-1'`, req.TenantID, post.ID).Scan(&head, &generation, &events, &keys); err != nil {
			return err
		}
		if body != req.Body || language == "" || head != 1 || generation != 0 || events != 1 || keys != 1 {
			return fmt.Errorf("body=%q language=%q head=%d generation=%d events=%d keys=%d", body, language, head, generation, events, keys)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	denied := req
	denied.AuthorID, denied.ClientKey = "outsider", "chatscale-denied"
	if _, err := s.sendPostRaw(ctx, denied); !errors.Is(err, ErrNotMember) {
		t.Fatalf("unauthorized send error=%v", err)
	}
	// Fail the second CTE's insertion. Both the post and sequence/head updates
	// must roll back, and a retry after the fault must obtain the next sequence.
	if err := s.execTenant(ctx, req.TenantID, `CREATE FUNCTION chatscale_fail_revision() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'chatscale revision fault'; END$$;
 CREATE TRIGGER chatscale_fail_revision BEFORE INSERT ON chat_post_revision FOR EACH ROW EXECUTE FUNCTION chatscale_fail_revision()`); err != nil {
		t.Fatal(err)
	}
	req.ClientKey = "chatscale-revision-fault"
	if _, err := s.sendPostRaw(ctx, req); err == nil {
		t.Fatal("revision fault committed a post")
	}
	if err := s.execTenant(ctx, req.TenantID, `DROP TRIGGER chatscale_fail_revision ON chat_post_revision; DROP FUNCTION chatscale_fail_revision()`); err != nil {
		t.Fatal(err)
	}
	retry, err := s.sendPostRaw(ctx, req)
	if err != nil || retry.Sequence != 2 {
		t.Fatalf("fault retry=%+v err=%v", retry, err)
	}
}
