package clockservice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/worktimerules"
)

type tclockWindowReader struct {
	mu      sync.Mutex
	windows timeprofile.WorkingTimeWindows
	reads   []timeprofile.WorkingTimeWindowRequest
}

func (r *tclockWindowReader) Observe(_ context.Context, req timeprofile.WorkingTimeWindowRequest) (timeprofile.WorkingTimeWindows, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = append(r.reads, req)
	return r.windows, nil
}

var errWindowDecisionConflict = errors.New("window decision conflict")

type tclockWindowDecisions struct {
	mu      sync.Mutex
	seen    map[string]struct{}
	records []string
}

func (d *tclockWindowDecisions) Record(_ context.Context, tenant, key, revision, decision string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = make(map[string]struct{})
	}
	identity := tenant + "\x00" + key + "\x00" + revision
	if _, exists := d.seen[identity]; exists {
		return errWindowDecisionConflict
	}
	d.seen[identity] = struct{}{}
	d.records = append(d.records, decision)
	return nil
}

type tclockWindowReevaluator struct {
	mu    sync.Mutex
	calls []string
}

func (r *tclockWindowReevaluator) Reevaluate(_ context.Context, tenant, key, revision string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, tenant+":"+key+":"+revision)
	return nil
}

func tclockWindowFixture() (WorkingTimeWindowService, *tclockWindowReader, *tclockWindowDecisions, *tclockWindowReevaluator) {
	reader := &tclockWindowReader{windows: timeprofile.WorkingTimeWindows{Windows: worktimerules.Windows{Revision: "r1", DayMinutes: 46 * 60, WeekMinutes: 46 * 60}}}
	decisions := &tclockWindowDecisions{}
	reevaluator := &tclockWindowReevaluator{}
	return WorkingTimeWindowService{Reader: reader, Decisions: decisions, Reevaluate: reevaluator}, reader, decisions, reevaluator
}

// TestTodo_WTIME_007 proves that a workflow receives a tenant/key-scoped
// window and records the exact revision returned by the OBSERVE capability.
func TestTodo_WTIME_007(t *testing.T) {
	service, reader, decisions, reevaluator := tclockWindowFixture()
	observation, err := service.Observe(context.Background(), WorkingTimeWindowReadRequest{TenantID: "tenant-a", AggregationKey: "worker-a", AsOf: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC), ReferencePeriod: 17 * 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if observation.Windows.Revision != "r1" || observation.AggregationKey != "worker-a" || len(reader.reads) != 1 {
		t.Fatalf("observation = %+v reads=%+v", observation, reader.reads)
	}
	if err := service.RecordDecision(context.Background(), observation, "decision-1"); err != nil {
		t.Fatal(err)
	}
	if err := service.NotifyCorrection(context.Background(), "tenant-a", "worker-a", "r2"); err != nil {
		t.Fatal(err)
	}
	if len(decisions.records) != 1 || len(reevaluator.calls) != 1 || reevaluator.calls[0] != "tenant-a:worker-a:r2" {
		t.Fatalf("decision/correction evidence = %v/%v", decisions.records, reevaluator.calls)
	}
}

func TestTodo_WTIME_007_Property(t *testing.T) {
	service, _, _, _ := tclockWindowFixture()
	var revision string
	for i := 0; i < 10; i++ {
		observation, err := service.Observe(context.Background(), WorkingTimeWindowReadRequest{TenantID: "tenant-a", AggregationKey: "worker-a", AsOf: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			revision = observation.Windows.Revision
		} else if observation.Windows.Revision != revision {
			t.Fatalf("trial %d revision = %q, want %q", i, observation.Windows.Revision, revision)
		}
	}
}

func TestTodo_WTIME_007_Race(t *testing.T) {
	service, _, decisions, _ := tclockWindowFixture()
	observation, err := service.Observe(context.Background(), WorkingTimeWindowReadRequest{TenantID: "tenant-a", AggregationKey: "worker-a", AsOf: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- service.RecordDecision(context.Background(), observation, "race-"+string(rune('a'+i)))
		}(i)
	}
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, errWindowDecisionConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	if successes != 1 || conflicts != 31 || len(decisions.records) != 1 {
		t.Fatalf("successes=%d conflicts=%d records=%d", successes, conflicts, len(decisions.records))
	}
}

func TestTodo_WTIME_007_Recovery(t *testing.T) {
	service, reader, _, reevaluator := tclockWindowFixture()
	first, err := service.Observe(context.Background(), WorkingTimeWindowReadRequest{TenantID: "tenant-a", AggregationKey: "worker-a", AsOf: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	reader.mu.Lock()
	reader.windows.Revision = "r2"
	reader.mu.Unlock()
	second, err := service.Observe(context.Background(), WorkingTimeWindowReadRequest{TenantID: "tenant-a", AggregationKey: "worker-a", AsOf: time.Now().UTC()})
	if err != nil || second.Windows.Revision == first.Windows.Revision {
		t.Fatalf("recovered revisions = %q/%q err=%v", first.Windows.Revision, second.Windows.Revision, err)
	}
	if err := service.NotifyCorrection(context.Background(), "tenant-a", "worker-a", second.Windows.Revision); err != nil {
		t.Fatal(err)
	}
	if len(reevaluator.calls) != 1 || reevaluator.calls[0] != "tenant-a:worker-a:r2" {
		t.Fatalf("reevaluation calls = %v", reevaluator.calls)
	}
}
