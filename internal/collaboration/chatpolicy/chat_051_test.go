package chatpolicy

import (
	"errors"
	"testing"
	"time"
)

func chat051CrossCompanyInput() Input {
	in := baseInput()
	in.Principal.Tenant = "vendor"
	in.Channel.Classification = "internal"
	in.Channel.Residency = "us"
	in.Channel.RequiredRoles = []string{"member"}
	in.HasMembership = true
	in.Membership = Membership{
		ConversationID: in.Channel.ID,
		PrincipalID:    in.Principal.ID,
		Tenant:         in.Principal.Tenant,
		State:          MembershipCurrent,
		Revision:       1,
		GrantVersion:   3,
		JoinedAt:       in.Now.Add(-time.Minute),
	}
	in.HasGrant = true
	in.Grant = Grant{
		ID:                 "grant-3",
		ConversationID:     in.Channel.ID,
		HostTenant:         in.Channel.HostTenant,
		ConsumerTenant:     in.Principal.Tenant,
		Version:            3,
		Scope:              "conversation",
		Classification:     in.Channel.Classification,
		Residency:          in.Channel.Residency,
		ExpiresAt:          in.Now.Add(time.Hour),
		Proposed:           true,
		AcceptedByHost:     true,
		AcceptedByConsumer: true,
	}
	return in
}

// TestTodo_CHAT_051 proves that the current bilateral grant, current consumer
// membership, and host classification/residency constraints compose for reads
// and posts to a host-owned conversation.
func TestTodo_CHAT_051(t *testing.T) {
	in := chat051CrossCompanyInput()
	for _, action := range []Action{ActionRead, ActionPost} {
		if decision, err := Evaluate(action, in); err != nil || !decision.Allowed {
			t.Fatalf("action %d denied for a current bilateral grant: decision=%+v err=%v", action, decision, err)
		}
	}
}

// TestTodo_CHAT_051_Conformance checks that every foreign-facing action
// requires the exact current bilateral grant and that expiry/revocation stops
// an already admitted consumer immediately at the policy boundary.
func TestTodo_CHAT_051_Conformance(t *testing.T) {
	actions := []Action{ActionDiscover, ActionJoin, ActionRead, ActionPost}
	cases := []struct {
		name   string
		change func(*Input)
	}{
		{name: "host consent missing", change: func(in *Input) { in.Grant.AcceptedByHost = false }},
		{name: "consumer consent missing", change: func(in *Input) { in.Grant.AcceptedByConsumer = false }},
		{name: "grant expired", change: func(in *Input) { in.Grant.ExpiresAt = in.Now }},
		{name: "grant revoked", change: func(in *Input) { in.Grant.RevokedAt = in.Now }},
		{name: "wrong host", change: func(in *Input) { in.Grant.HostTenant = "other-host" }},
		{name: "wrong consumer", change: func(in *Input) { in.Grant.ConsumerTenant = "other-consumer" }},
		{name: "wrong conversation", change: func(in *Input) { in.Grant.ConversationID = "other-conversation" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range actions {
				in := chat051CrossCompanyInput()
				tc.change(&in)
				if decision, err := Evaluate(action, in); !errors.Is(err, ErrNotAuthorized) || decision.Allowed {
					t.Fatalf("action %d admitted stale or mismatched grant: decision=%+v err=%v", action, decision, err)
				}
			}
		})
	}

	// Revocation is effective at its timestamp; a call immediately before that
	// instant is still current, while a call at the instant must be denied.
	in := chat051CrossCompanyInput()
	in.Grant.RevokedAt = in.Now.Add(time.Second)
	if _, err := Evaluate(ActionRead, in); err != nil {
		t.Fatalf("grant revoked in the future denied early: %v", err)
	}
	in.Now = in.Grant.RevokedAt
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("grant admitted at revocation boundary: %v", err)
	}
}

// TestTodo_CHAT_051_Security prevents a consumer grant from widening the
// host's data classification or residency policy, for any action that could
// disclose or mutate conversation data.
func TestTodo_CHAT_051_Security(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Input)
	}{
		{name: "classification mismatch", change: func(in *Input) { in.Grant.Classification = "restricted" }},
		{name: "residency mismatch", change: func(in *Input) { in.Grant.Residency = "eu" }},
		{name: "membership from another home tenant", change: func(in *Input) { in.Membership.Tenant = "other-consumer" }},
		{name: "membership for another conversation", change: func(in *Input) { in.Membership.ConversationID = "other-conversation" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []Action{ActionRead, ActionPost} {
				in := chat051CrossCompanyInput()
				tc.change(&in)
				if decision, err := Evaluate(action, in); !errors.Is(err, ErrNotAuthorized) || decision.Allowed {
					t.Fatalf("action %d disclosed or changed host data under mismatched policy: decision=%+v err=%v", action, decision, err)
				}
			}
		})
	}
}
