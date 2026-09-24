package chatpolicy

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var policyAt = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func baseInput() Input {
	return Input{Now: policyAt, Principal: Principal{ID: "alice", Tenant: "acme", Active: true, AuthorityRevision: 4, Roles: []string{"member"}}, Channel: Channel{ID: "conversation-1", HostTenant: "acme", Enabled: true, Revision: 8}}
}

func TestTodo_CHAT_010(t *testing.T) {
	in := baseInput()
	in.Channel.RequiredRoles = []string{"manager"}
	in.Channel.RoleMode = RolesAny
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("missing verified role error = %v", err)
	}
	in.Principal.Roles = []string{"manager"}
	in.Channel.RequiredQualifications = []string{"safety"}
	in.Principal.Qualifications = []Qualification{{Name: "safety", Verified: true, ValidFrom: policyAt.Add(-time.Hour), ValidTo: policyAt.Add(-time.Second)}}
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("expired qualification error = %v", err)
	}
	in.Principal.Qualifications[0].ValidTo = policyAt.Add(time.Second)
	if _, err := Evaluate(ActionJoin, in); err != nil {
		t.Fatalf("eligible nonmember could not join restricted public channel: %v", err)
	}
	if _, err := Evaluate(ActionPost, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("restricted post without membership error = %v", err)
	}
}

func TestTodo_CHAT_010_Security(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.Channel.AllowedPrincipals = []string{"bob"}
	if _, err := Evaluate(ActionDiscover, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("allowlist bypass error = %v", err)
	}
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("private membership bypass error = %v", err)
	}
	in = baseInput()
	in.Channel.AllowedPrincipals = []string{"alice"}
	in.Channel.DeniedPrincipals = []string{"alice"}
	if _, err := Evaluate(ActionDiscover, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("mandatory principal deny was overridden by allowlist: %v", err)
	}
	in.Channel.DeniedPrincipals = nil
	in.Channel.DeniedTenants = []string{"acme"}
	if _, err := Evaluate(ActionJoin, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("mandatory tenant deny was overridden by allowlist: %v", err)
	}
}

func TestTodo_CHAT_010_Property(t *testing.T) {
	in := baseInput()
	in.Channel.RequiredRoles = []string{"manager"}
	in.Channel.RoleMode = RolesAll
	in.Channel.RequiredQualifications = []string{"safety", "first-aid"}
	in.Channel.AllowedPrincipals = []string{"alice", "bob"}
	in.Channel.AllowedTenants = []string{"acme", "vendor"}
	in.Principal.Qualifications = []Qualification{{Name: "safety", Verified: true}, {Name: "first-aid", Verified: true}}
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipCurrent, Revision: 1, JoinedAt: policyAt.Add(-time.Hour)}
	for _, role := range []string{"manager", "member"} {
		in.Principal.Roles = []string{role}
		_, err := Evaluate(ActionRead, in)
		if role == "manager" && err != nil {
			t.Fatalf("role %q denied despite satisfying all rules: %v", role, err)
		}
		if role != "manager" && !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("role %q unexpectedly allowed: %v", role, err)
		}
	}
	in.Principal.Roles = []string{"manager"}
	if _, err := Evaluate(ActionRead, in); err != nil {
		t.Fatalf("all declared rules satisfied but denied: %v", err)
	}
	in.Principal.Qualifications[1].ValidTo = policyAt
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("expired required qualification did not deny: %v", err)
	}
	in.Principal.Qualifications[1].ValidTo = time.Time{}
	in.Channel.AllowedPrincipals = []string{"bob"}
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("role/qualification bypassed principal allowlist: %v", err)
	}
	in.Channel.AllowedPrincipals = []string{"alice"}
	in.Channel.RoleMode = RoleMode(255)
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("undeclared role composition did not fail closed: %v", err)
	}
}

