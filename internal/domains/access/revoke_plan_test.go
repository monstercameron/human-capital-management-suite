// TestTodo_WF_CAP_003 proves access.plan_revoke_all enumerates a worker's
// holdings and emits an ordered revocation plan dispatching through
// connector bindings.
package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/access"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type wfCap003Source struct {
	kind     access.HoldingKind
	holdings []access.Holding
	tenant   values.TenantId
	worker   string
	err      error
}

func (s *wfCap003Source) Kind() access.HoldingKind { return s.kind }

func (s *wfCap003Source) ListHoldings(tenant values.TenantId, workerRef string) ([]access.Holding, error) {
	s.tenant, s.worker = tenant, workerRef
	if s.err != nil {
		return nil, s.err
	}
	return append([]access.Holding(nil), s.holdings...), nil
}

func wfCap003External() []*wfCap003Source {
	return []*wfCap003Source{
		{kind: access.HoldingSession, holdings: []access.Holding{
			{Kind: access.HoldingSession, Key: "sess-web-1", System: "sso", ConnectorRef: "connector:sso", Lifecycle: access.LifecycleActive},
			{Kind: access.HoldingSession, Key: "sess-vpn-1", System: "vpn", ConnectorRef: "connector:vpn", Lifecycle: access.LifecycleActive},
		}},
		{kind: access.HoldingCredential, holdings: []access.Holding{
			{Kind: access.HoldingCredential, Key: "ssh-key-1", System: "iam", ConnectorRef: "connector:iam", Lifecycle: access.LifecycleActive},
		}},
		{kind: access.HoldingBadge, holdings: []access.Holding{
			{Kind: access.HoldingBadge, Key: "badge-7", System: "facilities", ConnectorRef: "connector:badge", Lifecycle: access.LifecycleActive},
		}},
	}
}

func wfCap003Sources(external []*wfCap003Source) []access.HoldingSource {
	out := make([]access.HoldingSource, 0, len(external))
	for _, s := range external {
		out = append(out, s)
	}
	return out
}

func wfCap003Worker() string { return "11111111-1111-4111-8111-111111111111" }

