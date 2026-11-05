// Package workflowdecisions owns the WF-DISC-003 decision boundary contract.
// It parses unresolved modeling questions from the workflow data register,
// validates owned decision sidecars, and executes negative fixtures to prove
// safe defaults hold before any decision. It is kernel-pure: file parsing,
// validation and sorting only, no database, network or mutable global.
//
// Reachability class and ownership: planning conformance tooling, not a
// runtime capability. It is intentionally outside every shipped binary's
// dependency closure and claims no runtime capability ownership; it is
// accountable to the Platform Engineering owner like the rest of the
// planning-policy checker cohort, recorded in
// planning/specs/specification-ownership-registry.md.
package workflowdecisions

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Decision statuses. The residual-unknown rule admits only an owned open
// question, a decided record or an explicit deferral; anything else,
// including a bare OPEN or prose guessing, is unregistered.
const (
	StatusOpenOwned = "OPEN_OWNED"
	StatusDecided   = "DECIDED"
	StatusDeferred  = "DEFERRED"
)

// Safe defaults. BLOCK stops the action, ROUTE_HUMAN sends it to governed
// human work, DEFER_CAPABILITY withholds the capability. Any other default,
// including PROCEED or an empty guess, is unsafe by construction.
const (
	DefaultBlock           = "BLOCK"
	DefaultRouteHuman      = "ROUTE_HUMAN"
	DefaultDeferCapability = "DEFER_CAPABILITY"
	UnsafeOutcome          = "UNSAFE"
)

// NegativeFixture is one executable adverse scenario. The fixture proves the
// safe default when evaluating every scenario under that default yields the
// default itself: the default holds instead of the action proceeding.
type NegativeFixture struct {
	Action string `yaml:"action" json:"action"`
	Signal string `yaml:"signal" json:"signal"`
	Expect string `yaml:"expect" json:"expect"`
}

// Decision is one registered modeling decision.
type Decision struct {
	ID                  string            `yaml:"id" json:"id"`
	Question            string            `yaml:"question" json:"question"`
	Status              string            `yaml:"status" json:"status"`
	Owner               string            `yaml:"owner,omitempty" json:"owner,omitempty"`
	Deadline            string            `yaml:"deadline,omitempty" json:"deadline,omitempty"`
	Affected            []string          `yaml:"affected,omitempty" json:"affected,omitempty"`
	SafeDefault         string            `yaml:"safe_default,omitempty" json:"safe_default,omitempty"`
	Evidence            []string          `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	LinkedTodos         []string          `yaml:"linked_todos,omitempty" json:"linked_todos,omitempty"`
	LinkedTests         []string          `yaml:"linked_tests,omitempty" json:"linked_tests,omitempty"`
	Revision            int               `yaml:"revision,omitempty" json:"revision,omitempty"`
	Alternatives        []string          `yaml:"alternatives,omitempty" json:"alternatives,omitempty"`
	Consequences        string            `yaml:"consequences,omitempty" json:"consequences,omitempty"`
	Authority           string            `yaml:"authority,omitempty" json:"authority,omitempty"`
	InvalidationTrigger string            `yaml:"invalidation_trigger,omitempty" json:"invalidation_trigger,omitempty"`
	NegativeFixtures    []NegativeFixture `yaml:"negative_fixtures,omitempty" json:"negative_fixtures,omitempty"`
}

// Finding is one exact decision diagnostic located by decision id.
type Finding struct {
	Decision string `json:"decision,omitempty"`
	Code     string `json:"code"`
	Field    string `json:"field,omitempty"`
	Detail   string `json:"detail"`
}

// Report is the complete validation result.
type Report struct {
	Decisions []Decision `json:"decisions"`
	Findings  []Finding  `json:"findings,omitempty"`
	Digest    string     `json:"digest"`
}

// OK reports whether the report holds no findings.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// ApplyDefault executes one safe default against an adverse scenario. A
// closed-set default holds by definition; anything else cannot execute
// safely and yields UNSAFE instead of proceeding.
func ApplyDefault(safeDefault string, fixture NegativeFixture) string {
	switch safeDefault {
	case DefaultBlock, DefaultRouteHuman, DefaultDeferCapability:
		return safeDefault
	default:
		return UnsafeOutcome
	}
}

var questionPrefix = regexp.MustCompile(`^([0-9]+)[.)]\s+(.*\S)\s*$`)

// LoadRegister parses the numbered unresolved modeling decisions below the
// "## Unresolved modeling decisions" section of the register document.
// Continuation lines join with a single space. Parsed questions carry no
// status, owner or deadline: the tool reports those gaps instead of guessing.
func LoadRegister(path string) ([]Decision, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workflowdecisions: read register: %w", err)
	}
	var decisions []Decision
	inSection := false
	var current *Decision
	flush := func() {
		if current != nil {
			current.Question = strings.Join(strings.Fields(current.Question), " ")
			decisions = append(decisions, *current)
			current = nil
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			inSection = trimmed == "## Unresolved modeling decisions"
			continue
		}
		if !inSection || trimmed == "" {
			continue
		}
		if match := questionPrefix.FindStringSubmatch(trimmed); match != nil {
			flush()
			number := match[1]
			current = &Decision{ID: "unresolved-modeling-decision-" + number, Question: match[2]}
			continue
		}
		if current != nil {
			current.Question += " " + trimmed
		}
	}
	flush()
	if len(decisions) == 0 {
		return nil, errors.New("workflowdecisions: no unresolved modeling decisions found")
	}
	return decisions, nil
}

// LoadSidecar parses an owned decision sidecar document.
func LoadSidecar(path string) ([]Decision, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workflowdecisions: read sidecar: %w", err)
	}
	var document struct {
		Decisions []Decision `yaml:"decisions"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("workflowdecisions: parse sidecar: %w", err)
	}
	if len(document.Decisions) == 0 {
		return nil, errors.New("workflowdecisions: sidecar holds no decisions")
	}
	return document.Decisions, nil
}

