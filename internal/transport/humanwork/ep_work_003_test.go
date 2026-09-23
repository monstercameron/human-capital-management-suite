package humanwork

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// completionOps is EP-WORK-003's test double for [Completions] and
// [Decisions], built on the same shared item set [workStore] (claim_release_
// test.go) already provides for [Reader] and [Claims] -- Complete and Decide
// mutate the identical map under the identical lock, so a fixture claimed
// and started through workStore's own machinery is immediately visible here.
//
// What this fake hand-mimics is deliberately narrow: the item_version
// compare-and-swap and the IN_PROGRESS -> COMPLETED transition
// [workitem.Store.Complete] performs against real Postgres, plus a
// programmable stand-in for [workitem.AuthorityRecheckPort] and
// [workitem.SessionRevocationPort] (denyAuthority, revokedSession) so this
// suite can drive every current-authority and session RED clause without a
// real driver, which is composed outside this package's file root. It
// reimplements no digest formula, no stale-proposal rule and no decision
// vocabulary of its own: [workitem.DecisionDigest], the item's own
// ProposalRef and [workitem.ApprovalDecision] are the real package values.
type completionOps struct {
	*workStore

	denyAuthority  map[string]bool // workItemID -> current-authority recheck refuses (changed manager, revoked delegation, SoD, ...)
	revokedSession map[string]bool // workItemID -> session check refuses
	completeCalls  atomic.Int64
	decideCalls    atomic.Int64
}

func newCompletionOps(store *workStore) *completionOps {
	return &completionOps{workStore: store, denyAuthority: map[string]bool{}, revokedSession: map[string]bool{}}
}

func (c *completionOps) Complete(_ context.Context, _, workItemID, _, principal string, expectedVersion uint64,
	completedOutputDigest string, now time.Time, _ workitem.TransitionMeta,
) (workitem.WorkItem, error) {
	c.completeCalls.Add(1)
	return c.apply(workItemID, principal, expectedVersion, completedOutputDigest, now)
}

func (c *completionOps) Decide(_ context.Context, _, workItemID, _, principal string, expectedVersion uint64,
	proposalRevisionRef string, decision workitem.ApprovalDecision, reasonRef string, now time.Time, _ workitem.TransitionMeta,
) (workitem.WorkItem, error) {
	c.decideCalls.Add(1)
	c.mu.Lock()
	item, ok := c.items[workItemID]
	c.mu.Unlock()
	if !ok {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeWorkItemNotFound, WorkItemID: workItemID}
	}
	if item.Kind != workitem.KindApproval {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeIllegalTransition, WorkItemID: workItemID}
	}
	if item.ProposalRef == "" || item.ProposalRef != proposalRevisionRef {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeStaleProposal, WorkItemID: workItemID}
	}
	digest := workitem.DecisionDigest(item.WorkItemID, proposalRevisionRef, decision, reasonRef)
	return c.apply(workItemID, principal, expectedVersion, digest, now)
}

// apply is Complete and Decide's shared write, matching
// [workitem.Store.Complete]'s own shape: version-guarded, session-checked,
// authority-rechecked, then exactly one IN_PROGRESS -> COMPLETED transition.
func (c *completionOps) apply(workItemID, principal string, expectedVersion uint64, digest string, now time.Time) (workitem.WorkItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[workItemID]
	if !ok {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeWorkItemNotFound, WorkItemID: workItemID}
	}
	if uint64(item.ItemVersion) != expectedVersion {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeStaleItem, WorkItemID: workItemID}
	}
	if c.revokedSession[workItemID] {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeSessionRevoked, WorkItemID: workItemID}
	}
	if c.denyAuthority[workItemID] {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeAuthorityChanged, WorkItemID: workItemID}
	}
	if item.Status != workitem.StatusInProgress {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeIllegalTransition, WorkItemID: workItemID}
	}
	completedAt := now
	item.Status = workitem.StatusCompleted
	item.CompletedBy = principal
	item.CompletedAt = &completedAt
	item.CompletedOutputDigest = digest
	item.ClaimID, item.ClaimedBy, item.ClaimedAt, item.ClaimExpiresAt = nil, "", nil, nil
	item.ItemVersion++
	c.items[workItemID] = item
	c.transitions = append(c.transitions, fmt.Sprintf("%s->COMPLETED@%d", workItemID, item.ItemVersion))
	return item, nil
}

