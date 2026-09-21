package vpat

// REV-099-04: accessibility checkpoints claimed conformance no tool ever
// ran. This package generates the VPAT from tools/uxqual/wcag results: a
// checkpoint reports Supports only when its check ran and passed, Does Not
// Support when its check ran and failed, and Not Evaluated when its check
// never ran. Conformance is never claimed without a run behind it.

import (
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// Conformance levels.
const (
	Supports       = "Supports"
	DoesNotSupport = "Does Not Support"
	NotEvaluated   = "Not Evaluated"
)

// Checkpoint is one VPAT row bound to a wcag check by result name.
type Checkpoint struct {
	// Check is the wcag result Name, e.g. "Reduced motion".
	Check string `json:"check"`
	// Criterion cites the WCAG 2.2 success criteria the check covers.
	Criterion string `json:"criterion"`
	// Level is the WCAG conformance level: A, AA or AAA.
	Level string `json:"level"`
}

// Checkpoints is the catalog in criterion order.
func Checkpoints() []Checkpoint {
	return []Checkpoint{
		{Check: "200% zoom and 400% reflow", Criterion: "1.4.4 Resize Text; 1.4.10 Reflow", Level: "AA"},
		{Check: "Accessible authorization projection", Criterion: "3.3.8 Accessible Authentication", Level: "AA"},
		{Check: "Focus order and accessible names", Criterion: "2.4.3 Focus Order; 4.1.2 Name, Role, Value", Level: "A"},
		{Check: "Reduced motion", Criterion: "2.3.3 Animation from Interactions", Level: "AAA"},
	}
}

// Row is one generated VPAT row.
type Row struct {
	Checkpoint
	// Conformance is Supports, Does Not Support or Not Evaluated.
	Conformance string `json:"conformance"`
	// Evidence carries the tool detail behind the verdict.
	Evidence string `json:"evidence"`
}

// Report is the generated VPAT.
type Report struct {
	// Rows covers every checkpoint in catalog order.
	Rows []Row `json:"rows"`
}

// Generate builds the VPAT from tool results. Results are indexed by check
// name; a checkpoint with no result is Not Evaluated, never Supports.
func Generate(results []qual.CriterionResult) Report {
	byName := make(map[string]qual.CriterionResult, len(results))
	for _, result := range results {
		byName[result.Name] = result
	}
	report := Report{}
	for _, checkpoint := range Checkpoints() {
		row := Row{Checkpoint: checkpoint, Conformance: NotEvaluated}
		if result, ok := byName[checkpoint.Check]; ok {
			row.Evidence = result.Detail
			if result.Pass {
				row.Conformance = Supports
			} else {
				row.Conformance = DoesNotSupport
			}
		}
		report.Rows = append(report.Rows, row)
	}
	return report
}

// Summary counts rows per conformance level.
func (r Report) Summary() map[string]int {
	out := map[string]int{Supports: 0, DoesNotSupport: 0, NotEvaluated: 0}
	for _, row := range r.Rows {
		out[row.Conformance]++
	}
	return out
}

// CanonicalReport renders the report one row-per-line for the golden pin.
func CanonicalReport(r Report) string {
	rows := append([]Row(nil), r.Rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Check < rows[j].Check })
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, row.Check+" :: "+row.Criterion+" :: "+row.Level+" :: "+row.Conformance)
	}
	return strings.Join(lines, "\n")
}
