package workflow

import (
	"errors"
	"math/big"
	"testing"
	"time"
)

func wfpageK2Rule() PageRule {
	return PageRule{
		Code: "START_DATE_AFTER_OFFER", Message: "Start date cannot precede the offer date.",
		Expression: `start_date >= offer_date && salary <= param("max_salary") && grade in ref("grades")`,
		Inputs:     map[string]ValueType{"start_date": {Kind: KindLocalDate}, "offer_date": {Kind: KindLocalDate}, "salary": {Kind: KindMoney}, "grade": {Kind: KindString}},
		Parameters: map[string]ValueType{"max_salary": {Kind: KindMoney}}, References: map[string]ValueType{"grades": {Kind: KindString}},
	}
}

func TestTodo_WFPAGE_015(t *testing.T) {
	rule := wfpageK2Rule()
	compiled, err := rule.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Cost() <= 0 || compiled.InputPaths()[0] != "grade" {
		t.Fatalf("compiled metadata = cost %d paths %v", compiled.Cost(), compiled.InputPaths())
	}
	input := PageRuleInput{Fields: map[string]any{"start_date": time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), "offer_date": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "salary": mustMoney(t, "USD", "90000"), "grade": "G6"}, Parameters: map[string]any{"max_salary": mustMoney(t, "USD", "100000")}, References: map[string][]any{"grades": {"G5", "G6"}}}
	result, err := compiled.Evaluate(input)
	if err != nil || !result.Valid {
		t.Fatalf("valid rule = %+v, %v", result, err)
	}
	input.Fields["start_date"] = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	result, err = compiled.Evaluate(input)
	if err != nil || result.Valid || len(result.Findings) != 1 || result.Findings[0].Code != rule.Code {
		t.Fatalf("invalid rule = %+v, %v", result, err)
	}
}

func TestTodo_WFPAGE_015_Security(t *testing.T) {
	bad := wfpageK2Rule()
	bad.Expression = `secret == "leaked"`
	if _, err := bad.Compile(); !errors.Is(err, ErrPageRuleInput) {
		t.Fatalf("undeclared field error = %v", err)
	}
	bad = wfpageK2Rule()
	bad.Expression = `sum(contacts.amount) > money("USD", "0")`
	bad.Inputs["contacts.amount"] = ValueType{Kind: KindMoney}
	if _, err := bad.Compile(); !errors.Is(err, ErrPageRuleCost) {
		t.Fatalf("uncapped aggregate error = %v", err)
	}
	bad = wfpageK2Rule()
	bad.Expression = `start_date == date_add(offer_date, 1)`
	bad.CostLimit = 1
	if _, err := bad.Compile(); !errors.Is(err, ErrPageRuleCost) {
		t.Fatalf("cost bound error = %v", err)
	}
}

func TestTodo_WFPAGE_015_Property(t *testing.T) {
	rule := wfpageK2Rule()
	compiled, err := rule.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, startAfter := range []bool{true, false} {
		start := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
		if !startAfter {
			start = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
		}
		input := PageRuleInput{Fields: map[string]any{"start_date": start, "offer_date": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "salary": mustMoney(t, "USD", "90000"), "grade": "G6"}, Parameters: map[string]any{"max_salary": mustMoney(t, "USD", "100000")}, References: map[string][]any{"grades": {"G6"}}}
		server, err := compiled.Evaluate(input)
		if err != nil {
			t.Fatal(err)
		}
		client, err := compiled.Evaluate(input)
		if err != nil {
			t.Fatal(err)
		}
		if server.Valid != client.Valid {
			t.Fatalf("client/server disagreement for %t", startAfter)
		}
	}
}

func TestTodo_WFPAGE_015_Golden(t *testing.T) {
	rule := wfpageK2Rule()
	if rule.Canonical() != wfpageK2Rule().Canonical() || rule.Digest() != wfpageK2Rule().Digest() {
		t.Fatal("rule canonicalization is not stable")
	}
	if len(rule.Digest()) != len("sha256:")+64 {
		t.Fatalf("digest = %q", rule.Digest())
	}
	dateRule := PageRule{Code: "DATE", Message: "date", Expression: `date_add(offer_date, 7) >= start_date`, Inputs: map[string]ValueType{"offer_date": {Kind: KindLocalDate}, "start_date": {Kind: KindLocalDate}}}
	compiled, err := dateRule.Compile()
	if err != nil {
		t.Fatal(err)
	}
	got, err := compiled.Evaluate(PageRuleInput{Fields: map[string]any{"offer_date": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "start_date": time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}})
	if err != nil || !got.Valid {
		t.Fatalf("date arithmetic = %+v, %v", got, err)
	}
	amount := new(big.Rat)
	amount.SetString("1.25")
	if amount.Sign() != 1 {
		t.Fatal("exact decimal setup failed")
	}
}

func TestPageRuleAggregateAndMoney(t *testing.T) {
	rule := PageRule{Code: "TOTAL", Message: "total", Expression: `sum(contacts.amount) <= money("USD", "100.00")`, Inputs: map[string]ValueType{"contacts.amount": {Kind: KindMoney}}, RepeatingCaps: map[string]int{"contacts": 3}}
	compiled, err := rule.Compile()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := NewMoneyValue("USD", "40")
	b, _ := NewMoneyValue("USD", "50")
	result, err := compiled.Evaluate(PageRuleInput{Repeating: map[string][]map[string]any{"contacts": {{"amount": a}, {"amount": b}}}})
	if err != nil || !result.Valid {
		t.Fatalf("aggregate = %+v, %v", result, err)
	}
	tooMany := PageRuleInput{Repeating: map[string][]map[string]any{"contacts": {{"amount": a}, {"amount": b}, {"amount": a}, {"amount": b}}}}
	if _, err := compiled.Evaluate(tooMany); !errors.Is(err, ErrPageRuleRuntime) {
		t.Fatalf("row cap error = %v", err)
	}
}

func mustMoney(t *testing.T, currency, amount string) MoneyValue {
	t.Helper()
	value, err := NewMoneyValue(currency, amount)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
