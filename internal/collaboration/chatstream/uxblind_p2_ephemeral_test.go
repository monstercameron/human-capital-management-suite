package chatstream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTodo_AGENTP_011_Integration(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	auth := ephemeralTestAuth{}
	s, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: auth, QueueSize: 8, ReplayLimit: 4, CursorTTL: time.Hour, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := s.Watch(context.Background(), WatchRequest{TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Watch(context.Background(), WatchRequest{TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "other", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer recipient.Close()
	defer other.Close()
	if err := s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1, RecipientSubjectID: "subject", RecipientHomeTenantID: "tenant", Ephemeral: true, ExpiresAt: now.Add(time.Hour), Payload: []byte("private")}); err != nil {
		t.Fatal(err)
	}
	got, err := recipient.Next(context.Background())
	if err != nil || string(got.Payload) != "private" || !got.Ephemeral {
		t.Fatalf("recipient event=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := other.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("other subscriber observed recipient-only event: %v", err)
	}
}

func TestTodo_AGENTP_011_RecipientHomeTenantEnvelope(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	s, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: anyAuth{}, QueueSize: 8, ReplayLimit: 4, CursorTTL: time.Hour, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Watch(context.Background(), WatchRequest{TenantID: "host", HomeTenantID: "home-a", SubjectID: "same-subject", ConversationID: "room", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Watch(context.Background(), WatchRequest{TenantID: "host", HomeTenantID: "home-b", SubjectID: "same-subject", ConversationID: "room", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer second.Close()

	if err := s.Publish(context.Background(), Event{TenantID: "host", ConversationID: "room", Sequence: 1, MembershipEpoch: 1, RecipientSubjectID: "same-subject", RecipientHomeTenantID: "home-a", Ephemeral: true, ExpiresAt: now.Add(time.Hour), Payload: []byte("a")}); err != nil {
		t.Fatal(err)
	}
	got, err := first.Next(context.Background())
	if err != nil || string(got.Payload) != "a" {
		t.Fatalf("home-a recipient event=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := second.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("home-b received home-a event: %v", err)
	}

	if err := s.Publish(context.Background(), Event{TenantID: "host", ConversationID: "room", Sequence: 2, MembershipEpoch: 1, RecipientSubjectID: "same-subject", RecipientHomeTenantID: "home-b", Ephemeral: true, ExpiresAt: now.Add(time.Hour), Payload: []byte("b")}); err != nil {
		t.Fatal(err)
	}
	got, err = second.Next(context.Background())
	if err != nil || string(got.Payload) != "b" {
		t.Fatalf("home-b recipient event=%+v err=%v", got, err)
	}
}

func TestTodo_AGENTP_011_MissingRecipientHomeTenantFailsClosed(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	s, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: anyAuth{}, QueueSize: 2, ReplayLimit: 2, CursorTTL: time.Hour, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(context.Background(), Event{TenantID: "host", ConversationID: "room", Sequence: 1, MembershipEpoch: 1, RecipientSubjectID: "same-subject", Ephemeral: true, ExpiresAt: now.Add(time.Hour)}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing recipient home tenant error=%v, want ErrInvalidConfig", err)
	}
}

type memberViewReader struct {
	mu    sync.Mutex
	calls map[string]int
}

func (r *memberViewReader) Read(_ context.Context, q ReadRequest) (Page, error) {
	r.mu.Lock()
	r.calls[q.SubjectID]++
	call := r.calls[q.SubjectID]
	r.mu.Unlock()
	if call == 1 {
		return Page{Complete: true}, nil
	}
	sequence := uint64(1)
	if q.SubjectID == "bob" {
		sequence = 2
	}
	if q.AfterSequence >= sequence {
		return Page{NextSequence: sequence, Complete: true}, nil
	}
	return Page{Events: []Event{{TenantID: q.TenantID, ConversationID: q.ConversationID, Sequence: sequence, MembershipEpoch: 1}}, NextSequence: sequence, Complete: true}, nil
}

func TestTodo_AGENTP_011_BridgeKeepsMemberViewsIsolated(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	reader := &memberViewReader{calls: make(map[string]int)}
	s, err := New(Config{Key: []byte("secret"), Reader: reader, Authorizer: anyAuth{}, QueueSize: 4, ReplayLimit: 2, CursorTTL: time.Hour, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	alice, err := (Bridge{Stream: s, Reader: reader, PollInterval: time.Millisecond, PageLimit: 2}).Watch(ctx, WatchRequest{TenantID: "host", HomeTenantID: "host", SubjectID: "alice", ConversationID: "room", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := (Bridge{Stream: s, Reader: reader, PollInterval: time.Millisecond, PageLimit: 2}).Watch(ctx, WatchRequest{TenantID: "host", HomeTenantID: "host", SubjectID: "bob", ConversationID: "room", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()
	defer bob.Close()

	next := func(sub *Subscription) Event {
		t.Helper()
		readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
		defer readCancel()
		event, nextErr := sub.Next(readCtx)
		if nextErr != nil {
			t.Fatalf("stream read: %v", nextErr)
		}
		return event
	}
	if event := next(alice); event.Sequence != 1 {
		t.Fatalf("alice received sequence %d, want 1", event.Sequence)
	}
	if event := next(bob); event.Sequence != 2 {
		t.Fatalf("bob received sequence %d, want 2", event.Sequence)
	}
	readCtx, readCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer readCancel()
	if _, err := alice.Next(readCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("alice received bob's member-filtered event: %v", err)
	}
	if _, err := bob.Next(readCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bob received alice's member-filtered event: %v", err)
	}
}

type ephemeralTestAuth struct{}

func (ephemeralTestAuth) Authorize(_ context.Context, access Access) error {
	if access.TenantID != "tenant" || access.ConversationID != "conversation" || access.MembershipEpoch == 0 {
		return errors.New("scope")
	}
	return nil
}

func TestTodo_AGENTP_011_Race(t *testing.T) {
	// Runs beside the package's other parallel tests so the race detector
	// sees this path against them.
	t.Parallel()
	now := time.Unix(100, 0).UTC()
	s, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: ephemeralTestAuth{}, QueueSize: 8, ReplayLimit: 4, CursorTTL: time.Hour, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Watch(context.Background(), WatchRequest{TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	s.Revoke("tenant", "tenant", "subject", "conversation", 2)
	_ = s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1, RecipientSubjectID: "subject", RecipientHomeTenantID: "tenant", Ephemeral: true, ExpiresAt: now.Add(time.Hour), Payload: []byte("late")})
	if _, err := sub.Next(context.Background()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("recipient leaving during delivery returned %v", err)
	}
}
