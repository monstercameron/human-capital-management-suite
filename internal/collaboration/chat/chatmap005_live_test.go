package chat

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"
)

// liveRepo is an in-memory repository with live, policy and search support. It
// holds exactly one position per share, as the real store does.
type liveRepo struct {
	chatmapRepo
	policy  LocationPolicy
	channel *LocationPolicy
	jur     map[string]LocationJurisdiction
	admin   bool
	updates int
}

func (f *liveRepo) UpdateLivePosition(_ context.Context, k LocationKey, place LocationPlace, now time.Time) error {
	if f.v.ID != k.ID || f.v.Ended || (f.v.ExpiresAt != nil && !now.Before(*f.v.ExpiresAt)) {
		return ErrNotFound
	}
	f.v.Place, f.v.PositionAt = place, &now
	f.updates++
	return nil
}
func (f *liveRepo) EndLocationReason(_ context.Context, _ LocationKey, reason string) error {
	f.v.Place, f.v.Ended, f.v.EndedReason = LocationPlace{}, true, reason
	return nil
}
func (f *liveRepo) EndLiveBySharer(_ context.Context, _, _, subject, _, reason string) (int64, error) {
	if f.v.SharerID != subject || f.v.Ended {
		return 0, nil
	}
	f.v.Place, f.v.Ended, f.v.EndedReason = LocationPlace{}, true, reason
	return 1, nil
}
func (f *liveRepo) SharerLocationKeys(_ context.Context, tenant, subject, _ string) ([]LocationKey, error) {
	if f.v.SharerID != subject {
		return nil, nil
	}
	return []LocationKey{{TenantID: tenant, ConversationID: f.v.ConversationID, PostID: f.v.PostID, ID: f.v.ID}}, nil
}
func (f *liveRepo) SearchLocations(ctx context.Context, p Principal, s *LocationService, tenant, conversation, _ string, _ time.Time) ([]LocationShare, error) {
	if f.v.ID == "" {
		return nil, nil
	}
	v, err := s.Read(ctx, p, LocationKey{TenantID: tenant, ConversationID: conversation, PostID: f.v.PostID, ID: f.v.ID})
	if err != nil {
		return nil, err
	}
	return []LocationShare{v}, nil
}
func (f *liveRepo) ReadLocationPolicy(context.Context, string, string) (LocationPolicy, *LocationPolicy, error) {
	return f.policy, f.channel, nil
}
func (f *liveRepo) WriteLocationPolicy(_ context.Context, _, conversation string, p LocationPolicy, _ string) error {
	if conversation == "" {
		f.policy = p
	} else {
		f.channel = &p
	}
	return nil
}
func (f *liveRepo) ReadLocationJurisdiction(_ context.Context, _, country string) (LocationJurisdiction, bool, error) {
	j, ok := f.jur[country]
	return j, ok, nil
}
func (f *liveRepo) WriteLocationJurisdiction(_ context.Context, _ string, j LocationJurisdiction, _ string) error {
	if f.jur == nil {
		f.jur = map[string]LocationJurisdiction{}
	}
	f.jur[j.Country] = j
	return nil
}
func (f *liveRepo) IsLocationAdmin(context.Context, Principal, string, string) bool { return f.admin }

type liveFixture struct {
	svc   *LocationService
	live  LiveLocationService
	repo  *liveRepo
	store *fakeStore
	now   time.Time
	alice Principal
}

func newLiveFixture() *liveFixture {
	f := &liveFixture{now: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), alice: Principal{TenantID: "tt", SubjectID: "alice"}}
	f.store = &fakeStore{conversation: Conversation{TenantID: "tt", ID: "c", Kind: Group, Revision: 1}, membership: Membership{TenantID: "tt", ConversationID: "c", SubjectID: "alice", HomeTenantID: "tt", HistoryVisibility: FullHistory}, post: Post{ID: "m", TenantID: "tt", ConversationID: "c", AuthorID: "alice", Revision: 1}}
	f.repo = &liveRepo{policy: DefaultLocationPolicy()}
	f.svc = &LocationService{Chat: newTestService(f.store, func() time.Time { return f.now }), Repo: f.repo, Now: func() time.Time { return f.now }}
	f.live = LiveLocationService{Locations: f.svc}
	return f
}
func (f *liveFixture) place(lat float64) LocationPlace {
	p := chatmapPlace(f.now)
	p.Position.Latitude = lat
	p.Precision = "exact"
	return p
}
func (f *liveFixture) start(d time.Duration) (LocationShare, LocationKey, error) {
	expires := f.now.Add(d)
	v, err := f.svc.Attach(context.Background(), AttachLocationRequest{Principal: f.alice, TenantID: "tt", ConversationID: "c", PostID: "m", PostRevision: 1, Place: f.place(42.1), ExpiresAt: &expires, Live: true, LiveInterval: 10 * time.Second})
	if err != nil {
		return v, LocationKey{}, err
	}
	f.store.post.References = []Reference{{Kind: LocationReference, TenantID: "tt", ID: v.ID}}
	return v, LocationKey{TenantID: "tt", ConversationID: "c", PostID: "m", ID: v.ID}, nil
}

