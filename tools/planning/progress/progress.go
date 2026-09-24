// Package progress reports backlog milestones without treating a checked
// Markdown box as proof that a todo is complete (GOV-015).
//
// The report deliberately keeps authored, implementation, refactor, evidence
// and gate acceptance as separate milestones. A todo is Complete only when it
// is checked, has fresh structured evidence, and explicitly records
// refactoring. Gate acceptance remains independent: P0 and conformance work can
// be complete without pretending that a later release gate accepted it. This
// makes missing evidence visible without conflating implementation completion
// with release authority.
package progress

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/evidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// Stage names the independent milestones in a ProgressReport. Stages are
// intentionally not an enum with a single current value: a checked todo can
// be Green while still lacking Evidence or GateAccepted.
type Stage string

const (
	StageAuthored     Stage = "AUTHORED"
	StageRed          Stage = "RED"
	StageGreen        Stage = "GREEN"
	StageRefactored   Stage = "REFACTORED"
	StageEvidenced    Stage = "EVIDENCED"
	StageGateAccepted Stage = "GATE_ACCEPTED"
	StageComplete     Stage = "COMPLETE"
)

// MaxEvidenceAge is the maximum age accepted by SummarizeMarkdown. Evidence
// is date-granular in planning/todos.md, so comparisons use UTC midnight.
const MaxEvidenceAge = 30 * 24 * time.Hour

// Item is the independently reported state of one todo.
type Item struct {
	ID           string
	Authored     bool
	Red          bool
	Green        bool
	Refactored   bool
	Evidenced    bool
	GateAccepted bool
	Complete     bool
	Issues       []string
}

// Report contains one item per parsed todo and stable milestone counts.
type Report struct {
	Items  []Item
	Counts map[Stage]int
}

var (
	refactoredRe   = regexp.MustCompile(`(?i)\brefactored\b`)
	gateAcceptedRe = regexp.MustCompile(`(?i)\bgate[- ]accepted\b|\bgate\s*:\s*accepted\b`)
)

// SummarizeMarkdown parses a planning Markdown corpus and reports its
// independent progress milestones as of now. The latest Evidence field for a
// todo is the claim evaluated: older evidence remains historical context, but
// cannot make a current claim complete when the latest claim is stale.
func SummarizeMarkdown(markdown string, now time.Time) (Report, error) {
	todos, parseErrs := todoregistry.ParseTodos(markdown)
	if len(parseErrs) != 0 {
		return Report{}, fmt.Errorf("parse planning todos: %v", parseErrs)
	}

	occurrences := make(map[string][]evidence.Occurrence)
	for _, occurrence := range evidence.ScanEvidenceFields(markdown) {
		occurrences[occurrence.ID] = append(occurrences[occurrence.ID], occurrence)
	}

	report := Report{Items: make([]Item, 0, len(todos)), Counts: make(map[Stage]int)}
	for _, todo := range todos {
		item := Item{ID: todo.ID, Authored: true, Green: todo.Done}
		// An unchecked authored item remains in the red/work-needed bucket.
		// A failure marker in a checked item's latest evidence also keeps Red
		// true, while Green remains true to preserve both facts.
		item.Red = !todo.Done

		claims := occurrences[todo.ID]
		if len(claims) == 0 {
			item.Issues = append(item.Issues, "missing evidence")
		} else {
			claim := claims[len(claims)-1]
			for _, violation := range evidence.CheckFreshness(todo.ID, claim.Label, claim.Body) {
				item.Issues = append(item.Issues, violation.Issue)
			}
			if dated, err := time.Parse("2006-01-02", claim.Label); err == nil {
				age := now.UTC().Truncate(24 * time.Hour).Sub(dated.UTC())
				if age < 0 || age > MaxEvidenceAge {
					item.Issues = append(item.Issues, fmt.Sprintf("stale evidence (dated %s)", claim.Label))
				}
			} else {
				// CheckFreshness reports the missing/invalid timestamp; retain
				// that diagnostic and do not manufacture a usable claim.
				item.Issues = append(item.Issues, "evidence timestamp is not a date")
			}

			failedResult := false
			if record, ok := evidence.ParseEvidenceField(todo.ID, claim.Label, claim.Body); ok {
				failedResult = record.Result == "FAIL" || record.Result == "FAILED"
			}
			if failedResult || strings.Contains(strings.ToUpper(claim.Body), "RESULT FAIL") ||
				strings.Contains(strings.ToUpper(claim.Body), "RESULT: FAIL") {
				item.Red = true
			}
			item.Refactored = len(item.Issues) == 0 && !failedResult && refactoredRe.MatchString(claim.Body)
			item.GateAccepted = len(item.Issues) == 0 && !failedResult && gateAcceptedRe.MatchString(claim.Body)
			item.Evidenced = len(item.Issues) == 0
		}

		item.Complete = item.Green && !item.Red && item.Refactored && item.Evidenced
		report.Items = append(report.Items, item)
	}

	for _, item := range report.Items {
		if item.Authored {
			report.Counts[StageAuthored]++
		}
		if item.Red {
			report.Counts[StageRed]++
		}
		if item.Green {
			report.Counts[StageGreen]++
		}
		if item.Refactored {
			report.Counts[StageRefactored]++
		}
		if item.Evidenced {
			report.Counts[StageEvidenced]++
		}
		if item.GateAccepted {
			report.Counts[StageGateAccepted]++
		}
		if item.Complete {
			report.Counts[StageComplete]++
		}
	}

	return report, nil
}

// Item returns the report item for id.
func (r Report) Item(id string) (Item, bool) {
	for _, item := range r.Items {
		if item.ID == id {
			return item, true
		}
	}
	return Item{}, false
}

// Count returns the number of todos at stage. Missing stages count as zero.
func (r Report) Count(stage Stage) int { return r.Counts[stage] }

// String returns a stable, compact golden representation of milestone counts.
func (r Report) String() string {
	stages := []Stage{
		StageAuthored,
		StageRed,
		StageGreen,
		StageRefactored,
		StageEvidenced,
		StageGateAccepted,
		StageComplete,
	}
	parts := make([]string, 0, len(stages))
	for _, stage := range stages {
		parts = append(parts, fmt.Sprintf("%s=%d", stage, r.Count(stage)))
	}
	return strings.Join(parts, " ")
}

// SortedItems returns a copy sorted by stable todo identity for consumers
// that need deterministic JSON/table output independent of source order.
func (r Report) SortedItems() []Item {
	items := append([]Item(nil), r.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
