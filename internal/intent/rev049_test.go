package intent_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/evolution"
)

// rev049Schema pins one version of a request or result schema.
func rev049Schema(name string) intent.SchemaRef {
	return intent.SchemaRef{SchemaID: name, Version: 1, ProtobufFullName: name}
}

// rev049Def returns a valid, minimal PromoteWorker-shaped definition at the
// given version. It is self-contained rather than imported from the catalog
// fixtures so this test never drifts because another lane edited the catalog.
func rev049Def(version uint32) intent.Definition {
	return intent.Definition{
		Ref:          intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: version},
		DisplayName:  "PromoteWorker",
		Description:  "Propose and execute a governed upward job or position change for one employment assignment.",
		OwnerDomain:  "PEOPLE",
		OwnerPlane:   "DOMAIN",
		Family:       intent.FamilyChangeRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectInternalMutation,
		EffectClass:  intent.EffectClassZero,
		Release:      intent.ReleaseP1A,
		InputSchema:  rev049Schema("hcmnext.people.v1.PromoteWorkerRequest"),
		ResultSchema: rev049Schema("hcmnext.people.v1.PromoteWorkerResult"),
		PhaseDepth:   "GATE_A_CONTRACT_GATE_B_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorAgent,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeReplay,
		},
		SubjectKinds: []string{"PERSON", "EMPLOYMENT", "POSITION"},
		RequiredInputs: []intent.RequiredInput{
			{Path: "employment_ref", Kind: intent.InputKindSubjectRef, Required: true},
			{Path: "target_position_ref", Kind: intent.InputKindReference, Required: true},
			{Path: "effective_time", Kind: intent.InputKindEffectiveTime, Required: true},
			{Path: "reason_ref", Kind: intent.InputKindReference, Required: true},
		},
		ApprovalRequired: true,
	}
}

func rev049Catalog() intent.Catalog {
	return intent.Catalog{
		Schemas: []intent.SchemaRef{
			rev049Schema("hcmnext.people.v1.PromoteWorkerRequest"),
			rev049Schema("hcmnext.people.v1.PromoteWorkerResult"),
		},
	}
}

func rev049Registry(t *testing.T, defs ...intent.Definition) *intent.Registry {
	t.Helper()
	reg, err := intent.NewRegistry(intent.ProfileManaged, defs, nil, rev049Catalog())
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	return reg
}

func rev049CompatibleV2() intent.Definition {
	d := rev049Def(2)
	d.RequiredInputs = append(append([]intent.RequiredInput(nil), d.RequiredInputs...),
		intent.RequiredInput{Path: "notes", Kind: intent.InputKindDocument, Required: false})
	return d
}

func rev049IncompatibleV2() intent.Definition {
	d := rev049Def(2)
	d.RequiredInputs = append(append([]intent.RequiredInput(nil), d.RequiredInputs...),
		intent.RequiredInput{Path: "compensation_committee_approval_ref", Kind: intent.InputKindReference, Required: true})
	return d
}

