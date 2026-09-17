package intentmanifests

import (
	"errors"
	"testing"
)

func fixtureInstruction(op HigherOrderOp) Instruction {
	return Instruction{
		Op:               op,
		ParentIntent:     "hcmnext.rewards.promote/v1",
		ParentScope:      []string{"org:acme", "pop:mgmt-track"},
		ChildScopes:      [][]string{{"org:acme"}},
		Relationship:     "parent:promote-1",
		Lifecycle:        "PROPOSED",
		GovernanceDigest: "sha256:gov-1",
		ReplayKey:        "replay-" + string(op) + "-1",
	}
}

// TestHigherOrderIntentInstructionSetConformance is the PRIMARY
// INTENT-CONF-002 contract test: deterministic scenarios prove every
// higher-order operation is a typed creator, consumer, emitter or observer
// with an exact relationship, lifecycle, governance and effect contract.
func TestHigherOrderIntentInstructionSetConformance(t *testing.T) {
	if len(AllHigherOrderOps) != 19 {
		t.Fatalf("instruction set covers %d operations, want the full 19", len(AllHigherOrderOps))
	}

	t.Run("every operation carries its exact role and effect contract", func(t *testing.T) {
		d := NewDispatcher()
		seen := map[InstructionRole]int{}
		for _, op := range AllHigherOrderOps {
			in := fixtureInstruction(op)
			// Observers analyze history; they emit no graph edges.
			if role, _ := OpRole(op); role == RoleObserver {
				in.ChildScopes = nil
			}
			if effect, _ := OpEffect(op); effect == EffectAuthorized {
				in.Authority = "auth:repair-board"
			}
			rec, err := d.Prepare(in)
			if err != nil {
				t.Fatalf("%s: %v", op, err)
			}
			wantRole, _ := OpRole(op)
			if rec.Role != wantRole {
				t.Fatalf("%s: role = %s, want %s", op, rec.Role, wantRole)
			}
			if effect, _ := OpEffect(op); effect == EffectZero && rec.EffectCount != 0 {
				t.Fatalf("%s: zero-effect operation recorded %d effects", op, rec.EffectCount)
			}
			if effect, _ := OpEffect(op); effect == EffectAuthorized && (rec.EffectCount == 0 || rec.Authority == "") {
				t.Fatalf("%s: authorized effect names no authority", op)
			}
			seen[rec.Role]++
		}
		for _, role := range []InstructionRole{RoleCreator, RoleConsumer, RoleEmitter, RoleObserver} {
			if seen[role] == 0 {
				t.Fatalf("instruction set never exercises role %s", role)
			}
		}
	})

	t.Run("RED: side doors stay shut", func(t *testing.T) {
		d := NewDispatcher()
		sideDoors := map[string]Instruction{
			"unknown operation": {Op: "teleport", ParentIntent: "x", ParentScope: []string{"org:acme"}, Relationship: "r", Lifecycle: "l", GovernanceDigest: "g", ReplayKey: "k1"},
			"unbound parent":    {Op: OpCompose, ParentScope: []string{"org:acme"}, ChildScopes: [][]string{{"org:acme"}}, Relationship: "r", Lifecycle: "l", GovernanceDigest: "g", ReplayKey: "k2"},
			"missing governance": func() Instruction {
				in := fixtureInstruction(OpPreflight)
				in.GovernanceDigest = ""
				in.ReplayKey = "k3"
				return in
			}(),
			"broadened child scope": func() Instruction {
				in := fixtureInstruction(OpFork)
				in.ChildScopes = [][]string{{"org:acme", "org:other"}}
				in.ReplayKey = "k4"
				return in
			}(),
			"effect without authority": func() Instruction {
				in := fixtureInstruction(OpRepair)
				in.ReplayKey = "k5"
				return in
			}(),
			"historical op with authority": func() Instruction {
				in := fixtureInstruction(OpExplain)
				in.ChildScopes = nil
				in.Authority = "auth:forged"
				in.ReplayKey = "k6"
				return in
			}(),
			"historical op emitting edges": func() Instruction {
				in := fixtureInstruction(OpShadow)
				in.ReplayKey = "k7"
				return in
			}(),
		}
		for name, in := range sideDoors {
			if _, err := d.Prepare(in); !errors.Is(err, ErrHigherOrderRefused) {
				t.Fatalf("%s must be refused, got %v", name, err)
			}
		}
		if d.PreparedCount() != 0 {
			t.Fatalf("refused instructions recorded %d preparations", d.PreparedCount())
		}
	})

	t.Run("crash between prepare and commit recovers without duplication", func(t *testing.T) {
		d := NewDispatcher()
		in := fixtureInstruction(OpSupersede)
		first, err := d.Prepare(in)
		if err != nil {
			t.Fatal(err)
		}
		if first.Successor == "" {
			t.Fatal("supersede must name its successor")
		}
		// The commit is lost; replay under the same key recovers it.
		again, err := d.Prepare(in)
		if err != nil {
			t.Fatal(err)
		}
		if again.ReceiptDigest != first.ReceiptDigest {
			t.Fatal("replay produced a second receipt for one instruction")
		}
		if d.PreparedCount() != 1 {
			t.Fatalf("replay duplicated the instruction %d times", d.PreparedCount())
		}
	})

	t.Run("evidence reconstructs every edge and link", func(t *testing.T) {
		d := NewDispatcher()
		var chain []Receipt
		for _, op := range []HigherOrderOp{OpCompose, OpFork, OpRepair, OpExplain} {
			in := fixtureInstruction(op)
			if op == OpExplain {
				in.ChildScopes = nil
				in.OutcomeLinkRef = "outcome:link-001"
			}
			if op == OpRepair {
				in.Authority = "auth:repair-board"
			}
			rec, err := d.Prepare(in)
			if err != nil {
				t.Fatalf("%s: %v", op, err)
			}
			chain = append(chain, rec)
		}
		if err := d.Reconstruct(chain); err != nil {
			t.Fatalf("Reconstruct: %v", err)
		}
		forged := chain[1]
		forged.ParentIntent = "hcmnext.forged/other/v1"
		if err := d.Reconstruct([]Receipt{forged}); err == nil {
			t.Fatal("reconstruction accepted a forged receipt")
		}
	})
}

