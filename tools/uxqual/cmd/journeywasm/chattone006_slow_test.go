package main

import (
	"testing"
	"time"
)

// TestChattoneSlowFiresAtFiveSecondsWhileBusy: a rewrite still running when
// the taking-longer delay passes escalates the busy line from "working" to
// "slow". The fake scheduler stands in for the browser's setTimeout; the
// onSlow closure mirrors the wasm adapter's fire path (serial guard, then the
// status selection chattonePaint uses).
func TestChattoneSlowFiresAtFiveSecondsWhileBusy(t *testing.T) {
	p := &chattonePreview{}
	typed := "Please keep these words"
	serial, ok := p.Begin(typed)
	if !ok {
		t.Fatal("begin")
	}
	var scheduled []*scheduledDebounce
	watcher := newChattoneSlowWatcher(func(delay time.Duration, callback func()) debounceTimer {
		entry := &scheduledDebounce{delay: delay, callback: callback, timer: &fakeDebounceTimer{}}
		scheduled = append(scheduled, entry)
		return entry.timer
	})
	fired := 0
	watcher.Begin(func() {
		if p.MarkSlow(serial) {
			fired++
		}
	})
	if len(scheduled) != 1 || scheduled[0].delay != 5*time.Second {
		t.Fatalf("scheduled slow notice = %+v, want one timer at 5s", scheduled)
	}
	if got := chattoneBusyStatus(p); got != "working" {
		t.Fatalf("busy status before the delay = %q, want working", got)
	}
	scheduled[0].callback()
	if fired != 1 {
		t.Fatalf("slow notice fired %d times, want 1", fired)
	}
	if got := chattoneBusyStatus(p); got != "slow" {
		t.Fatalf("busy status after the delay = %q, want slow", got)
	}
	// A repaint after the escalation keeps the notice: the bit survives until
	// the rewrite completes.
	if got := chattoneBusyStatus(p); got != "slow" {
		t.Fatalf("busy status on repaint = %q, want slow", got)
	}
}

// TestChattoneSlowStaleFireAfterCompleteIsNoOp: a notice that fires after the
// rewrite already landed must not escalate anything.
func TestChattoneSlowStaleFireAfterCompleteIsNoOp(t *testing.T) {
	p := &chattonePreview{}
	typed := "Please keep these words"
	serial, ok := p.Begin(typed)
	if !ok {
		t.Fatal("begin")
	}
	var scheduled []*scheduledDebounce
	watcher := newChattoneSlowWatcher(func(delay time.Duration, callback func()) debounceTimer {
		entry := &scheduledDebounce{delay: delay, callback: callback, timer: &fakeDebounceTimer{}}
		scheduled = append(scheduled, entry)
		return entry.timer
	})
	fired := false
	watcher.Begin(func() {
		if p.MarkSlow(serial) {
			fired = true
		}
	})
	if !p.Complete(serial, typed, typed, "Please retain these words") {
		t.Fatal("complete")
	}
	scheduled[0].callback()
	if fired || p.Slow {
		t.Fatal("a fire after completion escalated the finished rewrite")
	}
}

// TestChattoneSlowCancelStopsNotice: completing the rewrite cancels the armed
// notice, so a late fire never reaches the status line.
func TestChattoneSlowCancelStopsNotice(t *testing.T) {
	var scheduled []*scheduledDebounce
	armed := newChattoneSlowWatcher(func(delay time.Duration, callback func()) debounceTimer {
		entry := &scheduledDebounce{delay: delay, callback: callback, timer: &fakeDebounceTimer{}}
		scheduled = append(scheduled, entry)
		return entry.timer
	})
	fired := false
	armed.Begin(func() { fired = true })
	armed.Cancel()
	if !scheduled[0].timer.stopped {
		t.Fatal("cancel did not stop the armed timer")
	}
	scheduled[0].callback()
	if fired {
		t.Fatal("a cancelled notice still fired")
	}
}

// TestChattoneSlowSupersededBeginStaysSilent: a second rewrite on the same
// scope replaces the first notice; only the current rewrite may escalate.
func TestChattoneSlowSupersededBeginStaysSilent(t *testing.T) {
	var scheduled []*scheduledDebounce
	watcher := newChattoneSlowWatcher(func(delay time.Duration, callback func()) debounceTimer {
		entry := &scheduledDebounce{delay: delay, callback: callback, timer: &fakeDebounceTimer{}}
		scheduled = append(scheduled, entry)
		return entry.timer
	})
	var fired []string
	watcher.Begin(func() { fired = append(fired, "first") })
	watcher.Begin(func() { fired = append(fired, "second") })
	if len(scheduled) != 2 {
		t.Fatalf("scheduled notices = %d, want 2", len(scheduled))
	}
	if !scheduled[0].timer.stopped {
		t.Fatal("the second rewrite did not stop the first notice")
	}
	scheduled[0].callback()
	if len(fired) != 0 {
		t.Fatalf("superseded notice fired: %v", fired)
	}
	scheduled[1].callback()
	if len(fired) != 1 || fired[0] != "second" {
		t.Fatalf("current notice fired %v, want [second]", fired)
	}
}

// TestChattoneSlowBitClearedByLifecycle: the escalation belongs to one rewrite;
// every path that ends it (a new rewrite, completion, undo, clearing the
// draft) returns the busy line to "working".
func TestChattoneSlowBitClearedByLifecycle(t *testing.T) {
	p := &chattonePreview{}
	typed := "Please keep these words"
	serial, _ := p.Begin(typed)
	if !p.MarkSlow(serial) || chattoneBusyStatus(p) != "slow" {
		t.Fatal("escalation did not select slow")
	}
	if !p.Complete(serial, typed, typed, "Please retain these words") {
		t.Fatal("complete")
	}
	if p.Slow {
		t.Fatal("completion kept the slow bit")
	}
	serial, _ = p.Begin(typed)
	if !p.MarkSlow(serial) {
		t.Fatal("escalation of the second rewrite failed")
	}
	p.Clear()
	if p.Slow {
		t.Fatal("clearing the draft kept the slow bit")
	}
	serial, _ = p.Begin(typed)
	if !p.MarkSlow(serial) {
		t.Fatal("escalation of the third rewrite failed")
	}
	// A new rewrite after the busy one ends starts unescalated.
	p.Complete(serial, typed, typed, "Please retain these words")
	next, _ := p.Begin("Another draft with words")
	if p.Slow {
		t.Fatal("a new rewrite inherited the slow bit")
	}
	p.Complete(next, "Another draft with words", "Another draft with words", "Another rewrite here")
	if _, ok := p.Undo(); !ok || p.Slow {
		t.Fatal("undo kept the slow bit")
	}
}
