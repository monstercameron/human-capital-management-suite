package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentuxDemoSeedSink struct {
	rows  map[string]agentdemo.Email
	fail  int
	calls int
}

func (s *agentuxDemoSeedSink) SimulateCustomerEmail(ctx context.Context, email agentdemo.Email) (agentdemo.Receipt, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p.Subject() != localAgentDemoAdmin {
		return agentdemo.Receipt{}, agentdemo.ErrDenied
	}
	s.calls++
	if s.fail == s.calls {
		return agentdemo.Receipt{}, agentdemo.ErrUnavailable
	}
	prior, exists := s.rows[email.IdempotencyKey]
	if exists && prior != email {
		return agentdemo.Receipt{}, agentdemo.ErrConflict
	}
	s.rows[email.IdempotencyKey] = email
	return agentdemo.Receipt{MessageID: email.IdempotencyKey, State: "QUEUED", Replayed: exists}, nil
}

func TestAgentUXDemo_SeedInputs(t *testing.T) {
	emails := AgentUXDemoSupportEmails()
	if len(emails) != 5 {
		t.Fatal("missing demo email")
	}
	seen := map[string]bool{}
	for _, email := range emails {
		if _, err := agentuxDemoValidateEmail(email); err != nil || seen[email.IdempotencyKey] {
			t.Fatalf("invalid demo: %+v %v", email, err)
		}
		seen[email.IdempotencyKey] = true
	}
	emails[0].Subject = "tampered"
	if AgentUXDemoSupportEmails()[0].Subject == "tampered" {
		t.Fatal("fixture aliases")
	}
	p, err := localAgentDemoPrincipal(time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC), localAgentDemoTenant, localAgentDemoAdmin, "org:ironridge-demo:people")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), p)
	sink := &agentuxDemoSeedSink{rows: map[string]agentdemo.Email{}}
	for attempt := range 2 {
		receipts, err := AgentUXDemoSeedSupportEmails(ctx, sink)
		if err != nil || len(receipts) != 5 || len(sink.rows) != 5 {
			t.Fatalf("seed: %+v %v", receipts, err)
		}
		for _, receipt := range receipts {
			if receipt.State != "QUEUED" || receipt.Replayed != (attempt == 1) {
				t.Fatal("wrong preparation receipt")
			}
		}
	}
	if _, err := AgentUXDemoSeedSupportEmails(context.Background(), sink); !errors.Is(err, agentdemo.ErrDenied) {
		t.Fatal("unbound seeding")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		workflow := AgentUXDemoSupportWorkflow(locale)
		if errs := projectworkflow.Validate(workflow); len(errs) != 0 {
			t.Fatalf("%s board invalid: %v", locale, errs)
		}
		if len(workflow.Columns) != 4 || workflow.TaskTypes[0].InitialStatus != "support_new" {
			t.Fatal("support workflow missing columns")
		}
		names := []string{}
		for _, column := range workflow.Columns {
			names = append(names, column.Name)
		}
		if locale == "en-US" && !reflect.DeepEqual(names, []string{"New", "In progress", "Waiting on customer", "Done"}) {
			t.Fatalf("column contract: %v", names)
		}
		if locale != "en-US" && names[0] == "New" {
			t.Fatal("unlocalized columns")
		}
		if err := projectworkflow.ValidateTaskCreation(workflow, 1, 1, "task_default", "support_new", nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAgentUXDemo_SeedInputs_Fault(t *testing.T) {
	p, err := localAgentDemoPrincipal(time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC), localAgentDemoTenant, localAgentDemoAdmin, "org:ironridge-demo:people")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), p)
	sink := &agentuxDemoSeedSink{rows: map[string]agentdemo.Email{}, fail: 3}
	receipts, err := AgentUXDemoSeedSupportEmails(ctx, sink)
	if !errors.Is(err, agentdemo.ErrUnavailable) || len(receipts) != 2 {
		t.Fatal("partial receipt lost")
	}
	sink.fail = 0
	receipts, err = AgentUXDemoSeedSupportEmails(ctx, sink)
	if err != nil || len(sink.rows) != 5 || !receipts[0].Replayed || !receipts[1].Replayed || receipts[2].Replayed {
		t.Fatal("seed recovery duplicated or lost email")
	}
}
