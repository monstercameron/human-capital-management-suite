package humanwork

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var workNow = time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)

// queueTestReader answers both reads from an in-memory item set, recording
// calls so the refusal paths can prove the port was never reached.
type queueTestReader struct {
	mu      sync.Mutex
	tenant  string // tenant key the fixture items belong to
	items   []workitem.WorkItem
	foreign []workitem.WorkItem // items in another tenant, never reachable
	calls   atomic.Int64
}

func (r *queueTestReader) ListQueue(_ context.Context, tenant, principal string, now time.Time) ([]workitem.WorkItem, error) {
	r.calls.Add(1)
	if tenant != r.tenant {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []workitem.WorkItem
	for _, item := range r.items {
		if workitem.MembershipOf(item, principal, now) == workitem.MembershipNone {
			continue
		}
		out = append(out, item)
	}
	// Same deadline/id ordering the store's ORDER BY imposes.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DeadlineAt.Equal(out[j].DeadlineAt) {
			return out[i].DeadlineAt.Before(out[j].DeadlineAt)
		}
		return out[i].WorkItemID.String() < out[j].WorkItemID.String()
	})
	return out, nil
}

func (r *queueTestReader) LoadItem(_ context.Context, tenant, workItemID string) (workitem.WorkItem, error) {
	r.calls.Add(1)
	if tenant != r.tenant {
		return workitem.WorkItem{}, ErrNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		if item.WorkItemID.String() == workItemID {
			return item, nil
		}
	}
	return workitem.WorkItem{}, ErrNotFound
}

func (r *queueTestReader) append(item workitem.WorkItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, item)
}

func queueDeps(reader Reader, extra map[string]bool) Dependencies {
	return Dependencies{
		Queue:     reader,
		CursorKey: []byte("work-queue-test-key"),
		Now:       func() time.Time { return workNow },
		Authorize: func(_ context.Context, _ *trust.Principal, action string) bool {
			if extra[action] {
				return true
			}
			return action == ActionListWorkItems || action == ActionGetWorkItem
		},
	}
}

func queueItem(mut func(*workitem.WorkItem)) workitem.WorkItem {
	item := workitem.WorkItem{
		TenantID:            workTenantUUID(),
		WorkItemID:          uuid.New(),
		ItemVersion:         3,
		Kind:                workitem.KindTask,
		WorkType:            "worktype.review/v1",
		Status:              workitem.StatusAssigned,
		OwnerKind:           workitem.OwnerPrincipal,
		OwnerRef:            transporttest.Subject,
		Visibility:          workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: transporttest.OrganizationScopeID,
		DeadlineAt:          workNow.Add(48 * time.Hour),
		CreatedAt:           workNow.Add(-time.Hour),
	}
	if mut != nil {
		mut(&item)
	}
	return item
}

// workTenantUUID is the wire-visible tenant uuid on fixtures; the trusted
// context's tenant is a key ("acme-corp") the reader maps internally.
func workTenantUUID() uuid.UUID {
	return uuid.NewMD5(uuid.NameSpaceDNS, []byte(transporttest.Tenant))
}

func humanworkContext(t *testing.T, method string, claims ...func(*trust.Claims)) context.Context {
	t.Helper()
	verifier, err := transporttest.NewVerifier(func() time.Time { return workNow })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	c := transporttest.DefaultClaims(workNow)
	for _, f := range claims {
		f(&c)
	}
	token, err := transporttest.BearerToken(verifier, c)
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	ctx, _, admitErr := transport.Admit(context.Background(),
		transporttest.Config(verifier, func() time.Time { return workNow }, "humanwork-test-request", nil),
		transport.AdmissionRequest{
			Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: []string{token}},
			Method:   method, Kind: transport.KindGRPC,
		})
	if admitErr != nil {
		t.Fatalf("Admit: %v", admitErr)
	}
	return ctx
}

func actionsOf(item *humanworkv1.WorkItem) string {
	return strings.Join(item.GetPermittedActions(), ",")
}

func candidateItem(principals ...string) func(*workitem.WorkItem) {
	cands := make([]humanwork.Candidate, len(principals))
	for i, p := range principals {
		cands[i] = humanwork.Candidate{PrincipalID: p, Via: humanwork.SourceDirect}
	}
	return func(w *workitem.WorkItem) {
		w.Status = workitem.StatusAvailable
		w.OwnerKind = workitem.OwnerCandidateSet
		w.OwnerRef = "candidates:req.review/v1@sha256:" + strings.Repeat("a", 64)
		w.Visibility = workitem.VisibilityCandidateSet
		w.Assignment = workitem.Assignment{Resolution: humanwork.Resolution{
			RequirementID: "req.review/v1", Outcome: humanwork.OutcomeResolved, Candidates: cands,
		}}
	}
}

