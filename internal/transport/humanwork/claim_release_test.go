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

	"github.com/google/uuid"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// workStore is EP-WORK-002's test double: it plays both [Reader] and
// [Claims] over one shared, mutex-guarded item set, so a claim or release
// this suite drives through the handler is immediately visible to the
// handler's own post-write LoadItem. Membership and current authority are
// decided by [workitem.MembershipOf] -- the real function every other
// EP-WORK-002 test and the production endpoint both use -- so this fake
// reimplements no authorization rule of its own; only the item_version
// compare-and-swap and the status transition are hand-mimicked, in the same
// shape [workitem.Store.Claim]/[workitem.Store.Release] perform against real
// Postgres, using the mutex as the row lock.
type workStore struct {
	mu           sync.Mutex
	tenant       string
	items        map[string]workitem.WorkItem
	transitions  []string
	claimCalls   atomic.Int64
	releaseCalls atomic.Int64
}

func newWorkStore(tenant string, seed ...workitem.WorkItem) *workStore {
	s := &workStore{tenant: tenant, items: map[string]workitem.WorkItem{}}
	for _, item := range seed {
		s.items[item.WorkItemID.String()] = item
	}
	return s
}

func (s *workStore) ListQueue(_ context.Context, tenant, principal string, now time.Time) ([]workitem.WorkItem, error) {
	if tenant != s.tenant {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []workitem.WorkItem
	for _, item := range s.items {
		if workitem.MembershipOf(item, principal, now) != workitem.MembershipNone {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *workStore) LoadItem(_ context.Context, tenant, workItemID string) (workitem.WorkItem, error) {
	if tenant != s.tenant {
		return workitem.WorkItem{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[workItemID]
	if !ok {
		return workitem.WorkItem{}, ErrNotFound
	}
	return item, nil
}

func (s *workStore) Claim(_ context.Context, _, workItemID, principal string, expectedVersion uint64, claimExpiresAt, now time.Time, _ workitem.TransitionMeta) (workitem.WorkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCalls.Add(1)
	item, ok := s.items[workItemID]
	if !ok {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeWorkItemNotFound, WorkItemID: workItemID}
	}
	if uint64(item.ItemVersion) != expectedVersion {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeAlreadyClaimed, WorkItemID: workItemID, Detail: "lost the claim race for this work item"}
	}
	switch workitem.MembershipOf(item, principal, now) {
	case workitem.MembershipAssignee, workitem.MembershipCandidate:
	default:
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeUnauthorizedClaimant, WorkItemID: workItemID}
	}
	if item.Status != workitem.StatusAssigned && item.Status != workitem.StatusAvailable {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeAlreadyClaimed, WorkItemID: workItemID, Detail: "already claimed by " + item.ClaimedBy}
	}
	claimID := uuid.New()
	claimedAt, expiry := now, claimExpiresAt
	item.Status = workitem.StatusClaimed
	item.ClaimID = &claimID
	item.ClaimedBy = principal
	item.ClaimedAt = &claimedAt
	item.ClaimExpiresAt = &expiry
	item.ItemVersion++
	s.items[workItemID] = item
	s.transitions = append(s.transitions, fmt.Sprintf("%s->CLAIMED@%d", workItemID, item.ItemVersion))
	return item, nil
}

func (s *workStore) Release(_ context.Context, _, workItemID, principal string, expectedVersion uint64, now time.Time, _ workitem.TransitionMeta) (workitem.WorkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseCalls.Add(1)
	item, ok := s.items[workItemID]
	if !ok {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeWorkItemNotFound, WorkItemID: workItemID}
	}
	if uint64(item.ItemVersion) != expectedVersion {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeStaleItem, WorkItemID: workItemID}
	}
	releaseTo := func() workitem.Status {
		if item.OwnerKind == workitem.OwnerPrincipal {
			return workitem.StatusAssigned
		}
		return workitem.StatusAvailable
	}
	if item.Status.Claimed() && item.ClaimExpired(now) {
		target := releaseTo()
		item.Status, item.ClaimID, item.ClaimedBy, item.ClaimedAt, item.ClaimExpiresAt = target, nil, "", nil, nil
		item.ItemVersion++
		s.items[workItemID] = item
		s.transitions = append(s.transitions, fmt.Sprintf("%s->%s@%d(expiry)", workItemID, target, item.ItemVersion))
		return item, &workitem.Error{Code: workitem.CodeClaimExpired, WorkItemID: workItemID}
	}
	if !item.Status.Claimed() {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeIllegalTransition, WorkItemID: workItemID}
	}
	if item.ClaimedBy != principal {
		return workitem.WorkItem{}, &workitem.Error{Code: workitem.CodeUnauthorizedClaimant, WorkItemID: workItemID}
	}
	target := releaseTo()
	item.Status, item.ClaimID, item.ClaimedBy, item.ClaimedAt, item.ClaimExpiresAt = target, nil, "", nil, nil
	item.ItemVersion++
	s.items[workItemID] = item
	s.transitions = append(s.transitions, fmt.Sprintf("%s->%s@%d", workItemID, target, item.ItemVersion))
	return item, nil
}

