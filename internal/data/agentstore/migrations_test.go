package agentstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_AGENT_008_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("apply isolated agent migrations: %v", err)
	}
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("reapply isolated agent migrations: %v", err)
	}

	tenantA, tenantB := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, tenantA, tenantB); err != nil {
		t.Fatalf("seed trusted tenant projection: %v", err)
	}
	agentLogin, agentPassword := roleName("agent_login"), uuid.NewString()
	coreLogin := roleName("core_login")
	if err := createAgentLogin(ctx, db.SQL, agentLogin, agentPassword); err != nil {
		t.Fatalf("create isolated agent login: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+agentLogin); err != nil {
			t.Errorf("drop isolated agent login: %v", err)
		}
	})
	if _, err := db.SQL.ExecContext(ctx, "CREATE ROLE "+coreLogin+" LOGIN NOSUPERUSER NOBYPASSRLS"); err != nil {
		t.Fatalf("create core comparison login: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+coreLogin); err != nil {
			t.Errorf("drop core comparison login: %v", err)
		}
	})
	agentDSN := testDSN(t, db.URL, db.Schema, agentLogin, agentPassword, "postgres")
	configuredURL, err := url.Parse(agentDSN)
	if err != nil {
		t.Fatal(err)
	}
	query := configuredURL.Query()
	query.Set("pool_max_conns", "100")
	configuredURL.RawQuery = query.Encode()
	agentDSN = configuredURL.String()
	coreDSN := testDSN(t, db.URL, "", coreLogin, "unused", "core")
	cfg := Config{DSN: agentDSN, CoreDSN: coreDSN, MaxConns: 1}
	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("open credential-isolated agent pool: %v", err)
	}
	t.Cleanup(func() {
		if store != nil {
			store.Close()
		}
	})
	if got := store.PoolStats().MaxConns; got != 1 {
		t.Fatalf("agent pool max connections = %d, want 1", got)
	}
	if got := store.fencePool.Stats().MaxConns; got != 4 {
		t.Fatalf("persona fence pool accepted DSN connection override: max=%d, want4", got)
	}
	fenceCtx, cancelFence := context.WithTimeout(ctx, 30*time.Second)
	defer cancelFence()
	if err := store.RunTenantFenceTx(fenceCtx, tenantA, func(fence dbport.Tx) error {
		var scoped string
		if err := fence.QueryRow(fenceCtx, `SELECT current_setting('app.tenant_id')`).Scan(&scoped); err != nil {
			return err
		}
		if scoped != tenantA.String() {
			return fmt.Errorf("fence scope=%s", scoped)
		}
		return store.RunTenantTx(fenceCtx, tenantA, func(main dbport.Tx) error {
			var found uuid.UUID
			return main.QueryRow(fenceCtx, `SELECT tenant_id FROM tenant WHERE tenant_id=$1`, tenantA).Scan(&found)
		})
	}); err != nil {
		t.Fatalf("fence callback starved one-connection main pool: %v", err)
	}

	manifestA := testManifest("benefits-helper", 1, "harborcare-policy")
	seedManifestInstructions(t, store, tenantA, manifestA)
	if revision, err := store.SaveManifest(ctx, tenantA, manifestA, 0); err != nil || revision != 1 {
		t.Fatalf("save first manifest revision = %d, %v; want 1, nil", revision, err)
	}
	storedManifest, err := store.ManifestVersion(ctx, tenantA, manifestA.ID, manifestA.Version)
	if err != nil || storedManifest.InstructionsDigest != manifestA.InstructionsDigest {
		t.Fatalf("read exact manifest version = %+v, %v; want digest %q", storedManifest, err, manifestA.InstructionsDigest)
	}
	if _, err := store.ManifestVersion(ctx, tenantA, manifestA.ID, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing immutable manifest version error = %v, want ErrNotFound", err)
	}
	manifestB := testManifest("benefits-helper", 1, "ironridge-policy")
	seedManifestInstructions(t, store, tenantB, manifestB)
	if revision, err := store.SaveManifest(ctx, tenantB, manifestB, 0); err != nil || revision != 1 {
		t.Fatalf("save other tenant manifest revision = %d, %v; want 1, nil", revision, err)
	}
	manifestA2 := testManifest("benefits-helper", 2, "harborcare-policy-v2")
	seedManifestInstructions(t, store, tenantA, manifestA2)
	if revision, err := store.SaveManifest(ctx, tenantA, manifestA2, 1); err != nil || revision != 2 {
		t.Fatalf("save next manifest revision = %d, %v; want 2, nil", revision, err)
	}
	for _, tc := range []struct {
		name     string
		tenant   uuid.UUID
		version  uint64
		digest   string
		wantText string
		wantErr  error
	}{
		{name: "exact historical version", tenant: tenantA, version: 1, digest: manifestA.InstructionsDigest, wantText: testInstructions(manifestA.ID, manifestA.Version, manifestA.Purpose)},
		{name: "exact current version", tenant: tenantA, version: 2, digest: manifestA2.InstructionsDigest, wantText: testInstructions(manifestA2.ID, manifestA2.Version, manifestA2.Purpose)},
		{name: "wrong digest", tenant: tenantA, version: 1, digest: manifestA2.InstructionsDigest, wantErr: ErrInstructionContentNotFound},
		{name: "wrong tenant", tenant: tenantB, version: 1, digest: manifestA.InstructionsDigest, wantErr: ErrInstructionContentNotFound},
	} {
		t.Run("manifest instructions "+tc.name, func(t *testing.T) {
			got, err := store.ManifestInstructions(ctx, tc.tenant, manifestA.ID, tc.version, tc.digest)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("read error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.wantText {
				t.Fatalf("read instructions = %q, %v; want exact body %q", got, err, tc.wantText)
			}
		})
	}
	content := testInstructions(manifestA.ID, manifestA.Version, manifestA.Purpose)
	if digest, err := store.SaveInstructionContent(ctx, tenantA, content); err != nil || digest != manifestA.InstructionsDigest {
		t.Fatalf("idempotent instruction write = %q, %v; want retained digest %q", digest, err, manifestA.InstructionsDigest)
	}
	if err := store.RunTenantTx(ctx, tenantA, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agent_instruction_content SET content='rewritten'
			WHERE tenant_id=$1 AND digest=$2`, tenantA, manifestA.InstructionsDigest)
		return err
	}); err == nil {
		t.Fatal("agent app role updated immutable executable instruction content")
	}
	for _, invalid := range []struct {
		name   string
		tenant uuid.UUID
		text   string
	}{
		{name: "missing tenant", text: "instructions"},
		{name: "empty text", tenant: tenantA},
		{name: "whitespace text", tenant: tenantA, text: " \n\t"},
		{name: "invalid UTF-8", tenant: tenantA, text: string([]byte{0xff})},
		{name: "oversized", tenant: tenantA, text: strings.Repeat("x", maxInstructionContentBytes+1)},
	} {
		t.Run("reject invalid instruction content "+invalid.name, func(t *testing.T) {
			if _, err := store.SaveInstructionContent(ctx, invalid.tenant, invalid.text); !errors.Is(err, ErrInvalidInstructionContent) {
				t.Fatalf("save error = %v, want ErrInvalidInstructionContent", err)
			}
		})
	}
	if _, err := store.ManifestInstructions(ctx, uuid.Nil, manifestA.ID, 1, manifestA.InstructionsDigest); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid manifest instruction scope error = %v, want ErrInvalidConfig", err)
	}
	if _, err := store.SaveManifest(ctx, tenantA, testManifest("without-instructions", 1, "missing body"), 0); !errors.Is(err, ErrInstructionContentNotFound) {
		t.Fatalf("manifest without stored executable content error = %v, want ErrInstructionContentNotFound", err)
	}
	forgedManifest := testManifest("forged-content", 1, "must be rejected")
	forgedManifest.InstructionsDigest = "sha256:" + strings.Repeat("f", 64)
	if err := store.RunTenantTx(ctx, tenantA, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO agent_instruction_content(tenant_id,digest,content) VALUES ($1,$2,$3)`, tenantA, forgedManifest.InstructionsDigest, "different bytes")
		return err
	}); err != nil {
		t.Fatalf("insert corrupted-row fixture: %v", err)
	}
	if _, err := store.SaveManifest(ctx, tenantA, forgedManifest, 0); !errors.Is(err, ErrInstructionContentNotFound) {
		t.Fatalf("manifest with mismatching stored bytes error = %v, want ErrInstructionContentNotFound", err)
	}
	manifestStale := testManifest("benefits-helper", 3, "stale-writer")
	seedManifestInstructions(t, store, tenantA, manifestStale)
	if _, err := store.SaveManifest(ctx, tenantA, manifestStale, 1); err != ErrConflict {
		t.Fatalf("stale manifest writer error = %v, want ErrConflict", err)
	}
	if _, _, err := store.CurrentManifest(ctx, tenantB, "benefits-helper"); err != nil {
		t.Fatalf("tenant B could not read its own definition: %v", err)
	}
	if _, _, err := store.CurrentManifest(ctx, tenantA, "missing-definition"); err != ErrNotFound {
		t.Fatalf("missing definition error = %v, want ErrNotFound", err)
	}
	assertAgentSession(t, store, agentLogin, tenantA)
	assertTenantFence(t, store, tenantA, tenantB)
	writeDurableRequestAndOutbox(t, store, tenantA)

	store.Close()
	store = nil
	store, err = New(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen agent pool: %v", err)
	}
	got, revision, err := store.CurrentManifest(ctx, tenantA, "benefits-helper")
	if err != nil || revision != 2 || got.Purpose != manifestA2.Purpose {
		t.Fatalf("reopened manifest = revision %d, purpose %q, error %v; want revision 2 and %q", revision, got.Purpose, err, manifestA2.Purpose)
	}
	assertDurableRows(t, store, tenantA)
}

