package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestAgentWorkflowSourceAuthorityUsesDurableOwner(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	core := pgtest.New(t)
	tenant, instance := uuid.New(), uuid.New()
	request := commonAgentTestRequest(now)
	request.Source = agentrun.SourceIdentity{TenantID: "common-tenant", Kind: agentrun.SourceWorkflow, Ref: "workflow:" + instance.String() + ":analyze"}
	request.Source, _ = (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	plan := workflow.CompiledWorkflow{WorkflowID: "workflow-agent-authority", Version: 1, StartNodeID: "analyze", Nodes: []workflow.CompiledNode{{ID: "analyze", Type: workflow.StepCapability, Capability: &workflow.CompiledCapability{ID: "agents.invoke", Version: 1}, Governance: workflow.CompiledGovernance{Purpose: request.Purpose}, Mappings: []workflow.CompiledMapping{{Target: "request_json", SourceKind: workflow.SourceConstant, Constant: string(input)}}}}}
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := workflow.DecodeCanonicalPlan(body)
	if err != nil {
		t.Fatal(err)
	}
	versions := version.NewRegistry()
	if err = versions.Put(version.CompiledVersion{WorkflowID: plan.WorkflowID, DefinitionVersion: 1, SemanticVersion: "1.0.0", CompiledPlanDigest: decoded.Digest(), CanonicalPlanBytes: body, Status: version.StatusActive}); err != nil {
		t.Fatal(err)
	}
	execution := runtime.ExecutionContext{Principal: request.Principal.SponsorID, Tenant: request.Source.TenantID, Locale: "und", LegalEntity: request.LegalEntity, Purpose: request.Purpose, BillingRef: "billing:workflow", ExecutionMode: workflow.ModeExecute, WorkflowID: plan.WorkflowID, WorkflowVersion: 1, CompiledPlanDigest: decoded.Digest(), RuntimeVersion: runtime.RuntimeVersion}
	contextBytes, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'common-tenant','cell-local','Workflow source','ACTIVE',$2)`, tenant, now)
	core.Exec(t, `INSERT INTO workflow_instance(tenant_id,instance_id,cell_id,workflow_id,workflow_version,compiled_plan_hash,business_subject_refs,execution_mode,runtime_status,completion_dimensions,input_ref,effective_context_ref,variable_revision_head,current_node_ids,correlation_id,created_at) VALUES($1,$2,'cell-local',$3,1,$4,'{}','EXECUTE','RUNNING','{}','input:workflow',$5,0,'{analyze}','workflow-source',$6)`, tenant, instance, plan.WorkflowID, decoded.Digest(), execution.Digest(), now)
	core.Exec(t, `INSERT INTO workflow_execution_context(tenant_id,instance_id,context_digest,context,recorded_at) VALUES($1,$2,$3,$4,$5)`, tenant, instance, execution.Digest(), contextBytes, now)
	core.Exec(t, `INSERT INTO workflow_execution_delegation(tenant_id,instance_id,subject,subject_kind,tenant_key,organization_scope_id,roles,purposes,authentication_method,assurance,session_ref,evidence_ref,recorded_at) VALUES($1,$2,$3,'HUMAN','common-tenant','org-test','{}',ARRAY[$4],'BEARER_TOKEN','HIGH','session:test','evidence:test',$5)`, tenant, instance, request.Principal.SponsorID, request.Purpose, now)
	conn := core.NewConn(t)
	if _, err = conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	authority := AgentWorkflowSourceAuthority{Core: conn, Versions: versions, ResolveTenant: func(ref string) uuid.UUID {
		if ref == "common-tenant" {
			return tenant
		}
		return uuid.Nil
	}}
	if err = authority.CheckRequest(ctx, request); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*agentrun.Request){func(r *agentrun.Request) { r.Agent.Version = "999" }, func(r *agentrun.Request) { r.Source.Ref = "workflow:" + instance.String() + ":future" }, func(r *agentrun.Request) { r.Source.TenantID = "other" }, func(r *agentrun.Request) { r.Principal.SponsorID = "other-sponsor" }} {
		altered := request
		change(&altered)
		if err = authority.CheckRequest(ctx, altered); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
			t.Fatalf("altered source authority=%v", err)
		}
	}
	core.Exec(t, `UPDATE workflow_execution_delegation SET purposes='{revoked}' WHERE tenant_id=$1 AND instance_id=$2`, tenant, instance)
	if err = authority.CheckRequest(ctx, request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("revoked workflow purpose=%v", err)
	}
}
