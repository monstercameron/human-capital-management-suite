package timesession

import (
	"fmt"
	"strings"
	"time"
)

// SessionState is the closed vocabulary of clock-session states (FTIME-003).
type SessionState string

const (
	StateOpen       SessionState = "OPEN"
	StateOnBreak    SessionState = "ON_BREAK"
	StateClosed     SessionState = "CLOSED"
	StateAutoClosed SessionState = "AUTO_CLOSED"
)

// Valid reports whether s is a declared state. The empty string is
// deliberately invalid: it means "no session exists yet", not a state.
func (s SessionState) Valid() bool {
	switch s {
	case StateOpen, StateOnBreak, StateClosed, StateAutoClosed:
		return true
	default:
		return false
	}
}

func orNone(s SessionState) string {
	if s == "" {
		return "NONE"
	}
	return string(s)
}

// SegmentKind is what a session segment records time against.
type SegmentKind string

const (
	SegmentWork        SegmentKind = "WORK"
	SegmentBreak       SegmentKind = "BREAK"
	SegmentMeal        SegmentKind = "MEAL"
	SegmentTravel      SegmentKind = "TRAVEL"
	SegmentJobTransfer SegmentKind = "JOB_TRANSFER"
)

// Segment is one contiguous interval of a session's time, against an
// optional job/cost-code reference. End is zero while the segment is open.
type Segment struct {
	Kind        SegmentKind
	JobRef      string
	CostCodeRef string
	Start       time.Time
	End         time.Time
}

// Open reports whether the segment has not yet been closed.
func (s Segment) Open() bool { return s.End.IsZero() }

// IdentificationMethod is how the worker was identified at the punch.
type IdentificationMethod string

const (
	IdentPIN       IdentificationMethod = "PIN"
	IdentBadge     IdentificationMethod = "BADGE"
	IdentBiometric IdentificationMethod = "BIOMETRIC"
	IdentApp       IdentificationMethod = "APP"
	IdentDevice    IdentificationMethod = "DEVICE"
)

// PunchSource names where a punch originated and how the worker was
// identified there.
type PunchSource struct {
	Kind      string // e.g. KIOSK, MOBILE, HARDWARE_CLOCK, IMPORT, WEB
	DeviceRef string
	Method    IdentificationMethod
}

// ExceptionKind is the closed vocabulary of exceptions a session can carry
// without ever inventing the time or evidence it lacks.
type ExceptionKind string

const (
	ExceptionUnscheduledWork    ExceptionKind = "UNSCHEDULED_WORK"
	ExceptionMissingOut         ExceptionKind = "MISSING_OUT"
	ExceptionUncertainLocation  ExceptionKind = "UNCERTAIN_LOCATION"
	ExceptionAutoOutProvisional ExceptionKind = "AUTO_OUT_PROVISIONAL"
)

// Exception is a fact recorded against a session instead of a discarded or
// invented one.
type Exception struct {
	Kind   ExceptionKind
	Reason string
	At     time.Time
}

// AutoOutGrace is the server-owned grace timer FTIME-011 starts on
// sustained, sufficiently accurate exit evidence.
type AutoOutGrace struct {
	StartedAt time.Time
	ExpiresAt time.Time
}

// Session is the pure, replayable state of one worker's clock session. It
// carries just enough bookkeeping (LastIdempotencyKey/LastOutcome) to make
// Apply and ExpireGrace idempotent under a retried request; that bookkeeping
// is never present on the Outcome.Session snapshot returned to a caller.
type Session struct {
	Tenant     string
	Worker     string
	Assignment string
	SessionID  string
	State      SessionState
	Segments   []Segment
	Source     PunchSource
	Revision   uint64

	OpenExceptions []Exception
	PendingAutoOut *AutoOutGrace

	LastIdempotencyKey string
	LastOutcome        *Outcome
}

// cloneSession deep-copies the slices and pointer a transition mutates, and
// drops the previous idempotency bookkeeping so the caller cannot see a
// stale replay marker on a session that has since moved on.
func cloneSession(s Session) Session {
	next := s
	next.Segments = append([]Segment(nil), s.Segments...)
	next.OpenExceptions = append([]Exception(nil), s.OpenExceptions...)
	if s.PendingAutoOut != nil {
		g := *s.PendingAutoOut
		next.PendingAutoOut = &g
	}
	next.LastIdempotencyKey = ""
	next.LastOutcome = nil
	return next
}

