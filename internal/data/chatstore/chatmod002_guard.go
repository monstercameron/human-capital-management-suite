package chatstore

import "context"

// TrustedContentGuard is asked about the text of every server-owned write
// (an agent's public answer, a background reply, a scheduled announcement)
// before it is stored. Those writes carry a trusted author and skip the chat
// service's own content policy, so without this guard an agent's output would
// be the one place a workspace's language filters did not apply (CHATMOD-001).
// A refusal is returned to the caller unchanged and nothing is written.
type TrustedContentGuard func(ctx context.Context, tenant, conversation, author, body string) error

// SetTrustedContentGuard installs the guard. It is set once by the composition
// root before the store serves requests.
func (s *Store) SetTrustedContentGuard(g TrustedContentGuard) { s.trustedGuard = g }

func (s *Store) guardTrustedContent(ctx context.Context, r SendRequest) error {
	if !r.TrustedAuthor || s.trustedGuard == nil {
		return nil
	}
	return s.trustedGuard(ctx, r.TenantID, r.ConversationID, r.AuthorID, r.Body)
}
