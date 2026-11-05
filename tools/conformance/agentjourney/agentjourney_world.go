// Package agentjourney hosts the AGENT-047 conformance journey: one design
// partner creates, publishes, installs, chats with, schedules, governs, and
// recovers a company agent end to end.
//
// The journey composes the real services behind each leg — the owned model
// contract and pinned provider route (AGENT-021–AGENT-023), mention
// invocation (AGENT-027), autonomous triggers (AGENT-028), scheduled
// dispatch (AGENT-031), the workflow completion bridge (AGENT-033), the
// exact-approval submission path (AGENT-036), evaluation gates and the kill
// switch (AGENT-039), installation (AGENTP-007) and governed publication —
// with observable ports. A test passes only when the real service admits,
// routes, refuses, resumes, or fences the journey step; there is no pretend
// agent, model server, or RPC in this harness.
package agentjourney

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/install"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/publication"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/trigger"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/workflowbridge"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

// journeyNow pins the journey clock. Every world starts here and advances
// one minute per beat, so digests and transcripts are byte-stable across runs.
var journeyNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

const (
	journeyTenant    = "designpartner-acme"
	journeyLegal     = "le-acme"
	journeyOwner     = "owner-priya-nair"
	journeyReviewer  = "rev-lena-fischer"
	journeyMember    = "member-tom-almeida"
	journeyOutsider  = "outsider-fern-guest"
	journeyAgent     = "team-assistant"
	journeyPersona   = "team-assistant"
	journeyDM        = "dm-priya-assistant"
	journeyThread    = "thread-dm-1"
	journeyChannel   = "chan-team-alpha"
	journeySkill     = "team.answer"
	journeyScope     = "team:alpha"
	journeyPurpose   = "answer-team-questions"
	journeyDocID     = "doc-team-handbook"
	journeyDocLabel  = "Team handbook"
	journeyOrigin    = "https://acme.hcm.example"
	journeyTask      = "task-team-assistant-journey"
	journeyGoal      = "Brief the team and file the approved promotion"
	journeySchedule  = "sched-morning-digest"
	journeySponsor   = "sponsor-acme-people-ops"
	journeyGrantRef  = "grantref-acme-001"
	journeyPrincipal = "agentprincipal-team-assistant"
	journeyWorker    = "worker-omar"
	journeyTaskName  = "journey-task"
	journeyRegion    = "eu"
	journeyClass     = "INTERNAL"

	journeySubmitKey    = "journey-submit-1"
	journeyApprovalID   = "approval-journey-1"
	journeyRefusedCause = "workflow-refused"
)

// SubmittedIntent is one governed intent the approval submitter port
// recorded: the material it carried and the digest it was submitted under.
type SubmittedIntent struct {
	ItemID         string
	IntentType     string
	IntentVersion  string
	Subject        string
	Material       []agentapproval.MaterialField
	Digest         string
	IdempotencyKey string
}

// Journal is the world's observable notebook: every post the writers
// committed, every run the starters recorded, every submitted intent, and
// the final report text.
type Journal struct {
	PublicPosts  []agentdeliver.PublicPost
	PrivatePosts []agentdeliver.PrivatePost
	Runs         []agentinvoke.RunRequest
	Refusals     []agentinvoke.EphemeralRefusal
	Intents      []SubmittedIntent
	EffectKeys   []string
	Report       string
}

// World composes the real journey services with observable ports.
type World struct {
	T *testing.T

	Journal Journal

	Route         agentmodel.RouteRecord
	Evaluation    string
	Publication   publication.Publication
	Installation  install.Installation
	Invocation    agentinvoke.Invocation
	Delivery      agentdeliver.DeliveryReceipt
	Digest        scheduled.Outcome
	Reservation   trigger.Reservation
	Workflow      workflowbridge.Result
	Approval      agentapproval.AgentActionApproval
	SubmitReceipt agentapproval.Receipt

	// TriggerLoopErr carries the agent-authored loop refusal from Participate.
	TriggerLoopErr error

	mu         sync.Mutex
	now        time.Time
	seq        int
	transcript []string

	tenants map[string]bool

	router   *agentmodel.Router
	routes   []agentmodel.RouteRecord
	pin      agentmodel.ModelPin
	profile  agentmodel.TaskProfile
	manifest agentmanifest.Manifest

	evaluator *agentsecurity.Evaluator
	gate      *agentsecurity.Publisher
	kill      *agentsecurity.KillSwitch

	publisher   *publication.Service
	manifests   map[string]agentmanifest.Manifest
	reviews     map[string]publication.Review
	evaluations map[string]publication.Evaluation
	pubHistory  map[string]publication.Publication
	pubPointer  publication.Publication
	pubHas      bool
	pubQuarant  map[string]string
	fenceCalls  []string

	installs   *install.Service
	managers   map[string]bool
	membership map[string]install.MembershipSnapshot

	invocations *agentinvoke.Service
	invokeRepo  *agentinvoke.MemoryRepository

	delivery *agentdeliver.Service

	admission *agentrun.AdmissionService
	admitDrv  *agentrun.MemoryAdmissionStore
	records   map[string]agentrun.Record
	requests  map[string]agentrun.Request

	dispatcher *scheduled.Dispatcher
	receipts   map[string]scheduled.Receipt
	schedOff   map[string]bool

	triggers *trigger.Service
	sub      trigger.Subscription
	events   map[string]trigger.CommittedEvent
	ledger   map[string]trigger.Reservation

	approvals *agentapproval.Service

	runtime *agentrun.Runtime
	tasks   *agentrun.MemoryStore
	task    agentrun.AgentTask
	hasTask bool

	issuer   *stepup.Issuer
	stepGate *stepup.Gate
	manager  *trust.Principal
}

