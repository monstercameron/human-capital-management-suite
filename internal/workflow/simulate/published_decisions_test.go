package simulate

import (
	"context"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/rulepayload"
)

func TestTodo_WF_EXT_006(t *testing.T) {
	store := rulepayload.New()
	table := decisionFixtureTable()
	ref := workflow.Reference{Kind: workflow.RefRule, ID: table.ID, Version: table.Version}
	published, err := store.Publish(rulepayload.Payload{Ref: ref, Kind: rulepayload.KindDecisionTable, Table: &table})
	if err != nil {
		t.Fatal(err)
	}
	decision := compiledFixtureDecision(ref, published)
	evaluator := PublishedRuleDecisions{Payloads: store}
	got, err := evaluator.Decide(context.Background(), DecisionRequest{NodeID: "route", Decision: decision, Inputs: Bag{"amount": NewDecimal(simDecimal(t, "7"))}})
	if err != nil {
		t.Fatal(err)
	}
	if got.RouteKey != "ALLOW" || got.TraceRef != "fixture.threshold@table-v1#allow" {
		t.Fatalf("decision = %+v", got)
	}

	decision.Rule.Digest = "sha256:wrong"
	if _, err := evaluator.Decide(context.Background(), DecisionRequest{NodeID: "route", Decision: decision, Inputs: Bag{"amount": NewDecimal(simDecimal(t, "7"))}}); err == nil {
		t.Fatal("wrong content digest was accepted")
	}
}

func TestTodo_WF_EXT_006_Property(t *testing.T) {
	store := rulepayload.New()
	table := decisionFixtureTable()
	ref := workflow.Reference{Kind: workflow.RefRule, ID: table.ID, Version: table.Version}
	published, err := store.Publish(rulepayload.Payload{Ref: ref, Kind: rulepayload.KindDecisionTable, Table: &table})
	if err != nil {
		t.Fatal(err)
	}
	evaluator := PublishedRuleDecisions{Payloads: store}
	decision := compiledFixtureDecision(ref, published)
	for amount := int64(-2); amount <= 12; amount++ {
		input := Bag{"amount": NewDecimal(simDecimal(t, fmt.Sprint(amount)))}
		first, err := evaluator.Decide(context.Background(), DecisionRequest{NodeID: "route", Decision: decision, Inputs: input})
		if err != nil {
			t.Fatal(err)
		}
		second, err := evaluator.Decide(context.Background(), DecisionRequest{NodeID: "route", Decision: decision, Inputs: input})
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Fatalf("input %d: evaluation changed: %+v != %+v", amount, first, second)
		}
		want := "DENY"
		if amount >= 7 {
			want = "ALLOW"
		}
		if first.RouteKey != want {
			t.Fatalf("input %d route = %q, want %q", amount, first.RouteKey, want)
		}
	}
}

func TestTodo_WF_EXT_006_Golden(t *testing.T) {
	store := rulepayload.New()
	table := decisionFixtureTable()
	ref := workflow.Reference{Kind: workflow.RefRule, ID: table.ID, Version: table.Version}
	published, err := store.Publish(rulepayload.Payload{Ref: ref, Kind: rulepayload.KindDecisionTable, Table: &table})
	if err != nil {
		t.Fatal(err)
	}
	got, err := (PublishedRuleDecisions{Payloads: store}).Decide(context.Background(), DecisionRequest{
		NodeID: "route", Decision: compiledFixtureDecision(ref, published), Inputs: Bag{"amount": NewDecimal(simDecimal(t, "7"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantTrace = "fixture.threshold@table-v1#allow"
	if got.RouteKey != "ALLOW" || got.TraceRef != wantTrace || got.Detail != "published table "+wantTrace {
		t.Fatalf("golden decision = %+v", got)
	}
}

func decisionFixtureTable() rules.Table {
	return rules.Table{
		ID: "fixture.threshold", Version: "table-v1",
		Inputs:    []rules.Column{{Name: "amount", Kind: rules.KindDecimal}},
		Outputs:   []rules.Column{{Name: "route_key", Kind: rules.KindString}},
		HitPolicy: rules.HitPolicyFirst,
		Rows: []rules.Row{
			{ID: "allow", Conditions: []rules.Condition{rules.GreaterThan(rules.DecimalValue(values.MustDecimal("6", 0, values.RoundingExactRequired)))}, Outputs: []rules.Value{rules.StringValue("ALLOW")}},
			{ID: "deny", Conditions: []rules.Condition{rules.Any()}, Outputs: []rules.Value{rules.StringValue("DENY")}},
		},
	}
}

func compiledFixtureDecision(ref workflow.Reference, pin workflow.ResolvedReference) workflow.CompiledDecision {
	return workflow.CompiledDecision{
		RuleRef: ref.ID, Rule: &pin,
		Routes: []workflow.DecisionRoute{{Key: "ALLOW", Predicate: "amount_over_limit"}, {Key: "DENY", Predicate: "amount_under_limit"}},
	}
}

func simDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
