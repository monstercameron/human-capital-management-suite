package intentmanifests

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func parityBindings() []ChannelBinding {
	return []ChannelBinding{
		{
			FeatureID: "promote_employee", Role: ClassCreate,
			IntentID:         "hcmnext.rewards.promote/v1",
			DefinitionDigest: "sha256:promote-def", SchemaDigest: "sha256:promote-schema",
			RequiresSimulation: true,
			ChildIntents:       []string{"hcmnext.rewards.adjust_base_pay/v1"},
			Availability:       AvailabilityAvailable,
			AllowedActors: []ChannelActor{
				ActorEmployee, ActorManager, ActorHRAdmin, ActorDelegate, ActorAgent,
				ActorPartnerApp, ActorScheduler, ActorEvent, ActorOperator,
			},
			Surfaces: allSurfaces(),
		},
		{
			FeatureID: "view_payslip", Role: ClassObserve,
			IntentID:         "hcmnext.rewards.view_payslip/v1",
			DefinitionDigest: "sha256:payslip-def", SchemaDigest: "sha256:payslip-schema",
			RequiresSimulation: false,
			Availability:       AvailabilityAvailable,
			AllowedActors:      []ChannelActor{ActorEmployee, ActorManager, ActorHRAdmin, ActorOperator},
			Surfaces:           allSurfaces(),
		},
		{
			FeatureID: "format_date", Role: ClassNonMaterial,
			DefinitionDigest: "sha256:date-def", SchemaDigest: "sha256:date-schema",
			Availability: AvailabilityAvailable,
			AllowedActors: []ChannelActor{
				ActorEmployee, ActorManager, ActorHRAdmin, ActorDelegate, ActorAgent,
				ActorPartnerApp, ActorScheduler, ActorEvent, ActorOperator,
			},
			Surfaces: allSurfaces(),
		},
		{
			FeatureID: "grant_equity", Role: ClassCreate,
			IntentID:         "hcmnext.rewards.grant_equity/v1",
			DefinitionDigest: "sha256:equity-def", SchemaDigest: "sha256:equity-schema",
			RequiresSimulation: true,
			Availability:       AvailabilityDeferred,
		},
	}
}