// NewWorld builds a fresh design-partner fixture.
func NewWorld(t *testing.T) *World {
	t.Helper()
	world := &World{
		T:           t,
		now:         journeyNow,
		tenants:     map[string]bool{journeyTenant: true},
		transcript:  []string{},
		manifests:   map[string]agentmanifest.Manifest{},
		reviews:     map[string]publication.Review{},
		evaluations: map[string]publication.Evaluation{},
		pubHistory:  map[string]publication.Publication{},
		pubQuarant:  map[string]string{},
		managers:    map[string]bool{},
		membership:  map[string]install.MembershipSnapshot{},
		records:     map[string]agentrun.Record{},
		requests:    map[string]agentrun.Request{},
		receipts:    map[string]scheduled.Receipt{},
		schedOff:    map[string]bool{},
		events:      map[string]trigger.CommittedEvent{},
		ledger:      map[string]trigger.Reservation{},
	}
	world.managers[managerKey(journeyTenant, journeyDM, journeyOwner)] = true
	world.managers[managerKey(journeyTenant, journeyChannel, journeyOwner)] = true
	world.membership[journeyTenant+"\x00"+journeyDM] = install.MembershipSnapshot{Revision: 1}
	world.membership[journeyTenant+"\x00"+journeyChannel] = install.MembershipSnapshot{Revision: 1}

	world.evaluator = agentsecurity.NewEvaluator()
	world.gate = agentsecurity.NewPublisher()
	world.kill = agentsecurity.NewKillSwitch()

	var err error
	world.publisher, err = publication.New(publication.Dependencies{
		Manifests: world, Reviews: world, Evaluations: world,
		Eligibility: world, Quarantine: world, Repository: world,
	})
	if err != nil {
		t.Fatal(err)
	}
	world.installs, err = install.New(install.Config{
		Clock: func() time.Time { return world.now }, Manager: world, Membership: journeyMembership{world},
	})
	if err != nil {
		t.Fatal(err)
	}
	world.invokeRepo = agentinvoke.NewMemoryRepository()
	world.invocations, err = agentinvoke.NewService(agentinvoke.Config{
		Authority: world, Grants: world, Runs: world, Repository: world.invokeRepo,
		Ephemeral: world, Now: func() time.Time { return world.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	world.delivery = &agentdeliver.Service{Audience: journeyAudience{world}, Authorize: world, Public: world, Private: world}
	world.admitDrv = agentrun.NewMemoryAdmissionStore()
	world.admission, err = agentrun.NewAdmissionService(agentrun.AdmissionConfig{
		Authority: world, Store: world.admitDrv, Now: func() time.Time { return world.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	world.dispatcher, err = scheduled.NewDispatcher(world, journeyInbox{world}, world, world)
	if err != nil {
		t.Fatal(err)
	}
	world.triggers, err = trigger.New(world, world, world, world, world, world, func() time.Time { return world.now })
	if err != nil {
		t.Fatal(err)
	}
	world.approvals, err = agentapproval.New(func() time.Time { return world.now })
	if err != nil {
		t.Fatal(err)
	}
	world.tasks = agentrun.NewMemoryStore()
	world.runtime, err = agentrun.NewRuntime(world.tasks)
	if err != nil {
		t.Fatal(err)
	}
	var stepupKey [32]byte
	copy(stepupKey[:], "journey-stepup-key-32-bytes-ok!!")
	sessions := stepup.NewStaticSession("sess-priya-1", "sess-lena-1")
	world.issuer = stepup.NewIssuer(stepupKey, func() time.Time { return world.now }, nil)
	world.stepGate = stepup.NewGate(stepupKey, stepup.NewMemoryStore(), sessions, func(context.Context, stepup.Proof, stepup.Operation, *trust.Principal) error {
		return nil
	}, func() time.Time { return world.now })
	world.manager, err = trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(journeyTenant), Subject: journeyOwner, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-acme-people-ops", Roles: []string{"role.owner"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "sess-priya-1", IssuedAt: journeyNow.Add(-5 * time.Minute), ExpiresAt: journeyNow.Add(12 * time.Hour),
		CredentialDigest: "cred-" + journeyOwner,
	})
	if err != nil {
		t.Fatal(err)
	}
	return world
}

func managerKey(tenant, conversation, user string) string {
	return tenant + "\x00" + conversation + "\x00" + user
}

func sha256hex(material string) string {
	sum := sha256.Sum256([]byte(material))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// beat advances the journey clock one minute and returns the new time.
func (w *World) beat() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.now = w.now.Add(time.Minute)
	return w.now
}

func (w *World) clock() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.now
}

func (w *World) note(format string, args ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.transcript = append(w.transcript, fmt.Sprintf(format, args...))
}

// Transcript returns the ordered journey evidence lines.
func (w *World) Transcript() []string {
	w.T.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.transcript...)
}

// nextPost mints the next deterministic journey post identity.
func (w *World) nextPost() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	return fmt.Sprintf("post-journey-%d", w.seq)
}

// manifestRef pins one immutable fixture record for the journey manifest.
func manifestRef(id string, version uint64) agentmanifest.Reference {
	return agentmanifest.Reference{ID: id, Version: version, SchemaVersion: 1, Digest: sha256hex("ref:" + id)}
}

// journeyManifest builds the created agent version: the design partner's
// team assistant, pinned to its exact ceilings and evaluation suite.
func journeyManifest() agentmanifest.Manifest {
	return agentmanifest.Manifest{
		SchemaVersion:      agentmanifest.CurrentSchemaVersion,
		ID:                 journeyAgent,
		Version:            1,
		OwnerID:            journeyOwner,
		Purpose:            journeyPurpose,
		InstructionsDigest: sha256hex("instructions:" + journeyAgent + ":v1"),
		SourceCeiling:      []agentmanifest.Reference{manifestRef("doc-team-handbook", 3)},
		ToolCeiling:        []agentmanifest.Reference{manifestRef("team.answer", 1)},
		ModelPolicy:        manifestRef("model-policy-acme", 2),
		AutonomyCeiling:    "assisted",
		Budget: agentmanifest.Budget{
			MaxCostMicros: 100000, MaxInputTokens: 64000, MaxOutputTokens: 16000, MaxConcurrentRuns: 4,
		},
		OutputSchema:   manifestRef("team-answer-schema", 1),
		ContextGrants:  []agentmanifest.Reference{},
		EvaluationRefs: []agentmanifest.Reference{manifestRef("eval-suite-safety-grounding-tools", 5)},
	}
}

// SelectProvider routes the journey's pinned model task through the owned
// model contract and records the selected provider.
func (w *World) SelectProvider() agentmodel.RouteRecord {
	w.T.Helper()
	ctx := context.Background()
	w.manifest = journeyManifest()
	if err := w.manifest.Validate(); err != nil {
		w.T.Fatal(err)
	}
	manifestDigest, err := w.manifest.Digest()
	if err != nil {
		w.T.Fatal(err)
	}
	semantics, output, tools := "semantics-journey-1", "output-journey-1", "tools-journey-1"
	profiles := []agentmodel.ModelProfile{
		{
			ID: "openai-journey-primary", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-journey-1", Version: "2026-10-01"},
			Regions: []string{journeyRegion}, DataClasses: []string{journeyClass}, TaskProfileIDs: []string{journeyTaskName},
			MaxLatency: 30 * time.Second, MaxCostMicros: 5000, ExpectedCostMicros: 1000,
			SemanticsDigest: semantics, OutputSchemaDigest: output, ToolSchemaDigest: tools,
			Evaluation: agentmodel.ModelEvaluation{AgentVersionDigest: manifestDigest, SuiteDigest: "suite-journey-5", Passed: true},
		},
		{
			ID: "anthropic-journey-fallback", Identity: agentmodel.ModelIdentity{ProviderID: "anthropic", ModelID: "claude-journey-1", Version: "2026-09-15"},
			Regions: []string{journeyRegion}, DataClasses: []string{journeyClass}, TaskProfileIDs: []string{journeyTaskName},
			MaxLatency: 30 * time.Second, MaxCostMicros: 6000, ExpectedCostMicros: 1200,
			SemanticsDigest: semantics, OutputSchemaDigest: output, ToolSchemaDigest: tools,
			Evaluation: agentmodel.ModelEvaluation{AgentVersionDigest: manifestDigest, SuiteDigest: "suite-journey-5", Passed: true},
		},
		{
			ID: "private-journey-fallback", Identity: agentmodel.ModelIdentity{ProviderID: "private", ModelID: "acme-hosted-1", Version: "r42"},
			Regions: []string{journeyRegion}, DataClasses: []string{journeyClass}, TaskProfileIDs: []string{journeyTaskName},
			MaxLatency: 45 * time.Second, MaxCostMicros: 4000, ExpectedCostMicros: 800,
			SemanticsDigest: semantics, OutputSchemaDigest: output, ToolSchemaDigest: tools,
			Evaluation: agentmodel.ModelEvaluation{AgentVersionDigest: manifestDigest, SuiteDigest: "suite-journey-5", Passed: true},
		},
	}
	for i := range profiles {
		profiles[i].ProfileDigest = agentmodel.ModelProfileDigest(profiles[i])
	}
	w.router, err = agentmodel.NewRouter(profiles, w)
	if err != nil {
		w.T.Fatal(err)
	}
	selection := func(profile agentmodel.ModelProfile) agentmodel.ModelSelection {
		return agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	}
	w.pin = agentmodel.ModelPin{
		AgentVersionDigest: manifestDigest, TaskProfileID: journeyTaskName,
		Primary: selection(profiles[0]), Fallbacks: []agentmodel.ModelSelection{selection(profiles[1]), selection(profiles[2])},
		SemanticsDigest: semantics, OutputSchemaDigest: output, ToolSchemaDigest: tools,
	}
	w.profile = agentmodel.TaskProfile{
		ID: journeyTaskName, AgentVersionDigest: manifestDigest, Region: journeyRegion,
		DataClasses: []string{journeyClass}, MaxLatency: time.Minute, MaxCostMicros: 100000,
		SemanticsDigest: semantics, OutputSchemaDigest: output, ToolSchemaDigest: tools,
	}
	route, err := w.router.Route(ctx, agentmodel.RouteRequest{
		TraceID: "trace-journey-1", Pin: w.pin, Task: w.profile,
		BudgetRemainingMicros: 100000, Effect: agentmodel.EffectNone,
	})
	if err != nil {
		w.T.Fatal(err)
	}
	w.Route = route
	w.note("route:%s/%s@%s fallback=%v digest=%s", route.Selected.Identity.ProviderID, route.Selected.Identity.ModelID, route.Selected.Identity.Version, route.Fallback, route.Digest)
	return route
}

// RouteRecords returns the route decisions the journey recorded.
func (w *World) RouteRecords() []agentmodel.RouteRecord {
	w.T.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]agentmodel.RouteRecord(nil), w.routes...)
}

// RecordRoute implements agentmodel.RouteRecorder.
func (w *World) RecordRoute(_ context.Context, record agentmodel.RouteRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.routes = append(w.routes, record)
	return nil
}

// PublishAgent evaluates, reviews, and publishes the journey agent version.
func (w *World) PublishAgent() publication.Publication {
	w.T.Helper()
	ctx := context.Background()
	if w.manifest.ID == "" {
		w.T.Fatal("select the provider before publishing: the pin binds the created manifest")
	}
	manifestDigest, err := w.manifest.Digest()
	if err != nil {
		w.T.Fatal(err)
	}
	w.manifests[journeyTenant+"\x00"+journeyAgent+"\x00"+fmt.Sprint(w.manifest.Version)] = w.manifest
	release := agentsecurity.Release{
		Agent: journeyAgent, AgentBuild: "build-1", AgentVersion: "1",
		Model:       w.Route.Selected.Identity.ProviderID + "/" + w.Route.Selected.Identity.ModelID,
		ModelDigest: w.Route.Selected.ProfileDigest,
		Tool:        journeySkill, ToolVersion: 1,
		Prompt: "Answer team questions from the handbook with citations.", PromptHash: sha256hex("prompt:" + journeyAgent + ":v1"),
	}
	run, err := w.evaluator.Evaluate(release, []agentsecurity.EvalFixture{
		{Name: "safety-refusals", SafetyPass: true, GroundingPass: true, ToolSelectionPass: true},
		{Name: "grounded-citations", SafetyPass: true, GroundingPass: true, ToolSelectionPass: true},
		{Name: "tool-selection", SafetyPass: true, GroundingPass: true, ToolSelectionPass: true},
	})
	if err != nil {
		w.T.Fatal(err)
	}
	if err := run.Verify(); err != nil {
		w.T.Fatal(err)
	}
	if err := w.gate.Publish(run); err != nil {
		w.T.Fatal(err)
	}
	w.Evaluation = run.Digest
	w.evaluations[run.Digest] = publication.Evaluation{Digest: run.Digest, ManifestDigest: manifestDigest, Passed: true, Fresh: true}
	w.note("eval:passed fixtures=3 digest=%s", run.Digest)
	review, err := w.publisher.Review(ctx, journeyTenant, journeyAgent, w.manifest.Version, journeyReviewer, manifestDigest)
	if err != nil {
		w.T.Fatal(err)
	}
	w.note("review:%s reviewer=%s digest=%s", journeyAgent, review.ReviewerID, review.Digest)
	published, err := w.publisher.Publish(ctx, journeyTenant, journeyAgent, w.manifest.Version, 0, manifestDigest, run.Digest, review.Digest)
	if err != nil {
		w.T.Fatal(err)
	}
	if _, err := w.publisher.Admit(ctx, journeyTenant, journeyAgent, manifestDigest); err != nil {
		w.T.Fatal(err)
	}
	w.Publication = published
	w.note("publish:%s v%d generation=%d reviewer=%s", published.AgentID, published.ManifestVersion, published.Generation, published.ReviewerID)
	return published
}

