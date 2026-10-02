package chat

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func chatmapPlace(now time.Time) LocationPlace {
	return LocationPlace{Position: &LocationPosition{Latitude: 42.123456, Longitude: -71.123456, Accuracy: 20}, Source: LocationDevice, CapturedAt: now, Precision: "exact", Label: "Crew"}
}
func TestTodo_CHATMAP_002(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	p := chatmapPlace(now)
	v, err := PrepareLocation(p, now)
	if err != nil || v.Position.Latitude != p.Position.Latitude {
		t.Fatal(v, err)
	}
	for _, change := range []func(*LocationPlace){
		func(p *LocationPlace) { p.Position.Latitude = 91 },
		func(p *LocationPlace) { p.Position.Accuracy = 0 },
		func(p *LocationPlace) { p.Position.Longitude = math.NaN() },
		func(p *LocationPlace) { p.CapturedAt = now.Add(-6 * time.Minute) },
		func(p *LocationPlace) { p.Source = "unknown" },
		func(p *LocationPlace) { p.Precision = "unknown" },
	} {
		p := chatmapPlace(now)
		change(&p)
		if _, err := PrepareLocation(p, now); !errors.Is(err, ErrInvalidArgument) {
			t.Fatal("invalid accepted", err)
		}
	}
	_, err = (UnavailableAddressLookup{}).LookupAddress(context.Background(), Principal{}, "secret")
	if !errors.Is(err, ErrLocationUnavailable) {
		t.Fatal(err)
	}
	f := FixtureAddressLookup{Places: []LocationPlace{{Address: "Main Street"}}}
	xs, err := f.LookupAddress(context.Background(), Principal{}, "main")
	if err != nil || len(xs) != 1 {
		t.Fatal(xs, err)
	}
}
func TestTodo_CHATMAP_002_Property(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 1000; i++ {
		p := chatmapPlace(now)
		p.Precision = "approximate"
		p.ApproximateRadius = 500
		p.Address = "Apartment 5"
		p.Position.Latitude += float64(i) / 1e7
		v, err := PrepareLocation(p, now)
		if err != nil {
			t.Fatal(err)
		}
		if v.Position == p.Position || v.Position.Accuracy < 500 || v.Label != "" || v.Address != "" || v.SiteID != "" {
			t.Fatalf("precision leak %+v", v)
		}
		again, err := PrepareLocation(v, now)
		if err != nil || *again.Position != *v.Position {
			t.Fatal("coarsening not idempotent", err)
		}
	}
}

type chatmapRepo struct {
	v      LocationShare
	writes int
}

