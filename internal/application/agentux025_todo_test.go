package application

import "testing"

// The four tests below are the named tests of AGENTUX-025. Each runs the
// existing proofs of the mention path that cover its clause, under one name, so
// the composed path is checked wherever the todo's tests are run. What they do
// not cover is said in the lane report: there is not yet one regression test
// per hand fix of the 2026-10-01 review cell, and no run of the path under
// separated database roles.

// TestTodo_AGENTUX_025: a mention is carried from the committed post to one
// delivered answer. The post is committed before the run starts, one post
// starts one run however often it is replayed, the served wiring persists the
// run it starts, and with a fake model the served assembly answers a mention in
// a channel and a plain message in the person's own conversation with the
// agent.
func TestTodo_AGENTUX_025(t *testing.T) {
	s15Run(t,
		s15Proof{"the post is committed, then one run starts", TestPersonaChatInvocation_CommitsBeforeStartingOneOnBehalfOfT0Run},
		s15Proof{"a replayed post starts one run", TestPersonaChatInvocation_ReplayStartsOnlyOneRun},
		s15Proof{"the served wiring commits the post and persists the run", TestTodo_AGENTP_008_ServeWiringCommitsPostThenPersistsPersonaRun},
		s15Proof{"the served ports resolve the bound agent", TestTodo_AGENTP_008_ServedPortsCommitWithTrustedTenantAndResolveBoundPersona},
		s15Proof{"the served assembly answers a mention and a direct question", TestTodo_AGENTUX_075_Integration},
		s15Proof{"a direct conversation needs no typed mention", TestAgentUXR5Srv_DirectAgentInvocation},
		s15Proof{"the current policy bounds the installed one", TestTodo_AGENTUX_025_EffectivePolicyIntersectsCurrentCeiling},
	)
}

// TestTodo_AGENTUX_025_Integration: on the local demo preparation, over real
// chat and agent stores composed the way the served cell composes them, the
// administrator's mention in the public channel and in the direct conversation
// each get one delivered answer grounded in the placed document and a completed
// task; the answer arrives in the model's time; and a question in the direct
// conversation is stored once.
func TestTodo_AGENTUX_025_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"demo preparation, then a mention in the channel and in the direct conversation", TestTodo_AGENTUX_005_Integration},
		s15Proof{"an instant model answers within the bound", TestTodo_AGENTUX_028_Integration},
		s15Proof{"one stored answer in the direct conversation", TestTodo_AGENTUX_038_Integration},
		s15Proof{"the direct conversation has its policies", TestTodo_AGENTUX_025_PersonaDMProvisionerCreatesAndRepairsPolicies},
	)
}

// TestTodo_AGENTUX_025_Fault: a precondition that is missing is a typed
// refusal, never a silent success and never a lost post. Without a run
// authority, without a served dependency, with an incomplete configuration or
// with no chat the wiring fails closed; a run that fails does not fail the
// committed post; a chat commit that fails starts no run; a failed run is
// stored with a typed cause that carries no content; and a preparation that
// cannot finish leaves nothing half made.
func TestTodo_AGENTUX_025_Fault(t *testing.T) {
	s15Run(t,
		s15Proof{"every authority port is required", TestPersonaChatInvocation_RequiresEveryAuthorityPort},
		s15Proof{"no run authority", TestTodo_AGENTP_008_ServeWiringFailsClosedWithoutRunAuthority},
		s15Proof{"a served dependency is absent", TestTodo_AGENTP_008_ServedPortsFailClosedWhenDependenciesAreAbsent},
		s15Proof{"an incomplete configuration leaves chat unbound", TestTodo_AGENTP_008_ServedRuntimeLeavesChatUnboundOnIncompleteConfig},
		s15Proof{"configured without chat", TestTodo_AGENTP_008_ServeRefusesConfiguredInvocationWithoutChat},
		s15Proof{"the production composition must be complete", TestPersonaRunExecutor_RequiresCompleteProductionComposition},
		s15Proof{"a failed run does not fail the post", TestPersonaChatInvocation_RunFailureDoesNotFailCommittedPost},
		s15Proof{"a failed commit starts no run", TestPersonaChatInvocation_ChatCommitFailureDoesNotStartRun},
		s15Proof{"a failure is typed and carries no cause text", TestPersonaRunExecutor_FailureIsTypedWithoutLeakingCause},
		s15Proof{"a terminal failure is not retried", TestPersonaRunExecutor_TerminalFailuresAreNotRetryable},
		s15Proof{"an interrupted preparation", TestTodo_AGENTUX_005_Fault},
	)
}

// TestTodo_AGENTUX_025_Security: a mention by somebody the agent is not for,
// or of an agent that is not installed in the conversation, starts no run; a
// forged or missing trusted binding is rejected by the served post writer; a
// skill above the read tier is stopped before the runner; a third member in a
// direct conversation is not admitted; the run's human must be the verified
// person; and authority that changes while a run is going (installation,
// revocation, stop, membership, audience) is read again at the next boundary.
func TestTodo_AGENTUX_025_Security(t *testing.T) {
	s15Run(t,
		s15Proof{"uninstalled or unauthorized mention", TestPersonaChatInvocation_UninstalledOrUnauthorizedMentionStartsNoRun},
		s15Proof{"edited and agent-authored posts start nothing", TestPersonaChatInvocation_SkipsEditedAndAgentAuthoredPosts},
		s15Proof{"forged or missing trusted binding", TestTodo_AGENTP_008_ServedPostWriterRejectsForgedOrMissingTrustedBinding},
		s15Proof{"a skill above the read tier", TestPersonaChatInvocation_NonT0SkillIsStoppedBeforeRunner},
		s15Proof{"a third member in a direct conversation", TestAgentUXR5Srv_DirectAgentInvocation_ThreeMemberConversationIsNotAdmitted},
		s15Proof{"the run's human is the verified person", TestPersonaRunExecutor_ChatPrincipalRequiresMatchingHumanContext},
		s15Proof{"authority changed mid-run", TestTodo_AGENTUX_028_Security},
		s15Proof{"the demo preparation's own access", TestTodo_AGENTUX_005_Security},
		s15Proof{"an administrator's narrower policy is kept", TestTodo_AGENTUX_025_PersonaDMProvisionerLeavesAdministratorPolicyAlone},
	)
}
