// Package runtimedecision implements the WF-RUN-000 policy check: the
// durable-workflow-runtime build-or-adopt decision
// (definitions/runtime/durable-runtime-decision.yaml) must be complete
// against planning/specs/workflow-runtime.md's "Build or adopt" contract,
// every non-negotiable must carry a real PASS, FAIL or PARTIAL result whose
// evidence kind says how it was obtained (an executed repository fixture or a
// cited documentation assessment), and every evidence item it claims already
// exists in this repository must actually be present on disk.
package runtimedecision

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Evidence statuses.
const (
	StatusExists  = "EXISTS"
	StatusPending = "PENDING"
)

// Evaluation results against one non-negotiable. UNKNOWN and PENDING are
// not results: a record carrying either has not decided anything.
const (
	ResultPass    = "PASS"
	ResultFail    = "FAIL"
	ResultPartial = "PARTIAL"
)

var validEvaluationResults = map[string]struct{}{
	ResultPass:    {},
	ResultFail:    {},
	ResultPartial: {},
}

// Evidence kinds say how an evaluation result was obtained.
const (
	// EvidenceKindFixture is a result backed by named tests that exist in
	// this repository and were executed.
	EvidenceKindFixture = "FIXTURE"
	// EvidenceKindDocumented is a result assessed from cited public
	// documentation; no fixture was run against the candidate.
	EvidenceKindDocumented = "DOCUMENTED"
)

// Choice kinds.
const (
	ChoiceBuild       = "BUILD"
	choiceAdoptPrefix = "ADOPT:"
	kindBuild         = "BUILD"
	kindAdopt         = "ADOPT"
)

// Re-evaluation kinds.
const (
	// ReevaluationInitial is the first recorded decision.
	ReevaluationInitial = "INITIAL"
	// ReevaluationPreCode is a re-evaluation recorded before the gated
	// code it covers landed.
	ReevaluationPreCode = "PRE_CODE"
	// ReevaluationRetroactive is a re-evaluation recorded after the gated
	// code it covers had already landed; it must say so.
	ReevaluationRetroactive = "RETROACTIVE"
)

// Owner is one accountable owner recorded on the decision.
type Owner struct {
	Name string `yaml:"name"`
	Role string `yaml:"role"`
}

// NonNegotiable is one of the four fixed criteria from
// planning/specs/workflow-runtime.md's "Build or adopt" section.
type NonNegotiable struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
}

// FixtureTest names one Go test function and the repository file that
// declares it.
type FixtureTest struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// Evaluation is one candidate's scored result against one non-negotiable.
type Evaluation struct {
	NonNegotiable string        `yaml:"non_negotiable"`
	Result        string        `yaml:"result"`
	EvidenceKind  string        `yaml:"evidence_kind"`
	Reason        string        `yaml:"reason"`
	Tests         []FixtureTest `yaml:"tests,omitempty"`
	Sources       []string      `yaml:"sources,omitempty"`
	AssessedDate  string        `yaml:"assessed_date,omitempty"`
}

// Evidence is one link in a candidate's evidence trail: either a path that
// must exist in this repository (Status EXISTS), or an explicit forward
// reference to the todo that will produce it (Status PENDING).
type Evidence struct {
	Description string `yaml:"description"`
	Path        string `yaml:"path,omitempty"`
	Test        string `yaml:"test,omitempty"`
	Status      string `yaml:"status"`
	PendingTodo string `yaml:"pending_todo,omitempty"`
}

// Candidate is one runtime option evaluated by the decision record.
type Candidate struct {
	Name            string       `yaml:"name"`
	Kind            string       `yaml:"kind"`
	Module          string       `yaml:"module,omitempty"`
	Version         string       `yaml:"version,omitempty"`
	Selected        bool         `yaml:"selected"`
	Summary         string       `yaml:"summary"`
	Evaluation      []Evaluation `yaml:"evaluation"`
	Evidence        []Evidence   `yaml:"evidence"`
	RejectionReason string       `yaml:"rejection_reason,omitempty"`
}

