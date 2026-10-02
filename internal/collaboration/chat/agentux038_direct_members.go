package chat

import (
	"context"
	"errors"
)

// AGENTUX-038: a direct conversation is between two members. Once it has its
// two, nobody is added to it, by its owner or anybody else: a person's
// conversation with an agent holds that person's private answers, and a second
// person in its audience would read them and could ask the agent on the first
// person's documents.

// directConversationMembers is how many members a direct conversation has.
const directConversationMembers = 2

// refuseNewDirectMember refuses a membership that would give direct
// conversation c a third member. Writing an existing member's row again (a
// repair, or a change of that member's own settings) is not an addition, and a
// conversation still short of its two members can be completed.
func (s *Service) refuseNewDirectMember(ctx context.Context, c Conversation, m Membership) error {
	if c.Kind != Direct {
		return nil
	}
	held, err := s.store.GetMembership(ctx, m.TenantID, m.ConversationID, m.HomeTenantID, m.SubjectID)
	if err == nil && held.SubjectID == m.SubjectID && held.HomeTenantID == m.HomeTenantID && held.ConversationID == m.ConversationID {
		return nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	listed, err := s.store.ListMemberships(ctx, m.TenantID, m.ConversationID, Page{PageSize: 50})
	if err != nil {
		return err
	}
	// More rows than one page holds is not a direct conversation's membership;
	// it is refused rather than counted in part.
	if listed.NextCursor != "" {
		return ErrPermissionDenied
	}
	current := 0
	for _, member := range listed.Memberships {
		if member.LeftAt == nil {
			current++
		}
	}
	if current >= directConversationMembers {
		return ErrPermissionDenied
	}
	return nil
}
