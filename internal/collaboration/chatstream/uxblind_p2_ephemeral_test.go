package chatstream

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
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

	// The ordered case above cannot fail on timing. The rounds below put the
	// three parties on their own goroutines behind one gate: private answers
	// being published, the recipient reading them, and the recipient being
	// removed from the conversation part way through. Whatever the
	// interleaving, the recipient may read some of her own answers and then
	// is told access is revoked; she never reads one published after the
	// removal returned; and the other member of the room reads nothing.
	const rounds, answers = 200, 16
	for round := 0; round < rounds; round++ {
		stream, err := New(Config{Key: []byte("secret"), Reader: testReader{}, Authorizer: ephemeralTestAuth{}, QueueSize: answers, ReplayLimit: 4, CursorTTL: time.Hour, Clock: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		recipient, err := stream.Watch(context.Background(), WatchRequest{TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "subject", ConversationID: "conversation", MembershipEpoch: 1})
		if err != nil {
			t.Fatal(err)
		}
		other, err := stream.Watch(context.Background(), WatchRequest{TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "other", ConversationID: "conversation", MembershipEpoch: 1})
		if err != nil {
			t.Fatal(err)
		}

		var (
			gate    = make(chan struct{})
			parties sync.WaitGroup
			revoked atomic.Bool
			// firstAfter is the first sequence the publisher sent knowing the
			// removal had already returned; zero when there was none.
			firstAfter atomic.Uint64
			read       []uint64
			terminal   error
			foreign    string
			publishErr error
		)
		parties.Add(3)
		go func() {
			defer parties.Done()
			<-gate
			for sequence := uint64(1); sequence <= answers; sequence++ {
				if revoked.Load() {
					firstAfter.CompareAndSwap(0, sequence)
				}
				if err := stream.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: sequence, MembershipEpoch: 1, RecipientSubjectID: "subject", RecipientHomeTenantID: "tenant", Ephemeral: true, ExpiresAt: now.Add(time.Hour), Payload: []byte("private")}); err != nil {
					publishErr = err
					return
				}
				// Yield at a different point each round so the removal lands
				// before, between and after the answers across the rounds.
				if int(sequence) == round%answers {
					runtime.Gosched()
				}
			}
		}()
		go func() {
			defer parties.Done()
			<-gate
			for spin := 0; spin < round%7; spin++ {
				runtime.Gosched()
			}
			stream.Revoke("tenant", "tenant", "subject", "conversation", 2)
			revoked.Store(true)
		}()
		go func() {
			defer parties.Done()
			<-gate
			readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for {
				event, err := recipient.Next(readCtx)
				if err != nil {
					terminal = err
					return
				}
				if !event.Ephemeral || event.RecipientSubjectID != "subject" || event.RecipientHomeTenantID != "tenant" || string(event.Payload) != "private" {
					foreign = fmt.Sprintf("%+v", event)
					return
				}
				read = append(read, event.Sequence)
			}
		}()
		close(gate)
		parties.Wait()

		if publishErr != nil {
			t.Fatalf("round %d: publishing a private answer failed: %v", round, publishErr)
		}
		if foreign != "" {
			t.Fatalf("round %d: the recipient read an event that is not her private answer: %s", round, foreign)
		}
		if !errors.Is(terminal, ErrRevoked) {
			t.Fatalf("round %d: the removed recipient's stream ended with %v after %d answers, want access revoked", round, terminal, len(read))
		}
		for index, sequence := range read {
			if index > 0 && sequence <= read[index-1] {
				t.Fatalf("round %d: answers arrived out of order or twice: %v", round, read)
			}
			if after := firstAfter.Load(); after != 0 && sequence >= after {
				t.Fatalf("round %d: the recipient read answer %d, published after her removal returned (first such answer %d): %v", round, sequence, after, read)
			}
		}
		// Once revoked, always revoked: nothing queued earlier is handed out.
		for again := 0; again < 3; again++ {
			if event, err := recipient.Next(context.Background()); !errors.Is(err, ErrRevoked) {
				t.Fatalf("round %d: after removal the stream returned event %+v, err %v", round, event, err)
			}
		}
		// A context that is already over makes this a look at the queue with
		// no waiting: the other member has nothing, and was not closed.
		over, cancel := context.WithCancel(context.Background())
		cancel()
		if event, err := other.Next(over); !errors.Is(err, context.Canceled) {
			t.Fatalf("round %d: the other member of the room read %+v, err %v", round, event, err)
		}
		select {
		case <-other.Done():
			t.Fatalf("round %d: removing the recipient closed the other member's stream", round)
		default:
		}
		other.Close()
		recipient.Close()
	}
}
