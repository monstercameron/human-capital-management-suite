package main

import (
	"os"
	"runtime/debug"
	"testing"
	"time"
)

// chatperf2Runtime stands in for the Go runtime's collector setting.
type chatperf2Runtime struct{ percent int }

func (r *chatperf2Runtime) set(percent int) int {
	previous := r.percent
	r.percent = percent
	return previous
}

// TestTodo_CHATBUG_014_BootCollector: the collector stays off while the client
// starts, including the stretch after the UI framework has applied its own
// setting, and is turned back on once, to the setting it would have had, a
// moment after the first page is drawn or at the limit.
func TestTodo_CHATBUG_014_BootCollector(t *testing.T) {
	type scheduled struct {
		wait time.Duration
		call func()
	}
	const framework = 300 // GoWebComponents' interactive pacing

	// The ordinary start: the loader starts the client with the collector off,
	// the framework applies its pacing, the client holds it until the page is
	// drawn.
	var timers []scheduled
	runtime := &chatperf2Runtime{percent: -1}
	collector := chatperf2BootCollector{set: runtime.set, after: func(wait time.Duration, call func()) { timers = append(timers, scheduled{wait, call}) }}
	collector.Arm()
	if len(timers) != 1 || timers[0].wait != chatperf2CollectorLimit || runtime.percent != -1 {
		t.Fatalf("starting scheduled %d timers and left the collector at %d; want one timer at the limit and the collector off", len(timers), runtime.percent)
	}
	runtime.set(framework)
	collector.Hold()
	if runtime.percent != -1 {
		t.Fatalf("the collector runs at %d while the page is being drawn", runtime.percent)
	}
	collector.FirstPaint()
	if len(timers) != 2 || timers[1].wait != chatperf2CollectorSettle || runtime.percent != -1 {
		t.Fatalf("the first paint scheduled %d timers and left the collector at %d", len(timers), runtime.percent)
	}
	if chatperf2CollectorSettle >= chatperf2CollectorLimit {
		t.Fatal("the collector resumes later after the first paint than it would without one")
	}
	timers[1].call()
	if runtime.percent != framework {
		t.Fatalf("the collector resumed at %d, want the framework's %d", runtime.percent, framework)
	}
	// It resumes once: the limit's timer, a later page and a later hold change
	// nothing.
	runtime.set(250)
	timers[0].call()
	collector.Resume()
	collector.Hold()
	collector.FirstPaint()
	collector.Arm()
	if runtime.percent != 250 || len(timers) != 2 {
		t.Fatalf("after resuming, the collector was set again (%d) or timers were scheduled (%d)", runtime.percent, len(timers))
	}

	for name, tc := range map[string]struct {
		start    int
		steps    func(c *chatperf2BootCollector, r *chatperf2Runtime)
		want     int
		wantHeld int
	}{
		// A page that never draws still collects: the limit resumes it.
		"the limit alone": {-1, func(c *chatperf2BootCollector, r *chatperf2Runtime) { r.set(framework); c.Hold() }, framework, -1},
		// The framework never initialised: Go's default.
		"no framework": {-1, func(c *chatperf2BootCollector, r *chatperf2Runtime) {}, chatperf2CollectorDefault, -1},
		// The hold came before the framework's setting: the framework's stands.
		"held too early": {-1, func(c *chatperf2BootCollector, r *chatperf2Runtime) { c.Hold(); r.set(framework) }, framework, framework},
		// A loader that did not turn the collector off: held from the framework's
		// setting on, and given back.
		"an older loader": {100, func(c *chatperf2BootCollector, r *chatperf2Runtime) { r.set(framework); c.Hold() }, framework, -1},
	} {
		runtime := &chatperf2Runtime{percent: tc.start}
		var limit func()
		collector := chatperf2BootCollector{set: runtime.set, after: func(_ time.Duration, call func()) { limit = call }}
		collector.Arm()
		tc.steps(&collector, runtime)
		if runtime.percent != tc.wantHeld {
			t.Errorf("%s: the collector is at %d before resuming, want %d", name, runtime.percent, tc.wantHeld)
		}
		limit()
		if runtime.percent != tc.want {
			t.Errorf("%s: the collector resumed at %d, want %d", name, runtime.percent, tc.want)
		}
	}

	// The client's own collector drives the real runtime.
	previous := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previous)
	real := chatperf2BootCollector{set: chatperf2Collector.set, after: func(time.Duration, func()) {}}
	debug.SetGCPercent(framework)
	real.Hold()
	if off := debug.SetGCPercent(-1); off != -1 {
		t.Fatalf("holding left the runtime's collector at %d", off)
	}
	real.Resume()
	if now := debug.SetGCPercent(-1); now != framework {
		t.Fatalf("resuming set the runtime's collector to %d, want %d", now, framework)
	}

	// The browser half, which no native test can run: armed when the client
	// starts, held once the framework is up, resumed after the first paint.
	chatperfInOrder(t, "main", chatperfBody(t, "main_wasm.go", "func main() {"), "bootMark(bootPhaseGoMain)", "chatperf2Collector.Arm()", "start()")
	chatperfInOrder(t, "hydrateProductRouter", chatperfBody(t, "product_wasm.go", "func hydrateProductRouter("), "productRouter.Current()", "chatperf2Collector.Hold()", "ui.Hydrate(initial, rootSelector, options)")
	chatperfInOrder(t, "chatperfReleaseFirstPaint", chatperfBody(t, "chatperf_gate_wasm.go", "func chatperfReleaseFirstPaint() {"), "chatperf2Collector.FirstPaint()", "chatperfFirstPaint.release()")
	raw, err := os.ReadFile("product_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	chatperfInOrder(t, "startProduct", string(raw), "bootMark(bootPhaseHydrateStart)", "hydrateProductRouter(productRouter)", "bootMark(bootPhaseHydrated)", "chatperf2Collector.Hydrated()")

	// A page with no first paint of its own resumes after it is hydrated.
	var waits []time.Duration
	other := chatperf2BootCollector{set: (&chatperf2Runtime{percent: -1}).set, after: func(wait time.Duration, _ func()) { waits = append(waits, wait) }}
	other.Hydrated()
	if len(waits) != 1 || waits[0] != chatperf2CollectorAfterHydrate || chatperf2CollectorAfterHydrate >= chatperf2CollectorLimit {
		t.Fatalf("a hydrated page scheduled %v", waits)
	}
}
