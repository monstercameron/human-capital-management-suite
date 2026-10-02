package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatmod002TrustedGuard gives text an agent writes (a public answer, a
// background reply, a scheduled announcement) the same language filters as a
// person's. An announcement or agent answer is stored inside an envelope; the
// filters judge the words a reader will actually see.
func chatmod002TrustedGuard(policy *chat.FilterContentPolicy) chatstore.TrustedContentGuard {
	return func(ctx context.Context, tenant, conversation, author, body string) error {
		return policy.CheckAgentPost(ctx, tenant, conversation, author, chatui.AuthoredReaderText(body))
	}
}