// PublishTamperedEval attempts publication behind a broken eval seal.
func (w *World) PublishTamperedEval() error {
	w.T.Helper()
	manifest := journeyManifest()
	digest, err := manifest.Digest()
	if err != nil {
		w.T.Fatal(err)
	}
	_ = digest
	run, err := w.evaluator.Evaluate(agentsecurity.Release{
		Agent: journeyAgent, AgentBuild: "build-tamper", AgentVersion: "1",
		Model: "openai/gpt-journey-1", ModelDigest: "tamper", Tool: journeySkill, ToolVersion: 1,
		Prompt: "tampered", PromptHash: sha256hex("prompt:tamper"),
	}, []agentsecurity.EvalFixture{{Name: "safety-refusals", SafetyPass: true, GroundingPass: true, ToolSelectionPass: true}})
	if err != nil {
		w.T.Fatal(err)
	}
	run.Fixtures[0].SafetyPass = false
	return w.gate.Publish(run)
}

// ResolveManifest implements publication.ManifestResolver.
func (w *World) ResolveManifest(_ context.Context, tenant, agent string, version uint64) (agentmanifest.Manifest, error) {
	manifest, ok := w.manifests[tenant+"\x00"+agent+"\x00"+fmt.Sprint(version)]
	if !ok {
		return agentmanifest.Manifest{}, errors.New("agentjourney: unknown manifest")
	}
	return manifest, nil
}

// AuthorizeAgentReviewer implements publication.ReviewAuthority.
func (w *World) AuthorizeAgentReviewer(_ context.Context, tenant, agent, reviewer string) error {
	if tenant == journeyTenant && agent == journeyAgent && reviewer == journeyReviewer {
		return nil
	}
	return errors.New("agentjourney: reviewer is not authorized")
}

// ResolveEvaluation implements publication.EvaluationResolver.
func (w *World) ResolveEvaluation(_ context.Context, digest string) (publication.Evaluation, error) {
	evaluation, ok := w.evaluations[digest]
	if !ok {
		return publication.Evaluation{}, errors.New("agentjourney: unknown evaluation")
	}
	return evaluation, nil
}

// ValidateAgentVersion implements publication.Eligibility.
func (w *World) ValidateAgentVersion(_ context.Context, manifest agentmanifest.Manifest) error {
	return manifest.Validate()
}

// FenceAgentWrites implements publication.QuarantineEffects.
func (w *World) FenceAgentWrites(_ context.Context, tenant, agent, manifestDigest string) error {
	w.fenceCalls = append(w.fenceCalls, tenant+"\x00"+agent+"\x00"+manifestDigest)
	return nil
}

// StoreReview implements publication.Repository.
func (w *World) StoreReview(_ context.Context, review publication.Review) error {
	w.reviews[review.TenantID+"\x00"+review.AgentID+"\x00"+review.Digest] = review
	return nil
}

// ResolveReview implements publication.Repository.
func (w *World) ResolveReview(_ context.Context, tenant, agent, digest string) (publication.Review, error) {
	review, ok := w.reviews[tenant+"\x00"+agent+"\x00"+digest]
	if !ok {
		return publication.Review{}, errors.New("agentjourney: unknown review")
	}
	return review, nil
}

// ResolvePublication implements publication.Repository.
func (w *World) ResolvePublication(_ context.Context, tenant, agent string, version uint64) (publication.Publication, error) {
	found, ok := w.pubHistory[tenant+"\x00"+agent+"\x00"+fmt.Sprint(version)]
	if !ok {
		return publication.Publication{}, errors.New("agentjourney: unknown publication")
	}
	return found, nil
}

// Publish implements publication.Repository with generation compare-and-swap.
func (w *World) Publish(_ context.Context, expected uint64, candidate publication.Publication) (publication.Publication, error) {
	if w.pubHas && w.pubPointer.Generation != expected {
		return publication.Publication{}, publication.ErrConflict
	}
	if !w.pubHas && expected != 0 {
		return publication.Publication{}, publication.ErrConflict
	}
	if reason, fenced := w.pubQuarant[candidate.ManifestDigest]; fenced {
		return publication.Publication{}, fmt.Errorf("%w: %s", publication.ErrQuarantined, reason)
	}
	candidate.Generation = expected + 1
	w.pubPointer, w.pubHas = candidate, true
	w.pubHistory[candidate.TenantID+"\x00"+candidate.AgentID+"\x00"+fmt.Sprint(candidate.ManifestVersion)] = candidate
	return candidate, nil
}

// Quarantine implements publication.Repository.
func (w *World) Quarantine(_ context.Context, tenant, agent, manifestDigest, reason string) error {
	for _, record := range w.pubHistory {
		if record.TenantID == tenant && record.AgentID == agent && record.ManifestDigest == manifestDigest {
			w.pubQuarant[manifestDigest] = reason
			return nil
		}
	}
	return publication.ErrNotFound
}

// Admit implements publication.Repository.
func (w *World) Admit(_ context.Context, tenant, agent, manifestDigest string) (publication.Publication, error) {
	if !w.pubHas || w.pubPointer.TenantID != tenant || w.pubPointer.AgentID != agent || w.pubPointer.ManifestDigest != manifestDigest {
		return publication.Publication{}, publication.ErrNotFound
	}
	if reason, fenced := w.pubQuarant[manifestDigest]; fenced {
		return publication.Publication{}, fmt.Errorf("%w: %s", publication.ErrQuarantined, reason)
	}
	return w.pubPointer, nil
}

// InstallAgent installs the published persona into the journey DM.
func (w *World) InstallAgent() install.Installation {
	w.T.Helper()
	ctx := context.Background()
	if !w.pubHas {
		w.T.Fatal("publish the agent before installing it")
	}
	installation, err := w.installs.Install(ctx, install.InstallRequest{
		Persona: install.PersonaVersion{
			TenantID: journeyTenant, PersonaID: journeyPersona, Version: w.Publication.ManifestVersion,
			Lifecycle:             install.LifecyclePublished,
			OwnerID:               journeyOwner,
			AllowedChannelClasses: []install.ChannelClass{install.ChannelOneToOne, install.ChannelPrivate},
			TierCeiling:           install.TierT2,
			DataClasses:           []string{journeyClass},
		},
		Conversation: install.Conversation{
			TenantID: journeyTenant, ConversationID: journeyDM,
			Class: install.ChannelOneToOne, MembershipRevision: 1,
		},
		Policy: install.ChannelPersonaPolicy{
			MaxTier: install.TierT2, AllowedDataClasses: []string{journeyClass},
			AllowedChannelClasses: []install.ChannelClass{install.ChannelOneToOne, install.ChannelPrivate},
		},
		InstallerID: journeyOwner,
	})
	if err != nil {
		w.T.Fatal(err)
	}
	w.Installation = installation
	w.note("install:%s %s rev=%d", installation.ID, installation.Status, installation.Revision)
	return installation
}

// SuspendInstallation suspends the journey DM installation.
func (w *World) SuspendInstallation() {
	w.T.Helper()
	if err := w.installs.Suspend(context.Background(), install.SuspendRequest{
		Scope: install.SuspendInstallation, TenantID: journeyTenant,
		PersonaID: journeyPersona, PersonaVersion: w.Installation.PersonaVersion,
		InstallationID: w.Installation.ID, Reason: "journey-suspend", At: w.beat(),
	}); err != nil {
		w.T.Fatal(err)
	}
	installation, err := w.installs.Installation(context.Background(), w.Installation.ID)
	if err != nil {
		w.T.Fatal(err)
	}
	w.Installation = installation
}

// IsChannelManager implements install.ChannelManagerAuthorizer.
func (w *World) IsChannelManager(_ context.Context, tenant, conversation, user string) (bool, error) {
	return w.managers[managerKey(tenant, conversation, user)], nil
}

// journeyMembership adapts the journey roster to the install port.
type journeyMembership struct{ world *World }

// Snapshot implements install.MembershipProvider.
func (m journeyMembership) Snapshot(_ context.Context, tenant, conversation string) (install.MembershipSnapshot, error) {
	snapshot, ok := m.world.membership[tenant+"\x00"+conversation]
	if !ok {
		return install.MembershipSnapshot{}, errors.New("agentjourney: unknown conversation")
	}
	return snapshot, nil
}

// journeyInbox adapts the shared admission service to the schedule owner.
type journeyInbox struct{ world *World }

// Admit implements scheduled.Inbox.
func (a journeyInbox) Admit(ctx context.Context, req agentrun.Request) (agentrun.Record, bool, error) {
	return a.world.admission.Admit(ctx, req)
}

// journeyAudience adapts the journey membership to the delivery port.
type journeyAudience struct{ world *World }

// Snapshot implements agentdeliver.AudienceSource.
func (a journeyAudience) Snapshot(_ context.Context, conversation agentdeliver.Conversation) (agentdeliver.AudienceSnapshot, error) {
	world := a.world
	members := []agentdeliver.AudienceMember{{TenantID: conversation.TenantID, SubjectID: journeyOwner}}
	if conversation.ConversationID == journeyChannel {
		members = append(members, agentdeliver.AudienceMember{TenantID: conversation.TenantID, SubjectID: journeyMember})
	}
	_ = world
	return agentdeliver.AudienceSnapshot{Revision: 1, CurrentMembers: members}, nil
}

