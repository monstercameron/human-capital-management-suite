package timesession

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func t0(minute int) time.Time {
	return time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC).Add(time.Duration(minute) * time.Minute)
}

func basePunch(kind PunchKind, minute int) Punch {
	return Punch{
		Kind: kind, Tenant: "acme", Worker: "worker-1", Actor: "worker-1", Assignment: "assignment-1",
		SessionID: "session-1", DeviceTime: t0(minute), ServerReceiptTime: t0(minute).Add(time.Second),
		Scheduled: true,
	}
}

// TestTodo_FTIME_003 is the PRIMARY acceptance test: a full clock
// in/break/end-break/out cycle, each typed conflict rejecting the RED case
// it names, and unscheduled work captured as an exception instead of
// discarded.
func TestTodo_FTIME_003(t *testing.T) {
	var session Session

	in := basePunch(PunchIn, 0)
	session, outIn, err := Apply(session, in)
	if err != nil {
		t.Fatalf("clock-in: unexpected error: %v", err)
	}
	if outIn.Kind != OutcomeOpened || session.State != StateOpen || session.Revision != 1 {
		t.Fatalf("clock-in outcome = %+v, session = %+v", outIn, session)
	}
	if len(session.Segments) != 1 || session.Segments[0].Kind != SegmentWork || !session.Segments[0].Open() {
		t.Fatalf("clock-in segments = %+v", session.Segments)
	}
	if len(session.OpenExceptions) != 0 {
		t.Fatalf("scheduled clock-in should carry no exception, got %+v", session.OpenExceptions)
	}

	// RED: a second IN on an already-open session is rejected, not silently
	// re-opened.
	dup := basePunch(PunchIn, 1)
	_, _, err = Apply(session, dup)
	if !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("second clock-in: err = %v, want ErrAlreadyOpen", err)
	}

	breakStart := basePunch(PunchBreakStart, 60)
	breakStart.ExpectedRevision = session.Revision
	session, outBreak, err := Apply(session, breakStart)
	if err != nil {
		t.Fatalf("break start: unexpected error: %v", err)
	}
	if outBreak.Kind != OutcomeOnBreak || session.State != StateOnBreak {
		t.Fatalf("break start outcome = %+v, session state = %s", outBreak, session.State)
	}
	if open := lastOpenSegment(session); open == nil || open.Kind != SegmentBreak {
		t.Fatalf("break start open segment = %+v", open)
	}
	// Ending the work segment closes it at the break's device time.
	if session.Segments[0].End.IsZero() {
		t.Fatalf("work segment should have closed on break start")
	}

	// RED: BREAK_START again while already on break is rejected.
	_, _, err = Apply(session, breakStart)
	if !errors.Is(err, ErrAlreadyOnBreak) {
		t.Fatalf("second break start: err = %v, want ErrAlreadyOnBreak", err)
	}

	breakEnd := basePunch(PunchBreakEnd, 75)
	breakEnd.ExpectedRevision = session.Revision
	session, outResume, err := Apply(session, breakEnd)
	if err != nil {
		t.Fatalf("break end: unexpected error: %v", err)
	}
	if outResume.Kind != OutcomeResumed || session.State != StateOpen {
		t.Fatalf("break end outcome = %+v, session state = %s", outResume, session.State)
	}

	// RED: MEAL_END while there is no open meal segment (only a work
	// segment is open) is rejected as not-on-break, never silently accepted
	// against the wrong segment kind.
	mealEnd := basePunch(PunchMealEnd, 76)
	mealEnd.ExpectedRevision = session.Revision
	_, _, err = Apply(session, mealEnd)
	if !errors.Is(err, ErrNotOnBreak) {
		t.Fatalf("meal end while open work segment: err = %v, want ErrNotOnBreak", err)
	}

	out := basePunch(PunchOut, 480)
	out.ExpectedRevision = session.Revision
	session, outClose, err := Apply(session, out)
	if err != nil {
		t.Fatalf("clock-out: unexpected error: %v", err)
	}
	if outClose.Kind != OutcomeClosed || session.State != StateClosed {
		t.Fatalf("clock-out outcome = %+v, session state = %s", outClose, session.State)
	}
	for _, seg := range session.Segments {
		if seg.Open() {
			t.Fatalf("segment %+v should be closed after clock-out", seg)
		}
	}

	// RED: a second OUT on a closed session is rejected.
	_, _, err = Apply(session, out)
	if !errors.Is(err, ErrNotOpen) {
		t.Fatalf("second clock-out: err = %v, want ErrNotOpen", err)
	}
}

