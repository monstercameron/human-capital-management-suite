package runtime_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func testTenant023() uuid.UUID {
	return uuid.MustParse("02300000-0230-0230-0230-023002300230")
}

func TestTodo_WF_EXT_023(t *testing.T) {
	tenant := testTenant023()
	base := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	reg := runtime.NewTriggerRegistry()

	date := runtime.TriggerSource{
		ID: "period-close", Kind: runtime.TriggerSourceDate,
		WorkflowID: "period.close", IntentType: "period.close",
		TenantID: tenant, At: base.Add(2 * time.Hour),
	}
	sched := runtime.TriggerSource{
		ID: "window-open", Kind: runtime.TriggerSourceSchedule,
		WorkflowID: "window.open", IntentType: "window.open",
		TenantID: tenant, Anchor: base, Every: time.Hour,
	}
	event := runtime.TriggerSource{
		ID: "offer-accepted", Kind: runtime.TriggerSourceEvent,
		WorkflowID: "offer.accept", IntentType: "offer.accept",
		TenantID: tenant, EventType: "offer.accepted",
	}
	oblig := runtime.TriggerSource{
		ID: "continuation", Kind: runtime.TriggerSourceObligation,
		WorkflowID: "continuation.cover", IntentType: "continuation.cover",
		TenantID: tenant, ObligationType: "continuation.coverage",
	}
	for _, src := range []runtime.TriggerSource{date, sched, event, oblig} {
		if err := reg.Register(src); err != nil {
			t.Fatalf("register %s: %v", src.ID, err)
		}
	}

	red := map[string]runtime.TriggerSource{
		"duplicate id":            date,
		"empty workflow":          {ID: "x1", Kind: runtime.TriggerSourceDate, IntentType: "i", TenantID: tenant, At: base},
		"empty intent":            {ID: "x2", Kind: runtime.TriggerSourceDate, WorkflowID: "w", TenantID: tenant, At: base},
		"empty tenant":            {ID: "x3", Kind: runtime.TriggerSourceDate, WorkflowID: "w", IntentType: "i", At: base},
		"date without at":         {ID: "x4", Kind: runtime.TriggerSourceDate, WorkflowID: "w", IntentType: "i", TenantID: tenant},
		"schedule without every":  {ID: "x5", Kind: runtime.TriggerSourceSchedule, WorkflowID: "w", IntentType: "i", TenantID: tenant, Anchor: base},
		"schedule without anchor": {ID: "x6", Kind: runtime.TriggerSourceSchedule, WorkflowID: "w", IntentType: "i", TenantID: tenant, Every: time.Hour},
		"event without type":      {ID: "x7", Kind: runtime.TriggerSourceEvent, WorkflowID: "w", IntentType: "i", TenantID: tenant},
		"obligation without type": {ID: "x8", Kind: runtime.TriggerSourceObligation, WorkflowID: "w", IntentType: "i", TenantID: tenant},
		"unknown kind":            {ID: "x9", Kind: "CRON", WorkflowID: "w", IntentType: "i", TenantID: tenant},
	}
	for name, src := range red {
		if err := reg.Register(src); err == nil {
			t.Errorf("register %s: expected refusal, got nil", name)
		}
	}

	if fires := reg.Sweep(tenant, base, nil); len(fires) != 0 {
		t.Fatalf("early sweep: got %d fires, want 0", len(fires))
	}

	fires := reg.Sweep(tenant, base.Add(2*time.Hour), nil)
	if len(fires) != 3 {
		t.Fatalf("due sweep: got %d fires, want 3 (date + 2 schedule occurrences)", len(fires))
	}
	byID := map[string]runtime.TriggerFire{}
	for _, f := range fires {
		byID[f.SourceID] = f
		if f.Key == "" {
			t.Errorf("fire %s carries no occurrence key", f.SourceID)
		}
		src := f.StartSource()
		if src.Kind != runtime.StartSourceTrigger {
			t.Errorf("fire %s: source kind %q, want TRIGGER", f.SourceID, src.Kind)
		}
		if err := src.Validate(); err != nil {
			t.Errorf("fire %s: start source invalid: %v", f.SourceID, src.Kind)
		}
		if src.Trigger == nil || src.Trigger.TriggerID != f.SourceID || src.Trigger.Key != f.Key {
			t.Errorf("fire %s: start source does not round-trip fire identity", f.SourceID)
		}
	}
	if byID["period-close"].FiredAt != base.Add(2*time.Hour) {
		t.Errorf("date fire time = %v, want occurrence not sweep time", byID["period-close"].FiredAt)
	}
	if again := reg.Sweep(tenant, base.Add(2*time.Hour), nil); len(again) != 0 {
		t.Fatalf("repeat sweep: got %d fires, want at-most-once 0", len(again))
	}

	ef := reg.FireEvent(tenant, "offer.accepted", "evt-1", map[string]string{"offer": "o-9"}, base.Add(3*time.Hour))
	if len(ef) != 1 {
		t.Fatalf("event fire: got %d, want 1", len(ef))
	}
	if ef[0].Facts["offer"] != "o-9" {
		t.Errorf("event facts lost: %v", ef[0].Facts)
	}
	if dup := reg.FireEvent(tenant, "offer.accepted", "evt-1", nil, base.Add(3*time.Hour)); len(dup) != 0 {
		t.Fatalf("event redelivery: got %d fires, want 0", len(dup))
	}
	if none := reg.FireEvent(tenant, "offer.declined", "evt-9", nil, base.Add(3*time.Hour)); len(none) != 0 {
		t.Fatalf("unregistered event type: got %d fires, want 0", len(none))
	}

	tracker := runtime.NewObligationTracker()
	ob := runtime.OpenObligation{
		Type: "continuation.coverage", Key: "run-7/continuation",
		Facts:     map[string]string{"subject": "person-1"},
		EmittedBy: "run-7", EmittedAt: base.Add(3 * time.Hour),
	}
	if err := tracker.Emit(ob); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if err := tracker.Emit(ob); err == nil {
		t.Errorf("duplicate emit: expected refusal, got nil")
	}
	if err := tracker.Emit(runtime.OpenObligation{Type: "t"}); err == nil {
		t.Errorf("empty obligation key: expected refusal, got nil")
	}
	of := reg.Sweep(tenant, base.Add(3*time.Hour), tracker)
	if len(of) != 2 {
		t.Fatalf("obligation sweep: got %d fires, want 2 (obligation + schedule catch-up)", len(of))
	}
	var obFire *runtime.TriggerFire
	for i := range of {
		if of[i].SourceID == "continuation" {
			obFire = &of[i]
		}
	}
	if obFire == nil {
		t.Fatal("obligation sweep: no fire for the continuation source")
	}
	if obFire.Facts["subject"] != "person-1" {
		t.Errorf("obligation facts lost: %v", obFire.Facts)
	}
	if err := obFire.StartSource().Validate(); err != nil {
		t.Errorf("obligation start source invalid: %v", err)
	}
	if again := reg.Sweep(tenant, base.Add(3*time.Hour), tracker); len(again) != 0 {
		t.Fatalf("repeat obligation sweep: got %d fires, want 0", len(again))
	}
}

