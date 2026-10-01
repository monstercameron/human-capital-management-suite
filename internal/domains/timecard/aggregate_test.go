package timecard

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"
)

func mustNewTimecard(t *testing.T) Timecard {
	t.Helper()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	tc, err := NewTimecard("tenant-a", "worker-1", "assignment-1", start, start.Add(7*24*time.Hour))
	if err != nil {
		t.Fatalf("NewTimecard: %v", err)
	}
	return tc
}

func sessionLine(id string, minutes int) Line {
	return Line{ID: id, Kind: LineSession, Minutes: minutes, SourceRefs: []string{"punch-" + id}}
}

func rulesRef() attendance.VersionedRef {
	return attendance.VersionedRef{ID: "rules-1", Version: "v1"}
}

// TestTodo_WF_CAP_007 is the PRIMARY test: a full submit/attest/approve/lock
// lifecycle, and the RED conditions -- an edit after attestation stales the
// other party's attestation, and approval never fires without both parties
// bound to the live content.
func TestTodo_WF_CAP_007(t *testing.T) {
	tc := mustNewTimecard(t)
	now := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)

	tc, err := SetLines(tc, []Line{sessionLine("l1", 480), sessionLine("l2", 60)}, now, "worker-1")
	if err != nil {
		t.Fatalf("SetLines: %v", err)
	}
	if tc.Revision != 2 {
		t.Fatalf("revision after first edit = %d, want 2", tc.Revision)
	}

	tc, err = Submit(tc, now, "worker-1")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if tc.State != Submitted {
		t.Fatalf("state after submit = %s, want SUBMITTED", tc.State)
	}

	tc, err = Attest(tc, PartyWorker, "worker-1", now)
	if err != nil {
		t.Fatalf("Attest worker: %v", err)
	}
	if tc.State != Submitted {
		t.Fatalf("state after one attestation = %s, want SUBMITTED (supervisor has not attested)", tc.State)
	}

	// An edit after the worker's attestation must stale it: approval later
	// must fail until the worker re-attests the new content.
	tc, err = SetLines(tc, []Line{sessionLine("l1", 500), sessionLine("l2", 60)}, now, "supervisor-1")
	if err != nil {
		t.Fatalf("SetLines after attestation: %v", err)
	}
	digest := linesDigest(tc.Lines)
	if tc.WorkerAttestation.bound(digest) {
		t.Fatalf("worker attestation is still bound to the live content after an edit")
	}

	tc, err = Attest(tc, PartySupervisor, "supervisor-1", now)
	if err != nil {
		t.Fatalf("Attest supervisor: %v", err)
	}
	if tc.State != Submitted {
		t.Fatalf("state = %s, want SUBMITTED because the worker's attestation is stale", tc.State)
	}
	if _, err := Approve(tc, "supervisor-1", rulesRef(), tc.Revision, nil, now); !errors.Is(err, ErrTimecardRejected) {
		t.Fatalf("Approve with a stale attestation and wrong state: got %v, want ErrTimecardRejected", err)
	}

	// Worker re-attests against the edited content; now both bind.
	tc, err = Attest(tc, PartyWorker, "worker-1", now)
	if err != nil {
		t.Fatalf("Attest worker again: %v", err)
	}
	if tc.State != Attested {
		t.Fatalf("state after both attestations bind = %s, want ATTESTED", tc.State)
	}

	if _, err := Approve(tc, "supervisor-1", rulesRef(), tc.Revision+1, nil, now); !errors.Is(err, ErrTimecardRejected) {
		t.Fatalf("Approve with mismatched reviewed revision: got %v, want ErrTimecardRejected", err)
	}

	blocking := []attendance.Finding{{Kind: attendance.MissingException, ShiftID: "s1"}}
	if _, err := Approve(tc, "supervisor-1", rulesRef(), tc.Revision, blocking, now); !errors.Is(err, ErrTimecardRejected) {
		t.Fatalf("Approve with an unresolved exception: got %v, want ErrTimecardRejected", err)
	}

	tc, err = Approve(tc, "supervisor-1", rulesRef(), tc.Revision, nil, now)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if tc.State != Approved || tc.Approval == nil {
		t.Fatalf("state after approve = %s, approval = %v", tc.State, tc.Approval)
	}
	if tc.Approval.ReviewedRevision != tc.Revision {
		t.Fatalf("approval pins revision %d, want %d", tc.Approval.ReviewedRevision, tc.Revision)
	}

	tc, err = Lock(tc, now, "supervisor-1")
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if tc.State != Locked {
		t.Fatalf("state after lock = %s, want LOCKED", tc.State)
	}
	if _, err := SetLines(tc, []Line{sessionLine("l1", 999)}, now, "supervisor-1"); !errors.Is(err, ErrTimecardRejected) {
		t.Fatalf("SetLines on a locked timecard: got %v, want ErrTimecardRejected", err)
	}

	tc, err = Reopen(tc, ReopenSupervisorCorrection, "supervisor-1", now)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if tc.State != Reopened {
		t.Fatalf("state after reopen = %s, want REOPENED", tc.State)
	}
	last := tc.History[len(tc.History)-1]
	if last.Reason != string(ReopenSupervisorCorrection) || last.Detail == "" {
		t.Fatalf("reopen history entry = %+v, want a typed reason and the prior approval detail", last)
	}
}