// TestTodo_FTIME_003_Golden_UnscheduledWork is not a matrix label; it is a
// focused check that an unscheduled clock-in is captured, not discarded.
func TestUnscheduledWorkCapturedAsException(t *testing.T) {
	unscheduled := basePunch(PunchIn, 0)
	unscheduled.Scheduled = false
	session, outcome, err := Apply(Session{}, unscheduled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(session.OpenExceptions) != 1 || session.OpenExceptions[0].Kind != ExceptionUnscheduledWork {
		t.Fatalf("session exceptions = %+v, want one UNSCHEDULED_WORK", session.OpenExceptions)
	}
	if outcome.Exception == nil || outcome.Exception.Kind != ExceptionUnscheduledWork {
		t.Fatalf("outcome.Exception = %+v, want UNSCHEDULED_WORK", outcome.Exception)
	}
}

func TestStaleRevisionRejected(t *testing.T) {
	session, _, err := Apply(Session{}, basePunch(PunchIn, 0))
	if err != nil {
		t.Fatalf("clock-in: unexpected error: %v", err)
	}
	out := basePunch(PunchOut, 10)
	out.ExpectedRevision = session.Revision + 5 // stale/incorrect expectation
	_, _, err = Apply(session, out)
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale revision: err = %v, want ErrStaleRevision", err)
	}
}

func TestDuplicateIdempotencyKeyReturnsOriginalOutcome(t *testing.T) {
	in := basePunch(PunchIn, 0)
	in.IdempotencyKey = "req-1"
	session, first, err := Apply(Session{}, in)
	if err != nil {
		t.Fatalf("clock-in: unexpected error: %v", err)
	}
	session2, second, err := Apply(session, in)
	if !errors.Is(err, ErrDuplicatePunch) {
		t.Fatalf("retry: err = %v, want ErrDuplicatePunch", err)
	}
	if second.Kind != first.Kind || second.Session.SessionID != first.Session.SessionID || second.Session.Revision != first.Session.Revision {
		t.Fatalf("retry outcome = %+v, want original %+v", second, first)
	}
	if session2.SessionID != session.SessionID {
		t.Fatalf("retry must not mutate the session: got %+v, want unchanged %+v", session2, session)
	}
}

func TestApplyOnErrorLeavesSessionUnchanged(t *testing.T) {
	// FAULT: a malformed punch (no declared kind) must not mutate the
	// session it was applied to; the caller gets back exactly what it
	// passed in.
	session := Session{Tenant: "acme", Worker: "worker-1", Assignment: "assignment-1", SessionID: "session-1", State: StateOpen, Revision: 1}
	malformed := Punch{Tenant: "acme", Worker: "worker-1", Actor: "worker-1", Assignment: "assignment-1"}
	got, outcome, err := Apply(session, malformed)
	if !errors.Is(err, ErrInvalidPunch) {
		t.Fatalf("malformed punch: err = %v, want ErrInvalidPunch", err)
	}
	if !sameSession(got, session) {
		t.Fatalf("malformed punch mutated session: got %+v, want %+v", got, session)
	}
	if outcome.Kind != "" || outcome.Session.SessionID != "" || outcome.Exception != nil {
		t.Fatalf("malformed punch: outcome = %+v, want zero", outcome)
	}
}

// sameSession compares the fields that matter for "did Apply mutate this
// session", since Session holds slices and a pointer and cannot use ==.
func sameSession(a, b Session) bool {
	return a.Tenant == b.Tenant && a.Worker == b.Worker && a.Assignment == b.Assignment &&
		a.SessionID == b.SessionID && a.State == b.State && a.Revision == b.Revision &&
		len(a.Segments) == len(b.Segments) && len(a.OpenExceptions) == len(b.OpenExceptions)
}

func TestApplyRejectsMissingEvidence(t *testing.T) {
	cases := []struct {
		name  string
		punch Punch
	}{
		{"undeclared kind", Punch{Kind: "BOGUS", Tenant: "acme", Worker: "w1", Actor: "w1", Assignment: "a1", DeviceTime: t0(0), ServerReceiptTime: t0(0)}},
		{"missing tenant", Punch{Kind: PunchIn, Worker: "w1", Actor: "w1", Assignment: "a1", DeviceTime: t0(0), ServerReceiptTime: t0(0)}},
		{"missing device time", Punch{Kind: PunchIn, Tenant: "acme", Worker: "w1", Actor: "w1", Assignment: "a1", ServerReceiptTime: t0(0)}},
		{"missing server receipt time", Punch{Kind: PunchIn, Tenant: "acme", Worker: "w1", Actor: "w1", Assignment: "a1", DeviceTime: t0(0)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := Apply(Session{}, c.punch)
			if !errors.Is(err, ErrInvalidPunch) {
				t.Fatalf("%s: err = %v, want ErrInvalidPunch", c.name, err)
			}
		})
	}
}

// TestTodo_FTIME_003_Security is the SECURITY matrix entry: clocking in
// another worker without a delegation grant is rejected, and delegation
// legitimately allows it.
func TestTodo_FTIME_003_Security(t *testing.T) {
	impersonation := basePunch(PunchIn, 0)
	impersonation.Worker = "worker-2"
	impersonation.Actor = "worker-1" // a different actor, no delegation
	_, _, err := Apply(Session{}, impersonation)
	if !errors.Is(err, ErrDelegationRequired) {
		t.Fatalf("undelegated cross-worker clock-in: err = %v, want ErrDelegationRequired", err)
	}

	delegated := impersonation
	delegated.Delegated = true
	session, outcome, err := Apply(Session{}, delegated)
	if err != nil {
		t.Fatalf("delegated clock-in: unexpected error: %v", err)
	}
	if session.Worker != "worker-2" || outcome.Kind != OutcomeOpened {
		t.Fatalf("delegated clock-in session = %+v, outcome = %+v", session, outcome)
	}

	// A delegated grant on the session's own actual worker is a no-op check
	// (Worker == Actor) and must not be required.
	selfPunch := basePunch(PunchIn, 0)
	if _, _, err := Apply(Session{}, selfPunch); err != nil {
		t.Fatalf("self clock-in: unexpected error: %v", err)
	}
}

