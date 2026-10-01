package timecard

const schemaVersion = 1

// Version reports this package's contract version. A change to the meaning
// or shape of the timecard, review, allocation, duration or exception-
// period contracts must advance this value.
func Version() int { return schemaVersion }

// Explanation is a pure, reproducible summary of one timecard's current
// lifecycle state for operator display. It grants no authority.
type Explanation struct {
	WorkerID           string
	State              State
	Revision           uint64
	TotalMinutes       int
	WorkerAttested     bool
	SupervisorAttested bool
	Approved           bool
}

// Explain returns a pure explanation of a timecard's current state.
func (tc Timecard) Explain() Explanation {
	digest := linesDigest(tc.Lines)
	return Explanation{
		WorkerID: tc.WorkerID, State: tc.State, Revision: tc.Revision, TotalMinutes: tc.TotalMinutes(),
		WorkerAttested:     tc.WorkerAttestation != nil && tc.WorkerAttestation.bound(digest),
		SupervisorAttested: tc.SupervisorAttestation != nil && tc.SupervisorAttestation.bound(digest),
		Approved:           tc.Approval != nil,
	}
}

// Explain is the package-level spelling of Timecard.Explain.
func Explain(tc Timecard) Explanation { return tc.Explain() }
