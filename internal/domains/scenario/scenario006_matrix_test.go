package scenario

import (
	"strings"
	"testing"
)

// TestTodo_SCENARIO_006_Property: compilation is deterministic, ordered
// by key, closed over its dependencies and bound to plan and governance.
func TestTodo_SCENARIO_006_Property(t *testing.T) {
	plan := planWithTwoDeltas(t)
	spec := spec006()

	first := mustCompile006(t, plan, spec)
	second := mustCompile006(t, plan, spec)
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("identical compilation inputs replayed to different digests")
	}
	if first.RevisionDigest != plan.CanonicalDigest {
		t.Fatal("compilation did not preserve the plan digest")
	}

	// Order follows keys, not the plan's assumption order.
	keys := make(map[string]struct{}, len(first.Intents))
	previous := ""
	for _, intent := range first.Intents {
		if _, dup := keys[intent.Key]; dup {
			t.Fatalf("duplicate intent key: %q", intent.Key)
		}
		keys[intent.Key] = struct{}{}
		if intent.Key < previous {
			t.Fatalf("intents are not ordered by key: %v", first.Intents)
		}
		previous = intent.Key
		if !strings.HasPrefix(intent.SimulationLink, spec.SimulationRef+"#") {
			t.Fatalf("simulation link = %q", intent.SimulationLink)
		}
	}

	// Dependencies close over the compiled key set: every dependency is
	// itself a compiled intent, and nothing depends on itself.
	for _, intent := range first.Intents {
		for _, dep := range intent.DependsOn {
			if dep == intent.Key {
				t.Fatalf("intent %q depends on itself", intent.Key)
			}
			if _, ok := keys[dep]; !ok {
				t.Fatalf("intent %q depends on uncompiled %q", intent.Key, dep)
			}
		}
		for _, target := range intent.WriteSet {
			if strings.TrimSpace(target) == "" {
				t.Fatalf("intent %q has a blank write target", intent.Key)
			}
		}
	}

	// Governance is part of the binding: a version bump moves the digest.
	moved := spec006()
	moved.GovernanceVersion = "v2"
	regov := mustCompile006(t, plan, moved)
	if regov.CanonicalDigest == first.CanonicalDigest {
		t.Fatal("governance change did not move the compilation digest")
	}
	if err := regov.Validate(); err != nil {
		t.Fatal(err)
	}

	// The compilation is bounded: the plan fits MaxIntents and the digest
	// is stable across calls.
	bounded := spec006()
	bounded.MaxIntents = len(plan.Assumptions)
	fit := mustCompile006(t, plan, bounded)
	if len(fit.Intents) != len(plan.Assumptions) {
		t.Fatalf("bounded intents = %d", len(fit.Intents))
	}
	again, err := fit.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if again != fit.CanonicalDigest {
		t.Fatal("compilation digest is not stable")
	}
}
