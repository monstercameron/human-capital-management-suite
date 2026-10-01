package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/recruiting"
)

func TestTodo_RECRUIT_001_Served(t *testing.T) {
	surface := NewServedRecruitingSurface()
	if surface.Version == nil || surface.NewAggregate == nil {
		t.Fatal("served recruiting surface omitted the authoritative aggregate")
	}
	aggregate, err := surface.NewAggregate("candidate+requisition")
	if err != nil {
		t.Fatalf("new served recruiting aggregate: %v", err)
	}
	if aggregate.UniquenessPolicy != "candidate+requisition" || aggregate.Events != nil || aggregate.Outbox != nil {
		t.Fatalf("served aggregate = %+v, want an empty value-owned ATS boundary", aggregate)
	}
}

func TestTodo_RECRUIT_002_Served(t *testing.T) {
	surface := NewServedRecruitingSurface()
	if surface.NewStageLedger == nil {
		t.Fatal("served recruiting surface omitted governed stage construction")
	}
	if _, err := surface.NewStageLedger(nil, []string{"decider:recruiting"}); err == nil {
		t.Fatal("served stage construction accepted a missing aggregate")
	}
}

func TestTodo_RECRUIT_003_Served(t *testing.T) {
	surface := NewServedRecruitingSurface()
	if surface.NewOfferLedger == nil {
		t.Fatal("served recruiting surface omitted immutable offer construction")
	}
	ledger := surface.NewOfferLedger()
	if ledger == nil || len(ledger.EventKinds()) != 0 || len(ledger.HireIntents()) != 0 {
		t.Fatalf("served offer ledger = %+v, want an empty ledger", ledger)
	}
}

func TestTodo_RECRUIT_004_Served(t *testing.T) {
	surface := NewServedRecruitingSurface()
	if surface.ApplyTerminal == nil || surface.CorrectTerminal == nil || surface.RenderCandidacy == nil || surface.ReconcileExternal == nil {
		t.Fatal("served recruiting surface omitted ATS conformance operations")
	}
	rendered := surface.RenderCandidacy(recruiting.CandidacyRecord{}, recruiting.RenderPurpose("candidate"))
	if rendered.CandidacyID != "" {
		t.Fatalf("rendered empty candidacy = %+v, want no fabricated identity", rendered)
	}
}

func TestServedRecruitingAppSurface(t *testing.T) {
	var app App
	surface := (&app).Recruiting()
	if surface.NewAggregate == nil || surface.NewStageLedger == nil || surface.NewOfferLedger == nil {
		t.Fatal("composed app omitted recruiting capabilities")
	}
	if (&App{}).Recruiting().Version == nil {
		t.Fatal("app recruiting accessor did not expose the domain version")
	}
	var nilApp *App
	if nilApp.Recruiting().NewAggregate != nil {
		t.Fatal("nil app returned a live recruiting capability")
	}
}
