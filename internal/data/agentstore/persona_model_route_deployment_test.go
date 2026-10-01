package agentstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_021_IntegrationIndependentManifestDeployments(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	if _, err := db.SQL.Exec(`INSERT INTO tenant(tenant_id) VALUES($1),($2)`, tenant, other); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("model_deployment"), uuid.NewString()
	if _, err := db.SQL.Exec(fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '%s'", login, password)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec("GRANT hcmnext_persona_model_route_authority,hcmnext_agent_app TO " + login); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.SQL.Exec("DROP ROLE " + login); err != nil {
			t.Error(err)
		}
	})
	dsn := testDSN(t, db.URL, db.Schema, login, password, "postgres")
	writer, err := pgxadapter.NewPool(ctx, dsn, map[string]string{"role": "hcmnext_persona_model_route_authority"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	reader, err := New(ctx, Config{DSN: dsn, CoreDSN: "postgres://core:pw@127.0.0.1:5433/core", MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	from := time.Now().UTC().Truncate(time.Second)
	manifestA := "sha256:" + strings.Repeat("e", 64)
	manifestB := "sha256:" + strings.Repeat("f", 64)
	var semanticDigest string
	for index, manifest := range []string{manifestA, manifestB} {
		modelDigest := "sha256:" + strings.Repeat(string(rune('a'+index)), 64)
		in := validPublication("sha256:"+strings.Repeat("b", 64), modelDigest)
		in.EffectiveFrom = from
		in.RoutePayload = []byte(strings.ReplaceAll(string(in.RoutePayload), manifestA, manifest))
		semanticDigest = in.PolicyDigest
		q := PersonaRouteQualification{RunID: in.EvaluationRunID, TenantID: string(in.Tenant), PersonaID: in.PersonaID, PersonaVersion: in.PersonaVersion, ProfileDigest: in.ProfileDigest, ModelDigest: modelDigest, SuiteDigest: "sha256:" + strings.Repeat("c", 64), RunDigest: "sha256:" + strings.Repeat("d", 64), Passed: true, Fresh: true, ExpiresAt: from.Add(time.Hour)}
		publisher, err := NewPersonaModelRoutePublisher(writer, func(values.TenantId) uuid.UUID { return tenant }, qualificationFixture{value: q})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := publisher.PublishPersonaModelRoutePolicy(ctx, in); err != nil {
			t.Fatalf("publish manifest %s with shared semantic reference/revision1: %v", manifest, err)
		}
		if _, err := publisher.PublishPersonaModelRoutePolicy(ctx, in); !errors.Is(err, ErrConflict) {
			t.Fatalf("same manifest revision replay accepted: %v", err)
		}
		got, err := reader.CurrentPersonaModelRoutePolicyForAgent(ctx, tenant, in.LegalEntityID, in.PolicyID, 1, 1, in.PolicyDigest, manifest, from)
		if err != nil || got.AgentVersionDigest != manifest || got.Revision != 1 {
			t.Fatalf("exact manifest route=%+v error=%v", got, err)
		}
	}
	if _, err := reader.CurrentPersonaModelRoutePolicyForAgent(ctx, other, "entity-a", "model-routing", 1, 1, semanticDigest, manifestA, from); !errors.Is(err, ErrPersonaModelRouteNotFound) {
		t.Fatalf("other tenant route error=%v", err)
	}
	if _, err := reader.CurrentPersonaModelRoutePolicyForAgent(ctx, tenant, "entity-a", "model-routing", 1, 1, semanticDigest, "sha256:"+strings.Repeat("0", 64), from); !errors.Is(err, ErrPersonaModelRouteNotFound) {
		t.Fatalf("unpublished manifest route error=%v", err)
	}
	if _, err := reader.CurrentPersonaModelRoutePolicyForAgent(ctx, tenant, "entity-a", "model-routing", 1, 1, semanticDigest, manifestA, from.Add(time.Hour)); !errors.Is(err, ErrPersonaModelRouteNotFound) {
		t.Fatalf("expired qualified route accepted: %v", err)
	}
	if _, err := db.SQL.Exec(`UPDATE persona_model_route_deployment SET revision=2 WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("model deployment evidence mutated")
	}
}
