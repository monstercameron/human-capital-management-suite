package application

import "github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"

// bindAgentSourceAccess installs the document hub's live access check on the
// Chat service so every reader's projection of an agent answer decides, for
// that reader, whether each source is a link or only a title with an access
// note. Without it the projection fails closed and denies every link, even to
// the agent's owner. It reports whether the binding was made: a composition
// without Chat or without the document hub keeps the fail-closed projection.
func (c composedChat) bindAgentSourceAccess(documents *documenthubstore.Store) bool {
	if c.core == nil || documents == nil {
		return false
	}
	c.core.SetAgentSourceAccess(AgentUXAnswerSourceAccess{Documents: documents})
	// A reader's live view of an answer comes through the served stream, which
	// does not run on the core service: give it the same projection.
	if stream, ok := c.service.(*streamingChatService); ok {
		stream.projector = c.core
	}
	return true
}
