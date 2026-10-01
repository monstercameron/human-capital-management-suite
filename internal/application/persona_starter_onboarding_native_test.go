package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workerlifecyclestore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type nativeOnboardingSource struct {
	snapshot workerlifecyclestore.Snapshot
	calls    int
	err      error
}

func (s *nativeOnboardingSource) Get(_ context.Context, worker values.EntityRef) (workerlifecyclestore.Snapshot, error) {
	s.calls++
	return s.snapshot, s.err
}

type nativeOnboardingChannels struct {
	room  chatstore.PersonaChannelPolicySnapshot
	calls int
	err   error
}

func (s *nativeOnboardingChannels) CapturePersonaChannelPolicy(_ context.Context, tenant, channel, manager string) (chatstore.PersonaChannelPolicySnapshot, error) {
	s.calls++
	return s.room, s.err
}

func nativeOnboardingFixture(t *testing.T) (*NativePersonaOnboardingPort, *nativeOnboardingSource, *nativeOnboardingChannels, context.Context, *trust.Principal) {
	t.Helper()
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	date, _ := values.ParseLocalDate("2026-09-30")
	ref := func(kind string) values.EntityRef {
		return values.EntityRef{Tenant: "tenant-a", Kind: values.Kind(kind), Id: uuid.NewString()}
	}
	evidence := ref("evidence")
	plan, err := workerlifecycle.NewPlan(workerlifecycle.WorkerLifecyclePlan{Worker: ref("worker"), Employment: ref("employment"), Proposal: ref("proposal"), Event: workerlifecycle.EventStart, EventDate: date, Completion: workerlifecycle.CompleteAllRequired, Requirements: []workerlifecycle.Requirement{{ID: "identity", Ordinal: 1, Owner: "people", Due: workerlifecycle.DueRule{Calendar: values.CalendarRef{Ref: "gregorian", Version: "1"}}, Evidence: []values.EntityRef{evidence}, VerificationPolicy: ref("evidence_policy"), Completion: workerlifecycle.CompleteAllRequired, Required: true}}, Children: []workerlifecycle.ChildTemplate{{ID: "identity", Ordinal: 1, IntentType: "hcm.identity.verify", IntentVersion: "1"}, {ID: "training", Ordinal: 2, IntentType: "hcm.learning.assign", IntentVersion: "1", DependsOn: []string{"identity"}}}})
	if err != nil {
		t.Fatal(err)
	}
	request := workerlifecycle.ResolutionRequest{Plan: plan, AsOf: date, Requirements: []workerlifecycle.RequirementInput{{RequirementID: "identity", Kind: workerlifecycle.RequirementIdentity, Protected: true, FreshDays: 30}}, Facts: []workerlifecycle.WorkerFact{{RequirementID: "identity", ObservedAt: date, Evidence: evidence, Summary: "passport details must stay private"}}}
	ready, err := workerlifecycle.ResolveOnboardingReadiness(request)
	if err != nil {
		t.Fatal(err)
	}
	tracker, err := workerlifecycle.NewOnboardingTracker(plan, ready)
	if err != nil {
		t.Fatal(err)
	}
	tracker, _, err = workerlifecycle.EmitDueChildren(tracker, ready)
	if err != nil {
		t.Fatal(err)
	}
	revision, _ := values.NewSequenceRevision("worker-revision", 1)
	source := &nativeOnboardingSource{snapshot: workerlifecyclestore.Snapshot{Request: request, Tracker: tracker, WorkerRevision: revision, Revision: 3, ChannelID: "new-hire-channel", DisplayName: "Taylor"}}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "hr-manager", SubjectKind: trust.SubjectKindHuman, Purposes: []string{"persona-mention"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: at, ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	channels := &nativeOnboardingChannels{room: chatstore.PersonaChannelPolicySnapshot{TenantID: "tenant-a", ConversationID: "new-hire-channel", ManagerID: principal.Subject(), Kind: "PRIVATE_CHANNEL", Revision: 7, PolicyRevision: 2, Policy: chatstore.PersonaChannelPolicy{PlacementClass: "PRIVATE", MaxTier: "T2", AllowedDataClasses: []string{"ONBOARDING"}, AllowedChannelClasses: []string{"PRIVATE"}}}}
	directory := &nativeOnboardingDirectory{roles: []string{string(authz.RoleManager)}, organizations: []string{"people"}, subjects: []agentgate.Subject{{Ref: plan.Worker, Organization: authz.OrgUnitRef{Tenant: "tenant-a", ID: "people"}}}, fields: []authz.FieldID{authz.FieldWorkerNumber}}
	authority, err := NewCurrentPersonaOnboardingAuthority(directory, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	port, err := NewNativePersonaOnboardingPort(source, channels, func() time.Time { return at }, authority)
	if err != nil {
		t.Fatal(err)
	}
	return port, source, channels, trust.WithPrincipal(context.Background(), principal), principal
}

func TestTodo_AGENTP_021_Onboarding_NativeFactsTasksAndGroundedWelcome(t *testing.T) {
	port, source, channels, ctx, principal := nativeOnboardingFixture(t)
	worker := source.snapshot.Request.Plan.Worker
	readiness, err := port.Checklist(ctx, worker)
	if err != nil || readiness.Aggregate != workerlifecycle.AggregateReady || readiness.PlanDigest != source.snapshot.Request.Plan.CanonicalDigest || readiness.Validate() != nil {
		t.Fatalf("checklist: %+v %v", readiness, err)
	}
	if readiness.Results[0].Summary != "REDACTED" || source.snapshot.Request.Facts[0].Summary != "passport details must stay private" {
		t.Fatalf("protected account disclosed or owner source changed: %+v", readiness.Results)
	}
	tasks, err := port.Tasks(ctx, worker)
	if err != nil || len(tasks) != 1 || tasks[0].ID != source.snapshot.Tracker.Children[0].IntentID || tasks[0].Revision != source.snapshot.Tracker.Revision {
		t.Fatalf("durable tasks: %+v %v", tasks, err)
	}
	if source.snapshot.Tracker.Children[1].State != workerlifecycle.StatusChildPending {
		t.Fatal("T0 emitted a pending child")
	}
	draft, err := port.DraftWelcome(ctx, worker)
	if err != nil || !strings.Contains(draft, "Taylor") || !strings.Contains(draft, "2026-09-30") || strings.Contains(draft, "passport") || strings.Contains(draft, "READY") {
		t.Fatalf("grounded private draft: %q %v", draft, err)
	}
	intent, err := port.PrepareWelcomePost(ctx, principal, worker, draft, source.snapshot.ChannelID)
	if err != nil || intent.Worker != worker || intent.Draft != draft || intent.Channel != source.snapshot.ChannelID || channels.calls != 1 || !intent.WorkerRevision.Equal(source.snapshot.WorkerRevision) || intent.SnapshotRevision != source.snapshot.Revision || intent.PlanDigest != source.snapshot.Request.Plan.CanonicalDigest || intent.ChannelRevision != channels.room.Revision || intent.ChannelPolicyRevision != channels.room.PolicyRevision {
		t.Fatalf("post proposal: %+v %v", intent, err)
	}
	// Advancing the actual date re-evaluates owner evidence freshness rather
	// than reusing the persisted request's previous READY answer.
	port.now = func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }
	readiness, err = port.Checklist(ctx, worker)
	if err != nil || readiness.Aggregate != workerlifecycle.AggregateUnknown || len(readiness.Blockers) != 1 {
		t.Fatalf("stale evidence: %+v %v", readiness, err)
	}
}

func TestTodo_AGENTP_021_Onboarding_NativeAuthorizationAndCurrentChannel(t *testing.T) {
	port, source, channels, ctx, principal := nativeOnboardingFixture(t)
	worker := source.snapshot.Request.Plan.Worker
	if _, err := port.Checklist(context.Background(), worker); !errors.Is(err, ErrPersonaOnboardingFacts) || source.calls != 0 {
		t.Fatalf("unauthenticated read: %v calls=%d", err, source.calls)
	}
	foreign := worker
	foreign.Tenant = "tenant-b"
	if _, err := port.Tasks(ctx, foreign); !errors.Is(err, ErrPersonaOnboardingFacts) || source.calls != 0 {
		t.Fatalf("foreign read: %v calls=%d", err, source.calls)
	}
	draft, _ := port.DraftWelcome(ctx, worker)
	if _, err := port.PrepareWelcomePost(ctx, principal, worker, "model invented welcome", source.snapshot.ChannelID); !errors.Is(err, ErrPersonaOnboardingFacts) || channels.calls != 0 {
		t.Fatalf("ungrounded draft: %v", err)
	}
	if _, err := port.PrepareWelcomePost(ctx, principal, worker, draft, "other-channel"); !errors.Is(err, ErrPersonaOnboardingFacts) || channels.calls != 0 {
		t.Fatalf("unbound channel: %v", err)
	}
	mutations := []func(*chatstore.PersonaChannelPolicySnapshot){func(s *chatstore.PersonaChannelPolicySnapshot) { s.ExternalMembers = true }, func(s *chatstore.PersonaChannelPolicySnapshot) { s.Policy.MaxTier = "T1" }, func(s *chatstore.PersonaChannelPolicySnapshot) { s.Policy.AllowedDataClasses = []string{"WORKFORCE"} }, func(s *chatstore.PersonaChannelPolicySnapshot) { s.ManagerID = "other-manager" }, func(s *chatstore.PersonaChannelPolicySnapshot) { s.Policy.AlwaysPrivate = true }, func(s *chatstore.PersonaChannelPolicySnapshot) { s.Kind = "DIRECT" }, func(s *chatstore.PersonaChannelPolicySnapshot) { s.PolicyRevision = 0 }}
	for i, change := range mutations {
		fresh, fs, fc, fctx, fp := nativeOnboardingFixture(t)
		change(&fc.room)
		text, _ := fresh.DraftWelcome(fctx, fs.snapshot.Request.Plan.Worker)
		if _, err := fresh.PrepareWelcomePost(fctx, fp, fs.snapshot.Request.Plan.Worker, text, fs.snapshot.ChannelID); !errors.Is(err, ErrPersonaOnboardingFacts) {
			t.Fatalf("room mutation %d accepted", i)
		}
	}
	source.snapshot.DisplayName = "Changed name"
	if _, err := port.PrepareWelcomePost(ctx, principal, worker, draft, source.snapshot.ChannelID); !errors.Is(err, ErrPersonaOnboardingFacts) {
		t.Fatalf("stale reviewed name: %v", err)
	}
	source.err = workerlifecyclestore.ErrNotFound
	if _, err := port.DraftWelcome(ctx, worker); !errors.Is(err, workerlifecyclestore.ErrNotFound) {
		t.Fatalf("missing lifecycle source: %v", err)
	}
	if _, err := NewNativePersonaOnboardingPort(nil, channels, time.Now, port.authority); !errors.Is(err, ErrPersonaOnboardingFacts) {
		t.Fatalf("missing source: %v", err)
	}
	var nilSource *nativeOnboardingSource
	if _, err := NewNativePersonaOnboardingPort(nilSource, channels, time.Now, port.authority); !errors.Is(err, ErrPersonaOnboardingFacts) {
		t.Fatalf("typed nil source: %v", err)
	}
}
