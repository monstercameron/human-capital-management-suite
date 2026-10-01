package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/artifacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

func commonEvidenceFixture(t *testing.T) (*CommonAgentModelEvidence, context.Context, AgentModelExecutorRequest, agentrun.Record, *commonModelManifest, *commonModelContext, *pgtest.DB) {
	t.Helper()
	work, record, run, manifest, material, _ := commonModelFixture(t)
	request, err := work.BuildCommonAgentModelWork(context.Background(), record, run, material.material)
	if err != nil {
		t.Fatal(err)
	}
	request = commonAgentBindModelStep(request, run, 1)
	db := pgtest.New(t)
	db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'common-tenant','cell-local','Common model evidence','ACTIVE',CURRENT_TIMESTAMP)`, manifest.tenant)
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	evidence, err := NewCommonAgentModelEvidence(CommonAgentModelEvidence{Runtime: work.cfg.Runtime, Work: work, Core: conn, CoreSchema: db.Schema, RetentionClass: "agent-model-route", Now: work.cfg.Now})
	if err != nil {
		t.Fatal(err)
	}
	ctx := withCommonAgentModelEvidence(context.Background(), record, run, request)
	ctx = context.WithValue(ctx, openAIModelDispatchContextKey{}, openAIModelDispatchBinding{tenant: run.TenantID, runID: run.ID, stepID: request.StepID})
	return evidence, ctx, request, record, manifest, material, db
}

func commonEvidenceRoute(request AgentModelExecutorRequest) agentmodel.RouteRecord {
	route := agentmodel.RouteRecord{TraceID: request.StepID, AgentVersionDigest: request.Route.Pin.AgentVersionDigest, TaskProfileID: request.Route.Task.ID, Region: request.Route.Task.Region, BudgetMicros: request.Route.BudgetRemainingMicros, Selected: request.Route.Pin.Primary}
	raw, _ := json.Marshal(route)
	route.Digest = strings.TrimPrefix(personaRunBytesDigest(raw), "sha256:")
	return route
}

func TestTodo_AGENT_033_CommonEvidence_RouteArtifact_Integration(t *testing.T) {
	evidence, ctx, request, record, _, _, db := commonEvidenceFixture(t)
	route := commonEvidenceRoute(request)
	if err := evidence.RecordRoute(ctx, route); err != nil {
		t.Fatalf("record current route: %v", err)
	}
	tenant := evidence.Work.cfg.TenantUUID("common-tenant")
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	var contentID string
	if err := db.SQL.QueryRowContext(ctx, `SELECT content_id FROM `+artifacts.Schema(db.Schema)+`.artifact_reference_event WHERE tenant_id=$1 AND owner_kind='OBSERVATION' AND owner_id=$2`, tenant, record.ID+":model-route:"+route.TraceID).Scan(&contentID); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatal(err)
	}
	content, stored, err := artifacts.Retrieve(ctx, tx, artifacts.Schema(db.Schema), tenant, contentID, artifacts.RetrievalAuthorization{Purpose: record.Request.Purpose, AllowedClassifications: []model.ClassificationLabel{model.ClassInternal}, Scope: artifacts.SubjectScope{AllowedOwnerRefs: []string{record.ID + ":model-route:" + route.TraceID}}, RequestedBy: record.Request.Principal.AgentPrincipalID, ExpiresAt: record.Request.Deadline}, evidence.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var retained struct {
		AdmissionID   string                  `json:"admission_id"`
		RequestDigest string                  `json:"request_digest"`
		Principal     agentrun.PrincipalChain `json:"principal_chain"`
		Audience      agentrun.AudienceScope  `json:"audience"`
		Context       agentrun.ContextScope   `json:"context_scope"`
		Fence         uint64                  `json:"worker_fence"`
		Route         agentmodel.RouteRecord  `json:"route"`
	}
	if err := json.Unmarshal(content, &retained); err != nil {
		t.Fatal(err)
	}
	if stored.CreatorPrincipalRef != record.Request.Principal.AgentPrincipalID || retained.AdmissionID != record.ID || retained.RequestDigest != record.RequestDigest || retained.Principal != record.Request.Principal || retained.Audience != record.Request.Audience || retained.Context != record.Request.Context || retained.Fence == 0 || retained.Route.Digest != route.Digest {
		t.Fatalf("retained route lost owner bindings: %+v %+v", retained, stored)
	}
	bad := route
	bad.Eligibility = []agentmodel.Eligibility{{}}
	if err := evidence.RecordRoute(ctx, bad); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("changed route digest accepted: %v", err)
	}
	bad = route
	bad.Selected = agentmodel.ModelSelection{}
	if err := evidence.RecordRoute(ctx, bad); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("empty selected model accepted: %v", err)
	}
	if err := evidence.RecordRoute(context.Background(), route); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("unbound route accepted: %v", err)
	}
}

func TestTodo_AGENT_033_CommonEvidence_SourceClassification_Integration(t *testing.T) {
	evidence, ctx, request, _, manifest, _, _ := commonEvidenceFixture(t)
	for _, field := range request.Outbound.Fields {
		value, ok := field.Value.(string)
		if !ok {
			t.Fatalf("nonstring model field %s", field.Name)
		}
		classification := agentegress.SourceClassificationRequest{Tenant: request.Task.TenantID, Purpose: request.Outbound.Purpose, FieldName: field.Name, SourceClass: request.FieldSources[field.Name], DataClass: field.Class, Provenance: field.Provenance, ValueDigest: personaRunBytesDigest([]byte(value))}
		if err := evidence.VerifySourceClassification(ctx, classification); err != nil {
			t.Fatalf("current field %s: %v", field.Name, err)
		}
		classification.ValueDigest = personaRunBytesDigest([]byte("forged value"))
		if err := evidence.VerifySourceClassification(ctx, classification); !errors.Is(err, ErrCommonAgentWorker) {
			t.Fatalf("forged field %s: %v", field.Name, err)
		}
	}
	if len(request.Outbound.Fields) < 4 {
		t.Fatalf("missing source fields %+v", request.Outbound.Fields)
	}
	profile := request.Outbound.Fields[1]
	value := profile.Value.(string)
	classification := agentegress.SourceClassificationRequest{Tenant: request.Task.TenantID, Purpose: request.Outbound.Purpose, FieldName: profile.Name, SourceClass: "common-agent-source-context", DataClass: profile.Class, Provenance: profile.Provenance, ValueDigest: personaRunBytesDigest([]byte(value))}
	if err := evidence.VerifySourceClassification(ctx, classification); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("instructions laundered as source: %v", err)
	}
	classification.SourceClass = "common-agent-profile"
	classification.Tenant = "foreign-tenant"
	if err := evidence.VerifySourceClassification(ctx, classification); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("foreign tenant accepted: %v", err)
	}
	classification.Tenant = request.Task.TenantID
	manifest.instructions = "changed after build"
	if err := evidence.VerifySourceClassification(ctx, classification); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("changed instructions accepted: %v", err)
	}
	if err := evidence.VerifySourceClassification(context.Background(), classification); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("unbound source accepted: %v", err)
	}
}

func TestTodo_AGENT_033_CommonEvidence_ConstructorAndStaleLease(t *testing.T) {
	if _, err := NewCommonAgentModelEvidence(CommonAgentModelEvidence{}); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("unconfigured evidence: %v", err)
	}
	evidence, ctx, request, _, _, _, _ := commonEvidenceFixture(t)
	run, err := evidence.Runtime.GetRun(ctx, request.Task.TenantID, request.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Lease == nil {
		t.Fatal("fixture did not claim worker lease")
	}
	// Advance the trusted clock beyond the persisted lease without changing the
	// private dispatch binding: a stale worker may not record route evidence.
	evidence.Now = func() time.Time { return run.Lease.Until.Add(time.Second) }
	if err := evidence.RecordRoute(ctx, commonEvidenceRoute(request)); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("stale lease accepted: %v", err)
	}
}
