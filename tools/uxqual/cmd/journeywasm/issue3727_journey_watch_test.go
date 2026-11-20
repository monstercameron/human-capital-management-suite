package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type watchProbe struct {
	mu        sync.Mutex
	opened    map[string]int
	feeds     map[string]chan *journeyv1.JourneyDetail
	errs      map[string]error
	cancelled map[string]chan struct{}
	got       chan string
	failed    chan string
}

func newWatchProbe() *watchProbe {
	return &watchProbe{opened: map[string]int{}, feeds: map[string]chan *journeyv1.JourneyDetail{}, errs: map[string]error{},
		cancelled: map[string]chan struct{}{}, got: make(chan string, 16), failed: make(chan string, 16)}
}

func (p *watchProbe) feed(id string) chan *journeyv1.JourneyDetail {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.feeds[id] == nil {
		p.feeds[id] = make(chan *journeyv1.JourneyDetail, 4)
	}
	return p.feeds[id]
}

func (p *watchProbe) hub(max int) *chatJourneyWatchHub {
	return &chatJourneyWatchHub{
		Max: max, Retry: time.Millisecond,
		Open: func(ctx context.Context, id, since string) (func() (*journeyv1.JourneyDetail, error), error) {
			p.mu.Lock()
			p.opened[id]++
			err := p.errs[id]
			done := make(chan struct{})
			p.cancelled[id] = done
			p.mu.Unlock()
			if err != nil {
				return nil, err
			}
			feed := p.feed(id)
			go func() { <-ctx.Done(); close(done) }()
			return func() (*journeyv1.JourneyDetail, error) {
				select {
				case d := <-feed:
					return d, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}, nil
		},
		Deliver: func(identity, id string, d *journeyv1.JourneyDetail) { p.got <- id },
		Fail:    func(identity, id string, err error) { p.failed <- id },
	}
}

func (p *watchProbe) opens(id string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.opened[id]
}

func issue3727WaitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}

func TestIssue3727_StageChangeIsPushedToTheVisibleCard(t *testing.T) {
	p := newWatchProbe()
	h := p.hub(8)
	defer h.Stop()
	h.Sync("t\x00alice", []string{"j1"})
	p.feed("j1") <- &journeyv1.JourneyDetail{}
	select {
	case id := <-p.got:
		if id != "j1" {
			t.Fatalf("delivered %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a change on the stream never reached the card")
	}
}

func TestIssue3727_StreamsStopWhenCardLeavesViewAndOnIdentityChange(t *testing.T) {
	p := newWatchProbe()
	h := p.hub(8)
	defer h.Stop()
	h.Sync("t\x00alice", []string{"j1", "j2"})
	issue3727WaitFor(t, "both opened", func() bool { return p.opens("j1") > 0 && p.opens("j2") > 0 })
	p.mu.Lock()
	gone := p.cancelled["j1"]
	p.mu.Unlock()
	h.Sync("t\x00alice", []string{"j2"})
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Fatal("stream for a card that left view is still open")
	}
	if got := h.Active(); got != 1 {
		t.Fatalf("active = %d, want 1", got)
	}
	h.Sync("t\x00bob", []string{"j2"})
	issue3727WaitFor(t, "j2 reopened for the new identity", func() bool { return p.opens("j2") >= 2 })
	if got := h.Active(); got != 1 {
		t.Fatalf("active after identity change = %d, want 1", got)
	}
	h.Stop()
	if got := h.Active(); got != 0 {
		t.Fatalf("active after Stop = %d", got)
	}
}

func TestIssue3727_ConcurrentStreamsAreCapped(t *testing.T) {
	p := newWatchProbe()
	h := p.hub(2)
	defer h.Stop()
	h.Sync("t\x00alice", []string{"a", "b", "c", "d"})
	if got := h.Active(); got != 2 {
		t.Fatalf("active = %d, want cap 2", got)
	}
	if !h.Watching("a") || !h.Watching("b") || h.Watching("c") {
		t.Fatal("cap must keep the first (newest) cards")
	}
}

func TestIssue3727_RefusalStopsUpdatesAndSaysSo(t *testing.T) {
	p := newWatchProbe()
	p.errs["j1"] = status.Error(codes.PermissionDenied, "revoked")
	h := p.hub(8)
	defer h.Stop()
	h.Sync("t\x00alice", []string{"j1"})
	select {
	case <-p.failed:
	case <-time.After(2 * time.Second):
		t.Fatal("a refused watch was not reported")
	}
	time.Sleep(20 * time.Millisecond)
	if n := p.opens("j1"); n != 1 {
		t.Fatalf("a refused watch was retried %d times", n)
	}
}

func TestIssue3727_TransientFailuresGiveUpQuietlyForPollingFallback(t *testing.T) {
	p := newWatchProbe()
	p.errs["j1"] = errors.New("boom")
	h := p.hub(8)
	defer h.Stop()
	h.Sync("t\x00alice", []string{"j1"})
	issue3727WaitFor(t, "watch gives up", func() bool { return !h.Watching("j1") })
	select {
	case id := <-p.failed:
		t.Fatalf("transient failure replaced card %s with an error; polling must carry on", id)
	default:
	}
	if n := p.opens("j1"); n != 3 {
		t.Fatalf("opened %d times, want 3 attempts", n)
	}
}
