// Plan compilation: SCENARIO-006 compiles the deltas of an approved
// Plan into bounded, ordered BusinessIntents.
//
// Every delta must be governed: the spec allowlists compilable keys with
// their write sets and dependencies, names the conflict groups that must
// never combine, and cites the governance authority behind it. An
// unapproved plan, an ungoverned delta, conflicting deltas, a dangling
// dependency and an over-bound compilation all refuse as
// SCENARIO_006_REJECTED. Compilation is pure: no database, no clock, no
// network, no persistence — only the returned intents.
package scenario

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// CompilationRejectedCode is the stable machine-readable refusal code.
const CompilationRejectedCode = "SCENARIO_006_REJECTED"

// ErrCompilationRejected is the sentinel for refused compilations. Match
// it with errors.Is rather than parsing the reason.
var ErrCompilationRejected = errors.New("scenario: plan compilation rejected")

// CompilationRejectedError names the offending field and version.
type CompilationRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *CompilationRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// Is reports ErrCompilationRejected without parsing the reason.
func (e *CompilationRejectedError) Is(target error) bool {
	return target == ErrCompilationRejected
}

// AsCompilationRejected unwraps a SCENARIO_006_REJECTED refusal.
func AsCompilationRejected(err error) (*CompilationRejectedError, bool) {
	var rejected *CompilationRejectedError
	if errors.As(err, &rejected) && rejected.Code == CompilationRejectedCode {
		return rejected, true
	}
	return nil, false
}

func compilationRejected(field, state string) *CompilationRejectedError {
	return &CompilationRejectedError{Code: CompilationRejectedCode, Field: field, State: state, Version: schemaVersion}
}

// CompileSpec is the governance boundary for one compilation. AllowedKeys
// names every delta key the authority permits; WriteTargets declares the
// bounded write set per key; Dependencies declares the per-key ordering;
// ConflictGroups names key sets that must never compile together;
// SimulationRef links every intent to the simulation run behind it and
// MaxIntents bounds the output.
type CompileSpec struct {
	GovernanceRef     string
	GovernanceVersion string
	AllowedKeys       []string
	WriteTargets      map[string][]string
	Dependencies      map[string][]string
	ConflictGroups    [][]string
	SimulationRef     string
	MaxIntents        int
}

// Validate implements validation.
func (s CompileSpec) Validate() error {
	if strings.TrimSpace(s.GovernanceRef) == "" || strings.TrimSpace(s.GovernanceVersion) == "" {
		return fmt.Errorf("%w: governance ref and version are required", ErrInvalidScenario)
	}
	if len(s.AllowedKeys) == 0 {
		return fmt.Errorf("%w: at least one allowed key is required", ErrInvalidScenario)
	}
	allowed := make(map[string]struct{}, len(s.AllowedKeys))
	for _, key := range s.AllowedKeys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: empty allowed key", ErrInvalidScenario)
		}
		if _, dup := allowed[key]; dup {
			return fmt.Errorf("%w: duplicate allowed key %q", ErrInvalidScenario, key)
		}
		allowed[key] = struct{}{}
	}
	for _, key := range s.AllowedKeys {
		targets, ok := s.WriteTargets[key]
		if !ok || len(targets) == 0 {
			return fmt.Errorf("%w: allowed key %q carries no write set", ErrInvalidScenario, key)
		}
		for _, target := range targets {
			if strings.TrimSpace(target) == "" {
				return fmt.Errorf("%w: allowed key %q has a blank write target", ErrInvalidScenario, key)
			}
		}
	}
	for key, deps := range s.Dependencies {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("%w: dependencies for ungoverned key %q", ErrInvalidScenario, key)
		}
		for _, dep := range deps {
			if dep == key {
				return fmt.Errorf("%w: key %q depends on itself", ErrInvalidScenario, key)
			}
			if _, ok := allowed[dep]; !ok {
				return fmt.Errorf("%w: key %q depends on ungoverned %q", ErrInvalidScenario, key, dep)
			}
		}
	}
	for _, group := range s.ConflictGroups {
		for _, key := range group {
			if _, ok := allowed[key]; !ok {
				return fmt.Errorf("%w: conflict group names ungoverned key %q", ErrInvalidScenario, key)
			}
		}
	}
	if strings.TrimSpace(s.SimulationRef) == "" {
		return fmt.Errorf("%w: simulation ref is required", ErrInvalidScenario)
	}
	if s.MaxIntents <= 0 {
		return fmt.Errorf("%w: max intents must be positive", ErrInvalidScenario)
	}
	return nil
}

// CompiledIntent is one bounded, ordered intent: the proposal carries the
// exact assumption value, the write set bounds its effects, DependsOn
// orders it against sibling intents and SimulationLink traces it to the
// simulation run behind the plan.
type CompiledIntent struct {
	Key            string
	Value          TypedValue
	Unit           string
	ProvenanceRefs []string
	Proposal       string
	WriteSet       []string
	DependsOn      []string
	SimulationLink string
}

