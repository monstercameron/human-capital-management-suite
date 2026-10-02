package application

import "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"

// personaRunRetryable is whether a person may ask again about a finished run. A
// model that was unavailable and an answer that was interrupted both qualify:
// neither reached the person, and asking again is their own act, made with their
// current authority.
func personaRunRetryable(run runstate.Run) bool {
	return run.State == runstate.StateFailed && (run.TerminalCode == "MODEL_UNAVAILABLE" || run.TerminalCode == runstate.InterruptedCode)
}