func (f *chatmapRepo) AttachLocation(_ context.Context, v LocationShare) (LocationShare, error) {
	f.v = v
	f.writes++
	return v, nil
}
func (f *chatmapRepo) ReadLocation(_ context.Context, k LocationKey, now time.Time) (LocationShare, error) {
	if f.v.ID != k.ID {
		return LocationShare{}, ErrNotFound
	}
	if f.v.ExpiresAt != nil && !now.Before(*f.v.ExpiresAt) {
		f.v.Place = LocationPlace{}
		f.v.Ended = true
	}
	return f.v, nil
}
func (f *chatmapRepo) EndLocation(context.Context, LocationKey) error {
	f.v.Place = LocationPlace{}
	f.v.Ended = true
	f.writes++
	return nil
}
func (f *chatmapRepo) SweepLocations(context.Context, string, time.Time) (int64, error) {
	return 0, nil
}
func TestTodo_CHATMAP_002_Security(t *testing.T) {
	now := time.Now().UTC()
	p := Principal{TenantID: "tt", SubjectID: "alice"}
	store := &fakeStore{conversation: Conversation{TenantID: "tt", ID: "c", Kind: Group, Revision: 1}, membership: Membership{TenantID: "tt", ConversationID: "c", SubjectID: "alice", HomeTenantID: "tt", HistoryVisibility: FullHistory}, post: Post{ID: "m", TenantID: "tt", ConversationID: "c", AuthorID: "alice", Revision: 1}}
	repo := &chatmapRepo{}
	svc := &LocationService{Chat: newTestService(store, func() time.Time { return now }), Repo: repo, Now: func() time.Time { return now }}
	r := AttachLocationRequest{Principal: p, TenantID: "tt", ConversationID: "c", PostID: "m", PostRevision: 1, Place: chatmapPlace(now)}
	v, err := svc.Attach(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	store.post.References = []Reference{{Kind: LocationReference, TenantID: "tt", ID: v.ID}}
	k := LocationKey{TenantID: "tt", ConversationID: "c", PostID: "m", ID: v.ID}
	if _, err = svc.Read(context.Background(), p, k); err != nil {
		t.Fatal(err)
	}
	before := repo.writes
	r.Principal.SubjectID = "mallory"
	if _, err = svc.Attach(context.Background(), r); err == nil || repo.writes != before {
		t.Fatal("unauthorized mutation")
	}
	k.TenantID = "other"
	if _, err = svc.Read(context.Background(), p, k); err == nil {
		t.Fatal("cross tenant read")
	}
	k.TenantID = "tt"
	if err = svc.End(context.Background(), p, k); err != nil || repo.v.Place.Position != nil {
		t.Fatal(err)
	}
}

type chatmapGrant bool

func (g chatmapGrant) AllowsLocation(context.Context, Principal, string, string) bool { return bool(g) }
func TestTodo_CHATMAP_006_Security(t *testing.T) {
	now := time.Now().UTC()
	p := Principal{TenantID: "tt", SubjectID: "alice"}
	store := &fakeStore{conversation: Conversation{TenantID: "tt", ID: "c", Kind: Group, Revision: 1}, membership: Membership{TenantID: "tt", ConversationID: "c", SubjectID: "alice", HomeTenantID: "tt", HistoryVisibility: FullHistory}, post: Post{ID: "m", TenantID: "tt", ConversationID: "c", AuthorID: "alice", Revision: 1}}
	repo := &chatmapRepo{}
	svc := &LocationService{Chat: newTestService(store, func() time.Time { return now }), Repo: repo, Now: func() time.Time { return now }}
	actor, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tt"), Subject: "alice", SubjectKind: trust.SubjectKindAgent, OrganizationScopeID: "org", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "fixture", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:fixture"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), actor)
	request := AttachLocationRequest{Principal: p, TenantID: "tt", ConversationID: "c", PostID: "m", PostRevision: 1, Place: chatmapPlace(now)}
	if _, err = svc.Attach(ctx, request); err != ErrPermissionDenied || repo.writes != 0 {
		t.Fatal("ungranted agent", err)
	}
	svc.Grant = chatmapGrant(true)
	share, err := svc.Attach(ctx, request)
	if err != nil || share.Place.Source != LocationAgent || share.SharerKind != "agent" || share.Classification != LocationClassification {
		t.Fatal("agent attribution", err)
	}
	store.post.References = []Reference{{Kind: LocationReference, TenantID: "tt", ID: share.ID}}
	svc.Grant = chatmapGrant(false)
	if _, err = svc.Read(ctx, p, LocationKey{TenantID: "tt", ConversationID: "c", PostID: "m", ID: share.ID}); err != ErrPermissionDenied {
		t.Fatal("revoked skill read", err)
	}
	allowed := map[string]bool{"Attach": true, "Read": true, "End": true, "SharingNow": true, "SearchLocationSites": true, "LookupAddress": true, "MessageLocations": true}
	methods := reflect.TypeOf(svc)
	for i := 0; i < methods.NumMethod(); i++ {
		if !allowed[methods.Method(i).Name] {
			t.Fatal("unexpected cross-conversation location interface", methods.Method(i).Name)
		}
	}
}

type chatmapLookupUsage struct {
	operation, digest string
	calls             int
	err               error
}

func (u *chatmapLookupUsage) RecordLocationUsage(_ context.Context, tenant, subject, operation, digest string) error {
	u.calls++
	u.operation, u.digest = operation, digest
	return u.err
}
func TestTodo_CHATMAP_006_Usage(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{conversation: Conversation{TenantID: "tt", ID: "c", Kind: Group, Revision: 1}, membership: Membership{TenantID: "tt", ConversationID: "c", SubjectID: "alice", HomeTenantID: "tt", HistoryVisibility: FullHistory}}
	usage := &chatmapLookupUsage{}
	svc := &LocationService{Chat: newTestService(store, func() time.Time { return now }), Lookup: FixtureAddressLookup{Places: []LocationPlace{{Address: "Private street"}}}, Usage: usage}
	principal := Principal{TenantID: "tt", SubjectID: "alice"}
	if places, err := svc.LookupAddress(context.Background(), principal, "tt", "c", "Private street"); err != nil || len(places) != 1 {
		t.Fatal("lookup", err)
	}
	if usage.calls != 1 || usage.operation != "address.lookup" || len(usage.digest) != 64 || strings.Contains(usage.digest, "Private") {
		t.Fatal("personal data in usage", usage)
	}
	if _, err := svc.LookupAddress(context.Background(), principal, "other", "c", "Private street"); err == nil || usage.calls != 1 {
		t.Fatal("unauthorized lookup reached ledger")
	}
	usage.err = ErrUnavailable
	if _, err := svc.LookupAddress(context.Background(), principal, "tt", "c", "Private street"); !errors.Is(err, ErrLocationUnavailable) {
		t.Fatal("usage failure", err)
	}
	svc.Usage = nil
	if _, err := svc.LookupAddress(context.Background(), principal, "tt", "c", "Private street"); err != nil {
		t.Fatal("in-process usage", err)
	}
}