// TestTodo_REV_049_02 is the PRIMARY test: the registry gains a MANAGED
// publish action that runs the evolution compatibility check before
// accepting a new Definition version. Incompatible versions are refused
// with a typed error, compatible ones are accepted, and the check evidence
// is recorded on the receipt.
func TestTodo_REV_049_02(t *testing.T) {
	t.Run("bootstrap registries reject runtime publication", func(t *testing.T) {
		bootstrap, err := intent.NewRegistry(intent.ProfileBootstrap, []intent.Definition{rev049Def(1)}, nil, rev049Catalog())
		if err != nil {
			t.Fatalf("compile bootstrap registry: %v", err)
		}
		_, _, err = bootstrap.PublishManaged(rev049CompatibleV2(), evolution.ManagedChecker())
		if !errors.Is(err, intent.ErrManagedPublishRequiresManagedProfile) {
			t.Fatalf("bootstrap publish err = %v, want ErrManagedPublishRequiresManagedProfile", err)
		}
		if bootstrap.Len() != 1 {
			t.Fatal("a rejected bootstrap publish changed the registry")
		}
	})

	t.Run("GREEN: a compatible version publishes through the real evolution check", func(t *testing.T) {
		reg := rev049Registry(t, rev049Def(1))
		next, receipt, err := reg.PublishManaged(rev049CompatibleV2(), evolution.ManagedChecker())
		if err != nil {
			t.Fatalf("PublishManaged: %v", err)
		}
		if !receipt.Compatible {
			t.Fatalf("compatible publish recorded %+v", receipt)
		}
		if receipt.Evidence == "" {
			t.Fatal("the receipt records no check evidence")
		}
		if receipt.IntentTypeID != "hcmnext.people.promote_worker" || receipt.PreviousVersion != 1 || receipt.Version != 2 {
			t.Fatalf("receipt does not name the transition: %+v", receipt)
		}
		if next.Len() != 2 {
			t.Fatalf("managed registry holds %d definitions, want 2", next.Len())
		}
		if _, err := next.Resolve(intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 2}); err != nil {
			t.Fatalf("published v2 does not resolve: %v", err)
		}
		if reg.Len() != 1 {
			t.Fatal("PublishManaged mutated the source registry; the registry is immutable")
		}
	})

	t.Run("RED: an incompatible version is refused with a typed error", func(t *testing.T) {
		reg := rev049Registry(t, rev049Def(1))
		_, _, err := reg.PublishManaged(rev049IncompatibleV2(), evolution.ManagedChecker())
		if !errors.Is(err, intent.ErrIncompatibleDefinitionVersion) {
			t.Fatalf("incompatible publish err = %v, want ErrIncompatibleDefinitionVersion", err)
		}
		if reg.Len() != 1 {
			t.Fatal("a refused publish changed the registry")
		}
	})

	t.Run("a duplicate version is refused without running the check", func(t *testing.T) {
		reg := rev049Registry(t, rev049Def(1))
		called := false
		spy := intent.CompatibilityChecker(func(_, _ intent.Definition) (bool, string, error) {
			called = true
			return true, "spy", nil
		})
		dup := rev049Def(1)
		dup.Description = "A different description with the same version."
		_, _, err := reg.PublishManaged(dup, spy)
		if !errors.Is(err, intent.ErrDuplicateDefinition) {
			t.Fatalf("duplicate publish err = %v, want ErrDuplicateDefinition", err)
		}
		if called {
			t.Fatal("the compatibility check ran for a duplicate version; published versions are immutable")
		}
	})

	t.Run("a non-advancing version is refused", func(t *testing.T) {
		reg := rev049Registry(t, rev049Def(1))
		// Publish v3 over v1 (optional inputs only, so compatible) and
		// leave v2 unpublished: the v2 candidate is then below the tip
		// without reusing a published ref.
		v3 := rev049CompatibleV2()
		v3.Ref.Version = 3
		v3.RequiredInputs = append(append([]intent.RequiredInput(nil), v3.RequiredInputs...),
			intent.RequiredInput{Path: "extra", Kind: intent.InputKindDocument, Required: false})
		reg3, _, err := reg.PublishManaged(v3, evolution.ManagedChecker())
		if err != nil {
			t.Fatalf("publish v3: %v", err)
		}
		_, _, err = reg3.PublishManaged(rev049CompatibleV2(), evolution.ManagedChecker())
		if !errors.Is(err, intent.ErrManagedVersionNotAdvancing) {
			t.Fatalf("non-advancing publish err = %v, want ErrManagedVersionNotAdvancing", err)
		}
	})

	t.Run("a checker failure is not an acceptance", func(t *testing.T) {
		reg := rev049Registry(t, rev049Def(1))
		boom := errors.New("checker down")
		broken := intent.CompatibilityChecker(func(_, _ intent.Definition) (bool, string, error) {
			return false, "", boom
		})
		_, _, err := reg.PublishManaged(rev049CompatibleV2(), broken)
		if !errors.Is(err, boom) {
			t.Fatalf("checker failure err = %v, want the checker error", err)
		}
		if reg.Len() != 1 {
			t.Fatal("a failed check changed the registry")
		}
	})

	t.Run("a nil checker is a caller error, never a silent pass", func(t *testing.T) {
		reg := rev049Registry(t, rev049Def(1))
		if _, _, err := reg.PublishManaged(rev049CompatibleV2(), nil); err == nil {
			t.Fatal("a nil compatibility checker silently passed")
		}
	})

	t.Run("genesis publishes the first version with recorded evidence", func(t *testing.T) {
		reg := rev049Registry(t)
		next, receipt, err := reg.PublishManaged(rev049Def(1), evolution.ManagedChecker())
		if err != nil {
			t.Fatalf("genesis publish: %v", err)
		}
		if !receipt.Compatible || receipt.PreviousVersion != 0 || receipt.Evidence == "" {
			t.Fatalf("genesis receipt must record first-version evidence: %+v", receipt)
		}
		if next.Len() != 1 {
			t.Fatalf("managed registry holds %d definitions, want 1", next.Len())
		}
	})

	t.Run("a mutating checker cannot alter the predecessor or published candidate", func(t *testing.T) {
		reg := rev049Registry(t, rev049Def(1))
		candidate := rev049CompatibleV2()
		checker := intent.CompatibilityChecker(func(previous, current intent.Definition) (bool, string, error) {
			previous.RequiredInputs[0].Path = "mutated predecessor"
			current.RequiredInputs[0].Path = "mutated candidate"
			return true, "mutation probe", nil
		})
		next, _, err := reg.PublishManaged(candidate, checker)
		if err != nil {
			t.Fatalf("PublishManaged: %v", err)
		}
		original, err := reg.Resolve(intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 1})
		if err != nil {
			t.Fatalf("resolve original: %v", err)
		}
		if original.RequiredInputs[0].Path != "employment_ref" {
			t.Fatalf("checker mutated predecessor input path to %q", original.RequiredInputs[0].Path)
		}
		published, err := next.Resolve(intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 2})
		if err != nil {
			t.Fatalf("resolve published: %v", err)
		}
		if published.RequiredInputs[0].Path != "employment_ref" {
			t.Fatalf("checker mutation reached published input path %q", published.RequiredInputs[0].Path)
		}
		if candidate.RequiredInputs[0].Path != "employment_ref" {
			t.Fatalf("checker mutation reached caller candidate input path %q", candidate.RequiredInputs[0].Path)
		}
		candidate.RequiredInputs[0].Path = "caller mutation"
		published, err = next.Resolve(intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 2})
		if err != nil {
			t.Fatalf("resolve published after caller mutation: %v", err)
		}
		if published.RequiredInputs[0].Path != "employment_ref" {
			t.Fatalf("caller mutation reached published input path %q", published.RequiredInputs[0].Path)
		}
	})
}

// TestTodo_REV_049_02_Golden pins the accepted-version receipt bytes and
// digest: the evidence a compatible publish records is byte-stable.
func TestTodo_REV_049_02_Golden(t *testing.T) {
	reg := rev049Registry(t, rev049Def(1))
	_, receipt, err := reg.PublishManaged(rev049CompatibleV2(), evolution.ManagedChecker())
	if err != nil {
		t.Fatalf("PublishManaged: %v", err)
	}
	goldenText(t, "rev049_receipt.golden", receipt.CanonicalBytes())
	raw, err := os.ReadFile(filepath.Join("testdata", "rev049_receipt.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	sum := sha256.Sum256(raw)
	if receipt.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("receipt digest = %q, want the digest of the golden bytes", receipt.Digest)
	}
	if receipt.Digest == "" {
		t.Fatal("the receipt carries no digest")
	}
}