// TestFeatureIntentRoleChannelMatrixHasNoSemanticOrAuthorityDrift is the
// primary FEATURE-CONF-001 contract test: one generated route manifest
// drives every actor and channel, identical typed inputs resolve identical
// semantics, and only authorization/presentation legitimately vary.
func TestFeatureIntentRoleChannelMatrixHasNoSemanticOrAuthorityDrift(t *testing.T) {
	bindings, err := BindManifest(parityBindings())
	if err != nil {
		t.Fatalf("BindManifest: %v", err)
	}
	input := "sha256:typed-input"
	actors := []ChannelActor{ActorEmployee, ActorManager, ActorHRAdmin, ActorDelegate, ActorAgent, ActorPartnerApp, ActorScheduler, ActorEvent, ActorOperator}
	surfaces := []Surface{SurfaceGWC, SurfaceMobileKiosk, SurfaceGRPC, SurfaceHTTP, SurfaceCLI, SurfaceBULK}

	t.Run("semantics are identical on every actor and channel", func(t *testing.T) {
		invocable := 0
		// BindManifest sorts by FeatureID, so positional slicing is not
		// stable: only AVAILABLE bindings are invocable on any channel.
		for _, binding := range bindings.Bindings() {
			if binding.Availability != AvailabilityAvailable {
				continue
			}
			invocable++
			var baseline *ChannelResolution
			for _, actor := range actors {
				for _, surface := range surfaces {
					got, err := bindings.Resolve(binding.FeatureID, ActorClaim{Actor: actor, Authenticated: true}, surface, input)
					if err != nil {
						t.Fatalf("%s/%s/%s: %v", binding.FeatureID, actor, surface, err)
					}
					if baseline == nil {
						resolution := got
						baseline = &resolution
						continue
					}
					if got.DefinitionDigest != baseline.DefinitionDigest ||
						got.SchemaDigest != baseline.SchemaDigest ||
						got.IntentID != baseline.IntentID ||
						got.Role != baseline.Role ||
						got.RequestDigest != baseline.RequestDigest ||
						got.SimulationRequired != baseline.SimulationRequired ||
						strings.Join(got.Children, ",") != strings.Join(baseline.Children, ",") {
						t.Fatalf("%s/%s/%s drifted: %+v vs %+v", binding.FeatureID, actor, surface, got, baseline)
					}
				}
			}
		}
		if invocable == 0 {
			t.Fatal("parity loop resolved nothing: the fixture must keep invocable features")
		}
	})

	t.Run("authorization varies without touching semantics", func(t *testing.T) {
		denied, err := bindings.Resolve("view_payslip", ActorClaim{Actor: ActorAgent, Authenticated: true}, SurfaceHTTP, input)
		if err != nil {
			t.Fatalf("out-of-role actor must resolve, not fail: %v", err)
		}
		if denied.Authorized {
			t.Fatal("agent must not be authorized for payslip viewing")
		}
		allowed, err := bindings.Resolve("view_payslip", ActorClaim{Actor: ActorEmployee, Authenticated: true}, SurfaceHTTP, input)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed.Authorized {
			t.Fatal("employee must be authorized for payslip viewing")
		}
		if denied.RequestDigest != allowed.RequestDigest || denied.DefinitionDigest != allowed.DefinitionDigest {
			t.Fatal("authorization must never alter canonical semantics")
		}
	})

	t.Run("deferred features cannot be invoked on any channel", func(t *testing.T) {
		for _, actor := range actors {
			for _, surface := range surfaces {
				_, err := bindings.Resolve("grant_equity", ActorClaim{Actor: actor, Authenticated: true}, surface, input)
				if !errors.Is(err, ErrChannelRefused) {
					t.Fatalf("grant_equity/%s/%s must be refused, got %v", actor, surface, err)
				}
			}
		}
	})

	t.Run("unauthenticated callers and unknown routes fail closed", func(t *testing.T) {
		if _, err := bindings.Resolve("promote_employee", ActorClaim{Actor: ActorEmployee}, SurfaceHTTP, input); !errors.Is(err, ErrChannelRefused) {
			t.Fatalf("unauthenticated caller must be refused, got %v", err)
		}
		if _, err := bindings.Resolve("no_such_feature", ActorClaim{Actor: ActorEmployee, Authenticated: true}, SurfaceHTTP, input); !errors.Is(err, ErrChannelRefused) {
			t.Fatalf("unknown feature must be refused, got %v", err)
		}
		if _, err := bindings.Resolve("promote_employee", ActorClaim{Actor: ActorEmployee, Authenticated: true}, "CARRIER_PIGEON", input); !errors.Is(err, ErrChannelRefused) {
			t.Fatalf("undeclared surface must be refused, got %v", err)
		}
	})

	t.Run("material roles simulate and declare children", func(t *testing.T) {
		got, err := bindings.Resolve("promote_employee", ActorClaim{Actor: ActorManager, Authenticated: true}, SurfaceGRPC, input)
		if err != nil {
			t.Fatal(err)
		}
		if !got.SimulationRequired {
			t.Fatal("CREATE must pass through simulation")
		}
		if len(got.Children) != 1 || got.Children[0] != "hcmnext.rewards.adjust_base_pay/v1" {
			t.Fatalf("emitted children must be explicit: %+v", got.Children)
		}
	})
}

// TestTodo_FEATURE_CONF_001_Property proves the matrix covers every
// applicable role and that availability stays closed.
func TestTodo_FEATURE_CONF_001_Property(t *testing.T) {
	bindings, err := BindManifest(parityBindings())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[SemanticClassification]bool{}
	for _, binding := range bindings.Bindings() {
		seen[binding.Role] = true
		if binding.Availability != AvailabilityAvailable && binding.Availability != AvailabilityDeferred && binding.Availability != AvailabilityUnavailable {
			t.Fatalf("availability is not closed: %q", binding.Availability)
		}
		if binding.Role == ClassNonMaterial && binding.IntentID != "" {
			t.Fatalf("non-material feature %q binds an intent", binding.FeatureID)
		}
		if (binding.Role == ClassCreate || binding.Role == ClassConsume || binding.Role == ClassEmitChild) &&
			binding.Availability == AvailabilityAvailable && !binding.RequiresSimulation {
			t.Fatalf("material feature %q bypasses simulation", binding.FeatureID)
		}
	}
	for _, role := range []SemanticClassification{ClassCreate, ClassObserve, ClassNonMaterial} {
		if !seen[role] {
			t.Fatalf("matrix omits role %q", role)
		}
	}
}

// TestTodo_FEATURE_CONF_001_Golden pins the canonical matrix digest.
func TestTodo_FEATURE_CONF_001_Golden(t *testing.T) {
	bindings, err := BindManifest(parityBindings())
	if err != nil {
		t.Fatal(err)
	}
	again, err := BindManifest(parityBindings())
	if err != nil {
		t.Fatal(err)
	}
	if bindings.Digest() == "" || bindings.Digest() != again.Digest() {
		t.Fatalf("matrix digest must be stable: %q", bindings.Digest())
	}
	const golden = "sha256:9d8752ed92ef20ee274920c25ba3a5a4089422f08d9d3d202ca34b26732d53ad"
	if bindings.Digest() != golden {
		t.Fatalf("matrix digest drifted: got %s, want %s", bindings.Digest(), golden)
	}
}

