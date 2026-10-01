package agentapproval

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

type b2Clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *b2Clock) get() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *b2Clock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func b2Item(id string, tier Tier, risk string) ActionItem {
	item := ActionItem{
		ID: id, Kind: ItemGovernedIntent, Tier: tier,
		IntentDefinitionID: "PromoteWorker", IntentDefinitionVersion: "v3",
		Subjects:       []Subject{{ID: "worker-7", Kind: "worker"}},
		MaterialFields: []MaterialField{{Path: "job.level", Before: "L4", After: "L5"}},
		Sources:        []Source{{Ref: "report-7", Taint: "TRUSTED_INTERNAL"}},
		Uncertainty:    "source is current as of the task checkpoint", RiskClass: risk,
	}
	if tier == TierExternalWrite {
		item.Kind = ItemConnectionWrite
		item.IntentDefinitionID = ""
		item.IntentDefinitionVersion = ""
		item.ConnectionID = "hris-prod"
		item.ConnectionOperation = "worker.update"
	}
	return item
}

func b2Create(t *testing.T, service *Service, items ...ActionItem) AgentActionApproval {
	return b2CreateID(t, service, "approval-1", items...)
}

func b2CreateID(t *testing.T, service *Service, id string, items ...ActionItem) AgentActionApproval {
	t.Helper()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	approval, err := service.Create(CreateRequest{
		ID: id, TenantID: "tenant-1", TaskID: "task-1", AssignedUserID: "user-1", AgentID: "agent-1",
		OriginChain: []ActorRef{{Kind: ActorAgent, ID: "agent-1"}}, Items: items, TaskExpiresAt: now.Add(48 * time.Hour), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return approval
}

func b2HumanDecision(a AgentActionApproval) ApproveRequest {
	ids := make([]string, len(a.Items))
	for i, item := range a.Items {
		ids[i] = item.ID
	}
	return ApproveRequest{ApprovalID: a.ID, UserID: a.AssignedUserID, Surface: SurfaceProductTaskView, ExpectedDigest: a.Digest, PresentedItemIDs: ids, DecisionOrigin: []ActorRef{{Kind: ActorHuman, ID: a.AssignedUserID}}}
}

func TestTodo_AGENT2_006(t *testing.T) {
	clock := &b2Clock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	service, err := New(clock.get)
	if err != nil {
		t.Fatal(err)
	}
	approval := b2Create(t, service, b2Item("item-b", TierSubmitGoverned, "low"), b2Item("item-a", TierSubmitGoverned, "low"))
	if approval.Digest == "" || approval.Items[0].ID != "item-a" {
		t.Fatalf("server did not normalize the exact card: %+v", approval)
	}

	chat := b2HumanDecision(approval)
	chat.Surface = ApprovalSurface("CHAT_REACTION")
	if err := service.Approve(context.Background(), chat, nil); !errors.Is(err, ErrWrongSurface) {
		t.Fatalf("chat reaction was accepted: %v", err)
	}

	if err := service.Approve(context.Background(), b2HumanDecision(approval), nil); err != nil {
		t.Fatalf("user approval failed: %v", err)
	}
	var calls atomic.Int32
	receipt, err := service.Submit(context.Background(), approval.ID, approval.AssignedUserID, approval.Digest, "idem-1",
		RevalidatorFunc(func(_ context.Context, current AgentActionApproval) (RevalidationResult, error) {
			if current.Digest != approval.Digest || current.State != StateApproved {
				return RevalidationResult{}, errors.New("revalidator did not receive the exact approved card")
			}
			return RevalidationResult{GrantValid: true, AuthorityValid: true, Fresh: true}, nil
		}),
		SubmitterFunc(func(_ context.Context, req SubmissionRequest) error {
			calls.Add(1)
			if len(req.Items) != 2 || req.Digest != approval.Digest || req.IdempotencyKey != "idem-1" {
				return errors.New("submission was not bound to the approved batch")
			}
			return nil
		}))
	if err != nil || receipt.Replayed || calls.Load() != 1 {
		t.Fatalf("first submission mismatch: receipt=%+v err=%v calls=%d", receipt, err, calls.Load())
	}
	replay, err := service.Submit(context.Background(), approval.ID, approval.AssignedUserID, approval.Digest, "idem-1", RevalidatorFunc(func(context.Context, AgentActionApproval) (RevalidationResult, error) {
		return RevalidationResult{}, errors.New("revalidation must not run on replay")
	}), SubmitterFunc(func(context.Context, SubmissionRequest) error { return errors.New("duplicate effect") }))
	if err != nil || !replay.Replayed || calls.Load() != 1 {
		t.Fatalf("duplicate submission was not an idempotent replay: receipt=%+v err=%v calls=%d", replay, err, calls.Load())
	}
	wrongDigest, err := service.Submit(context.Background(), approval.ID, approval.AssignedUserID, "sha256:wrong", "idem-1", RevalidatorFunc(func(context.Context, AgentActionApproval) (RevalidationResult, error) {
		return RevalidationResult{}, errors.New("revalidation must not run on rejected replay")
	}), SubmitterFunc(func(context.Context, SubmissionRequest) error { return errors.New("duplicate effect") }))
	if !errors.Is(err, ErrDigestMismatch) || wrongDigest.ApprovalID != "" || calls.Load() != 1 {
		t.Fatalf("replay accepted a different digest: receipt=%+v err=%v calls=%d", wrongDigest, err, calls.Load())
	}
	if _, err := service.Submit(context.Background(), approval.ID, approval.AssignedUserID, approval.Digest, "idem-2", RevalidatorFunc(func(context.Context, AgentActionApproval) (RevalidationResult, error) {
		return RevalidationResult{GrantValid: true, AuthorityValid: true, Fresh: true}, nil
	}), SubmitterFunc(func(context.Context, SubmissionRequest) error { return nil })); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting retry returned %v", err)
	}
}

func TestTodo_AGENT2_006_Golden(t *testing.T) {
	clock := &b2Clock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	service, err := New(clock.get)
	if err != nil {
		t.Fatal(err)
	}
	approval := b2Create(t, service, b2Item("item-1", TierSubmitGoverned, "high"))
	const want = "sha256:875c724456d95fe116c0eededdb10b2c35251f664cd9e3cd7c208a707125c3e5"
	if approval.Digest != want {
		t.Fatalf("approval digest changed: got %q want %q", approval.Digest, want)
	}
	if Digest(approval) != want {
		t.Fatalf("digest is not reproducible")
	}

	left := b2Item("item-ties", TierSubmitGoverned, "low")
	left.Subjects = []Subject{{ID: "same", Kind: "worker"}, {ID: "same", Kind: "position"}}
	left.MaterialFields = []MaterialField{{Path: "same.path", Before: "L5", After: "L6"}, {Path: "same.path", Before: "L4", After: "L5"}}
	right := b2Item("item-ties", TierSubmitGoverned, "low")
	right.Subjects = []Subject{{ID: "same", Kind: "position"}, {ID: "same", Kind: "worker"}}
	right.MaterialFields = []MaterialField{{Path: "same.path", Before: "L4", After: "L5"}, {Path: "same.path", Before: "L5", After: "L6"}}
	first := b2CreateID(t, service, "approval-ties-a", left)
	second := b2CreateID(t, service, "approval-ties-b", right)
	if first.Digest != second.Digest {
		t.Fatalf("permuted tied material rows produced different digests: %s != %s", first.Digest, second.Digest)
	}
}

func TestTodo_AGENT2_006_Security(t *testing.T) {
	clock := &b2Clock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	service, err := New(clock.get)
	if err != nil {
		t.Fatal(err)
	}
	approval := b2Create(t, service, b2Item("item-1", TierExternalWrite, "critical"))
	request := b2HumanDecision(approval)
	if err := service.Approve(context.Background(), request, nil); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("high-risk approval without step-up returned %v", err)
	}
	request.DecisionOrigin = []ActorRef{{Kind: ActorHuman, ID: "user-1"}, {Kind: ActorAgent, ID: "agent-1"}}
	if err := service.Approve(context.Background(), request, StepUpVerifierFunc(func(context.Context, StepUpRequest) error { return nil })); !errors.Is(err, ErrAgentCannotApprove) {
		t.Fatalf("agent actor chain was accepted: %v", err)
	}
	request.DecisionOrigin = []ActorRef{{Kind: ActorHuman, ID: "user-1"}}
	request.PresentedItemIDs = nil
	if err := service.Approve(context.Background(), request, StepUpVerifierFunc(func(context.Context, StepUpRequest) error { return nil })); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("hidden batch item was accepted: %v", err)
	}
	request.PresentedItemIDs = []string{"item-1"}
	request.StepUpProof = stepup.Proof{ID: "step-up-1"}
	var verified atomic.Bool
	if err := service.Approve(context.Background(), request, StepUpVerifierFunc(func(_ context.Context, got StepUpRequest) error {
		verified.Store(got.ApprovalID == approval.ID && got.Digest == approval.Digest && got.UserID == "user-1")
		return nil
	})); err != nil {
		t.Fatalf("step-up approval failed: %v", err)
	}
	if !verified.Load() {
		t.Fatal("step-up verifier did not receive the exact digest binding")
	}

	other := b2CreateID(t, service, "approval-2", b2Item("item-2", TierSubmitGoverned, "low"))
	unauthorized := b2HumanDecision(other)
	unauthorized.UserID = "user-2"
	unauthorized.DecisionOrigin = []ActorRef{{Kind: ActorHuman, ID: "user-2"}}
	if err := service.Approve(context.Background(), unauthorized, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unassigned user was accepted: %v", err)
	}
}

