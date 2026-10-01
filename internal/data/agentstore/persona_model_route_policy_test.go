package agentstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestPersonaModelRoutePolicy_RejectsUnscopedLookup(t *testing.T) {
	store := &Store{}
	_, err := store.CurrentPersonaModelRoutePolicy(context.Background(), uuid.Nil, "entity", "policy", 1, 1, "sha256:"+strings.Repeat("a", 64), time.Unix(1, 0).UTC())
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unscoped lookup error=%v, want invalid config", err)
	}
}

func TestTodo_AGENTP_021_IntegrationSemanticAndRouteIntegrity(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	if _, err := db.SQL.Exec(`INSERT INTO tenant(tenant_id) VALUES($1),($2)`, tenant, other); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("semantic_policy"), uuid.NewString()
	if err := createAgentLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.SQL.Exec("DROP ROLE " + login); err != nil {
			t.Error(err)
		}
	})
	store, err := New(ctx, Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5433/core", MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	semantic := []byte(`{"id":"model-routing","version":1,"schema_version":1,"purpose":"persona.reply","budget":{"max_cost_micros":250}}`)
	_, policyDigest, err := CanonicalPersonaModelRoutePayload(semantic)
	if err != nil {
		t.Fatal(err)
	}
	route := []byte(`{"model_digest":"sha256:` + strings.Repeat("a", 64) + `","manifest_digest":"sha256:` + strings.Repeat("b", 64) + `"}`)
	_, routeDigest, err := CanonicalPersonaModelRoutePayload(route)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	insert := func(entity, digest string, payload []byte) {
		t.Helper()
		if _, err := db.SQL.Exec(`INSERT INTO persona_model_route_policy(tenant_id,legal_entity_id,policy_id,policy_version,policy_schema_version,policy_digest,revision,effective_from,route_payload,route_payload_digest,policy_payload) VALUES($1,$2,'model-routing',1,1,$3,1,$4,$5::jsonb,$6,$7::jsonb)`, tenant, entity, policyDigest, at, string(payload), digest, string(semantic)); err != nil {
			t.Fatal(err)
		}
	}
	insert("entity", routeDigest, route)
	got, err := store.CurrentPersonaModelRoutePolicy(ctx, tenant, "entity", "model-routing", 1, 1, policyDigest, at)
	if err != nil || got.PolicyDigest != policyDigest || got.RoutePayloadDigest != routeDigest || got.PolicyDigest == got.RoutePayloadDigest {
		t.Fatalf("read independent digests=%+v error=%v", got, err)
	}
	if _, err := store.CurrentPersonaModelRoutePolicy(ctx, other, "entity", "model-routing", 1, 1, policyDigest, at); !errors.Is(err, ErrPersonaModelRouteNotFound) {
		t.Fatalf("tenant isolation error=%v", err)
	}
	if _, err := store.CurrentPersonaModelRoutePolicy(ctx, tenant, "entity", "model-routing", 1, 1, routeDigest, at); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("route digest substituted for policy digest: %v", err)
	}
	insert("corrupt-route", routeDigest, []byte(`{"model_digest":"sha256:`+strings.Repeat("c", 64)+`"}`))
	if _, err := store.CurrentPersonaModelRoutePolicy(ctx, tenant, "corrupt-route", "model-routing", 1, 1, policyDigest, at); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("tampered route accepted: %v", err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO persona_model_route_policy(tenant_id,legal_entity_id,policy_id,policy_version,policy_schema_version,policy_digest,revision,effective_from,route_payload,route_payload_digest,policy_payload) VALUES($1,'corrupt-policy','model-routing',1,1,$2,1,$3,$4::jsonb,$5,'{"id":"model-routing","version":1,"schema_version":1,"purpose":"changed"}'::jsonb)`, tenant, policyDigest, at, string(route), routeDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CurrentPersonaModelRoutePolicy(ctx, tenant, "corrupt-policy", "model-routing", 1, 1, policyDigest, at); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("tampered semantic policy accepted: %v", err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO persona_model_route_policy(tenant_id,legal_entity_id,policy_id,policy_version,policy_schema_version,policy_digest,revision,effective_from,route_payload,route_payload_digest,policy_payload) SELECT tenant_id,legal_entity_id,policy_id,policy_version,policy_schema_version,policy_digest,2,effective_from,route_payload,route_payload_digest,policy_payload FROM persona_model_route_policy WHERE tenant_id=$1 AND legal_entity_id='entity'`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CurrentPersonaModelRoutePolicy(ctx, tenant, "entity", "model-routing", 1, 1, policyDigest, at); !errors.Is(err, ErrPersonaModelRouteAmbiguous) {
		t.Fatalf("overlapping routes accepted: %v", err)
	}
}
