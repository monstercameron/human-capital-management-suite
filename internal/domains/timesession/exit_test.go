package timesession

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func openSessionForExit(t *testing.T) Session {
	t.Helper()
	session, _, err := Apply(Session{}, basePunch(PunchIn, 0))
	if err != nil {
		t.Fatalf("setup clock-in: unexpected error: %v", err)
	}
	return session
}

var exitPolicy = ExitPolicy{AccuracyThresholdMeters: 25, SustainedMinimum: 5 * time.Minute, GraceDuration: 10 * time.Minute}

// TestTodo_FTIME_011 is the PRIMARY acceptance test: sustained accurate
// exit evidence starts a grace timer, re-entry cancels it, expiry closes
// the matching open session idempotently with a provisional time, and
// uncertain location leaves the session open with a visible exception.
func TestTodo_FTIME_011(t *testing.T) {
	session := openSessionForExit(t)
	now := t0(30)

	// Not yet sustained: no decision is made.
	notSustained := ExitEvidence{Exited: true, Consent: true, AccuracyMeters: 10, SustainedDuration: 1 * time.Minute}
	session, decision, err := EvaluateExit(exitPolicy, notSustained, session, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != ExitNone || session.PendingAutoOut != nil {
		t.Fatalf("not-yet-sustained evidence: decision = %+v, pending = %+v", decision, session.PendingAutoOut)
	}

	// Sustained and accurate: grace starts.
	sustained := ExitEvidence{Exited: true, Consent: true, AccuracyMeters: 10, SustainedDuration: 6 * time.Minute}
	session, decision, err = EvaluateExit(exitPolicy, sustained, session, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != ExitGraceStarted || session.PendingAutoOut == nil {
		t.Fatalf("sustained evidence: decision = %+v, pending = %+v", decision, session.PendingAutoOut)
	}
	if !decision.ExpiresAt.Equal(now.Add(exitPolicy.GraceDuration)) {
		t.Fatalf("grace expiry = %v, want %v", decision.ExpiresAt, now.Add(exitPolicy.GraceDuration))
	}

	// Re-entry cancels the pending grace.
	reentry := ExitEvidence{Exited: false}
	session, decision, err = EvaluateExit(exitPolicy, reentry, session, t0(35))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != ExitGraceCancelled || session.PendingAutoOut != nil {
		t.Fatalf("re-entry: decision = %+v, pending = %+v", decision, session.PendingAutoOut)
	}

	// A second re-entry signal with nothing pending is a no-op, not an
	// error.
	session, decision, err = EvaluateExit(exitPolicy, reentry, session, t0(36))
	if err != nil || decision.Kind != ExitNone {
		t.Fatalf("re-entry with nothing pending: decision = %+v, err = %v", decision, err)
	}

	// Exit again, let the grace mature, then expire it.
	session, decision, err = EvaluateExit(exitPolicy, sustained, session, t0(40))
	if err != nil || decision.Kind != ExitGraceStarted {
		t.Fatalf("second exit: decision = %+v, err = %v", decision, err)
	}
	expiresAt := decision.ExpiresAt

	_, _, err = ExpireGrace(session, t0(45), "auto-out-1") // before expiry
	if !errors.Is(err, ErrGraceNotExpired) {
		t.Fatalf("early expiry attempt: err = %v, want ErrGraceNotExpired", err)
	}

	closedSession, outcome, err := ExpireGrace(session, expiresAt.Add(time.Second), "auto-out-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Kind != OutcomeAutoClosed || closedSession.State != StateAutoClosed {
		t.Fatalf("expiry outcome = %+v, session state = %s", outcome, closedSession.State)
	}
	for _, seg := range closedSession.Segments {
		if seg.Open() {
			t.Fatalf("segment %+v should be closed by AUTO_OUT", seg)
		}
		if seg.End.After(expiresAt) {
			t.Fatalf("segment end %v is after the grace expiry %v: expiry must not invent a later time", seg.End, expiresAt)
		}
	}
	found := false
	for _, exc := range closedSession.OpenExceptions {
		if exc.Kind == ExceptionAutoOutProvisional {
			found = true
		}
	}
	if !found {
		t.Fatalf("closed session exceptions = %+v, want AUTO_OUT_PROVISIONAL for supervisor confirmation", closedSession.OpenExceptions)
	}

	// Idempotent replay: expiring again with the same key against the
	// closed result returns the same outcome, not an error.
	replaySession, replayOutcome, err := ExpireGrace(closedSession, expiresAt.Add(time.Hour), "auto-out-1")
	if err != nil {
		t.Fatalf("idempotent replay: unexpected error: %v", err)
	}
	if replayOutcome.Kind != outcome.Kind || replaySession.State != closedSession.State {
		t.Fatalf("idempotent replay = %+v, want same outcome as first expiry %+v", replayOutcome, outcome)
	}

	// A different key against the already-closed session is rejected: only
	// the matching open session may be auto-closed, and this one is no
	// longer open.
	_, _, err = ExpireGrace(closedSession, expiresAt.Add(time.Hour), "auto-out-2")
	if !errors.Is(err, ErrNotOpen) {
		t.Fatalf("second distinct expiry on closed session: err = %v, want ErrNotOpen", err)
	}
}

func TestUncertainLocationLeavesSessionOpen(t *testing.T) {
	session := openSessionForExit(t)

	noConsent := ExitEvidence{Exited: true, Consent: false, AccuracyMeters: 10, SustainedDuration: 10 * time.Minute}
	session, decision, err := EvaluateExit(exitPolicy, noConsent, session, t0(30))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind != ExitUncertain || session.PendingAutoOut != nil {
		t.Fatalf("no-consent exit: decision = %+v, pending = %+v", decision, session.PendingAutoOut)
	}
	if session.State != StateOpen {
		t.Fatalf("session state = %s, want OPEN (uncertain location never closes a session)", session.State)
	}
	if len(session.OpenExceptions) != 1 || session.OpenExceptions[0].Kind != ExceptionUncertainLocation {
		t.Fatalf("exceptions = %+v, want one UNCERTAIN_LOCATION", session.OpenExceptions)
	}

	poorAccuracy := ExitEvidence{Exited: true, Consent: true, AccuracyMeters: 5000, SustainedDuration: 10 * time.Minute}
	session2, decision2, err := EvaluateExit(exitPolicy, poorAccuracy, session, t0(31))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision2.Kind != ExitUncertain || session2.PendingAutoOut != nil {
		t.Fatalf("poor-accuracy exit: decision = %+v", decision2)
	}
}

// TestTodo_FTIME_011_Security proves that revoked consent or an
// insufficiently accurate signal can never be forced into a confirmed exit
// by an attacker replaying "Exited: true" without real evidence: the grace
// timer only ever starts on consented, accurate, sustained evidence.
func TestTodo_FTIME_011_Security(t *testing.T) {
	session := openSessionForExit(t)
	spoofedExit := ExitEvidence{Exited: true, Consent: false, AccuracyMeters: 1, SustainedDuration: time.Hour}
	session, decision, err := EvaluateExit(exitPolicy, spoofedExit, session, t0(30))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Kind == ExitGraceStarted || session.PendingAutoOut != nil {
		t.Fatalf("unconsented evidence started a grace timer: decision = %+v", decision)
	}
}

// TestTodo_FTIME_011_Race proves concurrent ExpireGrace replays with the
// same idempotency key against the same already-closed session all
// observe the identical single AUTO_OUT outcome; run with `go test -race`.
func TestTodo_FTIME_011_Race(t *testing.T) {
	session := openSessionForExit(t)
	sustained := ExitEvidence{Exited: true, Consent: true, AccuracyMeters: 10, SustainedDuration: 10 * time.Minute}
	session, decision, err := EvaluateExit(exitPolicy, sustained, session, t0(30))
	if err != nil || decision.Kind != ExitGraceStarted {
		t.Fatalf("setup exit: decision = %+v, err = %v", decision, err)
	}
	closed, first, err := ExpireGrace(session, decision.ExpiresAt.Add(time.Second), "race-key")
	if err != nil {
		t.Fatalf("setup expiry: unexpected error: %v", err)
	}

	const n = 64
	results := make([]Outcome, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, outcome, err := ExpireGrace(closed, decision.ExpiresAt.Add(time.Hour), "race-key")
			results[i] = outcome
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, errs[i])
		}
		if results[i].Kind != first.Kind || results[i].Session.State != first.Session.State {
			t.Fatalf("goroutine %d outcome = %+v, want %+v", i, results[i], first)
		}
	}
}