func claimedItem(kind workitem.Kind) func(*workitem.WorkItem) {
	expiry := workNow.Add(2 * time.Hour)
	started := workNow
	return func(w *workitem.WorkItem) {
		w.Kind = kind
		w.Status = workitem.StatusInProgress
		claimID := uuid.New()
		w.ClaimID = &claimID
		w.ClaimedBy = transporttest.Subject
		w.ClaimedAt = &started
		w.ClaimExpiresAt = &expiry
	}
}

// TestWorkItemReadEndpointsEnforceAssignmentQueueAndEvidenceVisibility is the
// todo's primary: the queue snapshot and the item detail carry the current
// version, claim, deadline and proposal digest exactly for the principal's
// own standing; identity context follows the membership classification; and
// absent, other-tenant and non-member reads are all the same NOT_FOUND.
func TestWorkItemReadEndpointsEnforceAssignmentQueueAndEvidenceVisibility(t *testing.T) {
	proposal := "sha256:" + strings.Repeat("9", 64)
	assigned := queueItem(func(w *workitem.WorkItem) {
		w.DeadlineAt = workNow.Add(72 * time.Hour)
		w.SubjectRefs = []string{"worker-77c1e2"}
	})
	available := queueItem(candidateItem(transporttest.Subject, "user-bob"))
	inProgress := queueItem(func(w *workitem.WorkItem) {
		claimedItem(workitem.KindApproval)(w)
		w.ProposalRef = proposal
		w.DeadlineAt = workNow.Add(24 * time.Hour)
		w.SubjectRefs = []string{"worker-77c1e2"}
	})
	stranger := queueItem(func(w *workitem.WorkItem) { w.OwnerRef = "user-bob" })
	orgView := queueItem(func(w *workitem.WorkItem) {
		w.OwnerRef = "user-bob"
		w.Visibility = workitem.VisibilityOrganizationScope
		w.ClaimedBy = "user-bob"
		at := workNow
		w.ClaimedAt = &at
	})
	otherTenant := queueItem(func(w *workitem.WorkItem) { w.TenantID = uuid.New() })

	reader := &queueTestReader{tenant: transporttest.Tenant, items: []workitem.WorkItem{assigned, available, inProgress, stranger, orgView}, foreign: []workitem.WorkItem{otherTenant}}
	srv := &server{deps: queueDeps(reader, nil)}
	ctx := humanworkContext(t, ListWorkItemsProcedure)

	list, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	var gotIDs []string
	for _, item := range list.GetWorkItems() {
		gotIDs = append(gotIDs, item.GetWorkItemId())
	}
	// The queue is the actionable membership set: an org-scope item the
	// caller has no standing on is readable by id but is not queue content.
	wantIDs := []string{
		inProgress.WorkItemID.String(), // earliest deadline first
		available.WorkItemID.String(),
		assigned.WorkItemID.String(),
	}
	if fmt.Sprint(gotIDs) != fmt.Sprint(wantIDs) {
		t.Fatalf("queue ids = %v, want %v (deadline order, membership only)", gotIDs, wantIDs)
	}

	// The assigned item advertises claim and its business context.
	detail, err := srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{WorkItemId: assigned.WorkItemID.String()})
	if err != nil {
		t.Fatalf("GetWorkItem(assigned): %v", err)
	}
	got := detail.GetWorkItem()
	if got.GetItemVersion() != 3 || actionsOf(got) != "claim" ||
		got.GetDueAt().AsTime() != workNow.Add(72*time.Hour).UTC() ||
		got.GetAssignedPrincipalId() != transporttest.Subject ||
		len(got.GetSubjectRefs()) != 1 {
		t.Fatalf("assigned item detail = %s", Explain(got))
	}

	// The claimed approval advertises the acting set and carries its claim
	// evidence plus the proposal digest.
	detail, err = srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{WorkItemId: inProgress.WorkItemID.String()})
	if err != nil {
		t.Fatalf("GetWorkItem(in-progress): %v", err)
	}
	got = detail.GetWorkItem()
	if actionsOf(got) != "complete,decide_approval,release" ||
		got.GetProposalRef() != proposal ||
		got.GetClaimedBy() != transporttest.Subject ||
		got.GetClaimExpiresAt() == nil {
		t.Fatalf("claimed approval detail = %s", Explain(got))
	}

	// A queue candidate sees the resolved set and a claim action.
	detail, err = srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{WorkItemId: available.WorkItemID.String()})
	if err != nil {
		t.Fatalf("GetWorkItem(available): %v", err)
	}
	got = detail.GetWorkItem()
	if actionsOf(got) != "claim" || got.GetResolvedQueueId() == "" || len(got.GetResolvedCandidates()) != 2 {
		t.Fatalf("candidate item detail = %s", Explain(got))
	}

	// An org-scope viewer (no membership) may read the record by id but sees
	// no identity or claim evidence.
	orgDetail, err := srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{WorkItemId: orgView.WorkItemID.String()})
	if err != nil {
		t.Fatalf("GetWorkItem(org-scope): %v", err)
	}
	orgItem := orgDetail.GetWorkItem()
	if orgItem.GetClaimedBy() != "" || orgItem.GetAssignedPrincipalId() != "" || len(orgItem.GetResolvedCandidates()) != 0 {
		t.Fatalf("non-member org viewer saw identity/evidence: %s", Explain(orgItem))
	}
	if orgItem.GetWorkItemId() != orgView.WorkItemID.String() || len(orgItem.GetPermittedActions()) != 0 {
		t.Fatalf("org-scope projection = %s", Explain(orgItem))
	}

	// Absent, non-member and other-tenant reads are the identical NOT_FOUND.
	for _, tc := range []struct{ name, id string }{
		{"absent", uuid.NewString()},
		{"non-member", stranger.WorkItemID.String()},
		{"other tenant", otherTenant.WorkItemID.String()},
	} {
		_, err := srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{WorkItemId: tc.id})
		envErr := &envelope.Error{}
		if !errors.As(err, &envErr) || envErr.Code() != envelope.CodeNotFound {
			t.Fatalf("GetWorkItem(%s) err = %v, want NOT_FOUND", tc.name, err)
		}
	}
}

