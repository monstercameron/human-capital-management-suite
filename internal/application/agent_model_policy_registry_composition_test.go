package application

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentportable"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT_021_ImmutablePolicyComposition(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := composeLocalAgentModelPolicyRegistry(ctx, agentModelPolicyRegistryCompositionInput{}); !errors.Is(err, ErrAgentModelPolicyUnavailable) {
		t.Fatalf("unconfigured composition=%v", err)
	}
	core, agent := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
	if _, err := core.SQL.Exec(`CREATE TABLE tenant(tenant_id uuid PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := agentstore.Migrate(ctx, agent.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := pgstore.TenantID("harborcare-demo")
	if _, err := core.SQL.Exec(`INSERT INTO tenant(tenant_id) VALUES($1)`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.SQL.Exec(`INSERT INTO tenant(tenant_id) VALUES($1)`, tenant); err != nil {
		t.Fatal(err)
	}
	loginDSN := func(prefix, role string) string {
		login := prefix + uuid.NewString()[:8]
		password := uuid.NewString()
		if _, err := agent.SQL.Exec(`CREATE ROLE ` + login + ` LOGIN PASSWORD '` + password + `' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS`); err != nil {
			t.Fatal(err)
		}
		if _, err := agent.SQL.Exec(`GRANT ` + role + ` TO ` + login); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := agent.SQL.Exec(`DROP ROLE ` + login); err != nil {
				t.Error(err)
			}
		})
		u, _ := url.Parse(agent.URL)
		u.User = url.UserPassword(login, password)
		q := u.Query()
		q.Set("search_path", agent.Schema)
		u.RawQuery = q.Encode()
		return u.String()
	}
	appDSN := loginDSN("policy_app_", agentstore.AppRole)
	publisherDSN := loginDSN("policy_pub_", agentmodelpolicystore.AuthorityRole)
	coreIdentity, _ := url.Parse(core.URL)
	coreIdentity.Path = "/isolated_core"
	coreIdentity.User = url.UserPassword("policy_core_reader", "comparison-only")
	store, err := agentstore.New(ctx, agentstore.Config{DSN: appDSN, CoreDSN: coreIdentity.String(), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	material := localOpenAIMaterialFixture()
	material.PolicySeed = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SeedSize))
	input := agentModelPolicyRegistryCompositionInput{Config: ServeConfig{Profile: ServeProfileLocalDev, Tenant: "harborcare-demo", AgentDatabaseURL: appDSN, DatabaseURL: coreIdentity.String()}, Core: core.Conn, Agents: composedAgentDatabase{store: store}, Material: material, PublisherDSN: publisherDSN, CurrentDeployment: PersonaModelDeploymentPolicyAuthority{Routes: store, Deployment: func(context.Context, values.TenantId) (PersonaModelDeployment, error) {
		return PersonaModelDeployment{}, ErrAgentModelPolicyUnavailable
	}, TenantUUID: func(t values.TenantId) uuid.UUID { return pgstore.TenantID(string(t)) }, Now: func() time.Time { return now }}, Now: func() time.Time { return now }}
	composed, err := composeLocalAgentModelPolicyRegistry(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	composed.close()
	// Reconstruct with the original dedicated key and DB login. No authority or
	// evaluator fact is reissued merely because the process restarted.
	restarted, err := composeLocalAgentModelPolicyRegistry(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.close()
	refs := restarted.Registry.ForTenant("harborcare-demo", restarted.Selection)
	model, err := refs.ResolveModelPolicy(ctx)
	if err != nil || model != restarted.Selection.ModelPolicy {
		t.Fatalf("model reference=%+v %v", model, err)
	}
	if schema, err := refs.ResolveOutputSchema(ctx); err != nil || schema != restarted.Selection.OutputSchema {
		t.Fatalf("schema reference=%+v %v", schema, err)
	}
	for id, expected := range restarted.Selection.EvaluationSuites {
		if ref, err := refs.ResolveEvaluationSuite(ctx, id); err != nil || ref != expected {
			t.Fatalf("evaluation ref=%+v %v", ref, err)
		}
	}
	if _, err := refs.ResolveEvaluationSuite(ctx, "unregistered"); err == nil {
		t.Fatal("unregistered evaluation resolved")
	}
	raw, err := restarted.Registry.ResolveTypedReference(ctx, "harborcare-demo", agentportable.ModelPolicyReference, model)
	if err != nil || string(raw) != string(LocalPersonaOpenAIPolicyRecords()[0].Content) {
		t.Fatalf("registered raw policy differs %v", err)
	}
	if _, err := restarted.References.ResolveModelPolicy(ctx); err == nil {
		t.Fatal("principal-aware source accepted unauthenticated tenant")
	}
	if err := restarted.Registry.CheckCurrentModelPolicy(ctx, agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "harborcare-demo"}, Deadline: now.Add(time.Minute)}, model); err == nil {
		t.Fatal("semantic contract alone granted production model eligibility")
	}
	var records, authorities, profiles int
	if err := agent.SQL.QueryRow(`SELECT (SELECT count(*) FROM agent_immutable_contract),(SELECT count(*) FROM agent_contract_authority),(SELECT count(*) FROM persona_model_route_policy)`).Scan(&records, &authorities, &profiles); err != nil {
		t.Fatal(err)
	}
	if records != 3 || authorities != 3 || profiles != 0 {
		t.Fatalf("bootstrap manufactured routing or reissued authority: records=%d authorities=%d routes=%d", records, authorities, profiles)
	}
	if _, err := core.SQL.Exec(`DELETE FROM tenant WHERE tenant_id=$1`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := refs.ResolveModelPolicy(ctx); err == nil {
		t.Fatal("removed actual tenant source retained authority")
	}
}