func TestTodo_WF_CAP_003(t *testing.T) {
	graph := validGraph(t)
	tenant := values.TenantId("tenant-a")
	external := wfCap003External()

	plan, err := access.PlanRevokeAll(tenant, wfCap003Worker(), graph, wfCap003Sources(external))
	if err != nil {
		t.Fatalf("PlanRevokeAll: %v", err)
	}
	// Native: 1 account credential + 1 linked entitlement. External: 2
	// sessions, 1 credential, 1 badge.
	if len(plan.Steps) != 6 {
		t.Fatalf("plan has %d steps, want 6: %+v", len(plan.Steps), plan.Steps)
	}
	wantOrder := []access.HoldingKind{
		access.HoldingSession, access.HoldingSession,
		access.HoldingCredential, access.HoldingCredential,
		access.HoldingEntitlement,
		access.HoldingBadge,
	}
	for i, step := range plan.Steps {
		if step.Sequence != i+1 {
			t.Fatalf("step %d has sequence %d", i, step.Sequence)
		}
		if step.Operation != "REVOKE" {
			t.Fatalf("step %d has operation %q", i, step.Operation)
		}
		if step.Holding.Kind != wantOrder[i] {
			t.Fatalf("step %d revokes %s, want %s", i, step.Holding.Kind, wantOrder[i])
		}
		if step.Holding.GraphOnly || step.Holding.ConnectorRef == "" {
			t.Fatalf("provisioned step %d has no connector binding: %+v", i, step.Holding)
		}
	}
	if plan.Steps[4].Holding.ConnectorRef != "connection/github-prod" {
		t.Fatalf("linked entitlement dispatches through %q", plan.Steps[4].Holding.ConnectorRef)
	}
	if plan.GraphDigest == "" || plan.GraphDigest != graph.Digest() {
		t.Fatal("plan does not bind the graph version it planned against")
	}
	if plan.Digest == "" {
		t.Fatal("plan carries no seal")
	}
	again, err := access.PlanRevokeAll(tenant, wfCap003Worker(), graph, wfCap003Sources(wfCap003External()))
	if err != nil {
		t.Fatalf("PlanRevokeAll again: %v", err)
	}
	if again.Digest != plan.Digest {
		t.Fatal("identical inputs produced a different plan seal")
	}
	for _, s := range external {
		if s.tenant != tenant || s.worker != wfCap003Worker() {
			t.Fatal("external sources were not scoped to the plan tenant and worker")
		}
	}

	t.Run("unlinked_entitlement_revokes_graph_only", func(t *testing.T) {
		f := validFixture(t, "c")
		f.expected.AccountLinkID = ""
		f.expected.ID = "expected-unlinked"
		g, err := access.NewGraph(tenant)
		if err != nil {
			t.Fatalf("new graph: %v", err)
		}
		for _, record := range []access.Record{f.identity, f.entitlement, f.expected} {
			if err := g.Add(record); err != nil {
				t.Fatalf("add %T: %v", record, err)
			}
		}
		// validFixture("c") uses the same worker as suffix "a".
		plan, err := access.PlanRevokeAll(tenant, wfCap003Worker(), g, nil)
		if err != nil {
			t.Fatalf("PlanRevokeAll: %v", err)
		}
		if len(plan.Steps) != 1 || !plan.Steps[0].Holding.GraphOnly {
			t.Fatalf("unlinked entitlement must revoke graph-only: %+v", plan.Steps)
		}
	})

	t.Run("revoked_holdings_are_reported_not_planned", func(t *testing.T) {
		f := validFixture(t, "a")
		f.expected.Lifecycle = access.LifecycleRevoked
		g, err := access.NewGraph(tenant)
		if err != nil {
			t.Fatalf("new graph: %v", err)
		}
		for _, record := range []access.Record{f.identity, f.account, f.entitlement, f.expected} {
			if err := g.Add(record); err != nil {
				t.Fatalf("add %T: %v", record, err)
			}
		}
		plan, err := access.PlanRevokeAll(tenant, wfCap003Worker(), g, nil)
		if err != nil {
			t.Fatalf("PlanRevokeAll: %v", err)
		}
		if len(plan.Steps) != 1 || plan.Steps[0].Holding.Kind != access.HoldingCredential {
			t.Fatalf("revoked entitlement must leave only the credential step: %+v", plan.Steps)
		}
		if len(plan.AlreadyRevoked) != 1 || plan.AlreadyRevoked[0].Kind != access.HoldingEntitlement {
			t.Fatalf("revoked entitlement must be reported: %+v", plan.AlreadyRevoked)
		}
	})

	t.Run("planning_fails_closed", func(t *testing.T) {
		if _, err := access.PlanRevokeAll(tenant, "no-such-worker", graph, nil); err == nil {
			t.Fatal("unknown worker must fail closed")
		}
		if _, err := access.PlanRevokeAll(values.TenantId("tenant-b"), wfCap003Worker(), graph, nil); !errors.Is(err, access.ErrTenantMismatch) {
			t.Fatalf("tenant mismatch must fail closed, got %v", err)
		}
		dup := wfCap003External()
		dup[0].holdings = append(dup[0].holdings, access.Holding{Kind: access.HoldingSession, Key: "sess-web-1", System: "sso", ConnectorRef: "connector:sso", Lifecycle: access.LifecycleActive})
		if _, err := access.PlanRevokeAll(tenant, wfCap003Worker(), graph, wfCap003Sources(dup)); err == nil {
			t.Fatal("duplicated holdings must fail closed")
		}
		liar := &wfCap003Source{kind: access.HoldingSession, holdings: []access.Holding{
			{Kind: access.HoldingBadge, Key: "badge-9", System: "facilities", ConnectorRef: "connector:badge", Lifecycle: access.LifecycleActive},
		}}
		if _, err := access.PlanRevokeAll(tenant, wfCap003Worker(), graph, []access.HoldingSource{liar}); err == nil {
			t.Fatal("a source listing outside its kind must fail closed")
		}
		unbound := &wfCap003Source{kind: access.HoldingBadge, holdings: []access.Holding{
			{Kind: access.HoldingBadge, Key: "badge-9", System: "facilities", Lifecycle: access.LifecycleActive},
		}}
		if _, err := access.PlanRevokeAll(tenant, wfCap003Worker(), graph, []access.HoldingSource{unbound}); err == nil {
			t.Fatal("a connector step without a binding must fail closed")
		}
	})
}

