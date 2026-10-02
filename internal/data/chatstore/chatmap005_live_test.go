package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATMAP_005_Integration(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	alice := chat.Principal{TenantID: v.TenantID, SubjectID: "alice"}
	bob := chat.Principal{TenantID: v.TenantID, SubjectID: "bob"}
	base := chat.NewService(s, func() time.Time { return clock })
	base.SetAuthority(forwardingAuthority{store: s})
	service := &chat.LocationService{Chat: base, Repo: s, Now: func() time.Time { return clock }}
	live := chat.LiveLocationService{Locations: service}
	expires := clock.Add(15 * time.Minute)
	place := v.Place
	place.CapturedAt = clock
	share, err := service.Attach(ctx, chat.AttachLocationRequest{Principal: alice, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, PostRevision: p.Revision, Place: place, ExpiresAt: &expires, Live: true, LiveInterval: 10 * time.Second})
	if err != nil || !share.Live {
		t.Fatal(share, err)
	}
	k := chat.LocationKey{TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, ID: share.ID}

	// A reader in the channel sees the sharer on the crew map, and the sharer's
	// own list shows it; an outsider sees nothing.
	view, err := live.LiveMap(ctx, bob, v.TenantID, v.ConversationID)
	if err != nil || len(view.Shares) != 1 || view.Shares[0].PostID != p.ID {
		t.Fatal("crew map", view, err)
	}
	if _, err = live.LiveMap(ctx, chat.Principal{TenantID: v.TenantID, SubjectID: "mallory"}, v.TenantID, v.ConversationID); err == nil {
		t.Fatal("outsider read the crew map")
	}
	if mine, e := live.MyShares(ctx, alice); e != nil || len(mine) != 1 {
		t.Fatal("my shares", len(mine), e)
	}
	if mine, e := live.MyShares(ctx, bob); e != nil || len(mine) != 0 {
		t.Fatal("another person's shares listed", len(mine), e)
	}

	// Updates replace the position: one row, one position, no trail.
	for i := 0; i < 3; i++ {
		clock = clock.Add(20 * time.Second)
		moved := place
		moved.CapturedAt = clock
		moved.Position = &chat.LocationPosition{Latitude: 42.2 + float64(i)/100, Longitude: -71.1, Accuracy: 20}
		if _, err = live.Update(ctx, alice, k, moved); err != nil {
			t.Fatal(err)
		}
	}
	var rows, tables int
	if err = s.RunTenantTx(ctx, v.TenantID, func(tx dbport.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM chat_location_share WHERE id=$1`, share.ID).Scan(&rows); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND (table_name LIKE '%location%' OR table_name LIKE '%position%' OR table_name LIKE '%route%' OR table_name LIKE '%trail%')`).Scan(&tables)
	}); err != nil || rows != 1 || tables != 3 {
		t.Fatal("a trail table or extra row exists", rows, tables, err)
	}
	got, err := service.Read(ctx, alice, k)
	if err != nil || got.Place.Position == nil || got.Place.Position.Latitude < 42.2 || got.Paused {
		t.Fatal("latest position", got, err)
	}

	// The server's clock ends it: after the end time an update is refused and
	// the position is gone from the row, with when and why recorded.
	clock = expires.Add(time.Second)
	moved := place
	moved.CapturedAt = clock
	if _, err = live.Update(ctx, alice, k, moved); !errors.Is(err, chat.ErrNotFound) {
		t.Fatal("update after expiry", err)
	}
	var erased bool
	var reason string
	var endedAt *time.Time
	if err = s.RunTenantTx(ctx, v.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT place IS NULL,ended_reason,ended_at FROM chat_location_share WHERE id=$1`, share.ID).Scan(&erased, &reason, &endedAt)
	}); err != nil || !erased || reason != "expired" || endedAt == nil || !endedAt.Equal(expires.Truncate(time.Microsecond)) {
		t.Fatal("expiry did not erase and record", erased, reason, endedAt, err)
	}
}

func TestTodo_CHATMAP_005_Leave(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	alice := chat.Principal{TenantID: v.TenantID, SubjectID: "alice"}
	base := chat.NewService(s, func() time.Time { return clock })
	base.SetAuthority(forwardingAuthority{store: s})
	service := &chat.LocationService{Chat: base, Repo: s, Now: func() time.Time { return clock }}
	live := chat.LiveLocationService{Locations: service}
	expires := clock.Add(time.Hour)
	place := v.Place
	place.CapturedAt = clock
	share, err := service.Attach(ctx, chat.AttachLocationRequest{Principal: alice, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, PostRevision: p.Revision, Place: place, ExpiresAt: &expires, Live: true})
	if err != nil {
		t.Fatal(err)
	}
	k := chat.LocationKey{TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, ID: share.ID}
	// Signing out ends every live share of the caller.
	if n, e := live.EndMine(ctx, alice, "", chat.LiveEndedSignOut); e != nil || n != 1 {
		t.Fatal("sign-out", n, e)
	}
	if got, _ := service.Read(ctx, alice, k); !got.Ended || got.EndedReason != chat.LiveEndedSignOut || got.EndedAt == nil || got.Place.Position != nil {
		t.Fatal("sign-out left a position or no reason", got)
	}
	// Leaving the conversation ends a share started again afterwards. A second
	// message carries it, since one message holds one location.
	next, err := s.SendPost(ctx, chat.SendPostRequest{Principal: alice, TenantID: v.TenantID, ConversationID: v.ConversationID, IdempotencyKey: "chatmap-live-2"}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: v.TenantID, Body: "Again"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.Attach(ctx, chat.AttachLocationRequest{Principal: alice, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: next.ID, PostRevision: next.Revision, Place: place, ExpiresAt: &expires, Live: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RunTenantTx(ctx, v.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND member_id='alice'`, v.TenantID, v.ConversationID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadLocation(ctx, chat.LocationKey{TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: next.ID, ID: again.ID}, clock)
	if err != nil || !read.Ended || read.EndedReason != chat.LiveEndedLeft || read.Place.Position != nil {
		t.Fatal("leaving did not end the share", read, err)
	}
}

