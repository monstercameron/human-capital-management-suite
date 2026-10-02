package agentconnect

import (
	"errors"
	"testing"
	"time"
)

// A later published revision replaces the live one without losing what users
// linked, and a lease minted under the old revision stops working.
func TestTodo_AGENT2_019_ReplaceKeepsLinksAndFencesLeases(t *testing.T) {
	issuer := &fakeIssuer{}
	registry := newTestRegistry(t, issuer)
	first := testRevision(t, UserDelegated)
	if err := registry.Register(first); err != nil {
		t.Fatal(err)
	}
	user := testUser()
	if err := registry.LinkAccount(user, "conn-a", testBinding("tenant-a", "user-account"), "external-user-a"); err != nil {
		t.Fatal(err)
	}
	leaseBefore, err := registry.IssueLease(user, "conn-a", "workers.read", "agent-a", "run-a", "case-review", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	second := copyRevision(first)
	second.Revision = 2
	second.Grants[0].Roles = []string{"director"}
	if err := registry.Replace(second); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	// The link survives; the grant no longer names this user's role, so the
	// effective reach is empty and a new lease is refused.
	if _, err := registry.LinkedAccount(user, "conn-a"); err != nil {
		t.Fatalf("the user's link was lost: %v", err)
	}
	if granted, err := registry.GrantedSkills(user, "conn-a"); err != nil || len(granted) != 0 {
		t.Fatalf("granted after replace = %+v %v", granted, err)
	}
	if _, err := registry.IssueLease(user, "conn-a", "workers.read", "agent-a", "run-b", "case-review", time.Minute); err == nil {
		t.Fatal("a lease was issued under a grant the new revision removed")
	}
	if _, err := registry.UseLease(leaseBefore, "hris.example", "lease"); !errors.Is(err, ErrLeaseFenced) {
		t.Fatalf("a lease minted under the old revision still worked: %v", err)
	}

	// History only grows: the same or an older number is refused, and so is a
	// revision for a connection that was never registered.
	if err := registry.Replace(second); !errors.Is(err, ErrInvalid) {
		t.Fatalf("same revision number = %v", err)
	}
	stranger := copyRevision(second)
	stranger.ID, stranger.Revision = "conn-b", 3
	if err := registry.Replace(stranger); err == nil {
		t.Fatal("a connection that was never registered was replaced")
	}
}
