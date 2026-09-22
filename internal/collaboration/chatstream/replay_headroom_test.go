package chatstream

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fullPageReader answers every read with a full page, which is what a catch-up
// on a busy conversation looks like.
type fullPageReader struct{ count int }

func (r fullPageReader) Read(_ context.Context, q ReadRequest) (Page, error) {
	var page Page
	for i := 1; i <= r.count && len(page.Events) < q.Limit; i++ {
		sequence := q.AfterSequence + uint64(i)
		page.Events = append(page.Events, Event{TenantID: q.TenantID, ConversationID: q.ConversationID, Sequence: sequence, MembershipEpoch: 1, Payload: []byte(`{}`)})
	}
	if len(page.Events) > 0 {
		page.NextSequence = page.Events[len(page.Events)-1].Sequence
	}
	return page, nil
}

// TestReplayLeavesRoomForLiveEvents proves a fresh subscription is never handed
// back with a full queue. Replay used to be clamped to the whole queue, so a
// conversation with at least QueueSize durable events opened full and the next
// published event closed the brand-new subscription with backpressure.
func TestReplayLeavesRoomForLiveEvents(t *testing.T) {
	now := time.Unix(100, 0)
	s, err := New(Config{Key: []byte("secret"), Reader: fullPageReader{count: 64}, Authorizer: &testAuth{}, QueueSize: 8, ReplayLimit: 64, CursorTTL: time.Minute, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Watch(context.Background(), WatchRequest{TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatalf("subscribe on a busy conversation = %v", err)
	}
	// The first live event after the replay must be accepted and delivered, not
	// dropped into a close.
	live := sub.Sequence() + 1
	if err := s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: live, MembershipEpoch: 1, Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= int(live); i++ {
		event, nextErr := sub.Next(context.Background())
		if nextErr != nil {
			if errors.Is(nextErr, ErrBackpressure) {
				t.Fatalf("a fresh subscription was closed by backpressure after %d events; the live event %d never arrived", i, live)
			}
			t.Fatal(nextErr)
		}
		if event.Sequence == live {
			sub.Close()
			return
		}
	}
	t.Fatalf("the live event %d was never delivered", live)
}
