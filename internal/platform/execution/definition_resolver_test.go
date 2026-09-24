package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type definitionStoreFunc func(context.Context, uuid.UUID, string, map[string]string, time.Time) (workflowversionstore.ActiveDefinition, bool, error)

func (f definitionStoreFunc) ResolveActiveDefinition(ctx context.Context, tenant uuid.UUID, intent string, facts map[string]string, at time.Time) (workflowversionstore.ActiveDefinition, bool, error) {
	return f(ctx, tenant, intent, facts, at)
}

type definitionFactsFunc func(context.Context, runtime.StartRequest) (map[string]string, error)

func (f definitionFactsFunc) Facts(ctx context.Context, req runtime.StartRequest) (map[string]string, error) {
	return f(ctx, req)
}

func TestTodo_WF_EXT_008(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("compile selector fixture plan: %v", err)
	}
	digest := plan.Digest()
	called := false
	resolver := execution.ActiveDefinitionResolver{
		Definitions: definitionStoreFunc(func(_ context.Context, gotTenant uuid.UUID, intent string, facts map[string]string, gotAt time.Time) (workflowversionstore.ActiveDefinition, bool, error) {
			called = true
			if gotTenant != tenant || intent != "hcmnext.people.change_manager/v1" || facts["change_kind"] != "MANAGER" || !gotAt.Equal(at) {
				t.Fatalf("selector received tenant=%s intent=%q facts=%v at=%s", gotTenant, intent, facts, gotAt)
			}
			return workflowversionstore.ActiveDefinition{
				Definition: workflow.Definition{WorkflowID: plan.WorkflowID},
				Compiled:   version.CompiledVersion{CompiledPlanDigest: digest, Status: version.StatusActive},
				Plan:       plan,
			}, true, nil
		}),
		Facts: definitionFactsFunc(func(context.Context, runtime.StartRequest) (map[string]string, error) {
			return map[string]string{"change_kind": "MANAGER"}, nil
		}),
	}
	req := runtime.StartRequest{
		TenantID: tenant, CreatedAt: at,
		Source: &runtime.StartSource{Kind: runtime.StartSourceProposal, IntentType: "hcmnext.people.change_manager/v1", Proposal: &runtime.ProposalBinding{}},
	}
	selection, err := resolver.ResolveWorkflow(ctx, req)
	if err != nil {
		t.Fatalf("ResolveWorkflow: %v", err)
	}
	if !called || selection.WorkflowID != plan.WorkflowID || selection.Pin.CompiledPlanDigest != digest || selection.Plan.Digest() != digest {
		t.Fatalf("selection = %+v, called=%v; want exact active plan %s", selection, called, digest)
	}
}

func TestTodo_WF_EXT_008_SelectionRefusals(t *testing.T) {
	ctx := context.Background()
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("compile selector fixture plan: %v", err)
	}
	digest := plan.Digest()
	base := runtime.StartRequest{TenantID: uuid.New(), CreatedAt: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
		Source: &runtime.StartSource{Kind: runtime.StartSourceProposal, IntentType: "hcmnext.people.change_manager/v1", Proposal: &runtime.ProposalBinding{}}}
	resolver := execution.ActiveDefinitionResolver{
		Definitions: definitionStoreFunc(func(context.Context, uuid.UUID, string, map[string]string, time.Time) (workflowversionstore.ActiveDefinition, bool, error) {
			return workflowversionstore.ActiveDefinition{Definition: workflow.Definition{WorkflowID: plan.WorkflowID}, Compiled: version.CompiledVersion{CompiledPlanDigest: digest}, Plan: plan}, false, nil
		}),
		Facts: definitionFactsFunc(func(context.Context, runtime.StartRequest) (map[string]string, error) { return nil, nil }),
	}
	if _, err := resolver.ResolveWorkflow(ctx, base); err == nil {
		t.Fatal("missing ACTIVE definition was accepted")
	}
	base.Source.IntentType = ""
	if _, err := resolver.ResolveWorkflow(ctx, base); err == nil {
		t.Fatal("missing typed intent was accepted")
	}
	base.Source.IntentType = "hcmnext.people.change_manager/v1"
	resolver.Facts = definitionFactsFunc(func(context.Context, runtime.StartRequest) (map[string]string, error) {
		return nil, errors.New("fact store unavailable")
	})
	if _, err := resolver.ResolveWorkflow(ctx, base); err == nil {
		t.Fatal("fact resolution failure was ignored")
	}
}
