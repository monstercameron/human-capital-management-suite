package main

import (
	"context"
	"sync"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Live journey cards (#3727). The hub keeps one WatchJourney stream open for
// each journey card in view, up to Max, and hands every change to Deliver so
// the card's stage pill moves within about a second instead of waiting for the
// 15 second re-read. It owns lifecycle only: streams stop when a card leaves
// view, when the signed-in identity changes (a stream always runs under the
// principal that opened it), and when the page goes away. A refusal ends the
// stream and is reported through Fail so a revoked grant stops showing details;
// a stream that keeps failing otherwise gives up quietly and the periodic
// re-read carries on as the fallback.
const (
	chatJourneyWatchMax      = 8
	chatJourneyWatchRetry    = 2 * time.Second
	chatJourneyWatchAttempts = 3
)

type chatJourneyWatchHub struct {
	Max   int
	Retry time.Duration
	// Open starts one journey's change feed and returns its receiver.
	Open func(ctx context.Context, id, sinceDigest string) (func() (*journeyv1.JourneyDetail, error), error)
	// Deliver receives each change; Fail receives a refusal that ends the feed.
	Deliver func(identity, id string, detail *journeyv1.JourneyDetail)
	Fail    func(identity, id string, err error)

	mu       sync.Mutex
	identity string
	streams  map[string]context.CancelFunc
}

// Sync makes the open streams match the visible ids (newest first) for one
// identity. Calling it again with the same set changes nothing.
func (h *chatJourneyWatchHub) Sync(identity string, ids []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.streams == nil || h.identity != identity {
		h.stopLocked()
		h.streams, h.identity = map[string]context.CancelFunc{}, identity
	}
	limit := h.Max
	if limit <= 0 {
		limit = chatJourneyWatchMax
	}
	want := map[string]bool{}
	var order []string
	for _, id := range ids {
		if id != "" && !want[id] && len(order) < limit {
			want[id] = true
			order = append(order, id)
		}
	}
	for id, cancel := range h.streams {
		if !want[id] {
			cancel()
			delete(h.streams, id)
		}
	}
	for _, id := range order {
		if h.streams[id] != nil {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		h.streams[id] = cancel
		go h.run(ctx, identity, id)
	}
}

// Stop ends every stream.
func (h *chatJourneyWatchHub) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopLocked()
}

func (h *chatJourneyWatchHub) stopLocked() {
	for id, cancel := range h.streams {
		cancel()
		delete(h.streams, id)
	}
}

// Active counts open streams.
func (h *chatJourneyWatchHub) Active() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.streams)
}

// Watching reports whether one journey has an open stream.
func (h *chatJourneyWatchHub) Watching(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.streams[id] != nil
}

// finish drops a stream that ended on its own; a stream cancelled by Sync or
// Stop was already removed, and a newer stream under the same id is left alone.
func (h *chatJourneyWatchHub) finish(ctx context.Context, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() == nil {
		if cancel := h.streams[id]; cancel != nil {
			cancel()
			delete(h.streams, id)
		}
	}
}

func chatJourneyWatchRefused(err error) bool {
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated, codes.NotFound:
		return true
	}
	return false
}

func (h *chatJourneyWatchHub) run(ctx context.Context, identity, id string) {
	defer h.finish(ctx, id)
	attempts, since := 0, ""
	for ctx.Err() == nil {
		recv, err := h.Open(ctx, id, since)
		delivered := false
		if err == nil {
			for {
				detail, recvErr := recv()
				if recvErr != nil {
					err = recvErr
					break
				}
				if detail == nil {
					continue
				}
				delivered = true
				since = detail.GetDetailDigest()
				if ctx.Err() != nil {
					return
				}
				h.Deliver(identity, id, detail)
			}
		}
		if ctx.Err() != nil {
			return
		}
		if chatJourneyWatchRefused(err) {
			if h.Fail != nil {
				h.Fail(identity, id, err)
			}
			return
		}
		if delivered {
			attempts = 0
		} else {
			attempts++
			if attempts >= chatJourneyWatchAttempts {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(h.Retry):
		}
	}
}
