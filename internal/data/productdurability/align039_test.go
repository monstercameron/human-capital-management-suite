package productdurability

import (
	"errors"
	"testing"
	"time"
)

func align039Schedule(t *testing.T) *RetentionSchedule {
	t.Helper()
	schedule := NewRetentionSchedule()
	for _, rule := range []RetentionRule{
		{Kind: "payroll.record", RetainFor: 7 * 365 * 24 * time.Hour, Then: DisposeDestroy},
		{Kind: "personnel.file", RetainFor: 10 * 365 * 24 * time.Hour, Then: DisposeArchive},
	} {
		if err := schedule.Register(rule); err != nil {
			t.Fatalf("Register(%s): %v", rule.Kind, err)
		}
	}
	return schedule
}

// TestTodo_ALIGN_039 proves retention and disposition on product records:
// unexpired records retain, expired records take their terminal
// disposition, and a legal hold freezes everything regardless of age.
func TestTodo_ALIGN_039(t *testing.T) {
	schedule := align039Schedule(t)
	created := durabilityBase()
	// A young payroll record retains.
	state, action, err := schedule.Classify("payroll.record", created, created.Add(365*24*time.Hour), false)
	if err != nil {
		t.Fatalf("Classify(young): %v", err)
	}
	if state != RecordActive || action != DisposeRetain {
		t.Fatalf("young = %s/%s, want ACTIVE/RETAIN", state, action)
	}
	// An eight-year-old payroll record expires into destruction.
	state, action, err = schedule.Classify("payroll.record", created, created.Add(8*365*24*time.Hour), false)
	if err != nil {
		t.Fatalf("Classify(expired): %v", err)
	}
	if state != RecordExpired || action != DisposeDestroy {
		t.Fatalf("expired = %s/%s, want EXPIRED/DESTROY", state, action)
	}
	// A hold freezes the same record whatever its age.
	state, action, err = schedule.Classify("payroll.record", created, created.Add(8*365*24*time.Hour), true)
	if err != nil {
		t.Fatalf("Classify(held): %v", err)
	}
	if state != RecordHeld || action != DisposeRetain {
		t.Fatalf("held = %s/%s, want HELD/RETAIN", state, action)
	}
}

func TestTodo_ALIGN_039_Property(t *testing.T) {
	schedule := align039Schedule(t)
	created := durabilityBase()
	boundary := created.Add(7 * 365 * 24 * time.Hour)
	// The retention boundary is exact: the boundary instant itself is
	// already expired.
	state, action, err := schedule.Classify("payroll.record", created, boundary, false)
	if err != nil {
		t.Fatal(err)
	}
	if state != RecordExpired || action != DisposeDestroy {
		t.Fatalf("boundary = %s/%s, want EXPIRED/DESTROY", state, action)
	}
	justBefore, _, err := schedule.Classify("payroll.record", created, boundary.Add(-time.Second), false)
	if err != nil {
		t.Fatal(err)
	}
	if justBefore != RecordActive {
		t.Fatalf("before boundary = %s, want ACTIVE", justBefore)
	}
}

func TestTodo_ALIGN_039_Golden(t *testing.T) {
	schedule := align039Schedule(t)
	const wantDigest = "sha256:8b74b91b1429fd413c4cf8582c77f305b85972b9f314de8dd3f7c68dea965146"
	if got := schedule.Digest(); got != wantDigest {
		t.Fatalf("schedule digest=%q want=%q", got, wantDigest)
	}
}

func TestTodo_ALIGN_039_Security(t *testing.T) {
	schedule := align039Schedule(t)
	created := durabilityBase()
	// Kinds without a rule are refused, never defaulted.
	if _, _, err := schedule.Classify("shadow.kind", created, created, false); !errors.Is(err, ErrRetentionUnknown) {
		t.Fatalf("Classify(unknown) = %v, want ErrRetentionUnknown", err)
	}
	// Zero instants are refused before any rule consult.
	if _, _, err := schedule.Classify("payroll.record", time.Time{}, created, false); !errors.Is(err, ErrRetentionInvalid) {
		t.Fatalf("Classify(zero created) = %v, want ErrRetentionInvalid", err)
	}
	// Rules must bound retention and name a terminal disposition.
	for _, rule := range []RetentionRule{
		{Kind: "bad.zero", RetainFor: 0, Then: DisposeDestroy},
		{Kind: "bad.negative", RetainFor: -time.Hour, Then: DisposeArchive},
		{Kind: "bad.retain", RetainFor: time.Hour, Then: DisposeRetain},
		{Kind: "", RetainFor: time.Hour, Then: DisposeDestroy},
	} {
		if err := schedule.Register(rule); !errors.Is(err, ErrRetentionInvalid) {
			t.Fatalf("Register(%+v) = %v, want ErrRetentionInvalid", rule, err)
		}
	}
}

