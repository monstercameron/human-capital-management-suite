package roleaccess

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type resolverStore struct {
	mu       sync.Mutex
	snapshot Snapshot
	loads    int
	err      error
}

func (s *resolverStore) Bootstrap(context.Context, values.TenantId, string) error { return nil }

func (s *resolverStore) Load(context.Context, values.TenantId, string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	if s.err != nil {
		return Snapshot{}, s.err
	}
	return s.snapshot, nil
}

func (s *resolverStore) loadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads
}

func (s *resolverStore) SaveRole(context.Context, values.TenantId, string, Role) (Role, error) {
	return Role{}, nil
}

func (s *resolverStore) SaveAssignment(_ context.Context, _ values.TenantId, _ string, value Assignment) (Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Assignments = append(s.snapshot.Assignments, value)
	return value, nil
}

func (s *resolverStore) SaveVisibility(context.Context, values.TenantId, string, string, VisibilityPolicy) (VisibilityPolicy, error) {
	return VisibilityPolicy{}, nil
}

func (s *resolverStore) SavePagePermission(context.Context, values.TenantId, string, PagePermission) (PagePermission, error) {
	return PagePermission{}, nil
}

func (s *resolverStore) SaveFeaturePermission(context.Context, values.TenantId, string, FeaturePermission) (FeaturePermission, error) {
	return FeaturePermission{}, nil
}

func TestContainsRoleMatchesNormalizedMembers(t *testing.T) {
	roles := []string{"manager", "hr_partner"}
	for _, role := range []string{"manager", " MANAGER ", "Hr_Partner"} {
		if !ContainsRole(roles, role) {
			t.Errorf("ContainsRole(%#v, %q) = false, want true", roles, role)
		}
	}
	for _, role := range []string{"", "  ", "comp_admin", "manage"} {
		if ContainsRole(roles, role) {
			t.Errorf("ContainsRole(%#v, %q) = true, want false", roles, role)
		}
	}
}

func TestHasEffectiveRolePrefersDurableOverAdmitted(t *testing.T) {
	snapshot := Snapshot{Assignments: []Assignment{{WorkerRef: "jane", RoleIDs: []string{"worker_self"}}}}
	if !HasEffectiveRole(snapshot, "jane", []string{"comp_admin"}, "worker_self") {
		t.Error("durable worker_self was not reported")
	}
	if HasEffectiveRole(snapshot, "jane", []string{"comp_admin"}, "comp_admin") {
		t.Error("admitted comp_admin widened a durable worker_self assignment")
	}
	if !HasEffectiveRole(snapshot, "other", []string{"comp_admin"}, "comp_admin") {
		t.Error("admitted roles were not used when no durable assignment exists")
	}
}

func TestHasAdministratorRoleReadsResolvedSet(t *testing.T) {
	if !HasAdministratorRole([]string{"worker_self", " HCM_ADMIN "}) {
		t.Error("hcm_admin was not recognized in the resolved set")
	}
	if !HasAdministratorRole([]string{"comp_admin"}) {
		t.Error("comp_admin was not recognized in the resolved set")
	}
	if HasAdministratorRole([]string{"manager", "worker_self"}) {
		t.Error("a non-administrator set was reported as administrative")
	}
}

func TestResolverCachesUntilInvalidated(t *testing.T) {
	now := time.Now()
	current := now
	store := &resolverStore{snapshot: Snapshot{Assignments: []Assignment{{WorkerRef: "jane", RoleIDs: []string{"manager"}}}}}
	resolver := NewResolver(time.Minute, func() time.Time { return current })
	ctx := context.Background()
	tenant := values.TenantId("acme-corp")

	roles, err := resolver.Resolve(ctx, store, tenant, "org", "jane", []string{"worker_self"})
	if err != nil || !reflect.DeepEqual(roles, []string{"manager"}) {
		t.Fatalf("Resolve = %#v, %v, want [manager]", roles, err)
	}
	// A store change without invalidation stays hidden behind the cache.
	store.mu.Lock()
	store.snapshot = Snapshot{Assignments: []Assignment{{WorkerRef: "jane", RoleIDs: []string{"comp_admin"}}}}
	store.mu.Unlock()
	roles, err = resolver.Resolve(ctx, store, tenant, "org", "jane", []string{"worker_self"})
	if err != nil || !reflect.DeepEqual(roles, []string{"manager"}) {
		t.Fatalf("cached Resolve = %#v, %v, want [manager]", roles, err)
	}
	if got := store.loadCount(); got != 1 {
		t.Fatalf("cached resolution loaded the store %d times, want 1", got)
	}

	resolver.Invalidate(tenant, " JANE ")
	roles, err = resolver.Resolve(ctx, store, tenant, "org", "jane", []string{"worker_self"})
	if err != nil || !reflect.DeepEqual(roles, []string{"comp_admin"}) {
		t.Fatalf("Resolve after Invalidate = %#v, %v, want [comp_admin]", roles, err)
	}
	if got := store.loadCount(); got != 2 {
		t.Fatalf("store loads = %d, want 2", got)
	}
}

