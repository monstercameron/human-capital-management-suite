package punchpolicy

import (
	"fmt"
	"strings"
	"time"
)

// LaborSegment is one open or closed span of a punch session attributed to
// a job and cost code. A session's segments always tile the session with no
// gap and no overlap: Transfer is the only way a new segment opens, and it
// always closes the one before it at the same instant the next one starts.
type LaborSegment struct {
	SessionID string
	JobCode   string
	CostCode  string
	Interval  Interval
}

// Open reports whether the segment has not yet been closed by a transfer or
// an out punch (Interval.End is zero).
func (s LaborSegment) Open() bool { return s.Interval.End.IsZero() }

func (s LaborSegment) validateOpen() error {
	if strings.TrimSpace(s.SessionID) == "" {
		return fmt.Errorf("%w: labor segment requires a session id", ErrInvalidTransfer)
	}
	if strings.TrimSpace(s.JobCode) == "" && strings.TrimSpace(s.CostCode) == "" {
		return fmt.Errorf("%w: labor segment requires a job or a cost code", ErrInvalidTransfer)
	}
	if s.Interval.Start.IsZero() {
		return fmt.Errorf("%w: labor segment requires a start", ErrInvalidTransfer)
	}
	if !s.Open() {
		return fmt.Errorf("%w: labor segment is already closed", ErrInvalidTransfer)
	}
	return nil
}

// TransferRequest is a worker's job or cost-code transfer at the device.
type TransferRequest struct {
	SessionID string
	At        time.Time
	JobCode   string
	CostCode  string
}

// Transfer closes open at At and opens the next segment with the requested
// job and cost code on the same session. At must be strictly after open's
// start, the session ids must match, and the requested job/cost code must
// actually differ from open's: a transfer that names no change is refused
// rather than silently splitting a session into two identical segments.
func Transfer(open LaborSegment, req TransferRequest) (closed LaborSegment, next LaborSegment, err error) {
	if err := open.validateOpen(); err != nil {
		return LaborSegment{}, LaborSegment{}, err
	}
	if strings.TrimSpace(req.SessionID) == "" || req.SessionID != open.SessionID {
		return LaborSegment{}, LaborSegment{}, fmt.Errorf("%w: transfer session %q does not match open segment session %q", ErrInvalidTransfer, req.SessionID, open.SessionID)
	}
	if req.At.IsZero() || !req.At.After(open.Interval.Start) {
		return LaborSegment{}, LaborSegment{}, fmt.Errorf("%w: transfer time must be strictly after the open segment's start", ErrInvalidTransfer)
	}
	if strings.TrimSpace(req.JobCode) == "" && strings.TrimSpace(req.CostCode) == "" {
		return LaborSegment{}, LaborSegment{}, fmt.Errorf("%w: transfer requires a job or a cost code", ErrInvalidTransfer)
	}
	if req.JobCode == open.JobCode && req.CostCode == open.CostCode {
		return LaborSegment{}, LaborSegment{}, ErrNoTransferChange
	}

	closed = open
	closed.Interval.End = req.At
	next = LaborSegment{
		SessionID: open.SessionID,
		JobCode:   req.JobCode,
		CostCode:  req.CostCode,
		Interval:  Interval{Start: req.At},
	}
	return closed, next, nil
}
