package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

type sweepLister map[string][]chatgate.Scope

func (l sweepLister) ListGateScopes(_ context.Context, tenant string) ([]chatgate.Scope, error) {
	return l[tenant], nil
}

type sweepRecorder struct {
	mu     sync.Mutex
	calls  []chatgate.Scope
	actors []chatgate.Actor
	system []bool
	fail   map[string]bool
}

func (r *sweepRecorder) SweepQueue(ctx context.Context, a chatgate.Actor, s chatgate.Scope) (int, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
	r.actors = append(r.actors, a)
	r.system = append(r.system, chatgateIsSystem(ctx, a))
	if r.fail[s.Conversation] {
		return 0, 0, errors.New("boom")
	}
	return 1, 0, nil
}

type sweepLog struct{ n int }

func (l *sweepLog) Error(string, ...any) { l.n++ }

const t3817Tenant = "tenant-a"
const t3817Tenant2 = "tenant-b"

// TestIssue3817_SweepRunsPerTenantAsSystem: every gate of every served tenant is
// swept as that tenant's system actor, and one failing gate does not stop the
// rest.
func TestIssue3817_SweepRunsPerTenantAsSystem(t *testing.T) {
	lister := sweepLister{
		t3817Tenant:  {{Tenant: t3817Tenant, Conversation: "a1"}, {Tenant: t3817Tenant, Conversation: "a2"}},
		t3817Tenant2: {{Tenant: t3817Tenant2, Conversation: "b1"}},
	}
	rec := &sweepRecorder{fail: map[string]bool{"a1": true}}
	log := &sweepLog{}
	n := sweepChatgateReviewQueues(t.Context(), lister, rec, []string{t3817Tenant, t3817Tenant2}, log)
	if len(rec.calls) != 3 || n != 2 || log.n != 1 {
		t.Fatalf("calls=%v swept=%d logged=%d", rec.calls, n, log.n)
	}
	for i, call := range rec.calls {
		if rec.actors[i].Tenant != call.Tenant || !rec.system[i] {
			t.Errorf("call %d ran as %+v system=%v", i, rec.actors[i], rec.system[i])
		}
	}
}

func TestIssue3817_SweepWorkloadRunsAndStops(t *testing.T) {
	rec := &sweepRecorder{}
	w := chatgateReviewSweepWorkload(sweepLister{t3817Tenant: {{Tenant: t3817Tenant, Conversation: "a1"}}}, rec, []string{t3817Tenant}, &sweepLog{}, time.Hour)
	if w == nil {
		t.Fatal("no workload")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	deadline := time.After(5 * time.Second)
	for {
		rec.mu.Lock()
		n := len(rec.calls)
		rec.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the first sweep never ran")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the workload did not stop")
	}
	if chatgateReviewSweepWorkload(nil, rec, []string{t3817Tenant}, nil, time.Hour) != nil || chatgateReviewSweepWorkload(sweepLister{}, rec, nil, nil, time.Hour) != nil {
		t.Fatal("a workload was built without a lister or tenants")
	}
}

// TestIssue3817_SystemActor_Security: the sweep's identity opens only the
// sweep's own permission, only when the context carries the in-process marker,
// and only inside its own tenant.
func TestIssue3817_SystemActor_Security(t *testing.T) {
	world := &chatgateWorld{roles: map[string][]string{}, kinds: map[string]chat.ConversationKind{"room": chat.PrivateChannel}, members: map[string]map[string]chat.MembershipRole{"room": {}}}
	a := &ChatgateAuthority{Roles: world, Store: world, People: world}
	scope := chatgate.Scope{Tenant: "tenant", Conversation: "room"}
	system := chatgate.Actor{Tenant: "tenant", Person: chatgateSweepActor}
	sysCtx := chatgateSystemContext(t.Context())
	if err := a.Check(sysCtx, system, scope, "retention"); err != nil {
		t.Fatalf("the sweep was refused its own permission: %v", err)
	}
	for _, permission := range []string{"admin", "export", "read_answers", "privacy_report", "agent_run", "eligible", "discover"} {
		if permission == "read_answers" {
			continue // a self permission any actor holds for their own answers
		}
		if err := a.Check(sysCtx, system, scope, permission); !errors.Is(err, chatgate.ErrDenied) {
			t.Errorf("the sweep identity held %q: %v", permission, err)
		}
	}
	if err := a.Check(t.Context(), system, scope, "retention"); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("a request that is not the sweep held its permission: %v", err)
	}
	if err := a.Check(sysCtx, chatgate.Actor{Tenant: "tenant", Person: "someone"}, scope, "retention"); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("an ordinary person held it with the marker: %v", err)
	}
	if err := a.Check(sysCtx, chatgate.Actor{Tenant: "other", Person: chatgateSweepActor}, scope, "retention"); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("another workspace's sweep reached this one: %v", err)
	}
}