// Validate implements validation.
func (c CompiledIntent) Validate() error {
	if strings.TrimSpace(c.Key) == "" || strings.TrimSpace(c.Unit) == "" {
		return fmt.Errorf("%w: intent key and unit are required", ErrInvalidScenario)
	}
	if err := c.Value.Validate(); err != nil {
		return err
	}
	if len(c.ProvenanceRefs) == 0 {
		return fmt.Errorf("%w: intent %q carries no provenance", ErrInvalidScenario, c.Key)
	}
	if strings.TrimSpace(c.Proposal) == "" || !strings.Contains(c.Proposal, c.Key) {
		return fmt.Errorf("%w: intent %q carries no proposal for its key", ErrInvalidScenario, c.Key)
	}
	if len(c.WriteSet) == 0 {
		return fmt.Errorf("%w: intent %q carries no write set", ErrInvalidScenario, c.Key)
	}
	for _, target := range c.WriteSet {
		if strings.TrimSpace(target) == "" {
			return fmt.Errorf("%w: intent %q has a blank write target", ErrInvalidScenario, c.Key)
		}
	}
	for _, dep := range c.DependsOn {
		if strings.TrimSpace(dep) == "" {
			return fmt.Errorf("%w: intent %q has a blank dependency", ErrInvalidScenario, c.Key)
		}
		if dep == c.Key {
			return fmt.Errorf("%w: intent %q depends on itself", ErrInvalidScenario, c.Key)
		}
	}
	if strings.TrimSpace(c.SimulationLink) == "" {
		return fmt.Errorf("%w: intent %q carries no simulation link", ErrInvalidScenario, c.Key)
	}
	return nil
}

func (c CompiledIntent) body() []byte {
	provenance := append([]string(nil), c.ProvenanceRefs...)
	sort.Strings(provenance)
	depends := append([]string(nil), c.DependsOn...)
	sort.Strings(depends)
	w := canonicalbytes.New("hcmnext.domains.scenario.CompiledIntent", schemaVersion).
		String("key", c.Key).Value("value", c.Value).String("unit", c.Unit).
		SortedStrings("provenance_ref", provenance).String("proposal", c.Proposal).
		SortedStrings("write_target", append([]string(nil), c.WriteSet...)).
		SortedStrings("depends_on", depends).String("simulation_link", c.SimulationLink)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Canonical implements canonicalbytes.Canonicalizer.
func (c CompiledIntent) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	return c.body()
}

// IntentCompilation is the bound result of compiling one approved Plan.
type IntentCompilation struct {
	ScenarioID        string
	Revision          uint64
	RevisionDigest    string
	GovernanceRef     string
	GovernanceVersion string
	Intents           []CompiledIntent
	SimulationRef     string
	CanonicalDigest   string
}

// Validate implements validation.
func (c IntentCompilation) Validate() error {
	if strings.TrimSpace(c.ScenarioID) == "" || c.Revision == 0 {
		return fmt.Errorf("%w: scenario id and non-zero revision are required", ErrInvalidScenario)
	}
	if strings.TrimSpace(c.RevisionDigest) == "" {
		return fmt.Errorf("%w: revision digest is required", ErrInvalidScenario)
	}
	if strings.TrimSpace(c.GovernanceRef) == "" || strings.TrimSpace(c.GovernanceVersion) == "" {
		return fmt.Errorf("%w: governance ref and version are required", ErrInvalidScenario)
	}
	if strings.TrimSpace(c.SimulationRef) == "" {
		return fmt.Errorf("%w: simulation ref is required", ErrInvalidScenario)
	}
	if len(c.Intents) == 0 {
		return fmt.Errorf("%w: at least one intent is required", ErrInvalidScenario)
	}
	keys := make(map[string]struct{}, len(c.Intents))
	previous := ""
	for i, intent := range c.Intents {
		if err := intent.Validate(); err != nil {
			return fmt.Errorf("intent %d: %w", i, err)
		}
		if _, dup := keys[intent.Key]; dup {
			return fmt.Errorf("%w: duplicate intent key %q", ErrInvalidScenario, intent.Key)
		}
		keys[intent.Key] = struct{}{}
		if intent.Key < previous {
			return fmt.Errorf("%w: intents are not ordered by key", ErrInvalidScenario)
		}
		previous = intent.Key
	}
	for _, intent := range c.Intents {
		for _, dep := range intent.DependsOn {
			if _, ok := keys[dep]; !ok {
				return fmt.Errorf("%w: intent %q depends on uncompiled %q", ErrInvalidScenario, intent.Key, dep)
			}
		}
		if intent.SimulationLink != c.SimulationRef+"#"+intent.Key {
			return fmt.Errorf("%w: intent %q has a foreign simulation link", ErrInvalidScenario, intent.Key)
		}
	}
	if c.CanonicalDigest != "" && c.CanonicalDigest != c.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidScenario)
	}
	return nil
}