func TestTodo_CHATMAP_005(t *testing.T) {
	f := newLiveFixture()
	ctx := context.Background()
	v, k, err := f.start(15 * time.Minute)
	if err != nil || !v.Live || v.LiveInterval != 10*time.Second {
		t.Fatal("start", v, err)
	}
	f.now = f.now.Add(20 * time.Second)
	moved, err := f.live.Update(ctx, f.alice, k, f.place(42.2))
	if err != nil || moved.Place.Position.Latitude != 42.2 {
		t.Fatal("update", err)
	}
	got, err := f.svc.Read(ctx, f.alice, k)
	if err != nil || got.Place.Position.Latitude != 42.2 || got.Paused {
		t.Fatal("latest position", got, err)
	}
	f.now = f.now.Add(5 * time.Minute)
	if got, _ = f.svc.Read(ctx, f.alice, k); !got.Paused {
		t.Fatal("a closed page is shown as paused")
	}
	view, err := f.live.LiveMap(ctx, f.alice, "tt", "c")
	if err != nil || len(view.Shares) != 1 {
		t.Fatal("crew map", view, err)
	}
	mine, err := f.live.MyShares(ctx, f.alice)
	if err != nil || len(mine) != 1 {
		t.Fatal("my shares", mine, err)
	}
	if err = f.live.Stop(ctx, f.alice, k); err != nil {
		t.Fatal(err)
	}
	got, err = f.svc.Read(ctx, f.alice, k)
	if err != nil || !got.Ended || got.Place.Position != nil || got.EndedReason != LiveEndedStopped {
		t.Fatal("stopped share still has a position or no reason", got, err)
	}
	if view, _ = f.live.LiveMap(ctx, f.alice, "tt", "c"); len(view.Shares) != 0 {
		t.Fatal("ended share on the map")
	}
	if _, err = f.live.Update(ctx, f.alice, k, f.place(42.3)); !errors.Is(err, ErrNotFound) {
		t.Fatal("update after stop", err)
	}
}

// TestTodo_CHATMAP_005_Property drives random sequences of start, update, stop,
// expiry and disconnect and checks the two promises: no position is readable
// once a share has ended, and a reader outside the conversation never gets one.
func TestTodo_CHATMAP_005_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	outsider := Principal{TenantID: "other", SubjectID: "mallory"}
	for round := 0; round < 200; round++ {
		f := newLiveFixture()
		ctx := context.Background()
		_, k, err := f.start(time.Duration(1+rng.Intn(20)) * time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		ended := false
		expires := *f.repo.v.ExpiresAt
		for step := 0; step < 30; step++ {
			switch rng.Intn(5) {
			case 0:
				f.now = f.now.Add(time.Duration(rng.Intn(120)) * time.Second)
				_, err = f.live.Update(ctx, f.alice, k, f.place(42+rng.Float64()))
			case 1:
				if f.live.Stop(ctx, f.alice, k) == nil {
					ended = true
				}
			case 2:
				f.now = f.now.Add(time.Duration(rng.Intn(400)) * time.Second)
			case 3:
				_, _ = f.live.EndMine(ctx, f.alice, "c", LiveEndedSignOut)
			case 4:
				_, _ = f.live.LiveMap(ctx, outsider, "tt", "c")
			}
			if !f.now.Before(expires) {
				ended = true
			}
			got, err := f.svc.Read(ctx, f.alice, k)
			if err != nil {
				t.Fatal(err)
			}
			if f.repo.v.Ended {
				ended = true
			}
			if ended && (!got.Ended || got.Place.Position != nil) {
				t.Fatalf("position readable after the share ended: round %d step %d", round, step)
			}
			if !ended && got.Ended {
				t.Fatalf("share ended early: round %d step %d", round, step)
			}
			if got, err := f.svc.Read(ctx, outsider, k); err == nil || got.Place.Position != nil {
				t.Fatal("reader outside the conversation received a position")
			}
			if view, err := f.live.LiveMap(ctx, outsider, "tt", "c"); err == nil && len(view.Shares) > 0 {
				t.Fatal("outsider saw the crew map")
			}
			if f.repo.v.ExpiresAt == nil || !f.repo.v.ExpiresAt.Equal(expires) {
				t.Fatal("an update moved the end time")
			}
		}
	}
}

