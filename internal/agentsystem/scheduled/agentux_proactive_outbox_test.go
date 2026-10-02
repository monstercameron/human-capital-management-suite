package scheduled

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAgentUXProactive_ScheduledSourceAndRevision_Security(t *testing.T) {
	owner, state, _, record := ownerFixture(t)
	record.SourceKind = agentrun.SourceAnnouncement
	record.RequesterID = record.OwnerID
	record.Persona = &agentrun.PersonaRef{ID: "policy-helper", Version: "6", Digest: "sha256:" + strings.Repeat("a", 64)}
	record.State, record.Revision, record.Cursor = StateActive, 1, values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	record.Misfire = schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, MaxCatchUp: 1}
	state.state = record
	if err := ValidateSchedule(record); err != nil {
		t.Fatal(err)
	}
	outbox := &memoryOutbox{state: state, deliveries: map[string]Delivery{}, receipts: map[string]Receipt{}}
	inbox, _ := testInbox()
	worker, err := NewOutboxWorker(owner, outbox, frozenContext{}, inbox)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Plan(context.Background(), record.Trigger.Definition.TenantID, record.Trigger.Definition.ID, record.Cursor.Time().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	pending, err := outbox.Pending(context.Background(), record.Trigger.Definition.TenantID, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("occurrences=%+v %v", pending, err)
	}
	request := pending[0].Request
	if request.Source.Kind != agentrun.SourceAnnouncement || request.CauseID != pending[0].Key || request.Principal.Mode != agentrun.ModeSponsored || request.Principal.InvokerID != "" || request.Principal.RequesterID != record.OwnerID || request.Principal.SponsorID == record.OwnerID || request.Persona == nil || *request.Persona != *record.Persona {
		t.Fatalf("announcement lost service actor or occurrence: %+v", request)
	}
	if err = worker.CheckRequest(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	forged := request
	forged.Principal.RequesterID = "outsider"
	if err = worker.CheckRequest(context.Background(), forged); !errors.Is(err, ErrFiringRefused) {
		t.Fatalf("forged owner accepted: %v", err)
	}
	state.state.Revision++
	if err = worker.CheckRequest(context.Background(), request); !errors.Is(err, ErrRevision) {
		t.Fatalf("stale occurrence accepted: %v", err)
	}
}