// MentionAs drives one persona mention as the given user.
func (w *World) MentionAs(user, conversation, body string) ([]agentinvoke.Invocation, error) {
	w.T.Helper()
	postID := w.nextPost()
	return w.invocations.OnPostCommit(context.Background(), agentinvoke.PostCommit{
		TenantID: journeyTenant, ConversationID: conversation, ThreadID: journeyThread,
		PostID: postID, AuthorID: user, AuthorKind: agentinvoke.HumanAuthor,
		Body: body, New: true,
		Mentions: []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: journeyPersona, Canonical: true}},
	})
}

// AnswerDM invokes the installed persona from a human DM mention and
// delivers its cited answer.
func (w *World) AnswerDM() agentinvoke.Invocation {
	w.T.Helper()
	ctx := context.Background()
	if w.Installation.ID == "" {
		w.T.Fatal("install the persona before answering a DM")
	}
	invocations, err := w.MentionAs(journeyOwner, journeyDM, "When is standup, @"+journeyPersona+"?")
	if err != nil {
		w.T.Fatal(err)
	}
	if len(invocations) != 1 {
		w.T.Fatalf("mention invocations=%d, want exactly one", len(invocations))
	}
	invocation := invocations[0]
	postID := invocation.PostID
	manifestDigest, err := w.manifest.Digest()
	if err != nil {
		w.T.Fatal(err)
	}
	request := agentrun.Request{
		Source:         agentrun.SourceIdentity{TenantID: journeyTenant, Kind: agentrun.SourcePersonaMention, Key: "mention:" + invocation.ID, Ref: postID},
		Persona:        &agentrun.PersonaRef{ID: journeyPersona, Version: "1", Digest: sha256hex("persona:" + journeyPersona + ":v1")},
		LegalEntity:    journeyLegal,
		Agent:          agentrun.VersionRef{AgentID: journeyAgent, Version: "1", Digest: manifestDigest},
		InstallationID: w.Installation.ID,
		Principal:      agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: journeyPrincipal, InvokerID: journeyOwner, DelegatedCredentialRef: invocation.Grant.ID},
		Purpose:        "persona-mention",
		Audience:       agentrun.AudienceScope{ID: "dm:" + journeyOwner, SnapshotID: "audience-rev-1", Digest: sha256hex("audience:dm")},
		Context:        agentrun.ContextScope{ID: "ctx:" + postID, SnapshotID: "ctx-rev-1", Digest: sha256hex("ctx:" + postID)},
		Deadline:       w.clock().Add(time.Hour),
		Budget:         agentrun.Budget{MaxCostMicros: 100000, MaxInputTokens: 64000, MaxOutputTokens: 16000},
		CauseID:        postID,
	}
	record, created, err := w.admission.Admit(ctx, request)
	if err != nil {
		w.T.Fatal(err)
	}
	if !created || record.Decision != agentrun.DecisionAccepted {
		w.T.Fatalf("mention admission=%+v created=%v, want the accepted run", record, created)
	}
	w.records[recordKey(journeyTenant, agentrun.SourcePersonaMention)] = record
	w.requests[recordKey(journeyTenant, agentrun.SourcePersonaMention)] = request
	receipt, err := w.delivery.Deliver(ctx, agentdeliver.DeliveryRequest{
		Conversation: agentdeliver.Conversation{
			TenantID: journeyTenant, ConversationID: journeyDM, Kind: agentdeliver.Direct,
			Policy:       agentdeliver.ChannelPolicy{AllowedClasses: []agentdeliver.DataClass{journeyClass}},
			TenantOrigin: journeyOrigin,
		},
		ParentPostID: postID,
		Invoker:      agentdeliver.AudienceMember{TenantID: journeyTenant, SubjectID: journeyOwner},
		Result: agentdeliver.Result{
			PersonaLabel: "Team assistant",
			Items: []agentdeliver.ResultItem{{
				ID:   "answer-1",
				Text: "Standup is at 9:30 each weekday.",
				Materials: []agentdeliver.Material{
					{Kind: agentdeliver.MaterialSource, ID: journeyDocID, DataClass: journeyClass},
				},
				Citations: []agentdeliver.Citation{{
					Title: journeyDocLabel, URL: journeyOrigin + "/docs/" + journeyDocID,
					SourceID: journeyDocID, DataClass: journeyClass,
				}},
			}},
		},
	})
	if err != nil {
		w.T.Fatal(err)
	}
	w.Invocation, w.Delivery = invocation, receipt
	w.note("mention:%s grant=%s skills=%s:%s", invocation.ID, invocation.Grant.ID, journeySkill, journeyScope)
	w.note("deliver:%s rev=%d citations=1", receipt.Mode, receipt.AudienceRevision)
	return invocation
}

func recordKey(tenant string, kind agentrun.SourceKind) string {
	return tenant + "\x00" + string(kind)
}

// AdmissionRecord returns the stored admission for one source kind.
func (w *World) AdmissionRecord(kind agentrun.SourceKind) (agentrun.Record, bool) {
	w.T.Helper()
	record, ok := w.records[recordKey(journeyTenant, kind)]
	return record, ok
}

// AnswerBody returns the delivered DM answer text across the committed posts.
func (w *World) AnswerBody() string {
	w.T.Helper()
	var bodies []string
	for _, post := range w.Journal.PrivatePosts {
		if post.Conversation.TenantID == journeyTenant && post.Conversation.ConversationID == journeyDM {
			bodies = append(bodies, post.Body)
		}
	}
	for _, post := range w.Journal.PublicPosts {
		if post.Conversation.TenantID == journeyTenant && post.Conversation.ConversationID == journeyDM {
			bodies = append(bodies, post.Body)
		}
	}
	return strings.Join(bodies, "\n\n")
}

// DigestPostCount returns the number of scheduled digest posts committed.
func (w *World) DigestPostCount() int {
	w.T.Helper()
	count := 0
	for _, post := range w.Journal.PublicPosts {
		if strings.HasPrefix(post.Body, "Morning digest") {
			count++
		}
	}
	return count
}

// Resolve implements agentinvoke.AuthorityResolver against the live install.
func (w *World) Resolve(_ context.Context, req agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	member := req.InvokerID == journeyOwner || req.InvokerID == journeyMember
	admission := agentinvoke.Admission{}
	if !member || req.TenantID != journeyTenant || req.PersonaID != journeyPersona {
		return admission, nil
	}
	installation, err := w.installs.Installation(context.Background(), w.Installation.ID)
	if err != nil {
		return agentinvoke.Admission{HumanMember: true}, nil
	}
	skills := agentinvoke.SkillScopes{journeySkill: {journeyScope}}
	admission = agentinvoke.Admission{
		Persona:          agentinvoke.Persona{ID: journeyPersona, Version: "1", PinnedSkills: skills, Current: true, Suspended: installation.Status != install.InstallationActive},
		Installation:     agentinvoke.Installation{ID: installation.ID, Current: installation.Status == install.InstallationActive, Suspended: installation.Status != install.InstallationActive, SkillCeiling: skills},
		Channel:          agentinvoke.ChannelPolicy{SkillCeiling: skills},
		Discoverable:     skills,
		HumanMember:      true,
		AudienceMember:   true,
		PersonaInstalled: true,
	}
	return admission, nil
}

// CreateOnBehalfOfGrant implements agentinvoke.GrantIssuer.
func (w *World) CreateOnBehalfOfGrant(_ context.Context, req agentinvoke.GrantRequest) (agentinvoke.DelegationGrant, error) {
	return agentinvoke.DelegationGrant{
		ID: "grant-" + req.InvocationID, UserID: req.UserID, TenantID: req.TenantID,
		TargetAgentID: req.TargetAgentID, Skills: req.Skills.Clone(), ExpiresAt: req.ExpiresAt,
	}, nil
}

// Start implements agentinvoke.RunStarter.
func (w *World) Start(_ context.Context, request agentinvoke.RunRequest) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, started := range w.Journal.Runs {
		if started.InvocationID == request.InvocationID {
			return nil
		}
	}
	w.Journal.Runs = append(w.Journal.Runs, request)
	return nil
}

// Refuse implements agentinvoke.EphemeralResponder.
func (w *World) Refuse(_ context.Context, refusal agentinvoke.EphemeralRefusal) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Journal.Refusals = append(w.Journal.Refusals, refusal)
	return nil
}

// Authorize implements agentdeliver.Reauthorizer.
func (w *World) Authorize(_ context.Context, req agentdeliver.ReauthorizationRequest) error {
	if req.Conversation.TenantID == "" || req.Audience.TenantID != req.Conversation.TenantID {
		return errors.New("agentjourney: cross-tenant material")
	}
	if req.Audience.SubjectID != journeyOwner && req.Audience.SubjectID != journeyMember {
		return errors.New("agentjourney: audience member cannot read the material")
	}
	if req.Material.DataClass != journeyClass {
		return errors.New("agentjourney: data class is outside the journey ceiling")
	}
	return nil
}

// CommitPublic implements agentdeliver.PublicPoster.
func (w *World) CommitPublic(_ context.Context, post agentdeliver.PublicPost) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Journal.PublicPosts = append(w.Journal.PublicPosts, post)
	return nil
}

// DeliverPrivate implements agentdeliver.PrivatePoster.
func (w *World) DeliverPrivate(_ context.Context, post agentdeliver.PrivatePost) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Journal.PrivatePosts = append(w.Journal.PrivatePosts, post)
	return nil
}

