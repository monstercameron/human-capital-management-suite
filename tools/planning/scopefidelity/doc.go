// Package scopefidelity audits backlog-governance fidelity for the section 81
// review rounds R001: scope-disposition compliance (REV-001-01), ticked items
// whose own evidence admits a non-green live run (REV-001-02), and CI wiring
// of the live-backlog governance gate (REV-001-03).
//
// The checkers are kernel-pure: they judge caller-supplied fixtures and
// file text, never the live backlog, so their tests stay deterministic while
// the live findings they model remain owned by the orchestrator's
// planning/definitions edits.
package scopefidelity

import (
	"sort"
	"strings"
)

// Finding is one deterministic audit finding.
type Finding struct {
	TodoID  string
	Code    string
	Message string
}

func sortFindings(out []Finding) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].TodoID != out[j].TodoID {
			return out[i].TodoID < out[j].TodoID
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Message < out[j].Message
	})
}

// RenderFindings renders findings deterministically as one
// "TodoID|Code|Message" line per finding, sorted by (TodoID, Code,
// Message), so golden files pin exact bytes.
func RenderFindings(findings []Finding) string {
	sorted := append([]Finding(nil), findings...)
	sortFindings(sorted)
	var b strings.Builder
	for _, f := range sorted {
		b.WriteString(f.TodoID)
		b.WriteString("|")
		b.WriteString(f.Code)
		b.WriteString("|")
		b.WriteString(f.Message)
		b.WriteString("\n")
	}
	return b.String()
}
