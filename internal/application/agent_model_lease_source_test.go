package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

type modelLeaseCustody struct{ now time.Time }

func (p modelLeaseCustody) IssueLease(ctx custody.Context, h custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	return custody.Lease{ID: "custody-1", Handle: h, Operation: op, ExpiresAt: p.now.Add(ttl), ContextDigest: custody.ContextDigest(ctx.RequestContext)}, nil
}

type modelLeaseAuthority struct {
	handle custody.Handle
	seen   ModelCredentialRequest
}

func (a *modelLeaseAuthority) ResolveModelCredential(_ context.Context, req ModelCredentialRequest) (custody.Handle, error) {
	a.seen = req
	return a.handle, nil
}

func modelLeaseSourceFixture(t *testing.T) (*ModelLeaseSource, *modelLeaseAuthority, TrustedModelTask) {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	manager, err := lease.NewManager(modelLeaseCustody{now: now}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	authority := &modelLeaseAuthority{handle: custody.Handle{ID: "opaque-model-key", Kind: custody.Secret, Version: "v3", Tenant: "tenant-a", Region: "us-east"}}
	source, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"openai": authority}, Leases: manager, MaxTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err := NewTrustedModelTask("tenant-a", "task-1", "agent-1", "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	return source, authority, task
}

func TestTodo_AGENT_020_ModelLeaseSource_IssuesAndConsumesOnce(t *testing.T) {
	source, authority, task := modelLeaseSourceFixture(t)
	got, err := source.Issue(context.Background(), ModelLeaseRequest{Task: task, ProviderID: "openai", Destination: "model.openai", Purpose: "agent.inference", Region: "us-east", TTL: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if authority.seen.TaskID != task.TaskID || authority.seen.TenantID != task.TenantID || authority.seen.AgentID != task.AgentID {
		t.Fatalf("authority request lost trusted binding: %+v", authority.seen)
	}
	if got.Handle.ID != "opaque-model-key" || got.Tenant != task.TenantID || got.Destination != "model.openai" {
		t.Fatalf("lease scope = %+v", got)
	}
	if _, err := source.Use(ModelLeaseUseRequest{Task: task, ProviderID: "openai", Destination: "model.openai", Lease: got}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Use(ModelLeaseUseRequest{Task: task, ProviderID: "openai", Destination: "model.openai", Lease: got}); !errors.Is(err, lease.ErrAlreadyUsed) {
		t.Fatalf("second use = %v, want single-use refusal", err)
	}
}

func TestTodo_AGENT_020_ModelLeaseSource_MissingProviderNamesOnlyProvider(t *testing.T) {
	source, _, task := modelLeaseSourceFixture(t)
	_, err := source.Issue(context.Background(), ModelLeaseRequest{Task: task, ProviderID: "anthropic", Destination: "model.anthropic", Purpose: "agent.inference", Region: "us-east", TTL: time.Second})
	if !errors.Is(err, ErrModelLeaseProviderMissing) || !containsModelLeaseText(err.Error(), "anthropic") {
		t.Fatalf("missing provider error = %v", err)
	}
	if containsModelLeaseText(err.Error(), "opaque-model-key") {
		t.Fatal("missing provider error exposed credential handle")
	}
}

func TestTodo_AGENT_024_ModelLeaseSource_RejectsUntrustedOrWidenedScope(t *testing.T) {
	source, _, task := modelLeaseSourceFixture(t)
	for name, req := range map[string]ModelLeaseRequest{
		"untrusted": {Task: TrustedModelTask{TenantID: task.TenantID, TaskID: task.TaskID, AgentID: task.AgentID, Workload: task.Workload}, ProviderID: "openai", Destination: "model.openai", Purpose: "agent.inference", Region: "us-east", TTL: time.Second},
		"too_long":  {Task: task, ProviderID: "openai", Destination: "model.openai", Purpose: "agent.inference", Region: "us-east", TTL: 2 * time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := source.Issue(context.Background(), req); !errors.Is(err, ErrModelLeaseTaskBinding) && !errors.Is(err, ErrModelLeaseScope) {
				t.Fatalf("Issue error = %v", err)
			}
		})
	}
	leaseValue, err := source.Issue(context.Background(), ModelLeaseRequest{Task: task, ProviderID: "openai", Destination: "model.openai", Purpose: "agent.inference", Region: "us-east", TTL: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := NewTrustedModelTask(task.TenantID, task.TaskID, "agent-other", task.Workload)
	if _, err := source.Use(ModelLeaseUseRequest{Task: foreign, ProviderID: "openai", Destination: "model.openai", Lease: leaseValue}); !errors.Is(err, ErrModelLeaseScope) {
		t.Fatalf("foreign task use = %v", err)
	}
}

func TestTodo_AGENT_024_ModelLeaseSource_RequiresAuthority(t *testing.T) {
	if _, err := NewModelLeaseSource(ModelLeaseSourceConfig{}); !errors.Is(err, ErrModelLeaseNotConfigured) {
		t.Fatalf("empty source = %v", err)
	}
	task, _ := NewTrustedModelTask("tenant-a", "task-1", "agent-1", "worker-1")
	if _, err := (&ModelLeaseSource{}).Issue(context.Background(), ModelLeaseRequest{Task: task}); !errors.Is(err, ErrModelLeaseNotConfigured) {
		t.Fatalf("nil source issue = %v", err)
	}
}

func containsModelLeaseText(value, want string) bool {
	for i := 0; i+len(want) <= len(value); i++ {
		if value[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