// VerifyAdmission implements agentrun.Authority.
func (w *World) VerifyAdmission(_ context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if !w.tenants[request.Source.TenantID] {
		return agentrun.AuthoritySnapshot{}, &agentrun.AdmissionRefusal{Code: "UNKNOWN_TENANT"}
	}
	if request.Purpose == journeyRefusedCause {
		return agentrun.AuthoritySnapshot{}, &agentrun.AdmissionRefusal{Code: "WORKFLOW_DENIED"}
	}
	return agentrun.AuthoritySnapshot{
		Agent:          request.Agent,
		InstallationID: request.InstallationID,
		Principal:      request.Principal,
		Audience:       request.Audience,
		Context:        request.Context,
		BudgetCeiling: agentrun.Budget{
			MaxCostMicros:  request.Budget.MaxCostMicros * 2,
			MaxInputTokens: request.Budget.MaxInputTokens * 2, MaxOutputTokens: request.Budget.MaxOutputTokens * 2,
		},
		GrantRef:     journeyGrantRef,
		PolicyDigest: sha256hex("policy:" + request.Source.TenantID),
	}, nil
}

// buildFiring binds one digest occurrence to the published agent version for
// a tenant, with the matching sponsored run request.
func (w *World) buildFiring(tenant string) (scheduled.Firing, agentrun.Request) {
	w.T.Helper()
	manifestDigest, err := w.manifest.Digest()
	if err != nil {
		w.T.Fatal(err)
	}
	firing := scheduled.Firing{
		Occurrence: schedule.Occurrence{
			Trigger: schedule.TriggerRef{TenantID: tenant, ID: journeySchedule, Version: "1"},
			Key:     "occ-2026-10-22-morning",
			Source:  schedule.SourceCron,
		},
		Target: schedule.AgentRunTarget{
			Agent:     schedule.AgentVersionRef{ID: journeyAgent, Version: "1", Digest: manifestDigest},
			SponsorID: journeySponsor,
			Purpose:   "morning-digest",
			Budget:    schedule.AgentRunBudget{MaxCostMicros: 100000, MaxInputTokens: 64000, MaxOutputTokens: 16000},
			Destination: schedule.AgentRunDestination{
				AudienceID: "chan:" + journeyChannel, AudienceSnapshotID: "audience-rev-1",
				AudienceDigest: sha256hex("audience:channel"),
			},
		},
	}
	request := agentrun.Request{
		Agent:          agentrun.VersionRef{AgentID: journeyAgent, Version: "1", Digest: manifestDigest},
		InstallationID: w.Installation.ID,
		LegalEntity:    journeyLegal,
		Principal:      agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: journeyPrincipal, SponsorID: journeySponsor},
		Purpose:        "morning-digest",
		Audience:       agentrun.AudienceScope{ID: "chan:" + journeyChannel, SnapshotID: "audience-rev-1", Digest: sha256hex("audience:channel")},
		Context:        agentrun.ContextScope{ID: "ctx:digest-morning", SnapshotID: "ctx-rev-1", Digest: sha256hex("ctx:digest-morning")},
		Deadline:       w.clock().Add(time.Hour),
		Budget:         agentrun.Budget{MaxCostMicros: 100000, MaxInputTokens: 64000, MaxOutputTokens: 16000},
		CauseID:        "occ-2026-10-22-morning",
	}
	return firing, request
}

// fireDigest dispatches one digest firing and posts its digest on first acceptance.
func (w *World) fireDigest(tenant string) scheduled.Outcome {
	w.T.Helper()
	ctx := context.Background()
	firing, request := w.buildFiring(tenant)
	outcome, err := w.dispatcher.Dispatch(ctx, firing, request)
	if err != nil {
		w.T.Fatal(err)
	}
	if outcome.Duplicate {
		return outcome
	}
	if err := w.dispatcher.CheckBeforeWork(ctx, outcome.Receipt); err != nil {
		w.T.Fatal(err)
	}
	record, err := w.admitDrv.Get(agentrun.SourceIdentity{TenantID: tenant, Kind: agentrun.SourceSchedule, Key: outcome.Receipt.SourceKey})
	if err != nil {
		w.T.Fatal(err)
	}
	w.records[recordKey(tenant, agentrun.SourceSchedule)] = record
	request.Source = agentrun.SourceIdentity{
		TenantID: tenant, Kind: agentrun.SourceSchedule,
		Key: outcome.Receipt.SourceKey, Ref: tenant + "/" + journeySchedule + "/1",
	}
	w.requests[recordKey(tenant, agentrun.SourceSchedule)] = request
	if err := w.CommitPublic(ctx, agentdeliver.PublicPost{
		Conversation: agentdeliver.Conversation{
			TenantID: tenant, ConversationID: journeyChannel, Kind: agentdeliver.PrivateChannel,
			Policy:       agentdeliver.ChannelPolicy{AllowedClasses: []agentdeliver.DataClass{journeyClass}},
			TenantOrigin: journeyOrigin,
		},
		ParentPostID:             "post-digest-morning",
		AuthorID:                 "Team assistant",
		Tier:                     agentdeliver.TierCommunicate,
		Body:                     "Morning digest for team alpha: standup at 9:30, one promotion pending review.",
		ExpectedAudienceRevision: 1,
		Attribution:              "scheduled digest",
	}); err != nil {
		w.T.Fatal(err)
	}
	return outcome
}

// PostDigest dispatches the scheduled morning digest exactly once.
func (w *World) PostDigest() scheduled.Outcome {
	w.T.Helper()
	outcome := w.fireDigest(journeyTenant)
	w.Digest = outcome
	w.note("schedule:%s run=%s %s duplicate=%v", outcome.Receipt.SourceKey, outcome.Receipt.RunRequestID, outcome.Receipt.Decision, outcome.Duplicate)
	return outcome
}

// FireDigestForTenant dispatches the digest firing for another tenant.
func (w *World) FireDigestForTenant(tenant string) scheduled.Outcome {
	w.T.Helper()
	w.tenants[tenant] = true
	return w.fireDigest(tenant)
}

// CheckCurrent implements scheduled.ScheduleState.
func (w *World) CheckCurrent(_ context.Context, firing scheduled.Firing) error {
	if firing.Occurrence.Trigger.ID != journeySchedule || w.schedOff[firing.Occurrence.Trigger.TenantID] {
		return errors.New("agentjourney: schedule is not current")
	}
	if firing.Target.Agent.ID != journeyAgent || firing.Target.Agent.Version != "1" {
		return errors.New("agentjourney: firing targets a different agent version")
	}
	return nil
}

// Get implements scheduled.ReceiptStore.
func (w *World) Get(_ context.Context, key string) (scheduled.Receipt, bool, error) {
	receipt, ok := w.receipts[key]
	return receipt, ok, nil
}

// CreateOrGet implements scheduled.ReceiptStore.
func (w *World) CreateOrGet(_ context.Context, candidate scheduled.Receipt) (scheduled.Receipt, bool, error) {
	if prior, ok := w.receipts[candidate.SourceKey]; ok {
		if prior != candidate {
			return scheduled.Receipt{}, false, scheduled.ErrReceiptConflict
		}
		return prior, false, nil
	}
	w.receipts[candidate.SourceKey] = candidate
	return candidate, true, nil
}

// CheckBeforeWork implements scheduled.WorkFence.
func (w *World) CheckBeforeWork(_ context.Context, receipt scheduled.Receipt) error {
	stored, ok := w.receipts[receipt.SourceKey]
	if !ok || stored != receipt || receipt.Decision != agentrun.DecisionAccepted {
		return errors.New("agentjourney: no accepted receipt for this run")
	}
	return nil
}

// Readmit replays one admission against the restored spine.
func (w *World) Readmit(kind agentrun.SourceKind) (agentrun.Record, bool, error) {
	w.T.Helper()
	request, ok := w.requests[recordKey(journeyTenant, kind)]
	if !ok {
		w.T.Fatalf("no stored %s request to re-admit", kind)
	}
	return w.admission.Admit(context.Background(), request)
}

// Participate admits one autonomous channel trigger and refuses the
// agent-authored loop event.
func (w *World) Participate() trigger.Reservation {
	w.T.Helper()
	ctx := context.Background()
	if w.Installation.ID == "" {
		w.T.Fatal("install the persona before participating")
	}
	fair := trigger.Budget{EventsPerMinute: 10, EventsPerDay: 100, CostMicrosPerMinute: 10000, CostMicrosPerDay: 100000, MaxConcurrent: 2, Cooldown: time.Minute}
	w.sub = trigger.Subscription{
		TenantID: journeyTenant, ChannelID: journeyChannel, InstallationID: w.Installation.ID,
		AgentID: journeyAgent, AgentVersion: "1", SponsorID: journeySponsor,
		Purpose: "channel-participation", AudienceID: "chan:" + journeyChannel, Revision: 1,
		Enabled: true, EventClasses: []trigger.EventClass{trigger.EventPostCreated},
		MaxCauseDepth: 3, ChannelBudget: trigger.Budget{
			EventsPerMinute: 60, EventsPerDay: 1000, CostMicrosPerMinute: 60000, CostMicrosPerDay: 600000,
			MaxConcurrent: 10, Cooldown: time.Minute,
		}, FairShare: fair,
	}
	w.events["outbox-post-1"] = trigger.CommittedEvent{
		OutboxID: "outbox-post-1", PostID: "post-chan-1", TenantID: journeyTenant,
		ChannelID: journeyChannel, Class: trigger.EventPostCreated, Origin: trigger.OriginHuman,
		EstimatedCostMicros: 100,
	}
	reservation, err := w.triggers.Admit(ctx, journeyTenant, journeyChannel, w.Installation.ID, "outbox-post-1")
	if err != nil {
		w.T.Fatal(err)
	}
	w.Reservation = reservation
	w.note("trigger:%s depth=%d install=%s", reservation.SourceKey, reservation.CauseDepth, reservation.InstallationID)
	w.events["outbox-post-2"] = trigger.CommittedEvent{
		OutboxID: "outbox-post-2", PostID: "post-chan-2", TenantID: journeyTenant,
		ChannelID: journeyChannel, Class: trigger.EventPostCreated, Origin: trigger.OriginAgent,
		AuthorInstallationID: w.Installation.ID, CauseChain: []string{"persona-install-9", w.Installation.ID},
		EstimatedCostMicros: 100,
	}
	_, w.TriggerLoopErr = w.triggers.Admit(ctx, journeyTenant, journeyChannel, w.Installation.ID, "outbox-post-2")
	return reservation
}

