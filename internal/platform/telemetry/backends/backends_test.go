package backends

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func testSignal(id, tenant string, kind Kind) Signal {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	return Signal{ID: id, TenantToken: tenant, Kind: kind, Region: "us-east", Digest: "digest-" + id, ObservedAt: now, RetainUntil: now.Add(time.Hour), Bytes: 10}
}

func TestTodo_OBS_003(t *testing.T) {
	stack, qualification, err := Default()
	if err != nil || !qualification.Complete {
		t.Fatalf("Default() stack=%v qualification=%+v err=%v", stack, qualification, err)
	}
	now := time.Date(2026, 9, 2, 12, 30, 0, 0, time.UTC)
	if err := stack.Ingest(testSignal("metric-1", "tenant-token-a", KindMetrics)); err != nil {
		t.Fatal(err)
	}
	if got := stack.Query("tenant-token-b", KindMetrics, now); len(got) != 0 {
		t.Fatalf("cross-tenant query returned %v", got)
	}
	if got := stack.Query("tenant-token-a", KindMetrics, now); len(got) != 1 {
		t.Fatalf("tenant query returned %d signals, want one", len(got))
	}
}

func TestTodo_OBS_003_Race(t *testing.T) {
	stack, _, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	signal := testSignal("same", "tenant-token-a", KindLogs)
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- stack.Ingest(signal)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := stack.Query(signal.TenantToken, signal.Kind, time.Time{}); len(got) != 1 || got[0].Digest != signal.Digest {
		t.Fatalf("concurrent duplicate ingest produced %#v", got)
	}
}

func TestTodo_OBS_003_Integration(t *testing.T) {
	stack, _, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	signal := testSignal("trace-1", "tenant-token-a", KindTraces)
	if err := stack.Ingest(signal); err != nil {
		t.Fatal(err)
	}
	restored, _, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(stack.ExportSnapshot()); err != nil {
		t.Fatal(err)
	}
	if got := restored.Query(signal.TenantToken, signal.Kind, time.Time{}); len(got) != 1 || got[0].Digest != signal.Digest {
		t.Fatalf("restore result=%v", got)
	}
}

func TestTodo_OBS_003_Recovery(t *testing.T) {
	specs := DefaultSpecs()
	specs[0].Private = false
	if _, qualification, err := New(specs); !errors.Is(err, ErrInvalidSpec) || qualification.Complete {
		t.Fatalf("unsafe stack qualification=%+v err=%v", qualification, err)
	}
	stack, _, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	signal := testSignal("expired", "tenant-token-a", KindMetrics)
	signal.RetainUntil = signal.ObservedAt.Add(-time.Second)
	if !errors.Is(stack.Ingest(signal), ErrExpired) {
		t.Fatal("expired signal was accepted")
	}
}

func TestTodo_OBS_003_QualificationRejectsIncompatibleBackendContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*[]Spec)
	}{
		{name: "protocol", mutate: func(specs *[]Spec) { (*specs)[0].Protocol = "otlp-http" }},
		{name: "license", mutate: func(specs *[]Spec) { (*specs)[0].License = "AGPL-3.0" }},
		{name: "kind", mutate: func(specs *[]Spec) { *specs = append(*specs, Spec{Kind: Kind("UNKNOWN")}) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			specs := DefaultSpecs()
			tc.mutate(&specs)
			if _, qualification, err := New(specs); !errors.Is(err, ErrInvalidSpec) || qualification.Complete {
				t.Fatalf("incompatible specs qualification=%+v err=%v", qualification, err)
			}
		})
	}
}

func TestTodo_OBS_003_RetentionBoundUsesObservedAt(t *testing.T) {
	stack, _, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	signal := testSignal("retention-bound", "tenant-token-a", KindMetrics)
	signal.RetainUntil = signal.ObservedAt.Add(DefaultSpecs()[0].Retention + time.Second)
	if !errors.Is(stack.Ingest(signal), ErrExpired) {
		t.Fatal("signal retained beyond backend retention was accepted")
	}
	if got := stack.Query(signal.TenantToken, signal.Kind, time.Time{}); len(got) != 0 {
		t.Fatalf("rejected signal was stored: %v", got)
	}
}

func TestTodo_OBS_003_TenantScopedSignalIDs(t *testing.T) {
	stack, _, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	first := testSignal("shared-id", "tenant-token-a", KindMetrics)
	second := testSignal("shared-id", "tenant-token-b", KindMetrics)
	if err := stack.Ingest(first); err != nil {
		t.Fatal(err)
	}
	if err := stack.Ingest(second); err != nil {
		t.Fatal(err)
	}
	if got := stack.Query(first.TenantToken, first.Kind, time.Time{}); len(got) != 1 {
		t.Fatalf("first tenant signals=%v", got)
	}
	if got := stack.Query(second.TenantToken, second.Kind, time.Time{}); len(got) != 1 {
		t.Fatalf("second tenant signals=%v", got)
	}
}

func TestTodo_OBS_003_RestoreIsAtomic(t *testing.T) {
	stack, _, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	original := testSignal("original", "tenant-token-a", KindMetrics)
	if err := stack.Ingest(original); err != nil {
		t.Fatal(err)
	}
	replacement := testSignal("replacement", "tenant-token-a", KindMetrics)
	invalid := testSignal("invalid", "tenant-token-a", KindMetrics)
	invalid.RetainUntil = invalid.ObservedAt.Add(DefaultSpecs()[0].Retention + time.Second)
	if !errors.Is(stack.Restore(Snapshot{Signals: []Signal{replacement, invalid}}), ErrExpired) {
		t.Fatal("restore with invalid signal succeeded")
	}
	if got := stack.Query(original.TenantToken, original.Kind, time.Time{}); len(got) != 1 || got[0].ID != original.ID {
		t.Fatalf("original state after failed restore=%v", got)
	}
}