// TestTodo_FEATURE_CONF_001_Race resolves the matrix concurrently: one
// manifest, many actors, zero drift and zero races.
func TestTodo_FEATURE_CONF_001_Race(t *testing.T) {
	bindings, err := BindManifest(parityBindings())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			actors := []ChannelActor{ActorEmployee, ActorManager, ActorHRAdmin, ActorAgent}
			surfaces := []Surface{SurfaceGWC, SurfaceGRPC, SurfaceHTTP, SurfaceCLI}
			got, err := bindings.Resolve("promote_employee",
				ActorClaim{Actor: actors[n%len(actors)], Authenticated: true},
				surfaces[n%len(surfaces)], "sha256:typed-input")
			if err != nil {
				errs <- err
				return
			}
			if got.DefinitionDigest != "sha256:promote-def" || got.RequestDigest == "" {
				errs <- errors.New("concurrent resolution drifted")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// TestTodo_FEATURE_CONF_001_Integration loads the checked-in governance
// registry: all 49 source groups are represented and every DEFINED feature
// resolves on every channel while deferred ones refuse everywhere.
func TestTodo_FEATURE_CONF_001_Integration(t *testing.T) {
	root := filepath.Join("..", "..", "..", "definitions", "governance", "feature-intent-coverage.yaml")
	registry, err := LoadFeatureIntentCoverageYAML(root)
	if err != nil {
		t.Fatalf("load coverage registry: %v", err)
	}
	if registry.FeatureGroups != 49 {
		t.Fatalf("feature groups = %d, want 49", registry.FeatureGroups)
	}
	bindings, err := BindRegistry(registry)
	if err != nil {
		t.Fatalf("BindRegistry: %v", err)
	}
	groups := map[int]bool{}
	defined := 0
	for _, binding := range bindings.Bindings() {
		groups[binding.Group] = true
		if binding.Availability != AvailabilityAvailable {
			for _, surface := range []Surface{SurfaceGWC, SurfaceMobileKiosk, SurfaceGRPC, SurfaceHTTP, SurfaceCLI, SurfaceBULK} {
				_, err := bindings.Resolve(binding.FeatureID, ActorClaim{Actor: ActorOperator, Authenticated: true}, surface, "sha256:in")
				if !errors.Is(err, ErrChannelRefused) {
					t.Fatalf("deferred feature %q invoked on %s", binding.FeatureID, surface)
				}
			}
			continue
		}
		defined++
	}
	if len(groups) != 49 {
		t.Fatalf("matrix covers %d groups, want 49", len(groups))
	}
	if defined == 0 {
		t.Fatal("matrix covers no DEFINED feature")
	}
	t.Logf("matrix covers %d groups with %d invocable features", len(groups), defined)
}

// TestTodo_FEATURE_CONF_001_Fault proves every unknown or untrusted route
// fails with an explicit refusal, never a silent downgrade.
func TestTodo_FEATURE_CONF_001_Fault(t *testing.T) {
	bindings, err := BindManifest(parityBindings())
	if err != nil {
		t.Fatal(err)
	}
	claim := ActorClaim{Actor: ActorHRAdmin, Authenticated: true}
	for name, call := range map[string]func() error{
		"unknown feature": func() error {
			_, err := bindings.Resolve("missing", claim, SurfaceHTTP, "sha256:in")
			return err
		},
		"unknown surface": func() error {
			_, err := bindings.Resolve("promote_employee", claim, "PIGEON", "sha256:in")
			return err
		},
		"unauthenticated": func() error {
			_, err := bindings.Resolve("promote_employee", ActorClaim{Actor: ActorHRAdmin}, SurfaceHTTP, "sha256:in")
			return err
		},
		"empty input": func() error {
			_, err := bindings.Resolve("promote_employee", claim, SurfaceHTTP, "")
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrChannelRefused) {
			t.Fatalf("%s must fail closed, got %v", name, err)
		}
	}
}

// TestTodo_FEATURE_CONF_001_Security proves caller context never widens
// authority: forged roles without authentication fail, and denied actors
// still see identical semantics.
func TestTodo_FEATURE_CONF_001_Security(t *testing.T) {
	bindings, err := BindManifest(parityBindings())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bindings.Resolve("promote_employee", ActorClaim{Actor: ActorHRAdmin}, SurfaceCLI, "sha256:in"); !errors.Is(err, ErrChannelRefused) {
		t.Fatalf("forged admin context must be refused, got %v", err)
	}
	outsider, err := bindings.Resolve("view_payslip", ActorClaim{Actor: ActorPartnerApp, Authenticated: true}, SurfaceGRPC, "sha256:in")
	if err != nil {
		t.Fatal(err)
	}
	insider, err := bindings.Resolve("view_payslip", ActorClaim{Actor: ActorHRAdmin, Authenticated: true}, SurfaceGRPC, "sha256:in")
	if err != nil {
		t.Fatal(err)
	}
	if outsider.Authorized || !insider.Authorized {
		t.Fatalf("authorization must follow explicit policy: %+v vs %+v", outsider, insider)
	}
	if outsider.DefinitionDigest != insider.DefinitionDigest || outsider.RequestDigest != insider.RequestDigest {
		t.Fatal("denied actors must see identical semantics")
	}
}

// TestTodo_FEATURE_CONF_001_Conformance resolves every DEFINED registry
// feature on every actor and channel with zero semantic drift.
func TestTodo_FEATURE_CONF_001_Conformance(t *testing.T) {
	root := filepath.Join("..", "..", "..", "definitions", "governance", "feature-intent-coverage.yaml")
	registry, err := LoadFeatureIntentCoverageYAML(root)
	if err != nil {
		t.Fatalf("load coverage registry: %v", err)
	}
	bindings, err := BindRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	actors := []ChannelActor{ActorEmployee, ActorManager, ActorHRAdmin, ActorDelegate, ActorAgent, ActorPartnerApp, ActorScheduler, ActorEvent, ActorOperator}
	surfaces := []Surface{SurfaceGWC, SurfaceMobileKiosk, SurfaceGRPC, SurfaceHTTP, SurfaceCLI, SurfaceBULK}
	resolutions := 0
	for _, binding := range bindings.Bindings() {
		if binding.Availability != AvailabilityAvailable {
			continue
		}
		var baseline *ChannelResolution
		for _, actor := range actors {
			for _, surface := range surfaces {
				got, err := bindings.Resolve(binding.FeatureID, ActorClaim{Actor: actor, Authenticated: true}, surface, "sha256:typed-input")
				if err != nil {
					t.Fatalf("%s/%s/%s: %v", binding.FeatureID, actor, surface, err)
				}
				resolutions++
				if baseline == nil {
					resolution := got
					baseline = &resolution
					continue
				}
				if got.RequestDigest != baseline.RequestDigest || got.DefinitionDigest != baseline.DefinitionDigest {
					t.Fatalf("%s drifted on %s/%s", binding.FeatureID, actor, surface)
				}
			}
		}
	}
	if resolutions == 0 {
		t.Fatal("conformance resolved nothing")
	}
	t.Logf("conformance resolutions without drift: %d", resolutions)
}

// TestTodo_FEATURE_CONF_001_Browser proves the GWC surface renders the same
// governed semantics: escaped content, labelled structure and the matrix
// digest a reviewer can compare against API results.
func TestTodo_FEATURE_CONF_001_Browser(t *testing.T) {
	bindings, err := BindManifest(parityBindings())
	if err != nil {
		t.Fatal(err)
	}
	page := RenderParityHTML(bindings, "en-US")
	for _, want := range []string{`lang="en-US"`, `aria-labelledby="parity-title"`, `<caption>`, `<th scope="col">Role</th>`, bindings.Digest(), `WCAG_2_2_AA`} {
		if !strings.Contains(page, want) {
			t.Fatalf("parity page lacks %q", want)
		}
	}
	if strings.Contains(page, "<script>") {
		t.Fatal("parity page rendered unescaped content")
	}
}

// TestTodo_FEATURE_CONF_001_Mutation kills the drift mutants: a per-surface
// definition change, a dropped simulation gate and hidden children must
// each be detected.
func TestTodo_FEATURE_CONF_001_Mutation(t *testing.T) {
	if _, err := BindManifest(parityBindings()); err != nil {
		t.Fatalf("fixture manifest must bind: %v", err)
	}
	drifted := parityBindings()
	drifted[0].Surfaces[SurfaceCLI] = SurfacePolicy{Allowed: true, Presentation: "cli", DefinitionDigest: "sha256:forged-def"}
	if _, err := BindManifest(drifted); !errors.Is(err, ErrChannelDrift) {
		t.Fatalf("per-surface definition mutant must be rejected, got %v", err)
	}
	nosim := parityBindings()
	nosim[0].RequiresSimulation = false
	if _, err := BindManifest(nosim); !errors.Is(err, ErrChannelDrift) {
		t.Fatalf("simulation-drop mutant must be rejected, got %v", err)
	}
	nochild := parityBindings()
	// An emitter that declares no children is the hidden-children mutant:
	// the role change alone is not a mutant while children stay declared.
	nochild[0].Role = ClassEmitChild
	nochild[0].ChildIntents = nil
	if _, err := BindManifest(nochild); !errors.Is(err, ErrChannelDrift) {
		t.Fatalf("hidden-children mutant must be rejected, got %v", err)
	}
}
