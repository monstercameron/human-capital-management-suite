package chatui

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// instantAfter is a clock whose every wait is over at once and is recorded, so
// the schedule can be asserted without sleeping.
type chatux012Clock struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (c *chatux012Clock) after(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.mu.Unlock()
	fired := make(chan time.Time, 1)
	fired <- time.Time{}
	return fired
}

func (c *chatux012Clock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

func chatux012WaitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// A read that fails is tried again 1 s, 2 s, 5 s and then every 15 s after, and
// the first attempt that succeeds ends the loop. The page learns of it through
// the attempt's own state change, with no reload.
func TestTodo_CHATUX_012(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 15 * time.Second, 15 * time.Second}
	for failures, expected := range want {
		if got := ReadBackoff(failures); got != expected {
			t.Fatalf("ReadBackoff(%d) = %v, want %v", failures, got, expected)
		}
	}

	t.Run("a failed read retries and then succeeds", func(t *testing.T) {
		clock := &chatux012Clock{}
		retrier := &ReadRetrier{After: clock.after}
		var attempts atomic.Int32
		landed := make(chan struct{})
		started := retrier.Start("widgets|room-1", nil, func() bool {
			if attempts.Add(1) < 6 {
				return false
			}
			close(landed)
			return true
		})
		if !started {
			t.Fatal("Start reported no loop for a key that had none")
		}
		select {
		case <-landed:
		case <-time.After(5 * time.Second):
			t.Fatal("the read never landed")
		}
		chatux012WaitFor(t, "the loop to end", func() bool { return !retrier.Active("widgets|room-1") })
		if got := attempts.Load(); got != 6 {
			t.Fatalf("attempts = %d, want 6 (5 failures and the success)", got)
		}
		waits := clock.recorded()
		if len(waits) != 6 || waits[0] != time.Second || waits[1] != 2*time.Second || waits[2] != 5*time.Second || waits[3] != 15*time.Second || waits[5] != 15*time.Second {
			t.Fatalf("waits = %v, want 1s 2s 5s then 15s", waits)
		}
	})

	t.Run("a failure is not remembered as an answer", func(t *testing.T) {
		clock := &chatux012Clock{}
		retrier := &ReadRetrier{After: clock.after}
		var attempts atomic.Int32
		// The first read of a key fails and its loop ends because the person
		// left; coming back must read again, not find the key already asked.
		var here atomic.Bool
		here.Store(true)
		retrier.Start("status|room-1", func() bool { return here.Load() }, func() bool {
			attempts.Add(1)
			here.Store(false)
			return false
		})
		chatux012WaitFor(t, "the first loop to end", func() bool { return !retrier.Active("status|room-1") })
		if attempts.Load() != 1 {
			t.Fatalf("attempts = %d, want 1", attempts.Load())
		}
		here.Store(true)
		landed := make(chan struct{})
		if !retrier.Start("status|room-1", func() bool { return here.Load() }, func() bool { close(landed); return true }) {
			t.Fatal("a key whose loop has ended was not allowed to start again")
		}
		select {
		case <-landed:
		case <-time.After(5 * time.Second):
			t.Fatal("the second read never ran")
		}
	})

	t.Run("a loop ends when the person leaves", func(t *testing.T) {
		clock := &chatux012Clock{}
		retrier := &ReadRetrier{After: clock.after}
		var attempts atomic.Int32
		var open atomic.Bool
		open.Store(true)
		retrier.Start("poll|room-1", func() bool { return open.Load() }, func() bool {
			if attempts.Add(1) == 2 {
				open.Store(false)
			}
			return false
		})
		chatux012WaitFor(t, "the loop to end", func() bool { return !retrier.Active("poll|room-1") })
		if got := attempts.Load(); got != 2 {
			t.Fatalf("attempts = %d, want 2: no attempt is made once the person has left", got)
		}
	})

	t.Run("one read in flight per key", func(t *testing.T) {
		clock := &chatux012Clock{}
		retrier := &ReadRetrier{After: clock.after}
		var running, peak, attempts atomic.Int32
		release := make(chan struct{})
		entered := make(chan struct{}, 8)
		attempt := func() bool {
			now := running.Add(1)
			for {
				seen := peak.Load()
				if now <= seen || peak.CompareAndSwap(seen, now) {
					break
				}
			}
			attempts.Add(1)
			entered <- struct{}{}
			<-release
			running.Add(-1)
			return true
		}
		if !retrier.Start("directory|a", nil, attempt) {
			t.Fatal("the first Start did not begin a loop")
		}
		<-entered
		for i := 0; i < 5; i++ {
			if retrier.Start("directory|a", nil, attempt) {
				t.Fatal("a second loop was started for a key that has one in flight")
			}
		}
		// Another read is independent.
		other := make(chan struct{})
		if !retrier.Start("directory|b", nil, func() bool { close(other); return true }) {
			t.Fatal("a different key must get its own loop")
		}
		<-other
		close(release)
		chatux012WaitFor(t, "the loop to end", func() bool { return !retrier.Active("directory|a") })
		if peak.Load() != 1 || attempts.Load() != 1 {
			t.Fatalf("peak in flight = %d, attempts = %d, want 1 and 1", peak.Load(), attempts.Load())
		}
	})

	t.Run("pressing Retry hurries a waiting loop instead of adding one", func(t *testing.T) {
		gate := make(chan time.Time)
		retrier := &ReadRetrier{After: func(time.Duration) <-chan time.Time { return gate }}
		landed := make(chan struct{})
		var attempts atomic.Int32
		retrier.Start("todo|room-1", nil, func() bool { attempts.Add(1); close(landed); return true })
		// The loop waits on a clock that never fires; Start must wake it, once it
		// is waiting, without starting a second loop.
		woken := false
		for i := 0; i < 2000 && !woken; i++ {
			if retrier.Start("todo|room-1", nil, func() bool { attempts.Add(10); return true }) {
				t.Fatal("Start began a second loop for a key that has one")
			}
			select {
			case <-landed:
				woken = true
			case <-time.After(time.Millisecond):
			}
		}
		if !woken || attempts.Load() != 1 {
			t.Fatalf("woken = %v, attempts = %d: the waiting loop's own attempt should have run once", woken, attempts.Load())
		}
	})

	t.Run("cancel ends the loops of a conversation", func(t *testing.T) {
		gate := make(chan time.Time)
		retrier := &ReadRetrier{After: func(time.Duration) <-chan time.Time { return gate }}
		for _, key := range []string{"widgets|room-1", "todo|room-1", "widgets|room-2"} {
			retrier.Start(key, nil, func() bool { return false })
		}
		retrier.CancelPrefix("widgets|")
		if retrier.Active("widgets|room-1") || retrier.Active("widgets|room-2") || !retrier.Active("todo|room-1") {
			t.Fatal("CancelPrefix ended the wrong loops")
		}
		retrier.Cancel("todo|room-1")
		if retrier.Active("todo|room-1") {
			t.Fatal("Cancel left its loop")
		}
	})
}

