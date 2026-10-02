package application

import "testing"

// TestTodo_AGENTUX_035 covers the disclosure decision for a document source: an
// answer that cites a document is posted to the channel when every current
// member may read it (the document is placed in that conversation or is
// workspace-wide), and is delivered privately otherwise.
func TestTodo_AGENTUX_035(t *testing.T) {
	s15Run(t,
		s15Proof{"public when every member may read every source", TestAgentUXProactive_PublicRule_Security},
		s15Proof{"a mention answer follows the same rule", TestAgentUXPublicAnswer_Default},
		s15Proof{"sources are bound to the answer", TestAgentUXProactive_CitationBinding_Security},
		s15Proof{"hub authority for every member", TestAgentUXProactive_DocumentAuthority_Integration},
		s15Proof{"public audience authority", TestTodo_AGENTP_012_PublicAuthority},
		s15Proof{"workspace sources", TestAgentUXSearch_PublicAudience_Security_Integration},
	)
}

// TestTodo_AGENTUX_035_Security: a member without read access, a member added
// between evaluation and commit, a document re-classified during the run, a
// guest, and another tenant each keep the answer private.
func TestTodo_AGENTUX_035_Security(t *testing.T) {
	s15Run(t,
		s15Proof{"member without read access and the reasons for staying private", TestTodo_AGENTUX_070},
		s15Proof{"member added after the decision", TestAgentUXProactive_MemberAddedAfterPublicReply_Security_Integration},
		s15Proof{"document changed during the run", TestAgentUXProactive_DocumentChangedDuringModel_Security_Integration},
		s15Proof{"withdrawn placement", TestAgentUXProactive_WithdrawnPlacementRefusesOccurrence_Security},
		s15Proof{"sealed sources and tenant binding", TestAgentUXProactive_SealedPublicSources_Security},
		s15Proof{"requester and tenant binding", TestAgentUXProactive_RequesterAndTenantBinding_Security},
		s15Proof{"the audience floor", TestTodo_AGENTP_012_Security},
		s15Proof{"the public authority", TestTodo_AGENTP_012_PublicAuthority_Security},
	)
}

// TestTodo_AGENTUX_035_Integration runs an answer through a real conversation:
// public by default, and the same answer shared later is checked again at that
// moment.
func TestTodo_AGENTUX_035_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"public answer is one channel message", TestAgentUXPublicAnswer_Default_Integration},
		s15Proof{"share re-checks the audience", TestTodo_AGENTUX_070_ShareSources},
		s15Proof{"the working status of a public answer is public", TestAgentUXPublicAnswer_Default_ProgressNamesThePublicAnswer},
	)
}

// TestTodo_AGENTUX_035_Fault: any doubt or race in the audience check falls to
// private delivery and never to a wider audience.
func TestTodo_AGENTUX_035_Fault(t *testing.T) {
	s15Run(t,
		s15Proof{"the floor fails closed", TestTodo_AGENTP_012_Fault},
		s15Proof{"decision table is never wider than the check", TestAgentUXPublicAnswer_Default_Property},
		s15Proof{"an unreadable source never widens the audience", TestAgentUXPublicAnswer_Default_Security},
	)
}
