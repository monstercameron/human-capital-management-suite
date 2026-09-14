package truststore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

// TestActiveJITGrantsReturnsOnlyCurrentRestorableAuthority proves the lookup
// a governed operator action relies on: only the latest revision of a
// requester's grant counts, only while it is valid, unrevoked and names the
// capability, and a durable record widened past its role's ceiling is not
// returned as authority.
func TestActiveJITGrantsReturnsOnlyCurrentRestorableAuthority(t *testing.T) {
	store, _, tenant := trustFixture(t)
	ctx := context.Background()
	issued := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	save := func(id, requester, role string, caps []string, ttl time.Duration) {
		t.Helper()
		scope, _ := json.Marshal(JITGrantScope{Role: role, TicketRef: "INC-1", Justification: "incident", Capabilities: caps, Purpose: "incident"})
		if err := store.PutJITGrant(ctx, tenant, JITGrantRecord{TenantID: tenant, RowID: uuid.New(), GrantID: id, Revision: 1, State: "ACTIVE",
			Requester: requester, Approver: "approver:lead", Scope: scope, NotBefore: issued, ExpiresAt: issued.Add(ttl)}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	save("g-pause", "operator:ana", string(jit.RoleIncidentResponder), []string{"WORKFLOW_PAUSE"}, 4*time.Hour)
	save("g-other-cap", "operator:ana", string(jit.RoleIncidentResponder), []string{"KEY_ROTATION"}, 4*time.Hour)
	save("g-other-user", "operator:bob", string(jit.RoleIncidentResponder), []string{"WORKFLOW_PAUSE"}, 4*time.Hour)
	save("g-widened", "operator:ana", string(jit.RolePayrollEmergency), []string{"WORKFLOW_PAUSE"}, 10*time.Hour)
	save("g-revoked", "operator:ana", string(jit.RoleIncidentResponder), []string{"WORKFLOW_PAUSE"}, 4*time.Hour)
	if _, err := store.UpdateJITGrant(ctx, tenant, "g-revoked", 1, "REVOKED", true); err != nil {
		t.Fatal(err)
	}
	key := values.TenantId("acme")
	grants, err := store.ActiveJITGrants(ctx, tenant, key, "operator:ana", "WORKFLOW_PAUSE", issued.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].ID != "g-pause" || grants[0].Tenant != key || !grants[0].ExpiresAt.Equal(issued.Add(4*time.Hour)) {
		t.Fatalf("active grants = %+v", grants)
	}
	if expired, _ := store.ActiveJITGrants(ctx, tenant, key, "operator:ana", "WORKFLOW_PAUSE", issued.Add(5*time.Hour)); len(expired) != 0 {
		t.Fatalf("expired grant returned: %+v", expired)
	}
	if early, _ := store.ActiveJITGrants(ctx, tenant, key, "operator:ana", "WORKFLOW_PAUSE", issued.Add(-time.Minute)); len(early) != 0 {
		t.Fatalf("not-yet-valid grant returned: %+v", early)
	}
	if _, err := store.ActiveJITGrants(ctx, tenant, key, "", "WORKFLOW_PAUSE", issued); CodeOf(err) != CodeInvalid {
		t.Fatalf("missing requester = %v", err)
	}
	if _, err := store.ActiveJITGrants(ctx, uuid.Nil, key, "operator:ana", "WORKFLOW_PAUSE", issued); CodeOf(err) != CodeTenantRequired {
		t.Fatalf("nil tenant = %v", err)
	}
}