func completionDeps(store *completionOps, extra map[string]bool) Dependencies {
	return Dependencies{
		Queue: store.workStore, Completions: store, Decisions: store, Idempotency: endpoint.NewCoordinator(),
		Now: func() time.Time { return workNow },
		Authorize: func(_ context.Context, _ *trust.Principal, action string) bool {
			if extra[action] {
				return true
			}
			return action == ActionCompleteWorkItem || action == ActionDecideApproval
		},
	}
}

// approvalWorkItem builds a CLAIMED-then-IN_PROGRESS APPROVAL work item bound
// to proposal, owned and claimed by transporttest.Subject -- exactly
// [workitem.PermittedActions]' own precondition for "decide_approval".
func approvalWorkItem(proposal string) workitem.WorkItem {
	return queueItem(func(w *workitem.WorkItem) {
		claimedItem(workitem.KindApproval)(w)
		w.ProposalRef = proposal
	})
}

// TestWorkCompletionAndApprovalEndpointsRejectStaleProposalAuthorityAndDuplicateDecision
// is EP-WORK-003's PRIMARY: CompleteWorkItem and DecideApproval both complete
// a claimed, IN_PROGRESS work item exactly once through the real wire types;
// a caller-asserted proposal revision that no longer matches the item's
// current one is refused before either write; a current-authority recheck
// that no longer admits the caller (a changed manager, a revoked delegation,
// or -- composed by whichever [workitem.AuthorityRecheckPort] a real driver
// injects -- the requester deciding their own proposal) is refused the same
// way; and a duplicate decision -- an exact idempotency-key replay -- returns
// the original result without a second write, while a replay of the same key
// under a mutated payload is refused rather than mutating the recorded one.
func TestWorkCompletionAndApprovalEndpointsRejectStaleProposalAuthorityAndDuplicateDecision(t *testing.T) {
	proposal := "sha256:" + strings.Repeat("1", 64)

	t.Run("CompleteWorkItem completes a claimed in-progress item exactly once", func(t *testing.T) {
		item := queueItem(claimedItem(workitem.KindTask))
		store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
		srv := &server{deps: completionDeps(store, nil)}
		ctx := humanworkContext(t, CompleteWorkItemProcedure)
		req := &humanworkv1.CompleteWorkItemRequest{
			IdempotencyKey: "complete-key-1", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
			OutputArtifactRef: "artifact:review-notes/v1", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "ev-1", Digest: "sha256:" + strings.Repeat("2", 64)}},
		}
		res, err := srv.CompleteWorkItem(ctx, req)
		if err != nil {
			t.Fatalf("CompleteWorkItem: %v", err)
		}
		got := res.GetWorkItem()
		if got.GetStatus() != humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_COMPLETED || got.GetItemVersion() != uint64(item.ItemVersion)+1 {
			t.Fatalf("complete result = %s", Explain(got))
		}
		if store.completeCalls.Load() != 1 {
			t.Fatalf("complete effect ran %d times, want 1", store.completeCalls.Load())
		}
	})

	t.Run("DecideApproval decides a claimed in-progress approval exactly once", func(t *testing.T) {
		item := approvalWorkItem(proposal)
		store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
		srv := &server{deps: completionDeps(store, nil)}
		ctx := humanworkContext(t, DecideApprovalProcedure)
		req := &humanworkv1.DecideApprovalRequest{
			IdempotencyKey: "decide-key-1", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
			ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "reason.evidence-sufficient/v1",
		}
		res, err := srv.DecideApproval(ctx, req)
		if err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}
		got := res.GetDecision()
		if got.GetWorkItemId() != item.WorkItemID.String() || got.GetProposalRevisionId() != proposal ||
			got.GetDecision() != intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE ||
			got.GetReasonRef() != "reason.evidence-sufficient/v1" || got.GetDecidingPrincipal().GetPrincipalId() != transporttest.Subject {
			t.Fatalf("decide result = %+v", got)
		}
		if store.decideCalls.Load() != 1 {
			t.Fatalf("decide effect ran %d times, want 1", store.decideCalls.Load())
		}

		// A duplicate decision: an exact replay of the same idempotency key
		// and payload returns the identical result without a second write.
		replay, err := srv.DecideApproval(ctx, req)
		if err != nil {
			t.Fatalf("DecideApproval replay: %v", err)
		}
		if replay.GetDecision().GetDecidedAt().AsTime() != got.GetDecidedAt().AsTime() {
			t.Fatalf("replay decided_at diverged: %v vs %v", replay.GetDecision().GetDecidedAt(), got.GetDecidedAt())
		}
		if store.decideCalls.Load() != 1 {
			t.Fatalf("decide effect ran %d times after replay, want still 1", store.decideCalls.Load())
		}

		// The same key with a mutated payload (a different decision) is a
		// conflict, never a second write mutating the recorded decision.
		mutated := &humanworkv1.DecideApprovalRequest{
			IdempotencyKey: "decide-key-1", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
			ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_REJECT, ReasonRef: "reason.evidence-sufficient/v1",
		}
		var envErr *envelope.Error
		if _, err := srv.DecideApproval(ctx, mutated); !errors.As(err, &envErr) || envErr.Code() != envelope.CodeAborted {
			t.Fatalf("mutated replay err = %v, want ABORTED", err)
		}
		if store.decideCalls.Load() != 1 {
			t.Fatalf("decide effect ran %d times after a mutated replay, want still 1", store.decideCalls.Load())
		}
		final, _ := store.LoadItem(context.Background(), transporttest.Tenant, item.WorkItemID.String())
		if final.CompletedOutputDigest != workitem.DecisionDigest(item.WorkItemID, proposal, workitem.ApprovalDecisionApprove, "reason.evidence-sufficient/v1") {
			t.Fatal("a mutated replay changed the recorded decision's digest")
		}
	})

	t.Run("a stale proposal revision refuses before any write", func(t *testing.T) {
		item := approvalWorkItem(proposal)
		store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
		srv := &server{deps: completionDeps(store, nil)}
		ctx := humanworkContext(t, DecideApprovalProcedure)
		stale := "sha256:" + strings.Repeat("9", 64)
		_, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
			IdempotencyKey: "stale-proposal", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
			ProposalRevisionId: stale, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "reason.x/v1",
		})
		var envErr *envelope.Error
		if !errors.As(err, &envErr) || envErr.Code() != envelope.CodeFailedPrecondition {
			t.Fatalf("stale proposal err = %v, want FAILED_PRECONDITION", err)
		}
		if store.decideCalls.Load() != 1 {
			// The effect runs (the port itself performs the check), but it
			// must not have written anything.
			t.Fatalf("decide effect ran %d times, want 1", store.decideCalls.Load())
		}
		final, _ := store.LoadItem(context.Background(), transporttest.Tenant, item.WorkItemID.String())
		if final.Status != workitem.StatusInProgress || final.ItemVersion != item.ItemVersion {
			t.Fatalf("a refused stale-proposal decision mutated the item: %s v%d", final.Status, final.ItemVersion)
		}
	})

	t.Run("a changed current authority refuses before any write", func(t *testing.T) {
		item := approvalWorkItem(proposal)
		store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
		store.denyAuthority[item.WorkItemID.String()] = true
		srv := &server{deps: completionDeps(store, nil)}
		ctx := humanworkContext(t, DecideApprovalProcedure)
		_, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
			IdempotencyKey: "authority-changed", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
			ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "reason.x/v1",
		})
		var envErr *envelope.Error
		if !errors.As(err, &envErr) || envErr.Code() != envelope.CodePermissionDenied {
			t.Fatalf("authority-changed err = %v, want PERMISSION_DENIED", err)
		}
		final, _ := store.LoadItem(context.Background(), transporttest.Tenant, item.WorkItemID.String())
		if final.Status != workitem.StatusInProgress {
			t.Fatalf("a refused authority-changed decision mutated the item: %s", final.Status)
		}
	})
}