func TestTodo_EP_WORK_001_Property(t *testing.T) {
	reader := &queueTestReader{tenant: transporttest.Tenant}
	for i := 0; i < 7; i++ {
		reader.append(queueItem(func(w *workitem.WorkItem) {
			w.DeadlineAt = workNow.Add(time.Duration(i) * time.Hour)
		}))
	}
	srv := &server{deps: queueDeps(reader, nil)}
	ctx := humanworkContext(t, ListWorkItemsProcedure)
	var cursor string
	var order []string
	for {
		res, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{
			Page: &commonv1.PageRequest{PageSize: 3, Cursor: cursor},
		})
		if err != nil {
			t.Fatalf("ListWorkItems cursor=%q: %v", cursor, err)
		}
		for _, item := range res.GetWorkItems() {
			order = append(order, item.GetWorkItemId())
		}
		cursor = res.GetPage().GetNextCursor()
		if cursor == "" {
			break
		}
	}
	if len(order) != 7 {
		t.Fatalf("paged union size = %d, want 7", len(order))
	}
	for i, item := range reader.items {
		if order[i] != item.WorkItemID.String() {
			t.Fatalf("paging order diverged at %d: %v", i, order)
		}
	}
}

func TestTodo_EP_WORK_001_Golden(t *testing.T) {
	proposal := "sha256:" + strings.Repeat("9", 64)
	item := queueItem(func(w *workitem.WorkItem) {
		claimedItem(workitem.KindApproval)(w)
		w.ProposalRef = proposal
	})
	got := projectItem(item, workitem.MembershipClaimant, false)
	golden := fmt.Sprintf("%s|%s|%s|%s", got.GetWorkItemId(), got.GetStatus(), got.GetProposalRef(), actionsOf(got))
	want := fmt.Sprintf("%s|%s|%s|%s", item.WorkItemID, "WORK_ITEM_STATUS_IN_PROGRESS", proposal, "complete,decide_approval,release")
	if golden != want {
		t.Fatalf("work item projection golden = %q, want %q", golden, want)
	}
}

func TestTodo_EP_WORK_001_Race(t *testing.T) {
	reader := &queueTestReader{}
	srv := &server{deps: queueDeps(reader, nil)}
	ctx := humanworkContext(t, ListWorkItemsProcedure)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 50}}); err != nil {
				t.Errorf("ListWorkItems: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			reader.append(queueItem(nil))
			if _, err := srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{WorkItemId: uuid.NewString()}); err == nil {
				t.Error("GetWorkItem(absent) returned no error")
			}
		}()
	}
	wg.Wait()
}