func claimDeps(store *workStore, extra map[string]bool) Dependencies {
	return Dependencies{
		Queue: store, Claims: store, Idempotency: endpoint.NewCoordinator(),
		Now: func() time.Time { return workNow },
		Authorize: func(_ context.Context, _ *trust.Principal, action string) bool {
			if extra[action] {
				return true
			}
			return action == ActionClaimWorkItem || action == ActionReleaseWorkItem
		},
	}
}

// TestWorkItemClaimReleaseEndpointsAreExclusiveVersionBoundAndIdempotent is
// the PRIMARY: a claim binds to the expected version and moves the item to
// CLAIMED; a stale-version replay of the identical idempotency key returns
// the very first outcome without a second write; and a release by the
// current claimant, bound to its own expected version, returns the item to
// its routed owner -- all through the wire types, not the domain package
// directly.
func TestWorkItemClaimReleaseEndpointsAreExclusiveVersionBoundAndIdempotent(t *testing.T) {
	item := queueItem(nil) // ASSIGNED to transporttest.Subject, OwnerKind PRINCIPAL
	store := newWorkStore(transporttest.Tenant, item)
	srv := &server{deps: claimDeps(store, nil)}
	ctx := humanworkContext(t, ClaimWorkItemProcedure)

	claimReq := &humanworkv1.ClaimWorkItemRequest{
		IdempotencyKey: "claim-key-1", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
	}
	res, err := srv.ClaimWorkItem(ctx, claimReq)
	if err != nil {
		t.Fatalf("ClaimWorkItem: %v", err)
	}
	got := res.GetWorkItem()
	if got.GetStatus() != humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_CLAIMED ||
		got.GetClaimedBy() != transporttest.Subject || got.GetItemVersion() != uint64(item.ItemVersion)+1 {
		t.Fatalf("claim result = %s", Explain(got))
	}
	if store.claimCalls.Load() != 1 {
		t.Fatalf("claim effect ran %d times, want 1", store.claimCalls.Load())
	}

	// Exact replay of the same idempotency key returns the identical result
	// without a second effect: the whole point of composing ENDPOINT-004's
	// Coordinator rather than forking a second idempotency mechanism.
	replay, err := srv.ClaimWorkItem(ctx, claimReq)
	if err != nil {
		t.Fatalf("ClaimWorkItem replay: %v", err)
	}
	if replay.GetWorkItem().GetItemVersion() != got.GetItemVersion() || replay.GetWorkItem().GetClaimedBy() != got.GetClaimedBy() {
		t.Fatalf("replay result diverged: %s vs %s", Explain(replay.GetWorkItem()), Explain(got))
	}
	if store.claimCalls.Load() != 1 {
		t.Fatalf("claim effect ran %d times after replay, want still 1", store.claimCalls.Load())
	}

	// The current claimant releases, bound to the version the claim left
	// behind; the item returns to ASSIGNED (a PRINCIPAL-owned item) and the
	// version advances again.
	releaseReq := &humanworkv1.ReleaseWorkItemRequest{
		IdempotencyKey: "release-key-1", WorkItemId: item.WorkItemID.String(),
		ExpectedItemVersion: got.GetItemVersion(), ReasonRef: "operator requested",
	}
	relRes, err := srv.ReleaseWorkItem(ctx, releaseReq)
	if err != nil {
		t.Fatalf("ReleaseWorkItem: %v", err)
	}
	relItem := relRes.GetWorkItem()
	if relItem.GetStatus() != humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_ASSIGNED ||
		relItem.GetClaimedBy() != "" || relItem.GetItemVersion() != got.GetItemVersion()+1 {
		t.Fatalf("release result = %s", Explain(relItem))
	}
	if store.releaseCalls.Load() != 1 {
		t.Fatalf("release effect ran %d times, want 1", store.releaseCalls.Load())
	}
}

