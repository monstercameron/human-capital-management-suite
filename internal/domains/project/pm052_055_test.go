package project

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTodo_PM_052(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	schedule := RecurrenceSchedule{ID: "daily-review", ProjectID: "project-1", Version: 3, Timezone: location.String(), Frequency: RecurrenceDaily, Interval: 1, StartAt: time.Date(2026, 3, 7, 9, 0, 0, 0, location), CatchUp: CatchUpAll}
	from := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)
	through := time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC)
	first, err := schedule.Generate(from, through, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 4 || first[1].LocalStart != "2026-03-08T09:00:00" || first[1].StartsAt.UTC().Format(time.RFC3339) != "2026-03-08T13:00:00Z" {
		t.Fatalf("local-time occurrences = %+v", first)
	}
	if first[0].StableKey != "daily-review:v3:2026-03-07T09:00:00" {
		t.Fatalf("stable occurrence key = %q", first[0].StableKey)
	}
	existing := map[string]struct{}{first[0].StableKey: {}}
	retry, err := schedule.Generate(from, through, existing)
	if err != nil || len(retry) != 3 {
		t.Fatalf("idempotent retry = %d, %v", len(retry), err)
	}
}

func TestTodo_PM_052_Golden(t *testing.T) {
	location, _ := time.LoadLocation("UTC")
	schedule := RecurrenceSchedule{ID: "weekly", ProjectID: "p", Version: 2, Timezone: "UTC", Frequency: RecurrenceWeekly, Interval: 1, StartAt: time.Date(2026, 1, 5, 8, 30, 0, 0, location), Weekdays: []time.Weekday{time.Monday, time.Wednesday}, CatchUp: CatchUpAll}
	occurrences, err := schedule.Generate(time.Date(2026, 1, 5, 0, 0, 0, 0, location), time.Date(2026, 1, 12, 0, 0, 0, 0, location), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(occurrences) != 2 || occurrences[0].StableKey != "weekly:v2:2026-01-05T08:30:00" || occurrences[1].StableKey != "weekly:v2:2026-01-07T08:30:00" {
		t.Fatalf("golden recurrence = %+v", occurrences)
	}
}

func TestTodo_PM_052_Recovery(t *testing.T) {
	location, _ := time.LoadLocation("UTC")
	schedule := RecurrenceSchedule{ID: "paused", ProjectID: "p", Version: 1, Timezone: "UTC", Frequency: RecurrenceDaily, Interval: 1, StartAt: time.Date(2026, 2, 1, 9, 0, 0, 0, location), Paused: true, PausedAt: time.Date(2026, 2, 2, 10, 0, 0, 0, location), ResumesAt: time.Date(2026, 2, 5, 10, 0, 0, 0, location), CatchUp: CatchUpAll}
	occurrences, err := schedule.Generate(time.Date(2026, 2, 1, 0, 0, 0, 0, location), time.Date(2026, 2, 6, 0, 0, 0, 0, location), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(occurrences) != 5 || occurrences[2].LocalStart != "2026-02-03T09:00:00" {
		t.Fatalf("catch-up occurrences = %+v", occurrences)
	}

	schedule.CatchUp = CatchUpSkip
	occurrences, err = schedule.Generate(time.Date(2026, 2, 1, 0, 0, 0, 0, location), time.Date(2026, 2, 6, 0, 0, 0, 0, location), nil)
	if err != nil || len(occurrences) != 2 {
		t.Fatalf("skip recovery = %d, %v", len(occurrences), err)
	}

	schedule.CatchUp = CatchUpLatest
	occurrences, err = schedule.Generate(time.Date(2026, 2, 1, 0, 0, 0, 0, location), time.Date(2026, 2, 6, 0, 0, 0, 0, location), nil)
	if err != nil || len(occurrences) != 3 || occurrences[2].LocalStart != "2026-02-05T09:00:00" {
		t.Fatalf("latest recovery = %+v, %v", occurrences, err)
	}
}

func TestTodo_PM_053(t *testing.T) {
	book, err := NewCycleBook("project-1")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := NewCycle("cycle-1", "project-1", "Sprint 1", "2026-01-01", "2026-01-15")
	second, _ := NewCycle("cycle-2", "project-1", "Sprint 2", "2026-01-10", "2026-01-20")
	book, err = book.Add(first)
	if err != nil {
		t.Fatal(err)
	}
	book, err = book.Add(second)
	if err != nil {
		t.Fatal(err)
	}
	book, first, err = book.Start("cycle-1", first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := book.Start("cycle-2", second.Revision); !errors.Is(err, ErrCycleOverlap) {
		t.Fatalf("overlapping active cycle error = %v", err)
	}
	book, first, err = book.AssignTask("cycle-1", first.Revision, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	closedBook, closed, err := book.Close("cycle-1", first.Revision, time.Date(2026, 1, 15, 17, 0, 0, 0, time.UTC), map[TaskID]string{"task-1": "ACTIVE"}, map[TaskID]CarryoverChoice{"task-1": CarryoverToNext}, []string{"scope:v1", "status:v1"})
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != CycleClosed || closed.CloseRecord == nil || len(closed.CloseRecord.CommittedScope) != 1 || closed.CloseRecord.Carryover[0].Choice != CarryoverToNext || len(closed.CloseRecord.ReportInputs) != 2 {
		t.Fatalf("close record = %+v", closed.CloseRecord)
	}
	if _, _, err := closedBook.Close("cycle-1", closed.Revision, time.Now(), map[TaskID]string{"task-1": "DONE"}, nil, nil); !errors.Is(err, ErrCycleClosed) {
		t.Fatalf("closed cycle mutation error = %v", err)
	}
}

func TestTodo_PM_053_Race(t *testing.T) {
	cycle, err := NewCycle("cycle-race", "project-1", "Race", "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan Cycle, 2)
	for _, taskID := range []TaskID{"a", "b"} {
		wg.Add(1)
		go func(taskID TaskID) {
			defer wg.Done()
			updated, updateErr := cycle.AddTask(cycle.Revision, taskID)
			if updateErr == nil {
				results <- updated
			}
		}(taskID)
	}
	wg.Wait()
	close(results)
	count := 0
	for range results {
		count++
	}
	if count != 2 {
		t.Fatalf("value cycle operations were not race-safe: %d results", count)
	}
}

func TestTodo_PM_053_Integration(t *testing.T) {
	cycle, err := NewCycle("integration", "project-1", "Integration", "2026-05-01", "2026-05-08")
	if err != nil {
		t.Fatal(err)
	}
	cycle, err = cycle.AddTask(cycle.Revision, "task")
	if err != nil {
		t.Fatal(err)
	}
	cycle, err = cycle.Start(cycle.Revision)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := cycle.Close(cycle.Revision, time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC), map[TaskID]string{"task": "DONE"}, nil, []string{"real-store-shaped-input"})
	if err != nil || closed.CloseRecord == nil || closed.CloseRecord.CommittedScope[0].Status != "DONE" {
		t.Fatalf("integrated cycle close = %+v, %v", closed, err)
	}
}

func TestTodo_PM_054(t *testing.T) {
	board, err := NewScrumBoard("project-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, taskID := range []TaskID{"a", "b", "c"} {
		board, err = board.AddBacklogTask(taskID)
		if err != nil {
			t.Fatal(err)
		}
	}
	board, cycle, err := board.PlanSprint("sprint-1", "Sprint 1", "2026-06-01", "2026-06-14", []TaskID{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	board, cycle, err = board.StartSprint(cycle.ID, cycle.Revision)
	if err != nil {
		t.Fatal(err)
	}
	board, cycle, err = board.ChangeScope(cycle.ID, cycle.Revision, []TaskID{"c"}, []TaskID{"b"})
	if err != nil {
		t.Fatal(err)
	}
	board, report, err := board.CloseSprint(cycle.ID, cycle.Revision, time.Date(2026, 6, 14, 17, 0, 0, 0, time.UTC), map[TaskID]string{"a": "DONE", "c": "ACTIVE"}, map[TaskID]CarryoverChoice{"c": CarryoverToNext}, []string{"sprint-report-input"})
	if err != nil || report.Planned != 2 || report.Completed != 1 || report.Carried != 1 {
		t.Fatalf("scrum close report = %+v, %v", report, err)
	}
	if _, err := board.Report("sprint-1"); err != nil || !containsTask(board.Backlog, "b") || !containsTask(board.Backlog, "c") {
		t.Fatalf("scrum close/backlog = %+v, %v", board.Backlog, err)
	}
}

func TestTodo_PM_054_Conformance(t *testing.T) {
	board, _ := NewScrumBoard("project-1")
	board, _ = board.AddBacklogTask("task")
	board, cycle, err := board.PlanSprint("sprint", "Sprint", "2026-07-01", "2026-07-08", []TaskID{"task"})
	if err != nil {
		t.Fatal(err)
	}
	board, cycle, err = board.StartSprint(cycle.ID, cycle.Revision)
	if err != nil {
		t.Fatal(err)
	}
	board, report, err := board.CloseSprint(cycle.ID, cycle.Revision, time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC), map[TaskID]string{"task": "DONE"}, nil, []string{"truth"})
	if err != nil || report.Planned != 1 || report.Completed != 1 {
		t.Fatalf("scrum conformance report = %+v, %v", report, err)
	}
	closed, _ := board.Report("sprint")
	if closed != report {
		t.Fatalf("report changed after close: %+v != %+v", closed, report)
	}
}

func TestTodo_PM_054_Browser(t *testing.T) {
	board, _ := NewScrumBoard("project-1")
	board, _ = board.AddBacklogTask("task")
	board, cycle, err := board.PlanSprint("browser-sprint", "Browser sprint", "2026-08-01", "2026-08-08", []TaskID{"task"})
	if err != nil || cycle.State != CyclePlanned || len(cycle.TaskIDs) != 1 {
		t.Fatalf("served planning state = %+v, %v", cycle, err)
	}
	if len(board.Backlog) != 0 {
		t.Fatalf("served backlog did not reflect planned scope: %+v", board.Backlog)
	}
}

func TestTodo_PM_055(t *testing.T) {
	board, err := NewScrumbanBoard("project-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	board, err = board.AddTask("a", ScrumbanReady)
	if err != nil {
		t.Fatal(err)
	}
	board, err = board.AddTask("b", ScrumbanReady)
	if err != nil {
		t.Fatal(err)
	}
	board, move, err := board.MoveTask("a", ScrumbanInProgress, 1)
	if err != nil || move.Sequence != 1 {
		t.Fatalf("first flow move = %+v, %v", move, err)
	}
	if _, _, err := board.MoveTask("b", ScrumbanInProgress, 1); !errors.Is(err, ErrWIPLimit) {
		t.Fatalf("WIP rejection = %v", err)
	}
	board, _, err = board.MoveTask("a", ScrumbanDone, 2)
	if err != nil {
		t.Fatal(err)
	}
	board, _, err = board.MoveTask("b", ScrumbanInProgress, 1)
	if err != nil || len(board.Moves) != 3 {
		t.Fatalf("flow after freeing WIP = %+v, %v", board.Moves, err)
	}
}

func TestTodo_PM_055_Conformance(t *testing.T) {
	board, _ := NewScrumbanBoard("project-1", 2)
	board, _ = board.AddTask("task", ScrumbanReady)
	cycle, _ := NewCycle("cycle", "project-1", "Flow cycle", "2026-09-01", "2026-09-08")
	board, cycle, err := board.PlanCycle(cycle)
	if err != nil {
		t.Fatal(err)
	}
	board, cycle, err = board.AssignCycleTask(cycle.ID, cycle.Revision, "task")
	if err != nil {
		t.Fatal(err)
	}
	board, cycle, err = board.StartCycle(cycle.ID, cycle.Revision)
	if err != nil {
		t.Fatal(err)
	}
	board, closed, err := board.CloseCycle(cycle.ID, cycle.Revision, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), map[TaskID]string{"task": "ACTIVE"}, map[TaskID]CarryoverChoice{"task": CarryoverToNext}, []string{"flow-input"})
	if err != nil || closed.Cycle.CycleID != "cycle" || board.Cycles.Cycles[0].State != CycleClosed || closed.MovesBefore != 0 {
		t.Fatalf("scrumban cycle composition = %+v, %+v, %v", board.Cycles, closed, err)
	}
}

func TestTodo_PM_055_Race(t *testing.T) {
	board, _ := NewScrumbanBoard("project-1", 1)
	board, _ = board.AddTask("task", ScrumbanReady)
	var wg sync.WaitGroup
	results := make(chan ScrumbanMove, 2)
	for _, status := range []string{ScrumbanInProgress, ScrumbanDone} {
		wg.Add(1)
		go func(status string) {
			defer wg.Done()
			updated, move, err := board.MoveTask("task", status, 1)
			if err == nil && updated.Tasks[0].Revision == 2 {
				results <- move
			}
		}(status)
	}
	wg.Wait()
	close(results)
	count := 0
	for range results {
		count++
	}
	if count != 2 {
		t.Fatalf("immutable board operations unexpectedly shared state: %d", count)
	}
}
