package agentpersonaeval

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidCatalog = errors.New("agentpersonaeval: invalid persona suite catalog")

// StaticPersonaSuiteCatalog is an immutable, validated mapping from a
// published EvalSuiteRef to its case corpus.
type StaticPersonaSuiteCatalog struct{ suites map[string][]TaskCase }

// NewStaticPersonaSuiteCatalog copies and validates each versioned suite.
func NewStaticPersonaSuiteCatalog(suites map[string][]TaskCase) (*StaticPersonaSuiteCatalog, error) {
	if len(suites) == 0 {
		return nil, ErrInvalidCatalog
	}
	copyOf := make(map[string][]TaskCase, len(suites))
	for ref, cases := range suites {
		if strings.TrimSpace(ref) == "" || ref != strings.TrimSpace(ref) {
			return nil, fmt.Errorf("%w: suite reference is invalid", ErrInvalidCatalog)
		}
		if err := validateCatalogCases(cases); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalidCatalog, ref, err)
		}
		copyOf[ref] = cloneTaskCases(sortedCases(cases))
	}
	return &StaticPersonaSuiteCatalog{suites: copyOf}, nil
}

// ResolvePersonaSuite returns a defensive copy of the exact catalog entry.
func (c *StaticPersonaSuiteCatalog) ResolvePersonaSuite(ref string) ([]TaskCase, error) {
	if c == nil || c.suites == nil {
		return nil, ErrInvalidCatalog
	}
	cases, ok := c.suites[ref]
	if !ok {
		return nil, fmt.Errorf("%w: unknown suite reference", ErrInvalidCatalog)
	}
	return cloneTaskCases(cases), nil
}

func validateCatalogCases(cases []TaskCase) error {
	if len(cases) == 0 {
		return fmt.Errorf("empty suite")
	}
	persona := PersonaVersion{PersonaID: "catalog-validation", Version: "1", PersonaDigest: "sha256:" + strings.Repeat("a", 64), Model: "catalog-validation", ModelDigest: "sha256:" + strings.Repeat("b", 64), EvalSuiteRef: "catalog-validation", Thresholds: Thresholds{MaximumAudienceLeaks: 0, MaximumPlanChanges: 0}}
	seen := make(map[string]struct{}, len(cases))
	for _, testCase := range cases {
		if testCase.ExpectedSkill != "" {
			persona.ExpectedSkills = appendUnique(persona.ExpectedSkills, testCase.ExpectedSkill)
		}
		if _, exists := seen[testCase.ID]; exists {
			return fmt.Errorf("duplicate case %q", testCase.ID)
		}
		seen[testCase.ID] = struct{}{}
	}
	if err := validatePersona(persona); err != nil {
		return err
	}
	kinds := make(map[CaseKind]bool, 5)
	for _, testCase := range cases {
		if err := validateCase(persona, testCase); err != nil {
			return err
		}
		kinds[testCase.Kind] = true
	}
	for _, kind := range []CaseKind{CaseInScope, CaseOutOfScope, CaseAuthority, CaseMixedAudience, CasePeerInjection} {
		if !kinds[kind] {
			return fmt.Errorf("required case kind %q is missing", kind)
		}
	}
	return nil
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}