// TestTodo_EP_WORK_002_Race drives two authorized candidates at the same
// AVAILABLE item concurrently, through real goroutines, each with its own
// idempotency key (a genuinely distinct logical request, not a replay).
// Exactly one may win; every loser must be refused safely -- not panic, not
// a second silent claim -- and the stored item must show exactly one CLAIMED
// transition.
//
// A loser can be refused at either of two layers, both safe and both
// legitimate: ENDPOINT-004's Coordinator refuses FAILED_PRECONDITION when
// its own precondition check (run against whatever version this racer's
// prepareMutation happened to observe before the race decided a winner)
// already disagrees with the expected version, without ever reaching the
// claim port; or [workitem.Store.Claim]'s own item_version compare-and-swap
// refuses ABORTED when the precondition happened to still agree but the
// exclusive write itself lost. Which racers land on which side is a
// scheduling accident, not a contract -- what the contract fixes is that
// exactly one wins and every loser's refusal is one of these two safe,
// typed conflicts.
func TestTodo_EP_WORK_002_Race(t *testing.T) {
	item := queueItem(candidateItem(transporttest.Subject, "user-bob"))
	store := newWorkStore(transporttest.Tenant, item)
	srv := &server{deps: claimDeps(store, nil)}
	ctxAmy := humanworkContext(t, ClaimWorkItemProcedure)
	ctxBob := humanworkContext(t, ClaimWorkItemProcedure, func(c *trust.Claims) { c.Subject = "user-bob" })

	const racers = 8
	type result struct {
		err     error
		version uint64
	}
	results := make([]result, racers)
	var wg sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			ctx, key := ctxAmy, fmt.Sprintf("race-amy-%d", i)
			if i%2 == 1 {
				ctx, key = ctxBob, fmt.Sprintf("race-bob-%d", i)
			}
			res, err := srv.ClaimWorkItem(ctx, &humanworkv1.ClaimWorkItemRequest{
				IdempotencyKey: key, WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
			})
			if err != nil {
				results[i] = result{err: err}
				return
			}
			results[i] = result{version: res.GetWorkItem().GetItemVersion()}
		}(i)
	}
	start.Done()
	wg.Wait()

	wins := 0
	for i, r := range results {
		if r.err == nil {
			wins++
			if r.version != uint64(item.ItemVersion)+1 {
				t.Errorf("winner %d claimed at version %d, want %d", i, r.version, item.ItemVersion+1)
			}
			continue
		}
		// A loser receives safe, typed conflict data -- FAILED_PRECONDITION
		// (a stale-revision precondition) or ABORTED (lost the exclusive
		// write) -- naming neither the winner's identity nor any internal
		// detail, never a panic and never a second successful claim.
		envErr := &envelope.Error{}
		if !errors.As(r.err, &envErr) ||
			(envErr.Code() != envelope.CodeAborted && envErr.Code() != envelope.CodeFailedPrecondition) {
			t.Errorf("loser %d err = %v, want ABORTED or FAILED_PRECONDITION", i, r.err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d of %d racers won the claim; exactly one may", wins, racers)
	}
	if calls := store.claimCalls.Load(); calls < 1 || calls > int64(racers) {
		t.Fatalf("claim effect ran %d times, want between 1 and %d", calls, racers)
	}
	claimedRows := 0
	for _, tr := range store.transitions {
		if strings.Contains(tr, "->CLAIMED@") {
			claimedRows++
		}
	}
	if claimedRows != 1 {
		t.Fatalf("%d CLAIMED transitions recorded, want exactly 1", claimedRows)
	}
}

// TestTodo_EP_WORK_002_Security proves current authority, not a
// caller-asserted or stale one: an unauthenticated caller and one denied the
// wire-level capability are refused before the store is ever reached; a
// caller with no current standing on a visible item is refused
// PERMISSION_DENIED rather than silently admitted or told the item does not
// exist (which would leak nothing extra, but would also hide a genuine
// authorization defect from an operator reading the code); and a stranger to
// a restricted-visibility item gets the same non-disclosing NOT_FOUND
// GetWorkItem would give them.
func TestTodo_EP_WORK_002_Security(t *testing.T) {
	item := queueItem(nil) // VisibilityAssigneeOnly, ASSIGNED to transporttest.Subject
	store := newWorkStore(transporttest.Tenant, item)

	// Unauthenticated: refused before the store is touched at all.
	srv := &server{deps: claimDeps(store, nil)}
	if _, err := srv.ClaimWorkItem(context.Background(), &humanworkv1.ClaimWorkItemRequest{
		IdempotencyKey: "k", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
	}); err == nil {
		t.Fatal("unauthenticated ClaimWorkItem succeeded")
	}
	if store.claimCalls.Load() != 0 {
		t.Fatal("unauthenticated call reached the claim port")
	}

	// Denied the wire-level capability outright.
	denyAll := Dependencies{Queue: store, Claims: store, Idempotency: endpoint.NewCoordinator(),
		Now: func() time.Time { return workNow }, Authorize: func(context.Context, *trust.Principal, string) bool { return false }}
	ctx := humanworkContext(t, ClaimWorkItemProcedure)
	_, err := (&server{deps: denyAll}).ClaimWorkItem(ctx, &humanworkv1.ClaimWorkItemRequest{
		IdempotencyKey: "k", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
	})
	var envErr *envelope.Error
	if !errors.As(err, &envErr) || envErr.Code() != envelope.CodePermissionDenied {
		t.Fatalf("unauthorized ClaimWorkItem err = %v, want PERMISSION_DENIED", err)
	}

	// A stranger to a VisibilityAssigneeOnly item cannot even see it exists:
	// identical to GetWorkItem's own NOT_FOUND.
	strangerCtx := humanworkContext(t, ClaimWorkItemProcedure, func(c *trust.Claims) { c.Subject = "user-stranger" })
	srv2 := &server{deps: claimDeps(store, nil)}
	_, err = srv2.ClaimWorkItem(strangerCtx, &humanworkv1.ClaimWorkItemRequest{
		IdempotencyKey: "k2", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
	})
	if !errors.As(err, &envErr) || envErr.Code() != envelope.CodeNotFound {
		t.Fatalf("stranger ClaimWorkItem err = %v, want NOT_FOUND", err)
	}

	// An org-scope-visible item (so the caller can see it exists, matching
	// PermittedActions offering nothing) but with no current standing --
	// neither the assignee nor an authorized candidate -- is refused
	// PERMISSION_DENIED by the domain's current-authority check, not
	// silently admitted because the caller could read the item.
	orgItem := queueItem(func(w *workitem.WorkItem) {
		w.Visibility = workitem.VisibilityOrganizationScope
	})
	orgStore := newWorkStore(transporttest.Tenant, orgItem)
	srv3 := &server{deps: claimDeps(orgStore, nil)}
	bystanderCtx := humanworkContext(t, ClaimWorkItemProcedure, func(c *trust.Claims) { c.Subject = "user-bystander" })
	_, err = srv3.ClaimWorkItem(bystanderCtx, &humanworkv1.ClaimWorkItemRequest{
		IdempotencyKey: "k3", WorkItemId: orgItem.WorkItemID.String(), ExpectedItemVersion: uint64(orgItem.ItemVersion),
	})
	if !errors.As(err, &envErr) || envErr.Code() != envelope.CodePermissionDenied {
		t.Fatalf("no-standing ClaimWorkItem err = %v, want PERMISSION_DENIED", err)
	}

	// A stale delegation is refused the same way, even though it once would
	// have authorized this principal: MembershipOf is re-evaluated against
	// Now, not against a caller-asserted or previously valid grant.
	staleCandidate := queueItem(func(w *workitem.WorkItem) {
		w.Status, w.OwnerKind, w.Visibility = workitem.StatusAvailable, workitem.OwnerCandidateSet, workitem.VisibilityOrganizationScope
		w.Assignment = workitem.Assignment{}
	})
	staleStore := newWorkStore(transporttest.Tenant, staleCandidate)
	srv4 := &server{deps: claimDeps(staleStore, nil)}
	staleCtx := humanworkContext(t, ClaimWorkItemProcedure, func(c *trust.Claims) { c.Subject = "user-stale-delegate" })
	_, err = srv4.ClaimWorkItem(staleCtx, &humanworkv1.ClaimWorkItemRequest{
		IdempotencyKey: "k4", WorkItemId: staleCandidate.WorkItemID.String(), ExpectedItemVersion: uint64(staleCandidate.ItemVersion),
	})
	if !errors.As(err, &envErr) || envErr.Code() != envelope.CodePermissionDenied {
		t.Fatalf("empty-candidate-set ClaimWorkItem err = %v, want PERMISSION_DENIED", err)
	}
}

// TestTodo_EP_WORK_002_Conformance covers malformed input, the stale
// expected-version conformance clause and cross-tenant scoping.
func TestTodo_EP_WORK_002_Conformance(t *testing.T) {
	item := queueItem(nil)
	store := newWorkStore(transporttest.Tenant, item)
	srv := &server{deps: claimDeps(store, nil)}
	ctx := humanworkContext(t, ClaimWorkItemProcedure)
	envErr := &envelope.Error{}

	for _, tc := range []struct {
		name string
		req  *humanworkv1.ClaimWorkItemRequest
		want envelope.Code
	}{
		{"empty work item id", &humanworkv1.ClaimWorkItemRequest{IdempotencyKey: "k", ExpectedItemVersion: 1}, envelope.CodeInvalidArgument},
		{"missing idempotency key", &humanworkv1.ClaimWorkItemRequest{WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: 1}, envelope.CodeInvalidArgument},
		{"zero expected version", &humanworkv1.ClaimWorkItemRequest{IdempotencyKey: "k", WorkItemId: item.WorkItemID.String()}, envelope.CodeInvalidArgument},
		{"absent work item", &humanworkv1.ClaimWorkItemRequest{IdempotencyKey: "k", WorkItemId: uuid.NewString(), ExpectedItemVersion: 1}, envelope.CodeNotFound},
		{"cross-tenant scope", &humanworkv1.ClaimWorkItemRequest{
			IdempotencyKey: "k", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
			Scope: &commonv1.ScopeContext{TenantId: "other-tenant"},
		}, envelope.CodeNotFound},
	} {
		if _, err := srv.ClaimWorkItem(ctx, tc.req); !errors.As(err, &envErr) || envErr.Code() != tc.want {
			t.Fatalf("%s err = %v, want %v", tc.name, err, tc.want)
		}
	}

	// CONFORMANCE's centerpiece: a stale expected revision refuses with the
	// current-safe precondition data (the item's real current version)
	// rather than stealing or releasing the claim -- and the claim port is
	// never even reached.
	stale := &humanworkv1.ClaimWorkItemRequest{
		IdempotencyKey: "stale-key", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion) + 41,
	}
	_, err := srv.ClaimWorkItem(ctx, stale)
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
	if store.claimCalls.Load() != 0 {
		t.Fatal("a stale-revision claim reached the claim port")
	}
	final, _ := store.LoadItem(context.Background(), transporttest.Tenant, item.WorkItemID.String())
	if final.Status != workitem.StatusAssigned || final.ClaimedBy != "" {
		t.Fatalf("a refused stale-revision claim mutated the item: %s claimedBy=%q", final.Status, final.ClaimedBy)
	}
}