// TestTodo_EP_WORK_003_Property proves the REFACTOR clause directly:
// CompleteWorkItem and DecideApproval, driven with equivalent items and an
// equivalent authority/session posture, refuse identically -- neither method
// has its own bespoke current-authority or session mechanic.
func TestTodo_EP_WORK_003_Property(t *testing.T) {
	proposal := "sha256:" + strings.Repeat("3", 64)
	for _, tc := range []struct {
		name          string
		deny          bool
		revoked       bool
		wantCode      envelope.Code
		wantCallsMore int64
	}{
		{"allowed", false, false, 0, 1},
		{"authority denied", true, false, envelope.CodePermissionDenied, 1},
		{"session revoked", false, true, envelope.CodeUnauthenticated, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskItem := queueItem(claimedItem(workitem.KindTask))
			approvalItem := approvalWorkItem(proposal)
			taskStore := newCompletionOps(newWorkStore(transporttest.Tenant, taskItem))
			approvalStore := newCompletionOps(newWorkStore(transporttest.Tenant, approvalItem))
			taskStore.denyAuthority[taskItem.WorkItemID.String()] = tc.deny
			approvalStore.denyAuthority[approvalItem.WorkItemID.String()] = tc.deny
			taskStore.revokedSession[taskItem.WorkItemID.String()] = tc.revoked
			approvalStore.revokedSession[approvalItem.WorkItemID.String()] = tc.revoked

			completeSrv := &server{deps: completionDeps(taskStore, nil)}
			decideSrv := &server{deps: completionDeps(approvalStore, nil)}
			completeCtx := humanworkContext(t, CompleteWorkItemProcedure)
			decideCtx := humanworkContext(t, DecideApprovalProcedure)

			_, completeErr := completeSrv.CompleteWorkItem(completeCtx, &humanworkv1.CompleteWorkItemRequest{
				IdempotencyKey: "prop-complete", WorkItemId: taskItem.WorkItemID.String(), ExpectedItemVersion: uint64(taskItem.ItemVersion),
				OutputArtifactRef: "artifact:x/v1", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "e"}},
			})
			_, decideErr := decideSrv.DecideApproval(decideCtx, &humanworkv1.DecideApprovalRequest{
				IdempotencyKey: "prop-decide", WorkItemId: approvalItem.WorkItemID.String(), ExpectedItemVersion: uint64(approvalItem.ItemVersion),
				ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "reason.x/v1",
			})

			var completeEnv, decideEnv *envelope.Error
			completeIsEnv := errors.As(completeErr, &completeEnv)
			decideIsEnv := errors.As(decideErr, &decideEnv)
			if tc.wantCode == 0 {
				if completeErr != nil || decideErr != nil {
					t.Fatalf("expected both to succeed: complete=%v decide=%v", completeErr, decideErr)
				}
				return
			}
			if !completeIsEnv || completeEnv.Code() != tc.wantCode {
				t.Fatalf("CompleteWorkItem err = %v, want code %v", completeErr, tc.wantCode)
			}
			if !decideIsEnv || decideEnv.Code() != tc.wantCode {
				t.Fatalf("DecideApproval err = %v, want code %v", decideErr, tc.wantCode)
			}
		})
	}
}