func TestTodo_CHAT_010_Golden(t *testing.T) {
	in := baseInput()
	in.Channel.RequiredRoles = []string{"member", "manager"}
	in.Channel.RoleMode = RolesAny
	in.Channel.RequiredQualifications = []string{"safety"}
	in.Channel.AllowedPrincipals = []string{"alice"}
	in.Channel.AllowedTenants = []string{"acme"}
	in.Principal.Qualifications = []Qualification{{Name: "safety", Verified: true, ValidFrom: policyAt, ValidTo: policyAt.Add(time.Hour)}}

	var got strings.Builder
	for _, tc := range []struct {
		name   string
		action Action
	}{{"DISCOVER", ActionDiscover}, {"JOIN", ActionJoin}, {"READ", ActionRead}, {"POST", ActionPost}} {
		_, err := Evaluate(tc.action, in)
		state := "allow"
		if errors.Is(err, ErrNotAuthorized) {
			state = "deny"
		} else if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		got.WriteString(tc.name + "=" + state + "\n")
	}
	const want = "DISCOVER=allow\nJOIN=allow\nREAD=deny\nPOST=deny\n"
	if got.String() != want {
		t.Fatalf("eligibility decision golden changed:\n got: %q\nwant: %q", got.String(), want)
	}
}

func TestTodo_CHAT_011(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipCurrent, Revision: 2}
	binding, err := BindStream(in)
	if err != nil {
		t.Fatalf("BindStream: %v", err)
	}
	in.Principal.Active = false
	if StreamValid(binding, in) {
		t.Fatal("stream remained valid after logout")
	}
}

func TestTodo_CHAT_011_Security(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipCurrent, Revision: 1}
	binding, err := BindStream(in)
	if err != nil {
		t.Fatalf("BindStream: %v", err)
	}
	in.Principal.AuthorityRevision++
	if StreamValid(binding, in) {
		t.Fatal("stale authority revision accepted")
	}
}

func TestTodo_CHAT_011_Fault(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipCurrent, Revision: 1}
	binding, err := BindStream(in)
	if err != nil {
		t.Fatalf("BindStream: %v", err)
	}
	in.Membership.State = MembershipRemoved
	in.Membership.Revision++
	if StreamValid(binding, in) {
		t.Fatal("removed member retained stream")
	}
}

func TestTodo_CHAT_012(t *testing.T) {
	in := baseInput()
	in.Principal.Tenant = "vendor"
	in.Channel.Classification = "internal"
	in.Channel.Residency = "us"
	in.HasGrant = true
	in.Grant = Grant{ID: "g1", ConversationID: in.Channel.ID, HostTenant: "acme", ConsumerTenant: "vendor", Version: 2, Scope: "conversation", Classification: "internal", Residency: "us", Proposed: true, AcceptedByHost: true, ExpiresAt: policyAt.Add(time.Hour)}
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("unaccepted bilateral grant error = %v", err)
	}
	in.Grant.AcceptedByConsumer = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "vendor", State: MembershipCurrent, Revision: 1, GrantVersion: 2}
	if _, err := Evaluate(ActionRead, in); err != nil {
		t.Fatalf("accepted bilateral grant denied: %v", err)
	}
}

func TestTodo_CHAT_012_Security(t *testing.T) {
	in := baseInput()
	in.Principal.Tenant = "vendor"
	in.HasGrant = true
	in.Grant = Grant{ConversationID: in.Channel.ID, HostTenant: "acme", ConsumerTenant: "vendor", Version: 1, Proposed: true, AcceptedByHost: true, AcceptedByConsumer: true, ExpiresAt: policyAt.Add(-time.Nanosecond)}
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("expired grant error = %v", err)
	}
}

func TestTodo_CHAT_012_Integration(t *testing.T) {
	in := baseInput()
	in.Principal.Tenant = "vendor"
	in.Channel.Classification = "internal"
	in.Channel.Residency = "us"
	in.HasGrant = true
	in.Grant = Grant{ConversationID: in.Channel.ID, HostTenant: "acme", ConsumerTenant: "vendor", Version: 1, Scope: "conversation", Classification: "internal", Residency: "us", Proposed: true, AcceptedByHost: true, AcceptedByConsumer: true, ExpiresAt: policyAt.Add(time.Hour)}
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "vendor", State: MembershipCurrent, Revision: 1}
	if _, err := Evaluate(ActionPost, in); err != nil {
		t.Fatalf("cross-company post denied: %v", err)
	}
}

func TestTodo_CHAT_013(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipCurrent, Revision: 1}
	if _, err := Evaluate(ActionRead, in); err != nil {
		t.Fatalf("current membership denied: %v", err)
	}
	in.Membership.State = MembershipLeft
	in.Membership.Revision++
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("historical membership retained access: %v", err)
	}
}

func TestTodo_CHAT_013_Security(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipSuspended, Revision: 3}
	if _, err := Evaluate(ActionRead, in); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("suspended member error = %v", err)
	}
}

