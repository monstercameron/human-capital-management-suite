package timeclockstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

func TestTodo_TCLOCK011_MissingPunchStore_FailsClosedWithoutStore(t *testing.T) {
	ctx := context.Background()
	if _, err := (Adapter{}).GetMissingPunchSession(ctx, "tenant", "session"); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("session read error=%v, want unavailable", err)
	}
	if _, err := (Adapter{}).GetMissingPunchObservation(ctx, "tenant", "observation"); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("observation read error=%v, want unavailable", err)
	}
	if _, err := (Adapter{}).GetMissingPunchRequest(ctx, "tenant", "request"); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("request read error=%v, want unavailable", err)
	}
}

func TestTodo_TCLOCK011_MissingPunchStore_RequestRequiresValidStore(t *testing.T) {
	ctx := context.Background()
	if _, err := (Adapter{Store: &timestore.Store{}}).GetMissingPunchRequest(ctx, "tenant", "request"); !errors.Is(err, timestore.ErrInvalid) {
		t.Fatalf("request read error=%v, want invalid store", err)
	}
}

func TestTodo_TCLOCK011_MissingPunchStore_ParsesOnlyPersistedException(t *testing.T) {
	missing, closed, workflow := sessionMissingOutFacts([]byte(`{"open_exceptions":[{"kind":"MISSING_OUT"}],"period_closed":true,"original_workflow_instance_ref":"wf-1"}`))
	if !missing || !closed || workflow != "wf-1" {
		t.Fatalf("facts=%v,%v,%q, want true,true,wf-1", missing, closed, workflow)
	}
	missing, closed, workflow = sessionMissingOutFacts([]byte(`{"status":"OPEN"}`))
	if missing || closed || workflow != "" {
		t.Fatalf("status-only payload fabricated facts=%v,%v,%q", missing, closed, workflow)
	}
}