func TestTodo_AGENT_008_Recovery(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("apply isolated agent migrations: %v", err)
	}
	tenantID := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenantID); err != nil {
		t.Fatalf("seed trusted tenant projection: %v", err)
	}
	agentLogin, agentPassword := roleName("agent_recovery"), uuid.NewString()
	if err := createAgentLogin(ctx, db.SQL, agentLogin, agentPassword); err != nil {
		t.Fatalf("create agent recovery login: %v", err)
	}
	agentDSN := testDSN(t, db.URL, db.Schema, agentLogin, agentPassword, "postgres")
	t.Cleanup(func() {
		if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+agentLogin); err != nil {
			t.Errorf("drop agent recovery login: %v", err)
		}
	})
	cfg := Config{DSN: agentDSN, CoreDSN: "postgres://core:pw@127.0.0.1:5433/core"}
	store, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("open agent recovery pool: %v", err)
	}
	t.Cleanup(func() {
		if store != nil {
			store.Close()
		}
	})
	manifest := testManifest("recovery-agent", 7, "durable recovery")
	seedManifestInstructions(t, store, tenantID, manifest)
	if _, err := store.SaveManifest(ctx, tenantID, manifest, 0); err != nil {
		t.Fatalf("commit manifest before restart: %v", err)
	}
	writeDurableRequestAndOutbox(t, store, tenantID)
	store.Close()
	store = nil
	store, err = New(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen agent recovery pool: %v", err)
	}
	got, revision, err := store.CurrentManifest(ctx, tenantID, manifest.ID)
	digest, digestErr := got.Digest()
	if err != nil || digestErr != nil || revision != 1 || got.Version != 7 || digest == "" {
		t.Fatalf("restored manifest = version %d, revision %d, error %v", got.Version, revision, err)
	}
	assertDurableRows(t, store, tenantID)
}