// TestTodo_EP_WORK_003_Golden pins the exact wire projection of a decided
// approval: the field values a client parses, joined in a fixed order.
func TestTodo_EP_WORK_003_Golden(t *testing.T) {
	proposal := "sha256:" + strings.Repeat("4", 64)
	item := approvalWorkItem(proposal)
	store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
	srv := &server{deps: completionDeps(store, nil)}
	ctx := humanworkContext(t, DecideApprovalProcedure)
	res, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
		IdempotencyKey: "golden-decide", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
		ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "reason.golden/v1",
	})
	if err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}
	got := res.GetDecision()
	golden := fmt.Sprintf("%s|%s|%s|%s|%s", got.GetWorkItemId(), got.GetProposalRevisionId(), got.GetDecision(), got.GetReasonRef(), got.GetDecidingPrincipal().GetPrincipalId())
	want := fmt.Sprintf("%s|%s|%s|%s|%s", item.WorkItemID, proposal, "APPROVAL_DECISION_KIND_APPROVE", "reason.golden/v1", transporttest.Subject)
	if golden != want {
		t.Fatalf("decide projection golden = %q, want %q", golden, want)
	}
}

// TestTodo_EP_WORK_003_Race drives real goroutines, each a genuinely distinct
// logical request (its own idempotency key) at the same AVAILABLE-turned-
// IN_PROGRESS approval item and the same expected version: exactly one may
// record a decision, and the item ends with exactly one COMPLETED
// transition -- RACE's requirement without the race detector, since it
// asserts an exact count rather than needing one.
func TestTodo_EP_WORK_003_Race(t *testing.T) {
	proposal := "sha256:" + strings.Repeat("5", 64)
	item := approvalWorkItem(proposal)
	store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
	srv := &server{deps: completionDeps(store, nil)}
	ctx := humanworkContext(t, DecideApprovalProcedure)

	const racers = 8
	results := make([]error, racers)
	var wg, start sync.WaitGroup
	start.Add(1)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			_, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
				IdempotencyKey: fmt.Sprintf("race-%d", i), WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
				ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "reason.race/v1",
			})
			results[i] = err
		}(i)
	}
	start.Done()
	wg.Wait()

	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
			continue
		}
		var envErr *envelope.Error
		if !errors.As(err, &envErr) || (envErr.Code() != envelope.CodeAborted && envErr.Code() != envelope.CodeFailedPrecondition) {
			t.Errorf("loser err = %v, want ABORTED or FAILED_PRECONDITION", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d of %d racers won the decision; exactly one may", wins, racers)
	}
	completedRows := 0
	for _, tr := range store.transitions {
		if strings.Contains(tr, "->COMPLETED@") {
			completedRows++
		}
	}
	if completedRows != 1 {
		t.Fatalf("%d COMPLETED transitions recorded, want exactly 1", completedRows)
	}
}