// SelectedOption is the signed choice the decision record makes.
type SelectedOption struct {
	Candidate  string `yaml:"candidate"`
	Choice     string `yaml:"choice"`
	Rationale  string `yaml:"rationale"`
	SignedBy   string `yaml:"signed_by"`
	SignedDate string `yaml:"signed_date"`
}

// RejectedOption records why one non-selected candidate was not chosen.
type RejectedOption struct {
	Candidate string `yaml:"candidate"`
	Reason    string `yaml:"reason"`
}

// Consequence is one downstream todo's stated impact from the choice made.
type Consequence struct {
	Todo   string `yaml:"todo"`
	Impact string `yaml:"impact"`
}

// ReevaluationTrigger names when and why this decision must be reopened.
type ReevaluationTrigger struct {
	Description string `yaml:"description"`
	GatingTodo  string `yaml:"gating_todo,omitempty"`
	Blocks      string `yaml:"blocks,omitempty"`
}

// Reevaluation is one dated evaluation of the decision. A RETROACTIVE entry
// must state that it was recorded after the gated code it covers landed and
// name that code.
type Reevaluation struct {
	Date              string   `yaml:"date"`
	Kind              string   `yaml:"kind"`
	RecordedAfterCode bool     `yaml:"recorded_after_code"`
	CodeLanded        []string `yaml:"code_landed,omitempty"`
	Statement         string   `yaml:"statement"`
	Outcome           string   `yaml:"outcome"`
}

// Decision is the parsed form of a WF-RUN-000 durable-runtime decision
// record.
type Decision struct {
	SchemaVersion       int                 `yaml:"schema_version"`
	TodoID              string              `yaml:"todo_id"`
	Title               string              `yaml:"title"`
	Status              string              `yaml:"status"`
	DecisionDate        string              `yaml:"decision_date"`
	Owners              []Owner             `yaml:"owners"`
	ReviewBy            string              `yaml:"review_by"`
	Refs                []string            `yaml:"refs,omitempty"`
	NonNegotiables      []NonNegotiable     `yaml:"non_negotiables"`
	Candidates          []Candidate         `yaml:"candidates"`
	NonCandidateNote    string              `yaml:"non_candidate_note,omitempty"`
	SelectedOption      SelectedOption      `yaml:"selected_option"`
	RejectedOptions     []RejectedOption    `yaml:"rejected_options"`
	Consequences        []Consequence       `yaml:"consequences"`
	Reevaluations       []Reevaluation      `yaml:"reevaluations"`
	ReevaluationTrigger ReevaluationTrigger `yaml:"reevaluation_trigger"`
	RollbackPlan        string              `yaml:"rollback_plan"`
}

// Load reads and strictly parses the decision record at path: an
// unrecognized key is a load error, so a field the checker does not enforce
// cannot quietly sit in the record.
func Load(path string) (*Decision, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("runtimedecision: reading %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var d Decision
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("runtimedecision: parsing %s: %w", path, err)
	}
	return &d, nil
}

// Result is the outcome of validating a Decision.
type Result struct {
	OK       bool
	Findings []string
}

// Error renders a non-OK Result as a single error, one finding per line.
func (r Result) Error() error {
	if r.OK {
		return nil
	}
	return fmt.Errorf("runtime decision record is incomplete or unevidenced:\n- %s", strings.Join(r.Findings, "\n- "))
}