// TestTodo_WF_CAP_003_Security proves no holding is omitted: every
// actionable holding appears exactly once, terminal holdings are reported,
// and nobody else's holdings leak into the plan.
func TestTodo_WF_CAP_003_Security(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	a, b := validFixture(t, "a"), validFixture(t, "b")
	b.account.Lifecycle = access.LifecycleSuspended
	b.expected.Lifecycle = access.LifecycleRetired
	g, err := access.NewGraph(tenant)
	if err != nil {
		t.Fatalf("new graph: %v", err)
	}
	for _, record := range []access.Record{a.identity, a.account, a.entitlement, a.expected, b.identity, b.account, b.entitlement, b.expected} {
		if err := g.Add(record); err != nil {
			t.Fatalf("add %T: %v", record, err)
		}
	}
	plan, err := access.PlanRevokeAll(tenant, wfCap003Worker(), g, wfCap003Sources(wfCap003External()))
	if err != nil {
		t.Fatalf("PlanRevokeAll: %v", err)
	}
	seen := map[string]int{}
	for _, step := range plan.Steps {
		key := string(step.Holding.Kind) + "\x00" + step.Holding.Key
		seen[key]++
	}
	// Native actionable: account-link-a credential + expected-a entitlement.
	// External: 2 sessions + ssh-key-1 + badge-7. Nothing else, nothing twice.
	want := []string{
		"CREDENTIAL\x00provider-account-a",
		"ENTITLEMENT\x00expected-a",
		"SESSION\x00sess-web-1",
		"SESSION\x00sess-vpn-1",
		"CREDENTIAL\x00ssh-key-1",
		"BADGE\x00badge-7",
	}
	if len(seen) != len(want) {
		t.Fatalf("plan covers %d holdings, want %d: %v", len(seen), len(want), seen)
	}
	for _, key := range want {
		if seen[key] != 1 {
			t.Fatalf("holding %q appears %d times: %v", key, seen[key], seen)
		}
	}
	for key := range seen {
		if key == "CREDENTIAL\x00provider-account-b" || key == "ENTITLEMENT\x00expected-b" {
			t.Fatalf("another worker's holding leaked into the plan: %q", key)
		}
	}
	rank := map[access.HoldingKind]int{
		access.HoldingSession: 0, access.HoldingCredential: 1, access.HoldingEntitlement: 2, access.HoldingBadge: 3,
	}
	for i := 1; i < len(plan.Steps); i++ {
		if rank[plan.Steps[i].Holding.Kind] < rank[plan.Steps[i-1].Holding.Kind] {
			t.Fatal("revocation order regressed: sessions, credentials, entitlements, badges")
		}
	}
}

// TestTodo_WF_CAP_003_Integration plans against the real store, executes,
// and verifies: survivors fail verification by name.
func TestTodo_WF_CAP_003_Integration(t *testing.T) {
	ctx := context.Background()
	tenant := values.TenantId("tenant-a")
	store := &access.MemoryStore{}
	f := validFixture(t, "a")
	for _, record := range []access.Record{f.identity, f.account, f.entitlement, f.expected} {
		if err := store.Add(ctx, record); err != nil {
			t.Fatalf("store add %T: %v", record, err)
		}
	}
	graph, err := store.Snapshot(ctx, tenant)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	plan, err := access.PlanRevokeAll(tenant, wfCap003Worker(), graph, wfCap003Sources(wfCap003External()))
	if err != nil {
		t.Fatalf("PlanRevokeAll: %v", err)
	}
	// Execute: every planned holding goes terminal (sessions vanish, the
	// rest revoke) and verification passes over the fresh enumeration.
	var current []access.Holding
	for _, step := range plan.Steps {
		if step.Holding.Kind == access.HoldingSession {
			continue
		}
		done := step.Holding
		done.Lifecycle = access.LifecycleRevoked
		current = append(current, done)
	}
	if err := plan.Verify(current); err != nil {
		t.Fatalf("verify after execution: %v", err)
	}
	// One survivor fails verification by name.
	survivor := plan.Steps[0].Holding
	if err := plan.Verify(append(current, survivor)); err == nil {
		t.Fatal("a surviving holding must fail verification")
	} else if got := err.Error(); !contains(got, survivor.Key) {
		t.Fatalf("verification does not name the survivor: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
