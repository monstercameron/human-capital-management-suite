// Ticked-todo closure (REV-103-02): every ticked (Done) todo must name
// TEST and TEST MATRIX functions that actually exist in the repository's
// *_test.go sources, must carry an Evidence line, and that line must name
// the command that ran them. A tick is only evidence when its named tests
// exist and its evidence names the command.
//
// Retired todos and reviewed variance already recorded in tsProvenTodos
// (TypeScript-proven suites and legacy evidence gaps) are out of scope:
// the former are withdrawn, the latter are tracked for re-proof, not
// re-flagged. Fix a finding by writing the missing test, recording an
// applicability reason, or unticking the todo.
package traceability

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// Ticked finding kinds reported by CheckTickedTodos.
const (
	// TickedMissingTest marks a ticked todo whose TEST name has no
	// Test/Fuzz/Benchmark function in the repository.
	TickedMissingTest = "MISSING_TEST"
	// TickedMissingMatrixTest marks a ticked todo whose TEST MATRIX entry
	// has no Test/Fuzz/Benchmark function in the repository.
	TickedMissingMatrixTest = "MISSING_MATRIX_TEST"
	// TickedMissingEvidence marks a ticked todo with no Evidence field.
	TickedMissingEvidence = "MISSING_EVIDENCE"
	// TickedMissingCommand marks a ticked todo whose Evidence field names
	// no go test/vet/run/build command.
	TickedMissingCommand = "MISSING_COMMAND"
)

// TickedFinding names one ticked todo's missing test, evidence or command.
type TickedFinding struct {
	ID     string
	Kind   string
	Detail string
}

func (f TickedFinding) String() string {
	return fmt.Sprintf("%s: %s: %s", f.ID, f.Kind, f.Detail)
}

var (
	// tickedCommandRe is the same command notion the evidence package
	// enforces: a backticked go test/vet/run/build invocation. Sharing the
	// spelling keeps the two checkers from disagreeing about what counts
	// as naming the command that ran the tests.
	tickedCommandRe = regexp.MustCompile("`(go (?:test|vet|run|build)[^`]*)`")
	// tickedTestNameRe admits only real Go test identifiers as matrix
	// candidates. TEST MATRIX also carries UNIT_ONLY applicability reasons
	// (see GOV-018), which are annotations rather than test names and must
	// not be reported as missing functions.
	tickedTestNameRe = nameRe
)

// CheckTickedTodos returns a TickedFinding for every ticked, non-retired,
// non-allow-listed todo whose TEST name has no function, whose TEST MATRIX
// entry has no function, which carries no Evidence field, or whose Evidence
// field names no command. Findings follow input order; matrix entries follow
// sorted class order so map iteration never perturbs the output. A TEST
// MATRIX PRIMARY entry duplicating TEST is reported once, as MISSING_TEST.
func CheckTickedTodos(todos []todoregistry.Todo, existingTests map[string]bool) []TickedFinding {
	var findings []TickedFinding

	for _, td := range todos {
		if !td.Done || td.Retired {
			continue
		}
		if _, ok := tsProvenTodos[td.ID]; ok {
			continue
		}

		if strings.TrimSpace(td.Evidence) == "" {
			findings = append(findings, TickedFinding{
				ID:     td.ID,
				Kind:   TickedMissingEvidence,
				Detail: "completed todo has no Evidence field",
			})
		} else if !tickedCommandRe.MatchString(td.Evidence) {
			findings = append(findings, TickedFinding{
				ID:     td.ID,
				Kind:   TickedMissingCommand,
				Detail: "Evidence field names no go test, go vet, go run or go build command",
			})
		}

		if td.Test != "" && !existingTests[td.Test] {
			findings = append(findings, TickedFinding{
				ID:     td.ID,
				Kind:   TickedMissingTest,
				Detail: fmt.Sprintf("TEST %s names no Test/Fuzz/Benchmark function in the repository", td.Test),
			})
		}

		classes := make([]string, 0, len(td.TestMatrix))
		for class := range td.TestMatrix {
			classes = append(classes, class)
		}
		sort.Strings(classes)
		for _, class := range classes {
			name := td.TestMatrix[class]
			if name == "" || name == td.Test || !tickedTestNameRe.MatchString(name) {
				continue
			}
			if !existingTests[name] {
				findings = append(findings, TickedFinding{
					ID:     td.ID,
					Kind:   TickedMissingMatrixTest,
					Detail: fmt.Sprintf("TEST MATRIX %s=%s names no Test/Fuzz/Benchmark function in the repository", class, name),
				})
			}
		}
	}

	return findings
}

// RenderTickedFindings renders findings in the stable line form pinned by
// the REV-103-02 golden test: one line per finding, LF-terminated.
func RenderTickedFindings(findings []TickedFinding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString(f.String() + "\n")
	}
	return b.String()
}
