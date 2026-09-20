package scopefidelity

import (
	"fmt"
	"strings"
)

// CI gate finding codes for REV-001-03.
const (
	// CodeMissingGateStep flags a workflow that invokes no live-backlog
	// governance binary.
	CodeMissingGateStep = "MISSING_GATE_STEP"
	// CodeMissingAllowlistRef flags a workflow that references no governance
	// allowlist, so every finding would fail the build.
	CodeMissingAllowlistRef = "MISSING_ALLOWLIST_REF"
	// CodeMissingLiveTarget flags a workflow that scans no live backlog
	// target, so the gate would prove only fixture logic.
	CodeMissingLiveTarget = "MISSING_LIVE_TARGET"
)

// GateExpectation declares what a CI live-backlog governance gate must wire:
// at least one governance binary, the allowlist of accepted exceptions, and
// the live backlog target it scans.
type GateExpectation struct {
	Binaries  []string
	Allowlist string
	Target    string
}

// CheckWorkflowGate returns one finding per missing wiring element of the
// live-backlog governance gate in a CI workflow document.
func CheckWorkflowGate(workflow, content string, exp GateExpectation) []Finding {
	var out []Finding
	gated := false
	for _, b := range exp.Binaries {
		if strings.Contains(content, b) {
			gated = true
			break
		}
	}
	if !gated {
		out = append(out, Finding{
			TodoID:  workflow,
			Code:    CodeMissingGateStep,
			Message: fmt.Sprintf("workflow invokes none of the live-backlog governance binaries %q", strings.Join(exp.Binaries, ", ")),
		})
	}
	if exp.Allowlist != "" && !strings.Contains(content, exp.Allowlist) {
		out = append(out, Finding{
			TodoID:  workflow,
			Code:    CodeMissingAllowlistRef,
			Message: fmt.Sprintf("workflow references no governance allowlist %q", exp.Allowlist),
		})
	}
	if exp.Target != "" && !strings.Contains(content, exp.Target) {
		out = append(out, Finding{
			TodoID:  workflow,
			Code:    CodeMissingLiveTarget,
			Message: fmt.Sprintf("workflow scans no live backlog target %q", exp.Target),
		})
	}
	sortFindings(out)
	return out
}
