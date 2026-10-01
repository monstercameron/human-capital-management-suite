package install

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testMembership struct {
	mu              sync.Mutex
	snapshot        MembershipSnapshot
	snapshotStarted chan struct{}
	releaseSnapshot chan struct{}
}

func (m *testMembership) Snapshot(context.Context, string, string) (MembershipSnapshot, error) {
	m.mu.Lock()
	snapshot := cloneMembership(m.snapshot)
	m.mu.Unlock()
	if m.snapshotStarted != nil {
		close(m.snapshotStarted)
	}
	if m.releaseSnapshot != nil {
		<-m.releaseSnapshot
	}
	return snapshot, nil
}

type testBilateral struct{ allowed bool }

func (p testBilateral) AllowsPersona(context.Context, string, string, string) (bool, error) {
	return p.allowed, nil
}

type testManager bool

func (m testManager) IsChannelManager(context.Context, string, string, string) (bool, error) {
	return bool(m), nil
}

type testProjector struct{ pauses, voids, expires atomic.Int32 }

func (p *testProjector) Pause(context.Context, PauseRequest) error {
	p.pauses.Add(1)
	return nil
}
func (p *testProjector) VoidApprovals(context.Context, PauseRequest) error {
	p.voids.Add(1)
	return nil
}
func (p *testProjector) ExpireCards(context.Context, PauseRequest) error {
	p.expires.Add(1)
	return nil
}

type testOwnerStatus struct{ status OwnerStatus }

func (o testOwnerStatus) ResolveOwner(context.Context, string, string, string) (OwnerStatus, error) {
	return o.status, nil
}

type testNotifier struct {
	mu            sync.Mutex
	notifications []Notification
}

func (n *testNotifier) Notify(_ context.Context, notification Notification) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notifications = append(n.notifications, notification)
	return nil
}

func personaFixture() PersonaVersion {
	return PersonaVersion{TenantID: "tenant-a", PersonaID: "comp-analyst", Version: 1, Lifecycle: LifecyclePublished, OwnerID: "owner-a", StewardID: "steward-a", AllowedChannelClasses: []ChannelClass{ChannelPrivate, ChannelOneToOne}, TierCeiling: TierT1, DataClasses: []string{"compensation.band", "compensation.salary"}, AlwaysPrivate: true, DailySpendCeiling: 10}
}

func policyFixture() ChannelPersonaPolicy {
	return ChannelPersonaPolicy{MaxTier: TierT1, AllowedDataClasses: []string{"compensation.band", "compensation.salary"}, AlwaysPrivate: true, AllowedChannelClasses: []ChannelClass{ChannelPrivate, ChannelOneToOne}, AllowExternalMembers: true}
}

