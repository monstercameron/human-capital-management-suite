package chatstream

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// flakyAuth answers "could not decide" while down is set, and allows the
// conversation's one subject otherwise.
type flakyAuth struct{ down atomic.Bool }

func (a *flakyAuth) Authorize(_ context.Context, x Access) error {
	if a.down.Load() {
		return fmt.Errorf("%w: pool exhausted", ErrAuthorityUnavailable)
	}
	if x.TenantID != "tenant" || x.SubjectID != "subject" || x.ConversationID != "conversation" {
		return errors.New("scope")
	}
	return nil
}

func newFlakyStream(t *testing.T, auth *flakyAuth, recheck time.Duration) *Stream {
	t.Helper()
	s, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: auth, QueueSize: 8, ReplayLimit: 4, CursorTTL: time.Minute, RecheckInterval: recheck})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func r2Event(sequence uint64) Event {
	return Event{TenantID: "tenant", ConversationID: "conversation", Sequence: sequence, MembershipEpoch: 1}
}

// A database that did not answer is not a revoked membership. Saying so closed
// the page's watch with "you do not have access" and it never tried again.
func TestR2_UndecidedAuthorityOnOpenIsNotARefusal(t *testing.T) {
	auth := &flakyAuth{}
	auth.down.Store(true)
	s := newFlakyStream(t, auth, time.Minute)
	_, err := s.Watch(context.Background(), req())
	if !errors.Is(err, ErrAuthorityUnavailable) {
		t.Fatalf("open on an undecided authority: %v, want ErrAuthorityUnavailable", err)
	}
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrRevoked) {
		t.Fatalf("open on an undecided authority was reported as a refusal: %v", err)
	}
}

// The safety-net recheck must not drop a healthy subscription over one tick in
// which the authority did not answer.
func TestR2_UndecidedRecheckKeepsTheSubscription(t *testing.T) {
	auth := &flakyAuth{}
	s := newFlakyStream(t, auth, 10*time.Millisecond)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	auth.down.Store(true)
	time.Sleep(80 * time.Millisecond)
	select {
	case <-sub.Done():
		t.Fatal("a recheck the authority did not answer closed the subscription")
	default:
	}
	auth.down.Store(false)
	if err := s.Publish(context.Background(), r2Event(1)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil || event.Sequence != 1 {
		t.Fatalf("event after the authority came back: %+v, %v", event, err)
	}
}

// An event is never delivered on an undecided answer. The subscription ends, but
// not as revoked, and its cursor stays where the last consumed event left it so
// the reconnect receives the event.
func TestR2_UndecidedAuthorityOnAnEventEndsTheStreamWithoutRevoking(t *testing.T) {
	auth := &flakyAuth{}
	s := newFlakyStream(t, auth, time.Minute)
	sub, err := s.Watch(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	auth.down.Store(true)
	if err := s.Publish(context.Background(), r2Event(1)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = sub.Next(ctx)
	if !errors.Is(err, ErrAuthorityUnavailable) || errors.Is(err, ErrRevoked) {
		t.Fatalf("event on an undecided authority: %v, want ErrAuthorityUnavailable and not ErrRevoked", err)
	}
}
