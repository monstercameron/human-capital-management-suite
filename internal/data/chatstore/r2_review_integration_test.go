package chatstore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// The outbox keeps events for posts the store no longer has (a purge, a reset of
// the demo data). One such row used to fail every read of the page it was on, so
// the watch of that conversation ended in an error at the same row on every
// reconnect and nothing after it was ever delivered. An event about a post that
// is gone has nothing to show and is skipped; the events around it still arrive.
func TestR2_WatchSkipsAnEventAboutAPurgedPost(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "c-1", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", Name: "general", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "u-1", Role: "manager", State: "active"}, {MemberID: "u-2", Role: "member", State: "active"}})
	moderated := NewModeratedAdapter(NewAdapter(s))
	ctx := context.Background()

	envelope := func(sequence int64, target string, value any) []byte {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(OutboxEnvelope{ConversationID: "c-1", TargetID: target, EventSequence: sequence, SchemaVersion: OutboxSchemaVersion, Value: raw})
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	reaction := chat.Reaction{TenantID: "tenant-a", ConversationID: "c-1", PostID: "p-gone", HomeTenantID: "tenant-a", SubjectID: "u-1", Emoji: "👍"}
	member := chat.Membership{TenantID: "tenant-a", ConversationID: "c-1", HomeTenantID: "tenant-a", SubjectID: "u-3", Role: chat.Member}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'reaction.added',$3::jsonb)`, "tenant-a", "c-1", string(envelope(1, "p-gone", reaction))); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'membership.added',$3::jsonb)`, "tenant-a", "c-1", string(envelope(2, "tenant-a:u-3", member)))
		return err
	}); err != nil {
		t.Fatal(err)
	}

	watcher := chat.WatchConversationRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "u-2"}, TenantID: "tenant-a", ConversationID: "c-1"}
	page, err := moderated.ReadConversationEvents(ctx, watcher, 0, 100)
	if err != nil {
		t.Fatalf("a reaction on a purged post failed the whole page: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].Event.Kind != chat.MembershipChanged || page.Events[0].Event.Sequence != 2 {
		t.Fatalf("events after the purged post's reaction: %+v, want the one membership event", page.Events)
	}
	if page.NextOffset == 0 {
		t.Fatal("the page did not move the watermark past the skipped row")
	}
}