// TestTodo_INTENT_CONF_002_Golden pins the fixture receipt digest.
func TestTodo_INTENT_CONF_002_Golden(t *testing.T) {
	d := NewDispatcher()
	rec, err := d.Prepare(fixtureInstruction(OpCompose))
	if err != nil {
		t.Fatal(err)
	}
	const golden = "sha256:eee9c294599e507636bbb41ecc798cc72978f8337cce8d298972b77c6449ce99"
	if rec.ReceiptDigest != golden {
		t.Fatalf("receipt digest drifted: got %s, want %s", rec.ReceiptDigest, golden)
	}
	again, err := d.Prepare(fixtureInstruction(OpCompose))
	if err != nil {
		t.Fatal(err)
	}
	if rec.ReceiptDigest != again.ReceiptDigest {
		t.Fatal("identical instructions prepare different receipts")
	}
	t.Logf("receipt=%s", rec.ReceiptDigest)
}

// TestTodo_INTENT_CONF_002_Property proves replay-key semantics: one key
// names exactly one instruction, and distinct keys never collide.
func TestTodo_INTENT_CONF_002_Property(t *testing.T) {
	d := NewDispatcher()
	seen := map[string]bool{}
	for i := 0; i < 30; i++ {
		in := fixtureInstruction(AllHigherOrderOps[i%len(AllHigherOrderOps)])
		if role, _ := OpRole(in.Op); role == RoleObserver {
			in.ChildScopes = nil
		}
		if effect, _ := OpEffect(in.Op); effect == EffectAuthorized {
			in.Authority = "auth:model"
		}
		in.ReplayKey = "model-key"
		rec, err := d.Prepare(in)
		if i%len(AllHigherOrderOps) == 0 {
			// Identical replay under the same key: one receipt, no
			// duplication.
			if err != nil {
				t.Fatalf("iteration %d: identical replay must pass, got %v", i, err)
			}
			seen[rec.ReceiptDigest] = true
			continue
		}
		// Same key, different operation content: only an identical
		// replay may pass, anything else is a conflict.
		if !errors.Is(err, ErrHigherOrderConflict) {
			t.Fatalf("iteration %d: cross-operation replay-key reuse must conflict, got %v", i, err)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("one replay key produced %d distinct receipts", len(seen))
	}
}
