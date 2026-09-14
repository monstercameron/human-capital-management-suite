package wcag

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
)

// InteractionState describes a rendered state whose meaning must remain
// perceivable after a theme changes. Cue is intentionally textual metadata
// (for example, underline, icon, or an accessible name), not a colour.
type InteractionState struct {
	Name, Foreground, Background string
	MinRatio                     float64
	Cue                          string
	// Essential marks a control boundary or indicator that must meet the
	// WCAG non-text 3:1 floor, even when a caller supplies a weaker request.
	Essential bool
}

// StatusSemantic describes the non-colour evidence exposed for a status.
// Label is the visible/accessible status text; Cue is a shape, icon, pattern,
// or other redundant indicator.
type StatusSemantic struct {
	Name, Label, Cue string
}

// StateResult records qualification of one interaction state.
type StateResult struct {
	State InteractionState
	Ratio float64
	Pass  bool
	Error string
}

// QualifyInteractionStates checks contrast and rejects states that provide no
// redundant cue. This keeps selected, hover, focus, disabled and visited
// meanings from being carried by colour alone.
func QualifyInteractionStates(states []InteractionState) []StateResult {
	if len(states) == 0 {
		return []StateResult{{State: InteractionState{Name: "(none)"}, Error: "at least one interaction state is required"}}
	}
	results := make([]StateResult, 0, len(states))
	for _, state := range states {
		minimum := state.MinRatio
		if minimum <= 0 {
			minimum = tokens.MinRatioNormalText
			if state.Essential {
				minimum = tokens.MinRatioLargeText
			}
		}
		ratio, err := tokens.ContrastRatio(state.Foreground, state.Background)
		result := StateResult{State: state, Ratio: ratio, Pass: err == nil && ratio >= minimum && strings.TrimSpace(state.Cue) != ""}
		switch {
		case state.Essential && state.MinRatio > 0 && state.MinRatio < tokens.MinRatioLargeText:
			result.Pass = false
			result.Error = fmt.Sprintf("essential state floor cannot be below %.1f:1", tokens.MinRatioLargeText)
		case err != nil:
			result.Error = err.Error()
		case ratio < minimum:
			result.Error = fmt.Sprintf("%.2f:1 is below %.1f:1", ratio, minimum)
		case strings.TrimSpace(state.Cue) == "":
			result.Error = "interaction meaning requires a non-colour cue"
		}
		results = append(results, result)
	}
	return results
}

// QualifyStatusSemantics returns an error for every status that is empty or
// colour-only. Callers can surface these errors in preview evidence.
func QualifyStatusSemantics(statuses []StatusSemantic) []error {
	errors := make([]error, 0)
	for _, status := range statuses {
		name := strings.TrimSpace(status.Name)
		switch {
		case name == "":
			errors = append(errors, fmt.Errorf("status name is required"))
		case strings.TrimSpace(status.Label) == "":
			errors = append(errors, fmt.Errorf("status %q requires visible or accessible text", name))
		case strings.TrimSpace(status.Cue) == "":
			errors = append(errors, fmt.Errorf("status %q is conveyed by colour alone", name))
		}
	}
	return errors
}
