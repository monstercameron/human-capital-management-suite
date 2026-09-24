package extensionregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestTodo_WF_EXT_011(t *testing.T) {
	manifest := validManifest()
	reg := New()
	if err := reg.Register(manifest); err != nil {
		t.Fatalf("register: %v", err)
	}
	resolved, ok := reg.Resolve(Ref{ID: manifest.ID, Version: manifest.Version})
	if !ok || resolved.Digest == "" || resolved.Manifest.Kind != KindBlock {
		t.Fatalf("resolved = %+v, ok=%v", resolved, ok)
	}
	if err := reg.Register(manifest); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate registration error = %v", err)
	}
	resolved.Manifest.Fixtures[0].Input[0] = 'x'
	again, _ := reg.Resolve(Ref{ID: manifest.ID, Version: manifest.Version})
	if string(again.Manifest.Fixtures[0].Input) != `{"value":1}` {
		t.Fatalf("registry fixture aliased caller: %s", again.Manifest.Fixtures[0].Input)
	}
}

func TestTodo_WF_EXT_011_Golden(t *testing.T) {
	m := validManifest()
	digest, err := Digest(m)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "sha256:35ff5a80e9d1e5baf3f52e73c344f705d0ab246dd0cc01251e4cfb2b3a7f0fbc" {
		t.Fatalf("digest = %s", digest)
	}
}

func TestTodo_WF_EXT_011_Conformance(t *testing.T) {
	reg := New()
	kinds := []Kind{KindBlock, KindFragment, KindReducer, KindFunction, KindTrigger, KindForm, KindBinding, KindCapability}
	for i, kind := range kinds {
		m := validManifest()
		m.ID = fmt.Sprintf("extension.%02d", i)
		m.Kind = kind
		if err := reg.Register(m); err != nil {
			t.Fatalf("register %s: %v", kind, err)
		}
	}
	fixtureExecutor := FixtureExecutorFunc(func(_ context.Context, in json.RawMessage) (json.RawMessage, error) {
		var value struct {
			Value int `json:"value"`
		}
		if err := json.Unmarshal(in, &value); err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Result int `json:"result"`
		}{value.Value + 1})
	})
	executors := map[Kind]FixtureExecutor{}
	for _, kind := range kinds {
		executors[kind] = fixtureExecutor
	}
	runner := ConformanceRunner{Executors: executors}
	if err := runner.Run(context.Background(), reg); err != nil {
		t.Fatalf("conformance: %v", err)
	}
	unsafe := validManifest()
	unsafe.ID = "block.unsafe"
	unsafe.Safety = SafetyContract{MinimumEffect: EffectExternalMutation, RequiresSafePoint: true}
	unsafe.Proof = CompilerProof{Effect: EffectPure, SafePoint: false}
	if err := reg.Register(unsafe); !errors.Is(err, ErrProofWeakened) {
		t.Fatalf("weakened proof registration = %v", err)
	}
}

func validManifest() Manifest {
	return Manifest{
		ID: "block.increment", Version: "1.0.0", Kind: KindBlock, Owner: "workflow",
		Fixtures: []Fixture{{ID: "increments", Input: json.RawMessage(`{"value":1}`), Expected: json.RawMessage(`{"result":2}`)}},
		Review:   ReviewRecord{ID: "review-1", Reviewer: "owner", Authority: "workflow.registry", ReviewedAt: "2026-09-23T12:00:00Z", Decision: "APPROVED", Rationale: "fixture verified"},
		Safety:   SafetyContract{MinimumEffect: EffectPure}, Proof: CompilerProof{Effect: EffectPure},
	}
}