// TestTodo_EP_WORK_003_Integration proves the connect/gRPC direct path and
// the HTTP edge path agree, for CompleteWorkItem, exactly as EP-WORK-002's
// own integration test does for ClaimWorkItem.
func TestTodo_EP_WORK_003_Integration(t *testing.T) {
	grpcItem := queueItem(claimedItem(workitem.KindTask))
	grpcStore := newCompletionOps(newWorkStore(transporttest.Tenant, grpcItem))
	grpcSrv := &server{deps: completionDeps(grpcStore, nil)}
	grpcCtx := humanworkContext(t, CompleteWorkItemProcedure)

	grpcRes, err := grpcSrv.CompleteWorkItem(grpcCtx, &humanworkv1.CompleteWorkItemRequest{
		IdempotencyKey: "integration-grpc", WorkItemId: grpcItem.WorkItemID.String(), ExpectedItemVersion: uint64(grpcItem.ItemVersion),
		OutputArtifactRef: "artifact:integration/v1", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "e"}},
	})
	if err != nil {
		t.Fatalf("direct (gRPC-shaped) CompleteWorkItem: %v", err)
	}

	httpItem := queueItem(claimedItem(workitem.KindTask))
	httpStore := newCompletionOps(newWorkStore(transporttest.Tenant, httpItem))
	handler := NewHandler(completionDeps(httpStore, nil))
	mux, ok := handler.(*http.ServeMux)
	if !ok {
		t.Fatalf("NewHandler returned %T, want *http.ServeMux", handler)
	}
	httpSrv := httptest.NewServer(withInvocation(mux, grpcCtx))
	t.Cleanup(httpSrv.Close)
	body := fmt.Sprintf(`{"idempotencyKey":"integration-http","workItemId":"%s","expectedItemVersion":"%d","outputArtifactRef":"artifact:integration/v1","evidenceRefs":[{"evidenceId":"e"}]}`,
		httpItem.WorkItemID.String(), httpItem.ItemVersion)
	resp, err := httpSrv.Client().Post(httpSrv.URL+CompleteWorkItemProcedure, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST CompleteWorkItem: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST CompleteWorkItem status = %d, want 200", resp.StatusCode)
	}

	grpcFinal := grpcRes.GetWorkItem()
	httpFinal, loadErr := httpStore.LoadItem(context.Background(), transporttest.Tenant, httpItem.WorkItemID.String())
	if loadErr != nil {
		t.Fatalf("LoadItem after HTTP complete: %v", loadErr)
	}
	if grpcFinal.GetStatus().String() != "WORK_ITEM_STATUS_"+string(httpFinal.Status) {
		t.Fatalf("gRPC status %s vs HTTP status %s diverged", grpcFinal.GetStatus(), httpFinal.Status)
	}
	if grpcFinal.GetItemVersion() != uint64(httpFinal.ItemVersion) {
		t.Fatalf("gRPC version %d vs HTTP version %d diverged", grpcFinal.GetItemVersion(), httpFinal.ItemVersion)
	}
}

