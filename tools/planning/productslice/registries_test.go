package productslice

import "testing"

// These tests exercise the loaders against the real repository tree (the
// same way tools/policy/phaseonegate's own tests call BuildProductionGraph
// against the live tree): they prove the seven Registries fields actually
// resolve the real ids PromotionSliceDefinition names, not a mocked
// fixture.

func TestLoadCapabilityIDsIncludesPromotionCapabilities(t *testing.T) {
	ids, err := LoadCapabilityIDs()
	if err != nil {
		t.Fatalf("LoadCapabilityIDs: %v", err)
	}
	for _, want := range []string{
		"hcmnext.people.promote_worker",
		"hcmnext.rewards.simulate_compensation",
		"hcmnext.rewards.evaluate_pay_band_position",
	} {
		if !ids[want] {
			t.Errorf("capability id %q not found in the live BOOTSTRAP registry", want)
		}
	}
	if ids["hcmnext.payroll.run_payroll"] {
		t.Error("unpublished capability id unexpectedly resolved")
	}
}

func TestLoadCoverageRegistryAndProjections(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	coverage, err := LoadCoverageRegistry(root)
	if err != nil {
		t.Fatalf("LoadCoverageRegistry: %v", err)
	}
	if len(coverage.Features) == 0 {
		t.Fatal("coverage registry has no features")
	}

	features := FeatureIDs(coverage)
	for _, want := range []string{"promotion_execute", "promotion_request_submit", "payband_position_evaluate"} {
		if !features[want] {
			t.Errorf("feature id %q not found in the live coverage registry", want)
		}
	}
	if features["this_feature_does_not_exist"] {
		t.Error("unknown feature id unexpectedly resolved")
	}

	intents := BusinessIntentIDs(coverage)
	for _, want := range []string{"hcmnext.people.promote_worker/v1", "hcmnext.rewards.simulate_compensation/v1"} {
		if !intents[want] {
			t.Errorf("business intent %q not found in the live coverage registry", want)
		}
	}
	if intents["DEFERRED"] {
		t.Error("the DEFERRED sentinel must never resolve as a business intent")
	}
}

func TestLoadCoverageRegistryMissingFile(t *testing.T) {
	if _, err := LoadCoverageRegistry(t.TempDir()); err == nil {
		t.Fatal("expected an error loading the coverage registry from a directory with no definitions/ tree")
	}
}

func TestPageIDsIncludesPromotionPages(t *testing.T) {
	pages := PageIDs()
	for _, want := range []string{"promotion.journeys.list", "promotion.journeys.detail"} {
		if !pages[want] {
			t.Errorf("page id %q not found", want)
		}
	}
	if pages["promotion.journeys.archived"] {
		t.Error("unknown page id unexpectedly resolved")
	}
}

func TestWidgetRefsIncludesPromotionWidgets(t *testing.T) {
	widgets := WidgetRefs()
	for _, want := range []string{"widget.table.workforce@1", "widget.timeline@1"} {
		if !widgets[want] {
			t.Errorf("widget ref %q not found", want)
		}
	}
	if widgets["widget.table.workforce@9"] {
		t.Error("unknown widget version unexpectedly resolved")
	}
}

func TestLoadPackageAllowlistIncludesPromotionPackages(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	allowlist, err := LoadPackageAllowlist(root)
	if err != nil {
		t.Fatalf("LoadPackageAllowlist: %v", err)
	}
	for _, want := range []string{
		"github.com/monstercameron/human-capital-management-suite/cmd/hcmnext",
		"github.com/monstercameron/human-capital-management-suite/internal/capability",
	} {
		if !allowlist[want] {
			t.Errorf("package %q not found in the live Phase 1 allowlist", want)
		}
	}
	if allowlist["github.com/monstercameron/human-capital-management-suite/internal/domains/garnishment"] {
		t.Error("a deferred product-line package unexpectedly resolved into the Phase 1 allowlist")
	}
}

func TestLoadTodoIDsIncludesPromotionTodos(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	todos, err := LoadTodoIDs(root)
	if err != nil {
		t.Fatalf("LoadTodoIDs: %v", err)
	}
	for _, want := range []string{"ALIGN-001", "WEB-001", "PROMO-001"} {
		if !todos[want] {
			t.Errorf("todo id %q not found in the live todo registry", want)
		}
	}
	if todos["PROMO-999"] {
		t.Error("unknown todo id unexpectedly resolved")
	}
}

func TestLoadLiveRegistriesAssemblesAllSevenFields(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	reg, err := LoadLiveRegistries(root)
	if err != nil {
		t.Fatalf("LoadLiveRegistries: %v", err)
	}
	if len(reg.BusinessIntents) == 0 {
		t.Error("BusinessIntents is empty")
	}
	if len(reg.Features) == 0 {
		t.Error("Features is empty")
	}
	if len(reg.Pages) == 0 {
		t.Error("Pages is empty")
	}
	if len(reg.Widgets) == 0 {
		t.Error("Widgets is empty")
	}
	if len(reg.Capabilities) == 0 {
		t.Error("Capabilities is empty")
	}
	if len(reg.Packages) == 0 {
		t.Error("Packages is empty")
	}
	if len(reg.Todos) == 0 {
		t.Error("Todos is empty")
	}
}
