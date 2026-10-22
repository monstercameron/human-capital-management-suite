package application

import "testing"

// The three named tests of AGENTUX-052. Each runs the proofs that cover its
// clause, under one name, so the outcome reaction and its switches are checked
// wherever the todo's tests are run. The commit-time gate itself is proved in
// the chat package (TestAgentUX052_ReactionPolicyGatesCommit) and the rows in
// the chat store (TestAgentUX052_ReactionPolicyRoundTrip); the page is proved
// in the chat UI (TestTodo_AGENTUX_052_Browser).

// TestTodo_AGENTUX_052: the reaction that fits the outcome takes the first
// one's place — answered keeps ✅ or carries one contextual emoji from the
// closed set, failed takes ⚠️, blocked takes 🤷, and a stopped run leaves the
// first reaction alone; a switch nobody set stays on, and an answer whose
// visibility cannot be read keeps ✅.
func TestTodo_AGENTUX_052(t *testing.T) {
	s15Run(t,
		s15Proof{"the outcome reaction, from a closed table and only from it", TestTodo_AGENTUX_052_OutcomeReaction},
		s15Proof{"the production chooser picks from the question without a model call", TestAgentUX052_OutcomeChooserTable},
		s15Proof{"a private or unreadable answer keeps ✅", TestAgentUX052_PrivateAnswerKeepsCheck},
		s15Proof{"visibility is read from durable chat", TestAgentUX052_ServedVisibilityReadsDurableChat},
		s15Proof{"with no rows stored, reactions land as before", TestTodo_AGENTUX_075_ReactionIntegration},
		s15Proof{"the stored switches and the owner's switch compose at commit", TestAgentUX052_CommitHonorsStoredSwitches},
	)
}

// TestTodo_AGENTUX_052_Security: no model output, question text or caller can
// put an emoji outside the closed set on a question; a private answer's topic
// cannot leak through its reaction; only a scope's administrator changes its
// switch; and a refused reaction stores nothing.
func TestTodo_AGENTUX_052_Security(t *testing.T) {
	s15Run(t,
		s15Proof{"the closed set holds against model output", TestTodo_AGENTUX_052_OutcomeReaction},
		s15Proof{"the production chooser cannot leave the closed set", TestAgentUX052_OutcomeChooserTable},
		s15Proof{"a private answer keeps ✅", TestAgentUX052_PrivateAnswerKeepsCheck},
		s15Proof{"only an administrator's switch write lands", TestAgentUX052_HTTPSwitches},
		s15Proof{"a refused reaction stores nothing", TestAgentUX052_CommitHonorsStoredSwitches},
	)
}

// TestTodo_AGENTUX_052_Integration: the settings vertical over the real store —
// the pages' reads, an administrator's writes, and the commit-time enforcement
// of what was written.
func TestTodo_AGENTUX_052_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"the pages' reads", TestAgentUX052_PolicyReads},
		s15Proof{"an administrator's writes", TestAgentUX052_SwitchWrites},
		s15Proof{"the switches over HTTP", TestAgentUX052_HTTPSwitches},
		s15Proof{"commits honor the stored switches", TestAgentUX052_CommitHonorsStoredSwitches},
	)
}