func TestTodo_WF_EXT_023_Property(t *testing.T) {
	tenant := testTenant023()
	base := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	reg := runtime.NewTriggerRegistry()
	tracker := runtime.NewObligationTracker()
	sources := []runtime.TriggerSource{
		{ID: "p-date", Kind: runtime.TriggerSourceDate, WorkflowID: "w", IntentType: "i", TenantID: tenant, At: base.Add(30 * time.Hour)},
		{ID: "p-sched", Kind: runtime.TriggerSourceSchedule, WorkflowID: "w", IntentType: "i", TenantID: tenant, Anchor: base, Every: 7 * time.Hour},
		{ID: "p-event", Kind: runtime.TriggerSourceEvent, WorkflowID: "w", IntentType: "i", TenantID: tenant, EventType: "e.t"},
		{ID: "p-oblig", Kind: runtime.TriggerSourceObligation, WorkflowID: "w", IntentType: "i", TenantID: tenant, ObligationType: "o.t"},
	}
	for _, src := range sources {
		if err := reg.Register(src); err != nil {
			t.Fatal(err)
		}
	}
	rng := rand.New(rand.NewSource(23))
	seen := map[string]int{}
	now := base
	record := func(fires []runtime.TriggerFire) {
		t.Helper()
		var last time.Time
		lastKey := ""
		first := true
		for _, f := range fires {
			seen[f.SourceID+"\x00"+f.Key]++
			if !first && (f.FiredAt.Before(last) || (f.FiredAt.Equal(last) && f.Key < lastKey)) {
				t.Fatalf("sweep output not deterministic: %+v after %v/%q", f, last, lastKey)
			}
			first, last, lastKey = false, f.FiredAt, f.Key
		}
	}
	for i := 0; i < 4000; i++ {
		switch rng.Intn(5) {
		case 0, 1:
			now = now.Add(time.Duration(rng.Intn(10)) * time.Hour)
			record(reg.Sweep(tenant, now, tracker))
		case 2:
			record(reg.FireEvent(tenant, "e.t", string(rune('a'+rng.Intn(4))), nil, now))
		case 3:
			_ = tracker.Emit(runtime.OpenObligation{
				Type: "o.t", Key: string(rune('a' + rng.Intn(4))),
				EmittedBy: "test", EmittedAt: now,
			})
		case 4:
			record(reg.Sweep(tenant, now, tracker))
		}
	}
	for key, n := range seen {
		if n != 1 {
			t.Fatalf("occurrence %q fired %d times, want at most once", key, n)
		}
	}
	if len(seen) == 0 {
		t.Fatal("property run fired nothing")
	}
}

