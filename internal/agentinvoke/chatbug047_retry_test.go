package agentinvoke

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// CHATBUG-047: asking again admits the question that already stands as another
// attempt. Each attempt is an invocation of its own; asking for the same attempt
// twice claims the same one and starts nothing twice.
func TestTodo_CHATBUG_047_RetryAdmission(t *testing.T) {
	authority := &p8Authority{byUser: map[string]Admission{}, defaultAdmission: p8Admission("manager")}
	runs, refusals := &p8Runs{}, &p8Refusals{}
	service := p8Service(authority, runs, refusals, nil)
	post := p8Post("manager", "post-1")

	first, err := service.ResolveMention(context.Background(), post)
	if err != nil || len(first) != 1 || len(runs.requests) != 1 {
		t.Fatalf("first admission=%+v err=%v runs=%d", first, err, len(runs.requests))
	}
	if first[0].ID != invocationID(post, "comp") {
		t.Fatalf("the first attempt changed its identifier: %s", first[0].ID)
	}

	retried, err := service.RetryMention(context.Background(), post, 1)
	if err != nil || len(retried) != 1 || len(runs.requests) != 2 {
		t.Fatalf("retry=%+v err=%v runs=%d", retried, err, len(runs.requests))
	}
	if retried[0].ID == first[0].ID || retried[0].PostID != "post-1" || retried[0].ThreadID != "thread-1" || retried[0].State != InvocationStarted {
		t.Fatalf("a retry is the same post as another invocation: first=%+v retry=%+v", first[0], retried[0])
	}
	if runs.requests[1].InvokingPostID != "post-1" || runs.requests[1].InvocationID != retried[0].ID || runs.requests[1].InvokerID != "manager" {
		t.Fatalf("the retry's run is not bound to the same question: %+v", runs.requests[1])
	}

	// Asking for the same attempt again claims the same invocation.
	again, err := service.RetryMention(context.Background(), post, 1)
	if err != nil || len(again) != 1 || again[0].ID != retried[0].ID || len(runs.requests) != 2 {
		t.Fatalf("a repeated retry started another run: %+v err=%v runs=%d", again, err, len(runs.requests))
	}

	next, err := service.RetryMention(context.Background(), post, 2)
	if err != nil || len(next) != 1 || next[0].ID == retried[0].ID || len(runs.requests) != 3 {
		t.Fatalf("the next attempt=%+v err=%v runs=%d", next, err, len(runs.requests))
	}
	if _, err := service.RetryMention(context.Background(), post, 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("attempt zero is a first admission, not a retry: %v", err)
	}
}

func TestTodo_CHATBUG_047_RetryAdmission_Race(t *testing.T) {
	authority := &p8Authority{byUser: map[string]Admission{}, defaultAdmission: p8Admission("manager")}
	runs, refusals := &p8Runs{}, &p8Refusals{}
	service := p8Service(authority, runs, refusals, nil)
	post := p8Post("manager", "post-1")
	if _, err := service.ResolveMention(context.Background(), post); err != nil {
		t.Fatal(err)
	}
	// Two rapid presses of "Ask again" read the same state and name the same attempt.
	var wg sync.WaitGroup
	ids := make([]string, 8)
	start := make(chan struct{})
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := service.RetryMention(context.Background(), post, 1)
			if err == nil && len(got) == 1 {
				ids[i] = got[0].ID
			}
		}()
	}
	close(start)
	wg.Wait()
	runs.mu.Lock()
	defer runs.mu.Unlock()
	if len(runs.requests) != 2 {
		t.Fatalf("rapid retries started %d runs in all, want the first and one retry", len(runs.requests))
	}
	for _, id := range ids {
		if id == "" || id != ids[0] {
			t.Fatalf("rapid retries named different attempts: %v", ids)
		}
	}
}

// A retry is judged under current authority, as a first question is.
func TestTodo_CHATBUG_047_RetryAdmission_CurrentAuthority(t *testing.T) {
	authority := &p8Authority{byUser: map[string]Admission{}, defaultAdmission: p8Admission("manager")}
	runs, refusals := &p8Runs{}, &p8Refusals{}
	service := p8Service(authority, runs, refusals, nil)
	post := p8Post("manager", "post-1")
	if _, err := service.ResolveMention(context.Background(), post); err != nil {
		t.Fatal(err)
	}
	// The asker has since left the conversation.
	authority.mu.Lock()
	authority.byUser["manager"] = p8Admission("outsider")
	authority.mu.Unlock()
	if got, err := service.RetryMention(context.Background(), post, 1); err != nil || len(got) != 0 || len(runs.requests) != 1 || len(refusals.values) != 1 {
		t.Fatalf("a retry after membership ended started a run: %+v err=%v runs=%d refusals=%d", got, err, len(runs.requests), len(refusals.values))
	}
}