// TestTodo_EP_WORK_002_Fault covers the two FAULT clauses: an expired lease
// must not let a release complete as if current, and an ambiguous
// (here: retried) request must not append duplicate assignment history.
func TestTodo_EP_WORK_002_Fault(t *testing.T) {
	t.Run("an expired lease is not released as if current", func(t *testing.T) {
		claimed := queueItem(claimedItem(workitem.KindTask))
		// The claim's expiry is workNow+2h in claimedItem; move Now past it.
		store := newWorkStore(transporttest.Tenant, claimed)
		srv := &server{deps: Dependencies{
			Queue: store, Claims: store, Idempotency: endpoint.NewCoordinator(),
			Now:       func() time.Time { return workNow.Add(3 * time.Hour) },
			Authorize: func(context.Context, *trust.Principal, string) bool { return true },
		}}
		ctx := humanworkContext(t, ReleaseWorkItemProcedure)
		_, err := srv.ReleaseWorkItem(ctx, &humanworkv1.ReleaseWorkItemRequest{
			IdempotencyKey: "release-expired", WorkItemId: claimed.WorkItemID.String(), ExpectedItemVersion: uint64(claimed.ItemVersion),
		})
		var envErr *envelope.Error
		if !errors.As(err, &envErr) || envErr.Code() != envelope.CodeFailedPrecondition {
			t.Fatalf("release-of-expired-lease err = %v, want FAILED_PRECONDITION", err)
		}
		final, loadErr := store.LoadItem(context.Background(), transporttest.Tenant, claimed.WorkItemID.String())
		if loadErr != nil {
			t.Fatalf("LoadItem: %v", loadErr)
		}
		// The item was released as an expiry, not as transporttest.Subject's
		// own completed release -- the call itself still refused.
		if final.Status != workitem.StatusAssigned || final.ClaimedBy != "" {
			t.Fatalf("post-expiry item = %s claimedBy=%q, want ASSIGNED with no claim", final.Status, final.ClaimedBy)
		}
	})

	t.Run("a retried request never appends duplicate assignment history", func(t *testing.T) {
		item := queueItem(nil)
		store := newWorkStore(transporttest.Tenant, item)
		srv := &server{deps: claimDeps(store, nil)}
		ctx := humanworkContext(t, ClaimWorkItemProcedure)
		req := &humanworkv1.ClaimWorkItemRequest{
			IdempotencyKey: "ambiguous-retry", WorkItemId: item.WorkItemID.String(), ExpectedItemVersion: uint64(item.ItemVersion),
		}
		first, err := srv.ClaimWorkItem(ctx, req)
		if err != nil {
			t.Fatalf("first ClaimWorkItem: %v", err)
		}
		// Simulate the client never having observed the first response (a
		// dropped connection, an ambiguous timeout) and retrying identically.
		for i := 0; i < 4; i++ {
			retry, err := srv.ClaimWorkItem(ctx, req)
			if err != nil {
				t.Fatalf("retry %d: %v", i, err)
			}
			if retry.GetWorkItem().GetItemVersion() != first.GetWorkItem().GetItemVersion() {
				t.Fatalf("retry %d version = %d, want the original %d", i, retry.GetWorkItem().GetItemVersion(), first.GetWorkItem().GetItemVersion())
			}
		}
		if store.claimCalls.Load() != 1 {
			t.Fatalf("claim effect ran %d times across 5 identical requests, want 1", store.claimCalls.Load())
		}
		claimedRows := 0
		for _, tr := range store.transitions {
			if strings.Contains(tr, "->CLAIMED@") {
				claimedRows++
			}
		}
		if claimedRows != 1 {
			t.Fatalf("%d CLAIMED transitions recorded across retries, want exactly 1", claimedRows)
		}
	})
}