func closeOpenSegments(s *Session, at time.Time) {
	for i := range s.Segments {
		if s.Segments[i].Open() {
			s.Segments[i].End = at
		}
	}
}

func lastOpenSegment(s Session) *Segment {
	for i := len(s.Segments) - 1; i >= 0; i-- {
		if s.Segments[i].Open() {
			return &s.Segments[i]
		}
	}
	return nil
}

// PunchKind is the closed vocabulary of punches Apply accepts.
type PunchKind string

const (
	PunchIn         PunchKind = "IN"
	PunchOut        PunchKind = "OUT"
	PunchBreakStart PunchKind = "BREAK_START"
	PunchBreakEnd   PunchKind = "BREAK_END"
	PunchMealStart  PunchKind = "MEAL_START"
	PunchMealEnd    PunchKind = "MEAL_END"
	PunchTransfer   PunchKind = "TRANSFER"
)

// Valid reports whether k is a declared punch kind.
func (k PunchKind) Valid() bool {
	switch k {
	case PunchIn, PunchOut, PunchBreakStart, PunchBreakEnd, PunchMealStart, PunchMealEnd, PunchTransfer:
		return true
	default:
		return false
	}
}

// Punch is one signed, source-attributed punch request. Worker is who the
// punch claims to clock; Actor is who is actually performing it. DeviceTime
// and ServerReceiptTime are kept distinct on purpose: Apply never collapses
// signed device time into server receipt time or the reverse.
type Punch struct {
	Kind       PunchKind
	Tenant     string
	Worker     string
	Actor      string
	Delegated  bool
	Assignment string

	// SessionID is the caller-chosen, deterministic session identifier. An
	// IN punch requires it (Apply creates no identifiers of its own); every
	// other punch, when set, must match the session it is applied to.
	SessionID string

	IdempotencyKey    string
	DeviceTime        time.Time
	ServerReceiptTime time.Time
	Source            PunchSource

	JobRef      string
	CostCodeRef string
	Travel      bool

	// Scheduled is the caller's resolved fact that this punch falls inside
	// a published shift. false does not reject the punch; it is captured as
	// an UNSCHEDULED_WORK exception instead.
	Scheduled bool

	ExpectedRevision uint64
}

func validatePunch(p Punch) error {
	if !p.Kind.Valid() {
		return fmt.Errorf("%w: kind %q is not declared", ErrInvalidPunch, p.Kind)
	}
	if strings.TrimSpace(p.Tenant) == "" || strings.TrimSpace(p.Worker) == "" ||
		strings.TrimSpace(p.Actor) == "" || strings.TrimSpace(p.Assignment) == "" {
		return fmt.Errorf("%w: tenant, worker, actor and assignment are required", ErrInvalidPunch)
	}
	if p.DeviceTime.IsZero() || p.ServerReceiptTime.IsZero() {
		return fmt.Errorf("%w: device time and server receipt time are required", ErrInvalidPunch)
	}
	return nil
}

// OutcomeKind is what a successful (or idempotently replayed) transition
// produced.
type OutcomeKind string

const (
	OutcomeOpened      OutcomeKind = "OPENED"
	OutcomeOnBreak     OutcomeKind = "ON_BREAK"
	OutcomeResumed     OutcomeKind = "RESUMED"
	OutcomeTransferred OutcomeKind = "TRANSFERRED"
	OutcomeClosed      OutcomeKind = "CLOSED"
	OutcomeAutoClosed  OutcomeKind = "AUTO_CLOSED"
)

// Outcome is Apply's (and ExpireGrace's) result: the resulting session
// snapshot, plus any exception the transition captured rather than
// discarded.
type Outcome struct {
	Kind      OutcomeKind
	Session   Session
	Exception *Exception
}