// Validate checks d for completeness against the WF-RUN-000 / build-or-adopt
// contract and, for every evidence item or fixture test claiming to already
// exist, that it is actually present under repoRoot. repoRoot is the
// absolute path of the repository root (the directory containing go.mod);
// evidence paths are interpreted relative to it.
func Validate(d *Decision, repoRoot string) Result {
	var findings []string
	add := func(format string, args ...any) {
		findings = append(findings, fmt.Sprintf(format, args...))
	}

	if d.SchemaVersion < 1 {
		add("schema_version must be >= 1, got %d", d.SchemaVersion)
	}
	if strings.TrimSpace(d.TodoID) == "" {
		add("todo_id is required")
	}
	if strings.TrimSpace(d.Status) == "" {
		add("status is required")
	}
	if strings.TrimSpace(d.DecisionDate) == "" {
		add("decision_date is required")
	}
	if strings.TrimSpace(d.ReviewBy) == "" {
		add("review_by is required")
	}
	if strings.TrimSpace(d.RollbackPlan) == "" {
		add("rollback_plan is required")
	}
	if len(d.Owners) == 0 {
		add("at least one owner is required")
	}
	for i, o := range d.Owners {
		if strings.TrimSpace(o.Name) == "" {
			add("owners[%d].name is required", i)
		}
		if strings.TrimSpace(o.Role) == "" {
			add("owners[%d].role is required", i)
		}
	}

	// The build-or-adopt contract fixes exactly four non-negotiables.
	nnByID := make(map[string]NonNegotiable, len(d.NonNegotiables))
	if len(d.NonNegotiables) != 4 {
		add("non_negotiables must list exactly the 4 non-negotiables from workflow-runtime.md's build-or-adopt section, found %d", len(d.NonNegotiables))
	}
	for i, nn := range d.NonNegotiables {
		if strings.TrimSpace(nn.ID) == "" {
			add("non_negotiables[%d].id is required", i)
			continue
		}
		if strings.TrimSpace(nn.Description) == "" {
			add("non_negotiables[%s].description is required", nn.ID)
		}
		if _, dup := nnByID[nn.ID]; dup {
			add("non_negotiables[%s] is duplicated", nn.ID)
		}
		nnByID[nn.ID] = nn
	}

	// RED: "the decision record lacks at least one evaluated candidate".
	if len(d.Candidates) == 0 {
		add("at least one evaluated candidate is required")
	}

	selectedCount := 0
	var selectedCandidate *Candidate
	candidateNames := make(map[string]struct{}, len(d.Candidates))
	for i := range d.Candidates {
		c := &d.Candidates[i]
		label := c.Name
		if label == "" {
			label = fmt.Sprintf("candidates[%d]", i)
		}
		candidateNames[c.Name] = struct{}{}

		if strings.TrimSpace(c.Name) == "" {
			add("candidates[%d].name is required", i)
		}
		if c.Kind != kindBuild && c.Kind != kindAdopt {
			add("candidate %q: kind %q is not BUILD or ADOPT", label, c.Kind)
		}
		if strings.TrimSpace(c.Summary) == "" {
			add("candidate %q: summary is required", label)
		}
		if c.Selected {
			selectedCount++
			selectedCandidate = c
		}

		// RED: "a pass/fail result against each of the four
		// non-negotiables". A candidate carries exactly one evaluation per
		// declared non-negotiable, with a real result, a reason and
		// evidence whose kind matches how it was obtained.
		seen := make(map[string]struct{}, len(c.Evaluation))
		for _, e := range c.Evaluation {
			if strings.TrimSpace(e.NonNegotiable) == "" {
				add("candidate %q: evaluation entry missing non_negotiable id", label)
				continue
			}
			if _, known := nnByID[e.NonNegotiable]; !known {
				add("candidate %q: evaluation references unknown non-negotiable %q", label, e.NonNegotiable)
			}
			if _, dup := seen[e.NonNegotiable]; dup {
				add("candidate %q: evaluation[%s] is duplicated", label, e.NonNegotiable)
			}
			seen[e.NonNegotiable] = struct{}{}
			if _, ok := validEvaluationResults[e.Result]; !ok {
				add("candidate %q: evaluation[%s].result %q is not a decision: every non-negotiable must be PASS, FAIL or PARTIAL (UNKNOWN and PENDING are refused)", label, e.NonNegotiable, e.Result)
			}
			if strings.TrimSpace(e.Reason) == "" {
				add("candidate %q: evaluation[%s] is missing a reason", label, e.NonNegotiable)
			}
			validateEvaluationEvidence(add, repoRoot, label, e)
			if c.Selected && (e.EvidenceKind != EvidenceKindFixture || e.Result != ResultPass) {
				add("candidate %q: selected candidate's evaluation[%s] must be a PASS backed by executed FIXTURE evidence, got %s/%s", label, e.NonNegotiable, e.Result, e.EvidenceKind)
			}
		}
		for id := range nnByID {
			if _, ok := seen[id]; !ok {
				add("candidate %q: missing an evaluation result for non-negotiable %q", label, id)
			}
		}

		// Evidence: every claimed-existing path must exist; every
		// pending item must name the todo that will produce it.
		for j, ev := range c.Evidence {
			switch ev.Status {
			case StatusExists:
				if strings.TrimSpace(ev.Path) == "" {
					add("candidate %q: evidence[%d] status EXISTS requires a path", label, j)
					continue
				}
				full := filepath.Join(repoRoot, filepath.FromSlash(ev.Path))
				if _, err := os.Stat(full); err != nil {
					add("candidate %q: evidence[%d] claims path %q exists but it does not (%v)", label, j, ev.Path, err)
				}
			case StatusPending:
				if strings.TrimSpace(ev.PendingTodo) == "" {
					add("candidate %q: evidence[%d] status PENDING requires pending_todo", label, j)
				}
				if c.Selected {
					add("candidate %q: selected candidate's evidence[%d] is still PENDING", label, j)
				}
			default:
				add("candidate %q: evidence[%d] status %q is not EXISTS or PENDING", label, j, ev.Status)
			}
		}
	}

	if selectedCount == 0 {
		add("exactly one candidate must be marked selected: true, found 0")
	} else if selectedCount > 1 {
		add("exactly one candidate must be marked selected: true, found %d", selectedCount)
	}

	// RED: "or a signed choice".
	validateChoice(add, d.SelectedOption.Choice, selectedCandidate)
	if strings.TrimSpace(d.SelectedOption.Rationale) == "" {
		add("selected_option.rationale is required")
	}
	if strings.TrimSpace(d.SelectedOption.SignedBy) == "" {
		add("selected_option.signed_by is required (an unsigned choice is not a decision)")
	} else if !signedByOwner(d.SelectedOption.SignedBy, d.Owners) {
		add("selected_option.signed_by %q does not name a declared owner", d.SelectedOption.SignedBy)
	}
	if strings.TrimSpace(d.SelectedOption.SignedDate) == "" {
		add("selected_option.signed_date is required")
	}
	if strings.TrimSpace(d.SelectedOption.Candidate) == "" {
		add("selected_option.candidate is required")
	} else if selectedCandidate == nil {
		add("selected_option.candidate %q does not match any candidate marked selected: true", d.SelectedOption.Candidate)
	} else if d.SelectedOption.Candidate != selectedCandidate.Name {
		add("selected_option.candidate %q does not match the selected candidate's name %q", d.SelectedOption.Candidate, selectedCandidate.Name)
	}

	// "rejects a record whose selected option lacks evidence."
	if selectedCandidate != nil && len(selectedCandidate.Evidence) == 0 {
		add("selected candidate %q has no evidence entries", selectedCandidate.Name)
	}

	// "rejected options with reasons": every non-selected candidate should
	// be accounted for in rejected_options with a non-empty reason.
	rejectedByName := make(map[string]RejectedOption, len(d.RejectedOptions))
	for i, r := range d.RejectedOptions {
		if strings.TrimSpace(r.Candidate) == "" {
			add("rejected_options[%d].candidate is required", i)
			continue
		}
		if strings.TrimSpace(r.Reason) == "" {
			add("rejected_options[%s].reason is required", r.Candidate)
		}
		if _, known := candidateNames[r.Candidate]; !known {
			add("rejected_options references unknown candidate %q", r.Candidate)
		}
		rejectedByName[r.Candidate] = r
	}
	for _, c := range d.Candidates {
		if c.Selected {
			continue
		}
		if _, ok := rejectedByName[c.Name]; !ok {
			add("candidate %q is not selected but has no matching rejected_options entry", c.Name)
		}
	}

	if len(d.Consequences) == 0 {
		add("at least one consequence entry is required (impact on the dependent WF-RUN todos)")
	}
	for i, c := range d.Consequences {
		if strings.TrimSpace(c.Todo) == "" {
			add("consequences[%d].todo is required", i)
		}
		if strings.TrimSpace(c.Impact) == "" {
			add("consequences[%s].impact is required", c.Todo)
		}
	}

	validateReevaluations(add, repoRoot, d)

	if strings.TrimSpace(d.ReevaluationTrigger.Description) == "" {
		add("reevaluation_trigger.description is required (names the P1B re-evaluation trigger)")
	}

	sort.Strings(findings)
	return Result{OK: len(findings) == 0, Findings: findings}
}