func (c IntentCompilation) body() []byte {
	intents := append([]CompiledIntent(nil), c.Intents...)
	sort.Slice(intents, func(i, j int) bool { return intents[i].Key < intents[j].Key })
	w := canonicalbytes.New("hcmnext.domains.scenario.IntentCompilation", schemaVersion).
		String("scenario_id", c.ScenarioID).Int("revision", int64(c.Revision)).
		String("revision_digest", c.RevisionDigest).String("governance_ref", c.GovernanceRef).
		String("governance_version", c.GovernanceVersion).String("simulation_ref", c.SimulationRef).
		Count("intents", len(intents))
	for _, intent := range intents {
		w.Value("intent", intent)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (c IntentCompilation) computedDigest() string {
	b := c.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Canonical returns the canonical encoding of a valid compilation.
func (c IntentCompilation) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	return c.body()
}

// Digest returns the stable digest of a valid compilation.
func (c IntentCompilation) Digest() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	return c.computedDigest(), nil
}

// Explain returns a stable summary for audit and presentation consumers.
func (c IntentCompilation) Explain() string {
	return fmt.Sprintf("intent compilation: scenario %s revision %d intents %d governance %s@%s simulation %s digest %s",
		c.ScenarioID, c.Revision, len(c.Intents), c.GovernanceRef, c.GovernanceVersion, c.SimulationRef, c.CanonicalDigest)
}

// proposalText renders the exact assumption behind one intent.
func proposalText(key string, value TypedValue, unit string) string {
	switch value.Kind {
	case ValueDecimal:
		return fmt.Sprintf("set %s to %s %s", key, value.Number.String(), unit)
	case ValueBoolean:
		return fmt.Sprintf("set %s to %t %s", key, value.Boolean, unit)
	default:
		return fmt.Sprintf("set %s to %s %s", key, value.Text, unit)
	}
}

// CompilePlan compiles the deltas of an approved Plan into bounded,
// ordered intents. Only APPROVED plans compile; every delta key must be
// governed by the spec; conflicting deltas never combine; dependencies
// must close over the compiled set; and the output never exceeds
// MaxIntents. It is pure: nothing is persisted and no intent executes.
func CompilePlan(plan Plan, spec CompileSpec) (IntentCompilation, error) {
	if err := plan.Validate(); err != nil {
		return IntentCompilation{}, compilationRejected("plan", "invalid")
	}
	if plan.Decision != PlanApproved {
		return IntentCompilation{}, compilationRejected("plan", "not-approved")
	}
	if err := spec.Validate(); err != nil {
		return IntentCompilation{}, compilationRejected("spec", "invalid")
	}
	allowed := make(map[string]struct{}, len(spec.AllowedKeys))
	for _, key := range spec.AllowedKeys {
		allowed[key] = struct{}{}
	}
	present := make(map[string]Assumption, len(plan.Assumptions))
	for _, assumption := range plan.Assumptions {
		if _, ok := allowed[assumption.Key]; !ok {
			return IntentCompilation{}, compilationRejected("deltas", "ungoverned-key")
		}
		present[assumption.Key] = assumption
	}
	for _, group := range spec.ConflictGroups {
		found := 0
		for _, key := range group {
			if _, ok := present[key]; ok {
				found++
			}
		}
		if found > 1 {
			return IntentCompilation{}, compilationRejected("deltas", "conflicting-deltas")
		}
	}
	if len(present) > spec.MaxIntents {
		return IntentCompilation{}, compilationRejected("deltas", "over-bound")
	}
	keys := make([]string, 0, len(present))
	for key := range present {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	compiled := IntentCompilation{
		ScenarioID: plan.ScenarioID, Revision: plan.Revision, RevisionDigest: plan.CanonicalDigest,
		GovernanceRef: spec.GovernanceRef, GovernanceVersion: spec.GovernanceVersion,
		SimulationRef: spec.SimulationRef,
	}
	for _, key := range keys {
		for _, dep := range spec.Dependencies[key] {
			if _, ok := present[dep]; !ok {
				return IntentCompilation{}, compilationRejected("deltas", "unknown-dependency")
			}
		}
		assumption := present[key]
		intent := CompiledIntent{
			Key: assumption.Key, Value: assumption.Value, Unit: assumption.Unit,
			ProvenanceRefs: append([]string(nil), assumption.ProvenanceRefs...),
			Proposal:       proposalText(assumption.Key, assumption.Value, assumption.Unit),
			WriteSet:       append([]string(nil), spec.WriteTargets[key]...),
			DependsOn:      append([]string(nil), spec.Dependencies[key]...),
			SimulationLink: spec.SimulationRef + "#" + key,
		}
		sort.Strings(intent.DependsOn)
		compiled.Intents = append(compiled.Intents, intent)
	}
	compiled.CanonicalDigest = compiled.computedDigest()
	if err := compiled.Validate(); err != nil {
		return IntentCompilation{}, compilationRejected("compilation", "invalid")
	}
	return compiled, nil
}