// TestTodo_FTIME_011_Fault covers the typed-error red paths: expiring with
// no grace pending, expiring before the grace matures, and starting or
// expiring against a session that is not open.
func TestTodo_FTIME_011_Fault(t *testing.T) {
	session := openSessionForExit(t)

	if _, _, err := ExpireGrace(session, t0(60), "key"); !errors.Is(err, ErrNoGraceStarted) {
		t.Fatalf("expire with no grace pending: err = %v, want ErrNoGraceStarted", err)
	}

	closedSession, _, err := Apply(session, func() Punch { p := basePunch(PunchOut, 480); p.ExpectedRevision = session.Revision; return p }())
	if err != nil {
		t.Fatalf("setup clock-out: unexpected error: %v", err)
	}
	if _, _, err := EvaluateExit(exitPolicy, ExitEvidence{Exited: true, Consent: true, AccuracyMeters: 5, SustainedDuration: time.Hour}, closedSession, t0(500)); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("evaluate exit on closed session: err = %v, want ErrNotOpen", err)
	}
	if _, _, err := ExpireGrace(closedSession, t0(500), "key"); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("expire grace on closed session: err = %v, want ErrNotOpen", err)
	}

	badPolicy := ExitPolicy{}
	if _, _, err := EvaluateExit(badPolicy, ExitEvidence{Exited: true, Consent: true, AccuracyMeters: 5, SustainedDuration: time.Hour}, session, t0(30)); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("evaluate exit with empty policy: err = %v, want ErrInvalidPolicy", err)
	}
}