func TestTodo_WF_EXT_023_Recovery(t *testing.T) {
	tenant := testTenant023()
	base := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	reg := runtime.NewTriggerRegistry()
	if err := reg.Register(runtime.TriggerSource{
		ID: "r-date", Kind: runtime.TriggerSourceDate,
		WorkflowID: "w", IntentType: "i", TenantID: tenant, At: base.Add(90 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(runtime.TriggerSource{
		ID: "r-sched", Kind: runtime.TriggerSourceSchedule,
		WorkflowID: "w", IntentType: "i", TenantID: tenant, Anchor: base, Every: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	if fires := reg.Sweep(tenant, base, nil); len(fires) != 0 {
		t.Fatalf("pre-downtime sweep: got %d, want 0", len(fires))
	}
	// Downtime: no sweep for three hours. Recovery must fire every missed
	// occurrence exactly once, oldest first.
	missed := reg.Sweep(tenant, base.Add(3*time.Hour), nil)
	if len(missed) != 4 {
		t.Fatalf("recovery sweep: got %d fires, want 4 (1 date + 3 schedule)", len(missed))
	}
	for i := 1; i < len(missed); i++ {
		if missed[i].FiredAt.Before(missed[i-1].FiredAt) {
			t.Fatalf("recovery fires out of order: %v before %v", missed[i].FiredAt, missed[i-1].FiredAt)
		}
	}
	if again := reg.Sweep(tenant, base.Add(3*time.Hour), nil); len(again) != 0 {
		t.Fatalf("post-recovery sweep: got %d, want 0", len(again))
	}
}

func TestTodo_WF_EXT_023_CatchUpResumes(t *testing.T) {
	tenant := testTenant023()
	base := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	reg := runtime.NewTriggerRegistry()
	if err := reg.Register(runtime.TriggerSource{
		ID: "capped", Kind: runtime.TriggerSourceSchedule,
		WorkflowID: "w", IntentType: "i", TenantID: tenant, Anchor: base, Every: time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	now := base.Add(2000 * time.Minute)
	first := reg.Sweep(tenant, now, nil)
	if len(first) != runtime.MaxTriggerCatchUp {
		t.Fatalf("first sweep: got %d fires, want the %d-occurrence cap", len(first), runtime.MaxTriggerCatchUp)
	}
	second := reg.Sweep(tenant, now, nil)
	if len(second) != 2000-runtime.MaxTriggerCatchUp {
		t.Fatalf("second sweep: got %d fires, want the remaining %d", len(second), 2000-runtime.MaxTriggerCatchUp)
	}
	seen := map[string]time.Time{}
	for _, f := range append(append([]runtime.TriggerFire{}, first...), second...) {
		if prev, dup := seen[f.Key]; dup {
			t.Fatalf("occurrence %q fired twice (at %v and %v)", f.Key, prev, f.FiredAt)
		}
		seen[f.Key] = f.FiredAt
	}
	if len(seen) != 2000 {
		t.Fatalf("got %d distinct occurrences, want 2000", len(seen))
	}
	for n := 1; n <= 2000; n++ {
		occ := base.Add(time.Duration(n) * time.Minute)
		key := "capped@" + occ.UTC().Format(time.RFC3339Nano)
		got, ok := seen[key]
		if !ok {
			t.Fatalf("occurrence n=%d (%s) never fired", n, key)
		}
		if !got.Equal(occ) {
			t.Fatalf("occurrence %s fired at %v, want the occurrence time %v", key, got, occ)
		}
	}
	if third := reg.Sweep(tenant, now, nil); len(third) != 0 {
		t.Fatalf("third sweep: got %d fires, want at-most-once 0", len(third))
	}
}
