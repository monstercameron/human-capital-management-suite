package chat

import "context"

// CHATBUG-014: a history read decides, for this reader, whether each source an
// agent answer cites may be opened, and it asked the document hub again for
// every citation of every answer on the page. One page of #general on the
// review data took one to two seconds to read for that reason alone: the same
// few documents are cited by many answers.
//
// Within one read, for one reader in one conversation, the same source has the
// same answer, so it is asked once. Nothing is kept between reads: the rule
// that a stored flag is never permission, and that access is resolved again on
// every read, is unchanged.

// agentSourceMemo answers repeated questions about the same source once, for
// the life of one read.
type agentSourceMemo struct {
	access AgentSourceAccess
	seen   map[agentSourceQuestion]agentSourceAnswer
}

// agentSourceQuestion is what an answer depends on. The reader is named by
// tenant and subject: one read is made for one principal, whose roles do not
// change while it runs.
type agentSourceQuestion struct {
	readerTenant, readerSubject string
	tenant, conversation        string
	source                      AgentDocumentSource
}

type agentSourceAnswer struct {
	source AgentDocumentSource
	err    error
}

// newAgentSourceMemo wraps access for one read. A nil access stays nil, which
// the projection treats as "nothing may be opened".
func newAgentSourceMemo(access AgentSourceAccess) AgentSourceAccess {
	if access == nil {
		return nil
	}
	return &agentSourceMemo{access: access, seen: make(map[agentSourceQuestion]agentSourceAnswer)}
}

func (m *agentSourceMemo) ResolveAgentDocumentSource(ctx context.Context, reader Principal, tenant, conversation string, source AgentDocumentSource) (AgentDocumentSource, error) {
	question := agentSourceQuestion{readerTenant: reader.TenantID, readerSubject: reader.SubjectID, tenant: tenant, conversation: conversation, source: source}
	if answer, ok := m.seen[question]; ok {
		return answer.source, answer.err
	}
	resolved, err := m.access.ResolveAgentDocumentSource(ctx, reader, tenant, conversation, source)
	if ctx.Err() == nil {
		// An answer cut short by the caller going away is not an answer about
		// the source, and is not kept.
		m.seen[question] = agentSourceAnswer{source: resolved, err: err}
	}
	return resolved, err
}