// ReplayTrigger replays the admitted human event against the ledger.
func (w *World) ReplayTrigger() error {
	w.T.Helper()
	_, err := w.triggers.Admit(context.Background(), journeyTenant, journeyChannel, w.Installation.ID, "outbox-post-1")
	return err
}

// ResolveSubscription implements trigger.SubscriptionResolver.
func (w *World) ResolveSubscription(_ context.Context, tenantID, channelID, installationID string) (trigger.Subscription, error) {
	if w.sub.TenantID != tenantID || w.sub.ChannelID != channelID || w.sub.InstallationID != installationID {
		return trigger.Subscription{}, errors.New("agentjourney: unknown subscription")
	}
	return w.sub, nil
}

// ResolveCommittedEvent implements trigger.OutboxReader.
func (w *World) ResolveCommittedEvent(_ context.Context, tenant, outboxID string) (trigger.CommittedEvent, error) {
	event, ok := w.events[outboxID]
	if !ok || event.TenantID != tenant {
		return trigger.CommittedEvent{}, errors.New("agentjourney: unknown committed event")
	}
	return event, nil
}

// CheckTriggerAccess implements trigger.AccessChecker.
func (w *World) CheckTriggerAccess(_ context.Context, sub trigger.Subscription, event trigger.CommittedEvent) error {
	if sub.TenantID != event.TenantID || sub.ChannelID != event.ChannelID || event.ChannelID != journeyChannel {
		return errors.New("agentjourney: trigger is outside the journey channel")
	}
	return nil
}

// ReserveTrigger implements trigger.AdmissionLedger.
func (w *World) ReserveTrigger(_ context.Context, reservation trigger.Reservation) error {
	if _, dup := w.ledger[reservation.SourceKey]; dup {
		return trigger.ErrReplay
	}
	w.ledger[reservation.SourceKey] = reservation
	return nil
}

// AuthorizeTriggerPause implements trigger.PauseAuthorizer.
func (w *World) AuthorizeTriggerPause(_ context.Context, actor trigger.Actor, sub trigger.Subscription) error {
	if actor.TenantID != sub.TenantID || actor.PrincipalID != journeyOwner {
		return errors.New("agentjourney: only the journey owner pauses triggers")
	}
	return nil
}

// SetTriggerPaused implements trigger.PauseWriter.
func (w *World) SetTriggerPaused(_ context.Context, actor trigger.Actor, sub trigger.Subscription, paused bool) error {
	if w.sub.TenantID != sub.TenantID || w.sub.ChannelID != sub.ChannelID || w.sub.InstallationID != sub.InstallationID || w.sub.Revision != sub.Revision {
		return trigger.ErrConflict
	}
	if actor.TenantID != sub.TenantID || actor.PrincipalID != journeyOwner {
		return trigger.ErrDenied
	}
	w.sub.Paused = paused
	w.sub.Revision++
	return nil
}

// workflowRequest builds one workflow-sourced run request.
func (w *World) workflowRequest(ref, purpose string) agentrun.Request {
	w.T.Helper()
	manifestDigest, err := w.manifest.Digest()
	if err != nil {
		w.T.Fatal(err)
	}
	return agentrun.Request{
		Source:         agentrun.SourceIdentity{TenantID: journeyTenant, Kind: agentrun.SourceWorkflow, Key: "workflow:" + ref, Ref: ref},
		LegalEntity:    journeyLegal,
		Agent:          agentrun.VersionRef{AgentID: journeyAgent, Version: "1", Digest: manifestDigest},
		InstallationID: w.Installation.ID,
		Principal:      agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: journeyPrincipal, SponsorID: journeySponsor},
		Purpose:        purpose,
		Audience:       agentrun.AudienceScope{ID: "workflow:" + ref, SnapshotID: "audience-rev-1", Digest: sha256hex("audience:" + ref)},
		Context:        agentrun.ContextScope{ID: "ctx:" + ref, SnapshotID: "ctx-rev-1", Digest: sha256hex("ctx:" + ref)},
		Deadline:       w.clock().Add(2 * time.Hour),
		Budget:         agentrun.Budget{MaxCostMicros: 100000, MaxInputTokens: 64000, MaxOutputTokens: 16000},
		CauseID:        ref,
	}
}

// InvokeWorkflow admits a workflow-sourced run and derives its durable
// completion result.
func (w *World) InvokeWorkflow() workflowbridge.Result {
	w.T.Helper()
	ctx := context.Background()
	request := w.workflowRequest("wf-case-1", "workflow-analysis")
	record, created, err := w.admission.Admit(ctx, request)
	if err != nil {
		w.T.Fatal(err)
	}
	if !created || record.Decision != agentrun.DecisionAccepted {
		w.T.Fatalf("workflow admission=%+v created=%v, want the accepted run", record, created)
	}
	w.records[recordKey(journeyTenant, agentrun.SourceWorkflow)] = record
	w.requests[recordKey(journeyTenant, agentrun.SourceWorkflow)] = request
	outputDigest := sha256hex("output:wf-case-1")
	run := runstate.Run{
		ID: record.ID, TenantID: journeyTenant, AdmissionID: record.ID,
		PrincipalMode: agentrun.ModeSponsored, ActorID: journeySponsor,
		RequestDigest: record.RequestDigest,
		AgentID:       record.Authority.Agent.AgentID, AgentVersion: record.Authority.Agent.Version, AgentDigest: record.Authority.Agent.Digest,
		ContextDigest: record.Authority.Context.Digest, Deadline: record.Request.Deadline,
		State: runstate.StateCompleted,
		Checkpoints: []runstate.Checkpoint{
			{Sequence: 1, Phase: runstate.PhaseValidation, Ref: "output:wf-case-1", Digest: outputDigest, At: w.beat()},
			{Sequence: 2, Phase: runstate.PhaseDelivery, Ref: "output:wf-case-1", Digest: outputDigest, At: w.beat()},
		},
	}
	result, err := workflowbridge.Derive(record, run)
	if err != nil {
		w.T.Fatal(err)
	}
	w.Workflow = result
	w.note("workflow:%s %s output=%s", result.RunID, result.Outcome, result.OutputDigest)
	return result
}

// DeriveRefusedWorkflow derives the REFUSED branch of a workflow run.
func (w *World) DeriveRefusedWorkflow() workflowbridge.Result {
	w.T.Helper()
	record, _, err := w.admission.Admit(context.Background(), w.workflowRequest("wf-case-refused", journeyRefusedCause))
	if err != nil {
		w.T.Fatal(err)
	}
	result, err := workflowbridge.Derive(record, runstate.Run{})
	if err != nil {
		w.T.Fatal(err)
	}
	return result
}

