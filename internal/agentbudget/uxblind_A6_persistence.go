package agentbudget

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrPersistence wraps every failure reported by a Persister. A transition
// that fails to persist is rolled back in memory, so callers never hold an
// admission the durable ledger does not know about.
var ErrPersistence = errors.New("agentbudget: durable ledger write failed")

// TransitionKind names the ledger state change handed to a Persister.
type TransitionKind string

const (
	TransitionOpenTask  TransitionKind = "OPEN_TASK"
	TransitionReserve   TransitionKind = "RESERVE"
	TransitionSettle    TransitionKind = "SETTLE"
	TransitionRelease   TransitionKind = "RELEASE"
	TransitionFail      TransitionKind = "FAIL"
	TransitionExtension TransitionKind = "EXTENSION"
	TransitionPause     TransitionKind = "PAUSE"
)

// DurableTask is the part of a task that survives a restart: its spec, the
// settled usage, the pause card and the attempt and failure counters. The
// in-flight reserved amount is deliberately absent. Attempts and Failures use
// the ledger's raw keys (the failure key contains a NUL byte), so a store must
// encode them losslessly.
type DurableTask struct {
	Spec     TaskSpec
	Revision uint64
	Used     Usage
	Paused   *PauseError
	Attempts map[string]int
	Failures map[string]int
}

// DurablePeriod is the settled usage of one user-day or tenant-month. Key is
// the ledger's period key ("<user or tenant>|<YYYY-MM-DD or YYYY-MM>") and
// Start the UTC start of that period.
type DurablePeriod struct {
	Scope Scope
	Key   string
	Start time.Time
	Used  Usage
}

// Transition is one committed ledger state change. Task is the task after the
// change; Periods lists the period counters the change touched (SETTLE only).
// Actual is the usage recorded by SETTLE and Extension the limits added by
// EXTENSION; both feed an append-only journal.
type Transition struct {
	Kind             TransitionKind
	At               time.Time
	Task             DurableTask
	Periods          []DurablePeriod
	ReservationID    string
	StepID           string
	Actual           Usage
	Extension        Limits
	RequestID        string
	ExpectedRevision uint64
	ResultRevision   uint64
	Result           ExtensionResult
	// RelatedTasks carries atomically updated lineage counters (currently the
	// root task for a child settlement/release/failure).
	RelatedTasks []DurableTask
}

// Persister makes ledger transitions durable. The ledger calls Persist while
// holding its lock, after computing the next state and before publishing it:
// a returned error aborts the transition and is surfaced wrapped in
// ErrPersistence, so no model work starts without a durable record.
//
// In-flight reservations are not persisted. A restart releases them: the
// reserved amounts are simply absent from the restored ledger, while the
// attempt counter written at RESERVE time still counts the interrupted try.
// agentrun RecoverStale reconciles the step itself.
//
// One process is the single writer of a tenant's ledger; Persist writes
// absolute counters, not increments.
type Persister interface {
	Persist(Transition) error
}

// RestoredState is the durable state loaded by a store for Ledger.Restore.
type RestoredState struct {
	Tasks   []DurableTask
	Periods []DurablePeriod
	Replays []DurableExtensionReplay
}

// DurableExtensionReplay is the immutable replay record for one extension
// request. It is restored before any new admission is attempted.
type DurableExtensionReplay struct {
	TaskID           string
	RequestID        string
	ExpectedRevision uint64
	ResultRevision   uint64
	Additional       Limits
	Result           ExtensionResult
}

// NewWithPersistence creates a ledger that reports every transition to p. A
// nil p behaves exactly like NewWithClock.
func NewWithPersistence(policy Policy, clock func() time.Time, p Persister) (*Ledger, error) {
	l, err := NewWithClock(policy, clock)
	if err != nil {
		return nil, err
	}
	l.persister = p
	return l, nil
}