// Apply is the pure IN/OUT/BREAK/MEAL/TRANSFER transition function. It
// never mutates session in place: on any error it returns session
// unchanged, exactly as passed in.
func Apply(session Session, punch Punch) (Session, Outcome, error) {
	if err := validatePunch(punch); err != nil {
		return session, Outcome{}, err
	}
	if punch.IdempotencyKey != "" && session.LastIdempotencyKey == punch.IdempotencyKey && session.LastOutcome != nil {
		return session, *session.LastOutcome, fmt.Errorf("%w: idempotency_key=%s", ErrDuplicatePunch, punch.IdempotencyKey)
	}
	if punch.Worker != punch.Actor && !punch.Delegated {
		return session, Outcome{}, fmt.Errorf("%w: actor=%s worker=%s", ErrDelegationRequired, punch.Actor, punch.Worker)
	}
	switch punch.Kind {
	case PunchIn:
		return applyIn(session, punch)
	case PunchOut:
		return applyOut(session, punch)
	case PunchBreakStart:
		return applyBreakStart(session, punch, SegmentBreak)
	case PunchMealStart:
		return applyBreakStart(session, punch, SegmentMeal)
	case PunchBreakEnd:
		return applyBreakEnd(session, punch, SegmentBreak)
	case PunchMealEnd:
		return applyBreakEnd(session, punch, SegmentMeal)
	case PunchTransfer:
		return applyTransfer(session, punch)
	default:
		return session, Outcome{}, fmt.Errorf("%w: kind %q", ErrInvalidPunch, punch.Kind)
	}
}

func finish(next Session, kind OutcomeKind, punch Punch, exc *Exception) (Session, Outcome, error) {
	outcome := Outcome{Kind: kind, Session: next, Exception: exc}
	next.LastIdempotencyKey = punch.IdempotencyKey
	next.LastOutcome = &outcome
	return next, outcome, nil
}

func applyIn(session Session, punch Punch) (Session, Outcome, error) {
	if session.State != "" {
		return session, Outcome{}, fmt.Errorf("%w: session %s is %s", ErrAlreadyOpen, session.SessionID, orNone(session.State))
	}
	if strings.TrimSpace(punch.SessionID) == "" {
		return session, Outcome{}, fmt.Errorf("%w: clock-in requires a session id", ErrInvalidPunch)
	}
	next := Session{
		Tenant: punch.Tenant, Worker: punch.Worker, Assignment: punch.Assignment,
		SessionID: punch.SessionID, State: StateOpen, Source: punch.Source, Revision: 1,
		Segments: []Segment{{Kind: SegmentWork, JobRef: punch.JobRef, CostCodeRef: punch.CostCodeRef, Start: punch.DeviceTime}},
	}
	var exc *Exception
	if !punch.Scheduled {
		next.OpenExceptions = append(next.OpenExceptions, Exception{
			Kind: ExceptionUnscheduledWork, Reason: "clock-in has no matching published shift", At: punch.DeviceTime,
		})
		exc = &next.OpenExceptions[len(next.OpenExceptions)-1]
	}
	return finish(next, OutcomeOpened, punch, exc)
}

func applyOut(session Session, punch Punch) (Session, Outcome, error) {
	if session.State != StateOpen && session.State != StateOnBreak {
		return session, Outcome{}, fmt.Errorf("%w: session %s is %s", ErrNotOpen, session.SessionID, orNone(session.State))
	}
	if punch.SessionID != "" && punch.SessionID != session.SessionID {
		return session, Outcome{}, fmt.Errorf("%w: punch session %s does not match %s", ErrInvalidPunch, punch.SessionID, session.SessionID)
	}
	if punch.ExpectedRevision != session.Revision {
		return session, Outcome{}, fmt.Errorf("%w: expected=%d actual=%d", ErrStaleRevision, punch.ExpectedRevision, session.Revision)
	}
	next := cloneSession(session)
	closeOpenSegments(&next, punch.DeviceTime)
	next.State = StateClosed
	next.Revision++
	return finish(next, OutcomeClosed, punch, nil)
}

func applyBreakStart(session Session, punch Punch, kind SegmentKind) (Session, Outcome, error) {
	if session.State == StateOnBreak {
		return session, Outcome{}, fmt.Errorf("%w: session %s", ErrAlreadyOnBreak, session.SessionID)
	}
	if session.State != StateOpen {
		return session, Outcome{}, fmt.Errorf("%w: session %s is %s", ErrNotOpen, session.SessionID, orNone(session.State))
	}
	if punch.SessionID != "" && punch.SessionID != session.SessionID {
		return session, Outcome{}, fmt.Errorf("%w: punch session %s does not match %s", ErrInvalidPunch, punch.SessionID, session.SessionID)
	}
	if punch.ExpectedRevision != session.Revision {
		return session, Outcome{}, fmt.Errorf("%w: expected=%d actual=%d", ErrStaleRevision, punch.ExpectedRevision, session.Revision)
	}
	next := cloneSession(session)
	closeOpenSegments(&next, punch.DeviceTime)
	next.Segments = append(next.Segments, Segment{Kind: kind, JobRef: punch.JobRef, CostCodeRef: punch.CostCodeRef, Start: punch.DeviceTime})
	next.State = StateOnBreak
	next.Revision++
	return finish(next, OutcomeOnBreak, punch, nil)
}