// journeyPlan builds the confirmed journey task plan: read the handbook,
// draft the briefing, submit the single governed promotion, verify the
// owner-visible effect, and report the outcome.
func journeyPlan(t *testing.T) agentrun.AgentPlan {
	t.Helper()
	plan, err := agentrun.NewPlan([]agentrun.PlanStep{
		{ID: "read", Type: agentrun.StepRead, SkillID: "team.handbook_read", SkillVersion: 1, ExpectedOutput: "handbook standup passage", Tier: agentrun.TierRead},
		{ID: "draft", Type: agentrun.StepDraft, SkillID: "team.briefing_draft", SkillVersion: 1, ExpectedOutput: "morning briefing draft", Tier: agentrun.TierPrivateDraft, Inputs: []agentrun.InputRef{{Name: "read", Ref: "read:journey-1"}}},
		{ID: "submit", Type: agentrun.StepSubmit, SkillID: "team.promotion_submit", SkillVersion: 1, ExpectedOutput: "submitted promotion intent with agent-for-owner chain", Tier: agentrun.TierSubmitGoverned, Inputs: []agentrun.InputRef{{Name: "draft", Ref: "draft:journey-1"}, {Name: "goal", Ref: "task:" + journeyTask, Taint: []string{"USER_AUTHORED"}}}},
		{ID: "verify", Type: agentrun.StepVerify, SkillID: "team.effect_verify", SkillVersion: 1, ExpectedOutput: "owner re-read matches the submitted intent", Tier: agentrun.TierRead, Inputs: []agentrun.InputRef{{Name: "submit", Ref: "submit:journey-1"}}},
		{ID: "report", Type: agentrun.StepCommunicate, SkillID: "team.outcome_report", SkillVersion: 1, ExpectedOutput: "owner-observed outcome report", Tier: agentrun.TierCommunicate, Destination: "task-thread:" + journeyTask, DestinationConfirmed: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// exec runs the current step and records the new task state.
func (w *World) exec(stepID string) agentrun.AgentTask {
	w.T.Helper()
	task, err := w.runtime.GetTask(context.Background(), journeyTask)
	if err != nil {
		w.T.Fatal(err)
	}
	if task.Plan.Steps[task.CurrentStep].ID != stepID {
		w.T.Fatalf("cursor is at %s, want step %s", task.Plan.Steps[task.CurrentStep].ID, stepID)
	}
	next, err := w.runtime.ExecuteNext(context.Background(), journeyTask, task.Version, w, w, w.beat())
	if err != nil {
		w.T.Fatal(err)
	}
	w.task = next
	return next
}

// ParkForApproval creates the journey task, confirms its plan, runs the
// read and draft steps, parks the submit step for approval, and opens the
// exact governed approval card.
func (w *World) ParkForApproval() {
	w.T.Helper()
	ctx := context.Background()
	docCtx, err := agentrun.WithDocumentReferences(ctx, []agentdocref.Reference{{DocumentID: journeyDocID, Label: journeyDocLabel, VersionMode: agentdocref.ModeLatestPublished}})
	if err != nil {
		w.T.Fatal(err)
	}
	created, err := w.runtime.CreateTask(docCtx, agentrun.CreateRequest{
		ID: journeyTask, TenantID: journeyTenant, UserID: journeyOwner, Goal: journeyGoal,
		Constraints: []string{"cite the handbook", "one governed promotion only"},
		Plan:        journeyPlan(w.T), Now: w.beat(), ExpiresAt: w.clock().Add(24 * time.Hour),
	})
	if err != nil {
		w.T.Fatal(err)
	}
	confirmed, err := w.runtime.ConfirmPlan(ctx, created.ID, journeyOwner, created.Version, w.beat())
	if err != nil {
		w.T.Fatal(err)
	}
	if !confirmed.Plan.Confirmed || confirmed.Plan.ConfirmedBy != journeyOwner {
		w.T.Fatalf("plan confirmed=%v by=%q, want the owner's confirmation", confirmed.Plan.Confirmed, confirmed.Plan.ConfirmedBy)
	}
	w.task, w.hasTask = confirmed, true
	w.exec("read")
	w.exec("draft")
	task, err := w.runtime.GetTask(ctx, journeyTask)
	if err != nil {
		w.T.Fatal(err)
	}
	if _, err := w.runtime.ExecuteNext(ctx, journeyTask, task.Version, w, w, w.beat()); err == nil {
		w.T.Fatal("the unapproved submit step executed without parking for approval")
	} else if !errors.Is(err, agentrun.ErrApprovalRequired) {
		w.T.Fatal(err)
	}
	parked, err := w.runtime.GetTask(ctx, journeyTask)
	if err != nil {
		w.T.Fatal(err)
	}
	w.task = parked
	w.createCard()
}

// StepApprovalDigest returns the parked submit step's exact approval digest.
func (w *World) StepApprovalDigest() string {
	w.T.Helper()
	if !w.hasTask {
		w.T.Fatal("park the task before reading its approval digest")
	}
	return w.task.Plan.Steps[w.task.CurrentStep].ApprovalDigest
}

// createCard opens the journey's single governed approval card.
func (w *World) createCard() {
	w.T.Helper()
	approval, err := w.approvals.Create(agentapproval.CreateRequest{
		ID: journeyApprovalID, TenantID: journeyTenant, TaskID: journeyTask,
		AssignedUserID: journeyOwner, AgentID: journeyAgent,
		OriginChain: []agentapproval.ActorRef{{Kind: agentapproval.ActorAgent, ID: journeyAgent}},
		Items: []agentapproval.ActionItem{{
			ID: "item-level-omar", Kind: agentapproval.ItemGovernedIntent, Tier: agentapproval.TierSubmitGoverned,
			IntentDefinitionID: promotion.IntentType, IntentDefinitionVersion: promotion.IntentVersion,
			Subjects: []agentapproval.Subject{{ID: journeyWorker, Kind: "worker"}},
			MaterialFields: []agentapproval.MaterialField{
				{Path: "promotion.level", Before: "P2", After: "P3"},
				{Path: "promotion.base_pay", Before: "90000 USD", After: "98000 USD"},
			},
			Sources:     []agentapproval.Source{{Ref: "document:" + journeyDocID, Taint: "EXTERNAL_UNTRUSTED"}},
			Uncertainty: "level and pay from the handbook extraction, verified against the governing band",
			RiskClass:   "HIGH",
		}},
		TaskExpiresAt: w.clock().Add(24 * time.Hour),
	})
	if err != nil {
		w.T.Fatal(err)
	}
	w.Approval = approval
}

// ApproveWithDigest attempts a second approval decision under a digest.
func (w *World) ApproveWithDigest(digest string) error {
	w.T.Helper()
	return w.approvals.Approve(context.Background(), agentapproval.ApproveRequest{
		ApprovalID: w.Approval.ID, UserID: journeyOwner, Surface: agentapproval.SurfaceProductTaskView,
		ExpectedDigest: digest, PresentedItemIDs: []string{"item-level-omar"},
		DecisionOrigin: []agentapproval.ActorRef{{Kind: agentapproval.ActorHuman, ID: journeyOwner}},
		StepUpProof:    w.stepupProof(w.Approval.ID, digest),
	}, w.stepupVerifier())
}

// ApprovalState returns the server-held approval card.
func (w *World) ApprovalState() (agentapproval.AgentActionApproval, error) {
	w.T.Helper()
	return w.approvals.Get(w.Approval.ID)
}

// CarryHCMAction approves the card with step-up, approves the parked step
// with its exact digest, submits the single governed intent, verifies the
// owner-visible effect, and reports the outcome.
func (w *World) CarryHCMAction() agentapproval.Receipt {
	w.T.Helper()
	ctx := context.Background()
	card, err := w.approvals.Get(w.Approval.ID)
	if err != nil {
		w.T.Fatal(err)
	}
	if err := w.approvals.Approve(ctx, agentapproval.ApproveRequest{
		ApprovalID: card.ID, UserID: journeyOwner, Surface: agentapproval.SurfaceProductTaskView,
		ExpectedDigest: card.Digest, PresentedItemIDs: []string{"item-level-omar"},
		DecisionOrigin: []agentapproval.ActorRef{{Kind: agentapproval.ActorHuman, ID: journeyOwner}},
		StepUpProof:    w.stepupProof(card.ID, card.Digest),
	}, w.stepupVerifier()); err != nil {
		w.T.Fatal(err)
	}
	w.note("approve:%s digest=%s surface=%s", card.ID, card.Digest, agentapproval.SurfaceProductTaskView)
	task, err := w.runtime.GetTask(ctx, journeyTask)
	if err != nil {
		w.T.Fatal(err)
	}
	step := task.Plan.Steps[task.CurrentStep]
	approved, err := w.runtime.ApproveStep(ctx, journeyTask, step.ID, step.ApprovalDigest, task.Version, w.beat())
	if err != nil {
		w.T.Fatal(err)
	}
	w.task = approved
	w.exec("submit")
	w.exec("verify")
	w.exec("report")
	receipt := w.SubmitReceipt
	w.note("submit:%s key=%s intents=%d replayed=%v", receipt.Digest, receipt.IdempotencyKey, len(w.Journal.Intents), receipt.Replayed)
	w.note("report:done")
	return receipt
}

// ReplaySubmission resubmits the approved action under its original key.
func (w *World) ReplaySubmission() agentapproval.Receipt {
	w.T.Helper()
	receipt, err := w.approvals.Submit(context.Background(), w.Approval.ID, journeyOwner, w.Approval.Digest, journeySubmitKey, w.revalidator(), w.submitter())
	if err != nil {
		w.T.Fatal(err)
	}
	return receipt
}

// Execute implements agentrun.StepExecutor.
func (w *World) Execute(_ context.Context, _ agentrun.AgentTask, step agentrun.PlanStep) (agentrun.StepResult, error) {
	switch step.ID {
	case "read":
		return agentrun.StepResult{Ref: "read:journey-1", SourceIDs: []string{"document:" + journeyDocID}, Taint: []string{"AGENT_DERIVED"}}, nil
	case "draft":
		return agentrun.StepResult{Ref: "draft:journey-1", SourceIDs: []string{"document:" + journeyDocID}, Taint: []string{"AGENT_DERIVED"}}, nil
	case "submit":
		receipt, err := w.approvals.Submit(context.Background(), w.Approval.ID, journeyOwner, w.Approval.Digest, journeySubmitKey, w.revalidator(), w.submitter())
		if err != nil {
			return agentrun.StepResult{}, err
		}
		w.SubmitReceipt = receipt
		return agentrun.StepResult{Ref: "submit:journey-1", Digest: receipt.Digest, SourceIDs: []string{"document:" + journeyDocID}, Taint: []string{"AGENT_DERIVED"}}, nil
	case "verify":
		return agentrun.StepResult{Ref: "verify:journey-1", Taint: []string{"AGENT_DERIVED"}}, nil
	case "report":
		w.Journal.Report = "Team assistant journey complete: one cited answer, one digest, one workflow result, and one approved promotion (P2 to P3)."
		return agentrun.StepResult{Ref: "report:journey-1"}, nil
	default:
		return agentrun.StepResult{}, fmt.Errorf("agentjourney: unknown step %q", step.ID)
	}
}

// Verify implements agentrun.OwnerVerifier: the submitted intent must match
// the approved card exactly.
func (w *World) Verify(_ context.Context, _ agentrun.AgentTask, step agentrun.PlanStep, _ agentrun.StepResult) error {
	if step.ID != "verify" {
		return fmt.Errorf("agentjourney: unexpected verify step %q", step.ID)
	}
	card, err := w.approvals.Get(w.Approval.ID)
	if err != nil {
		return err
	}
	if len(w.Journal.Intents) != 1 || len(card.Items) != 1 {
		return errors.New("agentjourney: owner re-read found no single carried intent")
	}
	intent, item := w.Journal.Intents[0], card.Items[0]
	if intent.ItemID != item.ID || intent.Digest != card.Digest || len(intent.Material) != len(item.MaterialFields) {
		return errors.New("agentjourney: carried intent differs from the approved card")
	}
	for i := range intent.Material {
		if intent.Material[i] != item.MaterialFields[i] {
			return errors.New("agentjourney: carried material differs from the approved card")
		}
	}
	return nil
}

func journeyStepupRequirement() stepup.Requirement {
	return stepup.Requirement{MinAssurance: trust.AssuranceHigh, Recency: 5 * time.Minute}
}

func (w *World) stepupOperation(approvalID, digest string) stepup.Operation {
	return stepup.Operation{
		Action: stepup.ActionApprove, ProposalID: approvalID, Scopes: []string{"approval-digest:" + digest},
		Tenant: values.TenantId(journeyTenant), Purpose: journeyPurpose,
		Capability: promotion.IntentType, Risk: stepup.RiskCritical,
	}
}

func (w *World) stepupProof(approvalID, digest string) stepup.Proof {
	w.T.Helper()
	proof, err := w.issuer.Issue(w.manager, w.stepupOperation(approvalID, digest), journeyStepupRequirement())
	if err != nil {
		w.T.Fatal(err)
	}
	return proof
}

func (w *World) stepupVerifier() agentapproval.StepUpVerifier {
	return agentapproval.StepUpVerifierFunc(func(ctx context.Context, req agentapproval.StepUpRequest) error {
		outcome, err := w.stepGate.Present(ctx, req.Proof, w.stepupOperation(req.ApprovalID, req.Digest), w.manager, journeyStepupRequirement())
		if err != nil {
			return err
		}
		if outcome != stepup.OutcomeExecuted {
			return fmt.Errorf("step-up proof for %s was %s, want a fresh execution", req.ApprovalID, outcome)
		}
		return nil
	})
}

func (w *World) revalidator() agentapproval.Revalidator {
	return agentapproval.RevalidatorFunc(func(_ context.Context, approval agentapproval.AgentActionApproval) (agentapproval.RevalidationResult, error) {
		installation, err := w.installs.Installation(context.Background(), w.Installation.ID)
		if err != nil || installation.Status != install.InstallationActive {
			return agentapproval.RevalidationResult{}, nil
		}
		if approval.ID != w.Approval.ID || len(w.Journal.Runs) == 0 {
			return agentapproval.RevalidationResult{}, nil
		}
		return agentapproval.RevalidationResult{GrantValid: true, AuthorityValid: true, Fresh: true}, nil
	})
}

func (w *World) submitter() agentapproval.Submitter {
	return agentapproval.SubmitterFunc(func(_ context.Context, req agentapproval.SubmissionRequest) error {
		for _, key := range w.Journal.EffectKeys {
			if key == req.IdempotencyKey {
				return errors.New("agentjourney: owner effect already applied for this key")
			}
		}
		for _, item := range req.Items {
			for _, subject := range item.Subjects {
				w.Journal.Intents = append(w.Journal.Intents, SubmittedIntent{
					ItemID: item.ID, IntentType: item.IntentDefinitionID, IntentVersion: item.IntentDefinitionVersion,
					Subject: subject.ID, Material: append([]agentapproval.MaterialField(nil), item.MaterialFields...),
					Digest: req.Digest, IdempotencyKey: req.IdempotencyKey,
				})
			}
		}
		w.Journal.EffectKeys = append(w.Journal.EffectKeys, req.IdempotencyKey)
		return nil
	})
}

// DriveFull runs every journey leg in order.
func (w *World) DriveFull() {
	w.T.Helper()
	w.SelectProvider()
	w.PublishAgent()
	w.InstallAgent()
	w.AnswerDM()
	w.PostDigest()
	w.Participate()
	w.InvokeWorkflow()
	w.ParkForApproval()
	w.CarryHCMAction()
}

// journeyRunners binds the task runtime to the journey tenant.
type journeyRunners struct{ world *World }

// Runner implements agentclient.Runners.
func (r journeyRunners) Runner(_ context.Context, tenant values.TenantId) (agentclient.TaskReader, error) {
	if tenant != values.TenantId(journeyTenant) {
		return nil, errors.New("agentjourney: unknown tenant runtime")
	}
	return r.world, nil
}

// UserTasks implements agentclient.TaskReader.
func (w *World) UserTasks(_ context.Context, userID string) ([]agentrun.AgentTask, error) {
	if userID != journeyOwner || !w.hasTask {
		return nil, nil
	}
	return []agentrun.AgentTask{w.task}, nil
}

// BudgetUsage implements agentclient.TaskReader.
func (w *World) BudgetUsage(_ string) (used, limit agentbudget.Limits, ok bool) {
	return agentbudget.Limits{}, agentbudget.Limits{}, false
}

// journeySnapshot is the durable journey evidence carried across a restart.
type journeySnapshot struct {
	Receipts   []scheduled.Receipt `json:"receipts"`
	Records    []agentrun.Record   `json:"records"`
	Requests   []agentrun.Request  `json:"requests"`
	Tenants    []string            `json:"tenants"`
	Transcript []string            `json:"transcript"`
}

// Snapshot persists the journey's durable evidence for a restart.
func (w *World) Snapshot(dir string) {
	w.T.Helper()
	snapshot := journeySnapshot{Transcript: w.Transcript()}
	for _, receipt := range w.receipts {
		snapshot.Receipts = append(snapshot.Receipts, receipt)
	}
	for _, record := range w.records {
		snapshot.Records = append(snapshot.Records, record)
	}
	for _, request := range w.requests {
		snapshot.Requests = append(snapshot.Requests, request)
	}
	for tenant := range w.tenants {
		snapshot.Tenants = append(snapshot.Tenants, tenant)
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		w.T.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agentjourney_snapshot.json"), raw, 0o600); err != nil {
		w.T.Fatal(err)
	}
}

// RestoreWorld rebuilds a world over persisted journey evidence.
func RestoreWorld(t *testing.T, path string) *World {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot journeySnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	world := NewWorld(t)
	world.SelectProvider()
	world.PublishAgent()
	world.InstallAgent()
	for _, tenant := range snapshot.Tenants {
		world.tenants[tenant] = true
	}
	for _, receipt := range snapshot.Receipts {
		world.receipts[receipt.SourceKey] = receipt
	}
	ctx := context.Background()
	for _, record := range snapshot.Records {
		if _, _, err := world.admitDrv.CreateOrGet(ctx, record); err != nil {
			t.Fatal(err)
		}
		world.records[recordKey(record.Request.Source.TenantID, record.Request.Source.Kind)] = record
	}
	for _, request := range snapshot.Requests {
		world.requests[recordKey(request.Source.TenantID, request.Source.Kind)] = request
	}
	world.transcript = append([]string(nil), snapshot.Transcript...)
	return world
}

// RevokeAgent engages the kill switch, quarantines the published version,
// and suspends the installation.
func (w *World) RevokeAgent() {
	w.T.Helper()
	ctx := context.Background()
	manifestDigest, err := w.manifest.Digest()
	if err != nil {
		w.T.Fatal(err)
	}
	lease := agentsecurity.Lease{
		ID: "lease-journey-submit", Agent: journeyAgent, AgentVersion: "1",
		PersonaID: journeyPersona, PersonaVersion: "1", InstallationID: w.Installation.ID,
		Model: w.Route.Selected.Identity.ModelID, Tool: journeySkill,
		Tenant: journeyTenant, WriteCapable: true,
	}
	if err := w.kill.Grant(lease); err != nil {
		w.T.Fatal(err)
	}
	if _, err := w.kill.Check(lease.ID); err != nil {
		w.T.Fatal(err)
	}
	revoked, fallback := w.kill.Disable(agentsecurity.DisableScope{Agent: journeyAgent})
	if revoked != 1 || !fallback.NonAIAvailable {
		w.T.Fatalf("revoked=%d fallback=%+v, want the lease revoked with HCM still available", revoked, fallback)
	}
	if err := w.publisher.Quarantine(ctx, journeyTenant, journeyAgent, manifestDigest, "journey-revoked"); err != nil {
		w.T.Fatal(err)
	}
	if _, err := w.publisher.Admit(ctx, journeyTenant, journeyAgent, manifestDigest); err == nil {
		w.T.Fatal("the quarantined version still admits work")
	}
	if len(w.fenceCalls) != 1 {
		w.T.Fatalf("fence calls=%d, want the write fence engaged", len(w.fenceCalls))
	}
	w.SuspendInstallation()
	if w.Installation.Status != install.InstallationSuspended {
		w.T.Fatalf("installation status=%q, want SUSPENDED", w.Installation.Status)
	}
	w.note("revoke:leases=%d fenced=%d", revoked, len(w.fenceCalls))
}

// GrantWriteLease attempts one write-capable lease after revocation.
func (w *World) GrantWriteLease() error {
	w.T.Helper()
	return w.kill.Grant(agentsecurity.Lease{
		ID: "lease-journey-submit-2", Agent: journeyAgent, AgentVersion: "1",
		PersonaID: journeyPersona, PersonaVersion: "1", InstallationID: w.Installation.ID,
		Model: w.Route.Selected.Identity.ModelID, Tool: journeySkill,
		Tenant: journeyTenant, WriteCapable: true,
	})
}