// validateEvaluationEvidence enforces that an evaluation's evidence kind
// matches what backs it: FIXTURE names tests that exist in the repository,
// DOCUMENTED cites https sources and an assessment date and claims no tests.
func validateEvaluationEvidence(add func(string, ...any), repoRoot, label string, e Evaluation) {
	switch e.EvidenceKind {
	case EvidenceKindFixture:
		if len(e.Tests) == 0 {
			add("candidate %q: evaluation[%s] evidence_kind FIXTURE requires at least one test", label, e.NonNegotiable)
		}
		for _, ft := range e.Tests {
			validateFixtureTest(add, repoRoot, label, e.NonNegotiable, ft)
		}
	case EvidenceKindDocumented:
		if len(e.Tests) != 0 {
			add("candidate %q: evaluation[%s] is DOCUMENTED but names fixture tests; a documentation assessment cannot claim a fixture run", label, e.NonNegotiable)
		}
		if len(e.Sources) == 0 {
			add("candidate %q: evaluation[%s] evidence_kind DOCUMENTED requires at least one cited source", label, e.NonNegotiable)
		}
		for _, s := range e.Sources {
			if !strings.HasPrefix(s, "https://") {
				add("candidate %q: evaluation[%s] source %q is not an https URL", label, e.NonNegotiable, s)
			}
		}
		if strings.TrimSpace(e.AssessedDate) == "" {
			add("candidate %q: evaluation[%s] evidence_kind DOCUMENTED requires assessed_date", label, e.NonNegotiable)
		}
	default:
		add("candidate %q: evaluation[%s].evidence_kind %q is not FIXTURE or DOCUMENTED", label, e.NonNegotiable, e.EvidenceKind)
	}
}

