package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

func TestTodo_AGENT_020_OpenAICredentialAuthority(t *testing.T) {
	now := time.Now().UTC()
	scope := OpenAIModelCredentialScope{TenantID: "tenant-a", Region: "us", Purpose: "persona.reply", Destination: "openai.profile"}
	authority, err := NewOpenAIModelCredentialAuthority(OpenAIModelCredentialConfig{CredentialID: "openai-key", Version: "v1", Workload: "persona.worker", Scopes: []OpenAIModelCredentialScope{scope}, MaxTTL: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := lease.NewManager(authority, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"openai": authority}, Leases: manager, MaxTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err := NewTrustedModelTask(scope.TenantID, "task-1", "agent-digest", "persona.worker")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := source.Issue(context.Background(), ModelLeaseRequest{Task: task, ProviderID: "openai", Destination: scope.Destination, Purpose: scope.Purpose, Region: scope.Region, TTL: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if issued.Handle.ID != "openai-key" || issued.Handle.Tenant != scope.TenantID || issued.CustodyLeaseID == "" {
		t.Fatalf("lease=%+v", issued)
	}
	if _, err := manager.Use(issued, scope.Destination, custody.Decrypt); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Use(issued, scope.Destination, custody.Decrypt); !errors.Is(err, lease.ErrAlreadyUsed) {
		t.Fatalf("replay=%v", err)
	}
	if _, err := authority.ResolveModelCredential(context.Background(), ModelCredentialRequest{ProviderID: "openai", TenantID: "other-tenant", Region: scope.Region, Purpose: scope.Purpose, Destination: scope.Destination, TaskID: "task-1", AgentID: "agent-digest"}); !errors.Is(err, ErrOpenAICredentialScope) {
		t.Fatalf("tenant escape=%v", err)
	}
	if _, err := NewOpenAIModelCredentialAuthority(OpenAIModelCredentialConfig{}); !errors.Is(err, ErrOpenAICredentialScope) {
		t.Fatalf("empty config=%v", err)
	}
}

func TestTodo_AGENT_021_OpenAIGatewayRequiresApprovedDependencies(t *testing.T) {
	if _, err := NewOpenAIAgentModelGateway(OpenAIAgentModelGatewayConfig{}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("empty config=%v", err)
	}
}
