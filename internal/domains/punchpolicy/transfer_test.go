package punchpolicy

import (
	"errors"
	"testing"
	"time"
)

func fixtureOpenSegment() LaborSegment {
	start := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	return LaborSegment{SessionID: "session-1", JobCode: "JOB-A", CostCode: "CC-100", Interval: Interval{Start: start}}
}

func TestTransfer_ClosesAndOpens(t *testing.T) {
	open := fixtureOpenSegment()
	transferAt := open.Interval.Start.Add(3 * time.Hour)

	closed, next, err := Transfer(open, TransferRequest{SessionID: "session-1", At: transferAt, JobCode: "JOB-B", CostCode: "CC-200"})
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if closed.Open() {
		t.Fatalf("the closed segment must have a non-zero end")
	}
	if !closed.Interval.End.Equal(transferAt) {
		t.Fatalf("closed segment end: got %v want %v", closed.Interval.End, transferAt)
	}
	if !next.Open() {
		t.Fatalf("the next segment must be open")
	}
	if !next.Interval.Start.Equal(transferAt) {
		t.Fatalf("next segment start must equal the transfer time")
	}
	if next.JobCode != "JOB-B" || next.CostCode != "CC-200" {
		t.Fatalf("next segment did not carry the requested job/cost code: got %s/%s", next.JobCode, next.CostCode)
	}
	if next.SessionID != open.SessionID {
		t.Fatalf("next segment must stay on the same session")
	}
	// Segments must tile the session: no gap, no overlap.
	if !closed.Interval.End.Equal(next.Interval.Start) {
		t.Fatalf("closed end and next start must be the same instant, got %v and %v", closed.Interval.End, next.Interval.Start)
	}
}

func TestTransfer_NoChangeRejected(t *testing.T) {
	open := fixtureOpenSegment()
	_, _, err := Transfer(open, TransferRequest{SessionID: "session-1", At: open.Interval.Start.Add(time.Hour), JobCode: "JOB-A", CostCode: "CC-100"})
	if !errors.Is(err, ErrNoTransferChange) {
		t.Fatalf("a transfer naming no change should fail with ErrNoTransferChange, got %v", err)
	}
}

func TestTransfer_InvalidRequests(t *testing.T) {
	open := fixtureOpenSegment()

	if _, _, err := Transfer(open, TransferRequest{SessionID: "session-2", At: open.Interval.Start.Add(time.Hour), JobCode: "JOB-B"}); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("mismatched session should fail with ErrInvalidTransfer, got %v", err)
	}
	if _, _, err := Transfer(open, TransferRequest{SessionID: "session-1", At: open.Interval.Start, JobCode: "JOB-B"}); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("a transfer at or before the segment start should fail with ErrInvalidTransfer, got %v", err)
	}
	alreadyClosed := open
	alreadyClosed.Interval.End = open.Interval.Start.Add(time.Hour)
	if _, _, err := Transfer(alreadyClosed, TransferRequest{SessionID: "session-1", At: open.Interval.Start.Add(2 * time.Hour), JobCode: "JOB-B"}); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("transferring an already-closed segment should fail with ErrInvalidTransfer, got %v", err)
	}
	noCode := open
	noCode.JobCode, noCode.CostCode = "", ""
	if _, _, err := Transfer(noCode, TransferRequest{SessionID: "session-1", At: open.Interval.Start.Add(time.Hour), JobCode: "JOB-B"}); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("an open segment with no job or cost code should fail with ErrInvalidTransfer, got %v", err)
	}
	if _, _, err := Transfer(open, TransferRequest{SessionID: "session-1", At: open.Interval.Start.Add(time.Hour)}); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("a transfer naming neither a job nor a cost code should fail with ErrInvalidTransfer, got %v", err)
	}
}