// TestTodo_FTIME_003_Race is the RACE matrix entry: concurrent Apply calls
// against independent copies of the same starting session must be
// deterministic. Apply mutates nothing in place, so every goroutine
// applying the identical punch to its own copy must observe the identical
// result; run with `go test -race`.
func TestTodo_FTIME_003_Race(t *testing.T) {
	base, _, err := Apply(Session{}, basePunch(PunchIn, 0))
	if err != nil {
		t.Fatalf("setup clock-in: unexpected error: %v", err)
	}
	punch := basePunch(PunchOut, 480)
	punch.ExpectedRevision = base.Revision
	punch.IdempotencyKey = "close-1"

	const n = 64
	results := make([]Outcome, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			copySession := base // struct copy; Segments/OpenExceptions slices are read-only here
			_, outcome, err := Apply(copySession, punch)
			results[i] = outcome
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i := 1; i < n; i++ {
		if errs[i] != errs[0] && !(errs[i] == nil && errs[0] == nil) {
			t.Fatalf("goroutine %d error = %v, want %v", i, errs[i], errs[0])
		}
		if results[i].Kind != results[0].Kind || results[i].Session.State != results[0].Session.State || results[i].Session.Revision != results[0].Session.Revision {
			t.Fatalf("goroutine %d outcome = %+v, want %+v", i, results[i], results[0])
		}
	}
	if results[0].Kind != OutcomeClosed || results[0].Session.State != StateClosed {
		t.Fatalf("concurrent clock-out outcome = %+v, want CLOSED", results[0])
	}
}

// TestTodo_FTIME_003_Fault is the FAULT matrix entry: malformed input never
// invents a time, mutates in place or panics; a fixed table of red-path
// requests all return typed errors and the original session.
func TestTodo_FTIME_003_Fault(t *testing.T) {
	session, _, err := Apply(Session{}, basePunch(PunchIn, 0))
	if err != nil {
		t.Fatalf("setup: unexpected error: %v", err)
	}

	transferWhileOnBreak := func() Session {
		breakStart := basePunch(PunchBreakStart, 5)
		breakStart.ExpectedRevision = session.Revision
		s, _, err := Apply(session, breakStart)
		if err != nil {
			t.Fatalf("setup break start: unexpected error: %v", err)
		}
		return s
	}()

	cases := []struct {
		name    string
		session Session
		punch   Punch
		wantErr error
	}{
		{"break-end without a break", session, basePunch(PunchBreakEnd, 10), ErrNotOnBreak},
		{"transfer while on break", transferWhileOnBreak, basePunch(PunchTransfer, 15), ErrNotOpen},
		{"out on a never-opened session", Session{}, basePunch(PunchOut, 20), ErrNotOpen},
		{"break-start on a never-opened session", Session{}, basePunch(PunchBreakStart, 20), ErrNotOpen},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := Apply(c.session, c.punch)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("%s: err = %v, want %v", c.name, err, c.wantErr)
			}
			if !sameSession(got, c.session) {
				t.Fatalf("%s: session mutated: got %+v, want %+v", c.name, got, c.session)
			}
		})
	}
}

func TestMissingOutCapturesExceptionWithoutInventingATime(t *testing.T) {
	session, _, err := Apply(Session{}, basePunch(PunchIn, 0))
	if err != nil {
		t.Fatalf("setup: unexpected error: %v", err)
	}
	scheduledEnd := t0(480)

	// Grace has not expired: the exception is not raised yet.
	_, _, err = MissingOut(session, scheduledEnd, false)
	if !errors.Is(err, ErrGraceNotExpired) {
		t.Fatalf("grace not expired: err = %v, want ErrGraceNotExpired", err)
	}

	next, exc, err := MissingOut(session, scheduledEnd, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exc.Kind != ExceptionMissingOut || !exc.At.Equal(scheduledEnd) {
		t.Fatalf("exception = %+v, want MISSING_OUT at %v", exc, scheduledEnd)
	}
	if next.State != StateOpen {
		t.Fatalf("session state = %s, want unchanged OPEN (missing-out never closes)", next.State)
	}
	if len(next.Segments) != 1 || !next.Segments[0].Open() {
		t.Fatalf("segments = %+v, want the original open work segment untouched", next.Segments)
	}
	if len(next.OpenExceptions) != 1 {
		t.Fatalf("open exceptions = %+v, want exactly one", next.OpenExceptions)
	}
}
