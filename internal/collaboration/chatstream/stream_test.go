package chatstream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type testReader struct{ events []Event }

func (r testReader) Read(_ context.Context, q ReadRequest) (Page, error) {
	out := []Event{}
	for _, e := range r.events {
		if e.Sequence > q.AfterSequence {
			out = append(out, e)
			if len(out) == q.Limit {
				break
			}
		}
	}
	return Page{Events: out, Complete: true}, nil
}

type mutableTestReader struct {
	mu     sync.Mutex
	events []Event
}

func (r *mutableTestReader) add(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *mutableTestReader) Read(_ context.Context, q ReadRequest) (Page, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, 0, q.Limit)
	for _, event := range r.events {
		if event.Sequence <= q.AfterSequence {
			continue
		}
		if len(out) == q.Limit {
			break
		}
		out = append(out, event)
	}
	return Page{Events: out, Complete: len(out) < q.Limit}, nil
}

type watermarkReader struct{}

func (watermarkReader) Read(_ context.Context, q ReadRequest) (Page, error) {
	if q.AfterSequence < 5 {
		return Page{NextSequence: 5, Complete: false}, nil
	}
	return Page{Events: []Event{{TenantID: "tenant", ConversationID: "conversation", Sequence: 6, MembershipEpoch: 1}}, NextSequence: 6, Complete: true}, nil
}

type testAuth struct {
	mu      sync.Mutex
	revoked bool
}

type epochAuth struct{}

func (epochAuth) Authorize(_ context.Context, access Access) error {
	if access.MembershipEpoch != 2 {
		return errors.New("stale epoch")
	}
	return nil
}

func (a *testAuth) Authorize(_ context.Context, x Access) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.revoked {
		return errors.New("revoked")
	}
	if x.TenantID != "tenant" || x.SubjectID != "subject" || x.ConversationID != "conversation" {
		return errors.New("scope")
	}
	return nil
}
func newTestStream(t *testing.T, auth *testAuth, clock *time.Time, queue int, events []Event) *Stream {
	t.Helper()
	s, err := New(Config{Key: []byte("secret"), Reader: testReader{events}, Authorizer: auth, QueueSize: queue, ReplayLimit: 10, CursorTTL: time.Minute, Clock: func() time.Time { return *clock }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func req() WatchRequest {
	return WatchRequest{TenantID: "tenant", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1}
}

func TestTodo_CHAT_018(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	s := newTestStream(t, auth, &now, 1, nil)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 2, MembershipEpoch: 1}); !errors.Is(err, nil) {
		t.Fatal(err)
	}
	_, err = sub.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = sub.Next(context.Background())
	if !errors.Is(err, ErrBackpressure) {
		t.Fatalf("Next error=%v", err)
	}
}

func TestTodo_CHAT_018_Race(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	s := newTestStream(t, auth, &now, 32, nil)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := uint64(1); i <= 20; i++ {
		wg.Add(1)
		go func(n uint64) {
			defer wg.Done()
			_ = s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: n, MembershipEpoch: 1})
		}(i)
	}
	wg.Wait()
	sub.Close()
}

func TestTodo_CHAT_018_Security(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	s := newTestStream(t, auth, &now, 2, nil)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	auth.mu.Lock()
	auth.revoked = true
	auth.mu.Unlock()
	_ = s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1})
	_, err = sub.Next(context.Background())
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("error=%v", err)
	}
}

func TestTodo_CHAT_019(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	s := newTestStream(t, auth, &now, 4, []Event{{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1}, {TenantID: "tenant", ConversationID: "conversation", Sequence: 2, MembershipEpoch: 1}})
	request := req()
	request.HomeTenantID = "home"
	request.RouteEpoch = 7
	sub, err := s.Watch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	first := sub.Cursor()
	if first == "" {
		t.Fatal("missing cursor")
	}
	for name, changed := range map[string]WatchRequest{
		"tenant":       {TenantID: "other", HomeTenantID: "home", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1, RouteEpoch: 7, Cursor: first},
		"home tenant":  {TenantID: "tenant", HomeTenantID: "other", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1, RouteEpoch: 7, Cursor: first},
		"principal":    {TenantID: "tenant", HomeTenantID: "home", SubjectID: "other", ConversationID: "conversation", MembershipEpoch: 1, RouteEpoch: 7, Cursor: first},
		"conversation": {TenantID: "tenant", HomeTenantID: "home", SubjectID: "subject", ConversationID: "other", MembershipEpoch: 1, RouteEpoch: 7, Cursor: first},
		"membership":   {TenantID: "tenant", HomeTenantID: "home", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 2, RouteEpoch: 7, Cursor: first},
		"route":        {TenantID: "tenant", HomeTenantID: "home", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1, RouteEpoch: 8, Cursor: first},
	} {
		if _, err := s.Watch(context.Background(), changed); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("changed %s scope error=%v", name, err)
		}
	}
	if _, err := s.Watch(context.Background(), WatchRequest{TenantID: "tenant", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1, Cursor: first + "x"}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("tamper bytes error=%v", err)
	}
}