func assertAgentSession(t *testing.T, store *Store, expectedLogin string, tenantID uuid.UUID) {
	t.Helper()
	err := store.RunTenantTx(context.Background(), tenantID, func(tx dbport.Tx) error {
		var sessionUser, currentUser string
		if err := tx.QueryRow(context.Background(), `SELECT session_user,current_user`).Scan(&sessionUser, &currentUser); err != nil {
			return err
		}
		if sessionUser != expectedLogin || currentUser != AppRole {
			t.Fatalf("database identity session_user=%q current_user=%q, want %q / %q", sessionUser, currentUser, expectedLogin, AppRole)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("check agent database identities: %v", err)
	}
}

func assertTenantFence(t *testing.T, store *Store, tenantA, tenantB uuid.UUID) {
	t.Helper()
	if err := store.RunTenantTx(context.Background(), tenantA, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM tenant`).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("tenant A sees %d tenant projections, want only itself", count)
		}
		var rows int64
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM agent_definition_version`).Scan(&rows); err != nil {
			return err
		}
		if rows != 2 {
			t.Fatalf("tenant A sees %d manifest versions, want only its own two", rows)
		}
		return nil
	}); err != nil {
		t.Fatalf("enforce RLS tenant fence: %v", err)
	}
	err := store.RunTenantTx(context.Background(), tenantA, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO agent_definition_version
			(tenant_id,definition_id,version,schema_version,digest,manifest)
			VALUES ($1,'forged',1,1,$2,'{}'::jsonb)`, tenantB, "sha256:"+strings.Repeat("0", 64))
		if err == nil {
			return fmt.Errorf("tenant A inserted a manifest version owned by tenant B")
		}
		return err
	})
	if err == nil {
		t.Fatal("tenant A inserted a manifest version owned by tenant B")
	}
	err = store.RunTenantTx(context.Background(), tenantA, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO agent_instruction_content(tenant_id,digest,content)
			VALUES ($1,$2,'foreign tenant content')`, tenantB, "sha256:"+strings.Repeat("d", 64))
		if err == nil {
			return fmt.Errorf("tenant A inserted executable content owned by tenant B")
		}
		return err
	})
	if err == nil {
		t.Fatal("tenant A inserted executable content owned by tenant B")
	}
	if err := store.RunTx(context.Background(), func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM tenant`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("unscoped pooled transaction sees %d tenant projections", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify tenant scope does not leak to next pooled transaction: %v", err)
	}
}

func writeDurableRequestAndOutbox(t *testing.T, store *Store, tenantID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	err := store.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO agent_run_request
			(tenant_id,request_id,source_kind,source_key_digest,request_digest,request_payload,
			 decision,authority_snapshot,admitted_at,deadline,agent_id,agent_version,agent_digest,
			 installation_id,legal_entity_id,principal_chain,purpose,audience,context_scope,budget,cause_id)
			VALUES ($1,'req-1','EVENT',$2,$3,'{}'::jsonb,'ACCEPTED','{}'::jsonb,now(),now()+interval '1 hour',
			 'benefits-helper','v2',$4,'install-1','','{"mode":"SPONSORED"}'::jsonb,
			 'policy review','[]'::jsonb,'[]'::jsonb,'{}'::jsonb,'cause-1')`,
			tenantID, strings.Repeat("a", 64), strings.Repeat("b", 64), "sha256:"+strings.Repeat("c", 64))
		if err != nil {
			return fmt.Errorf("insert run request: %w", err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_outbox
			(tenant_id,event_id,aggregate_kind,aggregate_id,event_type,payload,occurred_at)
			VALUES ($1,'evt-1','agent_run_request','req-1','AgentRunAccepted','{}'::jsonb,now())`, tenantID)
		return err
	})
	if err != nil {
		t.Fatalf("persist run request and outbox event: %v", err)
	}
}

func assertDurableRows(t *testing.T, store *Store, tenantID uuid.UUID) {
	t.Helper()
	if err := store.RunTenantTx(context.Background(), tenantID, func(tx dbport.Tx) error {
		for _, table := range []string{"agent_run_request", "agent_outbox"} {
			var count int
			if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				t.Fatalf("reopened store has %d %s rows, want 1", count, table)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read committed rows after pool reopen: %v", err)
	}
}

func createAgentLogin(ctx context.Context, db *sql.DB, name, password string) error {
	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD '%s'", name, password)); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "GRANT "+AppRole+" TO "+name)
	return err
}

func roleName(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func testDSN(t *testing.T, base, schema, user, password, database string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(user, password)
	u.Path = "/" + database
	q := u.Query()
	if schema != "" {
		q.Set("search_path", schema)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func testManifest(id string, version uint64, purpose string) agentmanifest.Manifest {
	ref := func(id string) agentmanifest.Reference {
		return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("0", 64)}
	}
	return agentmanifest.Manifest{
		SchemaVersion: agentmanifest.CurrentSchemaVersion, ID: id, Version: version,
		OwnerID: "owner-1", Purpose: purpose, InstructionsDigest: instructionDigest(testInstructions(id, version, purpose)),
		SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{},
		ModelPolicy: ref("model-policy"), AutonomyCeiling: "ASSISTED",
		Budget:       agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 1000, MaxOutputTokens: 500, MaxConcurrentRuns: 2},
		OutputSchema: ref("output-schema"), ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: []agentmanifest.Reference{ref("evaluation-suite")},
	}
}

func testInstructions(id string, version uint64, purpose string) string {
	return fmt.Sprintf("Instructions for %s version %d: %s", id, version, purpose)
}

func seedManifestInstructions(t *testing.T, store *Store, tenantID uuid.UUID, manifest agentmanifest.Manifest) {
	t.Helper()
	digest, err := store.SaveInstructionContent(context.Background(), tenantID, testInstructions(manifest.ID, manifest.Version, manifest.Purpose))
	if err != nil || digest != manifest.InstructionsDigest {
		t.Fatalf("save manifest instruction content digest = %q, %v; want %q", digest, err, manifest.InstructionsDigest)
	}
}
