package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func chatmapFixture(t *testing.T) (*Adapter, chat.Post, chat.LocationShare) {
	t.Helper()
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "chatmap-room", TenantID: "chatmap-tenant", Kind: chat.PrivateChannel, Name: "Crew", OwnerID: "alice", Revision: 1}
	m := chat.Membership{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	reader := m
	reader.SubjectID, reader.Role = "bob", chat.Member
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m, reader}, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "chatmap-note"}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "Meet here"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	expiry := now.Add(time.Hour)
	v := chat.LocationShare{Version: 1, ID: "chatmap-share", TenantID: c.TenantID, ConversationID: c.ID, PostID: p.ID, PostRevision: p.Revision, SharerID: "alice", SharerTenantID: c.TenantID, ExpiresAt: &expiry,
		Place: chat.LocationPlace{Position: &chat.LocationPosition{Latitude: 42.123456, Longitude: -71.123456, Accuracy: 20}, Source: chat.LocationDevice, Label: "Crew", Address: "secret address", Precision: "approximate", ApproximateRadius: 500, CapturedAt: now}}
	return s, p, v
}
func TestTodo_CHATMAP_002_Integration(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	stored, err := s.AttachLocation(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Place.Address != "" || stored.Place.Position.Latitude == v.Place.Position.Latitude {
		t.Fatal("exact position persisted")
	}
	key := chat.LocationKey{TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: v.PostID, ID: v.ID}
	got, err := s.ReadLocation(ctx, key, time.Now())
	if err != nil || got.Ended || got.Place.Position == nil {
		t.Fatal(got, err)
	}
	post, err := s.GetPost(ctx, v.TenantID, v.ConversationID, p.ID)
	if err != nil || len(post.References) != 1 || post.References[0].Kind != chat.LocationReference {
		t.Fatal(post, err)
	}
	var raw string
	if err = s.RunTenantTx(ctx, v.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT place::text FROM chat_location_share WHERE id=$1`, v.ID).Scan(&raw)
	}); err != nil {
		t.Fatal(err)
	}
	if raw == "" {
		t.Fatal("no durable place")
	}
	if n, err := s.SweepLocations(ctx, v.TenantID, v.ExpiresAt.Add(time.Second)); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := s.SweepLocations(ctx, v.TenantID, v.ExpiresAt.Add(time.Second)); err != nil || n != 0 {
		t.Fatal("not idempotent", n, err)
	}
	got, err = s.ReadLocation(ctx, key, v.ExpiresAt.Add(time.Second))
	if err != nil || !got.Ended || got.Place.Position != nil {
		t.Fatal(got, err)
	}
	var erased bool
	if err = s.RunTenantTx(ctx, v.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT place IS NULL FROM chat_location_share WHERE id=$1`, v.ID).Scan(&erased)
	}); err != nil || !erased {
		t.Fatal("position hidden rather than erased", err)
	}
}
func TestTodo_CHATMAP_002_Security(t *testing.T) {
	s, _, v := chatmapFixture(t)
	ctx := context.Background()
	if _, err := s.AttachLocation(ctx, v); err != nil {
		t.Fatal(err)
	}
	key := chat.LocationKey{TenantID: "other", ConversationID: v.ConversationID, PostID: v.PostID, ID: v.ID}
	if _, err := s.ReadLocation(ctx, key, time.Now()); !errors.Is(err, chat.ErrNotFound) {
		t.Fatal("tenant leak", err)
	}
	var count int
	if err := s.RunTenantTx(ctx, "other", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL row_security = on"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_location_share WHERE tenant_id='other'`).Scan(&count)
	}); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var enabled, forced bool
	var using, check string
	if err := s.RunTx(ctx, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT c.relrowsecurity,c.relforcerowsecurity,pg_get_expr(p.polqual,p.polrelid),pg_get_expr(p.polwithcheck,p.polrelid) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_policy p ON p.polrelid=c.oid WHERE n.nspname=current_schema() AND c.relname='chat_location_share' AND p.polname='tenant_isolation'`).Scan(&enabled, &forced, &using, &check)
	}); err != nil || !enabled || !forced || !tenantPolicyExpr(using) || !tenantPolicyExpr(check) {
		t.Fatal("tenant policy missing", err)
	}
	v.ID = "forged"
	v.SharerID = "mallory"
	if _, err := s.AttachLocation(ctx, v); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("forged author", err)
	}
}
func TestTodo_CHATMAP_006_Integration(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	if _, err := s.AttachLocation(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceRecordHold(ctx, v.TenantID, "chatmap-hold", "matter", "reason", "admin", []string{"post:" + p.ID}); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetPost(ctx, v.TenantID, v.ConversationID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: chat.Principal{TenantID: v.TenantID, SubjectID: "alice"}, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, ExpectedRevision: current.Revision}); err != nil {
		t.Fatal(err)
	}
	key := chat.LocationKey{TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: v.PostID, ID: v.ID}
	got, err := s.ReadLocation(ctx, key, time.Now())
	if err != nil || got.Ended {
		t.Fatal("held location lost", got, err)
	}
	if n, err := s.SweepLocations(ctx, v.TenantID, v.ExpiresAt.Add(time.Second)); err != nil || n != 1 {
		t.Fatal("hold extended expiry", n, err)
	}
}
func TestTodo_CHATMAP_002_EditRemoval(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	if _, err := s.AttachLocation(ctx, v); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditPost(ctx, chat.EditPostRequest{Principal: chat.Principal{TenantID: v.TenantID, SubjectID: "alice"}, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, Body: "Changed", ExpectedRevision: p.Revision + 1, References: []chat.Reference{}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadLocation(ctx, chat.LocationKey{TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: v.PostID, ID: v.ID}, time.Now())
	if err != nil || !got.Ended || got.Place.Position != nil {
		t.Fatal("edit retained location", got, err)
	}
}
func TestTodo_CHATMAP_004_Integration(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	principal := chat.Principal{TenantID: v.TenantID, SubjectID: "alice"}
	base := chat.NewService(s, time.Now)
	base.SetAuthority(forwardingAuthority{store: s})
	sites := chat.FixtureLocationSites{AllowedSubject: "alice", AllowedTenant: v.TenantID, Sites: []chat.LocationSite{{ID: "site-1", Label: "Depot", Address: "Old address", Position: chat.LocationPosition{Latitude: 40, Longitude: 10, Accuracy: 20}}}}
	service := &chat.LocationService{Chat: base, Repo: s, Sites: sites, Lookup: chat.FixtureAddressLookup{Places: []chat.LocationPlace{{Label: "Main Street", Address: "Main Street"}}}, Now: time.Now}
	at := v.ExpiresAt.Truncate(time.Microsecond).Add(123 * time.Nanosecond)
	v.ExpiresAt = &at
	req := chat.AttachLocationRequest{Principal: principal, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, PostRevision: p.Revision, Place: chat.LocationPlace{Source: chat.LocationJobSite, SiteID: "site-1", CapturedAt: time.Now().UTC(), Precision: "exact"}, ExpiresAt: v.ExpiresAt}
	share, err := service.Attach(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if replay, e := service.Attach(ctx, req); e != nil || replay.ID != share.ID {
		t.Fatal("attach replay", e)
	}
	k := chat.LocationKey{TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, ID: share.ID}
	stored, err := s.ReadLocation(ctx, k, time.Now())
	if err != nil || stored.Place.Position != nil || stored.Place.Address != "" || stored.Place.SiteID != "site-1" {
		t.Fatal("site snapshot stored", err)
	}
	read, err := service.Read(ctx, principal, k)
	if err != nil || read.Place.Address != "Old address" {
		t.Fatal(read.Place.Address, err)
	}
	reader := chat.Principal{TenantID: v.TenantID, SubjectID: "bob"}
	if shared, e := service.Read(ctx, reader, k); e != nil || shared.Place.Address != "Old address" {
		t.Fatal("message audience cannot read shared site", e)
	}
	if choices, e := service.SearchLocationSites(ctx, reader, v.TenantID, v.ConversationID, ""); !errors.Is(e, chat.ErrPermissionDenied) || len(choices) != 0 {
		t.Fatal("sharing a site exposed the site directory", e)
	}
	messageLocations, err := service.MessageLocations(ctx, principal, v.TenantID, v.ConversationID, p.ID)
	if err != nil || len(messageLocations) != 1 || messageLocations[0].ID != share.ID {
		t.Fatal("message references", err)
	}
	sites.Sites[0].Address = "Corrected address"
	sites.Sites[0].Position.Longitude = 11
	service.Sites = sites
	read, err = service.Read(ctx, principal, k)
	if err != nil || read.Place.Address != "Corrected address" || read.Place.Position.Longitude != 11 {
		t.Fatal("site correction did not propagate", err)
	}
	results, err := s.SearchLocations(ctx, principal, service, v.TenantID, v.ConversationID, "corrected", time.Now())
	if err != nil || len(results) != 1 {
		t.Fatal("site search", len(results), err)
	}
	list, err := service.SharingNow(ctx, principal, v.TenantID, v.ConversationID)
	if err != nil || len(list) != 1 || list[0].Classification != chat.LocationClassification {
		t.Fatal("sharing now", len(list), err)
	}
	if _, err = service.SearchLocationSites(ctx, principal, v.TenantID, "wrong-room", ""); err == nil {
		t.Fatal("sites audience bypass")
	}
	places, err := service.LookupAddress(ctx, principal, v.TenantID, v.ConversationID, "main")
	if err != nil || len(places) != 1 {
		t.Fatal("lookup", len(places), err)
	}
	if err = service.End(ctx, principal, k); err != nil {
		t.Fatal(err)
	}
	read, err = service.Read(ctx, principal, k)
	if err != nil || !read.Ended || read.Place.Position != nil {
		t.Fatal("end did not erase", err)
	}
	results, err = s.SearchLocations(ctx, principal, service, v.TenantID, v.ConversationID, "corrected", time.Now())
	if err != nil || len(results) != 0 {
		t.Fatal("expired in search", len(results), err)
	}
}
func TestTodo_CHATMAP_003_Integration(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	principal := chat.Principal{TenantID: v.TenantID, SubjectID: "alice"}
	base := chat.NewService(s, time.Now)
	base.SetAuthority(forwardingAuthority{store: s})
	service := &chat.LocationService{Chat: base, Repo: s, Now: time.Now}
	share, err := service.Attach(ctx, chat.AttachLocationRequest{Principal: principal, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, PostRevision: p.Revision, Place: v.Place, ExpiresAt: v.ExpiresAt})
	if err != nil {
		t.Fatal(err)
	}
	picture := chat.LocationPictureService{Locations: service, Renderer: chat.SchematicMap{}}
	k := chat.LocationKey{TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, ID: share.ID}
	image, err := picture.Picture(ctx, principal, k, 12, chat.MapSize{Width: 400, Height: 200}, chat.MapTheme{})
	if err != nil || len(image.Image) > 20000 || image.Attribution == "" {
		t.Fatal(len(image.Image), err)
	}
	principal.SubjectID = "mallory"
	if _, err = picture.Picture(ctx, principal, k, 12, chat.MapSize{Width: 400, Height: 200}, chat.MapTheme{}); err == nil {
		t.Fatal("map reader bypass")
	}
}
func TestTodo_CHATMAP_002_Erasure(t *testing.T) {
	s, p, v := chatmapFixture(t)
	ctx := context.Background()
	if _, err := s.AttachLocation(ctx, v); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: chat.Principal{TenantID: v.TenantID, SubjectID: "alice"}, TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, ExpectedRevision: p.Revision + 1}); err != nil {
		t.Fatal(err)
	}
	key := chat.LocationKey{TenantID: v.TenantID, ConversationID: v.ConversationID, PostID: p.ID, ID: v.ID}
	read, err := s.ReadLocation(ctx, key, time.Now())
	if err != nil || !read.Ended || read.Place.Position != nil {
		t.Fatal("unheld removal retained position", err)
	}
	if err = s.RunTenantTx(ctx, v.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, v.TenantID, v.ConversationID, p.ID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadLocation(ctx, key, time.Now()); err != chat.ErrNotFound {
		t.Fatal("erasure left a location record", err)
	}
}
