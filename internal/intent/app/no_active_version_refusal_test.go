package app

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestNoActiveWorkflowVersionIsItsOwnRefusal proves the engine's
// VERSION_NOT_ACTIVE reaches a caller as a non-retryable precondition naming
// the release it is waiting on, rather than as the retryable
// "execution unavailable" it used to share with a cell that has no driver
// wired at all. The two conditions read the same to a reader and are not: one
// is fixed by an operator releasing a version, the other by composing the
// cell differently, and neither is fixed by retrying.
func TestNoActiveWorkflowVersionIsItsOwnRefusal(t *testing.T) {
	refused := executionError(&runtime.Error{Code: runtime.CodeVersionNotActive, Detail: "no ACTIVE version"})
	if refused == nil {
		t.Fatal("VERSION_NOT_ACTIVE produced no refusal")
	}
	if refused.ReasonRef() != reasonNoActiveWorkflowVersion {
		t.Fatalf("reason = %q, want %q", refused.ReasonRef(), reasonNoActiveWorkflowVersion)
	}
	if refused.Code() != envelope.CodeFailedPrecondition {
		t.Fatalf("code = %s, want FAILED_PRECONDITION", refused.Code())
	}
	if refused.Detail().GetRetryable() {
		t.Fatal("the refusal is marked retryable; retrying never activates a version")
	}
	violations := refused.Detail().GetFieldViolations()
	if len(violations) != 1 || violations[0].GetFieldPath() != "workflow_version" ||
		violations[0].GetRuleRef() != ruleWorkflowVersionRelease {
		t.Fatalf("violations = %+v; the refusal must name the release it waits on", violations)
	}

	// A version that exists but does not resolve is a different fact and keeps
	// the generic execution-unavailable refusal.
	for _, code := range []string{runtime.CodeVersionResolutionFailed, runtime.CodeWorkflowResolutionFailed} {
		other := executionError(&runtime.Error{Code: code, Detail: "unresolved"})
		if other.ReasonRef() != reasonExecutionUnavailable {
			t.Fatalf("%s reason = %q, want %q", code, other.ReasonRef(), reasonExecutionUnavailable)
		}
	}
}

// TestNoActiveWorkflowVersionSurvivesTheJourneyPort proves the journey port
// does not flatten the refusal back into ErrJourneyUnavailable, which is what
// made the page answer UNAVAILABLE and invite a retry. The owned refusal
// travels intact, and the server log names it rather than calling it "failed".
func TestNoActiveWorkflowVersionSurvivesTheJourneyPort(t *testing.T) {
	refused := executionError(&runtime.Error{Code: runtime.CodeVersionNotActive, Detail: "no ACTIVE version"})
	ported := journeyError(refused)
	if ported == nil {
		t.Fatal("journeyError dropped the refusal")
	}
	if errors.Is(ported, workspace.ErrJourneyUnavailable) {
		t.Fatal("the refusal was mapped onto ErrJourneyUnavailable, which projects to a retryable UNAVAILABLE")
	}
	if errors.Is(ported, workspace.ErrJourneyStage) {
		t.Fatal("the refusal was mapped onto ErrJourneyStage; no stage change resolves it")
	}
	owned, ok := envelope.As(ported)
	if !ok || owned.ReasonRef() != reasonNoActiveWorkflowVersion || owned.Code() != envelope.CodeFailedPrecondition {
		t.Fatalf("ported refusal = %+v, %t; want the owned refusal intact", owned, ok)
	}
	if class := journeyErrorClass(ported); class != "no_active_workflow_version" {
		t.Fatalf("log class = %q, want the named condition", class)
	}
	// A cell with no driver at all still classifies as unavailable.
	if class := journeyErrorClass(journeyError(executionUnavailable())); class != "unavailable" {
		t.Fatalf("execution-unavailable log class = %q", class)
	}
}
