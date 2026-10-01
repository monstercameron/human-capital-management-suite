package application_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/availability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func servedAvailabilityRef(kind values.Kind, id string) values.EntityRef {
	canonicalIDs := map[string]string{
		"availability-1": "00000000-0000-4000-8000-000000000001",
		"worker-1":       "00000000-0000-4000-8000-000000000002",
		"employment-1":   "00000000-0000-4000-8000-000000000003",
		"source-1":       "00000000-0000-4000-8000-000000000004",
		"assignment-1":   "00000000-0000-4000-8000-000000000005",
		"impact-1":       "00000000-0000-4000-8000-000000000006",
		"schedule-1":     "00000000-0000-4000-8000-000000000007",
	}
	return values.EntityRef{Tenant: "avail-k1", Kind: kind, Id: canonicalIDs[id]}
}

func servedAvailabilityRevision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func servedAvailabilityInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	from, err := values.ParseLocalDate(start)
	if err != nil {
		t.Fatal(err)
	}
	to, err := values.ParseLocalDate(end)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(from, to, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func servedWorkerAvailability(t *testing.T, state availability.State) availability.WorkerAvailability {
	t.Helper()
	revision := servedAvailabilityRevision(t, "availability/worker-1", 1)
	return availability.WorkerAvailability{
		AvailabilityID: servedAvailabilityRef("availability", "availability-1"),
		Revision:       revision,
		Worker:         servedAvailabilityRef(availability.KindWorker, "worker-1"),
		Employment:     servedAvailabilityRef(availability.KindEmployment, "employment-1"),
		Scope:          availability.WorkerScope,
		State:          state,
		Reason:         "worker supplied fact",
		Effective:      servedAvailabilityInterval(t, "2026-04-01", "2026-04-02"),
		Source: availability.Source{
			Authority:   servedAvailabilityRef("source", "source-1"),
			Revision:    revision,
			TimezoneID:  "America/New_York",
			CalendarRef: "us-federal@2026",
		},
	}
}

// TestTodo_AVAIL_001 proves the contracts are reachable through application,
// the package composed by cmd/hcmnext, rather than only through unit imports.
func TestTodo_AVAIL_001(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	if surface.ValidateWorkerAvailability == nil || surface.ValidateAbsenceImpact == nil {
		t.Fatal("served availability surface omitted contract validators")
	}
	if err := surface.ValidateWorkerAvailability(servedWorkerAvailability(t, availability.Available)); err != nil {
		t.Fatalf("served availability contract rejected: %v", err)
	}

	assignment := servedAvailabilityRef(availability.KindAssignment, "assignment-1")
	impact := availability.AbsenceImpact{
		ImpactID:          servedAvailabilityRef("absence_impact", "impact-1"),
		Worker:            servedAvailabilityRef(availability.KindWorker, "worker-1"),
		Employment:        servedAvailabilityRef(availability.KindEmployment, "employment-1"),
		Assignment:        assignment,
		RequestedInterval: servedAvailabilityInterval(t, "2026-05-01", "2026-05-02"),
		Approval:          availability.AbsenceRequested,
		Reason:            "requested leave",
		ScheduleSource: availability.Source{
			Authority:   servedAvailabilityRef("schedule", "schedule-1"),
			Revision:    servedAvailabilityRevision(t, "schedule/assignment-1", 1),
			TimezoneID:  "America/New_York",
			CalendarRef: "us-federal@2026",
		},
	}
	if err := surface.ValidateAbsenceImpact(impact); err != nil {
		t.Fatalf("served absence impact contract rejected: %v", err)
	}
}

func TestTodo_AVAIL_001_Property(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	for _, state := range []availability.State{availability.Available, availability.Unavailable, availability.Restricted, availability.Unknown} {
		t.Run(string(state), func(t *testing.T) {
			if err := surface.ValidateWorkerAvailability(servedWorkerAvailability(t, state)); err != nil {
				t.Fatalf("state %s rejected: %v", state, err)
			}
		})
	}
}

func TestTodo_AVAIL_001_Conformance(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	fact := servedWorkerAvailability(t, availability.Available)
	fact.State = availability.State("ACTIVE")
	if err := surface.ValidateWorkerAvailability(fact); err == nil {
		t.Fatal("employment-like state accepted by served availability contract")
	}
}

func TestTodo_AVAIL_001_Mutation(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	fact := servedWorkerAvailability(t, availability.Available)
	fact.Source.TimezoneID = ""
	if err := surface.ValidateWorkerAvailability(fact); err == nil {
		t.Fatal("availability without source timezone accepted by served contract")
	}
}

func servedVersionedAbsenceRequest(t *testing.T) availability.AbsenceWindowRequest {
	t.Helper()
	worker := servedAvailabilityRef(availability.KindWorker, "worker-1")
	employment := servedAvailabilityRef(availability.KindEmployment, "employment-1")
	assignment := servedAvailabilityRef(availability.KindAssignment, "assignment-1")
	revision := servedAvailabilityRevision(t, "schedule/assignment-1", 4)
	schedule := availability.VersionedWorkSchedule{
		ScheduleID: servedAvailabilityRef("schedule", "schedule-1"),
		Assignment: assignment,
		Revision:   revision,
		Calendar:   values.CalendarRef{Ref: "us-federal", Version: "2026"},
		Timezone:   values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		WorkingWeekdays: map[time.Weekday]bool{
			time.Monday: true,
		},
		Shifts: []availability.WorkShift{{
			ID:             "shift-1",
			Date:           func() values.LocalDate { d, _ := values.ParseLocalDate("2026-03-09"); return d }(),
			Start:          func() values.LocalTime { v, _ := values.ParseLocalTime("09:00:00"); return v }(),
			End:            func() values.LocalTime { v, _ := values.ParseLocalTime("17:00:00"); return v }(),
			Disambiguation: values.DisambiguationRejectGap,
		}},
	}
	return availability.AbsenceWindowRequest{
		Worker: worker, Employment: employment, Assignment: assignment,
		Window:          servedAvailabilityInterval(t, "2026-03-09", "2026-03-10"),
		JurisdictionRef: "US-NY", Schedule: schedule,
		ExpectedScheduleRevision: revision,
	}
}

func TestTodo_AVAIL_002(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	if surface.SimulateVersionedAbsence == nil {
		t.Fatal("served availability surface omitted versioned absence simulation")
	}
	result, err := surface.SimulateVersionedAbsence(servedVersionedAbsenceRequest(t))
	if err != nil {
		t.Fatalf("served absence simulation: %v", err)
	}
	if result.Hours != 8 || result.Coverage != availability.CoverageDemandCreated || !result.Verify() {
		t.Fatalf("served simulation result = %+v", result)
	}
}

func TestTodo_AVAIL_002_Property(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	req := servedVersionedAbsenceRequest(t)
	first, err := surface.SimulateVersionedAbsence(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := surface.SimulateVersionedAbsence(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || first.Hours != second.Hours {
		t.Fatalf("simulation was not deterministic: first=%+v second=%+v", first, second)
	}
}

func TestTodo_AVAIL_002_Golden(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	result, err := surface.SimulateVersionedAbsence(servedVersionedAbsenceRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest == "" || !result.Verify() {
		t.Fatalf("served simulation lost its immutable digest: %+v", result)
	}
}

func TestTodo_AVAIL_002_Conformance(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	req := servedVersionedAbsenceRequest(t)
	req.ExpectedScheduleRevision = servedAvailabilityRevision(t, "schedule/assignment-1", 5)
	if _, err := surface.SimulateVersionedAbsence(req); !errors.Is(err, availability.ErrAbsenceWindowStale) {
		t.Fatalf("stale schedule error = %v", err)
	}
}

func servedRevisionRequest() availability.Revision {
	return availability.Revision{
		WorkerID: "worker-1", State: availability.Unavailable,
		IntentID: "intent:leave:worker-1", ProposalDigest: "sha256:proposal",
		Reason: "leave-start", LeaveEventID: "leave:worker-1:start", EffectiveStart: 10, EffectiveEnd: 17,
	}
}

func TestTodo_AVAIL_003(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	if surface.NewRevisionLog == nil || surface.PrepareRevision == nil || surface.CommitRevision == nil {
		t.Fatal("served availability surface omitted owning revision transaction")
	}
	log := surface.NewRevisionLog()
	token, err := surface.PrepareRevision(log, servedRevisionRequest())
	if err != nil {
		t.Fatal(err)
	}
	committed, err := surface.CommitRevision(log, token, nil)
	if err != nil || committed.State != availability.Unavailable {
		t.Fatalf("committed=%+v err=%v", committed, err)
	}
	if len(log.History("worker-1")) != 1 {
		t.Fatal("served revision transaction did not append history")
	}
}

func TestTodo_AVAIL_003_Property(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	log := surface.NewRevisionLog()
	firstToken, err := surface.PrepareRevision(log, servedRevisionRequest())
	if err != nil {
		t.Fatal(err)
	}
	first, err := surface.CommitRevision(log, firstToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	next := servedRevisionRequest()
	next.PriorRevision = first.RevisionID
	next.LeaveEventID = "leave:worker-1:return"
	next.State = availability.Available
	next.Restored = true
	next.EffectiveStart = 18
	next.EffectiveEnd = 20
	nextToken, err := surface.PrepareRevision(log, next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := surface.CommitRevision(log, nextToken, nil); err != nil || len(log.History("worker-1")) != 2 {
		t.Fatalf("successor history=%v err=%v", log.History("worker-1"), err)
	}
}

func TestTodo_AVAIL_003_Race(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	log := surface.NewRevisionLog()
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := surface.PrepareRevision(log, servedRevisionRequest())
			if err != nil {
				return
			}
			if _, err := surface.CommitRevision(log, token, nil); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 || len(log.History("worker-1")) != 1 {
		t.Fatalf("wins=%d history=%v", wins, log.History("worker-1"))
	}
}

func TestTodo_AVAIL_003_Recovery(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	log := surface.NewRevisionLog()
	if _, err := surface.PrepareRevision(log, servedRevisionRequest()); err != nil {
		t.Fatal(err)
	}
	recovered, err := surface.RecoverRevisions(log)
	if err != nil || len(recovered) != 1 {
		t.Fatalf("recovered=%v err=%v", recovered, err)
	}
	if again, err := surface.RecoverRevisions(log); err != nil || len(again) != 0 {
		t.Fatalf("recovery repeated: %v err=%v", again, err)
	}
}

func TestTodo_AVAIL_003_Mutation(t *testing.T) {
	surface := application.NewServedAvailabilitySurface()
	log := surface.NewRevisionLog()
	bad := servedRevisionRequest()
	bad.State = availability.Unknown
	if _, err := surface.PrepareRevision(log, bad); err == nil {
		t.Fatal("served transaction accepted an unknown successor state")
	}
}

func TestServedAvailabilityIsReachableFromApp(t *testing.T) {
	var app application.App
	surface := (&app).Availability()
	if surface.SimulateAbsence == nil || surface.NewRevisionLog == nil {
		t.Fatal("composed app omitted availability capability")
	}
	if (&application.App{}).Availability().SimulateVersionedAbsence == nil {
		t.Fatal("app availability surface did not expose versioned simulation")
	}
}
