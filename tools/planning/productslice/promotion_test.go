package productslice

import "testing"

func TestPromotionSliceDefinitionShape(t *testing.T) {
	s := PromotionSliceDefinition()

	if s.SliceID != "promotion" {
		t.Errorf("SliceID = %q, want %q", s.SliceID, "promotion")
	}
	if s.Version < 1 {
		t.Errorf("Version = %d, want >= 1", s.Version)
	}
	for name, list := range map[string][]string{
		"BusinessIntents": s.BusinessIntents,
		"Features":        s.Features,
		"Pages":           s.Pages,
		"Widgets":         s.Widgets,
		"Capabilities":    s.Capabilities,
		"Packages":        s.Packages,
		"Todos":           s.Todos,
		"Jurisdictions":   s.Jurisdictions,
		"Personas":        s.Personas,
		"ExitCriteria":    s.ExitCriteria,
	} {
		if len(list) == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestPromotionSliceDefinitionWidgetsMatchLiveRegistry(t *testing.T) {
	s := PromotionSliceDefinition()
	live := WidgetRefs()
	for _, ref := range s.Widgets {
		if !live[ref] {
			t.Errorf("PromotionSliceDefinition names widget %q, which is not in the live widget registry", ref)
		}
	}
	if len(s.Widgets) != len(live) {
		t.Errorf("PromotionSliceDefinition names %d widgets, live registry publishes %d", len(s.Widgets), len(live))
	}
}

func TestPromotionSliceDefinitionPagesMatchLiveRegistry(t *testing.T) {
	s := PromotionSliceDefinition()
	live := PageIDs()
	for _, ref := range s.Pages {
		if !live[ref] {
			t.Errorf("PromotionSliceDefinition names page %q, which is not in the live page registry", ref)
		}
	}
}

func TestPromotionSliceDefinitionIsDeterministic(t *testing.T) {
	a := PromotionSliceDefinition()
	b := PromotionSliceDefinition()
	if a.Digest() != b.Digest() {
		t.Fatalf("PromotionSliceDefinition is not deterministic: %s vs %s", a.Digest(), b.Digest())
	}
}

// TestTodo_ALIGN_001_Security proves the definition cannot smuggle
// authority: a slice that claims a package outside the live Phase 1
// allowlist, or a capability id the BOOTSTRAP registry never published, is
// refused by name -- not silently accepted because the rest of the slice
// looks legitimate. This is the same "unauthorized" half of the RED
// condition TestTodo_ALIGN_001 covers with a fixture registry, proved here
// against the real Promotion slice and the real live registries so a
// reviewer sees it fail against the actual admitted authority boundary.
func TestTodo_ALIGN_001_Security(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	reg, err := LoadLiveRegistries(root)
	if err != nil {
		t.Fatalf("LoadLiveRegistries: %v", err)
	}

	t.Run("package outside the live Phase 1 allowlist is refused", func(t *testing.T) {
		s := PromotionSliceDefinition()
		s.Packages = append(s.Packages, "github.com/monstercameron/human-capital-management-suite/internal/domains/garnishment")
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "packages", "github.com/monstercameron/human-capital-management-suite/internal/domains/garnishment")
	})

	t.Run("capability never published by the BOOTSTRAP registry is refused", func(t *testing.T) {
		s := PromotionSliceDefinition()
		s.Capabilities = append(s.Capabilities, "hcmnext.payroll.run_payroll")
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "capabilities", "hcmnext.payroll.run_payroll")
	})

	t.Run("todo that does not exist cannot stand as proof", func(t *testing.T) {
		s := PromotionSliceDefinition()
		s.Todos = append(s.Todos, "PROMO-999")
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "todos", "PROMO-999")
	})

	t.Run("the real Promotion slice itself carries none of these gaps", func(t *testing.T) {
		violations := PromotionSliceDefinition().Validate(reg)
		if !Valid(violations) {
			t.Fatalf("PromotionSliceDefinition() failed live validation: %v", ViolationStrings(violations))
		}
	})
}
