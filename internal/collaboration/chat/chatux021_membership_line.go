package chat

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// MembershipAddedMarker opens the body of the system line posted when somebody
// adds a person to a conversation (CHATUX-021). The body is the marker and the
// added person's subject identifier; clients write the sentence ("Walt Brennan
// added Loretta Haynes") from names they already hold, in the reader's
// language. The marker is an invisible separator, so a client that does not
// know it shows an unremarkable line rather than markup.
const MembershipAddedMarker = "⁣member-added:"

// MembershipAddedBody is the post body that records subjectID being added.
func MembershipAddedBody(subjectID string) string {
	return MembershipAddedMarker + strings.TrimSpace(subjectID)
}

// ParseMembershipAdded reports whether body is a membership system line, and
// whom it added.
func ParseMembershipAdded(body string) (subjectID string, ok bool) {
	rest, found := strings.CutPrefix(body, MembershipAddedMarker)
	rest = strings.TrimSpace(rest)
	return rest, found && rest != ""
}

// announceMembershipAdded posts the system line for a person somebody else just
// added. It runs after the membership is stored and never fails the add: the
// line is a courtesy to the people already there, not part of the permission.
// A person who was already an active member (an add that only changed their
// role) is not announced again.
func (s *Service) announceMembershipAdded(ctx context.Context, p Principal, c Conversation, before *Membership, saved Membership) {
	if before != nil && before.JoinedAt != nil && before.LeftAt == nil {
		return
	}
	key := "membership-added:" + c.ID + ":" + saved.SubjectID + ":" + strconv.FormatUint(saved.Revision, 10)
	body := MembershipAddedBody(saved.SubjectID)
	post := Post{ID: uuid.NewString(), ConversationID: c.ID, TenantID: c.TenantID, AuthorID: p.SubjectID, AuthorHomeTenantID: p.TenantID, Body: body, Revision: 1, CreatedAt: s.now()}
	_, _ = s.store.SendPost(ctx, SendPostRequest{Principal: p, TenantID: c.TenantID, ConversationID: c.ID, Body: body, IdempotencyKey: key}, post)
}

// withoutSystemPosts is posts without the system lines. It returns the same
// slice when there is none to drop.
func withoutSystemPosts(posts []Post) []Post {
	system := 0
	for _, p := range posts {
		if _, ok := ParseMembershipAdded(p.Body); ok {
			system++
		}
	}
	if system == 0 {
		return posts
	}
	kept := make([]Post, 0, len(posts)-system)
	for _, p := range posts {
		if _, ok := ParseMembershipAdded(p.Body); !ok {
			kept = append(kept, p)
		}
	}
	return kept
}
