// TriggerSweep composes the schedule trigger pipeline into the scheduler
// workload: it loads the activated published triggers, calculates the
// occurrences due since the previous sweep, and dispatches each through the
// shared Dispatcher, quarantining anything that cannot dispatch as a dead
// letter. It owns no timer, workflow or provider effects; the dispatch
// converter names intents and the quarantine store holds failures.
package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TriggerSweepConfig is the complete composition of one trigger sweep. Every
// reference field is required: the sweep refuses to run half-composed rather
// than dispatching with a nil store, dispatcher, quarantine, clock, zone or
// scope.
type TriggerSweepConfig struct {
	Registry    *schedule.Registry
	Dispatcher  *schedule.Dispatcher
	Quarantine  *schedule.QuarantineStore
	Lookback    time.Duration
	Misfire     schedule.MisfireConfig
	Zone        values.ZoneRef
	LeaderID    string
	TargetScope []string
}

// SweepReport is one sweep tick accounted: every loaded trigger is either
// swept (its due occurrences dispatched or replayed), skipped (event
// sources have no time occurrences to sweep) or quarantined, and every
// dispatched occurrence either named an intent or replayed a duplicate.
type SweepReport struct {
	SweptAt     time.Time
	Triggers    int
	Occurrences int
	Dispatched  int
	Duplicates  int
	Receipts    int
	Quarantined int
	IntentIDs   []string
	Letters     []schedule.DeadLetter
}

// TriggerSweeper periodically sweeps activated schedule triggers into the
// shared dispatcher. Sequence numbers are process-scoped and strictly
// increasing so the dispatcher's cursor can never see a rewind; intent
// identity stays deterministic because the converter names intents from the
// trigger digest and occurrence key alone.
type TriggerSweeper struct {
	mu         sync.Mutex
	registry   *schedule.Registry
	dispatcher *schedule.Dispatcher
	quarantine *schedule.QuarantineStore
	lookback   time.Duration
	misfire    schedule.MisfireConfig
	zone       values.ZoneRef
	leaderID   string
	scope      []string
	last       time.Time
	seq        uint64
}

// NewTriggerSweeper validates one sweep composition.
func NewTriggerSweeper(config TriggerSweepConfig) (*TriggerSweeper, error) {
	switch {
	case config.Registry == nil:
		return nil, errors.New("application: trigger sweep needs a trigger registry")
	case config.Dispatcher == nil:
		return nil, errors.New("application: trigger sweep needs a dispatcher")
	case config.Quarantine == nil:
		return nil, errors.New("application: trigger sweep needs a quarantine store")
	case config.Lookback <= 0:
		return nil, errors.New("application: trigger sweep needs a positive lookback")
	case config.Zone.ID == "":
		return nil, errors.New("application: trigger sweep needs a calculation zone")
	case config.LeaderID == "":
		return nil, errors.New("application: trigger sweep needs a leader identity")
	case len(config.TargetScope) == 0:
		return nil, errors.New("application: trigger sweep needs a dispatch scope")
	}
	if err := config.Misfire.Validate(); err != nil {
		return nil, fmt.Errorf("application: trigger sweep misfire config: %w", err)
	}
	return &TriggerSweeper{
		registry:   config.Registry,
		dispatcher: config.Dispatcher,
		quarantine: config.Quarantine,
		lookback:   config.Lookback,
		misfire:    config.Misfire,
		zone:       config.Zone,
		leaderID:   config.LeaderID,
		scope:      append([]string(nil), config.TargetScope...),
	}, nil
}