func TestTodo_CHAT_019_Security(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	s := newTestStream(t, auth, &now, 2, nil)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	token := sub.Cursor()
	now = now.Add(2 * time.Minute)
	if _, err := s.Watch(context.Background(), WatchRequest{TenantID: "tenant", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1, Cursor: token}); !errors.Is(err, ErrExpiredCursor) {
		t.Fatalf("expiry error=%v", err)
	}
}

func TestTodo_CHAT_019_Recovery(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	reader := &mutableTestReader{events: []Event{
		{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1},
		{TenantID: "tenant", ConversationID: "conversation", Sequence: 2, MembershipEpoch: 1},
	}}
	s, err := New(Config{Key: []byte("secret"), Reader: reader, Authorizer: auth, QueueSize: 8, ReplayLimit: 10, CursorTTL: time.Minute, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	request := req()
	request.RouteEpoch = 12
	sub, err := s.Watch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	e, err := sub.Next(context.Background())
	if err != nil || e.Sequence != 1 {
		t.Fatalf("first replay=%+v err=%v", e, err)
	}
	cursor := sub.Cursor()
	sub.Close()
	reader.add(Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 3, MembershipEpoch: 1})
	reader.add(Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 4, MembershipEpoch: 1})

	request.Cursor = cursor
	request.AfterSequence = 0
	resumed, err := s.Watch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	for _, want := range []uint64{2, 3, 4} {
		event, nextErr := resumed.Next(context.Background())
		if nextErr != nil || event.Sequence != want {
			t.Fatalf("catch-up event = %d err=%v; want exact sequence %d", event.Sequence, nextErr, want)
		}
	}
}

func TestTodo_CHAT_019_RecoveryOverReplayWindow(t *testing.T) {
	now := time.Unix(100, 0)
	reader := &mutableTestReader{}
	for sequence := uint64(1); sequence <= 12; sequence++ {
		reader.add(Event{TenantID: "tenant", ConversationID: "conversation", Sequence: sequence, MembershipEpoch: 1})
	}
	s, err := New(Config{Key: []byte("secret"), Reader: reader, Authorizer: &testAuth{}, QueueSize: 4, ReplayLimit: 10, CursorTTL: time.Minute, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	request := req()
	request.RouteEpoch = 12
	bridge := Bridge{Stream: s, Reader: reader, PollInterval: time.Millisecond, PageLimit: 2}
	sub, err := bridge.Watch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-sub.Done():
	case <-time.After(time.Second):
		sub.Close()
		t.Fatal("backlog larger than the replay window did not exercise bounded backpressure")
	}

	// Drain the accepted prefix before reconnecting. The catch-up cursor may
	// replay that accepted prefix, but must let the bounded bridge recover every
	// committed event after reconnect without a gap.
	for want := uint64(1); want <= 4; want++ {
		got, nextErr := sub.Next(context.Background())
		if nextErr != nil || got.Sequence != want {
			t.Fatalf("first subscription event=%d err=%v; want sequence %d", got.Sequence, nextErr, want)
		}
	}
	_, terminalErr := sub.Next(context.Background())
	if !errors.Is(terminalErr, ErrBackpressure) {
		t.Fatalf("terminal error=%v, want backpressure after the accepted prefix", terminalErr)
	}
	var backpressure *BackpressureError
	if !errors.As(terminalErr, &backpressure) || backpressure.Cursor == "" {
		t.Fatalf("terminal backpressure has no catch-up cursor: %v", terminalErr)
	}
	cursor := backpressure.Cursor
	decoded, err := s.h.decodeCursor(cursor)
	if err != nil || decoded.Sequence != 0 {
		t.Fatalf("backpressure cursor sequence=%d err=%v; want sequence 0 before queued events were consumed", decoded.Sequence, err)
	}
	request.Cursor = cursor
	resumed, err := (Bridge{Stream: s, Reader: reader, PollInterval: 100 * time.Millisecond, PageLimit: 2}).Watch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	for want := uint64(1); want <= 12; want++ {
		got, nextErr := resumed.Next(context.Background())
		if nextErr != nil || got.Sequence != want {
			t.Fatalf("resumed event=%d err=%v; want exact committed sequence %d", got.Sequence, nextErr, want)
		}
	}
}

func TestTodo_CHAT_020(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	s := newTestStream(t, auth, &now, 2, nil)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	s.Revoke("tenant", "tenant", "subject", "conversation", 2)
	_, err = sub.Next(context.Background())
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("revocation=%v", err)
	}
}

func TestTodo_CHAT_020_Security(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	s := newTestStream(t, auth, &now, 2, nil)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1})
	s.Revoke("tenant", "tenant", "subject", "conversation", 2)
	if _, err := sub.Next(context.Background()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("queued event after revoke: %v", err)
	}
}

func TestTodo_CHAT_020_MembershipEpochInvalidatesCursor(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	s := newTestStream(t, auth, &now, 2, nil)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	token := sub.Cursor()
	changed := req()
	changed.MembershipEpoch = 2
	changed.Cursor = token
	if _, err := s.Watch(context.Background(), changed); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("epoch-bound cursor accepted: %v", err)
	}
}

