package agentdelegationstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
)

// The user's page lists only the user's own active grants, and revoking a task
// revokes its grants so the next exchange is refused; another user's task
// matches nothing.
func TestTodo_AGENT2_018_UserGrantsIntegration(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t, "acme-corp", "other-corp")
	pg := env.grants(t, "acme-corp")
	service := newService(t, "acme-corp", pg)
	if _, err := service.CreateGrant(grantRequest("acme-corp", "grant-42")); err != nil {
		t.Fatal(err)
	}
	store, err := New(env.appConn(t), env.mapper)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := store.ActiveUserGrants(ctx, "acme-corp", "user-42", storeNow)
	if err != nil || len(grants) != 1 || grants[0].GrantID != "grant-42" || grants[0].TaskID != "task-9" || len(grants[0].Skills) != 2 || !grants[0].ExpiresAt.Equal(storeNow.Add(24*time.Hour)) {
		t.Fatalf("grants = %+v %v", grants, err)
	}
	if grants, err := store.ActiveUserGrants(ctx, "acme-corp", "user-99", storeNow); err != nil || len(grants) != 0 {
		t.Fatalf("another user sees %+v %v", grants, err)
	}
	if grants, err := store.ActiveUserGrants(ctx, "other-corp", "user-42", storeNow); err != nil || len(grants) != 0 {
		t.Fatalf("another tenant sees %+v %v", grants, err)
	}
	if grants, err := store.ActiveUserGrants(ctx, "acme-corp", "user-42", storeNow.Add(48*time.Hour)); err != nil || len(grants) != 0 {
		t.Fatalf("an expired grant is listed: %+v %v", grants, err)
	}

	if n, err := store.RevokeUserTaskGrants(ctx, "acme-corp", "user-99", "task-9", "user revoked grant"); err != nil || n != 0 {
		t.Fatalf("another user revoked %d grants (%v)", n, err)
	}
	if _, err := service.Exchange(exchange("grant-42")); err != nil {
		t.Fatalf("a grant that was not revoked stopped working: %v", err)
	}
	if n, err := store.RevokeUserTaskGrants(ctx, "acme-corp", "user-42", "task-9", "user revoked grant"); err != nil || n != 1 {
		t.Fatalf("revoked %d grants (%v)", n, err)
	}
	if _, err := newService(t, "acme-corp", env.grants(t, "acme-corp")).Exchange(exchange("grant-42")); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("exchange after the user's revoke = %v", err)
	}
	if grants, err := store.ActiveUserGrants(ctx, "acme-corp", "user-42", storeNow); err != nil || len(grants) != 0 {
		t.Fatalf("a revoked grant is still listed: %+v %v", grants, err)
	}
}
