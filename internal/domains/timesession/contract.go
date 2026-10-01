// Package timesession owns the pure clock-session domain: how IN, BREAK,
// MEAL and TRANSFER punches move a worker's session between OPEN, ON_BREAK,
// CLOSED and AUTO_CLOSED (FTIME-003), how a confirmed jobsite exit closes an
// unattended session under a server-owned grace timer (FTIME-011), how a
// worker's missed-punch request is decided under segregation of duties
// (TCLOCK-011), and how one punch's supplied facts classify into ACCEPT,
// REVIEW, HOLD or DUPLICATE with fixed precedence (WTIME-003's pure
// classifier, mirrored from internal/workflow/conformance/time's DECISION
// routes for the reasons that overlap).
//
// Every function here is pure: no database, no network, no package-level
// mutable state, and no clock of its own. Every instant a decision depends
// on is a parameter (Punch.DeviceTime, Punch.ServerReceiptTime, or an
// explicit now); nothing calls time.Now.
package timesession

const contractVersion = 1

// Version is this package's contract version. A change to the meaning or
// shape of Session, Outcome, Classification or a decision result advances
// it (ARCH-GO-009).
func Version() int { return contractVersion }

// Explanation is the descriptive, non-authoritative summary of a Session's
// current shape, for audit and support tooling. It never mutates and never
// fails: an empty Session explains as the zero state.
type Explanation struct {
	SessionID          string
	Tenant             string
	Worker             string
	Assignment         string
	State              SessionState
	SegmentCount       int
	OpenExceptionCount int
	Revision           uint64
	HasPendingAutoOut  bool
}

// Explain summarizes a session.
func (s Session) Explain() Explanation {
	return Explanation{
		SessionID:          s.SessionID,
		Tenant:             s.Tenant,
		Worker:             s.Worker,
		Assignment:         s.Assignment,
		State:              s.State,
		SegmentCount:       len(s.Segments),
		OpenExceptionCount: len(s.OpenExceptions),
		Revision:           s.Revision,
		HasPendingAutoOut:  s.PendingAutoOut != nil,
	}
}

// Explain is the free-function spelling of Session.Explain, matching the
// package-level Explain convention used elsewhere in internal/domains.
func Explain(s Session) Explanation { return s.Explain() }
