package chatfilter

import (
	"context"
	"errors"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	enWords = strings.Fields("the quarterly plan needs a review before friday and the team wants to ship it soon please send the report about class assessment passage glass of grass at the cocktail party in Essex with Hancock and the assistant from Scunthorpe thanks for the update see you at lunch room 4 or 7 meeting notes attached ok")
	deWords = strings.Fields("die Planung für das Quartal braucht eine Prüfung bis Freitag und das Team möchte sie bald abschließen bitte sende den Bericht über die Klasse an der Kasse mit Spaß und Masse im Schifffahrt Museum danke für die Antwort bis später")
	arWords = strings.Fields("مرحبا كيف حالك اليوم نحتاج الى مراجعة الخطة قبل يوم الجمعة شكرا لك على التقرير عن الأحماض والخرائط والملعب سنلتقي في الغداء بعد العمل")
	hitText = []string{"damn it", "you idiot!", "shut up!", "d@mn", "Verdammt!", "اللعنة!", "اخرس", "sh!t", "Arschloch"}
)

func sentence(rng *rand.Rand, words int) string {
	var b strings.Builder
	pools := [][]string{enWords, deWords, arWords}
	pool := pools[rng.Intn(3)]
	for i := 0; i < words; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(pool[rng.Intn(len(pool))])
		if rng.Intn(14) == 0 {
			b.WriteString([]string{",", ".", "!", "?", ":)"}[rng.Intn(5)])
		}
		if rng.Intn(40) == 0 {
			b.WriteByte(' ')
			b.WriteString(hitText[rng.Intn(len(hitText))])
		}
	}
	return b.String()
}

