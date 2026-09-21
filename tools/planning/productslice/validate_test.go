package productslice

import "testing"

// fixtureRegistries is a small, controlled Registries snapshot -- not the
// live tree -- so Validate's rules can be proved deterministically without
// depending on go list, generated code, or the real coverage/todo files.
func fixtureRegistries() Registries {
	return Registries{
		BusinessIntents: map[string]bool{"hcmnext.people.promote_worker/v1": true},
		Features:        map[string]bool{"promotion_execute": true},
		Pages:           map[string]bool{"promotion.journeys.list": true},
		Widgets:         map[string]bool{"widget.table.workforce@1": true},
		Capabilities:    map[string]bool{"hcmnext.people.promote_worker": true},
		Packages:        map[string]bool{"github.com/monstercameron/human-capital-management-suite/cmd/hcmnext": true},
		Todos:           map[string]bool{"PROMO-001": true},
	}
}

func validFixtureSlice() ProductSliceDefinition {
	return ProductSliceDefinition{
		SliceID:         "promotion",
		Version:         1,
		BusinessIntents: []string{"hcmnext.people.promote_worker/v1"},
		Features:        []string{"promotion_execute"},
		Pages:           []string{"promotion.journeys.list"},
		Widgets:         []string{"widget.table.workforce@1"},
		Capabilities:    []string{"hcmnext.people.promote_worker"},
		Packages:        []string{"github.com/monstercameron/human-capital-management-suite/cmd/hcmnext"},
		Todos:           []string{"PROMO-001"},
		Jurisdictions:   []string{"US-ALL"},
		Personas:        []string{"manager"},
		ExitCriteria:    []string{"a permitted principal discovers the page"},
	}
}

// TestTodo_ALIGN_001 is the primary test: it proves the RED and GREEN
// halves of the ProductSliceDefinition contract in one place.
//
// RED: a slice that is missing a business owner (no BusinessIntents), is
// ownerless (a Capability ref the registry never published), unauthorized
// (a Package outside the live allowlist), stale/unrecoverable (a Todo ref
// that does not exist), or inconsistent (a slice_id/version left implicit)
// is refused -- Validate returns a non-empty, exactly-attributed violation
// list rather than silently accepting the gap.
//
// GREEN: a slice built entirely from real registry ids validates cleanly
// against a Registries snapshot describing exactly those ids: zero
// violations, deterministically, from versioned inputs.
func TestTodo_ALIGN_001(t *testing.T) {
	reg := fixtureRegistries()

	t.Run("GREEN: fully resolved slice validates cleanly", func(t *testing.T) {
		violations := validFixtureSlice().Validate(reg)
		if !Valid(violations) {
			t.Fatalf("expected zero violations, got: %v", ViolationStrings(violations))
		}
	})

	t.Run("RED: missing slice id and version", func(t *testing.T) {
		s := validFixtureSlice()
		s.SliceID = ""
		s.Version = 0
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "slice_id", "")
		assertHasViolation(t, violations, "version", "")
	})

	t.Run("RED: ownerless capability (never published)", func(t *testing.T) {
		s := validFixtureSlice()
		s.Capabilities = []string{"hcmnext.payroll.run_payroll"}
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "capabilities", "hcmnext.payroll.run_payroll")
	})

	t.Run("RED: unauthorized package (outside the Phase 1 allowlist)", func(t *testing.T) {
		s := validFixtureSlice()
		s.Packages = []string{"github.com/monstercameron/human-capital-management-suite/internal/domains/garnishment"}
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "packages", "github.com/monstercameron/human-capital-management-suite/internal/domains/garnishment")
	})

	t.Run("RED: stale/unrecoverable todo proof", func(t *testing.T) {
		s := validFixtureSlice()
		s.Todos = []string{"PROMO-999"}
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "todos", "PROMO-999")
	})

	t.Run("RED: unknown page and widget", func(t *testing.T) {
		s := validFixtureSlice()
		s.Pages = []string{"promotion.journeys.archived"}
		s.Widgets = []string{"widget.table.workforce@9"}
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "pages", "promotion.journeys.archived")
		assertHasViolation(t, violations, "widgets", "widget.table.workforce@9")
	})

	t.Run("RED: unknown business intent and feature", func(t *testing.T) {
		s := validFixtureSlice()
		s.BusinessIntents = []string{"hcmnext.people.fire_worker/v1"}
		s.Features = []string{"unknown_feature"}
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "business_intents", "hcmnext.people.fire_worker/v1")
		assertHasViolation(t, violations, "features", "unknown_feature")
	})

	t.Run("RED: empty required lists", func(t *testing.T) {
		s := validFixtureSlice()
		s.Jurisdictions = nil
		s.Personas = nil
		s.ExitCriteria = nil
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "jurisdictions", "")
		assertHasViolation(t, violations, "personas", "")
		assertHasViolation(t, violations, "exit_criteria", "")
	})

	t.Run("RED: inconsistent (duplicate reference within one field)", func(t *testing.T) {
		s := validFixtureSlice()
		s.Todos = []string{"PROMO-001", "PROMO-001"}
		violations := s.Validate(reg)
		assertHasViolation(t, violations, "todos", "PROMO-001")
		found := 0
		for _, v := range violations {
			if v.Field == "todos" && v.Ref == "PROMO-001" && v.Detail == "duplicate reference" {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("expected exactly one duplicate-reference violation for PROMO-001, got %d", found)
		}
	})
}

// TestTodo_ALIGN_001_Property proves the invariant checkRefs is built on:
// for every one of the seven resolved fields, an unknown reference in that
// field always produces a violation naming exactly that field and that ref
// -- never a different field, never silently dropped, regardless of which
// field it is or what the other six fields contain.
func TestTodo_ALIGN_001_Property(t *testing.T) {
	reg := fixtureRegistries()

	cases := []struct {
		field  string
		mutate func(*ProductSliceDefinition, string)
	}{
		{"business_intents", func(s *ProductSliceDefinition, ref string) { s.BusinessIntents = []string{ref} }},
		{"features", func(s *ProductSliceDefinition, ref string) { s.Features = []string{ref} }},
		{"pages", func(s *ProductSliceDefinition, ref string) { s.Pages = []string{ref} }},
		{"widgets", func(s *ProductSliceDefinition, ref string) { s.Widgets = []string{ref} }},
		{"capabilities", func(s *ProductSliceDefinition, ref string) { s.Capabilities = []string{ref} }},
		{"packages", func(s *ProductSliceDefinition, ref string) { s.Packages = []string{ref} }},
		{"todos", func(s *ProductSliceDefinition, ref string) { s.Todos = []string{ref} }},
	}

	const bogusRef = "does-not-exist-in-any-registry"
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			s := validFixtureSlice()
			c.mutate(&s, bogusRef)
			violations := s.Validate(reg)
			assertHasViolation(t, violations, c.field, bogusRef)
			for _, v := range violations {
				if v.Ref == bogusRef && v.Field != c.field {
					t.Fatalf("bogus ref in field %q was reported against field %q instead", c.field, v.Field)
				}
			}
		})
	}
}

func assertHasViolation(t *testing.T, violations []Violation, field, ref string) {
	t.Helper()
	for _, v := range violations {
		if v.Field == field && (ref == "" || v.Ref == ref) {
			return
		}
	}
	t.Fatalf("expected a violation for field %q ref %q, got: %v", field, ref, ViolationStrings(violations))
}
