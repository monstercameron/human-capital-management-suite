package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentUXOps4RunSource struct {
	rows      []AgentOwnerRunProjection
	allowed   ownerops.Audience
	err       error
	audiences []ownerops.Audience
}

func (s *agentUXOps4RunSource) DashboardProjection(_ context.Context, _ *trust.Principal, audience ownerops.Audience) ([]AgentOwnerRunProjection, error) {
	s.audiences = append(s.audiences, audience)
	if s.err != nil {
		return nil, s.err
	}
	if s.allowed != "" && audience != s.allowed {
		return nil, ownerops.ErrDenied
	}
	return s.rows, nil
}

func TestAgentUXOps4_CombinedRunSourceProjectsFinishedPersonaRuns(t *testing.T) {
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	native := &agentUXOps4RunSource{err: ownerops.ErrDenied}
	persona := &agentUXOps4RunSource{allowed: ownerops.AudienceOperator, rows: []AgentOwnerRunProjection{{
		View: ownerops.TaskView{
			TaskID:         "persona-run-failed",
			AgentID:        "policy-helper",
			Version:        "2",
			InstallationID: "install-policy",
			State:          string(runstate.StateFailed),
			FailureCode:    "TOOL_EXECUTION_FAILED",
			Revision:       8,
		},
		RequestedBy:     "Walt Brennan",
		Location:        "#benefits",
		FailureGate:     string(runstate.FailureGateToolScope),
		FailureOwner:    "internal/application",
		FailureLocation: "persona_runtime_tools.go:37",
		StartedAt:       now.Add(-45 * time.Second),
		UpdatedAt:       now,
	}}}
	service := &AgentOwnerControls{
		Runs:       AgentCombinedRunSource{Sources: []AgentOwnerRunSource{native, persona}},
		Identities: &agentUXOps2IdentitySource{identity: AgentControlIdentity{Name: "Policy Helper", OwnerName: "Maya Chen"}},
	}
	reply, err := service.Snapshot(trust.WithPrincipal(context.Background(), portableApplicationPrincipal(t)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Snapshot.Runs) != 1 {
		t.Fatalf("runs = %+v", reply.Snapshot.Runs)
	}
	run := reply.Snapshot.Runs[0]
	if run.Name != "Policy Helper" || run.RequestedBy != "Walt Brennan" || run.Location != "#benefits" || run.Duration != "45s" || run.FailureGate != string(runstate.FailureGateToolScope) || run.FailurePlace != "persona_runtime_tools.go:37" {
		t.Fatalf("finished persona projection = %+v", run)
	}
	if len(native.audiences) == 0 || len(persona.audiences) == 0 {
		t.Fatalf("combined source did not query both native and persona sources: native=%v persona=%v", native.audiences, persona.audiences)
	}
}

func TestAgentUXOps4_FailureClassifierStoresClosedVocabulary(t *testing.T) {
	cause := fmt.Errorf("%w: request body document body model output", personaRuntimeToolDeniedHere())
	refusal := personaRunFailureRefusal("TOOL_EXECUTION_FAILED", cause)
	if refusal.Gate != runstate.FailureGateToolScope || refusal.Owner != "internal/application" || !regexp.MustCompile(`^[A-Za-z0-9_]+\.go:[0-9]+$`).MatchString(refusal.Location) {
		t.Fatalf("refusal = %+v", refusal)
	}
	for _, value := range []string{string(refusal.Gate), refusal.Owner, refusal.Location} {
		for _, forbidden := range []string{"request body", "document body", "model output"} {
			if strings.Contains(value, forbidden) {
				t.Fatalf("refusal leaked %q in %q", forbidden, value)
			}
		}
	}
	if got := personaRunFailureRefusal("DELIVERY_FAILED", errors.New("document body")); got.Gate != runstate.FailureGateDeliveryWrite {
		t.Fatalf("delivery gate = %+v", got)
	}
}

type agentUXOps4RolloutStore struct {
	*versionRolloutFake
	identity agentpersonastore.PersonaChatIdentity
}

func TestAgentUXOps4_RolloutDirectConversationUsesAgentIdentityName(t *testing.T) {
	ctx, _, store, _ := versionRolloutFixture(t)
	principal, _ := trust.FromContext(ctx)
	store.row.DisplayName = "Policy Helper"
	wrapped := &agentUXOps4RolloutStore{versionRolloutFake: store, identity: agentpersonastore.PersonaChatIdentity{TenantID: principal.Tenant(), AgentID: "agent:policy-helper", PersonaID: "persona", Active: true}}
	label := rolloutAgentChatIdentityLabel(ctx, wrapped, chat.Membership{HomeTenantID: principal.Tenant().String(), SubjectID: "agent:policy-helper"})
	if label != "Policy Helper" {
		t.Fatalf("agent identity label = %q", label)
	}
}

func TestAgentUXR6Ops_PersonaRunDashboardProjectionIntegration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := values.TenantId("tenant-a")
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
	agentURL, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := agentURL.Query()
	query.Set("search_path", db.Schema)
	agentURL.RawQuery = query.Encode()
	agents, err := agentstore.New(ctx, agentstore.Config{DSN: agentURL.String(), CoreDSN: "postgres://core@127.0.0.1:1/core", MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agents.Close)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := agents.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		createdAt := now.Add(-2 * time.Hour)
		deadline := now.Add(-time.Hour)
		leaseUntil := now.Add(-90 * time.Minute)
		updatedAt := now.Add(-45 * time.Minute)
		inserts := []struct {
			statement string
			args      []any
		}{
			{`INSERT INTO persona_versions(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES($1,'policy-helper',6,'agent.policy@6','policy-helper','Policy Helper','{}','sha256:` + strings.Repeat("a", 64) + `',$2)`, []any{tenantID, createdAt}},
			{`INSERT INTO persona_owners(tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES($1,'policy-helper','BUSINESS_OWNER','owner-a','system',$2)`, []any{tenantID, createdAt}},
			{`INSERT INTO persona_invocations(tenant_id,invocation_id,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version,installation_id,mode,skills,actor,owner_id,admission_id,run_id,task_id,context_digest,state,created_at,started_at) VALUES($1,'invoke-a','direct-a','thread-a','post-a','walt','policy-helper','6','install-a','ON_BEHALF_OF','{}','{}','owner-a','request-a','run-a','task-a','sha256:` + strings.Repeat("b", 64) + `','STARTED',$2,$2)`, []any{tenantID, createdAt}},
			{`INSERT INTO agent_run_request(tenant_id,request_id,source_kind,source_key_digest,source_ref,request_digest,request_payload,decision,authority_snapshot,admitted_at,deadline,agent_id,agent_version,agent_digest,installation_id,principal_chain,purpose,audience,context_scope,budget,cause_id) VALUES($1,'request-a','PERSONA_MENTION','` + strings.Repeat("c", 64) + `','post-a','` + strings.Repeat("d", 64) + `','{}','ACCEPTED','{}',$2,$3,'policy-helper','6','sha256:` + strings.Repeat("e", 64) + `','install-a','{"mode":"ON_BEHALF_OF"}','answer policy','[]','[]','{}','invoke-a')`, []any{tenantID, createdAt, deadline}},
			{`INSERT INTO agent_run_execution(tenant_id,run_id,admission_id,request_digest,agent_id,agent_version,agent_digest,context_digest,deadline,state,revision,fence,lease_owner,lease_until,created_at,updated_at) VALUES($1,'run-a','request-a','` + strings.Repeat("d", 64) + `','policy-helper','6','sha256:` + strings.Repeat("e", 64) + `','sha256:` + strings.Repeat("b", 64) + `',$2,'RUNNING',4,1,'worker-a',$3,$4,$5)`, []any{tenantID, deadline, leaseUntil, createdAt, updatedAt}},
		}
		for _, insert := range inserts {
			if _, err := tx.Exec(ctx, insert.statement, insert.args...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source := AgentPersonaRunSource{
		DB: agents,
		TenantUUID: func(value values.TenantId) uuid.UUID {
			if value == tenant {
				return tenantID
			}
			return uuid.Nil
		},
		Directory: agentUXR6OpsDirectory{"owner-a": "Maya Chen", "walt": "Walt Brennan", "stranger": "Una Stranger"},
	}
	ownerRows, err := source.DashboardProjection(ctx, agentUXR6OpsPrincipal(t, tenant, "owner-a"), ownerops.AudienceOwner)
	if err != nil {
		t.Fatal(err)
	}
	invokerRows, err := source.DashboardProjection(ctx, agentUXR6OpsPrincipal(t, tenant, "walt"), ownerops.AudienceMember)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.DashboardProjection(ctx, agentUXR6OpsPrincipal(t, tenant, "stranger"), ownerops.AudienceOwner); !errors.Is(err, ownerops.ErrDenied) {
		t.Fatalf("stranger error = %v", err)
	}
	for _, rows := range [][]AgentOwnerRunProjection{ownerRows, invokerRows} {
		if len(rows) != 1 || rows[0].View.TaskID != "run-a" || rows[0].RequestedBy != "Walt Brennan" || !agentRunStoppedResponding(rows[0].View.State, rows[0].Deadline, rows[0].LeaseUntil) {
			t.Fatalf("projection rows = %+v", rows)
		}
	}
}

type agentUXR6OpsDirectory map[string]string

func (d agentUXR6OpsDirectory) ResolvePersonaCatalogTarget(_ context.Context, _ values.TenantId, id string) (productui.PersonaAdminTarget, error) {
	return productui.PersonaAdminTarget{ID: id, Label: d[id]}, nil
}

func agentUXR6OpsPrincipal(t *testing.T, tenant values.TenantId, subject string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "r6-" + subject, IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "r6-" + subject})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (s *agentUXOps4RolloutStore) LookupPersonaChatIdentity(context.Context, string) (agentpersonastore.PersonaChatIdentity, error) {
	return s.identity, nil
}
