package timeclockstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
)

func TestBatchAdapterRejectsMultipleSessionEvents(t *testing.T) {
	a := Adapter{}
	work := &clockservice.PunchWork{SessionEvents: []clockservice.SessionEvent{{Kind: "OPENED"}, {Kind: "EXTRA"}}}
	_, err := a.CommitPunchBatch(context.Background(), "tenant", "device", []clockservice.BatchCommitEntry{{Sequence: 1, Work: work}})
	if err == nil || errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("err=%v, want multi-event validation before store availability", err)
	}
}

func TestBatchAdapterNilStoreFailsClosed(t *testing.T) {
	a := Adapter{}
	_, err := a.CommitPunchBatch(context.Background(), "tenant", "device", []clockservice.BatchCommitEntry{{Sequence: 1}})
	if !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("err=%v, want ErrUnavailable", err)
	}
}