func installFixture(t *testing.T, cfg Config) (*Service, Installation) {
	t.Helper()
	if cfg.Manager == nil {
		cfg.Manager = testManager(true)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	persona := personaFixture()
	installation, err := s.Install(context.Background(), InstallRequest{Persona: persona, Conversation: Conversation{TenantID: persona.TenantID, ConversationID: "room-a", Class: ChannelPrivate}, Policy: policyFixture(), InstallerID: "manager-a", ChannelManager: true})
	if err != nil {
		t.Fatal(err)
	}
	return s, installation
}

func TestTodo_AGENTP_007(t *testing.T) {
	membership := &testMembership{snapshot: MembershipSnapshot{Revision: 1, AllowedDataClasses: []string{"compensation.band", "compensation.salary"}}}
	s, installation := installFixture(t, Config{Membership: membership, Bilateral: testBilateral{allowed: true}, Limits: Limits{DailySpendMicros: 100}})
	second, err := s.Install(context.Background(), InstallRequest{Persona: personaFixture(), Conversation: Conversation{TenantID: "tenant-a", ConversationID: "room-a", Class: ChannelPrivate}, Policy: policyFixture(), InstallerID: "manager-a", ChannelManager: true})
	if err != nil {
		t.Fatalf("second installation: %v", err)
	}
	if second.ID == installation.ID {
		t.Fatal("independent persona installations reused an id")
	}
	if _, err := s.Install(context.Background(), InstallRequest{Persona: personaFixture(), Conversation: Conversation{TenantID: "tenant-a", ConversationID: "company", Class: ChannelPublic}, Policy: policyFixture(), InstallerID: "manager-a", ChannelManager: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("compensation persona entered an all-company channel: %v", err)
	}
	membership.mu.Lock()
	membership.snapshot = MembershipSnapshot{Revision: 2, ExternalMembers: true, AllowedDataClasses: []string{"compensation.band"}}
	membership.mu.Unlock()
	if err := s.UpdateMembership("tenant-a", "room-a", membership.snapshot); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMembership("tenant-a", "room-a", MembershipSnapshot{Revision: 2, AllowedDataClasses: []string{"compensation.band", "compensation.salary"}}); !errors.Is(err, ErrFenced) {
		t.Fatalf("conflicting snapshot reused an existing membership revision: %v", err)
	}
	decision, err := s.EffectivePolicy(context.Background(), installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Policy.AllowedDataClasses[0] != "compensation.band" || len(decision.Policy.AllowedDataClasses) != 1 || decision.MembershipRevision != 2 {
		t.Fatalf("membership change did not narrow the effective policy: %+v", decision)
	}
	if err := s.RemoveInstallation(context.Background(), installation.ID, "manager-a"); err != nil {
		t.Fatal(err)
	}
	remaining, err := s.Installation(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.Status != InstallationActive {
		t.Fatalf("removing one installation changed its sibling: %+v", remaining)
	}
}

func TestTodo_AGENTP_007_ManagerAuthority(t *testing.T) {
	p := personaFixture()
	conversation := Conversation{TenantID: p.TenantID, ConversationID: "room-a", Class: ChannelPrivate}
	request := InstallRequest{Persona: p, Conversation: conversation, Policy: policyFixture(), InstallerID: "manager-a", ChannelManager: true}

	service, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), request); !errors.Is(err, ErrDenied) {
		t.Fatalf("caller-supplied manager flag authorized installation: %v", err)
	}

	service, err = New(Config{Manager: testManager(false)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), request); !errors.Is(err, ErrDenied) {
		t.Fatalf("non-manager was allowed to install persona: %v", err)
	}
}

func TestTodo_AGENTP_007_PolicyCeilings(t *testing.T) {
	p := personaFixture()
	p.ConversationSearch = true
	policy := policyFixture()
	policy.AlwaysPrivate = false
	policy.ConversationSearchAllowed = false
	service, err := New(Config{Manager: testManager(true)})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := service.Install(context.Background(), InstallRequest{Persona: p, Conversation: Conversation{TenantID: p.TenantID, ConversationID: "room-a", Class: ChannelPrivate}, Policy: policy, InstallerID: "manager-a"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := service.EffectivePolicy(context.Background(), installed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Policy.AlwaysPrivate {
		t.Fatal("persona's always-private ceiling was lost in the installation policy")
	}
	if decision.Policy.ConversationSearchAllowed {
		t.Fatal("channel policy widened conversation search despite denying it")
	}
}

func TestTodo_AGENTP_007_ExternalAudienceFloor(t *testing.T) {
	membership := &testMembership{snapshot: MembershipSnapshot{Revision: 2, ExternalMembers: true, AllowedDataClasses: []string{"compensation.band"}}}
	p := personaFixture()
	p.AllowedChannelClasses = []ChannelClass{ChannelExternal}
	policy := policyFixture()
	policy.AllowedChannelClasses = []ChannelClass{ChannelExternal}
	policy.AllowExternalMembers = true
	service, err := New(Config{Manager: testManager(true), Bilateral: testBilateral{allowed: true}, Membership: membership})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := service.Install(context.Background(), InstallRequest{Persona: p, Conversation: Conversation{TenantID: p.TenantID, ConversationID: "partner-room", Class: ChannelExternal, ExternalMembers: true}, Policy: policy, InstallerID: "manager-a"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := service.EffectivePolicy(context.Background(), installed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Policy.AllowedDataClasses) != 1 || decision.Policy.AllowedDataClasses[0] != "compensation.band" {
		t.Fatalf("external audience did not narrow data classes: %+v", decision.Policy.AllowedDataClasses)
	}

	membership.mu.Lock()
	membership.snapshot = MembershipSnapshot{Revision: 3, ExternalMembers: true}
	membership.mu.Unlock()
	decision, err = service.EffectivePolicy(context.Background(), installed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Policy.AllowedDataClasses) != 0 {
		t.Fatalf("external membership without an audience floor retained data classes: %+v", decision.Policy.AllowedDataClasses)
	}
}

func TestTodo_AGENTP_007_Golden(t *testing.T) {
	_, installation := installFixture(t, Config{Limits: Limits{DailySpendMicros: 100}})
	got, err := json.Marshal(struct {
		Tenant, Persona, Conversation, Installer, Status string
		Version, Revision, Epoch                         uint64
	}{installation.TenantID, installation.PersonaID, installation.ConversationID, installation.InstallerID, string(installation.Status), installation.PersonaVersion, installation.Revision, installation.RevocationEpoch})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"Tenant":"tenant-a","Persona":"comp-analyst","Conversation":"room-a","Installer":"manager-a","Status":"ACTIVE","Version":1,"Revision":1,"Epoch":1}`
	if string(got) != want {
		t.Fatalf("installation golden changed:\n got %s\nwant %s", got, want)
	}
}

func TestTodo_AGENTP_007_Security(t *testing.T) {
	s, err := New(Config{Manager: testManager(true), Bilateral: testBilateral{allowed: false}})
	if err != nil {
		t.Fatal(err)
	}
	p := personaFixture()
	p.AllowedChannelClasses = []ChannelClass{ChannelExternal}
	policy := policyFixture()
	policy.AllowedChannelClasses = []ChannelClass{ChannelExternal}
	policy.AllowExternalMembers = true
	if _, err := s.Install(context.Background(), InstallRequest{Persona: p, Conversation: Conversation{TenantID: p.TenantID, ConversationID: "external", Class: ChannelExternal, ExternalMembers: true}, Policy: policy, InstallerID: "manager-a", ChannelManager: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("external installation bypassed bilateral policy: %v", err)
	}
	p.AllowedChannelClasses = []ChannelClass{ChannelCrossCompany}
	crossCompanyPolicy := policyFixture()
	crossCompanyPolicy.AllowedChannelClasses = []ChannelClass{ChannelCrossCompany}
	crossCompanyPolicy.AllowCrossCompanyMembers = true
	if _, err := s.Install(context.Background(), InstallRequest{Persona: p, Conversation: Conversation{TenantID: p.TenantID, ConversationID: "cross-company", Class: ChannelCrossCompany, CrossCompanyMembers: true}, Policy: crossCompanyPolicy, InstallerID: "manager-a"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-company installation bypassed bilateral policy: %v", err)
	}
	approved, err := New(Config{Manager: testManager(true), Bilateral: testBilateral{allowed: true}})
	if err != nil {
		t.Fatal(err)
	}
	policy.AllowExternalMembers = false
	if _, err := approved.Install(context.Background(), InstallRequest{Persona: p, Conversation: Conversation{TenantID: p.TenantID, ConversationID: "external-policy", Class: ChannelExternal}, Policy: policy, InstallerID: "manager-a"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("external conversation class bypassed the channel external-membership ceiling: %v", err)
	}
	p.AllowedChannelClasses = []ChannelClass{ChannelExternal}
	if _, err := s.Install(context.Background(), InstallRequest{Persona: p, Conversation: Conversation{TenantID: "tenant-b", ConversationID: "foreign", Class: ChannelExternal, ExternalMembers: true}, Policy: policy, InstallerID: "manager-a", ChannelManager: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-tenant installation was accepted: %v", err)
	}
	policy = policyFixture()
	policy.MaxTier = TierT2
	if _, err := s.Install(context.Background(), InstallRequest{Persona: personaFixture(), Conversation: Conversation{TenantID: "tenant-a", ConversationID: "tier-room", Class: ChannelPrivate}, Policy: policy, InstallerID: "manager-a"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("channel tier exceeded the persona ceiling: %v", err)
	}
	policy = policyFixture()
	policy.AllowedDataClasses = append(policy.AllowedDataClasses, "compensation.bank_account")
	if _, err := s.Install(context.Background(), InstallRequest{Persona: personaFixture(), Conversation: Conversation{TenantID: "tenant-a", ConversationID: "data-room", Class: ChannelPrivate}, Policy: policy, InstallerID: "manager-a"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("channel data classes exceeded persona reach: %v", err)
	}
}

func TestTodo_AGENTP_007_Race(t *testing.T) {
	s, _ := installFixture(t, Config{Limits: Limits{DailySpendMicros: 100}})
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := s.Install(context.Background(), InstallRequest{Persona: personaFixture(), Conversation: Conversation{TenantID: "tenant-a", ConversationID: "race-room", Class: ChannelPrivate}, Policy: policyFixture(), InstallerID: "manager-a", ChannelManager: true}); err == nil {
				successes.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() != 5 {
		t.Fatalf("active installation ceiling was not atomic: %d", successes.Load())
	}

	// Force the membership source read to race a committed membership callback.
	// BeginStep must use the newer audience floor before it admits the step.
	membership := &testMembership{
		snapshot: MembershipSnapshot{Revision: 1, AllowedDataClasses: []string{"compensation.band", "compensation.salary"}},
	}
	s, installation := installFixture(t, Config{Membership: membership, Bilateral: testBilateral{allowed: true}, Limits: Limits{DailySpendMicros: 100}})
	membership.snapshotStarted = make(chan struct{})
	membership.releaseSnapshot = make(chan struct{})
	type stepResult struct {
		decision PolicyDecision
		err      error
	}
	result := make(chan stepResult, 1)
	go func() {
		decision, err := s.BeginStep(context.Background(), installation.ID)
		result <- stepResult{decision: decision, err: err}
	}()
	<-membership.snapshotStarted
	if err := s.UpdateMembership("tenant-a", "room-a", MembershipSnapshot{Revision: 2, ExternalMembers: true, AllowedDataClasses: []string{"compensation.band"}}); err != nil {
		t.Fatal(err)
	}
	close(membership.releaseSnapshot)
	step := <-result
	if step.err != nil {
		t.Fatalf("step failed after recomputing membership policy: %v", step.err)
	}
	if step.decision.MembershipRevision != 2 || len(step.decision.Policy.AllowedDataClasses) != 1 || step.decision.Policy.AllowedDataClasses[0] != "compensation.band" {
		t.Fatalf("invocation committed against stale audience policy: %+v", step.decision)
	}
}

func TestTodo_AGENTP_015(t *testing.T) {
	clock := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	s, installation := installFixture(t, Config{Clock: func() time.Time { return clock }, Limits: Limits{PerInvokerPerHour: 2, PerInvokerConcurrent: 1, PerConversationPerHour: 3, DailySpendMicros: 10}})
	req := MentionRequest{TenantID: "tenant-a", InvokerID: "manager-a", PersonaID: "comp-analyst", ConversationID: "room-a", InstallationID: installation.ID, EstimatedSpend: 6, At: clock}
	lease, err := s.AdmitMention(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitMention(context.Background(), req); !errors.Is(err, ErrLimit) {
		t.Fatalf("concurrent ceiling did not refuse before work: %v", err)
	} else {
		var typed *LimitError
		if !errors.As(err, &typed) || !typed.Ephemeral || typed.Reason != ReasonInvokerConcurrency {
			t.Fatalf("limit denial was not typed and ephemeral: %v", err)
		}
	}
	if err := lease.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Settle(6); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitMention(context.Background(), MentionRequest{TenantID: "tenant-a", InvokerID: "manager-a", PersonaID: "comp-analyst", ConversationID: "room-a", InstallationID: installation.ID, EstimatedSpend: 5, At: clock}); !errors.Is(err, ErrLimit) {
		t.Fatalf("daily spend ceiling was not enforced: %v", err)
	}
}

func TestTodo_AGENTP_015_Golden(t *testing.T) {
	err := &LimitError{Reason: ReasonConversationRate, RetryAfter: time.Hour, Ephemeral: true}
	got, marshalErr := json.Marshal(struct {
		Reason    string `json:"reason"`
		Retry     int64  `json:"retry_after_seconds"`
		Ephemeral bool   `json:"ephemeral"`
	}{string(err.Reason), int64(err.RetryAfter / time.Second), err.Ephemeral})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if string(got) != `{"reason":"PER_CONVERSATION_RATE","retry_after_seconds":3600,"ephemeral":true}` {
		t.Fatalf("limit golden changed: %s", got)
	}
}

func TestTodo_AGENTP_015_Property(t *testing.T) {
	clock := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	s, installation := installFixture(t, Config{Clock: func() time.Time { return clock }, Limits: Limits{PerInvokerPerHour: 30, PerInvokerConcurrent: 3, PerConversationPerHour: 120, DailySpendMicros: 1000}})
	admitted := 0
	for i := 0; i < 200; i++ {
		lease, err := s.AdmitMention(context.Background(), MentionRequest{TenantID: "tenant-a", InvokerID: "u", PersonaID: "comp-analyst", ConversationID: "room-a", InstallationID: installation.ID, EstimatedSpend: 1, At: clock})
		if err != nil {
			continue
		}
		admitted++
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if admitted != 30 {
		t.Fatalf("admitted %d invocations, want exactly the per-invoker ceiling", admitted)
	}
}

func TestTodo_AGENTP_015_Race(t *testing.T) {
	s, installation := installFixture(t, Config{Limits: Limits{PerInvokerPerHour: 100, PerInvokerConcurrent: 3, PerConversationPerHour: 100, DailySpendMicros: 1000}})
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	type admission struct {
		err error
	}
	for round := 0; round < 20; round++ {
		start := make(chan struct{})
		finish := make(chan struct{})
		results := make(chan admission, 20)
		var wg sync.WaitGroup
		for i := 0; i < cap(results); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				lease, err := s.AdmitMention(context.Background(), MentionRequest{TenantID: "tenant-a", InvokerID: "u", PersonaID: "comp-analyst", ConversationID: "room-a", InstallationID: installation.ID, EstimatedSpend: 1})
				if err == nil {
					current := inFlight.Add(1)
					for {
						old := maxInFlight.Load()
						if current <= old || maxInFlight.CompareAndSwap(old, current) {
							break
						}
					}
				}
				results <- admission{err: err}
				if err == nil {
					<-finish
					inFlight.Add(-1)
					if releaseErr := lease.Release(); releaseErr != nil {
						t.Errorf("release admitted mention: %v", releaseErr)
					}
				}
			}()
		}
		close(start)
		admitted := 0
		for i := 0; i < cap(results); i++ {
			result := <-results
			if result.err == nil {
				admitted++
				continue
			}
			var limit *LimitError
			if !errors.As(result.err, &limit) || limit.Reason != ReasonInvokerConcurrency {
				close(finish)
				wg.Wait()
				t.Fatalf("round %d refusal was not the concurrency limit: %v", round, result.err)
			}
		}
		close(finish)
		wg.Wait()
		if admitted != 3 {
			t.Fatalf("round %d admitted %d concurrent mentions, want 3", round, admitted)
		}
	}
	if maxInFlight.Load() > 3 {
		t.Fatalf("concurrent mentions exceeded ceiling: %d", maxInFlight.Load())
	}
}

func TestTodo_AGENTP_016(t *testing.T) {
	projector := &testProjector{}
	s, installation := installFixture(t, Config{Projector: projector, Limits: Limits{DailySpendMicros: 100}})
	if err := s.RegisterTask(Task{ID: "task-1", TenantID: "tenant-a", PersonaID: "comp-analyst", InstallationID: installation.ID, State: TaskWaiting, ApprovalOpen: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginStep(context.Background(), installation.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SuspendInstallation(context.Background(), installation.ID, "ADMIN_KILL"); err != nil {
		t.Fatal(err)
	}
	task, err := s.Task("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.State != TaskPaused || task.ApprovalOpen || task.CardExpiresAt.IsZero() {
		t.Fatalf("suspend did not pause and expire task state: %+v", task)
	}
	if _, err := s.BeginStep(context.Background(), installation.ID); !errors.Is(err, ErrUnavailable) && !errors.Is(err, ErrFenced) {
		t.Fatalf("suspended installation started a step: %v", err)
	}
	if projector.pauses.Load() != 1 || projector.voids.Load() != 1 || projector.expires.Load() != 1 {
		t.Fatalf("lifecycle projection incomplete: pauses=%d voids=%d expires=%d", projector.pauses.Load(), projector.voids.Load(), projector.expires.Load())
	}
}

func TestTodo_AGENTP_016_Golden(t *testing.T) {
	s, installation := installFixture(t, Config{Limits: Limits{DailySpendMicros: 100}})
	if err := s.SuspendInstallation(context.Background(), installation.ID, "ADMIN_KILL"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Installation(context.Background(), installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(struct {
		Status   string `json:"status"`
		Revision uint64 `json:"revision"`
		Epoch    uint64 `json:"epoch"`
	}{string(got.Status), got.Revision, got.RevocationEpoch})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"status":"SUSPENDED","revision":2,"epoch":2}` {
		t.Fatalf("fence golden changed: %s", data)
	}
}

func TestTodo_AGENTP_016_Security(t *testing.T) {
	s, first := installFixture(t, Config{Limits: Limits{DailySpendMicros: 100}})
	otherPersona := personaFixture()
	otherPersona.PersonaID = "policy-helper"
	second, err := s.Install(context.Background(), InstallRequest{Persona: otherPersona, Conversation: Conversation{TenantID: "tenant-a", ConversationID: "room-a", Class: ChannelPrivate}, Policy: policyFixture(), InstallerID: "manager-a", ChannelManager: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SuspendPersona(context.Background(), "tenant-a", first.PersonaID, "PERSONA_SUSPENDED"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginStep(context.Background(), second.ID); err != nil {
		t.Fatalf("suspending one persona affected another installation: %v", err)
	}
}

func TestTodo_AGENTP_016_Race(t *testing.T) {
	s, installation := installFixture(t, Config{Limits: Limits{DailySpendMicros: 100}})
	var wg sync.WaitGroup
	var startedAfterSuspend atomic.Int32
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_ = s.SuspendInstallation(context.Background(), installation.ID, "RACE_KILL")
	}()
	go func() {
		defer wg.Done()
		<-start
		if err := s.SuspendInstallation(context.Background(), installation.ID, "RACE_KILL"); err != nil {
			return
		}
		if _, err := s.BeginStep(context.Background(), installation.ID); err == nil {
			startedAfterSuspend.Add(1)
		}
	}()
	close(start)
	wg.Wait()
	if startedAfterSuspend.Load() != 0 {
		t.Fatal("a step began after the suspend commit")
	}
}

func TestTodo_AGENTP_016_Fault(t *testing.T) {
	clock := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	notifier := &testNotifier{}
	s, installation := installFixture(t, Config{Clock: func() time.Time { return clock }, OwnerStatus: testOwnerStatus{status: OwnerStatus{HasOwnerRole: false, InvalidSince: clock.Add(-15 * 24 * time.Hour)}}, Notifier: notifier, Limits: Limits{DailySpendMicros: 100}})
	if err := s.SweepOrphans(context.Background(), clock); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Installation(context.Background(), installation.ID); err != nil || got.Status != InstallationSuspended {
		t.Fatalf("orphan sweep left persona active: %+v err=%v", got, err)
	}
	notifier.mu.Lock()
	count := len(notifier.notifications)
	notifier.mu.Unlock()
	if count != 1 {
		t.Fatalf("orphan sweep did not notify administrators: %d", count)
	}
}