// TestTodo_EP_WORK_003_Fault covers the two FAULT clauses this todo adds
// beyond WORK-006's own: missing required evidence refuses before the
// completion port is ever reached, and a revoked session refuses a decision
// (the same revoked-session behavior [workitem.CompleteWithAuthorityRecheck]
// already guarantees, proven here through the wire).
func TestTodo_EP_WORK_003_Fault(t *testing.T) {
	t.Run("missing evidence refuses before the completion port runs", func(t *testing.T) {
		item := queueItem(claimedItem(workitem.KindTask))
		store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
		srv := &server{deps: completionDeps(store, nil)}
		ctx := humanworkContext(t, CompleteWorkItemProcedure)
		_, err := srv.CompleteWorkItem(ctx, &humanworkv1.CompleteWorkItemRequest{
			IdempotencyKey: "no-evidence", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
			OutputArtifactRef: "artifact:x/v1",
		})
		var envErr *envelope.Error
		if !errors.As(err, &envErr) || envErr.Code() != envelope.CodeInvalidArgument {
			t.Fatalf("missing-evidence err = %v, want INVALID_ARGUMENT", err)
		}
		if store.completeCalls.Load() != 0 {
			t.Fatal("a request with no evidence reached the completion port")
		}
	})

	t.Run("a revoked session refuses a decision", func(t *testing.T) {
		proposal := "sha256:" + strings.Repeat("6", 64)
		item := approvalWorkItem(proposal)
		store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
		store.revokedSession[item.WorkItemID.String()] = true
		srv := &server{deps: completionDeps(store, nil)}
		ctx := humanworkContext(t, DecideApprovalProcedure)
		_, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
			IdempotencyKey: "revoked-session", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
			ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "reason.x/v1",
		})
		var envErr *envelope.Error
		if !errors.As(err, &envErr) || envErr.Code() != envelope.CodeUnauthenticated {
			t.Fatalf("revoked-session err = %v, want UNAUTHENTICATED", err)
		}
		final, _ := store.LoadItem(context.Background(), transporttest.Tenant, item.WorkItemID.String())
		if final.Status != workitem.StatusInProgress {
			t.Fatalf("a refused revoked-session decision mutated the item: %s", final.Status)
		}
	})
}

// TestTodo_EP_WORK_003_Security proves: an unauthenticated caller and one
// denied the wire-level capability never reach the write port; separation of
// duties holds (the principal who raised the request is refused deciding it,
// modeled here as the driver's [workitem.AuthorityRecheckPort] refusing --
// exactly the same code path a changed manager or a revoked delegation
// produces, so this suite cannot tell them apart from the wire and neither
// can an attacker); and restricted evidence supplied on a completion request
// never appears anywhere in the response.
func TestTodo_EP_WORK_003_Security(t *testing.T) {
	item := queueItem(claimedItem(workitem.KindTask))
	store := newCompletionOps(newWorkStore(transporttest.Tenant, item))

	// Unauthenticated: refused before the store is touched at all.
	srv := &server{deps: completionDeps(store, nil)}
	if _, err := srv.CompleteWorkItem(context.Background(), &humanworkv1.CompleteWorkItemRequest{
		IdempotencyKey: "k", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
		OutputArtifactRef: "artifact:x/v1", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "e"}},
	}); err == nil {
		t.Fatal("unauthenticated CompleteWorkItem succeeded")
	}
	if store.completeCalls.Load() != 0 {
		t.Fatal("unauthenticated call reached the completion port")
	}

	// Denied the wire-level capability outright.
	denyAll := Dependencies{Queue: store.workStore, Completions: store, Decisions: store, Idempotency: endpoint.NewCoordinator(),
		Now: func() time.Time { return workNow }, Authorize: func(context.Context, *trust.Principal, string) bool { return false }}
	ctx := humanworkContext(t, CompleteWorkItemProcedure)
	_, err := (&server{deps: denyAll}).CompleteWorkItem(ctx, &humanworkv1.CompleteWorkItemRequest{
		IdempotencyKey: "k", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
		OutputArtifactRef: "artifact:x/v1", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "e"}},
	})
	var envErr *envelope.Error
	if !errors.As(err, &envErr) || envErr.Code() != envelope.CodePermissionDenied {
		t.Fatalf("unauthorized CompleteWorkItem err = %v, want PERMISSION_DENIED", err)
	}

	// Separation of duties: the principal who raised the request cannot
	// approve it. The driver's own AuthorityRecheckPort is where a real SoD
	// fact (requester == decider) is evaluated; this fake models exactly
	// that refusal, and the wire projects it identically to any other
	// current-authority change.
	proposal := "sha256:" + strings.Repeat("7", 64)
	sodItem := approvalWorkItem(proposal)
	sodStore := newCompletionOps(newWorkStore(transporttest.Tenant, sodItem))
	sodStore.denyAuthority[sodItem.WorkItemID.String()] = true // "the requester is the caller" recheck result
	sodSrv := &server{deps: completionDeps(sodStore, nil)}
	sodCtx := humanworkContext(t, DecideApprovalProcedure)
	_, err = sodSrv.DecideApproval(sodCtx, &humanworkv1.DecideApprovalRequest{
		IdempotencyKey: "sod", WorkItemId: sodItem.WorkItemID.String(), ExpectedItemVersion: uint64(sodItem.ItemVersion),
		ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "reason.x/v1",
	})
	if !errors.As(err, &envErr) || envErr.Code() != envelope.CodePermissionDenied {
		t.Fatalf("requester-decides-own-proposal err = %v, want PERMISSION_DENIED", err)
	}
	final, _ := sodStore.LoadItem(context.Background(), transporttest.Tenant, sodItem.WorkItemID.String())
	if final.Status != workitem.StatusInProgress {
		t.Fatal("a refused self-approval attempt mutated the item")
	}

	// Restricted evidence never appears in the response: a canary evidence
	// id is supplied on the request and must not be echoed anywhere on the
	// projected WorkItem.
	canaryItem := queueItem(claimedItem(workitem.KindTask))
	canaryStore := newCompletionOps(newWorkStore(transporttest.Tenant, canaryItem))
	canarySrv := &server{deps: completionDeps(canaryStore, nil)}
	canaryCtx := humanworkContext(t, CompleteWorkItemProcedure)
	const canary = "restricted-medical-record-eb31f9"
	res, err := canarySrv.CompleteWorkItem(canaryCtx, &humanworkv1.CompleteWorkItemRequest{
		IdempotencyKey: "canary", WorkItemId: canaryItem.WorkItemID.String(), ExpectedItemVersion: uint64(canaryItem.ItemVersion),
		OutputArtifactRef: "artifact:x/v1", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: canary, EvidenceKind: "medical-document", Digest: "sha256:" + strings.Repeat("8", 64)}},
	})
	if err != nil {
		t.Fatalf("CompleteWorkItem: %v", err)
	}
	if strings.Contains(res.GetWorkItem().String(), canary) {
		t.Fatalf("restricted evidence id leaked into the completion response: %s", res.GetWorkItem().String())
	}
}