func TestResolverExpiryReloadsAndScopesByTenant(t *testing.T) {
	now := time.Now()
	current := now
	store := &resolverStore{snapshot: Snapshot{Assignments: []Assignment{{WorkerRef: "jane", RoleIDs: []string{"manager"}}}}}
	resolver := NewResolver(time.Minute, func() time.Time { return current })
	ctx := context.Background()

	if _, err := resolver.Resolve(ctx, store, "acme-corp", "org", "jane", nil); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Another tenant resolves separately even for the same subject.
	if _, err := resolver.Resolve(ctx, store, "vendor-corp", "org", "jane", []string{"worker_self"}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := store.loadCount(); got != 2 {
		t.Fatalf("store loads = %d, want 2 (one per tenant)", got)
	}
	// Past the TTL both entries reload.
	current = now.Add(2 * time.Minute)
	if _, err := resolver.Resolve(ctx, store, "acme-corp", "org", "jane", nil); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := store.loadCount(); got != 3 {
		t.Fatalf("store loads after expiry = %d, want 3", got)
	}
	// Tenant-wide invalidation drops that tenant's entries only.
	resolver.InvalidateTenant("vendor-corp")
	if _, err := resolver.Resolve(ctx, store, "vendor-corp", "org", "jane", []string{"worker_self"}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := store.loadCount(); got != 4 {
		t.Fatalf("store loads after tenant invalidation = %d, want 4", got)
	}
}

func TestResolverLoadFailureCachesNothing(t *testing.T) {
	store := &resolverStore{err: errors.New("store unavailable")}
	resolver := NewResolver(time.Minute, nil)
	if _, err := resolver.Resolve(context.Background(), store, "acme-corp", "org", "jane", []string{"worker_self"}); err == nil {
		t.Fatal("Resolve succeeded despite the store failure")
	}
	store.mu.Lock()
	store.err = nil
	store.mu.Unlock()
	roles, err := resolver.Resolve(context.Background(), store, "acme-corp", "org", "jane", []string{"worker_self"})
	if err != nil || !reflect.DeepEqual(roles, []string{"worker_self"}) {
		t.Fatalf("Resolve after recovery = %#v, %v, want admitted fallback", roles, err)
	}
	if got := store.loadCount(); got != 2 {
		t.Fatalf("store loads = %d, want 2 (the failure must not have cached)", got)
	}
}

func TestResolverNilReceiverResolvesWithoutCache(t *testing.T) {
	store := &resolverStore{snapshot: Snapshot{Assignments: []Assignment{{WorkerRef: "jane", RoleIDs: []string{"manager"}}}}}
	var resolver *Resolver
	roles, err := resolver.Resolve(context.Background(), store, "acme-corp", "org", "jane", nil)
	if err != nil || !reflect.DeepEqual(roles, []string{"manager"}) {
		t.Fatalf("nil Resolve = %#v, %v, want [manager]", roles, err)
	}
	if _, err := resolver.Resolve(context.Background(), store, "acme-corp", "org", "jane", nil); err != nil {
		t.Fatalf("nil Resolve: %v", err)
	}
	if got := store.loadCount(); got != 2 {
		t.Fatalf("store loads = %d, want 2 (nil resolver must not cache)", got)
	}
	resolver.Invalidate("acme-corp", "jane")
	resolver.InvalidateTenant("acme-corp")
}
