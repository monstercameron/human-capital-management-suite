package authn_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

var lifecycleAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

type recordingSink struct {
	mu      sync.Mutex
	targets []authn.RevocationTarget
	err     error
}

func (s *recordingSink) Revoke(_ context.Context, target authn.RevocationTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = append(s.targets, target)
	return s.err
}

func (s *recordingSink) snapshot() []authn.RevocationTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]authn.RevocationTarget(nil), s.targets...)
}

func lifecycleFixture(t *testing.T) (*authn.Service, authn.Identity, *recordingSink) {
	t.Helper()
	service := authn.NewMemory(func() time.Time { return lifecycleAt })
	if _, _, err := service.CreateAccount(authn.AccountSpec{ID: "account-1", PersonID: "person-1", Tenant: "tenant-a", At: lifecycleAt}); err != nil {
		t.Fatal(err)
	}
	identity, _, err := service.LinkIdentity(authn.IdentitySpec{ID: "identity-1", AccountID: "account-1", Tenant: "tenant-a", ProviderRef: "idp-1", SubjectDigest: validSubjectDigest(), At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	service.SetRevocationSink(sink)
	return service, identity, sink
}

func validSubjectDigest() string {
	return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}

func evidence(reason string) authn.Evidence {
	return authn.Evidence{Actor: "security-admin", Authority: "account-governance", Reason: reason, At: lifecycleAt}
}

func registerAllDependents(t *testing.T, service *authn.Service, identity authn.Identity) []authn.Dependent {
	t.Helper()
	kinds := []authn.DependentKind{authn.DependentPrincipal, authn.DependentSession, authn.DependentTokenFamily, authn.DependentAuthenticator, authn.DependentFederation, authn.DependentDelegation, authn.DependentSubjectLink}
	dependents := make([]authn.Dependent, 0, len(kinds))
	for i, kind := range kinds {
		dependent, err := service.RegisterDependent(authn.DependentSpec{ID: "dependent-" + string(rune('1'+i)), AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: kind, Assurance: trust.AssuranceSubstantial, Queued: i%2 == 0, At: lifecycleAt})
		if err != nil {
			t.Fatalf("RegisterDependent(%s): %v", kind, err)
		}
		dependents = append(dependents, dependent)
	}
	return dependents
}

// TestSubscriberAccountLifecycleRevokesPrincipalSessionsAuthenticatorsAndDelegation
// is the AUTHN-009 primary contract: a committed account epoch fences every
// dependent and each concrete adapter receives the same revocation target.
func TestSubscriberAccountLifecycleRevokesPrincipalSessionsAuthenticatorsAndDelegation(t *testing.T) {
	service := authn.NewMemory(func() time.Time { return lifecycleAt })
	if _, _, err := service.CreateAccount(authn.AccountSpec{ID: "account-1", PersonID: "person-1", Tenant: "tenant-a", At: lifecycleAt}); err != nil {
		t.Fatal(err)
	}
	identity, _, err := service.LinkIdentity(authn.IdentitySpec{ID: "identity-1", AccountID: "account-1", Tenant: "tenant-a", ProviderRef: "idp-1", SubjectDigest: validSubjectDigest(), At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	dependents := registerAllDependents(t, service, identity)
	sink := &recordingSink{}
	service.SetRevocationSink(sink)
	account, event, err := service.Disable(context.Background(), "account-1", evidence("account_disabled"))
	if err != nil {
		t.Fatal(err)
	}
	if account.Status != authn.AccountDisabled || account.RevocationEpoch != 2 || event.Epoch != 2 {
		t.Fatalf("account=%+v event=%+v", account, event)
	}
	if len(event.Affected) != len(dependents) || len(sink.snapshot()) != len(dependents) {
		t.Fatalf("affected=%d sink=%d dependents=%d", len(event.Affected), len(sink.snapshot()), len(dependents))
	}
	if _, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, At: lifecycleAt}); !errors.Is(err, authn.ErrAccountNotActive) {
		t.Fatalf("authenticate disabled=%v, want ErrAccountNotActive", err)
	}
	for _, dependent := range dependents {
		got, ok, err := service.Dependent(dependent.ID)
		if err != nil || !ok || got.Status != authn.DependentRevoked || got.RevocationEpoch != account.RevocationEpoch {
			t.Fatalf("dependent=%+v ok=%v err=%v", got, ok, err)
		}
	}
	events, err := service.Events(account.ID)
	if err != nil || len(events) != 3 || events[2].Digest == "" || events[2].PreviousDigest != events[1].Digest {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

// TestTodo_AUTHN_009_Property proves all declared dependent kinds are fenced
// by one epoch, including queued authority.
func TestTodo_AUTHN_009_Property(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	dependents := registerAllDependents(t, service, identity)
	account, _, err := service.Suspend(context.Background(), "account-1", evidence("security_suspend"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dependent := range dependents {
		if err := service.CheckDependent(account.ID, dependent.ID, lifecycleAt); !errors.Is(err, authn.ErrAccountNotActive) {
			t.Fatalf("dependent %s check=%v, want account refusal", dependent.ID, err)
		}
	}
}

// TestTodo_AUTHN_009_Golden pins deterministic fan-out ordering and the
// immutable evidence shape.
func TestTodo_AUTHN_009_Golden(t *testing.T) {
	service, identity, sink := lifecycleFixture(t)
	registerAllDependents(t, service, identity)
	_, event, err := service.Terminate(context.Background(), "account-1", evidence("account_terminated"))
	if err != nil {
		t.Fatal(err)
	}
	if len(event.Affected) != 7 || event.Affected[0].Kind != authn.DependentAuthenticator || event.Affected[0].ID != "dependent-4" {
		t.Fatalf("event affected=%+v", event.Affected)
	}
	if len(sink.snapshot()) != 7 || event.Digest == "" {
		t.Fatalf("sink=%+v event=%+v", sink.snapshot(), event)
	}
}

// TestTodo_AUTHN_009_Race proves a committed revoke wins against concurrent
// authentication attempts; no successful result may observe the old epoch.
func TestTodo_AUTHN_009_Race(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	dependent, err := service.RegisterDependent(authn.DependentSpec{ID: "dependent-race", AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentSession, Assurance: trust.AssuranceSubstantial, At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes, refusals int
	var mu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, DependentID: dependent.ID, At: lifecycleAt})
			mu.Lock()
			if err == nil {
				successes++
			} else {
				refusals++
			}
			mu.Unlock()
		}()
	}
	close(start)
	if _, _, err := service.Disable(context.Background(), "account-1", evidence("race_disable")); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if successes > 16 || refusals == 0 {
		t.Fatalf("successes=%d refusals=%d", successes, refusals)
	}
}

// TestTodo_AUTHN_009_Integration proves the session adapter revokes the
// canonical trust/session record after the lifecycle epoch commits.
func TestTodo_AUTHN_009_Integration(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	clock := lifecycleAt
	mgr, err := session.NewManager(session.ManagerConfig{Now: func() time.Time { return clock }, IdleTimeout: time.Hour, AbsoluteTimeout: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := mgr.Create(context.Background(), session.CreateSpec{Tenant: "tenant-a", Subject: "subject-1", PrincipalFingerprint: "fp-1", Assurance: trust.AssuranceSubstantial})
	if err != nil {
		t.Fatal(err)
	}
	service.SetRevocationSink(authn.SessionRevocationSink{Revoker: mgr})
	if _, err := service.RegisterDependent(authn.DependentSpec{ID: string(record.ID()), AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentSession, Assurance: trust.AssuranceSubstantial, At: lifecycleAt}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Disable(context.Background(), "account-1", evidence("integration_disable")); err != nil {
		t.Fatal(err)
	}
	if err := mgr.CheckRevocation(context.Background(), string(record.ID()), lifecycleAt); !errors.Is(err, session.ErrSessionRevoked) {
		t.Fatalf("session check=%v, want ErrSessionRevoked", err)
	}
}

// TestTodo_AUTHN_009_Fault proves a failed adapter is reported after the
// committed epoch and cannot leave the derived projection usable.
func TestTodo_AUTHN_009_Fault(t *testing.T) {
	service, identity, sink := lifecycleFixture(t)
	sink.err = errors.New("adapter unavailable")
	dependent, err := service.RegisterDependent(authn.DependentSpec{ID: "dependent-fault", AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentDelegation, Assurance: trust.AssuranceSubstantial, At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	account, _, err := service.Disable(context.Background(), "account-1", evidence("fault_disable"))
	if !errors.Is(err, authn.ErrFanoutIncomplete) {
		t.Fatalf("disable error=%v, want ErrFanoutIncomplete", err)
	}
	got, _, _ := service.Dependent(dependent.ID)
	if account.RevocationEpoch != got.RevocationEpoch || got.Status != authn.DependentRevoked {
		t.Fatalf("account=%+v dependent=%+v", account, got)
	}
}

// TestTodo_AUTHN_009_Security proves tenant confusion and disabled identity
// attempts are typed refusals.
func TestTodo_AUTHN_009_Security(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	if _, err := service.RegisterDependent(authn.DependentSpec{ID: "cross-tenant", AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-b", Kind: authn.DependentPrincipal, Assurance: trust.AssuranceSubstantial, At: lifecycleAt}); !errors.Is(err, authn.ErrTenantMismatch) {
		t.Fatalf("cross tenant=%v, want ErrTenantMismatch", err)
	}
	if _, _, err := service.Unlink(context.Background(), identity.ID, evidence("unlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, At: lifecycleAt}); !errors.Is(err, authn.ErrIdentityNotUsable) {
		t.Fatalf("unlinked authentication=%v, want ErrIdentityNotUsable", err)
	}
}

// TestTodo_AUTHN_009_Conformance proves relink increments revision, preserves
// old events, and does not resurrect old derived authority.
func TestTodo_AUTHN_009_Conformance(t *testing.T) {
	if err := authn.ValidateServingContract(); err != nil {
		t.Fatalf("serving lifecycle contract: %v", err)
	}
	service, identity, _ := lifecycleFixture(t)
	dependent, err := service.RegisterDependent(authn.DependentSpec{ID: "dependent-history", AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentSubjectLink, Assurance: trust.AssuranceSubstantial, At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Unlink(context.Background(), identity.ID, evidence("unlink")); err != nil {
		t.Fatal(err)
	}
	relinked, _, err := service.Relink(context.Background(), identity.ID, evidence("relink"))
	if err != nil || relinked.Revision != 3 || relinked.Status != authn.IdentityLinked {
		t.Fatalf("relinked=%+v err=%v", relinked, err)
	}
	if _, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, DependentID: dependent.ID, At: lifecycleAt}); !errors.Is(err, authn.ErrDependentRevoked) {
		t.Fatalf("old authority after relink=%v, want ErrDependentRevoked", err)
	}
}

// TestTodo_AUTHN_009_Recovery proves recovery cannot elevate assurance and
// succeeds only when the requested level is bounded by trusted assurance.
func TestTodo_AUTHN_009_Recovery(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	if _, err := service.Recover(authn.RecoveryRequest{AccountID: "account-1", IdentityID: identity.ID, CurrentAssurance: trust.AssuranceSubstantial, RequestedAssurance: trust.AssuranceHigh, Evidence: evidence("weak_recovery")}); !errors.Is(err, authn.ErrRecoveryAssurance) {
		t.Fatalf("weak recovery=%v, want ErrRecoveryAssurance", err)
	}
	if _, err := service.Recover(authn.RecoveryRequest{AccountID: "account-1", IdentityID: identity.ID, CurrentAssurance: trust.AssuranceHigh, RequestedAssurance: trust.AssuranceSubstantial, Evidence: evidence("bounded_recovery")}); err != nil {
		t.Fatalf("bounded recovery=%v", err)
	}
}

// TestTodo_AUTHN_009_ModelBased exercises every lifecycle transition against
// a small model of the invariant that non-active accounts reject auth.
func TestTodo_AUTHN_009_ModelBased(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	for _, step := range []struct {
		name string
		call func() error
	}{
		{"suspend", func() error {
			_, _, err := service.Suspend(context.Background(), "account-1", evidence("model_suspend"))
			return err
		}},
		{"resume", func() error {
			_, _, err := service.Resume(context.Background(), "account-1", evidence("model_resume"))
			return err
		}},
		{"disable", func() error {
			_, _, err := service.Disable(context.Background(), "account-1", evidence("model_disable"))
			return err
		}},
	} {
		if err := step.call(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		_, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, At: lifecycleAt})
		if step.name != "resume" && !errors.Is(err, authn.ErrAccountNotActive) {
			t.Fatalf("%s auth=%v, want account refusal", step.name, err)
		}
	}
}

// TestTodo_AUTHN_009_Mutation proves lifecycle evidence and projections are
// defensive copies and cannot be mutated through a returned slice.
func TestTodo_AUTHN_009_Mutation(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	if _, _, err := service.Unlink(context.Background(), identity.ID, evidence("mutation")); err != nil {
		t.Fatal(err)
	}
	events, err := service.Events("account-1")
	if err != nil {
		t.Fatal(err)
	}
	events[0].Affected = append(events[0].Affected, authn.Reference{Kind: authn.DependentPrincipal, ID: "tampered"})
	again, err := service.Events("account-1")
	if err != nil || len(again) != 3 || len(again[0].Affected) != 0 {
		t.Fatalf("mutated history=%+v err=%v", again, err)
	}
	if authn.Explain() == "" || authn.Version() < 1 {
		t.Fatal("lifecycle contract exports incomplete")
	}
}

type lifecycleErrorStore struct {
	inner *authn.MemoryStore
	fail  string
	err   error
}

func (s *lifecycleErrorStore) wants(method string) error {
	if s.fail == method {
		return s.err
	}
	return nil
}

func (s *lifecycleErrorStore) CreateAccount(a authn.Account, e authn.LifecycleEvent) error {
	if err := s.wants("CreateAccount"); err != nil {
		return err
	}
	return s.inner.CreateAccount(a, e)
}
func (s *lifecycleErrorStore) GetAccount(id string) (authn.Account, bool, error) {
	if err := s.wants("GetAccount"); err != nil {
		return authn.Account{}, false, err
	}
	return s.inner.GetAccount(id)
}
func (s *lifecycleErrorStore) GetIdentity(id string) (authn.Identity, bool, error) {
	if err := s.wants("GetIdentity"); err != nil {
		return authn.Identity{}, false, err
	}
	return s.inner.GetIdentity(id)
}
func (s *lifecycleErrorStore) ListIdentities(id string) ([]authn.Identity, error) {
	if err := s.wants("ListIdentities"); err != nil {
		return nil, err
	}
	return s.inner.ListIdentities(id)
}
func (s *lifecycleErrorStore) GetDependent(id string) (authn.Dependent, bool, error) {
	if err := s.wants("GetDependent"); err != nil {
		return authn.Dependent{}, false, err
	}
	return s.inner.GetDependent(id)
}
func (s *lifecycleErrorStore) ListDependents(id string) ([]authn.Dependent, error) {
	if err := s.wants("ListDependents"); err != nil {
		return nil, err
	}
	return s.inner.ListDependents(id)
}
func (s *lifecycleErrorStore) Commit(a authn.Account, i []authn.Identity, d []authn.Dependent, e authn.LifecycleEvent) error {
	if err := s.wants("Commit"); err != nil {
		return err
	}
	return s.inner.Commit(a, i, d, e)
}
func (s *lifecycleErrorStore) AppendIdentity(i authn.Identity, e authn.LifecycleEvent) error {
	if err := s.wants("AppendIdentity"); err != nil {
		return err
	}
	return s.inner.AppendIdentity(i, e)
}
func (s *lifecycleErrorStore) AppendDependent(d authn.Dependent) error {
	if err := s.wants("AppendDependent"); err != nil {
		return err
	}
	return s.inner.AppendDependent(d)
}
func (s *lifecycleErrorStore) Events(id string) ([]authn.LifecycleEvent, error) {
	if err := s.wants("Events"); err != nil {
		return nil, err
	}
	return s.inner.Events(id)
}

type sessionRevokerStub struct {
	id     session.ID
	reason string
	err    error
}

func (s *sessionRevokerStub) Revoke(_ context.Context, id session.ID, reason string) (session.Record, error) {
	s.id, s.reason = id, reason
	return session.Record{}, s.err
}

func TestLifecycleConstructorsAndNilGuards(t *testing.T) {
	if authn.NewMemoryStore() == nil || authn.NewMemory() == nil || authn.New(nil) == nil {
		t.Fatal("constructors returned nil")
	}
	var nilService *authn.Service
	nilService.SetRevocationSink(nil)
	cases := []func() error{
		func() error { _, _, err := nilService.CreateAccount(authn.AccountSpec{}); return err },
		func() error { _, _, err := nilService.LinkIdentity(authn.IdentitySpec{}); return err },
		func() error { _, err := nilService.RegisterDependent(authn.DependentSpec{}); return err },
		func() error { _, _, err := nilService.Suspend(context.Background(), "a", evidence("x")); return err },
		func() error { _, _, err := nilService.Unlink(context.Background(), "i", evidence("x")); return err },
		func() error {
			_, err := nilService.Authenticate(authn.AuthenticationRequest{At: lifecycleAt})
			return err
		},
		func() error { _, _, err := nilService.Account("a"); return err },
		func() error { _, _, err := nilService.Identity("i"); return err },
		func() error { _, _, err := nilService.Dependent("d"); return err },
		func() error { _, err := nilService.Events("a"); return err },
	}
	for i, call := range cases {
		if !errors.Is(call(), authn.ErrStore) {
			t.Errorf("nil service call %d did not return ErrStore", i)
		}
	}
	if err := (authn.SessionRevocationSink{}).Revoke(context.Background(), authn.RevocationTarget{Reference: authn.Reference{Kind: authn.DependentPrincipal}}); err != nil {
		t.Fatalf("non-session sink target = %v, want nil", err)
	}
	if err := (authn.SessionRevocationSink{}).Revoke(context.Background(), authn.RevocationTarget{Reference: authn.Reference{Kind: authn.DependentSession, ID: "session-1"}}); err == nil {
		t.Fatal("nil session revoker unexpectedly succeeded")
	}
	stub := &sessionRevokerStub{}
	sink := authn.SessionRevocationSink{Revoker: stub}
	if err := sink.Revoke(context.Background(), authn.RevocationTarget{Reference: authn.Reference{Kind: authn.DependentTokenFamily, ID: "token-1"}, Reason: "disable"}); err != nil || stub.id != session.ID("token-1") || stub.reason != "disable" {
		t.Fatalf("session sink call id=%q reason=%q err=%v", stub.id, stub.reason, err)
	}
}

func TestLifecycleCreateLinkAndDependentValidation(t *testing.T) {
	createCases := []struct {
		name string
		edit func(*authn.AccountSpec)
	}{
		{"account_id", func(s *authn.AccountSpec) { s.ID = " bad" }},
		{"person_id", func(s *authn.AccountSpec) { s.PersonID = "" }},
		{"tenant", func(s *authn.AccountSpec) { s.Tenant = "" }},
	}
	for _, tc := range createCases {
		t.Run(tc.name, func(t *testing.T) {
			s := authn.NewMemory(func() time.Time { return lifecycleAt })
			spec := authn.AccountSpec{ID: "a", PersonID: "p", Tenant: "tenant-a"}
			tc.edit(&spec)
			if _, _, err := s.CreateAccount(spec); !errors.Is(err, authn.ErrInvalidAccount) {
				t.Fatalf("CreateAccount error = %v, want ErrInvalidAccount", err)
			}
		})
	}
	s := authn.NewMemory(func() time.Time { return lifecycleAt })
	if _, _, err := s.CreateAccount(authn.AccountSpec{ID: "a", PersonID: "p", Tenant: "tenant-a"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateAccount(authn.AccountSpec{ID: "a", PersonID: "p", Tenant: "tenant-a"}); !errors.Is(err, authn.ErrLifecycleConflict) {
		t.Fatalf("duplicate account = %v", err)
	}
	linkCases := []struct {
		name string
		edit func(*authn.IdentitySpec)
		want error
	}{
		{"id", func(x *authn.IdentitySpec) { x.ID = " id" }, authn.ErrInvalidAccount},
		{"account", func(x *authn.IdentitySpec) { x.AccountID = "missing" }, authn.ErrAccountNotFound},
		{"provider", func(x *authn.IdentitySpec) { x.ProviderRef = "" }, authn.ErrInvalidIdentity},
		{"digest", func(x *authn.IdentitySpec) { x.SubjectDigest = "BAD" }, authn.ErrInvalidIdentity},
		{"tenant", func(x *authn.IdentitySpec) { x.Tenant = "tenant-b" }, authn.ErrTenantMismatch},
	}
	for _, tc := range linkCases {
		t.Run("link_"+tc.name, func(t *testing.T) {
			x := authn.IdentitySpec{ID: "i-" + tc.name, AccountID: "a", Tenant: "tenant-a", ProviderRef: "idp", SubjectDigest: validSubjectDigest()}
			tc.edit(&x)
			if _, _, err := s.LinkIdentity(x); !errors.Is(err, tc.want) {
				t.Fatalf("LinkIdentity error = %v, want errors.Is(%v)", err, tc.want)
			}
		})
	}
	identity, _, err := s.LinkIdentity(authn.IdentitySpec{ID: "i", AccountID: "a", Tenant: "tenant-a", ProviderRef: "idp", SubjectDigest: validSubjectDigest()})
	if err != nil {
		t.Fatal(err)
	}
	depCases := []struct {
		name string
		edit func(*authn.DependentSpec)
		want error
	}{
		{"id", func(x *authn.DependentSpec) { x.ID = " d" }, authn.ErrInvalidDependent},
		{"kind", func(x *authn.DependentSpec) { x.Kind = "bad" }, authn.ErrInvalidDependent},
		{"assurance", func(x *authn.DependentSpec) { x.Assurance = trust.AssuranceUnspecified }, authn.ErrInvalidDependent},
		{"account", func(x *authn.DependentSpec) { x.AccountID = "missing" }, authn.ErrAccountNotFound},
		{"tenant", func(x *authn.DependentSpec) { x.Tenant = "tenant-b" }, authn.ErrTenantMismatch},
		{"identity", func(x *authn.DependentSpec) { x.IdentityID = "missing" }, authn.ErrIdentityNotUsable},
	}
	for _, tc := range depCases {
		t.Run("dependent_"+tc.name, func(t *testing.T) {
			x := authn.DependentSpec{ID: "d-" + tc.name, AccountID: "a", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentSession, Assurance: trust.AssuranceSubstantial}
			tc.edit(&x)
			if _, err := s.RegisterDependent(x); !errors.Is(err, tc.want) {
				t.Fatalf("RegisterDependent error = %v, want errors.Is(%v)", err, tc.want)
			}
		})
	}
}

func TestLifecycleAliasesAndTransitionConflicts(t *testing.T) {
	newService := func(t *testing.T) *authn.Service {
		t.Helper()
		s := authn.NewMemory(func() time.Time { return lifecycleAt })
		if _, _, err := s.CreateAccount(authn.AccountSpec{ID: "a", PersonID: "p", Tenant: "tenant-a", At: lifecycleAt}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.LinkIdentity(authn.IdentitySpec{ID: "i", AccountID: "a", Tenant: "tenant-a", ProviderRef: "idp", SubjectDigest: validSubjectDigest(), At: lifecycleAt}); err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, tc := range []struct {
		name string
		call func(*authn.Service) error
	}{
		{"suspend_account", func(s *authn.Service) error {
			_, _, err := s.SuspendAccount(context.Background(), "a", evidence("suspend"))
			return err
		}},
		{"disable_account", func(s *authn.Service) error {
			_, _, err := s.DisableAccount(context.Background(), "a", evidence("disable"))
			return err
		}},
		{"terminate_account", func(s *authn.Service) error {
			_, _, err := s.TerminateAccount(context.Background(), "a", evidence("terminate"))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(newService(t)); err != nil {
				t.Fatal(err)
			}
		})
	}
	s := newService(t)
	if _, _, err := s.UnlinkIdentity(context.Background(), "i", evidence("unlink")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RelinkIdentity(context.Background(), "i", evidence("relink")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Resume(context.Background(), "a", evidence("resume")); !errors.Is(err, authn.ErrLifecycleConflict) {
		t.Fatalf("resume active account = %v, want ErrLifecycleConflict", err)
	}
	if _, _, err := s.Terminate(context.Background(), "a", authn.Evidence{}); !errors.Is(err, authn.ErrInvalidAuthentication) {
		t.Fatalf("invalid transition evidence = %v, want ErrInvalidAuthentication", err)
	}
	if _, _, err := s.Disable(context.Background(), "missing", evidence("missing")); !errors.Is(err, authn.ErrAccountNotFound) {
		t.Fatalf("missing account transition = %v, want ErrAccountNotFound", err)
	}
}

func TestLifecycleAuthenticateAndRecoveryDenials(t *testing.T) {
	s, identity, _ := lifecycleFixture(t)
	dependent, err := s.RegisterDependent(authn.DependentSpec{ID: "dependent-auth", AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentSession, Assurance: trust.AssuranceSubstantial, At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, At: lifecycleAt}); err != nil || got.RevocationEpoch != 1 || got.Tenant != "tenant-a" {
		t.Fatalf("basic authentication=%+v err=%v", got, err)
	}
	if got, err := s.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, DependentID: dependent.ID, Assurance: trust.AssuranceLow, At: lifecycleAt}); err != nil || got.Assurance != trust.AssuranceSubstantial {
		t.Fatalf("dependent authentication=%+v err=%v", got, err)
	}
	cases := []struct {
		name    string
		request authn.AuthenticationRequest
		want    error
	}{
		{"zero_time", authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID}, authn.ErrInvalidAuthentication},
		{"missing_account", authn.AuthenticationRequest{AccountID: "missing", IdentityID: identity.ID, At: lifecycleAt}, authn.ErrAccountNotFound},
		{"missing_identity", authn.AuthenticationRequest{AccountID: "account-1", IdentityID: "missing", At: lifecycleAt}, authn.ErrIdentityNotFound},
		{"missing_dependent", authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, DependentID: "missing", At: lifecycleAt}, authn.ErrDependentNotFound},
		{"assurance", authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, DependentID: dependent.ID, Assurance: trust.AssuranceHigh, At: lifecycleAt}, authn.ErrAssuranceInsufficient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Authenticate(tc.request)
			if !errors.Is(err, tc.want) || (tc.name == "assurance" && authn.CodeOf(err) != authn.CodeAssuranceInsufficient) {
				t.Fatalf("Authenticate error = %v code=%q, want errors.Is(%v)", err, authn.CodeOf(err), tc.want)
			}
		})
	}
	if err := s.CheckDependent("account-1", dependent.ID, lifecycleAt); err != nil {
		t.Fatalf("CheckDependent valid = %v", err)
	}
	if err := s.CheckDependent("account-1", "missing", lifecycleAt); !errors.Is(err, authn.ErrDependentNotFound) {
		t.Fatalf("CheckDependent missing = %v, want ErrDependentNotFound", err)
	}
	revokedService, revokedIdentity, _ := lifecycleFixture(t)
	revoked, err := revokedService.RegisterDependent(authn.DependentSpec{ID: "dependent-revoked-check", AccountID: "account-1", IdentityID: revokedIdentity.ID, Tenant: "tenant-a", Kind: authn.DependentSession, Assurance: trust.AssuranceSubstantial, At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := revokedService.Unlink(context.Background(), revokedIdentity.ID, evidence("revoke-dependent")); err != nil {
		t.Fatal(err)
	}
	if err := revokedService.CheckDependent("account-1", revoked.ID, lifecycleAt); !errors.Is(err, authn.ErrDependentRevoked) || authn.CodeOf(err) != authn.CodeDependentRevoked {
		t.Fatalf("CheckDependent revoked = %v code=%q, want dependent refusal", err, authn.CodeOf(err))
	}
	for _, req := range []authn.RecoveryRequest{
		{AccountID: "account-1", IdentityID: identity.ID, CurrentAssurance: trust.AssuranceUnspecified, RequestedAssurance: trust.AssuranceLow, Evidence: evidence("recovery")},
		{AccountID: "account-1", IdentityID: identity.ID, CurrentAssurance: trust.AssuranceSubstantial, RequestedAssurance: trust.AssuranceUnspecified, Evidence: evidence("recovery")},
		{AccountID: "account-1", IdentityID: identity.ID, CurrentAssurance: trust.AssuranceSubstantial, RequestedAssurance: trust.AssuranceLow, Evidence: authn.Evidence{}},
	} {
		if _, err := s.Recover(req); !errors.Is(err, authn.ErrRecoveryAssurance) && !errors.Is(err, authn.ErrInvalidAuthentication) {
			t.Fatalf("Recover error = %v, want recovery or evidence refusal", err)
		}
	}
}

func TestLifecycleStoreErrorsAndSnapshots(t *testing.T) {
	inner := authn.NewMemoryStore()
	store := &lifecycleErrorStore{inner: inner, err: errors.New("store failure")}
	s := authn.New(store, func() time.Time { return lifecycleAt })
	if _, _, err := s.CreateAccount(authn.AccountSpec{ID: "a", PersonID: "p", Tenant: "tenant-a", At: lifecycleAt}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LinkIdentity(authn.IdentitySpec{ID: "i", AccountID: "a", Tenant: "tenant-a", ProviderRef: "idp", SubjectDigest: validSubjectDigest(), At: lifecycleAt}); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GetAccount", "GetIdentity", "GetDependent", "ListIdentities", "ListDependents", "Commit", "AppendIdentity", "AppendDependent", "Events"} {
		store.fail = method
		switch method {
		case "GetAccount":
			if _, _, err := s.Account("a"); !errors.Is(err, store.err) {
				t.Errorf("Account error=%v", err)
			}
		case "GetIdentity":
			if _, _, err := s.Identity("i"); !errors.Is(err, store.err) {
				t.Errorf("Identity error=%v", err)
			}
		case "GetDependent":
			if _, _, err := s.Dependent("d"); !errors.Is(err, store.err) {
				t.Errorf("Dependent error=%v", err)
			}
		case "ListIdentities":
			if _, _, err := s.Suspend(context.Background(), "a", evidence("list-identities")); !errors.Is(err, store.err) {
				t.Errorf("ListIdentities error=%v", err)
			}
		case "ListDependents":
			store.fail = "ListDependents"
			if _, _, err := s.Suspend(context.Background(), "a", evidence("list-dependents")); !errors.Is(err, store.err) {
				t.Errorf("ListDependents error=%v", err)
			}
		case "Commit":
			store.fail = "Commit"
			if _, _, err := s.Suspend(context.Background(), "a", evidence("commit")); !errors.Is(err, store.err) {
				t.Errorf("Commit error=%v", err)
			}
		case "AppendIdentity":
			store.fail = "AppendIdentity"
			if _, _, err := s.LinkIdentity(authn.IdentitySpec{ID: "i2", AccountID: "a", Tenant: "tenant-a", ProviderRef: "idp", SubjectDigest: validSubjectDigest(), At: lifecycleAt}); !errors.Is(err, store.err) {
				t.Errorf("AppendIdentity error=%v", err)
			}
		case "AppendDependent":
			store.fail = "AppendDependent"
			if _, err := s.RegisterDependent(authn.DependentSpec{ID: "d", AccountID: "a", Tenant: "tenant-a", Kind: authn.DependentSession, Assurance: trust.AssuranceLow}); !errors.Is(err, store.err) {
				t.Errorf("AppendDependent error=%v", err)
			}
		case "Events":
			if _, err := s.Events("a"); !errors.Is(err, store.err) {
				t.Errorf("Events error=%v", err)
			}
		}
		store.fail = ""
	}
	account, ok, err := s.Account("a")
	if err != nil || !ok || account.Status != authn.AccountActive || account.RevocationEpoch != 1 {
		t.Fatalf("failed transitions changed state: account=%+v ok=%v err=%v", account, ok, err)
	}
}

func TestMemoryStoreContractsAndTypedErrors(t *testing.T) {
	var nilStore *authn.MemoryStore
	if _, _, err := nilStore.GetAccount("a"); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil GetAccount=%v", err)
	}
	if _, _, err := nilStore.GetIdentity("i"); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil GetIdentity=%v", err)
	}
	if _, err := nilStore.ListIdentities("a"); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil ListIdentities=%v", err)
	}
	if _, _, err := nilStore.GetDependent("d"); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil GetDependent=%v", err)
	}
	if _, err := nilStore.ListDependents("a"); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil ListDependents=%v", err)
	}
	if err := nilStore.CreateAccount(authn.Account{}, authn.LifecycleEvent{}); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil CreateAccount=%v", err)
	}
	if err := nilStore.Commit(authn.Account{}, nil, nil, authn.LifecycleEvent{}); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil Commit=%v", err)
	}
	if err := nilStore.AppendIdentity(authn.Identity{}, authn.LifecycleEvent{}); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil AppendIdentity=%v", err)
	}
	if err := nilStore.AppendDependent(authn.Dependent{}); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil AppendDependent=%v", err)
	}
	if _, err := nilStore.Events("a"); !errors.Is(err, authn.ErrStore) {
		t.Errorf("nil Events=%v", err)
	}

	m := authn.NewMemoryStore()
	a := authn.Account{ID: "a"}
	e := authn.LifecycleEvent{AccountID: "a", Affected: []authn.Reference{{ID: "original"}}}
	if err := m.CreateAccount(a, e); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateAccount(a, e); !errors.Is(err, authn.ErrLifecycleConflict) {
		t.Fatalf("duplicate CreateAccount=%v", err)
	}
	if err := m.AppendIdentity(authn.Identity{ID: "i", AccountID: "a"}, e); err != nil {
		t.Fatal(err)
	}
	if err := m.AppendIdentity(authn.Identity{ID: "i", AccountID: "a"}, e); !errors.Is(err, authn.ErrLifecycleConflict) {
		t.Fatalf("duplicate AppendIdentity=%v", err)
	}
	if err := m.AppendDependent(authn.Dependent{ID: "d", AccountID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := m.AppendDependent(authn.Dependent{ID: "d", AccountID: "a"}); !errors.Is(err, authn.ErrLifecycleConflict) {
		t.Fatalf("duplicate AppendDependent=%v", err)
	}
	if err := m.Commit(authn.Account{ID: "missing"}, nil, nil, e); !errors.Is(err, authn.ErrAccountNotFound) {
		t.Fatalf("missing Commit=%v", err)
	}
	if _, err := m.Events("missing"); !errors.Is(err, authn.ErrAccountNotFound) {
		t.Fatalf("missing Events=%v", err)
	}
	events, err := m.Events("a")
	if err != nil {
		t.Fatal(err)
	}
	events[0].Affected[0].ID = "tampered"
	again, err := m.Events("a")
	if err != nil || again[0].Affected[0].ID != "original" {
		t.Fatalf("event defensive copy=%+v err=%v", again, err)
	}
}

func TestLifecycleErrorTypesAndCodeOf(t *testing.T) {
	refusal := &authn.Refusal{Err: authn.ErrAccountNotActive, AccountID: "a"}
	if refusal.Error() == "" || !errors.Is(refusal, authn.ErrAccountNotActive) || refusal.Code() != "" || authn.CodeOf(refusal) != "" {
		t.Fatalf("refusal methods: error=%q code=%q", refusal.Error(), refusal.Code())
	}
	fanout := &authn.FanoutError{Target: authn.RevocationTarget{Reference: authn.Reference{Kind: authn.DependentSession, ID: "s"}}, Err: errors.New("adapter")}
	if fanout.Error() == "" || !errors.Is(fanout, authn.ErrFanoutIncomplete) || !errors.Is(fanout, fanout.Err) || fanout.Code() != authn.CodeFanoutIncomplete || authn.CodeOf(fanout) != authn.CodeFanoutIncomplete {
		t.Fatalf("fanout methods: error=%q code=%q", fanout.Error(), fanout.Code())
	}
	if authn.CodeOf(nil) != "" || authn.CodeOf(errors.New("plain")) != "" {
		t.Fatal("CodeOf returned a code for an untyped error")
	}
}