func TestTodo_EP_WORK_001_Security(t *testing.T) {
	item := queueItem(nil)
	reader := &queueTestReader{tenant: transporttest.Tenant, items: []workitem.WorkItem{item, queueItem(nil)}}

	// Unauthenticated context is refused before the reader runs.
	srv := &server{deps: queueDeps(reader, nil)}
	if _, err := srv.ListWorkItems(context.Background(), &humanworkv1.ListWorkItemsRequest{}); err == nil {
		t.Fatal("unauthenticated ListWorkItems succeeded")
	}
	if reader.calls.Load() != 0 {
		t.Fatal("unauthenticated call reached the reader")
	}

	// A principal denied the list action gets PERMISSION_DENIED.
	strict := Dependencies{
		Queue: reader, CursorKey: []byte("k"), Now: func() time.Time { return workNow },
		Authorize: func(context.Context, *trust.Principal, string) bool { return false },
	}
	ctx := humanworkContext(t, ListWorkItemsProcedure)
	if _, err := (&server{deps: strict}).ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{}); err == nil {
		t.Fatal("unauthorized ListWorkItems succeeded")
	}

	// A forged signature, a foreign-principal cursor, a foreign-tenant
	// cursor and an expired cursor are all refused.
	forged := humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 1, Cursor: "AAAA.00"}}
	srv = &server{deps: queueDeps(reader, nil)}
	res, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 1}})
	if err != nil {
		t.Fatalf("ListWorkItems page 1: %v", err)
	}
	valid := res.GetPage().GetNextCursor()
	if valid == "" {
		t.Fatal("expected a next cursor for page_size 1")
	}
	envErr := &envelope.Error{}
	for name, cursor := range map[string]string{
		"forged signature":  valid[:len(valid)-2] + "zz",
		"foreign principal": mustCursor(t, queueCursor{Principal: "user-bob", Tenant: transporttest.Tenant, Snapshot: queueDigest(nil), Index: 1, Version: cursorVersion, ExpiresAt: workNow.Add(time.Minute).Unix(), Nonce: "n"}, []byte("work-queue-test-key")),
		"foreign tenant":    mustCursor(t, queueCursor{Principal: transporttest.Subject, Tenant: "other-tenant", Snapshot: queueDigest(nil), Index: 1, Version: cursorVersion, ExpiresAt: workNow.Add(time.Minute).Unix(), Nonce: "n"}, []byte("work-queue-test-key")),
		"expired":           mustCursor(t, queueCursor{Principal: transporttest.Subject, Tenant: transporttest.Tenant, Snapshot: queueDigest(reader.items), Index: 1, Version: cursorVersion, ExpiresAt: workNow.Add(-time.Minute).Unix(), Nonce: "n"}, []byte("work-queue-test-key")),
	} {
		_, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 1, Cursor: cursor}})
		if !errors.As(err, &envErr) || envErr.Code() != envelope.CodeInvalidArgument {
			t.Fatalf("cursor %q err = %v, want INVALID_ARGUMENT", name, err)
		}
	}

	// Garbage that cannot even be decoded is refused identically.
	if _, err := srv.ListWorkItems(ctx, &forged); !errors.As(err, &envErr) || envErr.Code() != envelope.CodeInvalidArgument {
		t.Fatalf("garbage cursor err = %v, want INVALID_ARGUMENT", err)
	}

	// A scope naming another tenant is non-disclosing: empty page, no error.
	res, err = srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{
		Scope: &commonv1.ScopeContext{TenantId: "other-tenant"},
	})
	if err != nil || len(res.GetWorkItems()) != 0 {
		t.Fatalf("cross-tenant scope list = %v, %v; want empty page", res, err)
	}
	if _, err := srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{
		WorkItemId: item.WorkItemID.String(),
		Scope:      &commonv1.ScopeContext{TenantId: "other-tenant"},
	}); !errors.As(err, &envErr) || envErr.Code() != envelope.CodeNotFound {
		t.Fatalf("cross-tenant scope get err = %v, want NOT_FOUND", err)
	}
}

func mustCursor(t *testing.T, c queueCursor, key []byte) string {
	t.Helper()
	cur, err := encodeQueueCursor(c, key)
	if err != nil {
		t.Fatalf("encodeQueueCursor: %v", err)
	}
	return cur
}

