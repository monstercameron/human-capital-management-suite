//go:build js && wasm

package main

import (
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatperfFirstPaint is the page's gate (chatperf_gate.go).
var chatperfFirstPaint chatperfGate

const (
	// chatperfPaintGrace lets the browser put the first timeline on screen
	// before the held work starts competing for the thread.
	chatperfPaintGrace = 60 * time.Millisecond
	// chatperfFirstPaintLimit opens the gate for a page whose first timeline
	// never arrives, so nothing waits on a read that is itself stuck.
	chatperfFirstPaintLimit = 4 * time.Second
	// chatperfReleaseStep spaces the held work out, so it does not all land in
	// the frame after the first paint.
	chatperfReleaseStep = 25 * time.Millisecond
)

// chatperfAfterFirstPaint runs work on its own goroutine: at once when the
// first timeline has been shown, otherwise when it is.
func chatperfAfterFirstPaint(work func()) {
	if !chatperfFirstPaint.hold(work) {
		go work()
	}
}

// chatperfStartAfterFirstPaint starts a long-running job once the first
// timeline has been shown. The stop it returns works whether or not the job has
// started, and a job stopped before it started never starts.
func chatperfStartAfterFirstPaint(start func() func()) func() {
	var mu sync.Mutex
	var stop func()
	stopped := false
	chatperfAfterFirstPaint(func() {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			stop = start()
		}
	})
	return func() {
		mu.Lock()
		stopped = true
		running := stop
		mu.Unlock()
		if running != nil {
			running()
		}
	}
}

// chatperfTimelineShown is called after a render that drew a settled timeline.
// The held work starts a moment later, once per page.
func chatperfTimelineShown() {
	if chatperfFirstPaint.isOpen() {
		return
	}
	time.AfterFunc(chatperfPaintGrace, chatperfReleaseFirstPaint)
}

func chatperfReleaseFirstPaint() {
	// The page is drawn: the collector, off since the client started, resumes
	// shortly (chatperf2_boot_gc.go).
	chatperf2Collector.FirstPaint()
	waiting := chatperfFirstPaint.release()
	if len(waiting) == 0 {
		return
	}
	bootMark("chat-held-reads")
	go func() {
		for _, work := range waiting {
			go work()
			time.Sleep(chatperfReleaseStep)
		}
	}()
}

// chatperfPageMounted arms the limit for the page that has just mounted and
// returns what to call when it is left.
func chatperfPageMounted() func() {
	limit := time.AfterFunc(chatperfFirstPaintLimit, chatperfReleaseFirstPaint)
	return func() {
		limit.Stop()
		chatperfFirstPaint.reset()
	}
}

// chatperfReactionRead is the reaction read of the conversation being opened.
var chatperfReactionRead chatperfTurn

// chatperfStreamWait is the longest a conversation's stream waits for its
// reaction read. A read that is slow or failing does not hold live delivery
// back for longer than this.
const chatperfStreamWait = 1500 * time.Millisecond

// chatperfSubscribeAfterReactions starts the live stream of a conversation once
// its reaction read has landed (see chatperfTurn), or after chatperfStreamWait.
// The stream is not started for a conversation the reader has left meanwhile.
func chatperfSubscribeAfterReactions(id string, generation uint64) {
	select {
	case <-chatperfReactionRead.wait(id):
	case <-time.After(chatperfStreamWait):
	}
	if chatBrowser.generationActive(generation) && chatBrowser.selectedID() == id {
		subscribeChatConversation(id)
	}
}

// chatperfLastDrawn is what the latest render of the page showed.
var chatperfLastDrawn chatperfDrawn

func chatperfDrawnOf(model chatui.Model) chatperfDrawn {
	return chatperfDrawn{State: string(model.State), Selected: model.SelectedID, Messages: len(model.Messages)}
}

// chatperfDrawnNow is what a render would show of the timeline now. It reads
// the three fields alone: a snapshot builds the page's whole callback table.
func chatperfDrawnNow() chatperfDrawn {
	chatBrowser.mu.RLock()
	defer chatBrowser.mu.RUnlock()
	return chatperfDrawn{State: string(chatBrowser.model.State), Selected: chatBrowser.model.SelectedID, Messages: len(chatBrowser.model.Messages)}
}

// chatperfCatchUp repaints once when the model moved on between the page's
// first render and its mount: a change committed in that gap had no page to
// repaint.
func chatperfCatchUp() {
	if chatRerender != nil && chatperfBehind(chatperfLastDrawn, chatperfDrawnNow()) {
		chatRerender()
	}
}

// chatperfSkeletonOnScreen reports whether the page is still waiting for its
// first timeline: nothing has been shown yet and the messages are being read.
func chatperfSkeletonOnScreen() bool {
	return !chatperfFirstPaint.isOpen() && chatperfDrawnNow().State == string(chatui.StateLoading)
}

// chatperfSkeletonRender gathers the repaints asked for while the skeleton is
// on screen. The first of them still shows within chatperfSkeletonLimit, so the
// sidebar's badges and names do not wait for the messages.
var chatperfSkeletonRender = newChatperfRenderCoalescer(browserDebounceScheduler, time.Now,
	func(time.Time) chatperfRenderPace {
		return chatperfRenderPace{quiet: chatperfSkeletonQuiet, limit: chatperfSkeletonLimit}
	},
	func() {
		if chatRerender != nil {
			chatRerender()
		}
	})

const (
	chatperfSkeletonQuiet = 150 * time.Millisecond
	chatperfSkeletonLimit = 600 * time.Millisecond
)
