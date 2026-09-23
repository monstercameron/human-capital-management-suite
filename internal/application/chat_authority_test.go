package application

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatRoleReader struct{ snapshot roleaccess.Snapshot }

func (r *chatRoleReader) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return r.snapshot, nil
}

type chatSessionChecker struct{ revoked bool }

func (c *chatSessionChecker) CheckRevocation(context.Context, string, time.Time) error {
	if c.revoked {
		return errors.New("revoked")
	}
	return nil
}

type chatAdminFacts struct{ role string }

func (f chatAdminFacts) ResolveChatFacts(_ context.Context, tenant, subject string, _ time.Time) (chatpolicy.Principal, error) {
	return chatpolicy.Principal{ID: subject, Tenant: tenant, Active: true, Roles: []string{f.role}, AuthorityRevision: 1}, nil
}

func TestTodo_CHAT_012_Security_CompanyConsent(t *testing.T) {
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "admin", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-host", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	s := NewChatCompanyGrants(chatAdminFacts{role: "hcm_admin"}, chatauthority.New(nil))
	ctx := trust.WithPrincipal(context.Background(), principal)
	if err := s.Accept(ctx, "host", "consumer", "conversation", "grant", at); err == nil {
		t.Fatal("host accepted consumer consent")
	}
	if err := s.Revoke(ctx, "host", "consumer", "conversation", "grant", at); err == nil {
		t.Fatal("storage-less revoke succeeded")
	}
	limited := NewChatCompanyGrants(chatAdminFacts{role: "employee"}, chatauthority.New(nil))
	if _, err := limited.Propose(ctx, "host", "consumer", "conversation", "internal", "US", at.Add(time.Hour), at); err == nil {
		t.Fatal("non-admin proposed sharing")
	}
}

