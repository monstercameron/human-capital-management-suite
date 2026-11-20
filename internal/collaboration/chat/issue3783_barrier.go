package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Issue 3783: information barriers. A workspace can name pairs of groups (teams
// in the people directory) that may never share a conversation. The rules are
// held and decided outside this package; the service asks one port before it
// writes any membership, on every path that makes someone a member.

// ErrInformationBarrier is returned when a membership would put two separated
// groups in one conversation. It is a kind of permission denial, so a caller
// that only knows about denials still refuses the write. It deliberately
// carries no group name and no person: whoever is told "no" is not told who is
// on the other side.
var ErrInformationBarrier = fmt.Errorf("%w: information barrier", ErrPermissionDenied)

// ReasonInformationBarrier is the reason reference a transport reports for
// ErrInformationBarrier, and the one a client matches to explain the refusal.
const ReasonInformationBarrier = "chat.information_barrier"

// MembershipBarrierPort decides whether the joining people may be added to a
// conversation of the tenant. present lists the subjects already in the
// conversation, in this tenant's own directory; it is called only when the
// port needs it, so a workspace with no barriers never pays for the read.
// A nil error admits; ErrInformationBarrier refuses; any other error is a
// failure to decide, and the service refuses on it as well.
type MembershipBarrierPort interface {
	CheckJoin(ctx context.Context, tenant string, joining []string, present func(context.Context) ([]string, error)) error
}

// SetMembershipBarrier installs the barrier port. A nil port leaves a workspace
// with no barriers, as before.
func (s *Service) SetMembershipBarrier(p MembershipBarrierPort) { s.membershipBarrier = p }

// checkMembershipBarrier asks the port about the people who would join. It
// fails closed: an answer that is neither admit nor refuse stops the write.
func (s *Service) checkMembershipBarrier(ctx context.Context, tenant, conversation string, joining []string) error {
	if s.membershipBarrier == nil || len(joining) == 0 {
		return nil
	}
	present := func(ctx context.Context) ([]string, error) {
		if conversation == "" {
			return nil, nil
		}
		return s.activeSubjects(ctx, tenant, conversation)
	}
	err := s.membershipBarrier.CheckJoin(ctx, tenant, joining, present)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrInformationBarrier):
		return ErrInformationBarrier
	default:
		return ErrUnavailable
	}
}

// activeSubjects lists the people of the tenant who are in the conversation
// now, reading every page.
func (s *Service) activeSubjects(ctx context.Context, tenant, conversation string) ([]string, error) {
	var out []string
	cursor := ""
	for pages := 0; pages < 500; pages++ {
		listed, err := s.store.ListMemberships(ctx, tenant, conversation, Page{Cursor: cursor, PageSize: 200})
		if err != nil {
			return nil, err
		}
		for _, m := range listed.Memberships {
			if m.LeftAt == nil && m.HomeTenantID == tenant && strings.TrimSpace(m.SubjectID) != "" {
				out = append(out, m.SubjectID)
			}
		}
		if listed.NextCursor == "" {
			return out, nil
		}
		cursor = listed.NextCursor
	}
	// A roster too long to read is not decided in part.
	return nil, ErrUnavailable
}

// homeSubjects are the subjects among the memberships that belong to the
// tenant's own directory, which is the only directory a barrier reads.
func homeSubjects(tenant string, ms ...Membership) []string {
	var out []string
	for _, m := range ms {
		if m.HomeTenantID == tenant && strings.TrimSpace(m.SubjectID) != "" {
			out = append(out, m.SubjectID)
		}
	}
	return out
}