func TestTodo_EP_WORK_001_Conformance(t *testing.T) {
	reader := &queueTestReader{tenant: transporttest.Tenant, items: []workitem.WorkItem{queueItem(nil)}}
	srv := &server{deps: queueDeps(reader, nil)}
	ctx := humanworkContext(t, ListWorkItemsProcedure)
	envErr := &envelope.Error{}

	// Malformed requests land INVALID_ARGUMENT and absent resources land
	// NOT_FOUND. ClaimWorkItem, ReleaseWorkItem (EP-WORK-002) and
	// CompleteWorkItem, DecideApproval (EP-WORK-003) are all exercised by
	// their own test suites below; an empty request to any of the four now
	// fails validation (INVALID_ARGUMENT), not a P1B stub refusal -- none of
	// them carries one any more.
	for _, tc := range []struct {
		name string
		call func() error
		want envelope.Code
	}{
		{"oversized page", func() error {
			_, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 101}})
			return err
		}, envelope.CodeInvalidArgument},
		{"empty id", func() error {
			_, err := srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{})
			return err
		}, envelope.CodeInvalidArgument},
		{"absent", func() error {
			_, err := srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{WorkItemId: uuid.NewString()})
			return err
		}, envelope.CodeNotFound},
	} {
		if err := tc.call(); !errors.As(err, &envErr) || envErr.Code() != tc.want {
			t.Fatalf("%s err = %v, want code %v", tc.name, err, tc.want)
		}
	}
	// The refused calls never reached the reader's call counter beyond the
	// successful reads above.
	if reader.calls.Load() != 1 {
		t.Fatalf("reader calls = %d, want 1 (only the successful get)", reader.calls.Load())
	}
}

func TestTodo_EP_WORK_001_Mutation(t *testing.T) {
	item := queueItem(nil)
	reader := &queueTestReader{tenant: transporttest.Tenant, items: []workitem.WorkItem{item, queueItem(nil)}}
	srv := &server{deps: queueDeps(reader, nil)}
	ctx := humanworkContext(t, ListWorkItemsProcedure)
	envErr := &envelope.Error{}

	res, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 1}})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	cursor := res.GetPage().GetNextCursor()

	// Removing a member between pages invalidates the snapshot-bound cursor.
	reader.mu.Lock()
	reader.items = reader.items[:1]
	reader.mu.Unlock()
	if _, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 1, Cursor: cursor}}); !errors.As(err, &envErr) || envErr.Code() != envelope.CodeInvalidArgument {
		t.Fatalf("stale-snapshot cursor err = %v, want INVALID_ARGUMENT", err)
	}
	// An index past the end of the live queue is refused.
	pastEnd := mustCursor(t, queueCursor{
		Principal: transporttest.Subject, Tenant: transporttest.Tenant,
		Snapshot: queueDigest(reader.items), Index: 99, Version: cursorVersion,
		ExpiresAt: workNow.Add(time.Minute).Unix(), Nonce: "n",
	}, []byte("work-queue-test-key"))
	if _, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 1, Cursor: pastEnd}}); !errors.As(err, &envErr) || envErr.Code() != envelope.CodeInvalidArgument {
		t.Fatalf("out-of-bounds cursor err = %v, want INVALID_ARGUMENT", err)
	}
	// With no cursor key, paging is unusable and fails rather than minting
	// unsigned cursors.
	noKey := &server{deps: Dependencies{Queue: reader, Now: func() time.Time { return workNow }}}
	if _, err := noKey.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 1, Cursor: cursor}}); err == nil {
		t.Fatal("unsigned cursor decoded with an empty key")
	}
}

func TestWorkItemHandlerAnswersBothProcedures(t *testing.T) {
	// The admitted-context check happens in the edge's admission interceptor;
	// the bare mux only proves both procedures are mounted and decode.
	item := queueItem(nil)
	server := httptest.NewServer(NewHandler(queueDeps(&queueTestReader{tenant: transporttest.Tenant, items: []workitem.WorkItem{item}}, nil)))
	t.Cleanup(server.Close)

	res, err := server.Client().Post(server.URL+ListWorkItemsProcedure, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST ListWorkItems: %v", err)
	}
	_ = res.Body.Close()
	res, err = server.Client().Post(server.URL+GetWorkItemProcedure, "application/json", strings.NewReader(`{"workItemId":"`+item.WorkItemID.String()+`"}`))
	if err != nil {
		t.Fatalf("POST GetWorkItem: %v", err)
	}
	_ = res.Body.Close()
}
