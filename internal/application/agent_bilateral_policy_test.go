package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/bilateral"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/pressly/goose/v3"
)

type agentBilateralCurrentTestDelegate struct{}

func (agentBilateralCurrentTestDelegate) CheckCurrent(context.Context, agentrun.Request) error {
	return nil
}

func TestTodo_AGENT_037_Integration(t *testing.T) {
	ctx := context.Background()
	core, chat := pgtest.New(t), pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, chat.SQL, migrationFS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	host, home := uuid.New(), uuid.New()
	request := commonAgentTestRequest(now)
	request.Source.TenantID = "host"
	request.Audience.ID = "shared-room"
	request.Principal.AgentPrincipalID = uuid.NewString()
	request.Principal.SponsorID = uuid.NewString()
	for name, id := range map[string]uuid.UUID{"host": host, "home": home} {
		core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,$2,'cell-local',$3,'ACTIVE',now())`, id, name, name)
	}
	chat.Exec(t, `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES('shared-room','host','CHANNEL','Shared','owner')`)
	chat.Exec(t, `INSERT INTO chat_membership(tenant_id,conversation_id,member_id,home_tenant_id,joined_at) VALUES('host','shared-room','host-user','host',$1),('host','shared-room','home-user','home',$1)`, now.Add(-time.Hour))
	chat.Exec(t, `INSERT INTO chat_share_grant(id,tenant_id,conversation_id,consumer_tenant,version,scope,classification,residency,proposed_by,proposed_at,accepted_by,accepted_at,expires_at) VALUES('share-a','host','shared-room','home',2,'conversation','INTERNAL','us-east','host-admin',$1,'home-admin',$1,$2)`, now.Add(-time.Hour), now.Add(time.Hour))
	membership := []any{[]any{"home", "home-user", uint64(1), "active", ""}, []any{"host", "host-user", uint64(1), "active", ""}}
	raw, _ := json.Marshal(membership)
	sum := sha256.Sum256(raw)
	scope := AgentBilateralApprovalScope{SchemaVersion: 1, Kind: "AGENT_BILATERAL_APPROVAL", AgentPrincipalID: request.Principal.AgentPrincipalID, SponsorID: request.Principal.SponsorID, GrantID: "share-a", GrantVersion: 2, MembershipDigest: "sha256:" + hex.EncodeToString(sum[:]),
		Terms: bilateral.Terms{ConversationID: "shared-room", HostTenant: "host", ConsumerTenant: "home", InstallationID: request.InstallationID, AgentID: request.Agent.AgentID, AgentVersion: request.Agent.Version, Purpose: request.Purpose, ProcessingRegion: "us-east", Retention: "30-days", Egress: []string{"chat", "agent-output"}, IncidentContact: "operations", ExitBehavior: "suspend", ExpiresAt: now.Add(time.Hour)}}
	raw, _ = json.Marshal(scope)
	var homeBinding uuid.UUID
	for _, company := range []uuid.UUID{host, home} {
		principal, source, binding := uuid.New(), uuid.New(), uuid.New()
		if company == home {
			homeBinding = binding
		}
		core.Exec(t, `INSERT INTO principal(tenant_id,principal_id,kind,subject,assurance,authn_method) VALUES($1,$2,'SERVICE',$3,'AAL1','workload')`, company, principal, "policy:"+principal.String())
		core.Exec(t, `INSERT INTO authority_source(tenant_id,authority_source_id,kind,display_name,uri,valid_interval,content_digest) VALUES($1,$2,'POLICY_BUNDLE','Bilateral approval','policy:bilateral',tstzrange($3,$4,'[)'),$5)`, company, source, now.Add(-time.Hour), now.Add(time.Hour), strings.TrimPrefix(request.Agent.Digest, "sha256:"))
		core.Exec(t, `INSERT INTO authority_binding(tenant_id,binding_id,principal_id,authority_source_id,scope,valid_from,valid_to) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7)`, company, binding, principal, source, string(raw), now.Add(-time.Hour), now.Add(time.Hour))
	}
	policy := DatabaseAgentBilateralPolicy{CoreDB: appRoleConnForPopulation(t, core), ChatDB: chat.NewConn(t), TenantUUID: func(key values.TenantId) uuid.UUID {
		if key == "host" {
			return host
		}
		if key == "home" {
			return home
		}
		return uuid.Nil
	}, Now: func() time.Time { return now }}
	proof, err := policy.CheckAgentBilateral(ctx, request)
	if err != nil || !personaRunAuthorityDigest(proof) {
		t.Fatalf("actual bilateral policy = %s %v", proof, err)
	}
	wrapped := AgentBilateralAuthority{Delegate: &commonAgentTestAuthority{}, Policy: policy}
	snapshot, err := wrapped.VerifyAdmission(ctx, request)
	if err != nil || snapshot.Principal != request.Principal || !personaRunAuthorityDigest(snapshot.PolicyDigest) || snapshot.PolicyDigest == ("sha256:"+strings.Repeat("b", 64)) {
		t.Fatalf("bilateral authority intersection = %+v %v", snapshot, err)
	}
	current := AgentBilateralBindingPolicy{Delegate: agentBilateralCurrentTestDelegate{}, Policy: policy}
	if err := current.CheckCurrent(ctx, request); err != nil {
		t.Fatalf("installation/publication bilateral gate = %v", err)
	}
	for name, mutate := range map[string]func(*agentrun.Request){
		"version":      func(r *agentrun.Request) { r.Agent.Version = "other-version" },
		"installation": func(r *agentrun.Request) { r.InstallationID = "other-installation" },
		"purpose":      func(r *agentrun.Request) { r.Purpose = "other-purpose" },
		"sponsor":      func(r *agentrun.Request) { r.Principal.SponsorID = uuid.NewString() },
	} {
		t.Run(name, func(t *testing.T) {
			changed := request
			mutate(&changed)
			if _, err := policy.CheckAgentBilateral(ctx, changed); !errors.Is(err, bilateral.ErrDenied) {
				t.Fatalf("company approvals reused for different %s: %v", name, err)
			}
		})
	}
	core.Exec(t, `UPDATE authority_binding SET valid_to=$3 WHERE tenant_id=$1 AND binding_id=$2`, home, homeBinding, now)
	if _, err = policy.CheckAgentBilateral(ctx, request); !errors.Is(err, bilateral.ErrDenied) {
		t.Fatalf("unilateral approval accepted: %v", err)
	}
	if err := current.CheckCurrent(ctx, request); !errors.Is(err, bilateral.ErrDenied) {
		t.Fatalf("native installation/publication skipped bilateral = %v", err)
	}
	core.Exec(t, `UPDATE authority_binding SET valid_to=$3 WHERE tenant_id=$1 AND binding_id=$2`, home, homeBinding, now.Add(time.Hour))
	chat.Exec(t, `UPDATE chat_membership SET revision=revision+1 WHERE tenant_id='host' AND conversation_id='shared-room' AND member_id='home-user'`)
	if _, err = policy.CheckAgentBilateral(ctx, request); !errors.Is(err, bilateral.ErrDenied) {
		t.Fatalf("membership changed before reply: %v", err)
	}
	chat.Exec(t, `UPDATE chat_membership SET revision=1 WHERE tenant_id='host' AND conversation_id='shared-room' AND member_id='home-user'`)
	chat.Exec(t, `UPDATE chat_share_grant SET revoked_at=$1,revoked_by='host-admin' WHERE id='share-a'`, now)
	if _, err = policy.CheckAgentBilateral(ctx, request); !errors.Is(err, bilateral.ErrDenied) {
		t.Fatalf("revoked share accepted: %v", err)
	}
}

func TestTodo_AGENT_037_Security(t *testing.T) {
	request := commonAgentTestRequest(time.Now().UTC())
	if _, err := (DatabaseAgentBilateralPolicy{}).CheckAgentBilateral(context.Background(), request); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatal(err)
	}
	if _, err := (AgentBilateralAuthority{}).VerifyAdmission(context.Background(), request); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatal(err)
	}
	if err := (AgentBilateralBindingPolicy{}).CheckCurrent(context.Background(), request); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatal(err)
	}
}
