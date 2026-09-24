package queue

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func testItem(id string, priority Priority) Item {
	return Item{ID: id, Priority: priority, Digest: "digest-" + id}
}

func TestTelemetryBackpressureAndShutdownReturnExactDropAndDegradedState(t *testing.T) {
	pipeline, err := New(Config{Capacity: 2, RetryBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := pipeline.Submit(testItem("critical", PriorityCritical)); err != nil || !result.Accepted {
		t.Fatalf("critical submit result=%+v err=%v", result, err)
	}
	if result, err := pipeline.Submit(testItem("normal", PriorityNormal)); err != nil || !result.Accepted {
		t.Fatalf("normal submit result=%+v err=%v", result, err)
	}
	result, err := pipeline.Submit(testItem("low", PriorityLow))
	if err != nil || !result.Dropped || result.Reason != DropQueueFull {
		t.Fatalf("full submit result=%+v err=%v", result, err)
	}
	attempts := make(map[string]int)
	receipt := pipeline.Shutdown(func(item Item) error {
		attempts[item.ID]++
		if item.ID == "normal" && attempts[item.ID] == 2 {
			return nil
		}
		if item.ID == "normal" {
			return errors.New("temporary exporter stall")
		}
		return nil
	}, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 2, 12, 1, 0, 0, time.UTC))
	if receipt.Status != StatusPartial || receipt.Accepted != 2 || receipt.Exported != 2 || receipt.Dropped != 1 || receipt.Retries != 1 {
		t.Fatalf("receipt=%+v, want partial with one queue drop and one retry", receipt)
	}
	if health := pipeline.Health(); !health.Degraded || health.QueueDepth != 0 || health.DropReasons[DropQueueFull] != 1 {
		t.Fatalf("health=%+v, want degraded with exact queue-full drop", health)
	}
}

func TestTodo_OBS_019(t *testing.T) {
	pipeline, err := New(Config{Capacity: 1, RetryBudget: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Submit(testItem("one", PriorityNormal)); err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Submit(testItem("one", PriorityNormal)); err != nil {
		t.Fatal(err)
	}
	receipt := pipeline.Shutdown(nil, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 2, 12, 1, 0, 0, time.UTC))
	if receipt.Status != StatusPartial || receipt.DropReasons[DropDuplicate] != 1 || receipt.DropReasons[DropExporterUnavailable] != 1 {
		t.Fatalf("receipt=%+v, want duplicate and unavailable drops", receipt)
	}
}

func TestTodo_OBS_019_Property(t *testing.T) {
	pipeline, err := New(Config{Capacity: 3, RetryBudget: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_, _ = pipeline.Submit(testItem(string(rune('a'+i)), PriorityLow))
	}
	if got := pipeline.Health().QueueDepth; got > 3 {
		t.Fatalf("queue depth=%d exceeded capacity", got)
	}
}

func TestTodo_OBS_019_Race(t *testing.T) {
	pipeline, err := New(Config{Capacity: 2, RetryBudget: 0})
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	results := make(chan SubmitResult, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		id := fmt.Sprintf("item-%02d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := pipeline.Submit(testItem(id, PriorityNormal))
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	accepted := 0
	for result := range results {
		if result.Accepted {
			accepted++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if accepted != 2 || pipeline.Health().QueueDepth != 2 {
		t.Fatalf("concurrent capacity admitted %d items, queue depth %d; want 2 each", accepted, pipeline.Health().QueueDepth)
	}
}

func TestTodo_OBS_019_Integration(t *testing.T) {
	pipeline, err := New(Config{Capacity: 1, RetryBudget: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Submit(testItem("one", PriorityNormal)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)
	receipt := pipeline.Shutdown(func(Item) error { return nil }, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), deadline)
	if receipt.Status != StatusTimedOut || receipt.DropReasons[DropShutdownTimeout] != 1 {
		t.Fatalf("receipt=%+v, want timed out shutdown", receipt)
	}
}

func TestTodo_OBS_019_Fault(t *testing.T) {
	pipeline, err := New(Config{Capacity: 1, RetryBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Submit(testItem("one", PriorityCritical)); err != nil {
		t.Fatal(err)
	}
	receipt := pipeline.Shutdown(func(Item) error { return errors.New("stalled") }, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 2, 12, 1, 0, 0, time.UTC))
	if receipt.Status != StatusPartial || receipt.Retries != 1 || receipt.DropReasons[DropRetryExhausted] != 1 {
		t.Fatalf("receipt=%+v", receipt)
	}
}

func TestTodo_OBS_019_Recovery(t *testing.T) {
	pipeline, err := New(Config{Capacity: 1, RetryBudget: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Submit(testItem("one", PriorityNormal)); err != nil {
		t.Fatal(err)
	}
	receipt := pipeline.Shutdown(func(Item) error { return nil }, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 2, 12, 1, 0, 0, time.UTC))
	if receipt.Status != StatusComplete || receipt.QueueAtClose != 0 {
		t.Fatalf("receipt=%+v", receipt)
	}
	if result, err := pipeline.Submit(testItem("after", PriorityNormal)); err != nil || result.Reason != DropShutdownTimeout {
		t.Fatalf("post-shutdown submit result=%+v err=%v", result, err)
	}
}

func BenchmarkTodo_OBS_019(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pipeline, err := New(Config{Capacity: 8, RetryBudget: 1})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := pipeline.Submit(testItem("benchmark", PriorityNormal)); err != nil {
			b.Fatal(err)
		}
		pipeline.Shutdown(func(Item) error { return nil }, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 2, 12, 1, 0, 0, time.UTC))
	}
}
