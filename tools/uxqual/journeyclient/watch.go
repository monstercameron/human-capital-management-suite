package journeyclient

import (
	"context"
	"errors"
	"io"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// This file is the whole of the client's live behaviour, and it is one
// function deep on purpose: WatchJourney is the only streaming RPC on the
// page, and everything about it that could go wrong -- a stream that ends at
// the server's ceiling, a cell that is not answering, a reader who navigates
// away mid-stream -- is decided here rather than spread through the state
// machine.
//
// The shape it is written against is the generated one:
//
//	rpc WatchJourney(WatchJourneyRequest) returns (stream WatchJourneyResponse)
//
// with request fields intent_id and since_digest, and one response field,
// detail. The server emits the current detail once (unless since_digest
// already names it), then one message per change, and ends the stream with
// OK at its fifteen-minute ceiling. There is no heartbeat, so silence means
// nothing has changed and is never a reason to reconnect.

const (
	// defaultWatchRetry is how long the client waits before re-opening a
	// stream that ended on its own. The server's ceiling is minutes, so this
	// is not a poll interval; it is only there so that a cell which is
	// refusing or closing immediately is retried at a human pace rather than
	// in a loop.
	defaultWatchRetry = 2 * time.Second
	// maxWatchAttempts is how many consecutive fruitless re-opens the client
	// makes before it stops and says so. A stream that delivered anything
	// resets the count, so a page left open all day reconnects at every
	// ceiling forever; only a stream that keeps ending with nothing to show
	// gives up.
	maxWatchAttempts = 3
)

// startWatch opens the change feed for one journey, if the reader is still
// looking at it.
//
// sinceDigest is the digest of the detail the client already has, so the
// server can skip re-sending it. That is the whole reason the initial read
// and the watch are separate calls: the page draws from InspectJourney
// immediately and the stream then carries only what changed.
func (a *App) startWatch(generation int, intentID, sinceDigest string) {
	if intentID == "" {
		return
	}
	a.mu.Lock()
	if a.generation != generation {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.stopWatchLocked()
	a.cancelWatch = cancel
	retry := a.WatchRetry
	a.mu.Unlock()

	a.Async(func() { a.watch(ctx, generation, intentID, sinceDigest, retry) })
}

// stopWatchLocked ends the current watch. The caller holds mu.
func (a *App) stopWatchLocked() {
	if a.cancelWatch != nil {
		a.cancelWatch()
		a.cancelWatch = nil
	}
}

// watch runs one journey's change feed until the reader leaves it, the page
// ends, or the cell stops answering.
func (a *App) watch(ctx context.Context, generation int, intentID, sinceDigest string, retry time.Duration) {
	attempts := 0
	for {
		openedAt := time.Now()
		stream, err := a.svc.WatchJourney(ctx, &journeyv1.WatchJourneyRequest{
			IntentId:    intentID,
			SinceDigest: sinceDigest,
		})
		if a.watchEnded(ctx, generation) {
			return
		}
		if err != nil {
			// A refusal is the engine's answer to the same read
			// InspectJourney just made, so it is worth showing; a transport
			// hiccup is not, until it has happened often enough to mean the
			// page is no longer live.
			attempts++
			if attempts >= maxWatchAttempts {
				a.show(noticeFromError(err, a.localeCopy()))
				return
			}
			if !sleepUntil(ctx, retry) {
				return
			}
			continue
		}

		delivered := false
		var termination error
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				termination = recvErr
				// Every stream ends: with OK at the server's ceiling, with
				// the caller's cancellation, or with a refusal. The three
				// are told apart below, not here.
				break
			}
			detail := msg.GetDetail()
			if detail == nil {
				continue
			}
			delivered = true
			sinceDigest = detail.GetDetailDigest()
			// The notice the reader is looking at is carried across a live
			// update: an approval's "Approved" must not be wiped half a
			// second later by the stream delivering the same approval.
			notice := a.currentNotice()
			// A durable terminal result supersedes an earlier success message
			// saying that approval is still waiting on the effective date.
			if detail.GetLedger() != nil && notice != nil && notice.Tone == toneSuccess {
				notice = approvalNotice(detail)
			}
			a.applyDetail(generation, detail, notice)
		}

		if a.watchEnded(ctx, generation) {
			return
		}
		if delivered || quietWatchRollover(termination, time.Since(openedAt)) {
			attempts = 0
		} else {
			attempts++
		}
		if attempts >= maxWatchAttempts {
			a.show(keyedNotice(toneWarning, "journey.watch_stopped_title", "journey.watch_stopped_detail"))
			return
		}
		if !sleepUntil(ctx, retry) {
			return
		}
	}
}

// A quiet, established stream can reach the transport's shorter deadline
// without a domain change. Reconnect with the same digest; do not count normal
// rotation as an outage. Immediate EOF/deadline failures still exhaust retries.
func quietWatchRollover(err error, lifetime time.Duration) bool {
	return lifetime >= 10*time.Second && (errors.Is(err, io.EOF) ||
		errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded)
}

// watchEnded reports whether this watch has been superseded: its context was
// cancelled (the page ended, or the route changed), or the reader navigated
// somewhere else.
func (a *App) watchEnded(ctx context.Context, generation int) bool {
	return ctx.Err() != nil || a.stale(generation)
}

// currentNotice is the notice on screen right now.
func (a *App) currentNotice() *journey.Notice {
	return a.store.Page().Notice
}

// sleepUntil waits for d, reporting false when the context ended first. A
// non-positive d returns immediately, which is what makes the watch's retry
// behaviour testable without a real wait.
func sleepUntil(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