// Restore merges stored tasks and period counters into the ledger. Reserved
// amounts start at zero. It refuses a task the ledger already holds, an
// invalid limit and an unknown period scope, and it applies nothing when any
// row is refused.
func (l *Ledger) Restore(state RestoredState) error {
	if l == nil {
		return ErrInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := make(map[string]bool, len(state.Tasks))
	for _, t := range state.Tasks {
		if t.Spec.ID == "" || t.Spec.TenantID == "" || t.Spec.UserID == "" || t.Revision == 0 || t.Revision > uint64(^uint64(0)>>1) || !t.Spec.Limit.positive() || !t.Used.valid() {
			return fmt.Errorf("%w: stored task %q is invalid", ErrInvalid, t.Spec.ID)
		}
		if t.Spec.ParentTaskID == "" && (t.Spec.RootTaskID != "" || t.Spec.Depth != 0) {
			return fmt.Errorf("%w: root task %q has lineage", ErrInvalid, t.Spec.ID)
		}
		if t.Spec.ParentTaskID != "" && (t.Spec.RootTaskID == "" || t.Spec.Depth < 1 || t.Spec.Depth > 16 || t.Spec.ParentTaskID == t.Spec.ID || t.Spec.RootTaskID == t.Spec.ID) {
			return fmt.Errorf("%w: stored child task %q has invalid lineage", ErrInvalid, t.Spec.ID)
		}
		if _, ok := l.tasks[t.Spec.ID]; ok || seen[t.Spec.ID] {
			return fmt.Errorf("%w: %s", ErrTaskExists, t.Spec.ID)
		}
		seen[t.Spec.ID] = true
	}
	for _, t := range state.Tasks {
		if t.Spec.ParentTaskID == "" {
			continue
		}
		var parent *DurableTask
		for i := range state.Tasks {
			if state.Tasks[i].Spec.ID == t.Spec.ParentTaskID {
				parent = &state.Tasks[i]
				break
			}
		}
		if parent == nil {
			if existing := l.tasks[t.Spec.ParentTaskID]; existing != nil {
				p := durableTask(existing)
				parent = &p
			}
		}
		expectedRoot := ""
		expectedDepth := 1
		if parent != nil {
			expectedRoot, expectedDepth = parent.Spec.ID, parent.Spec.Depth+1
			if parent.Spec.RootTaskID != "" {
				expectedRoot = parent.Spec.RootTaskID
			}
		}
		if parent == nil || parent.Spec.TenantID != t.Spec.TenantID || parent.Spec.UserID != t.Spec.UserID || t.Spec.RootTaskID != expectedRoot || t.Spec.Depth != expectedDepth || t.Spec.Limit.exceeds(parent.Spec.Limit) {
			return fmt.Errorf("%w: stored child task %q has invalid parent", ErrInvalid, t.Spec.ID)
		}
	}
	for _, replay := range state.Replays {
		if replay.TaskID == "" || !validRequestID(replay.RequestID) || replay.ExpectedRevision == 0 || replay.ExpectedRevision >= uint64(^uint64(0)>>1) || replay.ResultRevision != replay.ExpectedRevision+1 || !replay.Additional.valid() || !replay.Additional.any() || replay.Result.TaskID != replay.TaskID || replay.Result.RequestID != replay.RequestID || replay.Result.ExpectedRevision != replay.ExpectedRevision || replay.Result.PreviousRevision != replay.ExpectedRevision || replay.Result.Revision != replay.ResultRevision || replay.Result.Additional != replay.Additional || !replay.Result.Limit.positive() || replay.Result.NewLimit != replay.Result.Limit {
			return fmt.Errorf("%w: stored extension replay %q is invalid", ErrInvalid, replay.RequestID)
		}
		if _, ok := l.tasks[replay.TaskID]; ok {
			// validated below after all task IDs have been collected
		} else {
			found := false
			for _, task := range state.Tasks {
				if task.Spec.ID == replay.TaskID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%w: extension replay task %q is missing", ErrInvalid, replay.TaskID)
			}
		}
		key := replayKey(replay.TaskID, replay.RequestID)
		if _, exists := l.replays[key]; exists {
			return fmt.Errorf("%w: extension replay %q already exists", ErrExtensionConflict, replay.RequestID)
		}
	}
	for _, p := range state.Periods {
		if (p.Scope != ScopeUser && p.Scope != ScopeTenant) || p.Key == "" || !p.Used.valid() {
			return fmt.Errorf("%w: stored period %q is invalid", ErrInvalid, p.Key)
		}
	}
	for _, t := range state.Tasks {
		ts := &taskState{spec: t.Spec, Revision: t.Revision, used: t.Used, paused: clonePause(t.Paused), attempts: cloneCounts(t.Attempts), failures: cloneCounts(t.Failures)}
		l.tasks[t.Spec.ID] = ts
	}
	for _, p := range state.Periods {
		periods := l.users
		if p.Scope == ScopeTenant {
			periods = l.tenants
		}
		l.periodLocked(periods, p.Key).used = p.Used
	}
	for _, replay := range state.Replays {
		l.replays[replayKey(replay.TaskID, replay.RequestID)] = extensionReplay{TaskID: replay.TaskID, RequestID: replay.RequestID, ExpectedRevision: replay.ExpectedRevision, Additional: replay.Additional, Result: replay.Result}
	}
	return nil
}

func cloneCounts(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneTaskState(in *taskState) *taskState {
	out := *in
	out.paused = clonePause(in.paused)
	out.attempts = cloneCounts(in.attempts)
	out.failures = cloneCounts(in.failures)
	return &out
}

func durableTask(t *taskState) DurableTask {
	return DurableTask{Spec: t.spec, Revision: t.Revision, Used: t.used, Paused: clonePause(t.paused), Attempts: cloneCounts(t.attempts), Failures: cloneCounts(t.failures)}
}

func advanceRevision(task *taskState) error {
	// PostgreSQL stores revisions in BIGINT, so stop at its signed maximum
	// rather than allowing an in-memory value that cannot be durably written.
	if task == nil || task.Revision >= uint64(^uint64(0)>>1) {
		return fmt.Errorf("%w: task revision overflow", ErrInvalid)
	}
	task.Revision++
	return nil
}

func durablePeriod(scope Scope, key string, p *periodState) DurablePeriod {
	return DurablePeriod{Scope: scope, Key: key, Start: periodStart(scope, key), Used: p.used}
}

// periodStart parses the date suffix of a period key back into its UTC start.
func periodStart(scope Scope, key string) time.Time {
	layout := "2006-01-02"
	if scope == ScopeTenant {
		layout = "2006-01"
	}
	suffix := key[strings.LastIndex(key, "|")+1:]
	start, err := time.Parse(layout, suffix)
	if err != nil {
		return time.Time{}
	}
	return start.UTC()
}

func (l *Ledger) persistLocked(t Transition) error {
	if l.persister == nil {
		return nil
	}
	if t.At.IsZero() {
		t.At = l.now().UTC()
	}
	if err := l.persister.Persist(t); err != nil {
		return fmt.Errorf("%w: %w", ErrPersistence, err)
	}
	return nil
}

// recordPauseLocked persists a pause raised while admission was refused. The
// refusal already fails closed, so a write error keeps the in-memory pause and
// still returns the pause: a lost card is re-derived from the durable counters
// on the next Reserve after a restart.
func (l *Ledger) recordPauseLocked(task *taskState, pause *PauseError) *PauseError {
	next := cloneTaskState(task)
	if err := advanceRevision(next); err == nil {
		if err := l.persistLocked(Transition{Kind: TransitionPause, Task: durableTask(next), ExpectedRevision: task.Revision, ResultRevision: next.Revision}); err == nil {
			*task = *next
		}
	}
	return pause
}