func TestTodo_AGENT2_006_Race(t *testing.T) {
	clock := &b2Clock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	service, err := New(clock.get)
	if err != nil {
		t.Fatal(err)
	}
	approval := b2Create(t, service, b2Item("item-1", TierSubmitGoverned, "low"))
	request := b2HumanDecision(approval)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var approved, revoked atomic.Int32
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		if err := service.Approve(context.Background(), request, nil); err == nil {
			approved.Add(1)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		if err := service.Revoke(approval.ID, "user revoked before submission"); err == nil {
			revoked.Add(1)
		}
	}()
	close(start)
	wg.Wait()
	if approved.Load()+revoked.Load() == 0 {
		t.Fatalf("approve and revoke both lost: approved=%d revoked=%d", approved.Load(), revoked.Load())
	}
	current, err := service.Get(approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != StateApproved && current.State != StateRevoked {
		t.Fatalf("concurrent decisions did not converge on one terminal decision: %+v", current)
	}
}

func TestTodo_AGENT2_006_Mutation(t *testing.T) {
	clock := &b2Clock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	service, err := New(clock.get)
	if err != nil {
		t.Fatal(err)
	}
	item := b2Item("item-1", TierSubmitGoverned, "low")
	approval := b2Create(t, service, item)
	mutated := approval
	mutated.Items[0].MaterialFields[0].After = "L99"
	mutated.Items[0].Sources[0].Taint = "UNTRUSTED_PEER"
	if Digest(mutated) == approval.Digest {
		t.Fatal("material mutation did not change the digest")
	}
	read, err := service.Get(approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	read.Items[0].MaterialFields[0].After = "L99"
	readAgain, err := service.Get(approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if readAgain.Items[0].MaterialFields[0].After != "L5" {
		t.Fatal("caller mutation changed the server-held approval")
	}
	request := b2HumanDecision(approval)
	request.ExpectedDigest = Digest(mutated)
	if err := service.Approve(context.Background(), request, nil); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("mutated digest was accepted: %v", err)
	}
	clock.advance(25 * time.Hour)
	if err := service.Approve(context.Background(), b2HumanDecision(approval), nil); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired approval returned %v", err)
	}
}

func TestTodo_AGENT2_006_RequiresTaintedSources(t *testing.T) {
	item := b2Item("item-1", TierSubmitGoverned, "low")
	item.Sources = []Source{{Ref: "source", Taint: ""}}
	service, err := New(func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(CreateRequest{TenantID: "tenant", TaskID: "task", AssignedUserID: "user", AgentID: "agent", Items: []ActionItem{item}, TaskExpiresAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("untainted source returned %v", err)
	}
}

var _ StepUpVerifier = StepUpVerifierFunc(nil)
var _ Revalidator = RevalidatorFunc(nil)
var _ Submitter = SubmitterFunc(nil)
var _ = stepup.ActionApprove