// validateFixtureTest proves a named fixture test is really declared in the
// named file, so a record cannot cite a test that does not exist.
func validateFixtureTest(add func(string, ...any), repoRoot, label, nn string, ft FixtureTest) {
	if !strings.HasPrefix(ft.Name, "Test") {
		add("candidate %q: evaluation[%s] fixture test %q is not a Go test name", label, nn, ft.Name)
		return
	}
	if !strings.HasSuffix(ft.Path, "_test.go") {
		add("candidate %q: evaluation[%s] fixture test %s path %q is not a _test.go file", label, nn, ft.Name, ft.Path)
		return
	}
	src, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(ft.Path)))
	if err != nil {
		add("candidate %q: evaluation[%s] fixture test %s path %q cannot be read (%v)", label, nn, ft.Name, ft.Path, err)
		return
	}
	if !bytes.Contains(src, []byte("func "+ft.Name+"(")) {
		add("candidate %q: evaluation[%s] fixture test %s is not declared in %q", label, nn, ft.Name, ft.Path)
	}
}

// validateChoice checks the choice grammar (BUILD or ADOPT:<module>@<version>)
// against the selected candidate's kind, module and version.
func validateChoice(add func(string, ...any), choice string, selected *Candidate) {
	switch {
	case strings.TrimSpace(choice) == "":
		add("selected_option.choice is required (e.g. BUILD or ADOPT:<module>@<version>)")
	case choice == ChoiceBuild:
		if selected != nil && selected.Kind != kindBuild {
			add("selected_option.choice BUILD does not match the selected candidate's kind %q", selected.Kind)
		}
	case strings.HasPrefix(choice, choiceAdoptPrefix):
		module, version, ok := strings.Cut(strings.TrimPrefix(choice, choiceAdoptPrefix), "@")
		if !ok || module == "" || version == "" {
			add("selected_option.choice %q is not ADOPT:<module>@<version>", choice)
			return
		}
		if selected != nil && (selected.Kind != kindAdopt || selected.Module != module || selected.Version != version) {
			add("selected_option.choice %q does not match the selected candidate (kind %q, module %q, version %q)", choice, selected.Kind, selected.Module, selected.Version)
		}
	default:
		add("selected_option.choice %q is not BUILD or ADOPT:<module>@<version>", choice)
	}
}

