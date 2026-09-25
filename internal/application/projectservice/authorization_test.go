package projectservice

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
)

type membershipPolicyStub struct{ calls int }

func (m *membershipPolicyStub) Authorize(context.Context, string, string, string, projectaccess.Capability) error {
	m.calls++
	return nil
}
func (m *membershipPolicyStub) AuthorizeList(context.Context, string, string) error {
	m.calls++
	return nil
}

func TestMembershipAuthorizerRechecksCurrentEmployeeStanding(t *testing.T) {
	policy := &membershipPolicyStub{}
	auth := MembershipAuthorizer{Membership: policy, Standing: &inviteeEligibilityStub{err: projectaccess.ErrMemberNotFound}}
	err := auth.Authorize(context.Background(), testPrincipal(t), "project-a", projectaccess.ReadProject)
	if !errors.Is(err, projectaccess.ErrMemberNotFound) {
		t.Fatalf("Authorize error=%v, want current employee denial", err)
	}
	if policy.calls != 0 {
		t.Fatalf("membership lookup ran for a terminated/nonemployee principal: %d", policy.calls)
	}

	standing := &inviteeEligibilityStub{class: workforceInviteeBaselineClass}
	auth.Standing = standing
	if err := auth.Authorize(context.Background(), testPrincipal(t), "project-a", projectaccess.ReadProject); err != nil {
		t.Fatalf("active employee authorize: %v", err)
	}
	if policy.calls != 1 || len(standing.users) != 1 || standing.users[0] != "alice" {
		t.Fatalf("current standing was not checked before membership: policy=%d standing=%v", policy.calls, standing.users)
	}
}
