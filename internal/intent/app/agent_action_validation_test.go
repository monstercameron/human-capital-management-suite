package app

import (
	"context"
	"testing"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/proto"
)

func TestTodo_AGENT_035(t *testing.T) {
	h := newSubmitHarness(t)
	principal := submitPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	request := promoteWorkerCreateRequest(t, principal, "agent-validation")
	published, err := h.Service.GetIntentDefinition(ctx, &registryv1.GetIntentDefinitionRequest{Definition: request.Definition})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Service.ValidateAgentIntentDraft(ctx, published.IntentDefinition, request); err != nil {
		fatalWithDiagnostic(t, "validate", err)
	}
	page, err := h.Store.ListIntents(ctx, principal.Tenant().String(), 50, "")
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("validation wrote drafts: %+v %v", page, err)
	}
}

func TestTodo_AGENT_035_Security(t *testing.T) {
	h := newSubmitHarness(t)
	principal := submitPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	request := promoteWorkerCreateRequest(t, principal, "agent-validation-security")
	published, err := h.Service.GetIntentDefinition(ctx, &registryv1.GetIntentDefinitionRequest{Definition: request.Definition})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Service.ValidateAgentIntentDraft(context.Background(), published.IntentDefinition, request); err == nil {
		t.Fatal("unauthenticated validation accepted")
	}
	request.Scope = &commonv1.ScopeContext{TenantId: "foreign-tenant", OrganizationScopeId: principal.OrganizationScopeID(), Purpose: principal.DefaultPurpose()}
	if err := h.Service.ValidateAgentIntentDraft(ctx, published.IntentDefinition, request); err == nil {
		t.Fatal("foreign tenant draft accepted")
	}
	page, err := h.Store.ListIntents(ctx, principal.Tenant().String(), 50, "")
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("security refusal wrote drafts: %+v %v", page, err)
	}
}

func TestTodo_AGENT_035_Mutation(t *testing.T) {
	h := newSubmitHarness(t)
	principal := submitPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	request := promoteWorkerCreateRequest(t, principal, "agent-validation-mutation")
	published, err := h.Service.GetIntentDefinition(ctx, &registryv1.GetIntentDefinitionRequest{Definition: request.Definition})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*intentsv1.IntentDefinition, *intentsv1.CreateIntentRequest)
	}{
		{"risk", func(d *intentsv1.IntentDefinition, r *intentsv1.CreateIntentRequest) { d.RiskClass = "LOW" }},
		{"governance", func(d *intentsv1.IntentDefinition, r *intentsv1.CreateIntentRequest) {
			d.GovernanceRequirementRefs = nil
		}},
		{"capability", func(d *intentsv1.IntentDefinition, r *intentsv1.CreateIntentRequest) { d.RequiredCapabilityRefs = nil }},
		{"schema", func(d *intentsv1.IntentDefinition, r *intentsv1.CreateIntentRequest) {
			r.Request.Schema.SchemaId = "invented"
		}},
		{"subject", func(d *intentsv1.IntentDefinition, r *intentsv1.CreateIntentRequest) {
			r.Subjects[0].SubjectKind = "INVENTED"
		}},
		{"arguments", func(d *intentsv1.IntentDefinition, r *intentsv1.CreateIntentRequest) {
			r.Request.ProtobufWireBytes = nil
		}},
		{"initiator", func(d *intentsv1.IntentDefinition, r *intentsv1.CreateIntentRequest) {
			r.Initiator.PrincipalId = "someone-else"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := proto.Clone(published.IntentDefinition).(*intentsv1.IntentDefinition)
			r := proto.Clone(request).(*intentsv1.CreateIntentRequest)
			tc.mutate(d, r)
			if err := h.Service.ValidateAgentIntentDraft(ctx, d, r); err == nil {
				t.Fatal("changed action accepted")
			}
			page, err := h.Store.ListIntents(ctx, principal.Tenant().String(), 50, "")
			if err != nil || len(page.Records) != 0 {
				t.Fatalf("refusal wrote drafts: %+v %v", page, err)
			}
		})
	}
}