// TestTodo_EP_WORK_003_Conformance covers malformed input for both methods,
// cross-tenant scoping, and the centerpiece: a stale expected item version
// refuses with current-safe precondition data and never reaches the write
// port.
func TestTodo_EP_WORK_003_Conformance(t *testing.T) {
	item := queueItem(claimedItem(workitem.KindTask))
	proposal := "sha256:" + strings.Repeat("2", 64)
	approval := approvalWorkItem(proposal)
	store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
	store.items[approval.WorkItemID.String()] = approval
	srv := &server{deps: completionDeps(store, nil)}
	ctx := humanworkContext(t, CompleteWorkItemProcedure)
	envErr := &envelope.Error{}

	for _, tc := range []struct {
		name string
		call func() error
		want envelope.Code
	}{
		{"complete: empty work item id", func() error {
			_, err := srv.CompleteWorkItem(ctx, &humanworkv1.CompleteWorkItemRequest{IdempotencyKey: "k", ExpectedItemVersion: 1, OutputArtifactRef: "a", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "e"}}})
			return err
		}, envelope.CodeInvalidArgument},
		{"complete: missing output artifact ref", func() error {
			_, err := srv.CompleteWorkItem(ctx, &humanworkv1.CompleteWorkItemRequest{IdempotencyKey: "k", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion), EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "e"}}})
			return err
		}, envelope.CodeInvalidArgument},
		{"complete: absent work item", func() error {
			_, err := srv.CompleteWorkItem(ctx, &humanworkv1.CompleteWorkItemRequest{IdempotencyKey: "k", WorkItemId: "00000000-0000-0000-0000-000000000000", ExpectedItemVersion: 1, OutputArtifactRef: "a", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "e"}}})
			return err
		}, envelope.CodeNotFound},
		{"complete: cross-tenant scope", func() error {
			_, err := srv.CompleteWorkItem(ctx, &humanworkv1.CompleteWorkItemRequest{
				IdempotencyKey: "k", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
				OutputArtifactRef: "a", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "e"}},
				Scope: &commonv1.ScopeContext{TenantId: "other-tenant"},
			})
			return err
		}, envelope.CodeNotFound},
		{"decide: missing proposal revision", func() error {
			_, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
				IdempotencyKey: "k", WorkItemId: approval.WorkItemID.String(), ExpectedItemVersion: uint64(approval.ItemVersion),
				Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "r",
			})
			return err
		}, envelope.CodeInvalidArgument},
		{"decide: missing reason ref", func() error {
			_, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
				IdempotencyKey: "k", WorkItemId: approval.WorkItemID.String(), ExpectedItemVersion: uint64(approval.ItemVersion),
				ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE,
			})
			return err
		}, envelope.CodeInvalidArgument},
		{"decide: unspecified decision is a zero value, never a decision", func() error {
			_, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
				IdempotencyKey: "k", WorkItemId: approval.WorkItemID.String(), ExpectedItemVersion: uint64(approval.ItemVersion),
				ProposalRevisionId: proposal, ReasonRef: "r",
			})
			return err
		}, envelope.CodeInvalidArgument},
	} {
		if err := tc.call(); !errors.As(err, &envErr) || envErr.Code() != tc.want {
			t.Fatalf("%s err = %v, want %v", tc.name, err, tc.want)
		}
	}

	// The stale-revision centerpiece: refuses with the current version
	// disclosed, and the completion port is never reached.
	stale := &humanworkv1.CompleteWorkItemRequest{
		IdempotencyKey: "stale-key", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion) + 41,
		OutputArtifactRef: "artifact:x/v1", EvidenceRefs: []*commonv1.EvidenceRef{{EvidenceId: "e"}},
	}
	_, err := srv.CompleteWorkItem(ctx, stale)
	if !errors.As(err, &envErr) || envErr.Code() != envelope.CodeFailedPrecondition {
		t.Fatalf("stale revision err = %v, want FAILED_PRECONDITION", err)
	}
	foundCurrent := false
	for _, v := range envErr.Violations() {
		if strings.Contains(v.Description, fmt.Sprint(item.ItemVersion)) {
			foundCurrent = true
		}
	}
	if !foundCurrent {
		t.Fatalf("stale revision violations = %+v, want the current version disclosed", envErr.Violations())
	}
	if store.completeCalls.Load() != 0 {
		t.Fatal("a stale-revision completion reached the completion port")
	}
}