func TestTodo_CHATMAP_005_Fault(t *testing.T) {
	f := newLiveFixture()
	ctx := context.Background()
	_, k, err := f.start(15 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// The device's clock is wrong: a capture time that is not near the server's
	// now is refused, so a device cannot keep a share alive or backdate it.
	late := f.place(42.4)
	late.CapturedAt = f.now.Add(time.Hour)
	f.now = f.now.Add(time.Minute)
	if _, err = f.live.Update(ctx, f.alice, k, late); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("device clock accepted", err)
	}
	// The connection is lost and the share runs out: the server's clock ends it.
	f.now = f.now.Add(16 * time.Minute)
	if _, err = f.live.Update(ctx, f.alice, k, f.place(42.5)); !errors.Is(err, ErrNotFound) {
		t.Fatal("update after the end time", err)
	}
	got, err := f.svc.Read(ctx, f.alice, k)
	if err != nil || !got.Ended || got.Place.Position != nil {
		t.Fatal("a lost connection extended the share", got, err)
	}
	// A repository without live support reports it instead of pretending.
	bare := &LocationService{Chat: f.svc.Chat, Repo: &chatmapRepo{}, Now: f.svc.Now}
	if _, err = (LiveLocationService{Locations: bare}).Update(ctx, f.alice, k, f.place(42)); !errors.Is(err, ErrLocationUnavailable) {
		t.Fatal(err)
	}
}

func TestTodo_CHATMAP_005_Security(t *testing.T) {
	f := newLiveFixture()
	ctx := context.Background()
	expires := f.now.Add(time.Hour)
	request := AttachLocationRequest{Principal: f.alice, TenantID: "tt", ConversationID: "c", PostID: "m", PostRevision: 1, Place: f.place(42.1), ExpiresAt: &expires, Live: true}
	open := request
	open.ExpiresAt = nil
	if _, err := f.svc.Attach(ctx, open); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("open-ended live share accepted", err)
	}
	long := request
	tooLong := f.now.Add(9 * time.Hour)
	long.ExpiresAt = &tooLong
	if _, err := f.svc.Attach(ctx, long); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("share longer than the administrator's maximum accepted", err)
	}
	siteLive := request
	siteLive.Place = LocationPlace{Source: LocationJobSite, SiteID: "site-1", CapturedAt: f.now, Precision: "exact"}
	if _, err := f.svc.Attach(ctx, siteLive); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("a live share from a job site accepted", err)
	}
	f.repo.policy.LiveEnabled = false
	if _, err := f.svc.Attach(ctx, request); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("live sharing switched off but accepted", err)
	}
	f.repo.policy = DefaultLocationPolicy()
	f.repo.channel = &LocationPolicy{SharingEnabled: true, LiveEnabled: true, ExactAllowed: false, MaxLive: time.Hour, MaxRetention: time.Hour}
	if _, err := f.svc.Attach(ctx, request); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("exact position accepted where the channel forbids it", err)
	}
	request.Place.Precision, request.Place.ApproximateRadius = "approximate", 500
	_, k, err := func() (LocationShare, LocationKey, error) {
		v, err := f.svc.Attach(ctx, request)
		f.store.post.References = []Reference{{Kind: LocationReference, TenantID: "tt", ID: v.ID}}
		return v, LocationKey{TenantID: "tt", ConversationID: "c", PostID: "m", ID: v.ID}, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	// A device cannot turn an approximate share into an exact one.
	f.now = f.now.Add(time.Minute)
	exact := f.place(42.123456)
	exact.Address = "7 Secret Lane"
	if _, err = f.live.Update(ctx, f.alice, k, exact); err != nil {
		t.Fatal(err)
	}
	if got := f.repo.v.Place; got.Position.Latitude == 42.123456 || got.Address != "" || got.Precision != "approximate" {
		t.Fatal("approximate share refined by an update", got)
	}
	// Only the sharer updates or stops.
	other := Principal{TenantID: "tt", SubjectID: "bob"}
	if _, err = f.live.Update(ctx, other, k, f.place(42)); err == nil {
		t.Fatal("another person moved the pin")
	}
	if err = f.live.Stop(ctx, other, k); err == nil {
		t.Fatal("another person stopped the share")
	}
	// An agent's position cannot be supplied as a device position.
	agent := f.place(42)
	agent.Source = LocationAgent
	if _, err = f.live.Update(ctx, f.alice, k, agent); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	// Settings: only an administrator, and only inside the limits.
	pol := DefaultLocationPolicy()
	if err = f.live.SetPolicy(ctx, f.alice, "tt", "", pol); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("non-administrator changed the settings", err)
	}
	f.repo.admin = true
	pol.MaxRetention = 48 * time.Hour
	if err = f.live.SetPolicy(ctx, f.alice, "tt", "", pol); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("retention lengthened past the workspace maximum", err)
	}
	pol.MaxRetention = time.Hour
	if err = f.live.SetPolicy(ctx, f.alice, "tt", "c", pol); err != nil {
		t.Fatal(err)
	}
	if err = f.live.SetJurisdiction(ctx, f.alice, "tt", LocationJurisdiction{Country: "DE", Enabled: false, Basis: ""}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("a jurisdiction setting without its basis", err)
	}
}