// Sweep loads the activated triggers and dispatches every occurrence due
// since the previous sweep. Per-trigger failures are quarantined, never
// returned: one poisoned trigger must not starve the rest of the sweep.
func (s *TriggerSweeper) Sweep(ctx context.Context, now time.Time) (SweepReport, error) {
	if err := ctx.Err(); err != nil {
		return SweepReport{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	report := SweepReport{SweptAt: now.UTC()}
	start := s.last
	if start.IsZero() {
		start = now.Add(-s.lookback)
	}
	if !now.After(start) {
		return report, nil
	}
	for _, trigger := range s.registry.ListActive() {
		report.Triggers++
		if trigger.Definition.Source.Kind != schedule.SourceCron {
			s.quarantineSource(trigger, now, &report)
			continue
		}
		s.sweepTrigger(trigger, start, now, &report)
	}
	s.last = now
	return report, nil
}

func (s *TriggerSweeper) sweepTrigger(trigger schedule.PublishedTrigger, start, now time.Time, report *SweepReport) {
	result, err := schedule.Calculate(trigger, schedule.OccurrenceRequest{
		Window: schedule.OccurrenceWindow{
			Start: values.NewInstant(start),
			End:   values.NewInstant(now),
		},
		Zone:    s.zone,
		Misfire: s.misfire,
	})
	if err != nil {
		s.quarantineFiring(schedule.Firing{
			Kind:       schedule.FiringOccurrence,
			Key:        "sweep/calculate/" + trigger.Ref().String(),
			Sequence:   s.next(),
			ObservedAt: now,
			Trigger:    trigger,
		}, "occurrence calculation failed", err.Error(), report)
		return
	}
	for _, occurrence := range result.Occurrences {
		report.Occurrences++
		outcome, err := s.dispatcher.Dispatch(schedule.Firing{
			Kind:        schedule.FiringOccurrence,
			Key:         occurrence.Key,
			Sequence:    s.next(),
			ObservedAt:  now,
			Occurrence:  occurrence,
			Trigger:     trigger,
			TargetScope: append([]string(nil), s.scope...),
			Leader:      schedule.LeaderClaim{ID: s.leaderID},
		})
		if err != nil {
			s.quarantineFiring(schedule.Firing{
				Kind:       schedule.FiringOccurrence,
				Key:        occurrence.Key,
				Sequence:   s.next(),
				ObservedAt: now,
				Occurrence: occurrence,
				Trigger:    trigger,
			}, "dispatch failed", err.Error(), report)
			continue
		}
		switch {
		case outcome.Duplicate:
			report.Duplicates++
		case outcome.Created:
			report.Dispatched++
			report.IntentIDs = append(report.IntentIDs, outcome.IntentIDs...)
		default:
			report.Receipts++
		}
	}
}

// quarantineSource quarantines an activated trigger the time sweep cannot
// calculate. Event-source triggers never have time occurrences; keeping one
// activated against a time sweep is a configuration error the dead letter
// names, with a stable key so every later sweep replays it as a duplicate
// instead of minting a new letter.
func (s *TriggerSweeper) quarantineSource(trigger schedule.PublishedTrigger, now time.Time, report *SweepReport) {
	s.quarantineFiring(schedule.Firing{
		Kind:       schedule.FiringEvent,
		Key:        "sweep/source/" + trigger.Ref().String(),
		Sequence:   s.next(),
		ObservedAt: now,
		Trigger:    trigger,
	}, "trigger source has no time occurrences",
		fmt.Sprintf("source %s is not calculable by the time sweep", trigger.Definition.Source.Kind), report)
}

func (s *TriggerSweeper) quarantineFiring(firing schedule.Firing, reason, failure string, report *SweepReport) {
	letter, err := s.quarantine.Quarantine(schedule.QuarantineRequest{
		Firing:  firing,
		Reason:  reason,
		Failure: failure,
		Attempt: 1,
	})
	if err != nil {
		slog.Error("trigger sweep could not quarantine a failed firing", "key", firing.Key, "error", err)
		return
	}
	report.Quarantined++
	report.Letters = append(report.Letters, letter)
}

func (s *TriggerSweeper) next() uint64 {
	s.seq++
	return s.seq
}

// Run sweeps every interval until the context ends. Sweep failures are
// per-trigger quarantined inside Sweep; a Run-level error only means the
// context ended, which is a clean shutdown, not a failure.
func (s *TriggerSweeper) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if _, err := s.Sweep(ctx, now); err != nil {
				slog.Error("trigger sweep tick failed", "error", err)
			}
		}
	}
}