// While the widgets are being read, and while a read keeps failing, the page
// carries no loading sentence in any language: the widget area is empty, and a
// pinned widget appears once its read lands.
func TestTodo_CHATUX_012_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", locale == "ar")
		loading := m.t(KeyWidgetLoading)
		if loading == "" || loading == KeyWidgetLoading {
			t.Fatalf("%s: the loading copy is missing", locale)
		}
		m.ChannelWidgetsLoading = true
		for _, markup := range []string{renderNode(t, inlineChannelWidgets(m)), renderNode(t, integrate1InlineWidgets(m))} {
			if strings.Contains(markup, loading) || strings.Contains(markup, strings.TrimSuffix(loading, "…")) {
				t.Fatalf("%s: the widget area prints a loading sentence while loading: %s", locale, markup)
			}
			if strings.Contains(markup, `role="alert"`) || strings.Contains(markup, "widget-inline-notice") {
				t.Fatalf("%s: a read in progress must not look like an error: %s", locale, markup)
			}
		}
		// Loaded: the pinned widget shows.
		m.ChannelWidgetsLoading = false
		m.ChannelTeam = ChannelTeamWidget{Revision: 1, Pinned: true, Purpose: "Keep the launch on time"}
		markup := renderNode(t, inlineChannelWidgets(m))
		if !strings.Contains(markup, "Keep the launch on time") || strings.Contains(markup, loading) {
			t.Fatalf("%s: the loaded widget is missing or still loading: %s", locale, markup)
		}
		// A save that failed is still reported, with its Retry.
		m.ChannelWidgetsError = "save"
		m.Callbacks.RetryChannelWidgets = func() {}
		if !strings.Contains(renderNode(t, integrate1InlineWidgets(m)), m.t(KeyWidgetError)) {
			t.Fatalf("%s: a refused save lost its message", locale)
		}
	}
}
