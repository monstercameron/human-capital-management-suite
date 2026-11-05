package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionladder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// uxblind089Rendezvous is a two-party barrier: each side signals on its own
// channel, then waits for the other's signal. Run one after the other, the
// first side waits out its timeout; run together, both proceed.
type uxblind089Rendezvous struct {
	ladder  chan struct{}
	vacancy chan struct{}
	timeout time.Duration
}

func (r *uxblind089Rendezvous) meet(side string, mine chan<- struct{}, other <-chan struct{}) bool {
	select {
	case mine <- struct{}{}:
	case <-time.After(r.timeout):
		return false
	}
	select {
	case <-other:
		return true
	case <-time.After(r.timeout):
		return false
	}
}

// uxblind089LadderDB is never opened: the tenant mapping below answers Nil
// once the vacancy read arrives (the authored ladder), and a mapping the
// vacancy read never meets answers a real UUID so the open fails loudly.
type uxblind089LadderDB struct{ err error }

func (d uxblind089LadderDB) Begin(context.Context) (dbport.Tx, error) {
	return nil, d.err
}

type uxblind089VacancyDirectory struct {
	rendezvous *uxblind089Rendezvous
	err        error
}

func (d *uxblind089VacancyDirectory) Directory(_ context.Context, _ values.TenantId, _ position.AsOf) ([]positionfacts.DirectoryRow, error) {
	if d.err != nil {
		return nil, d.err
	}
	if !d.rendezvous.meet("vacancy", d.rendezvous.vacancy, d.rendezvous.ladder) {
		return nil, errors.New("the vacancy read never overlapped the ladder read")
	}
	return nil, nil
}

func uxblind089ListWorkersEngine(rendezvous *uxblind089Rendezvous) *journeyEngine {
	return &journeyEngine{
		ladder: &promotionLadderSource{reader: promotionladder.Reader{
			DB: uxblind089LadderDB{err: errors.New("uxblind089: the ladder database must not be opened")},
			TenantUUID: func(values.TenantId) uuid.UUID {
				if !rendezvous.meet("ladder", rendezvous.ladder, rendezvous.vacancy) {
					return uuid.New()
				}
				return uuid.Nil
			},
		}},
		positions:      &uxblind089VacancyDirectory{rendezvous: rendezvous},
		positionReader: &vacancyFacts{},
		now:            func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) },
	}
}

// TestTodo_UXBLIND_089_WorkerReadsOverlap is the RED for the ListWorkers leg
// of the cold-load window: the ladder, the vacancy list and the population
// were read one after the other, paying three sequential database round
// trips on every cold load. The reads are independent, so they must overlap;
// the merged answer keeps the exact shape the sequential read produced.
func TestTodo_UXBLIND_089_WorkerReadsOverlap(t *testing.T) {
	rendezvous := &uxblind089Rendezvous{
		ladder:  make(chan struct{}, 1),
		vacancy: make(chan struct{}, 1),
		timeout: 5 * time.Second,
	}
	engine := uxblind089ListWorkersEngine(rendezvous)
	ctx := trust.WithPrincipal(context.Background(), submitPrincipal(t))
	workers, options, err := engine.ListWorkers(ctx)
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	if len(workers) == 0 {
		t.Fatal("ListWorkers answered no workers: the corpus fallback is lost")
	}
	for _, worker := range workers {
		if worker.WorkerRef == "" {
			t.Fatal("ListWorkers answered a worker with no reference")
		}
	}
	if len(options.PositionVacancies) != 0 {
		t.Fatalf("PositionVacancies = %d, want the empty directory's none", len(options.PositionVacancies))
	}
	if len(options.JobCodes) == 0 || len(options.OrgUnits) == 0 {
		t.Fatal("ListWorkers answered no ladder options: the authored ladder is lost")
	}
}

// TestTodo_UXBLIND_089_WorkerReadErrorPrecedence pins the failure contract
// the overlap must preserve: a ladder failure wins over a vacancy failure,
// exactly as the sequential read reported them.
func TestTodo_UXBLIND_089_WorkerReadErrorPrecedence(t *testing.T) {
	newEngine := func(vacancyErr error) *journeyEngine {
		engine := &journeyEngine{
			ladder: &promotionLadderSource{reader: promotionladder.Reader{
				DB:         uxblind089LadderDB{err: errors.New("ladder store down")},
				TenantUUID: func(values.TenantId) uuid.UUID { return uuid.New() },
			}},
			positions:      &uxblind089VacancyDirectory{err: vacancyErr},
			positionReader: &vacancyFacts{},
			now:            func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) },
		}
		return engine
	}
	ctx := trust.WithPrincipal(context.Background(), submitPrincipal(t))

	both := newEngine(errors.New("vacancy store down"))
	if _, _, err := both.ListWorkers(ctx); err == nil || !strings.Contains(err.Error(), "promotion ladder") {
		t.Fatalf("two failing reads reported %v, want the ladder failure first", err)
	}

	vacancyOnly := newEngine(errors.New("vacancy store down"))
	vacancyOnly.ladder = nil
	if _, _, err := vacancyOnly.ListWorkers(ctx); err == nil || !strings.Contains(err.Error(), "position directory") {
		t.Fatalf("a failing vacancy read reported %v, want the vacancy failure", err)
	}
}
