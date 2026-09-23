package humanwork

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func rt004Reader() *queueTestReader {
	return &queueTestReader{tenant: transporttest.Tenant, items: []workitem.WorkItem{queueItem(nil)}}
}

func rt004Unwired(reader Reader) *server {
	return &server{deps: Dependencies{Queue: reader, CursorKey: []byte("work-queue-test-key"), Now: func() time.Time { return workNow }}}
}

func codeOfHumanwork(t *testing.T, err error) envelope.Code {
	t.Helper()
	envErr := &envelope.Error{}
	if !errors.As(err, &envErr) {
		t.Fatalf("error is not an owned envelope error: %v", err)
	}
	return envErr.Code()
}

// TestTodo_RBAC_RT_004 is the PRIMARY: a WorkService composed without its
// authorization hook refuses every call with PERMISSION_DENIED. The list
// refusal is the suite's K-07 case: an outsider learns nothing from an
// empty page because there is no empty page, only a refusal.
func TestTodo_RBAC_RT_004(t *testing.T) {
	srv := rt004Unwired(rt004Reader())
	ctx := humanworkContext(t, ListWorkItemsProcedure)

	if _, err := srv.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{}); err == nil {
		t.Fatal("unwired ListWorkItems succeeded")
	} else if codeOfHumanwork(t, err) != envelope.CodePermissionDenied {
		t.Fatalf("unwired ListWorkItems code = %v, want permission denied (not an empty page)", err)
	}
	if _, err := srv.GetWorkItem(ctx, &humanworkv1.GetWorkItemRequest{WorkItemId: "item-1"}); err == nil {
		t.Fatal("unwired GetWorkItem succeeded")
	} else if codeOfHumanwork(t, err) != envelope.CodePermissionDenied {
		t.Fatalf("unwired GetWorkItem code = %v", err)
	}
	for action, call := range map[string]func() error{
		ActionClaimWorkItem: func() error {
			_, err := srv.ClaimWorkItem(ctx, &humanworkv1.ClaimWorkItemRequest{WorkItemId: "item-1", IdempotencyKey: "k", ExpectedItemVersion: 1})
			return err
		},
		ActionReleaseWorkItem: func() error {
			_, err := srv.ReleaseWorkItem(ctx, &humanworkv1.ReleaseWorkItemRequest{WorkItemId: "item-1", IdempotencyKey: "k", ExpectedItemVersion: 1})
			return err
		},
		ActionCompleteWorkItem: func() error {
			_, err := srv.CompleteWorkItem(ctx, &humanworkv1.CompleteWorkItemRequest{WorkItemId: "item-1", IdempotencyKey: "k", ExpectedItemVersion: 1})
			return err
		},
		ActionDecideApproval: func() error {
			_, err := srv.DecideApproval(ctx, &humanworkv1.DecideApprovalRequest{WorkItemId: "item-1", IdempotencyKey: "k", ExpectedItemVersion: 1})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("unwired %s succeeded", action)
		} else if codeOfHumanwork(t, err) != envelope.CodePermissionDenied {
			t.Errorf("unwired %s code = %v", action, err)
		}
	}
}

// TestTodo_RBAC_RT_004_Security proves the hook, not the token, decides:
// a principal admitted with workforce token roles but refused by the hook
// (as the durable resolution would refuse an unassigned subject) loses on
// every method, and a wired hook still answers before scope handling, so a
// foreign-tenant scope never downgrades the refusal into a quiet page.
func TestTodo_RBAC_RT_004_Security(t *testing.T) {
	deny := func(context.Context, *trust.Principal, string) bool { return false }
	srv := &server{deps: Dependencies{Queue: rt004Reader(), CursorKey: []byte("work-queue-test-key"),
		Now: func() time.Time { return workNow }, Authorize: deny}}
	roleCtx := humanworkContext(t, ListWorkItemsProcedure, func(c *trust.Claims) {
		c.Roles = []string{"hcm_admin", "manager"}
	})

	if _, err := srv.ListWorkItems(roleCtx, &humanworkv1.ListWorkItemsRequest{}); err == nil {
		t.Fatal("hook-denied list with admin token roles succeeded")
	} else if codeOfHumanwork(t, err) != envelope.CodePermissionDenied {
		t.Fatalf("hook-denied list code = %v", err)
	}
	if _, err := srv.GetWorkItem(roleCtx, &humanworkv1.GetWorkItemRequest{WorkItemId: "item-1"}); err == nil {
		t.Fatal("hook-denied get with admin token roles succeeded")
	}
	// An unwired service denies even the non-disclosing foreign-scope
	// shape: the refusal is the answer, never a quiet empty page.
	unwired := rt004Unwired(rt004Reader())
	if _, err := unwired.ListWorkItems(roleCtx, &humanworkv1.ListWorkItemsRequest{}); err == nil {
		t.Fatal("unwired list with admin token roles succeeded")
	} else if codeOfHumanwork(t, err) != envelope.CodePermissionDenied {
		t.Fatalf("unwired list code = %v", err)
	}
}

// TestTodo_RBAC_RT_004_Integration drives the served Connect mux: the
// admitted caller the hook allows reads the queue over HTTP, while the
// caller the hook refuses is turned away at the same route.
func TestTodo_RBAC_RT_004_Integration(t *testing.T) {
	allow := func(_ context.Context, p *trust.Principal, action string) bool {
		return p.Subject() == transporttest.Subject && action == ActionListWorkItems
	}
	allowed := httptest.NewServer(NewHandler(Dependencies{
		Queue: rt004Reader(), CursorKey: []byte("work-queue-test-key"),
		Now: func() time.Time { return workNow }, Authorize: allow,
	}))
	t.Cleanup(allowed.Close)

	post := func(server *httptest.Server, ctx context.Context, body string) int {
		t.Helper()
		req := httptest.NewRequest("POST", server.URL+ListWorkItemsProcedure, strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		server.Config.Handler.ServeHTTP(rec, req)
		return rec.Code
	}

	insider := humanworkContext(t, ListWorkItemsProcedure)
	if code := post(allowed, insider, `{}`); code != 200 {
		t.Fatalf("allowed list over HTTP = %d, want 200", code)
	}
	outsiderCtx := humanworkContext(t, ListWorkItemsProcedure, func(c *trust.Claims) {
		c.Subject = "outsider-x"
	})
	if code := post(allowed, outsiderCtx, `{}`); code == 200 {
		t.Fatal("hook-refused list over HTTP returned 200")
	}

	unwired := httptest.NewServer(NewHandler(Dependencies{
		Queue: rt004Reader(), CursorKey: []byte("work-queue-test-key"),
		Now: func() time.Time { return workNow },
	}))
	t.Cleanup(unwired.Close)
	if code := post(unwired, insider, `{}`); code == 200 {
		t.Fatal("unwired list over HTTP returned 200")
	}
}