// TestTodo_CHATMAP_006_Settings stores the administrator's choices and applies
// them to the same service that serves the composer.
func TestTodo_CHATMAP_006_Settings(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	alice := chat.Principal{TenantID: v.TenantID, SubjectID: "alice"}
	bob := chat.Principal{TenantID: v.TenantID, SubjectID: "bob"}
	base := chat.NewService(s, time.Now)
	base.SetAuthority(forwardingAuthority{store: s})
	service := &chat.LocationService{Chat: base, Repo: s, Now: time.Now}
	live := chat.LiveLocationService{Locations: service}
	// The channel's owner administers it; a member does not.
	narrow := chat.DefaultLocationPolicy()
	narrow.LiveEnabled = false
	narrow.MaxRetention = 30 * time.Minute
	if err := live.SetPolicy(ctx, bob, v.TenantID, v.ConversationID, narrow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("member changed the channel settings", err)
	}
	if err := live.SetPolicy(ctx, alice, v.TenantID, v.ConversationID, narrow); err != nil {
		t.Fatal(err)
	}
	if err := live.SetPolicy(ctx, alice, v.TenantID, "", narrow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("workspace settings changed without an administrator port", err)
	}
	got, err := live.Policy(ctx, bob, v.TenantID, v.ConversationID)
	if err != nil || got.LiveEnabled || got.MaxRetention != 30*time.Minute {
		t.Fatal("stored channel policy", got, err)
	}
	expires := time.Now().Add(time.Hour)
	if _, err = service.Attach(ctx, chat.AttachLocationRequest{Principal: alice, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, PostRevision: p.Revision, Place: v.Place, ExpiresAt: &expires, Live: true}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("live share accepted in a channel that switched it off", err)
	}
	// A static share is shortened to the retention limit.
	static, err := service.Attach(ctx, chat.AttachLocationRequest{Principal: alice, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, PostRevision: p.Revision, Place: v.Place})
	if err != nil || static.ExpiresAt == nil || time.Until(*static.ExpiresAt) > 31*time.Minute {
		t.Fatal("retention limit not applied", static.ExpiresAt, err)
	}
	// A country that has not agreed to sharing stays off, with the basis kept.
	if err = s.WriteLocationJurisdiction(ctx, v.TenantID, chat.LocationJurisdiction{Country: "*", Enabled: false, Basis: "agreement pending"}, "admin"); err != nil {
		t.Fatal(err)
	}
	j, found, err := s.ReadLocationJurisdiction(ctx, v.TenantID, "*")
	if err != nil || !found || j.Enabled || j.Basis != "agreement pending" {
		t.Fatal("jurisdiction", j, found, err)
	}
	effective, err := live.Policy(ctx, bob, v.TenantID, v.ConversationID)
	if err != nil || effective.SharingEnabled {
		t.Fatal("jurisdiction rule not applied", effective, err)
	}
}
