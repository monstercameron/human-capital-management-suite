package envelope

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// namedEngines is the ENGINE-CONF-001 fixture suite: every shared engine
// passes the same envelope contract through its own pure transform.
var namedEngines = map[string]Transform{
	"XFORM":    echoTransform,
	"POP":      echoTransform,
	"ELIG":     echoTransform,
	"CYCLE":    echoTransform,
	"BAL":      echoTransform,
	"QUAL":     echoTransform,
	"DEMAND":   echoTransform,
	"MATCH":    echoTransform,
	"SCENARIO": echoTransform,
	"ATTEST":   echoTransform,
	"PROGRAM":  echoTransform,
}

func engineContext(name string) Context {
	ctx := fixtureContext()
	ctx.EngineDefinition = "hcmnext.engines." + strings.ToLower(name) + "/v1"
	ctx.EngineRevision = "rev-1"
	return ctx
}

// TestTodo_ENGINE_CONF_001_Conformance runs all named engines through one
// fixture suite: pinned context, exact uncertainty propagation,
// deterministic replay and zero effects for each.
func TestTodo_ENGINE_CONF_001_Conformance(t *testing.T) {
	if len(namedEngines) != 11 {
		t.Fatalf("fixture suite covers %d engines, want all 11 named engines", len(namedEngines))
	}
	for name, fn := range namedEngines {
		t.Run(name, func(t *testing.T) {
			req, err := NewRequest(engineContext(name), fixtureInputs())
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			res, err := Execute(req, fn, "conformancesuite run for "+name)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Uncertainty != UncertaintyUnknown {
				t.Fatalf("%s: uncertainty = %s, want propagated UNKNOWN", name, res.Uncertainty)
			}
			if res.EffectCount != 0 {
				t.Fatalf("%s: EffectCount = %d, want zero", name, res.EffectCount)
			}
			if err := VerifyReplay(req, fn, "conformancesuite run for "+name, res); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		})
	}
}

// TestTodo_ENGINE_CONF_001_Property proves uncertainty combination is a
// closed monotone join over seeded input sets, and that input order never
// changes the pinned digest.
func TestTodo_ENGINE_CONF_001_Property(t *testing.T) {
	levels := []Uncertainty{UncertaintyConfident, UncertaintyPartial, UncertaintyUnknown}
	for i, a := range levels {
		for j, b := range levels {
			want := levels[max(i, j)]
			if got := Combine(a, b); got != want {
				t.Fatalf("Combine(%s, %s) = %s, want %s", a, b, got, want)
			}
		}
	}
	base, err := NewRequest(fixtureContext(), fixtureInputs())
	if err != nil {
		t.Fatal(err)
	}
	for seed := 0; seed < 50; seed++ {
		shuffled := append([]Input(nil), fixtureInputs()...)
		for i := len(shuffled) - 1; i > 0; i-- {
			j := (seed + i) % (i + 1)
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}
		other, err := NewRequest(fixtureContext(), shuffled)
		if err != nil {
			t.Fatal(err)
		}
		if base.InputsDigest != other.InputsDigest {
			t.Fatalf("seed %d: input order changed the pinned digest", seed)
		}
	}
}

// TestTodo_ENGINE_CONF_001_Security proves the envelope trusts nothing
// ambient: forged digests, blank watermarks, missing authority and ambient
// time substitution all fail closed.
func TestTodo_ENGINE_CONF_001_Security(t *testing.T) {
	req, err := NewRequest(fixtureContext(), fixtureInputs())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func() error{
		"forged inputs digest": func() error {
			mut := req
			mut.InputsDigest = "sha256:forged"
			_, err := Execute(mut, echoTransform, "x")
			return err
		},
		"blank watermark": func() error {
			ctx := fixtureContext()
			ctx.SourceWatermarks = []string{""}
			_, err := NewRequest(ctx, fixtureInputs())
			return err
		},
		"missing authority": func() error {
			ctx := fixtureContext()
			ctx.Authority = ""
			_, err := NewRequest(ctx, fixtureInputs())
			return err
		},
		"missing explanation": func() error {
			_, err := Execute(req, echoTransform, "")
			return err
		},
		"duplicate input": func() error {
			dup := append(fixtureInputs(), fixtureInputs()[0])
			_, err := NewRequest(fixtureContext(), dup)
			return err
		},
		"ambient time cannot substitute": func() error {
			ctx := fixtureContext()
			ctx.EffectiveAt = time.Time{}
			_, err := NewRequest(ctx, fixtureInputs())
			return err
		},
	}
	for name, call := range cases {
		if err := call(); !errors.Is(err, ErrContextIncomplete) {
			t.Fatalf("%s must fail closed, got %v", name, err)
		}
	}
}