func applyBreakEnd(session Session, punch Punch, kind SegmentKind) (Session, Outcome, error) {
	if session.State != StateOnBreak {
		return session, Outcome{}, fmt.Errorf("%w: session %s is %s", ErrNotOnBreak, session.SessionID, orNone(session.State))
	}
	last := lastOpenSegment(session)
	if last == nil || last.Kind != kind {
		return session, Outcome{}, fmt.Errorf("%w: open segment is not %s", ErrNotOnBreak, kind)
	}
	if punch.SessionID != "" && punch.SessionID != session.SessionID {
		return session, Outcome{}, fmt.Errorf("%w: punch session %s does not match %s", ErrInvalidPunch, punch.SessionID, session.SessionID)
	}
	if punch.ExpectedRevision != session.Revision {
		return session, Outcome{}, fmt.Errorf("%w: expected=%d actual=%d", ErrStaleRevision, punch.ExpectedRevision, session.Revision)
	}
	next := cloneSession(session)
	closeOpenSegments(&next, punch.DeviceTime)
	next.Segments = append(next.Segments, Segment{Kind: SegmentWork, JobRef: punch.JobRef, CostCodeRef: punch.CostCodeRef, Start: punch.DeviceTime})
	next.State = StateOpen
	next.Revision++
	return finish(next, OutcomeResumed, punch, nil)
}

func applyTransfer(session Session, punch Punch) (Session, Outcome, error) {
	if session.State != StateOpen {
		return session, Outcome{}, fmt.Errorf("%w: session %s is %s", ErrNotOpen, session.SessionID, orNone(session.State))
	}
	if punch.SessionID != "" && punch.SessionID != session.SessionID {
		return session, Outcome{}, fmt.Errorf("%w: punch session %s does not match %s", ErrInvalidPunch, punch.SessionID, session.SessionID)
	}
	if punch.ExpectedRevision != session.Revision {
		return session, Outcome{}, fmt.Errorf("%w: expected=%d actual=%d", ErrStaleRevision, punch.ExpectedRevision, session.Revision)
	}
	next := cloneSession(session)
	closeOpenSegments(&next, punch.DeviceTime)
	kind := SegmentWork
	switch {
	case punch.Travel:
		kind = SegmentTravel
	case punch.JobRef != "" || punch.CostCodeRef != "":
		kind = SegmentJobTransfer
	}
	next.Segments = append(next.Segments, Segment{Kind: kind, JobRef: punch.JobRef, CostCodeRef: punch.CostCodeRef, Start: punch.DeviceTime})
	next.Revision++
	return finish(next, OutcomeTransferred, punch, nil)
}

// MissingOut is called when a scheduled-end WAIT (WTIME-003's TimeExpr WAIT)
// expires without an OUT punch. It never invents a close time: it appends a
// MISSING_OUT exception to the still-open session and leaves every segment
// exactly as it was.
func MissingOut(session Session, scheduledEnd time.Time, graceExpired bool) (Session, Exception, error) {
	if session.State != StateOpen && session.State != StateOnBreak {
		return session, Exception{}, fmt.Errorf("%w: session %s is %s", ErrNotOpen, session.SessionID, orNone(session.State))
	}
	if scheduledEnd.IsZero() {
		return session, Exception{}, fmt.Errorf("%w: scheduled end is required", ErrInvalidPunch)
	}
	if !graceExpired {
		return session, Exception{}, ErrGraceNotExpired
	}
	exc := Exception{Kind: ExceptionMissingOut, Reason: "no out punch by scheduled end", At: scheduledEnd}
	next := cloneSession(session)
	next.OpenExceptions = append(next.OpenExceptions, exc)
	return next, exc, nil
}