func TestTodo_ALIGN_039_Integration(t *testing.T) {
	schedule := align039Schedule(t)
	journal := NewHistoryJournal()
	created := durabilityBase()
	// The record's creation is a journaled material fact; retention is
	// derived from that chronology, not from a separate clock.
	entry, err := journal.Append(durabilityTenant, "payroll.record", "sha256:run-1", created)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := journal.Replay(durabilityTenant, "payroll.record")
	if err != nil || len(replayed) != 1 {
		t.Fatalf("Replay = %d, %v", len(replayed), err)
	}
	state, action, err := schedule.Classify("payroll.record", replayed[0].RecordedAt, entry.RecordedAt.Add(8*365*24*time.Hour), false)
	if err != nil {
		t.Fatal(err)
	}
	if state != RecordExpired || action != DisposeDestroy {
		t.Fatalf("journaled record = %s/%s, want EXPIRED/DESTROY", state, action)
	}
}

func TestTodo_ALIGN_039_Fault(t *testing.T) {
	schedule := align039Schedule(t)
	created := durabilityBase()
	// A hold blocks destruction a century later.
	state, action, err := schedule.Classify("payroll.record", created, created.Add(100*365*24*time.Hour), true)
	if err != nil {
		t.Fatal(err)
	}
	if state != RecordHeld || action != DisposeRetain {
		t.Fatalf("century-held = %s/%s, want HELD/RETAIN", state, action)
	}
	// An eleven-year-old personnel file archives; a nine-year-old retains.
	state, action, err = schedule.Classify("personnel.file", created, created.Add(11*365*24*time.Hour), false)
	if err != nil {
		t.Fatal(err)
	}
	if state != RecordExpired || action != DisposeArchive {
		t.Fatalf("old file = %s/%s, want EXPIRED/ARCHIVE", state, action)
	}
	state, _, err = schedule.Classify("personnel.file", created, created.Add(9*365*24*time.Hour), false)
	if err != nil {
		t.Fatal(err)
	}
	if state != RecordActive {
		t.Fatalf("young file = %s, want ACTIVE", state)
	}
}

func TestTodo_ALIGN_039_Conformance(t *testing.T) {
	schedule := align039Schedule(t)
	created := durabilityBase()
	// Every registered kind classifies cleanly from birth: no kind is a
	// dead rule.
	for _, kind := range []string{"payroll.record", "personnel.file"} {
		state, action, err := schedule.Classify(kind, created, created.Add(time.Second), false)
		if err != nil {
			t.Fatalf("Classify(%s at birth): %v", kind, err)
		}
		if state != RecordActive || action != DisposeRetain {
			t.Fatalf("%s at birth = %s/%s", kind, state, action)
		}
	}
}

func FuzzTodo_ALIGN_039_Fuzz(f *testing.F) {
	f.Add("payroll.record", int64(0), false)
	f.Fuzz(func(t *testing.T, kind string, ageSeconds int64, held bool) {
		schedule := align039Schedule(t)
		created := durabilityBase()
		now := created.Add(time.Duration(ageSeconds%(100*365*24*3600)) * time.Second)
		firstState, firstAction, firstErr := schedule.Classify(kind, created, now, held)
		secondState, secondAction, secondErr := schedule.Classify(kind, created, now, held)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("classification is not deterministic: %v vs %v", firstErr, secondErr)
		}
		if firstErr == nil && (firstState != secondState || firstAction != secondAction) {
			t.Fatalf("classification diverged: %s/%s vs %s/%s", firstState, firstAction, secondState, secondAction)
		}
	})
}