func TestTodo_CHAT_020_LiveEventUsesSubscriberEpoch(t *testing.T) {
	now := time.Unix(100, 0)
	s, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: epochAuth{}, QueueSize: 2, ReplayLimit: 2, CursorTTL: time.Minute, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Watch(context.Background(), WatchRequest{TenantID: "tenant", HomeTenantID: "home", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	if got, err := sub.Next(context.Background()); err != nil || got.Sequence != 1 {
		t.Fatalf("live event=%+v err=%v", got, err)
	}
}

func TestTodo_CHAT_018_BridgeAdvancesFilteredWatermark(t *testing.T) {
	now := time.Unix(100, 0)
	auth := &testAuth{}
	s, err := New(Config{Key: []byte("secret"), Reader: watermarkReader{}, Authorizer: auth, QueueSize: 2, ReplayLimit: 2, CursorTTL: time.Minute, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := (Bridge{Stream: s, Reader: watermarkReader{}, PollInterval: time.Millisecond, PageLimit: 2}).Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 6 {
		t.Fatalf("event sequence=%d, want 6", event.Sequence)
	}
}

// TestTodo_CHAT_020_TenantScopedFanout proves fan-out and revocation are keyed
// by host tenant as well as conversation: the same conversation identifier in
// another tenant reaches neither the subscriber nor the revocation.
func TestTodo_CHAT_020_TenantScopedFanout(t *testing.T) {
	now := time.Unix(100, 0)
	s := newTestStream(t, &testAuth{}, &now, 2, nil)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	// Another tenant reusing the conversation identifier must not be delivered.
	if err := s.Publish(context.Background(), Event{TenantID: "other", ConversationID: "conversation", Sequence: 9, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	// Nor may its revocation close this tenant's subscription.
	s.Revoke("other", "other", "subject", "conversation", 9)
	s.RevokeTenant("other", "other", "conversation")
	if err := s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	event, err := sub.Next(context.Background())
	if err != nil || event.TenantID != "tenant" || event.Sequence != 1 {
		t.Fatalf("delivered=%+v err=%v, want only this tenant's sequence 1", event, err)
	}
}

// TestTodo_CHAT_020_RevokeTenant proves a grant revocation can close every
// subscription a consumer tenant holds without enumerating its subjects, and
// that a different home tenant keeps its access.
func TestTodo_CHAT_020_RevokeTenant(t *testing.T) {
	now := time.Unix(100, 0)
	s, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: anyAuth{}, QueueSize: 2, ReplayLimit: 2, CursorTTL: time.Minute, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := s.Watch(context.Background(), WatchRequest{TenantID: "host", HomeTenantID: "consumer", SubjectID: "guest", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.Watch(context.Background(), WatchRequest{TenantID: "host", HomeTenantID: "host", SubjectID: "employee", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	s.RevokeTenant("host", "consumer", "conversation")
	if _, err := consumer.Next(context.Background()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("consumer after grant revoke=%v, want ErrRevoked", err)
	}
	if err := s.Publish(context.Background(), Event{TenantID: "host", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	if event, err := local.Next(context.Background()); err != nil || event.Sequence != 1 {
		t.Fatalf("host member after consumer revoke=%+v err=%v", event, err)
	}
	// A membership removal in one home tenant must not close another's.
	s.Revoke("host", "consumer", "employee", "conversation", 1)
	if err := s.Publish(context.Background(), Event{TenantID: "host", ConversationID: "conversation", Sequence: 2, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	if event, err := local.Next(context.Background()); err != nil || event.Sequence != 2 {
		t.Fatalf("home-tenant scoped revoke closed the wrong subscription: %+v err=%v", event, err)
	}
}

type anyAuth struct{}

func (anyAuth) Authorize(context.Context, Access) error { return nil }

// TestTodo_CHAT_020_RecheckIntervalDefault pins the safety-net recheck period:
// revocation is event driven, so the periodic re-authorization must not run
// every second.
func TestTodo_CHAT_020_RecheckIntervalDefault(t *testing.T) {
	s, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: anyAuth{}, QueueSize: 1, ReplayLimit: 1, CursorTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if s.h.config.RecheckInterval != DefaultRecheckInterval {
		t.Fatalf("default recheck=%v, want %v", s.h.config.RecheckInterval, DefaultRecheckInterval)
	}
	explicit, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: anyAuth{}, QueueSize: 1, ReplayLimit: 1, CursorTTL: time.Minute, RecheckInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if explicit.h.config.RecheckInterval != 5*time.Millisecond {
		t.Fatalf("configured recheck=%v", explicit.h.config.RecheckInterval)
	}
	// A configured short interval still closes a subscription whose authority
	// went away without an explicit revocation event.
	auth := &testAuth{}
	ticking, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: auth, QueueSize: 1, ReplayLimit: 1, CursorTTL: time.Minute, RecheckInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := ticking.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	auth.mu.Lock()
	auth.revoked = true
	auth.mu.Unlock()
	if _, err := sub.Next(context.Background()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("safety-net recheck=%v, want ErrRevoked", err)
	}
}
