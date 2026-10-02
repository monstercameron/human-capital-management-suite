package application

import "testing"

// TestTodo_AGENTUX_056 covers the read-only skill "Search workspace documents":
// one searcher with a scope (conversation placements or workspace-public), a
// bounded section per hit, and the keyword fallback when no embedding model is
// configured. The meaning search itself runs on the repository's deterministic
// fake embedder; the real embedding model is not downloaded or approved.
func TestTodo_AGENTUX_056(t *testing.T) {
	s15Run(t,
		s15Proof{"bounded sections", TestAgentUXSearch_BoundedSections},
		s15Proof{"keyword fallback is the default", TestAgentUXSearch_KeywordFallback_Integration},
		s15Proof{"index replay", TestAgentUXSearch_IndexReplay_Integration},
	)
}

// TestTodo_AGENTUX_056_Security: a document restricted after indexing is never
// returned or cited, a section the asker cannot read never reaches the model,
// another tenant's vectors are never read and document text cannot change the
// agent's skills or audience.
func TestTodo_AGENTUX_056_Security(t *testing.T) {
	s15Run(t,
		s15Proof{"workspace search", TestAgentUXSearch_Workspace_Security_Integration},
		s15Proof{"public audience", TestAgentUXSearch_PublicAudience_Security_Integration},
		s15Proof{"bounded public section", TestAgentUXSearch_BoundedPublicSection_Security_Integration},
		s15Proof{"unheaded public section", TestAgentUXSearch_UnheadedPublicSection_Security_Integration},
	)
}

// TestTodo_AGENTUX_056_Integration runs the search against the test database
// with the repository's deterministic fake embedder.
func TestTodo_AGENTUX_056_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"keyword fallback", TestAgentUXSearch_KeywordFallback_Integration},
		s15Proof{"index replay", TestAgentUXSearch_IndexReplay_Integration},
		s15Proof{"workspace security", TestAgentUXSearch_Workspace_Security_Integration},
	)
}

// TestTodo_AGENTUX_056_Performance holds a search under the todo's 300 ms bound
// at the cell's corpus size.
func TestTodo_AGENTUX_056_Performance(t *testing.T) {
	TestAgentUXSearch_Corpus_Performance_Integration(t)
}
