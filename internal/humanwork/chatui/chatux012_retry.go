package chatui

import (
	"strings"
	"sync"
	"time"
)

// CHATUX-012: a read the Chat page makes on load that fails (the server was
// restarting, the network dropped) must heal itself. Every such read goes
// through a ReadRetrier: one loop per read, waiting 1 s, 2 s, 5 s and then 15 s
// between attempts, until the read succeeds or the person has left the thing
// it was for. The page shows nothing about it meanwhile.

// ReadBackoff is the wait before retry number failures+1: 1 s, 2 s, 5 s, then
// every 15 s.
func ReadBackoff(failures int) time.Duration {
	switch {
	case failures <= 0:
		return time.Second
	case failures == 1:
		return 2 * time.Second
	case failures == 2:
		return 5 * time.Second
	}
	return 15 * time.Second
}

type readLoop struct {
	wake    chan struct{}
	stop    chan struct{}
	waiting bool
}

// ReadRetrier keeps at most one retry loop per key, so a failing read never
// multiplies: a second Start for a key that already has a loop does not begin
// another one.
type ReadRetrier struct {
	mu    sync.Mutex
	loops map[string]*readLoop
	// After returns a channel that fires once d has passed. Tests replace it.
	After func(d time.Duration) <-chan time.Time
}

// NewReadRetrier returns a retrier that waits on the real clock.
func NewReadRetrier() *ReadRetrier {
	return &ReadRetrier{loops: map[string]*readLoop{}, After: time.After}
}

// Start retries attempt until it reports true or alive reports false. The first
// attempt waits ReadBackoff(0), because Start is called after an attempt has
// just failed. If a loop for key exists already it is not duplicated: when it is
// waiting its wait is cut short (the person pressed Retry), and when its attempt
// is in flight nothing happens. Start reports whether it began a new loop.
func (r *ReadRetrier) Start(key string, alive func() bool, attempt func() bool) bool {
	if r == nil {
		return false
	}
	if alive == nil {
		alive = func() bool { return true }
	}
	r.mu.Lock()
	if r.loops == nil {
		r.loops = map[string]*readLoop{}
	}
	if existing := r.loops[key]; existing != nil {
		if existing.waiting {
			select {
			case existing.wake <- struct{}{}:
			default:
			}
		}
		r.mu.Unlock()
		return false
	}
	if attempt == nil {
		r.mu.Unlock()
		return false
	}
	loop := &readLoop{wake: make(chan struct{}, 1), stop: make(chan struct{})}
	r.loops[key] = loop
	after := r.After
	if after == nil {
		after = time.After
	}
	r.mu.Unlock()
	go func() {
		defer r.finish(key, loop)
		for failures := 0; ; failures++ {
			if !alive() {
				return
			}
			r.setWaiting(loop, true)
			select {
			case <-after(ReadBackoff(failures)):
			case <-loop.wake:
			case <-loop.stop:
				return
			}
			r.setWaiting(loop, false)
			if !alive() {
				return
			}
			if attempt() {
				return
			}
		}
	}()
	return true
}

func (r *ReadRetrier) setWaiting(loop *readLoop, waiting bool) {
	r.mu.Lock()
	loop.waiting = waiting
	if waiting {
		select {
		case <-loop.wake:
		default:
		}
	}
	r.mu.Unlock()
}

func (r *ReadRetrier) finish(key string, loop *readLoop) {
	r.mu.Lock()
	if r.loops[key] == loop {
		delete(r.loops, key)
	}
	r.mu.Unlock()
}

// Active reports whether key has a loop, waiting or attempting.
func (r *ReadRetrier) Active(key string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loops[key] != nil
}

// Cancel ends the loop for key. An attempt already in flight finishes, but no
// later one starts.
func (r *ReadRetrier) Cancel(key string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	loop := r.loops[key]
	delete(r.loops, key)
	r.mu.Unlock()
	if loop != nil {
		close(loop.stop)
	}
}

// CancelPrefix ends every loop whose key starts with prefix, for example every
// read that belonged to one conversation.
func (r *ReadRetrier) CancelPrefix(prefix string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	var stopped []*readLoop
	for key, loop := range r.loops {
		if strings.HasPrefix(key, prefix) {
			stopped = append(stopped, loop)
			delete(r.loops, key)
		}
	}
	r.mu.Unlock()
	for _, loop := range stopped {
		close(loop.stop)
	}
}
