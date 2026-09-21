package envelope

import (
	"errors"
	"testing"
	"time"
)

func fixtureContext() Context {
	return Context{
		EngineDefinition: "hcmnext.engines.test/echo",
		EngineRevision:   "rev-1",
		Tenant:           "tenant-acme",
		Org:              "org-001",
		Purpose:          "ENGINE_CONF_001 conformance",
		Authority:        "auth-test",
		EffectiveAt:      time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		KnownAt:          time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
		SourceWatermarks: []string{"source-a@100", "source-b@200"},
		Classification:   "INTERNAL",
		Provenance:       "fixture",
	}
}

func fixtureInputs() []Input {
	return []Input{
		{Name: "salary_band", Value: "B4", Uncertainty: UncertaintyConfident, Classification: "INTERNAL", Provenance: "hr-source"},
		{Name: "employment_start", Value: "2024-03-01", Uncertainty: UncertaintyPartial, Classification: "INTERNAL", Provenance: "hr-source"},
		{Name: "manager_assessment", Value: "pending", Uncertainty: UncertaintyUnknown, Classification: "INTERNAL", Provenance: "hr-source"},
	}
}

// echoTransform is the honest engine: it derives outputs from inputs only
// and claims exactly the propagated floor.
func echoTransform(req Request) ([]Input, Uncertainty, error) {
	outputs := make([]Input, 0, len(req.Inputs))
	for _, in := range req.Inputs {
		outputs = append(outputs, Input{
			Name: in.Name + ":echo", Value: in.Value,
			Uncertainty: in.Uncertainty, Classification: req.Context.Classification, Provenance: req.Context.Provenance,
		})
	}
	return outputs, req.CombinedUncertainty(), nil
}

// TestSharedEngineEnvelopePinsContextPropagatesUnknownAndReplaysDeterministically
// is the PRIMARY ENGINE-CONF-001 contract test: one immutable typed
// envelope, pinned inputs and context, exact uncertainty propagation,
// canonical digests, deterministic replay and zero effects.
func TestSharedEngineEnvelopePinsContextPropagatesUnknownAndReplaysDeterministically(t *testing.T) {
	req, err := NewRequest(fixtureContext(), fixtureInputs())
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if got := req.CombinedUncertainty(); got != UncertaintyUnknown {
		t.Fatalf("combined uncertainty = %s, want UNKNOWN while one input is unknown", got)
	}

	t.Run("pinned context survives execution", func(t *testing.T) {
		res, err := Execute(req, echoTransform, "echo every input with its own uncertainty")
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.EngineDefinition != req.Context.EngineDefinition || res.EngineRevision != req.Context.EngineRevision {
			t.Fatal("result does not pin the executed definition and revision")
		}
		if res.InputsDigest != req.InputsDigest {
			t.Fatal("result is not bound to the pinned inputs digest")
		}
		if res.Uncertainty != UncertaintyUnknown {
			t.Fatalf("result uncertainty = %s, want the propagated UNKNOWN floor", res.Uncertainty)
		}
		if res.ResultDigest == "" || res.ExplanationDigest == "" {
			t.Fatal("result carries no canonical digests")
		}
		explanation, err := Explain(res)
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		if explanation.ResultDigest != res.ResultDigest || explanation.ExplanationDigest != res.ExplanationDigest {
			t.Fatalf("explanation identity = %+v, want result digests", explanation)
		}
		if res.EffectCount != 0 {
			t.Fatalf("EffectCount = %d, want zero: engines have no effect channel", res.EffectCount)
		}
	})

	t.Run("RED: incomplete result cannot be explained", func(t *testing.T) {
		if _, err := Explain(Result{}); !errors.Is(err, ErrContextIncomplete) {
			t.Fatalf("Explain(empty) = %v, want ErrContextIncomplete", err)
		}
	})

	t.Run("replay is deterministic", func(t *testing.T) {
		first, err := Execute(req, echoTransform, "echo every input with its own uncertainty")
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if err := VerifyReplay(req, echoTransform, "echo every input with its own uncertainty", first); err != nil {
			t.Fatalf("VerifyReplay: %v", err)
		}
	})

	t.Run("RED: incomplete context never executes", func(t *testing.T) {
		broken := fixtureContext()
		broken.Tenant = ""
		if _, err := NewRequest(broken, fixtureInputs()); !errors.Is(err, ErrContextIncomplete) {
			t.Fatalf("tenant-less context must be refused, got %v", err)
		}
		bare, err := NewRequest(fixtureContext(), fixtureInputs())
		if err != nil {
			t.Fatal(err)
		}
		bare.InputsDigest = "sha256:forged"
		if _, err := Execute(bare, echoTransform, "x"); !errors.Is(err, ErrContextIncomplete) {
			t.Fatalf("tampered inputs digest must be refused, got %v", err)
		}
	})

	t.Run("RED: UNKNOWN inputs never become confident results", func(t *testing.T) {
		overconfident := func(req Request) ([]Input, Uncertainty, error) {
			out, _, err := echoTransform(req)
			return out, UncertaintyConfident, err
		}
		if _, err := Execute(req, overconfident, "claims confidence over unknown inputs"); !errors.Is(err, ErrUncertaintyDowngrade) {
			t.Fatalf("uncertainty downgrade must be refused, got %v", err)
		}
	})
}

// TestTodo_ENGINE_CONF_001_Golden pins the canonical digests for the
// fixture request so any framing drift fails loudly.
func TestTodo_ENGINE_CONF_001_Golden(t *testing.T) {
	req, err := NewRequest(fixtureContext(), fixtureInputs())
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	res, err := Execute(req, echoTransform, "echo every input with its own uncertainty")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	const (
		goldenInputs = "sha256:a29a292bf5fd382e31eed9dc254bba8c4ca000d7219ad0c7430aeabbb486f2d0"
		goldenResult = "sha256:abc369c4e389f59e23f12201b11d37056d87ff218c583b561ba7f0145347156c"
		goldenExpl   = "sha256:facea90e5baed202f45f407c70dfe5ad0f42880ea5a700aba3e8162f5c334b02"
	)
	if req.InputsDigest != goldenInputs || res.ResultDigest != goldenResult || res.ExplanationDigest != goldenExpl {
		t.Fatalf("envelope digests drifted: inputs=%s result=%s explanation=%s",
			req.InputsDigest, res.ResultDigest, res.ExplanationDigest)
	}
	again, err := Execute(req, echoTransform, "echo every input with its own uncertainty")
	if err != nil {
		t.Fatal(err)
	}
	if res.ResultDigest != again.ResultDigest || res.ExplanationDigest != again.ExplanationDigest {
		t.Fatal("identical requests produce different digests: replay is not deterministic")
	}
	t.Logf("inputs=%s result=%s explanation=%s", req.InputsDigest, res.ResultDigest, res.ExplanationDigest)
}