// TestTodo_CHATMOD_002_Performance: under 2 ms per message at the 95th
// percentile with all nine built-in lists on, through Service.Evaluate, over
// 2000 messages of mixed length in three languages. The store is read once.
func TestTodo_CHATMOD_002_Performance(t *testing.T) {
	svc, store := allBuiltinsOn(t)
	now, _ := fixedClock()
	svc.cacheClock = now
	rng := rand.New(rand.NewSource(7))
	messages := make([]string, 2000)
	for i := range messages {
		switch n := rng.Intn(100); {
		case n < 40:
			messages[i] = sentence(rng, 3+rng.Intn(6))
		case n < 85:
			messages[i] = sentence(rng, 15+rng.Intn(30))
		case n < 97:
			messages[i] = sentence(rng, 100+rng.Intn(120))
		default:
			messages[i] = sentence(rng, 500+rng.Intn(300))
		}
	}
	// warm: the first call reads the store and compiles the nine lists once.
	if _, err := svc.Evaluate(t.Context(), Input{Tenant: "t", Channel: "general", Body: "warm up"}, false); err != nil {
		t.Fatal(err)
	}
	durations := make([]time.Duration, len(messages))
	hits := 0
	for i, m := range messages {
		start := time.Now()
		r, err := svc.Evaluate(t.Context(), Input{Tenant: "t", Channel: "general", Body: m}, false)
		durations[i] = time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Hits) > 0 {
			hits++
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[len(durations)*95/100]
	// The Windows clock ticks every ~0.5 ms, so one call is read as 0 or one
	// tick. Each message is therefore also timed over 8 back-to-back calls and
	// divided, which resolves the real cost to a few tens of microseconds.
	const repeats = 8
	amortized := make([]time.Duration, len(messages))
	for i, m := range messages {
		start := time.Now()
		for r := 0; r < repeats; r++ {
			if _, err := svc.Evaluate(t.Context(), Input{Tenant: "t", Channel: "general", Body: m}, false); err != nil {
				t.Fatal(err)
			}
		}
		amortized[i] = time.Since(start) / repeats
	}
	sort.Slice(amortized, func(i, j int) bool { return amortized[i] < amortized[j] })
	real95, real99 := amortized[len(amortized)*95/100], amortized[len(amortized)*99/100]
	t.Logf("p95 %s (per-call, clock-quantized) and %s (amortized over %d calls), p99 %s, max %s, over %d messages (%d with hits), every built-in list on", p95, real95, repeats, real99, amortized[len(amortized)-1], len(messages), hits)
	if p95 >= 2*time.Millisecond || real95 >= 2*time.Millisecond {
		t.Fatalf("p95 exceeds 2ms: %s per call, %s amortized", p95, real95)
	}
	if hits < 20 {
		t.Fatalf("only %d messages hit; the corpus does not exercise the lists", hits)
	}
	if defs, ens := store.reads(); defs != 1 || ens != 1 {
		t.Fatalf("store read %d/%d times for %d messages", defs, ens, len(messages)+1)
	}
}

// failingStore fails reads on demand.
type failingStore struct {
	*memStore
	mu   sync.Mutex
	fail bool
}

func (f *failingStore) Definitions(ctx context.Context, tenant string) ([]Definition, error) {
	f.mu.Lock()
	fail := f.fail
	f.mu.Unlock()
	if fail {
		return nil, ErrUnavailable
	}
	return f.memStore.Definitions(ctx, tenant)
}

// TestTodo_CHATMOD_002_Cache covers the evaluation cache of Service: at most
// one store read per tenant per three seconds, immediate invalidation by this
// instance's own writes, HasActive without compiling, and behaviour that must
// not change: dry runs do not enforce, channel overrides work in both
// directions, exemptions apply.
func TestTodo_CHATMOD_002_Cache(t *testing.T) {
	ctx := t.Context()
	admin := Actor{Tenant: "t", Subject: "admin"}
	newService := func() (*Service, *memStore, *time.Time, *time.Time) {
		store := newMemStore()
		clock, cacheNow := fixedClock()
		_, serviceNow := fixedClock()
		svc := &Service{Store: store, Registry: NewRegistry(), Authority: fixtureAuthority{workspace: true}, Delivery: &recordingDelivery{}, cacheClock: clock, Now: func() time.Time { return *serviceNow }}
		return svc, store, cacheNow, serviceNow
	}
	judge := func(svc *Service, channel, body string) Result {
		r, err := svc.Evaluate(ctx, Input{Tenant: "t", Channel: channel, Body: body}, false)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	t.Run("one read per window and HasActive compiles nothing", func(t *testing.T) {
		svc, store, cacheNow, _ := newService()
		if on, err := svc.HasActive(ctx, "t", "general"); err != nil || on {
			t.Fatalf("nothing is enabled by default: %v %v", on, err)
		}
		if err := svc.Enable(ctx, admin, Enablement{RuleID: "builtin-en-profanity", Enabled: true}, false); err != nil {
			t.Fatal(err)
		}
		if on, err := svc.HasActive(ctx, "t", "general"); err != nil || !on {
			t.Fatalf("enabled: %v %v", on, err)
		}
		svc.cacheMu.Lock()
		compiled := len(svc.evaluators)
		svc.cacheMu.Unlock()
		if compiled != 0 {
			t.Fatalf("HasActive compiled %d evaluators", compiled)
		}
		d0, e0 := store.reads()
		for i := 0; i < 50; i++ {
			judge(svc, "general", "an ordinary message")
			judge(svc, "random", "another one")
			if on, _ := svc.HasActive(ctx, "t", "general"); !on {
				t.Fatal("HasActive flipped")
			}
		}
		d1, e1 := store.reads()
		if d1 != d0 || e1 != e0 {
			t.Fatalf("store read again inside the window: defs %d->%d, enablements %d->%d", d0, d1, e0, e1)
		}
		*cacheNow = cacheNow.Add(2900 * time.Millisecond)
		judge(svc, "general", "inside the window")
		if d, _ := store.reads(); d != d1 {
			t.Fatal("re-read before the window ended")
		}
		*cacheNow = cacheNow.Add(200 * time.Millisecond)
		judge(svc, "general", "after the window")
		if d, e := store.reads(); d != d1+1 || e != e1+1 {
			t.Fatalf("expected exactly one fresh read, got %d/%d", d-d1, e-e1)
		}
	})

	t.Run("own writes invalidate at once, another instance's within the window", func(t *testing.T) {
		svc, store, cacheNow, _ := newService()
		d := rule("block")
		if err := svc.CreateVersion(ctx, admin, d); err != nil {
			t.Fatal(err)
		}
		if r := judge(svc, "c", "quartz"); len(r.Hits) != 0 {
			t.Fatal("rules are off until enabled")
		}
		if err := svc.Enable(ctx, admin, Enablement{RuleID: d.ID, Enabled: true}, false); err != nil {
			t.Fatal(err)
		}
		if r := judge(svc, "c", "quartz"); r.Action != "block" {
			t.Fatal("Enable did not take effect immediately")
		}
		d.Version, d.Match = "1.1.0", []string{"topaz"}
		if err := svc.CreateVersion(ctx, admin, d); err != nil {
			t.Fatal(err)
		}
		if r := judge(svc, "c", "quartz"); len(r.Hits) != 0 {
			t.Fatal("CreateVersion did not take effect immediately")
		}
		if r := judge(svc, "c", "topaz"); r.Action != "block" {
			t.Fatal("new version not in force")
		}
		if err := svc.Enable(ctx, admin, Enablement{RuleID: d.ID, Enabled: false}, false); err != nil {
			t.Fatal(err)
		}
		if r := judge(svc, "c", "topaz"); len(r.Hits) != 0 {
			t.Fatal("disable did not take effect immediately")
		}
		// another instance enables a second rule behind this one's back.
		o := rule("flag")
		o.ID, o.Match = "other", []string{"opal"}
		store.defs["t"] = append(store.defs["t"], o)
		store.rawEnable("t", Enablement{RuleID: "other", Enabled: true})
		if r := judge(svc, "c", "opal"); len(r.Hits) != 0 {
			t.Fatal("expected the cached view inside the window")
		}
		*cacheNow = cacheNow.Add(3 * time.Second)
		if r := judge(svc, "c", "opal"); r.Action != "flag" {
			t.Fatal("another instance's change not seen after the window")
		}
	})

	t.Run("a failed read is not cached", func(t *testing.T) {
		store := newMemStore()
		fail := &failingStore{memStore: store}
		clock, _ := fixedClock()
		svc := &Service{Store: fail, Registry: NewRegistry(), cacheClock: clock}
		fail.fail = true
		if _, err := svc.Evaluate(ctx, Input{Tenant: "t", Body: "x"}, false); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("store error lost: %v", err)
		}
		if _, err := svc.HasActive(ctx, "t", ""); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("store error lost: %v", err)
		}
		fail.mu.Lock()
		fail.fail = false
		fail.mu.Unlock()
		if _, err := svc.Evaluate(ctx, Input{Tenant: "t", Body: "x"}, false); err != nil {
			t.Fatalf("a failure was cached: %v", err)
		}
		if _, err := (&Service{}).HasActive(ctx, "t", ""); !errors.Is(err, ErrUnavailable) {
			t.Fatal("zero Service must be unavailable, not panic")
		}
	})

	t.Run("dry runs do not enforce, even when the window outlives them", func(t *testing.T) {
		svc, store, _, serviceNow := newService()
		if err := svc.Enable(ctx, admin, Enablement{RuleID: "builtin-en-profanity", Enabled: true}, true); err != nil {
			t.Fatal(err)
		}
		r, err := svc.Evaluate(ctx, Input{Tenant: "t", Channel: "c", Body: "well damn it!"}, true)
		if err != nil || len(r.Hits) == 0 || !r.Hits[0].DryRun || r.Action != "" || r.Refusal() != nil || r.Masked != "well damn it!" {
			t.Fatalf("dry run enforced: %+v %v", r, err)
		}
		if rows := store.records(); len(rows) == 0 || !rows[0].Hit.DryRun {
			t.Fatalf("dry-run hit not recorded as dry: %+v", rows)
		}
		*serviceNow = serviceNow.Add(8 * 24 * time.Hour) // the cache window is unchanged
		r, err = svc.Evaluate(ctx, Input{Tenant: "t", Channel: "c", Body: "well damn it!"}, false)
		if err != nil || r.Action != "block" || r.Refusal() == nil || r.Hits[0].DryRun {
			t.Fatalf("a week later the rule must enforce: %+v %v", r, err)
		}
	})

	t.Run("channel overrides work in both directions and exemptions apply", func(t *testing.T) {
		svc, _, _, _ := newService()
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(svc.Enable(ctx, admin, Enablement{RuleID: "builtin-en-profanity", Enabled: true}, false))
		must(svc.Enable(ctx, admin, Enablement{RuleID: "builtin-en-profanity", Channel: "off", Enabled: false}, false))
		must(svc.Enable(ctx, admin, Enablement{RuleID: "builtin-de-profanity", Channel: "on", Enabled: true, Action: "mask"}, false))
		if r := judge(svc, "general", "damn"); r.Action != "block" {
			t.Fatal("workspace-wide rule missing")
		}
		if r := judge(svc, "off", "damn"); len(r.Hits) != 0 {
			t.Fatal("channel opt-out ignored")
		}
		if r := judge(svc, "general", "verdammt"); len(r.Hits) != 0 {
			t.Fatal("rule off in the workspace applied elsewhere")
		}
		if r := judge(svc, "on", "ach verdammt"); r.Action != "mask" || r.Masked != "ach "+removed {
			t.Fatalf("channel opt-in or its action override ignored: %+v", r)
		}
		for channel, want := range map[string]bool{"general": true, "off": false, "on": true, "": true} {
			if on, err := svc.HasActive(ctx, "t", channel); err != nil || on != want {
				t.Errorf("HasActive(%q) = %v %v, want %v", channel, on, err, want)
			}
		}
		custom := rule("block")
		custom.ID, custom.ExemptRoles = "exempt", []string{"reviewer"}
		must(svc.CreateVersion(ctx, admin, custom))
		must(svc.Enable(ctx, admin, Enablement{RuleID: "exempt", Enabled: true}, false))
		if r, _ := svc.Evaluate(ctx, Input{Tenant: "t", Channel: "x", Body: "quartz", Roles: []string{"reviewer"}}, false); len(r.Hits) != 0 {
			t.Fatal("exempt role judged")
		}
		if r, _ := svc.Evaluate(ctx, Input{Tenant: "t", Channel: "x", Body: "quartz", Roles: []string{"member"}}, false); r.Action != "block" {
			t.Fatal("other role not judged")
		}
	})

	t.Run("concurrent use with writes", func(t *testing.T) {
		svc, _, _, _ := newService()
		var wg sync.WaitGroup
		stop := make(chan struct{})
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					if _, err := svc.Evaluate(ctx, Input{Tenant: "t", Channel: "c", Body: "damn it"}, false); err != nil {
						t.Error(err)
						return
					}
					if _, err := svc.HasActive(ctx, "t", "c"); err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
		for i := 0; i < 200; i++ {
			if err := svc.Enable(ctx, admin, Enablement{RuleID: "builtin-en-profanity", Enabled: i%2 == 0}, false); err != nil {
				t.Fatal(err)
			}
		}
		close(stop)
		wg.Wait()
		// the last write was Enabled=false; it is visible at once.
		if r := judge(svc, "c", "damn it"); len(r.Hits) != 0 {
			t.Fatal("last write not visible")
		}
	})
}
