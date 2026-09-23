package application

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

// growingReader is a durable event source a test can append to while a stream is
// running, which is what makes the difference between a subscription that is
// alive and one that merely opened.
type growingReader struct {
	mu     sync.Mutex
	events []chatstream.Event
}

func (r *growingReader) add(tenant, conversation string, sequence uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	payload, _ := json.Marshal(chatcore.ConversationEvent{Kind: chatcore.PostCreated, Sequence: sequence})
	r.events = append(r.events, chatstream.Event{TenantID: tenant, ConversationID: conversation, Sequence: sequence, MembershipEpoch: 3, Payload: payload})
}

func (r *growingReader) Read(_ context.Context, q chatstream.ReadRequest) (chatstream.Page, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var page chatstream.Page
	for _, e := range r.events {
		if e.Sequence <= q.AfterSequence {
			continue
		}
		if len(page.Events) == q.Limit {
			break
		}
		page.Events = append(page.Events, e)
	}
	if len(page.Events) > 0 {
		page.NextSequence = page.Events[len(page.Events)-1].Sequence
	} else {
		page.NextSequence = q.AfterSequence
		page.Complete = true
	}
	return page, nil
}

// TestTodo_CHAT_018_WatchSurvivesAFullReplayAndDeliversLivePosts is the live
// server's 40-80 ms stream death. A catch-up replay was clamped to the whole
// queue, so a conversation with at least QueueSize durable events opened with a
// full queue and the bridge's first poll closed the subscription with
// backpressure. The stream now runs for seconds and delivers a post sent while
// it is open.
func TestTodo_CHAT_018_WatchSurvivesAFullReplayAndDeliversLivePosts(t *testing.T) {
	reader := &growingReader{}
	// Thirty-two durable events and a queue of thirty-two: the exact shape that
	// used to be fatal.
	for i := 1; i <= 32; i++ {
		reader.add("t", "c", uint64(i))
	}
	cfg := ChatStreamRuntimeConfig{
		CursorKey: "life-key", Reader: reader, Authorizer: streamAuthStub{},
		PollInterval: 50 * time.Millisecond, PageLimit: 100, QueueSize: 32, ReplayLimit: 100,
		CursorTTL: time.Minute, Budgets: defaultChatAdmissionConfig(),
	}
	runtime, err := NewChatStreamRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	inner := newChatServiceStub()
	service := &streamingChatService{ConversationService: inner, runtime: runtime, membership: membershipResolverStub{member: chatcore.Membership{TenantID: "t", ConversationID: "c", HomeTenantID: "t", SubjectID: "u", Revision: 3}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := service.WatchConversation(ctx, chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "t", SubjectID: "u"}, TenantID: "t", ConversationID: "c"})
	if err != nil {
		t.Fatal(err)
	}

	// A post sent while the stream is open, after the replay window.
	deadline := time.After(3 * time.Second)
	go func() {
		time.Sleep(700 * time.Millisecond)
		reader.add("t", "c", 999)
	}()
	var live bool
	for !live {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("the stream closed before the live post arrived")
			}
			if e.Event.Sequence == 999 {
				live = true
			}
		case <-deadline:
			t.Fatal("no live post within three seconds")
		}
	}
	// Still open after two seconds of running, not merely open once.
	time.Sleep(1300 * time.Millisecond)
	reader.add("t", "c", 1000)
	select {
	case e, ok := <-events:
		if !ok {
			t.Fatal("the stream closed during a two-second run")
		}
		if e.Event.Sequence != 1000 {
			t.Fatalf("event after two seconds = %d", e.Event.Sequence)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the stream stopped delivering after two seconds")
	}
	if got := runtime.admission.Available(chatadmission.Request{TenantID: "t", ConversationID: "c", Lane: chatadmission.LaneRead}); got == 0 {
		t.Fatal("the read lane was consumed by a live watch")
	}
}
