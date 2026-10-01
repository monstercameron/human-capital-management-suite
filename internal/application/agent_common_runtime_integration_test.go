package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type commonAgentExactSource struct{ request agentrun.Request }

type commonAgentTestModelPolicy struct{ revoked *bool }

func (p commonAgentTestModelPolicy) CheckCurrentModelPolicy(_ context.Context, _ agentrun.Request, reference agentmanifest.Reference) error {
	if p.revoked != nil && *p.revoked || reference.ID != "reference-a" || reference.Version != 1 || reference.SchemaVersion != 1 || !personaRunAuthorityDigest(reference.Digest) {
		return commonAgentRefusal("MODEL_POLICY_DENIED")
	}
	return nil
}

func (s commonAgentExactSource) CheckRequest(_ context.Context, request agentrun.Request) error {
	if request.Source != s.request.Source || request.CauseID != s.request.CauseID || request.Agent != s.request.Agent || request.Context != s.request.Context ||
		request.Principal != s.request.Principal || request.Purpose != s.request.Purpose || request.Audience != s.request.Audience || request.Budget != s.request.Budget || !request.Deadline.Equal(s.request.Deadline) {
		return commonAgentRefusal("SOURCE_CHANGED")
	}
	return nil
}

func TestTodo_AGENT_011_CommonAuthority_Integration(t *testing.T) {
	ctx := context.Background()
	core := pgtest.New(t)
	agentDB := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agentDB.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'common-tenant','cell-local','Common','ACTIVE',CURRENT_TIMESTAMP)`, tenant)
	core.Exec(t, `INSERT INTO tenant_agent_setting(tenant_id,enabled,revision,updated_by) VALUES($1,true,1,'test-admin')`, tenant)
	agentDB.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant)
	agents := commonAgentOpenIntegrationStore(t, agentDB)
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	request := commonAgentTestRequest(now)
	request.Principal.AgentPrincipalID = uuid.NewString()
	request.Principal.SponsorID = uuid.NewString()
	manifest := commonAgentIntegrationManifest(t, agents, tenant, request)
	request.Agent.Digest, _ = manifest.Digest()
	request.Source, _ = (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
	scope := CommonAgentBindingScope{SchemaVersion: 1, TenantID: request.Source.TenantID, Agent: request.Agent, InstallationID: request.InstallationID,
		AgentPrincipal: request.Principal.AgentPrincipalID, SponsorID: request.Principal.SponsorID, LegalEntity: request.LegalEntity, Purpose: request.Purpose,
		Audience: request.Audience, Context: request.Context, Sources: []agentrun.SourceKind{request.Source.Kind}, BudgetCeiling: request.Budget, Deadline: request.Deadline}
	raw, _ := json.Marshal(scope)
	source := uuid.New()
	core.Exec(t, `INSERT INTO authority_source(tenant_id,authority_source_id,kind,display_name,uri,valid_interval,content_digest)
		VALUES($1,$2,'POLICY_BUNDLE','Agent authority','policy:agent-current',tstzrange($3,$4,'[)'),$5)`, tenant, source, now.Add(-time.Hour), now.Add(2*time.Hour), strings.TrimPrefix(request.Agent.Digest, "sha256:"))
	for _, principal := range []string{request.Principal.AgentPrincipalID, request.Principal.SponsorID} {
		core.Exec(t, `INSERT INTO principal(tenant_id,principal_id,kind,subject,assurance,authn_method) VALUES($1,$2,'SERVICE',$3,'AAL1','workload')`, tenant, principal, "service:"+principal)
		core.Exec(t, `INSERT INTO authority_binding(tenant_id,binding_id,principal_id,authority_source_id,scope,valid_from,valid_to) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7)`, tenant, uuid.New(), principal, source, string(raw), now.Add(-time.Hour), now.Add(2*time.Hour))
	}
	coreConn := core.NewConn(t)
	if _, err := coreConn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	mapper := func(ref values.TenantId) uuid.UUID {
		if ref == "common-tenant" {
			return tenant
		}
		return uuid.Nil
	}
	modelRevoked := false
	modelPolicy := commonAgentTestModelPolicy{revoked: &modelRevoked}
	authority, err := NewCommonAgentAuthority(CommonAgentAuthorityConfig{CoreDB: coreConn, Agents: agents, TenantUUID: mapper,
		Sources: map[agentrun.SourceKind]CommonAgentSourceAuthority{request.Source.Kind: commonAgentExactSource{request}}, Models: modelPolicy, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := authority.VerifyAdmission(ctx, request)
	if err != nil || snapshot.Principal != request.Principal || snapshot.GrantRef == "" || snapshot.BudgetCeiling != request.Budget {
		t.Fatalf("current database authority = %+v %v", snapshot, err)
	}
	configuration := CommonAgentRuntimeConfig{Stores: DatabaseCommonAgentStores{Agents: agents, TenantUUID: mapper}, Authority: authority, Now: func() time.Time { return now },
		ExecutionFences: map[agentrun.SourceKind]CommonAgentExecutionFence{agentrun.SourceSchedule: commonAgentTestExecutionFence{}}}
	runtime, err := NewCommonAgentRuntime(configuration)
	if err != nil {
		t.Fatal(err)
	}
	record, created, err := runtime.Admit(ctx, request)
	if err != nil || !created || record.Decision != agentrun.DecisionAccepted {
		t.Fatalf("durable actual admission = %+v %v %v", record, created, err)
	}
	// Recompose both the repositories and runtime, so subsequent operations
	// can only obtain the acceptance and work state from PostgreSQL.
	restarted, err := NewCommonAgentRuntime(configuration)
	if err != nil {
		t.Fatal(err)
	}
	run, err := restarted.GetRun(ctx, "common-tenant", record.ID)
	if err != nil || run.State != runstate.StateReady || run.PrincipalMode != agentrun.ModeSponsored || run.ActorID != request.Principal.SponsorID {
		t.Fatalf("restart ready = %+v %v", run, err)
	}
	pending, err := restarted.Pending(ctx, "common-tenant", request.Source.Kind, 100)
	if err != nil || len(pending) != 1 || pending[0].ID != record.ID {
		t.Fatalf("durable pending scan = %+v %v", pending, err)
	}
	if _, created, err := restarted.Admit(ctx, request); err != nil || created {
		t.Fatalf("durable replay = %v %v", created, err)
	}
	claimed, err := restarted.Claim(ctx, "common-tenant", record.ID, "worker-1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	recovered, err := restarted.Recover(ctx, "common-tenant", record.ID)
	if err != nil || recovered.State != runstate.StateReady || recovered.Lease != nil || recovered.Fence != claimed.Fence {
		t.Fatalf("durable recovery = %+v %v", recovered, err)
	}
	modelRevoked = true
	if _, err := restarted.Claim(ctx, "common-tenant", record.ID, "worker-model-revoked", time.Minute); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("revoked model policy executed = %v", err)
	}
	modelRevoked = false
	core.Exec(t, `UPDATE tenant_agent_setting SET enabled=false,revision=revision+1,updated_by='test-admin' WHERE tenant_id=$1`, tenant)
	if _, err := restarted.Claim(ctx, "common-tenant", record.ID, "worker-disabled", time.Minute); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("disabled tenant executed = %v", err)
	}
	core.Exec(t, `UPDATE tenant_agent_setting SET enabled=true,revision=revision+1,updated_by='test-admin' WHERE tenant_id=$1`, tenant)
	core.Exec(t, `UPDATE principal SET lifecycle='REVOKED',revocation_epoch=revocation_epoch+1 WHERE tenant_id=$1 AND principal_id=$2`, tenant, request.Principal.SponsorID)
	if _, err := restarted.Claim(ctx, "common-tenant", record.ID, "worker-2", time.Minute); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("revoked actual sponsor executed = %v", err)
	}
	if _, err := restarted.GetRun(ctx, "foreign-tenant", record.ID); err == nil {
		t.Fatal("foreign tenant read accepted")
	}
}

func commonAgentIntegrationManifest(t *testing.T, store *agentstore.Store, tenant uuid.UUID, request agentrun.Request) agentmanifest.Manifest {
	t.Helper()
	instructions, err := store.SaveInstructionContent(context.Background(), tenant, "Analyze only the governed source context.")
	if err != nil {
		t.Fatal(err)
	}
	ref := agentmanifest.Reference{ID: "reference-a", Version: 1, SchemaVersion: 1, Digest: request.Agent.Digest}
	manifest := agentmanifest.Manifest{SchemaVersion: 1, ID: request.Agent.AgentID, Version: 1, OwnerID: "owner-a", Purpose: request.Purpose, InstructionsDigest: instructions,
		SourceCeiling: []agentmanifest.Reference{ref}, ToolCeiling: []agentmanifest.Reference{ref}, ModelPolicy: ref, AutonomyCeiling: "T2", Budget: agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 200, MaxOutputTokens: 100, MaxConcurrentRuns: 1},
		OutputSchema: ref, EvaluationRefs: []agentmanifest.Reference{ref}}
	if _, err := store.SaveManifest(context.Background(), tenant, manifest, 0); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func commonAgentOpenIntegrationStore(t *testing.T, db *pgtest.DB) *agentstore.Store {
	t.Helper()
	login, password := "common_agent_"+strings.ReplaceAll(uuid.NewString(), "-", ""), uuid.NewString()
	db.Exec(t, fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD '%s'", login, password))
	db.Exec(t, "GRANT "+agentstore.AppRole+" TO "+login)
	t.Cleanup(func() { db.Exec(t, "DROP ROLE "+login) })
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(login, password)
	query := u.Query()
	query.Set("search_path", db.Schema)
	u.RawQuery = query.Encode()
	core := *u
	core.User = url.UserPassword("core-login", "different-secret")
	core.Path = "/other-core-database"
	store, err := agentstore.New(context.Background(), agentstore.Config{DSN: u.String(), CoreDSN: core.String(), MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}
