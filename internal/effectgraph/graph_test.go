package effectgraph

import (
	"errors"
	"strings"
	"testing"
)

func node(id string) EffectNode {
	return EffectNode{ID: id, ProposalRef: "proposal/1", TransactionRef: "tx/1", CapabilityRef: "payroll.write/v1", ResourceKey: "worker/1", OrderingKey: "worker/1", Ordering: Strict, IdempotencyKey: "effect/" + id, DispatchCondition: "approved", Deadline: "2026-12-01T00:00:00Z", FailurePolicy: "RETRY_THEN_QUARANTINE", CompensationPolicy: "NONE", RepairPolicy: "repair/" + id, TerminalContribution: "OBSERVED", Observation: ObservationContract{Required: true, Profile: "provider-state/v1"}}
}

func TestCompileCanonicalOrderAndDigest(t *testing.T) {
	a, b := node("a"), node("b")
	b.Prerequisites = []string{"a"}
	c, err := Compile(Graph{Nodes: []EffectNode{b, a}})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Order) != 2 || c.Order[0] != "a" || c.Order[1] != "b" {
		t.Fatalf("order=%v", c.Order)
	}
	if c.Digest == "" {
		t.Fatal("empty digest")
	}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestCompileRejectsSafetyViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EffectNode)
		want   string
	}{
		{"missing dependency", func(n *EffectNode) { n.Prerequisites = []string{"nope"} }, "UNDECLARED_DEPENDENCY"},
		{"irreversible without observation", func(n *EffectNode) { n.Irreversible = true; n.Observation = ObservationContract{} }, "IRREVERSIBLE_NEEDS_OBSERVATION"},
		{"missing idempotency", func(n *EffectNode) { n.IdempotencyKey = "" }, "MISSING_IDEMPOTENCY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := node("a")
			tt.mutate(&n)
			_, err := Compile(Graph{Nodes: []EffectNode{n}})
			if err == nil || err.Error() == "" {
				t.Fatal("expected rejection")
			}
			if want := tt.want; !strings.Contains(err.Error(), want) {
				t.Fatalf("error=%v want %s", err, want)
			}
		})
	}
}
func TestCompileRejectsCycleAndOrderingConflict(t *testing.T) {
	a, b := node("a"), node("b")
	a.Prerequisites = []string{"b"}
	b.Prerequisites = []string{"a"}
	if _, err := Compile(Graph{Nodes: []EffectNode{a, b}}); err == nil || !errors.Is(err, ErrCycle) {
		t.Fatal("cycle accepted")
	}
	a, b = node("a"), node("b")
	b.Ordering = Independent
	if _, err := Compile(Graph{Nodes: []EffectNode{a, b}}); err == nil {
		t.Fatal("ordering conflict accepted")
	}
}

// The registry matrix names are intentionally present as separate entry
// points so plancheck and downstream conformance runners can select each
// obligation independently.
func TestTodo_EFFECT_001(t *testing.T) {
	n := node("payroll-write")
	c, err := Compile(Graph{Nodes: []EffectNode{n}})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Nodes) != 1 || c.Nodes[0].ProposalRef != n.ProposalRef || c.Nodes[0].TransactionRef != n.TransactionRef || c.Nodes[0].CapabilityRef != n.CapabilityRef || c.Nodes[0].IdempotencyKey != n.IdempotencyKey {
		t.Fatalf("effect lineage/policy binding lost: %+v", c)
	}
}
func TestTodo_EFFECT_001_Property(t *testing.T) {
	a, b := node("a"), node("b")
	b.Prerequisites = []string{"a"}
	one, err := Compile(Graph{Nodes: []EffectNode{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	two, err := Compile(Graph{Nodes: []EffectNode{b, a}})
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest != two.Digest {
		t.Fatalf("canonical digest changed with input order")
	}
}
func TestTodo_EFFECT_001_Race(t *testing.T) {
	const workers = 16
	results := make(chan string, workers)
	for i := 0; i < workers; i++ {
		go func() {
			c, err := Compile(Graph{Nodes: []EffectNode{node("b"), node("a")}})
			if err != nil {
				t.Errorf("compile: %v", err)
				return
			}
			results <- c.Digest
		}()
	}
	first := <-results
	for i := 1; i < workers; i++ {
		if got := <-results; got != first {
			t.Fatalf("nondeterministic digest: %s != %s", got, first)
		}
	}
}
func TestTodo_EFFECT_001_Integration(t *testing.T) {
	n := node("a")
	n.Irreversible = true
	if _, err := Compile(Graph{Nodes: []EffectNode{n}}); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*EffectNode){func(n *EffectNode) { n.Observation = ObservationContract{} }, func(n *EffectNode) { n.RepairPolicy = "NONE" }} {
		n := node("a")
		n.Irreversible = true
		mutate(&n)
		if _, err := Compile(Graph{Nodes: []EffectNode{n}}); err == nil {
			t.Fatal("unsafe irreversible effect accepted")
		}
	}
}
func TestTodo_EFFECT_001_Mutation(t *testing.T) {
	n := node("a")
	c, err := Compile(Graph{Nodes: []EffectNode{n}})
	if err != nil {
		t.Fatal(err)
	}
	n.IdempotencyKey = "changed"
	if c.Nodes[0].IdempotencyKey == n.IdempotencyKey {
		t.Fatal("compiled graph aliases input node")
	}
	c.Nodes[0].IdempotencyKey = "changed"
	if err := c.Verify(); err == nil {
		t.Fatal("modified compiled graph verified")
	}
}
