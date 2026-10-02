package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func chatstateChange(status chatpolicy.ChannelStatus, revision uint64) chat.ChangeChannelStatusRequest {
	return chat.ChangeChannelStatusRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "u-1"}, TenantID: "tenant-a", ConversationID: "c-1", Status: status, ExpectedRevision: revision, Reason: "Incident review"}
}
func TestTodo_CHATSTATE_001_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := t.Context()
	at := time.Now().UTC()
	current, err := s.ReadChannelStatus(ctx, "tenant-a", "c-1")
	if err != nil || current.Status != chatpolicy.StatusOpen || current.Revision != 1 {
		t.Fatalf("legacy read = %+v %v", current, err)
	}
	r := chatstateChange(chatpolicy.StatusLocked, 1)
	until := at.Add(time.Hour)
	r.Until = &until
	changed, err := s.CommitChannelStatus(ctx, r, at, func(context.Context, chat.ChannelStatus) error { return nil })
	if err != nil || changed.Revision != 2 {
		t.Fatalf("change = %+v %v", changed, err)
	}
	read, err := s.ReadChannelStatus(ctx, "tenant-a", "c-1")
	if err != nil || read.Reason != r.Reason || read.ChangedBy != "u-1" || read.Until == nil {
		t.Fatalf("metadata = %+v %v", read, err)
	}
	events, err := s.PendingOutbox(ctx, "tenant-a", 10)
	if err != nil || len(events) != 1 || events[0].EventType != "conversation.status_changed" {
		t.Fatalf("event = %+v %v", events, err)
	}
	var audit int
	if err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_audit_event WHERE tenant_id=$1 AND action='conversation.status_changed'`, "tenant-a").Scan(&audit)
	}); err != nil || audit != 1 {
		t.Fatalf("audit count = %d %v", audit, err)
	}
	if _, err = s.CommitChannelStatus(ctx, r, at, func(context.Context, chat.ChannelStatus) error { return nil }); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale change = %v", err)
	}
	n, err := s.SweepChannelStatuses(ctx, "tenant-a", until)
	if err != nil || n != 1 {
		t.Fatalf("sweep = %d %v", n, err)
	}
	read, err = s.ReadChannelStatus(ctx, "tenant-a", "c-1")
	if err != nil || read.Status != chatpolicy.StatusOpen || read.Revision != 3 || read.Until != nil {
		t.Fatalf("swept = %+v %v", read, err)
	}
	n, err = s.SweepChannelStatuses(ctx, "tenant-a", until)
	if err != nil || n != 0 {
		t.Fatalf("repeat sweep = %d %v", n, err)
	}
}

func TestTodo_CHATSTATE_001_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := t.Context()
	at := time.Now().UTC()
	if _, err := s.ReadChannelStatus(ctx, "tenant-b", "c-1"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("tenant leak = %v", err)
	}
	r := chatstateChange(chatpolicy.StatusLocked, 1)
	if _, err := s.CommitChannelStatus(ctx, r, at, func(context.Context, chat.ChannelStatus) error { return chat.ErrPermissionDenied }); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revocation = %v", err)
	}
	read, _ := s.ReadChannelStatus(ctx, "tenant-a", "c-1")
	if read.Revision != 1 {
		t.Fatal("refused change mutated")
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,sequence,body) VALUES('existing','tenant-a','c-1','u-1',1,'preserved')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitChannelStatus(ctx, r, at, nil); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("missing authority = %v", err)
	}
	if _, err := s.CommitChannelStatus(ctx, r, at, func(context.Context, chat.ChannelStatus) error { return nil }); err != nil {
		t.Fatal(err)
	}
	err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,sequence,body) VALUES('forged','tenant-a','c-1','u-1',1,'bypass')`)
		return err
	})
	if err == nil {
		t.Fatal("direct locked post succeeded")
	}
	for _, sql := range []string{`UPDATE chat_post SET body='edited' WHERE tenant_id='tenant-a' AND id='existing'`, `INSERT INTO chat_reaction(tenant_id,post_id,member_id,emoji) VALUES('tenant-a','existing','u-1','ok')`, `INSERT INTO chat_pin(tenant_id,conversation_id,post_id,member_id) VALUES('tenant-a','c-1','existing','u-1')`} {
		err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error { _, err := tx.Exec(ctx, sql); return err })
		if err == nil {
			t.Fatalf("locked mutation succeeded: %s", sql)
		}
	}
	r = chatstateChange(chatpolicy.StatusArchived, 2)
	if _, err = s.CommitChannelStatus(ctx, r, at, func(context.Context, chat.ChannelStatus) error { return nil }); !errors.Is(err, chat.ErrChannelHeld) {
		t.Fatalf("locked archive = %v", err)
	}
}

func TestTodo_CHATSTATE_001_HoldIntegration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	a := NewAdapter(s)
	ctx := t.Context()
	if err := a.PlaceRecordHold(ctx, "tenant-a", "hold", "matter", "review", "u-1", []string{"conversation:c-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitChannelStatus(ctx, chatstateChange(chatpolicy.StatusArchived, 1), time.Now().UTC(), func(context.Context, chat.ChannelStatus) error { return nil }); !errors.Is(err, chat.ErrChannelHeld) {
		t.Fatalf("held archive = %v", err)
	}
	err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, "tenant-a", "c-1")
		return err
	})
	if err == nil {
		t.Fatal("held conversation deleted")
	}
}