// signedByOwner reports whether signer names a declared owner, either
// exactly or followed by a parenthesised role ("workflow-runtime (domain
// owner)").
func signedByOwner(signer string, owners []Owner) bool {
	for _, o := range owners {
		name := strings.TrimSpace(o.Name)
		if name == "" {
			continue
		}
		if signer == name || strings.HasPrefix(signer, name+" (") {
			return true
		}
	}
	return false
}

// validateReevaluations requires a dated history whose latest entry carries
// the signed choice, and a RETROACTIVE entry to admit it was recorded after
// the code it covers and name that code.
func validateReevaluations(add func(string, ...any), repoRoot string, d *Decision) {
	if len(d.Reevaluations) == 0 {
		add("at least one reevaluations entry is required (the dated evaluation history of this decision)")
		return
	}
	latest := -1
	for i, r := range d.Reevaluations {
		if strings.TrimSpace(r.Date) == "" {
			add("reevaluations[%d].date is required", i)
		}
		if strings.TrimSpace(r.Statement) == "" {
			add("reevaluations[%d].statement is required", i)
		}
		if strings.TrimSpace(r.Outcome) == "" {
			add("reevaluations[%d].outcome is required", i)
		}
		switch r.Kind {
		case ReevaluationInitial, ReevaluationPreCode:
			if r.RecordedAfterCode {
				add("reevaluations[%d] is %s but recorded_after_code is true; a re-evaluation recorded after the code landed is RETROACTIVE", i, r.Kind)
			}
		case ReevaluationRetroactive:
			if !r.RecordedAfterCode {
				add("reevaluations[%d] is RETROACTIVE but does not state recorded_after_code: true", i)
			}
			if len(r.CodeLanded) == 0 {
				add("reevaluations[%d] is RETROACTIVE but names no code_landed", i)
			}
		default:
			add("reevaluations[%d].kind %q is not INITIAL, PRE_CODE or RETROACTIVE", i, r.Kind)
		}
		for _, p := range r.CodeLanded {
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(p))); err != nil {
				add("reevaluations[%d] names code_landed %q that does not exist (%v)", i, p, err)
			}
		}
		if latest < 0 || r.Date >= d.Reevaluations[latest].Date {
			latest = i
		}
	}
	last := d.Reevaluations[latest]
	if choice := strings.TrimSpace(d.SelectedOption.Choice); choice != "" && last.Outcome != choice {
		add("the latest reevaluation (%s) outcome %q does not match selected_option.choice %q", last.Date, last.Outcome, choice)
	}
	if signed := strings.TrimSpace(d.SelectedOption.SignedDate); signed != "" && signed != last.Date {
		add("selected_option.signed_date %q is not the latest reevaluation date %q", signed, last.Date)
	}
}

// ValidateFile loads path and validates it in one call.
func ValidateFile(path, repoRoot string) (Result, error) {
	d, err := Load(path)
	if err != nil {
		return Result{}, err
	}
	return Validate(d, repoRoot), nil
}
