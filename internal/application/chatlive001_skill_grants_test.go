package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentskillgrantstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_CHATLIVE_001_AssistantPinnedSkillGrantedByPreparation reproduces the
// stored shape of the review cell: Policy Helper's two skill grants exist,
// Assistant version 2 pins a third skill (workspace search) nobody granted, so
// discovery returns two of three pinned skills and the directory drops the
// persona. The preparation must grant the missing skill, once.
func TestTodo_CHATLIVE_001_AssistantPinnedSkillGrantedByPreparation(t *testing.T) {
	const tenant = values.TenantId("ironridge-demo")
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, string(tenant), string(tenant))
	mapper := func(values.TenantId) uuid.UUID { return tenantID }
	roles := []string{"comp_admin", "hcm_admin", "intent_author", "promotion_operator"}
	scope := "org:ironridge-demo:executive"

	definitions := []agentskills.SkillDefinition{personaChatReplySkillDefinition(), personaPolicySearchSkillDefinition(), personaWorkspaceSearchSkillDefinition()}
	var records []agentskills.SkillRecord
	var pins []agentskills.SkillPin
	for _, definition := range definitions {
		record := agentskills.SkillRecord{Definition: definition, Digest: "sha256:" + definition.ID, Status: agentskills.StatusActive}
		records = append(records, record)
		pins = append(pins, agentskills.SkillPin{ID: definition.ID, Version: definition.Version, Digest: record.Digest})
	}
	// Policy Helper's bootstrap granted exactly its own two pins.
	for _, pin := range pins[:2] {
		db.Exec(t, `INSERT INTO agent_skill_grant(tenant_id,grant_id,skill_id,skill_version,roles,population,organization_scopes,purposes,not_before,granted_by,granted_at,admin_evidence_ref) VALUES($1,$2,$3,$4,$5,'employees',ARRAY[$6],ARRAY['persona-mention','persona_admin_preview'],now()-interval '1 day','ir-001-walt-brennan',now()-interval '1 day','local-demo/persona-policy-helper/v1/x')`,
			tenantID, "local-demo/policy-helper/"+pin.ID+"/v1", pin.ID, int64(pin.Version), roles, scope)
	}
	assistant := agentpersona.PersonaProfile{PersonaID: localAgentDemoAssistantPersonaID, Version: 2, Handle: "assistant", DisplayName: "Assistant", Owner: "ir-001-walt-brennan", SkillPins: pins,
		Audience: agentpersona.Audience{Roles: roles, Populations: []string{"employees"}, OrganizationScopes: []string{scope}}}

	now := time.Now().UTC().Truncate(time.Microsecond)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: "ir-001-walt-brennan", SubjectKind: trust.SubjectKindHuman, Roles: roles, Purposes: []string{personaChatReplyPurpose}, OrganizationScopeID: scope,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "chatlive-001", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:chatlive-001"})
	if err != nil {
		t.Fatal(err)
	}
	app := db.NewConn(t)
	if _, err := app.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	root, err := agentskillgrantstore.New(app, mapper)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := root.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewAgentSkillSource(sourceSkillCatalog{records: records}, grants, sourceCurrent{user: agentgate.UserContext{Principal: principal, Population: "employees", Roles: roles, OrganizationScopes: []string{scope}}})
	if err != nil {
		t.Fatal(err)
	}
	discover := func() []agentskills.SkillRecord {
		t.Helper()
		got, err := source.Discover(trust.WithPrincipal(context.Background(), principal), principal, personaChatReplyPurpose)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := discover(); len(got) != 2 || hasExactPinnedSkills(pins, got) {
		t.Fatalf("stored shape did not reproduce the defect: discovered %d skills, directory would list Assistant=%t", len(got), hasExactPinnedSkills(pins, got))
	}
	owner := db.NewConn(t)
	created, err := ensureLocalAgentDemoSkillGrants(context.Background(), owner, mapper, tenant, assistant, now)
	if err != nil || created != 1 {
		t.Fatalf("preparation granted %d skills, err=%v; want exactly the workspace search skill", created, err)
	}
	if got := discover(); len(got) != 3 || !hasExactPinnedSkills(pins, got) {
		t.Fatalf("after preparation discovered %d skills; Assistant would still be dropped from the directory", len(got))
	}
	again, err := ensureLocalAgentDemoSkillGrants(context.Background(), owner, mapper, tenant, assistant, now.Add(time.Minute))
	if err != nil || again != 0 {
		t.Fatalf("rerun granted %d skills, err=%v; the preparation must be idempotent", again, err)
	}

	// A grant that exists for a skill but does not cover the audience is
	// refused, never rewritten.
	narrow := assistant
	narrow.Audience = agentpersona.Audience{Roles: append(append([]string{}, roles...), "other_role"), Populations: []string{"employees"}, OrganizationScopes: []string{scope}}
	if _, err := ensureLocalAgentDemoSkillGrants(context.Background(), owner, mapper, tenant, narrow, now); err == nil {
		t.Fatal("a wider audience than the existing grants was accepted silently")
	}
}