// TestTodo_EP_WORK_003_Mutation proves the append-only half of "a
// duplicate/mutated decision must never complete": once a decision has
// recorded, a fresh (distinct-idempotency-key) attempt to decide the same
// item again -- even with the identical decision -- is refused as stale
// rather than appending a second decision, because the item's own version
// already moved. The recorded decision's digest is unchanged by the refused
// attempt.
func TestTodo_EP_WORK_003_Mutation(t *testing.T) {
	proposal := "sha256:" + strings.Repeat("3", 64)
	item := approvalWorkItem(proposal)
	store := newCompletionOps(newWorkStore(transporttest.Tenant, item))
	srv := &server{deps: completionDeps(store, nil)}
	ctx := humanworkContext(t, DecideApprovalProcedure)

	_, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
		IdempotencyKey: "first", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
		ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "reason.a/v1",
	})
	if err != nil {
		t.Fatalf("first DecideApproval: %v", err)
	}
	originalDigest := func() string {
		final, _ := store.LoadItem(context.Background(), transporttest.Tenant, item.WorkItemID.String())
		return final.CompletedOutputDigest
	}()

	// A second, distinct decision attempt against the same (now stale)
	// expected version is refused rather than appending a second decision.
	_, err = srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{
		IdempotencyKey: "second", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
		ProposalRevisionId: proposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_REJECT, ReasonRef: "reason.b/v1",
	})
	var envErr *envelope.Error
	if !errors.As(err, &envErr) || envErr.Code() != envelope.CodeFailedPrecondition {
		t.Fatalf("second decision err = %v, want FAILED_PRECONDITION", err)
	}
	final, _ := store.LoadItem(context.Background(), transporttest.Tenant, item.WorkItemID.String())
	if final.CompletedOutputDigest != originalDigest {
		t.Fatalf("the recorded decision's digest changed: %q -> %q", originalDigest, final.CompletedOutputDigest)
	}
	if final.Status != workitem.StatusCompleted {
		t.Fatalf("final status = %s, want COMPLETED (the first decision's own result, untouched)", final.Status)
	}
	completedRows := 0
	for _, tr := range store.transitions {
		if strings.Contains(tr, "->COMPLETED@") {
			completedRows++
		}
	}
	if completedRows != 1 {
		t.Fatalf("%d COMPLETED transitions recorded, want exactly 1 (append-only)", completedRows)
	}
}
