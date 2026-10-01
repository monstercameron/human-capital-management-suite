package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/artifacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_AGENT_015_NativeSourceArtifact_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'native-context','cell-local','Native context','ACTIVE',CURRENT_TIMESTAMP)`, tenant)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	projection := CommonAgentSourceContext{Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "Approved snapshot content"}}}
	content, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatal(err)
	}
	stored, _, err := artifacts.Put(ctx, tx, artifacts.Schema(db.Schema), artifacts.PutRequest{Tenant: tenant, Content: content, MediaType: "application/json", Classification: model.ClassInternal, RetentionClass: "agent-context", CreatorPrincipalRef: "issuer", EvidenceID: "approved-context"})
	if err != nil {
		t.Fatal(err)
	}
	if err = artifacts.AddReference(ctx, tx, artifacts.Schema(db.Schema), tenant, stored.ContentID, artifacts.OwnerRef{Kind: artifacts.OwnerReceipt, ID: "approved-scope"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	source := NativeCommonAgentSource{Core: conn, CoreSchema: db.Schema, ResolveTenant: func(key string) uuid.UUID {
		if key == "native-context" {
			return tenant
		}
		return uuid.Nil
	}, Now: func() time.Time { return now }, InputClasses: []model.ClassificationLabel{model.ClassInternal}}
	request := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "native-context"}, Purpose: "approved-work", Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal", InvokerID: "human-invoker"}, Context: agentrun.ContextScope{ID: "approved-scope", SnapshotID: "artifact:" + stored.ContentID, Digest: "sha256:" + stored.ContentID}, Deadline: now.Add(time.Hour)}
	got, err := source.readPinnedContextArtifact(ctx, request)
	if err != nil || len(got.Messages) != 1 || got.Messages[0].Content != "Approved snapshot content" || len(got.References) != 1 || got.References[0].Digest != request.Context.Digest {
		t.Fatalf("approved context: %+v, %v", got, err)
	}
	request.Context.ID = "different-scope"
	if _, err := source.readPinnedContextArtifact(ctx, request); err == nil {
		t.Fatal("unreferenced scope read protected context")
	}
	request.Context.ID = "approved-scope"
	request.Context.Digest = "sha256:wrong"
	if _, err := source.readPinnedContextArtifact(ctx, request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("changed pin: %v", err)
	}
	var requestedBy string
	if err := db.SQL.QueryRowContext(ctx, `SELECT requested_by FROM `+artifacts.Schema(db.Schema)+`.artifact_retrieval_refusal WHERE tenant_id=$1`, tenant).Scan(&requestedBy); err != nil || requestedBy != "human-invoker" {
		t.Fatalf("refusal identity %q: %v", requestedBy, err)
	}
}

func TestTodo_AGENT_015_NativeSourceAndOutputRefusals(t *testing.T) {
	ctx := context.Background()
	if _, err := (NativeCommonAgentSource{}).ReadCommonAgentSourceContext(ctx, agentrun.Record{}, runstate.Run{}); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("unconfigured source: %v", err)
	}
	if _, err := (NativeCommonAgentExecutionSource{}).BuildCommonAgentModelWork(ctx, agentrun.Record{}, runstate.Run{}); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("unconfigured model work: %v", err)
	}
	if _, err := (CommonAgentProtectedOutput{}).ValidateAndPersistCommonAgentOutput(ctx, agentrun.Record{}, runstate.Run{}, agentmodel.ModelResult{}); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("unconfigured output: %v", err)
	}
	if err := (CommonAgentProtectedOutput{}).DeliverCommonAgentOutput(ctx, agentrun.Record{}, runstate.Run{}, CommonAgentOutput{Ref: "artifact:forged", Digest: "sha256:forged"}); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("forged delivery: %v", err)
	}
}

func TestTodo_AGENT_015_WorkflowExecutionContextHydration(t *testing.T) {
	execution := runtime.ExecutionContext{Principal: "actual-invoker", Tenant: "tenant-a", Locale: "de-DE", Organization: "organization-a", LegalEntity: "entity-a", Purpose: "bounded-analysis", BillingRef: "billing-a", ExecutionMode: workflow.ModeExecute, WorkflowID: "workflow-a", WorkflowVersion: 3, CompiledPlanDigest: "sha256:plan", RuntimeVersion: runtime.RuntimeVersion}
	material, err := commonAgentWorkflowExecutionMaterial(CommonAgentSourceContext{Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "Approved input"}}}, execution, "instance-a")
	if err != nil || len(material.Messages) != 2 || !strings.Contains(material.Messages[0].Content, `"locale":"de-DE"`) || !strings.Contains(material.Messages[0].Content, `"compiled_plan_digest":"sha256:plan"`) || strings.Contains(material.Messages[0].Content, "actual-invoker") || material.Messages[1].Content != "Approved input" || len(material.References) != 1 || material.References[0].ID != "workflow-execution:instance-a" || material.References[0].Digest != execution.Digest() {
		t.Fatalf("hydrated workflow execution context %+v: %v", material, err)
	}
	execution.Locale = ""
	if _, err := commonAgentWorkflowExecutionMaterial(CommonAgentSourceContext{}, execution, "instance-a"); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("invalid execution context: %v", err)
	}
}

type commonAgentProtectedOutputPolicyTest struct {
	content   []byte
	delivered int
}

func (p *commonAgentProtectedOutputPolicyTest) ValidateOutput(_ context.Context, record agentrun.Record, result agentmodel.ModelResult) ([]byte, model.ClassificationLabel, string, error) {
	if result.Text != "approved answer" || record.ID == "" {
		return nil, "", "", ErrCommonAgentWorker
	}
	p.content = []byte(fmt.Sprintf(`{"run_id":%q,"answer":%q}`, record.ID, result.Text))
	return p.content, model.ClassInternal, "agent-output", nil
}

func (p *commonAgentProtectedOutputPolicyTest) DeliverOutput(_ context.Context, _ agentrun.Record, _ CommonAgentOutput, content []byte) error {
	if string(content) != string(p.content) {
		return ErrCommonAgentWorker
	}
	p.delivered++
	return nil
}

func TestTodo_AGENT_015_NativeOutputArtifact_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'common-tenant','cell-local','Common output','ACTIVE',CURRENT_TIMESTAMP)`, tenant)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	runtime, _, _, now := commonAgentTestRuntime(t)
	record, _, err := runtime.Admit(ctx, commonAgentTestRequest(*now))
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.GetRun(ctx, "common-tenant", record.ID)
	if err != nil {
		t.Fatal(err)
	}
	policy := &commonAgentProtectedOutputPolicyTest{}
	outputOwner := CommonAgentProtectedOutput{Core: conn, CoreSchema: db.Schema, ResolveTenant: func(key string) uuid.UUID {
		if key == "common-tenant" {
			return tenant
		}
		return uuid.Nil
	}, Policy: policy, Current: runtime, Now: func() time.Time { return *now }, OutputClasses: []model.ClassificationLabel{model.ClassInternal}, SourceClasses: []model.ClassificationLabel{model.ClassInternal}}
	output, err := outputOwner.ValidateAndPersistCommonAgentOutput(ctx, record, run, agentmodel.ModelResult{Text: "approved answer"})
	if err != nil || output.Ref == "" || output.Digest == "" {
		t.Fatalf("persisted output %+v: %v", output, err)
	}
	if err := outputOwner.DeliverCommonAgentOutput(ctx, record, run, output); err != nil || policy.delivered != 1 {
		t.Fatalf("delivery %d: %v", policy.delivered, err)
	}
	changed := output
	changed.Ref = "artifact:" + strings.Repeat("a", 64)
	changed.Digest = "sha256:" + strings.Repeat("a", 64)
	if err := outputOwner.DeliverCommonAgentOutput(ctx, record, run, changed); err == nil || policy.delivered != 1 {
		t.Fatalf("forged artifact delivered %d: %v", policy.delivered, err)
	}
	var references, refusals int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM `+artifacts.Schema(db.Schema)+`.artifact_reference_event WHERE tenant_id=$1 AND content_id=$2 AND owner_id=$3`, tenant, strings.TrimPrefix(output.Digest, "sha256:"), record.ID).Scan(&references); err != nil || references != 1 {
		t.Fatalf("run reference %d: %v", references, err)
	}
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM `+artifacts.Schema(db.Schema)+`.artifact_retrieval_refusal WHERE tenant_id=$1`, tenant).Scan(&refusals); err != nil || refusals != 1 {
		t.Fatalf("refusal evidence %d: %v", refusals, err)
	}
}