// TestTodo_EP_WORK_002_Integration proves the connect/gRPC path (the direct
// method call [Register] wires up) and the HTTP edge path ([NewHandler])
// agree on the outcome for equivalent requests: the same claim, made through
// each transport against its own equivalent fixture, produces the same
// resulting status, claimant and version delta.
func TestTodo_EP_WORK_002_Integration(t *testing.T) {
	grpcItem := queueItem(nil)
	grpcStore := newWorkStore(transporttest.Tenant, grpcItem)
	grpcSrv := &server{deps: claimDeps(grpcStore, nil)}
	grpcCtx := humanworkContext(t, ClaimWorkItemProcedure)

	grpcRes, err := grpcSrv.ClaimWorkItem(grpcCtx, &humanworkv1.ClaimWorkItemRequest{
		IdempotencyKey: "integration-grpc", WorkItemId: grpcItem.WorkItemID.String(), ExpectedItemVersion: uint64(grpcItem.ItemVersion),
	})
	if err != nil {
		t.Fatalf("direct (gRPC-shaped) ClaimWorkItem: %v", err)
	}

	httpItem := queueItem(nil)
	httpStore := newWorkStore(transporttest.Tenant, httpItem)
	handler := NewHandler(claimDeps(httpStore, nil))
	// The HTTP edge's admission interceptor is not part of this handler in
	// isolation (it is composed at the server-assembly layer, same as
	// TestWorkItemHandlerAnswersBothProcedures already establishes for the
	// read methods); the trusted invocation is supplied the same way here.
	mux, ok := handler.(*http.ServeMux)
	if !ok {
		t.Fatalf("NewHandler returned %T, want *http.ServeMux", handler)
	}
	httpSrv := httptest.NewServer(withInvocation(mux, grpcCtx))
	t.Cleanup(httpSrv.Close)
	body := fmt.Sprintf(`{"idempotencyKey":"integration-http","workItemId":"%s","expectedItemVersion":"%d"}`,
		httpItem.WorkItemID.String(), httpItem.ItemVersion)
	resp, err := httpSrv.Client().Post(httpSrv.URL+ClaimWorkItemProcedure, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST ClaimWorkItem: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST ClaimWorkItem status = %d, want 200", resp.StatusCode)
	}

	grpcFinal := grpcRes.GetWorkItem()
	httpFinal, loadErr := httpStore.LoadItem(context.Background(), transporttest.Tenant, httpItem.WorkItemID.String())
	if loadErr != nil {
		t.Fatalf("LoadItem after HTTP claim: %v", loadErr)
	}
	if grpcFinal.GetStatus().String() != "WORK_ITEM_STATUS_"+string(httpFinal.Status) {
		t.Fatalf("gRPC status %s vs HTTP status %s diverged", grpcFinal.GetStatus(), httpFinal.Status)
	}
	if grpcFinal.GetClaimedBy() != httpFinal.ClaimedBy {
		t.Fatalf("gRPC claimant %q vs HTTP claimant %q diverged", grpcFinal.GetClaimedBy(), httpFinal.ClaimedBy)
	}
	if grpcFinal.GetItemVersion() != uint64(httpFinal.ItemVersion) {
		t.Fatalf("gRPC version %d vs HTTP version %d diverged", grpcFinal.GetItemVersion(), httpFinal.ItemVersion)
	}
}

// withInvocation wraps h so every request carries ctx's trusted invocation
// and principal directly, standing in for the admission interceptor a real
// server assembly composes in front of this handler.
func withInvocation(h http.Handler, ctx context.Context) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