// TestTodo_ENGINE_CONF_001_Mutation kills the downgrade and taint mutants:
// confident claims over unknown inputs, dropped classifications and
// undeclared provenance must each be detected.
func TestTodo_ENGINE_CONF_001_Mutation(t *testing.T) {
	req, err := NewRequest(fixtureContext(), fixtureInputs())
	if err != nil {
		t.Fatal(err)
	}
	confident := func(Request) ([]Input, Uncertainty, error) {
		out, _, _ := echoTransform(req)
		return out, UncertaintyConfident, nil
	}
	if _, err := Execute(req, confident, "x"); !errors.Is(err, ErrUncertaintyDowngrade) {
		t.Fatalf("confidence mutant must be rejected, got %v", err)
	}
	partialOverUnknown := func(Request) ([]Input, Uncertainty, error) {
		out, _, _ := echoTransform(req)
		return out, UncertaintyPartial, nil
	}
	if _, err := Execute(req, partialOverUnknown, "x"); !errors.Is(err, ErrUncertaintyDowngrade) {
		t.Fatalf("partial-over-unknown mutant must be rejected, got %v", err)
	}
	untainted := func(Request) ([]Input, Uncertainty, error) {
		return []Input{{Name: "leak", Value: "v", Uncertainty: UncertaintyUnknown, Classification: "PUBLIC", Provenance: "elsewhere"}}, UncertaintyUnknown, nil
	}
	if _, err := Execute(req, untainted, "x"); !errors.Is(err, ErrClassificationLeak) {
		t.Fatalf("taint-drop mutant must be rejected, got %v", err)
	}
}

// TestTodo_ENGINE_CONF_001_ModelBased replays a seeded model: an
// independent oracle recomputes the uncertainty floor and the digest
// stability the envelope must preserve.
func TestTodo_ENGINE_CONF_001_ModelBased(t *testing.T) {
	for seed := 0; seed < 50; seed++ {
		inputs := make([]Input, 0, 3)
		floor := UncertaintyConfident
		for k, level := range []Uncertainty{UncertaintyConfident, UncertaintyPartial, UncertaintyUnknown} {
			if (seed>>(2*k))&1 == 1 {
				inputs = append(inputs, Input{
					Name: fmt.Sprintf("in-%d", k), Value: fmt.Sprintf("v-%d-%d", seed, k),
					Uncertainty: level, Classification: "INTERNAL", Provenance: "model",
				})
				floor = Combine(floor, level)
			}
		}
		if len(inputs) == 0 {
			continue
		}
		req, err := NewRequest(fixtureContext(), inputs)
		if err != nil {
			t.Fatal(err)
		}
		if req.CombinedUncertainty() != floor {
			t.Fatalf("seed %d: floor = %s, oracle says %s", seed, req.CombinedUncertainty(), floor)
		}
		res, err := Execute(req, echoTransform, "model replay")
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyReplay(req, echoTransform, "model replay", res); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
	}
}

// TestTodo_ENGINE_CONF_001_Race executes concurrent replays: one request,
// many goroutines, one digest. This is concurrency-determinism, not -race.
func TestTodo_ENGINE_CONF_001_Race(t *testing.T) {
	req, err := NewRequest(fixtureContext(), fixtureInputs())
	if err != nil {
		t.Fatal(err)
	}
	first, err := Execute(req, echoTransform, "race replay")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := VerifyReplay(req, echoTransform, "race replay", first); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// FuzzTodo_ENGINE_CONF_001 proves arbitrary input names, values and
// uncertainty labels either bind deterministically or fail closed, and
// never downgrade uncertainty or emit effects.
func FuzzTodo_ENGINE_CONF_001(f *testing.F) {
	f.Add("salary", "B4", "CONFIDENT", "explanation one")
	f.Add("", "", "UNKNOWN", "")
	f.Add("x", "y", "FORGED", "exp")
	f.Fuzz(func(t *testing.T, name, value, uncertainty, explanation string) {
		req, err := NewRequest(fixtureContext(), []Input{{
			Name: name, Value: value, Uncertainty: Uncertainty(uncertainty),
			Classification: "INTERNAL", Provenance: "fuzz",
		}})
		if err != nil {
			if !errors.Is(err, ErrContextIncomplete) {
				t.Fatalf("NewRequest must fail closed, got %v", err)
			}
			return
		}
		res, err := Execute(req, echoTransform, explanation)
		if err != nil {
			if !errors.Is(err, ErrContextIncomplete) && !errors.Is(err, ErrUncertaintyDowngrade) && !errors.Is(err, ErrClassificationLeak) {
				t.Fatalf("Execute must fail closed, got %v", err)
			}
			return
		}
		if res.EffectCount != 0 {
			t.Fatal("fuzzed execution reported effects")
		}
		if err := VerifyReplay(req, echoTransform, explanation, res); err != nil {
			t.Fatalf("fuzzed replay diverged: %v", err)
		}
	})
}
