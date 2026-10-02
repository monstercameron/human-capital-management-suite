package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// A message that carries a location is special-category data to every reader of
// the disclosure class, even when its words were classified as ordinary and the
// location was attached afterwards.
func TestTodo_CHATMAP_006_DLPClass(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	digest := publicChatBodyDigest(p.Body)
	if err := s.Store.PutPublicChatPostClassification(ctx, v.TenantID, v.ConversationID, p.ID, digest, dlp.ClassInternal); err != nil {
		t.Fatal(err)
	}
	class, err := s.Store.ReadableChatDisclosureClass(ctx, v.TenantID, v.TenantID, "alice", v.ConversationID, p.ID, digest)
	if err != nil || class != dlp.ClassInternal {
		t.Fatalf("plain message class=%s err=%v", class, err)
	}
	if _, err = s.AttachLocation(ctx, v); err != nil {
		t.Fatal(err)
	}
	class, err = s.Store.ReadableChatDisclosureClass(ctx, v.TenantID, v.TenantID, "alice", v.ConversationID, p.ID, digest)
	if err != nil || class != dlp.ClassSpecialCategory || dlp.DataClass(chat.LocationDLPClass) != dlp.ClassSpecialCategory {
		t.Fatalf("message with a location class=%s err=%v", class, err)
	}
	// Ending the share does not lower the class of the message that carried it.
	if _, err = s.Store.SweepLocations(ctx, v.TenantID, v.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if class, err = s.Store.ReadableChatDisclosureClass(ctx, v.TenantID, v.TenantID, "alice", v.ConversationID, p.ID, digest); err != nil || class != dlp.ClassSpecialCategory {
		t.Fatalf("class fell after expiry: %s %v", class, err)
	}
}

func TestTodo_CHATMAP_006_JurisdictionList(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	tenant := "chatmap-jur-tenant"
	for _, j := range []chat.LocationJurisdiction{{Country: "DE", Enabled: false, Basis: "works council agreement pending"}, {Country: "*", Enabled: true, Basis: "default"}, {Country: "FR", Enabled: true, Basis: "agreement signed"}} {
		if err := s.Store.WriteLocationJurisdiction(ctx, tenant, j, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Store.ListLocationJurisdictions(ctx, tenant)
	if err != nil || len(rows) != 3 || rows[0].Country != "*" || rows[1].Country != "DE" || rows[1].Enabled || rows[1].Basis == "" || rows[2].Country != "FR" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if other, err := s.Store.ListLocationJurisdictions(ctx, "another-tenant"); err != nil || len(other) != 0 {
		t.Fatalf("another workspace's rows listed: %+v %v", other, err)
	}
}
