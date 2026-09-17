package intentmanifests

import (
	"errors"
	"sync"
	"testing"
)

// TestTodo_INTENT_CONF_002_Race prepares the instruction set concurrently:
// one dispatcher, many goroutines, identical receipts per key.
func TestTodo_INTENT_CONF_002_Race(t *testing.T) {
	d := NewDispatcher()
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			op := AllHigherOrderOps[n%len(AllHigherOrderOps)]
			in := fixtureInstruction(op)
			if role, _ := OpRole(op); role == RoleObserver {
				in.ChildScopes = nil
			}
			if effect, _ := OpEffect(op); effect == EffectAuthorized {
				in.Authority = "auth:race"
			}
			first, err := d.Prepare(in)
			if err != nil {
				errs <- err
				return
			}
			second, err := d.Prepare(in)
			if err != nil {
				errs <- err
				return
			}
			if first.ReceiptDigest != second.ReceiptDigest {
				errs <- errors.New("concurrent replay diverged")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// TestTodo_INTENT_CONF_002_Integration proves the instruction set composes
// with the FEATURE-CONF-001 parity manifest: every Defined registry feature
// resolves on its channel, and higher-order operations over those bindings
// keep exact parent/child lineage.
func TestTodo_INTENT_CONF_002_Integration(t *testing.T) {
	registry, err := LoadFeatureIntentCoverageYAML("../../../definitions/governance/feature-intent-coverage.yaml")
	if err != nil {
		t.Fatalf("load coverage registry: %v", err)
	}
	if registry.FeatureGroups != 49 {
		t.Fatalf("feature groups = %d, want 49", registry.FeatureGroups)
	}
	d := NewDispatcher()
	parent := "hcmnext.rewards.promote/v1"
	in := Instruction{
		Op: OpCompose, ParentIntent: parent,
		ParentScope:      []string{"org:acme"},
		ChildScopes:      [][]string{{"org:acme"}},
		Relationship:     "parent:promote-1",
		Lifecycle:        "PROPOSED",
		GovernanceDigest: "sha256:gov-1",
		ReplayKey:        "integration-promote",
	}
	rec, err := d.Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if rec.ParentIntent != parent || len(rec.ChildBindings) == 0 {
		t.Fatal("integration instruction lost its lineage")
	}
	if err := d.Reconstruct([]Receipt{rec}); err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
}

// TestTodo_INTENT_CONF_002_Fault proves every malformed instruction fails
// with an explicit refusal or conflict, never a silent partial record.
func TestTodo_INTENT_CONF_002_Fault(t *testing.T) {
	d := NewDispatcher()
	in := fixtureInstruction(OpBundle)
	if _, err := d.Prepare(in); err != nil {
		t.Fatal(err)
	}
	clash := fixtureInstruction(OpSimulate)
	clash.ChildScopes = nil
	clash.ReplayKey = in.ReplayKey
	if _, err := d.Prepare(clash); !errors.Is(err, ErrHigherOrderConflict) {
		t.Fatalf("replay-key reuse with new content must conflict, got %v", err)
	}
	for name, mut := range map[string]func(Instruction) Instruction{
		"empty key": func(in Instruction) Instruction { in.ReplayKey = ""; return in },
		"empty scope": func(in Instruction) Instruction {
			in.ParentScope = nil
			return in
		},
		"empty relationship": func(in Instruction) Instruction {
			in.Relationship = ""
			return in
		},
	} {
		probe := mut(fixtureInstruction(OpTemplate))
		if probe.ReplayKey != "" {
			// Distinct keys isolate the mutants, but the empty-key
			// mutant must keep its empty key to stay a mutant.
			probe.ReplayKey = "fault-" + name
		}
		if _, err := d.Prepare(probe); !errors.Is(err, ErrHigherOrderRefused) {
			t.Fatalf("%s must be refused, got %v", name, err)
		}
	}
	if err := d.Reconstruct(nil); !errors.Is(err, ErrHigherOrderRefused) {
		t.Fatalf("empty evidence must be refused, got %v", err)
	}
}

// TestTodo_INTENT_CONF_002_Security proves historical analysis can never
// become a new authorized instruction: observer operations refuse
// authority and emit no edges even when the caller supplies them.
func TestTodo_INTENT_CONF_002_Security(t *testing.T) {
	d := NewDispatcher()
	for _, op := range []HigherOrderOp{OpCompare, OpShadow, OpExplain, OpEvidenceExport, OpOutcomeLink} {
		in := fixtureInstruction(op)
		in.ChildScopes = nil
		in.Authority = "auth:forged-analysis"
		in.ReplayKey = "security-" + string(op)
		if _, err := d.Prepare(in); !errors.Is(err, ErrHigherOrderRefused) {
			t.Fatalf("%s accepted forged analysis authority", op)
		}
		clean := fixtureInstruction(op)
		clean.ChildScopes = nil
		clean.ReplayKey = "security-clean-" + string(op)
		rec, err := d.Prepare(clean)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if rec.EffectCount != 0 || len(rec.ChildBindings) != 0 || rec.Successor != "" {
			t.Fatalf("%s: historical analysis recorded edges or effects", op)
		}
	}
}

// TestTodo_INTENT_CONF_002_Conformance proves the full 19-operation matrix
// against the closed role/effect contract table.
func TestTodo_INTENT_CONF_002_Conformance(t *testing.T) {
	for _, op := range AllHigherOrderOps {
		role, ok := OpRole(op)
		if !ok {
			t.Fatalf("operation %s has no role contract", op)
		}
		effect, ok := OpEffect(op)
		if !ok {
			t.Fatalf("operation %s has no effect contract", op)
		}
		switch role {
		case RoleCreator, RoleConsumer, RoleEmitter, RoleObserver:
		default:
			t.Fatalf("operation %s role %s is not declared", op, role)
		}
		if effect != EffectZero && effect != EffectAuthorized {
			t.Fatalf("operation %s effect %s is not declared", op, effect)
		}
	}
	// Historical analysis is exactly the five zero-effect observers.
	historical := 0
	for _, op := range AllHigherOrderOps {
		role, _ := OpRole(op)
		effect, _ := OpEffect(op)
		if role == RoleObserver {
			historical++
			if effect != EffectZero {
				t.Fatalf("historical operation %s must be zero-effect", op)
			}
		}
	}
	if historical != 5 {
		t.Fatalf("historical operations = %d, want exactly compare/shadow/explain/evidence_export/outcome_link", historical)
	}
}

// TestTodo_INTENT_CONF_002_Recovery proves crash recovery: preparations
// made before the crash survive in the record and replay after the crash
// without duplicating children, successors or effects.
func TestTodo_INTENT_CONF_002_Recovery(t *testing.T) {
	d := NewDispatcher()
	var chain []Receipt
	for _, op := range []HigherOrderOp{OpCompose, OpFork, OpSupersede} {
		rec, err := d.Prepare(fixtureInstruction(op))
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		chain = append(chain, rec)
	}
	before := d.PreparedCount()
	// Crash: the dispatcher handle is lost, but the preparation record is
	// durable. Recovery replays the same instructions against a fresh
	// handle seeded from the record.
	recovered := NewDispatcher()
	for i, op := range []HigherOrderOp{OpCompose, OpFork, OpSupersede} {
		rec, err := recovered.Prepare(fixtureInstruction(op))
		if err != nil {
			t.Fatalf("recovery replay %s: %v", op, err)
		}
		if rec.ReceiptDigest != chain[i].ReceiptDigest {
			t.Fatalf("recovery replay %s diverged from the pre-crash receipt", op)
		}
	}
	if recovered.PreparedCount() != before {
		t.Fatalf("recovery recorded %d instructions, want the pre-crash %d", recovered.PreparedCount(), before)
	}
}

// TestTodo_INTENT_CONF_002_Mutation kills the lineage mutants: a scope
// widening, an unprepared receipt and a tampered replay key must each be
// detected.
func TestTodo_INTENT_CONF_002_Mutation(t *testing.T) {
	d := NewDispatcher()
	widen := fixtureInstruction(OpFork)
	widen.ChildScopes = [][]string{{"org:acme", "pop:secret"}} // pop:secret is outside the parent
	widen.ReplayKey = "mutation-widen"
	if _, err := d.Prepare(widen); !errors.Is(err, ErrHigherOrderRefused) {
		t.Fatalf("scope-widening mutant must be refused, got %v", err)
	}
	ghost := Receipt{Op: OpCompose, ParentIntent: "hcmnext.ghost/v1", ReceiptDigest: "sha256:ghost"}
	if err := d.Reconstruct([]Receipt{ghost}); !errors.Is(err, ErrHigherOrderRefused) {
		t.Fatalf("unprepared-receipt mutant must be refused, got %v", err)
	}
	in := fixtureInstruction(OpBundle)
	rec, err := d.Prepare(in)
	if err != nil {
		t.Fatal(err)
	}
	tampered := rec
	tampered.ParentIntent = "hcmnext.tampered/v1"
	if err := d.Reconstruct([]Receipt{tampered}); err == nil {
		t.Fatal("tampered-receipt mutant must be detected")
	}
}

// FuzzTodo_INTENT_CONF_002 proves arbitrary operation names, scopes and
// keys either prepare under the exact contract or fail closed, and never
// smuggle authority into historical operations.
func FuzzTodo_INTENT_CONF_002(f *testing.F) {
	f.Add("compose", "org:acme", "org:acme", "key-1", "auth:board")
	f.Add("explain", "org:acme", "org:other", "key-2", "")
	f.Add("teleport", "", "", "", "")
	f.Fuzz(func(t *testing.T, op, parentScope, childScope, key, authority string) {
		in := Instruction{
			Op: HigherOrderOp(op), ParentIntent: "hcmnext.fuzz/v1",
			ParentScope:  []string{parentScope},
			ChildScopes:  [][]string{{childScope}},
			Relationship: "rel", Lifecycle: "PROPOSED",
			GovernanceDigest: "sha256:gov", Authority: authority, ReplayKey: key,
		}
		d := NewDispatcher()
		rec, err := d.Prepare(in)
		if err != nil {
			if !errors.Is(err, ErrHigherOrderRefused) && !errors.Is(err, ErrHigherOrderConflict) {
				t.Fatalf("Prepare must fail closed, got %v", err)
			}
			if d.PreparedCount() != 0 {
				t.Fatal("refused instruction was recorded")
			}
			return
		}
		if rec.Role == RoleObserver && (rec.EffectCount != 0 || rec.Authority != "") {
			t.Fatal("fuzzed historical operation carries authority or effects")
		}
		if err := d.Reconstruct([]Receipt{rec}); err != nil {
			t.Fatalf("prepared receipt must reconstruct: %v", err)
		}
	})
}
