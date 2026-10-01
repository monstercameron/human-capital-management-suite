package agentpersonaeval

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// PersonaEvaluationSuite is the checked-in, versioned YAML suite contract.
type PersonaEvaluationSuite struct {
	ID      string     `yaml:"id"`
	Version int        `yaml:"version"`
	Cases   []TaskCase `yaml:"fixtures"`
}

// ParsePersonaEvaluationSuite decodes a strict AGENTP-021 suite document.
func ParsePersonaEvaluationSuite(data []byte) (PersonaEvaluationSuite, error) {
	var document struct {
		SchemaVersion int                    `yaml:"schema_version"`
		Template      yaml.Node              `yaml:"template"`
		Persona       yaml.Node              `yaml:"persona"`
		Suite         PersonaEvaluationSuite `yaml:"evaluation_suite"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return PersonaEvaluationSuite{}, fmt.Errorf("agentpersonaeval: decode suite: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return PersonaEvaluationSuite{}, fmt.Errorf("agentpersonaeval: suite document must contain one YAML value")
	}
	suite := document.Suite
	if document.SchemaVersion != 1 || strings.TrimSpace(suite.ID) == "" || suite.Version < 1 {
		return PersonaEvaluationSuite{}, fmt.Errorf("%w: suite id and positive version are required", ErrInvalidCatalog)
	}
	for i := range suite.Cases {
		kind := strings.ToLower(string(suite.Cases[i].Kind))
		if kind == "audience" {
			kind = string(CaseMixedAudience)
		}
		suite.Cases[i].Kind = CaseKind(kind)
		if suite.Cases[i].Kind == CaseMixedAudience && suite.Cases[i].ExpectedVisibility == "" {
			return PersonaEvaluationSuite{}, fmt.Errorf("%w: mixed-audience fixture %q needs expected_visibility", ErrInvalidCatalog, suite.Cases[i].ID)
		}
	}
	if err := validateCatalogCases(suite.Cases); err != nil {
		return PersonaEvaluationSuite{}, fmt.Errorf("%w: %s: %v", ErrInvalidCatalog, suite.ID, err)
	}
	suite.Cases = cloneTaskCases(sortedCases(suite.Cases))
	return suite, nil
}