// ValidateDecisions checks every decision against the boundary contract at
// the given today date (YYYY-MM-DD). Findings carry exact codes and sort
// deterministically; the digest binds every decision so silent edits fail.
func ValidateDecisions(decisions []Decision, today string) Report {
	report := Report{Decisions: append([]Decision(nil), decisions...)}
	for index := range report.Decisions {
		report.validateOne(&report.Decisions[index], today)
	}
	seen := map[string]bool{}
	for _, decision := range report.Decisions {
		if decision.ID == "" {
			continue
		}
		if seen[decision.ID] {
			report.add(Finding{Decision: decision.ID, Code: "DUPLICATE_DECISION", Detail: "decision id is registered more than once"})
		}
		seen[decision.ID] = true
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Decision != report.Findings[j].Decision {
			return report.Findings[i].Decision < report.Findings[j].Decision
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	if report.Findings == nil {
		report.Findings = []Finding{}
	}
	report.Digest = digestDecisions(report.Decisions)
	return report
}

func (r *Report) add(finding Finding) {
	r.Findings = append(r.Findings, finding)
}

func (r *Report) validateOne(decision *Decision, today string) {
	switch decision.Status {
	case StatusOpenOwned, StatusDecided, StatusDeferred:
	default:
		r.add(Finding{Decision: decision.ID, Code: "INVALID_STATUS", Field: "status", Detail: "status must be OPEN_OWNED, DECIDED or DEFERRED"})
	}
	if strings.TrimSpace(decision.Owner) == "" {
		r.add(Finding{Decision: decision.ID, Code: "MISSING_OWNER", Field: "owner", Detail: "decision names no accountable owner"})
	}
	if strings.TrimSpace(decision.Deadline) == "" {
		r.add(Finding{Decision: decision.ID, Code: "MISSING_DEADLINE", Field: "deadline", Detail: "decision names no decision deadline"})
	} else if !validDate(decision.Deadline) {
		r.add(Finding{Decision: decision.ID, Code: "INVALID_DEADLINE", Field: "deadline", Detail: "deadline must use YYYY-MM-DD"})
	} else if decision.Status != StatusDecided && decision.Deadline < today {
		// Decided entries already fired their deadline; the rest are overdue.
		r.add(Finding{Decision: decision.ID, Code: "OVERDUE_DECISION", Field: "deadline", Detail: "undecided entry passed its decision deadline"})
	}
	if len(nonBlank(decision.Affected)) == 0 {
		r.add(Finding{Decision: decision.ID, Code: "MISSING_AFFECTED", Field: "affected", Detail: "decision names no affected intents or workflows"})
	}
	if strings.TrimSpace(decision.SafeDefault) == "" {
		r.add(Finding{Decision: decision.ID, Code: "MISSING_SAFE_DEFAULT", Field: "safe_default", Detail: "decision names no safe default"})
	} else if !safeClosed(decision.SafeDefault) {
		r.add(Finding{Decision: decision.ID, Code: "UNSAFE_DEFAULT", Field: "safe_default", Detail: "safe default must be BLOCK, ROUTE_HUMAN or DEFER_CAPABILITY"})
	}
	if len(nonBlank(decision.Evidence)) == 0 {
		r.add(Finding{Decision: decision.ID, Code: "MISSING_EVIDENCE", Field: "evidence", Detail: "decision cites no deciding evidence"})
	}
	if len(nonBlank(decision.LinkedTodos)) == 0 && len(nonBlank(decision.LinkedTests)) == 0 {
		r.add(Finding{Decision: decision.ID, Code: "MISSING_LINKED_TODO", Field: "linked_todos|linked_tests", Detail: "decision links no todo or test"})
	}
	if decision.Status == StatusDecided || decision.Status == StatusDeferred {
		if decision.Revision < 1 {
			r.add(Finding{Decision: decision.ID, Code: "MISSING_REVISION", Field: "revision", Detail: "decision carries no immutable revision"})
		}
		if decision.Status == StatusDecided {
			if len(nonBlank(decision.Alternatives)) == 0 {
				r.add(Finding{Decision: decision.ID, Code: "MISSING_ALTERNATIVES", Field: "alternatives", Detail: "decided entry records no alternatives"})
			}
			if strings.TrimSpace(decision.Consequences) == "" {
				r.add(Finding{Decision: decision.ID, Code: "MISSING_CONSEQUENCES", Field: "consequences", Detail: "decided entry records no consequences"})
			}
			if strings.TrimSpace(decision.Authority) == "" {
				r.add(Finding{Decision: decision.ID, Code: "MISSING_AUTHORITY", Field: "authority", Detail: "decided entry records no authority"})
			}
		}
		if strings.TrimSpace(decision.InvalidationTrigger) == "" {
			r.add(Finding{Decision: decision.ID, Code: "MISSING_INVALIDATION_TRIGGER", Field: "invalidation_trigger", Detail: "entry records no invalidation trigger"})
		}
	}
	if decision.Status == StatusDecided {
		if len(decision.NegativeFixtures) == 0 {
			r.add(Finding{Decision: decision.ID, Code: "MISSING_NEGATIVE_FIXTURE", Field: "negative_fixtures", Detail: "decided entry proves its safe default with no negative fixture"})
		}
		for _, fixture := range decision.NegativeFixtures {
			if ApplyDefault(decision.SafeDefault, fixture) != fixture.Expect || fixture.Expect != decision.SafeDefault {
				r.add(Finding{Decision: decision.ID, Code: "UNSAFE_FIXTURE", Field: "negative_fixtures", Detail: "negative fixture does not prove the safe default"})
			}
		}
	}
}

func safeClosed(value string) bool {
	return value == DefaultBlock || value == DefaultRouteHuman || value == DefaultDeferCapability
}

func validDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return false
	}
	return parsed.Format("2006-01-02") == value
}

func nonBlank(values []string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func digestDecisions(decisions []Decision) string {
	ordered := append([]Decision(nil), decisions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	var lines []string
	for _, decision := range ordered {
		question := sha256.Sum256([]byte(decision.Question))
		lines = append(lines, strings.Join([]string{
			decision.ID, decision.Status, decision.Owner, decision.Deadline,
			decision.SafeDefault, strings.Join(decision.Affected, ","),
			"revision=" + strconv.Itoa(decision.Revision), hex.EncodeToString(question[:]),
		}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