// TestTodo_WF_CAP_007_Property proves totals equal the punch sum: for many
// generated sets of paired punches, the aggregate's TotalMinutes always
// equals the independently-summed minutes of the closed session lines it
// was built from, regardless of ordering.
func TestTodo_WF_CAP_007_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(20260101))
	for trial := 0; trial < 200; trial++ {
		n := 1 + rng.Intn(12)
		var lines []Line
		wantTotal := 0
		for i := 0; i < n; i++ {
			minutes := rng.Intn(600)
			open := rng.Intn(4) == 0
			id := "line-" + time.Duration(i).String()
			if open {
				lines = append(lines, Line{ID: id, Kind: LineSession, Minutes: 0, Open: true, SourceRefs: []string{"src-" + id}})
				continue
			}
			lines = append(lines, Line{ID: id, Kind: LineSession, Minutes: minutes, SourceRefs: []string{"src-" + id}})
			wantTotal += minutes
		}
		// Shuffle: order must not affect the total.
		rng.Shuffle(len(lines), func(i, j int) { lines[i], lines[j] = lines[j], lines[i] })

		tc := mustNewTimecard(t)
		now := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)
		tc, err := SetLines(tc, lines, now, "worker-1")
		if err != nil {
			t.Fatalf("trial %d: SetLines: %v", trial, err)
		}
		if got := tc.TotalMinutes(); got != wantTotal {
			t.Fatalf("trial %d: TotalMinutes = %d, want %d (manual sum of closed lines)", trial, got, wantTotal)
		}
	}
}

func TestTodo_WF_CAP_007_RejectAndInvalidStates(t *testing.T) {
	tc := mustNewTimecard(t)
	now := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)
	tc, err := SetLines(tc, []Line{sessionLine("l1", 120)}, now, "worker-1")
	if err != nil {
		t.Fatalf("SetLines: %v", err)
	}
	tc, err = Submit(tc, now, "worker-1")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	tc, err = Reject(tc, RejectMissingEvidence, "supervisor-1", now)
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if tc.State != Draft {
		t.Fatalf("state after reject = %s, want DRAFT", tc.State)
	}
	if _, err := Reject(tc, "NOT_A_REASON", "supervisor-1", now); !errors.Is(err, ErrTimecardRejected) {
		t.Fatalf("Reject with an undeclared reason: got %v, want ErrTimecardRejected", err)
	}
	if _, err := Reopen(tc, ReopenWorkerDispute, "worker-1", now); !errors.Is(err, ErrTimecardRejected) {
		t.Fatalf("Reopen a draft timecard: got %v, want ErrTimecardRejected", err)
	}
}
