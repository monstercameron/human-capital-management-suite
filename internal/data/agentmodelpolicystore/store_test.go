package agentmodelpolicystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_AGENT_021_ImmutablePolicyContracts(t *testing.T) {
	content := []byte(`{"id":"policy","identity":{"provider":"openai"},"schema_version":1,"version":1}`)
	r := Record{Kind: ModelPolicy, Reference: agentmanifest.Reference{ID: "policy", Version: 1, SchemaVersion: 1, Digest: ContentDigest(content)}, Content: content}
	if err := ValidateRecord(r); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"version":1,"schema_version":1,"id":"policy","identity":{"provider":"openai"}}`, `{"id":"policy","id":"policy","schema_version":1,"version":1}`} {
		changed := r
		changed.Content = []byte(raw)
		changed.Reference.Digest = ContentDigest(changed.Content)
		if err := ValidateRecord(changed); !errors.Is(err, ErrInvalid) {
			t.Fatalf("noncanonical semantic policy=%v", err)
		}
	}
	suite := agenteval.PolicyHelperSuite("hcmnext.skill.policy_helper")
	raw, _ := json.Marshal(suite)
	eval := Record{Kind: EvaluationSuite, Reference: agentmanifest.Reference{ID: suite.ID, Version: 1, SchemaVersion: 1, Digest: agenteval.PersonaSuiteDigest(suite)}, Content: raw}
	if err := ValidateRecord(eval); err != nil {
		t.Fatal(err)
	}
	if eval.Reference.Digest == ContentDigest(raw) {
		t.Fatal("suite domain digest lost")
	}
	eval.Reference.Digest = ContentDigest(raw)
	if err := ValidateRecord(eval); !errors.Is(err, ErrInvalid) {
		t.Fatalf("undomained suite digest=%v", err)
	}
	for _, mutate := range []func(*Record){func(r *Record) { r.Kind = "unknown" }, func(r *Record) { r.Reference.Version = 0 }, func(r *Record) { r.Reference.SchemaVersion = 0 }, func(r *Record) { r.Reference.ID = " " }, func(r *Record) { r.Reference.Digest = "wrong" }, func(r *Record) { r.Content = []byte("bad-json") }, func(r *Record) { r.Content = nil }} {
		copy := r
		mutate(&copy)
		if err := ValidateRecord(copy); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid record accepted: %+v, %v", copy, err)
		}
	}
	if _, err := New(nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil reader=%v", err)
	}
	if _, err := NewPublisher(nil, verifiedPublication{}); !errors.Is(err, ErrAuthority) {
		t.Fatalf("nil publisher db=%v", err)
	}
	if _, err := NewPublisher(failedDB{}, nil); !errors.Is(err, ErrAuthority) {
		t.Fatalf("nil verifier=%v", err)
	}
	store, _ := New(failedDB{})
	if _, err := store.Resolve(context.Background(), uuid.New(), ModelPolicy, r.Reference, time.Now()); err == nil {
		t.Fatal("database failure accepted")
	}
	publisher, _ := NewPublisher(failedDB{}, verifiedPublication{})
	if err := publisher.Publish(context.Background(), uuid.New(), r, authorityFixture(time.Now())); err == nil {
		t.Fatal("publication database failure accepted")
	}
	if err := publisher.Publish(context.Background(), uuid.Nil, r, Authority{}); !errors.Is(err, ErrAuthority) {
		t.Fatalf("invalid authority=%v", err)
	}
}

func TestTodo_AGENT_021_PolicyRegistryPersistenceRLSAndRevocation(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	if _, err := db.SQL.Exec(`INSERT INTO tenant(tenant_id) VALUES($1),($2)`, tenant, other); err != nil {
		t.Fatal(err)
	}
	publisherDB := roleDB{db: db.Conn, role: AuthorityRole}
	servingDB := roleDB{db: db.Conn, role: agentstore.AppRole}
	publisher, _ := NewPublisher(publisherDB, verifiedPublication{})
	store, _ := New(servingDB)
	now := time.Now().UTC().Truncate(time.Second).Add(600 * time.Nanosecond)
	raw := []byte(`{"id":"model.policy","identity":"openai:gpt-5-mini-2025-08-07","schema_version":1,"version":1}`)
	r := Record{Kind: ModelPolicy, Reference: agentmanifest.Reference{ID: "model.policy", Version: 1, SchemaVersion: 1, Digest: ContentDigest(raw)}, Content: raw}
	a := authorityFixture(now)
	if err := publisher.Publish(ctx, tenant, r, a); err != nil {
		t.Fatalf("publish=%v", err)
	}
	restarted, _ := New(servingDB)
	current, err := restarted.Resolve(ctx, tenant, ModelPolicy, r.Reference, now)
	if err != nil || current.Revision != 1 || string(current.Record.Content) != string(raw) || current.Authority.SourceID != a.SourceID || !current.Authority.EffectiveFrom.Equal(a.EffectiveFrom) || !current.Authority.EffectiveUntil.Equal(a.EffectiveUntil) || string(current.Authority.Document) != string(a.Document) || string(current.Authority.Signature) != string(a.Signature) {
		t.Fatalf("restart resolution=%+v %v", current, err)
	}
	for _, tc := range []struct {
		name   string
		tenant uuid.UUID
		ref    agentmanifest.Reference
		at     time.Time
	}{{"other tenant", other, r.Reference, now}, {"expired", tenant, r.Reference, a.EffectiveUntil}, {"future", tenant, r.Reference, a.EffectiveFrom.Add(-time.Second)}, {"before signed start within stored microsecond", tenant, r.Reference, a.EffectiveFrom.Add(-time.Nanosecond)}} {
		if _, err := store.Resolve(ctx, tc.tenant, ModelPolicy, tc.ref, tc.at); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s=%v", tc.name, err)
		}
	}
	if _, err := store.Resolve(ctx, tenant, ModelPolicy, r.Reference, a.EffectiveUntil.Add(-time.Nanosecond)); err != nil {
		t.Fatalf("last signed-valid nanosecond refused=%v", err)
	}
	wrong := r.Reference
	wrong.Digest = ContentDigest([]byte("different"))
	if _, err := store.Resolve(ctx, tenant, ModelPolicy, wrong, now); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("digest substitution=%v", err)
	}
	if err := publisher.Publish(ctx, tenant, r, a); !errors.Is(err, ErrConflict) {
		t.Fatalf("source replay=%v", err)
	}
	changed := r
	changed.Content = []byte(`{"id":"model.policy","identity":"replacement","schema_version":1,"version":1}`)
	changed.Reference.Digest = ContentDigest(changed.Content)
	a.SourceRevision = 2
	if err := publisher.Publish(ctx, tenant, changed, a); !errors.Is(err, ErrConflict) {
		t.Fatalf("immutable identity replacement=%v", err)
	}
	appPublisher, _ := NewPublisher(servingDB, verifiedPublication{})
	if err := appPublisher.Publish(ctx, tenant, r, a); !errors.Is(err, ErrAuthority) {
		t.Fatalf("serving credential published=%v", err)
	}
	rejected, _ := NewPublisher(publisherDB, verifiedPublication{err: errors.New("unreviewed source")})
	if err := rejected.Publish(ctx, tenant, r, a); !errors.Is(err, ErrAuthority) {
		t.Fatalf("unverified source published=%v", err)
	}
	if _, err := db.SQL.Exec(`UPDATE agent_immutable_contract SET content='changed' WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("immutable content updated")
	}
	if _, err := db.SQL.Exec(`DELETE FROM agent_contract_authority WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("authority history deleted")
	}
	tx, err := publisherDB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, other.String()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_immutable_contract WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 0 {
		t.Fatalf("publisher RLS leaked=%d,%v", count, err)
	}
	_ = tx.Rollback(ctx)
	a.Revoked = true
	if err := publisher.Publish(ctx, tenant, r, a); err != nil {
		t.Fatalf("revoke=%v", err)
	}
	if _, err := restarted.Resolve(ctx, tenant, ModelPolicy, r.Reference, now); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("revoked latest fell back=%v", err)
	}
	var appInsert, pubInsert, pubUpdate, pubDelete, rls, forced bool
	if err := db.SQL.QueryRow(`SELECT has_table_privilege('hcmnext_agent_app','agent_immutable_contract','INSERT'),has_table_privilege('hcmnext_agent_model_policy_authority','agent_immutable_contract','INSERT'),has_table_privilege('hcmnext_agent_model_policy_authority','agent_immutable_contract','UPDATE'),has_table_privilege('hcmnext_agent_model_policy_authority','agent_immutable_contract','DELETE'),relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='agent_immutable_contract'::regclass`).Scan(&appInsert, &pubInsert, &pubUpdate, &pubDelete, &rls, &forced); err != nil {
		t.Fatal(err)
	}
	if appInsert || !pubInsert || pubUpdate || pubDelete || !rls || !forced {
		t.Fatalf("registry privileges app=%v,pub=%v/%v/%v,rls=%v/%v", appInsert, pubInsert, pubUpdate, pubDelete, rls, forced)
	}
}

type roleDB struct {
	db   dbport.Beginner
	role string
}

func (d roleDB) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+d.role); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

type verifiedPublication struct{ err error }

func (v verifiedPublication) VerifyPolicyPublication(context.Context, dbport.Tx, uuid.UUID, Record, Authority) error {
	return v.err
}

type failedDB struct{}

func (failedDB) Begin(context.Context) (dbport.Tx, error) { return nil, fmt.Errorf("unavailable") }
func authorityFixture(now time.Time) Authority {
	return Authority{SourceID: "signed-reviewed-deployment", SourceRevision: 1, KeyID: "trusted-key", Document: []byte(`{"review":"durable-review"}`), Signature: []byte("verified-by-test-authority"), EffectiveFrom: now.Add(-time.Minute), EffectiveUntil: now.Add(time.Hour)}
}