func TestTodo_CHAT_010_Security_CurrentRoles(t *testing.T) {
	at := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "worker-1", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org:host", Roles: []string{"hcm_admin"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-1", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	roles := &chatRoleReader{snapshot: roleaccess.Snapshot{Roles: []roleaccess.Role{{ID: "hcm_admin", Version: 1, Active: true}}, Assignments: []roleaccess.Assignment{{WorkerRef: "worker-1", Version: 1, RoleIDs: []string{"hcm_admin"}}}}}
	sessions := &chatSessionChecker{}
	source := newChatAuthoritySource(newCurrentRoleChatFacts(roles, sessions))
	ctx := trust.WithPrincipal(context.Background(), p)
	current, err := source.Resolve(ctx, "host", "worker-1", at)
	if err != nil || len(current.Roles) != 1 {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	roles.snapshot.Assignments[0].RoleIDs = nil
	current, err = source.Resolve(ctx, "host", "worker-1", at)
	if err != nil || len(current.Roles) != 0 {
		t.Fatalf("role loss persisted=%+v err=%v", current, err)
	}
	if _, err := source.Resolve(ctx, "host", "other", at); err == nil {
		t.Fatal("forged subject admitted")
	}
	if _, err := source.Resolve(ctx, "host", "worker-1", at.Add(2*time.Hour)); err == nil {
		t.Fatal("expired credential admitted")
	}
	sessions.revoked = true
	if _, err := source.Resolve(ctx, "host", "worker-1", at); err == nil {
		t.Fatal("revoked session admitted")
	}
}

func chatPrincipalFixture() chatpolicy.Principal {
	return chatpolicy.Principal{ID: "subject", Tenant: "home", Active: true, AuthorityRevision: 1}
}

type countingChatFacts struct {
	calls int
	p     chatpolicy.Principal
	err   error
}

func (f *countingChatFacts) ResolveChatFacts(_ context.Context, tenant, subject string, _ time.Time) (chatpolicy.Principal, error) {
	f.calls++
	if f.err != nil {
		return chatpolicy.Principal{}, f.err
	}
	p := f.p
	p.ID, p.Tenant = subject, tenant
	return p, nil
}

// TestTodo_CHAT_010_AuthorityCacheBoundsCoreReads proves the policy freshness
// contract: repeated authorizations reuse one core read, the TTL expires it, a
// revocation invalidates it immediately, and a failure is never cached.
func TestTodo_CHAT_010_AuthorityCacheBoundsCoreReads(t *testing.T) {
	at := time.Now().UTC()
	now := at
	cache := newChatAuthorityCache(time.Minute, func() time.Time { return now })
	inner := &countingChatFacts{p: chatpolicy.Principal{Active: true, AuthorityRevision: 3}}
	facts := newCachedChatFacts(inner, cache)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "home", Subject: "subject", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-1", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), p)
	for i := 0; i < 5; i++ {
		got, err := facts.ResolveChatFacts(ctx, "home", "subject", at)
		if err != nil || got.AuthorityRevision != 3 {
			t.Fatalf("resolve %d: %+v %v", i, got, err)
		}
	}
	if inner.calls != 1 {
		t.Fatalf("core reads=%d, want one cached read", inner.calls)
	}
	// A revocation path drops the entry at once, before the TTL.
	cache.InvalidatePrincipal("home", "subject")
	if _, err := facts.ResolveChatFacts(ctx, "home", "subject", at); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 {
		t.Fatalf("invalidated principal reused a cached fact: calls=%d", inner.calls)
	}
	// Past the TTL the entry is reloaded even without a revocation.
	now = now.Add(2 * time.Minute)
	if _, err := facts.ResolveChatFacts(ctx, "home", "subject", at); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 3 {
		t.Fatalf("expired fact served: calls=%d", inner.calls)
	}
	// A failure is never cached: the next call must ask again.
	cache.InvalidatePrincipal("home", "subject")
	inner.err = errChatIdentity
	if _, err := facts.ResolveChatFacts(ctx, "home", "subject", at); err == nil {
		t.Fatal("failed fact resolution reported success")
	}
	if _, err := facts.ResolveChatFacts(ctx, "home", "subject", at); err == nil || inner.calls != 5 {
		t.Fatalf("failure cached: calls=%d err=%v", inner.calls, err)
	}
	if newCachedChatFacts(inner, nil) != ChatAuthorityFacts(inner) {
		t.Fatal("a nil cache must leave the source untouched")
	}
}

// TestTodo_CHAT_010_AuthorityCacheIsBounded proves the cache cannot grow without
// limit and that an absent channel policy is remembered as absent.
func TestTodo_CHAT_010_AuthorityCacheIsBounded(t *testing.T) {
	now := time.Now().UTC()
	cache := newChatAuthorityCache(time.Minute, func() time.Time { return now })
	for i := 0; i < maxChatAuthorityEntries+64; i++ {
		cachedChatPut(cache, cache.facts, chatCacheKey("home", "subject", strconv.Itoa(i)), chatPrincipalFixture())
	}
	if len(cache.facts) > maxChatAuthorityEntries {
		t.Fatalf("cached facts=%d, want at most %d", len(cache.facts), maxChatAuthorityEntries)
	}
	cachedChatPut(cache, cache.policies, chatCacheKey("host", "c"), cachedChatPolicy{absent: true})
	entry, ok := cachedChatGet(cache, cache.policies, chatCacheKey("host", "c"))
	if !ok || !entry.absent {
		t.Fatalf("absent policy not remembered: %+v ok=%v", entry, ok)
	}
	cache.InvalidateConversation("host", "c")
	if _, ok := cachedChatGet(cache, cache.policies, chatCacheKey("host", "c")); ok {
		t.Fatal("invalidated conversation kept its policy")
	}
	var nilCache *chatAuthorityCache
	nilCache.InvalidateConversation("host", "c")
	nilCache.InvalidatePrincipal("home", "subject")
	nilCache.InvalidateTenant("home")
	if _, ok := cachedChatGet[chatpolicy.Principal](nilCache, nil, "k"); ok {
		t.Fatal("nil cache reported a hit")
	}
}