func TestTodo_CHAT_013_Integration(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipCurrent, Revision: 1}
	if _, err := Evaluate(ActionJoin, in); err != nil {
		t.Fatalf("join check denied: %v", err)
	}
}

// TestTodo_CHAT_013_Golden pins the membership-lifecycle decision matrix: for
// each lifecycle state (current, left, suspended, removed) that a prior
// join/invite/accept could have produced, only the current-authority state
// grants read/post access even though every state remains in history.
func TestTodo_CHAT_013_Golden(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true

	var got strings.Builder
	for _, tc := range []struct {
		name  string
		state MembershipState
	}{
		{"CURRENT", MembershipCurrent},
		{"LEFT", MembershipLeft},
		{"SUSPENDED", MembershipSuspended},
		{"REMOVED", MembershipRemoved},
	} {
		in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: tc.state, Revision: 1}
		_, readErr := Evaluate(ActionRead, in)
		_, postErr := Evaluate(ActionPost, in)
		readState, postState := "allow", "allow"
		if errors.Is(readErr, ErrNotAuthorized) {
			readState = "deny"
		} else if readErr != nil {
			t.Fatalf("%s READ: unexpected error: %v", tc.name, readErr)
		}
		if errors.Is(postErr, ErrNotAuthorized) {
			postState = "deny"
		} else if postErr != nil {
			t.Fatalf("%s POST: unexpected error: %v", tc.name, postErr)
		}
		got.WriteString(tc.name + " READ=" + readState + " POST=" + postState + "\n")
	}
	const want = "CURRENT READ=allow POST=allow\n" +
		"LEFT READ=deny POST=deny\n" +
		"SUSPENDED READ=deny POST=deny\n" +
		"REMOVED READ=deny POST=deny\n"
	if got.String() != want {
		t.Fatalf("membership lifecycle decision golden changed:\n got: %q\nwant: %q", got.String(), want)
	}
}

func TestTodo_CHAT_020(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipCurrent, Revision: 1}
	binding, err := BindStream(in)
	if err != nil {
		t.Fatalf("BindStream: %v", err)
	}
	in.Channel.Revision++
	if StreamValid(binding, in) {
		t.Fatal("stream accepted stale policy revision")
	}
}

func TestTodo_CHAT_020_Security(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipCurrent, Revision: 1}
	binding, err := BindStream(in)
	if err != nil {
		t.Fatalf("BindStream: %v", err)
	}
	in.Membership.State = MembershipRemoved
	in.Membership.Revision++
	if StreamValid(binding, in) {
		t.Fatal("derived read accepted revoked membership")
	}
}

func TestTodo_CHAT_020_Fault(t *testing.T) {
	in := baseInput()
	in.Channel.Private = true
	in.HasMembership = true
	in.Membership = Membership{ConversationID: in.Channel.ID, PrincipalID: "alice", Tenant: "acme", State: MembershipCurrent, Revision: 1}
	if _, err := BindStream(in); err != nil {
		t.Fatalf("initial bind: %v", err)
	}
	if _, err := Evaluate(ActionRead, Input{Now: policyAt, Channel: in.Channel}); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("missing principal did not fail closed: %v", err)
	}
}

func TestClockInjection(t *testing.T) {
	if got := Now(func() time.Time { return policyAt }); !got.Equal(policyAt) {
		t.Fatalf("clock = %v", got)
	}
	if !Now(nil).IsZero() {
		t.Fatal("nil clock returned time")
	}
}

func TestGrantLifecycleIsBilateralAndVersioned(t *testing.T) {
	g, err := ProposeGrant("g1", "conversation-1", "acme", "vendor", "conversation", "internal", "us", 1, policyAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("ProposeGrant: %v", err)
	}
	if g.AcceptedByConsumer {
		t.Fatal("proposal unexpectedly has consumer consent")
	}
	if _, err := AcceptGrant(g, "other", policyAt); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("wrong consumer error = %v", err)
	}
	g, err = AcceptGrant(g, "vendor", policyAt)
	if err != nil || !g.AcceptedByConsumer {
		t.Fatalf("AcceptGrant = %#v, %v", g, err)
	}
	revoked, err := RevokeGrant(g, policyAt)
	if err != nil || revoked.Version != 2 || revoked.RevokedAt != policyAt {
		t.Fatalf("RevokeGrant = %#v, %v", revoked, err)
	}
}
