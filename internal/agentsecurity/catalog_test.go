package agentsecurity

import (
	"context"
	"errors"
	"testing"
)

// stubCatalog is a scripted DefinitionCatalog for port-contract tests.
type stubCatalog struct {
	view CatalogDefinition
	err  error
}

func (s stubCatalog) LookupDefinition(context.Context, string) (CatalogDefinition, error) {
	if s.err != nil {
		return CatalogDefinition{}, s.err
	}
	return s.view, nil
}

func catalogTestAction(id string) ProposedAction {
	return ProposedAction{
		DefinitionID: id,
		Arguments:    map[string]string{"subject": "person:p1", "days": "3"},
		Sources:      []string{"person:p1"},
		Taint:        []string{"DERIVED"},
		Uncertainty:  "balance read is point-in-time",
		Bulk:         1,
	}
}

// TestCatalogPortBindsCompilerToLiveView proves the compiler takes its gates
// from the DefinitionCatalog view: review, simulation and version flow from
// the port, and an unknown definition refuses without a draft.
func TestCatalogPortBindsCompilerToLiveView(t *testing.T) {
	registry, admission := ingestionFixture(t)
	compiler, err := NewActionCompiler(stubCatalog{view: CatalogDefinition{
		ID: "live.promote", Version: "v3",
		RequiresReview: true, RequiresSimulation: true, MaxBulk: 2,
		InputSchemaRef:    "live.v1.PromoteRequest/v1",
		CapabilityRefs:    []string{"live.promote.plan/v1"},
		GovernanceRefs:    []string{"approval.live/v1"},
		SideEffectProfile: "INTERNAL_MUTATION", RiskClass: "R3",
	}}, registry)
	if err != nil {
		t.Fatalf("NewActionCompiler: %v", err)
	}
	draft, err := compiler.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), catalogTestAction("live.promote"), conciergeAttribution())
	if err != nil {
		t.Fatalf("CompileAction: %v", err)
	}
	if draft.DefinitionID != "live.promote" || draft.DefinitionVersion != "v3" {
		t.Fatalf("draft targets %s@%s, want live.promote@v3", draft.DefinitionID, draft.DefinitionVersion)
	}
	if !draft.RequiresReview || !draft.RequiresSimulation {
		t.Fatal("draft lost the catalog view's review/simulation gates")
	}

	unknown, err := NewActionCompiler(stubCatalog{err: ErrUnknownDefinition}, registry)
	if err != nil {
		t.Fatalf("NewActionCompiler: %v", err)
	}
	if _, err := unknown.CompileAction(context.Background(), admission, "people.lookup", validActionOutput(), catalogTestAction("live.invented"), conciergeAttribution()); err == nil {
		t.Fatal("compiler accepted a definition the catalog does not publish")
	} else {
		var refused *Refusal
		if !errors.As(err, &refused) || refused.Code != RefusalCapability {
			t.Fatalf("refusal = %v, want a RefusalCapability refusal", err)
		}
	}
}

// TestCatalogSentinelIsDiscoverable pins the unknown-definition sentinel the
// compiler keys its refusal on.
func TestCatalogSentinelIsDiscoverable(t *testing.T) {
	if ErrUnknownDefinition == nil || ErrUnknownDefinition.Error() == "" {
		t.Fatal("ErrUnknownDefinition must be a non-empty sentinel")
	}
	r := NewDefinitionRegistry()
	if _, err := r.LookupDefinition(context.Background(), "missing"); !errors.Is(err, ErrUnknownDefinition) {
		t.Fatalf("empty test catalog error = %v, want ErrUnknownDefinition", err)
	}
}
