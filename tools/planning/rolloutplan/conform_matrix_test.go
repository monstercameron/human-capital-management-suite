package rolloutplan

import "testing"

func conformHealthy(kind ConformArtifactKind, hook string) ConformSnapshot {
	return ConformSnapshot{
		Kind: kind, Target: "pilot-cell", Version: "v1.4.0",
		Epoch: 7, Healthy: true, Paused: false,
		RollbackVersion: "v1.3.2", Explain: "routine update",
		ActivationHook: hook,
	}
}

// TestTodo_ROLLOUT_008_Property: the core invariants are identical across
// implementations — same core fields, same verdict — while activation
// hooks stay kind-namespaced.
func TestTodo_ROLLOUT_008_Property(t *testing.T) {
	hooks := map[ConformArtifactKind]string{
		ConformService:       "service:restart-cell",
		ConformConfiguration: "config:apply-flags",
		ConformSemantic:      "semantic:republish-pack",
	}
	for kind, hook := range hooks {
		if err := CheckConformance(conformHealthy(kind, hook)); err != nil {
			t.Fatalf("%s healthy: %v", kind, err)
		}
		// A foreign hook namespace never activates here.
		foreign := conformHealthy(kind, "service:restart-cell")
		if kind != ConformService {
			if err := CheckConformance(foreign); err == nil {
				t.Fatalf("%s accepted a service hook", kind)
			}
		}
	}
	// Every core invariant binds every kind identically.
	mutate := map[string]func(*ConformSnapshot){
		"target":           func(s *ConformSnapshot) { s.Target = "" },
		"version":          func(s *ConformSnapshot) { s.Version = "" },
		"epoch":            func(s *ConformSnapshot) { s.Epoch = 0 },
		"healthy":          func(s *ConformSnapshot) { s.Healthy = false },
		"paused":           func(s *ConformSnapshot) { s.Paused = true },
		"rollback_version": func(s *ConformSnapshot) { s.RollbackVersion = "" },
		"explain":          func(s *ConformSnapshot) { s.Explain = "" },
	}
	for field, fn := range mutate {
		for kind, hook := range hooks {
			snap := conformHealthy(kind, hook)
			fn(&snap)
			rej, ok := AsRejection(CheckConformance(snap))
			if !ok {
				t.Fatalf("%s/%s: no rejection", kind, field)
			}
			if rej.Field != field {
				t.Fatalf("%s/%s: rejection names %q", kind, field, rej.Field)
			}
		}
	}
}

// TestTodo_ROLLOUT_008_Mutation: boundary mutants die — rollback-to-self,
// unpadded versus padded versions, epoch edges, hook prefixes.
func TestTodo_ROLLOUT_008_Mutation(t *testing.T) {
	base := conformHealthy(ConformService, "service:restart-cell")

	self := base
	self.RollbackVersion = self.Version
	if _, ok := AsRejection(CheckConformance(self)); !ok {
		t.Fatal("rollback-to-self accepted as a rollback plan")
	}
	padded := base
	padded.Version = " v1.4.0"
	if rej, ok := AsRejection(CheckConformance(padded)); !ok || rej.Field != "version" {
		t.Fatalf("padded version: %+v", rej)
	}
	epochOne := base
	epochOne.Epoch = 1
	if err := CheckConformance(epochOne); err != nil {
		t.Fatalf("epoch 1 refused: %v", err)
	}
	bare := base
	bare.ActivationHook = "restart-cell"
	if rej, ok := AsRejection(CheckConformance(bare)); !ok || rej.Field != "activation_hook" {
		t.Fatalf("unprefixed hook: %+v", rej)
	}
	unknown := base
	unknown.Kind = "FIRMWARE"
	if rej, ok := AsRejection(CheckConformance(unknown)); !ok || rej.Field != "kind" {
		t.Fatalf("unknown kind: %+v", rej)
	}
	// Rejection carries the offending version for attribution.
	if rej, _ := AsRejection(CheckConformance(padded)); rej.Version != " v1.4.0" {
		t.Fatalf("rejection version=%q", rej.Version)
	}
}
