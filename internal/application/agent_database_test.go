package application

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func TestAgentDatabaseConfigurationIsDeclaredSecretAndLocalDevIsolated(t *testing.T) {
	var found bootstrap.Field
	for _, field := range ServeConfigFields() {
		if field.Name == FieldAgentDatabaseURL {
			found = field
			break
		}
	}
	if found.Env != EnvAgentDatabaseURL || !found.Secret || found.Default != "" {
		t.Fatalf("agent database field=%+v, want secret env-backed field without standard default", found)
	}
	fields := ServeConfigFieldsForArgs([]string{"-profile", ServeProfileLocalDev})
	for _, field := range fields {
		if field.Name == FieldAgentDatabaseURL && field.Default != LocalDevAgentDatabaseURL {
			t.Fatalf("local-dev agent default=%q, want %q", field.Default, LocalDevAgentDatabaseURL)
		}
	}
}

func TestTodo_AGENTP_006_ServedEvaluationVerificationSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	const tenant values.TenantId = "evaluation-composition-test"
	mapTenant := func(t values.TenantId) uuid.UUID { return pgstore.TenantID(string(t)) }
	if _, err := db.SQL.Exec(`INSERT INTO tenant(tenant_id) VALUES($1)`, mapTenant(tenant)); err != nil {
		t.Fatal(err)
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	clock := func() time.Time { return now }
	seed := sha256.Sum256([]byte("test-only evaluator composition key"))
	private := ed25519.NewKeyFromSeed(seed[:])
	encoded, err := json.Marshal([]PersonaEvaluationVerificationKeyConfig{{TenantID: string(tenant), SuiteID: "test-suite", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey))}})
	if err != nil {
		t.Fatal(err)
	}
	keyID, _ := PersonaEvaluationVerificationKeyID(string(tenant), "test-suite", "v1")
	digest := "sha256:" + strings.Repeat("a", 64)
	// Positive signature fixture only; no evaluation or persona is seeded in
	// the user's database by this test.
	claim := agentpersonastore.PersonaEvaluationClaim{TenantID: string(tenant), RunID: "test-run", PersonaID: "test-persona", PersonaVersion: 1,
		ProfileDigest: digest, SuiteDigest: digest, RunDigest: digest, ModelDigest: digest, Passed: true,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), KeyID: keyID}
	evidence, err := agentpersonastore.SignPersonaEvaluationClaim(private, claim)
	if err != nil {
		t.Fatal(err)
	}
	unconfigured, err := composeAgentPersonaStore(conn, mapTenant, "", clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := unconfigured.RecordPersonaEvaluation(ctx, tenant, evidence); !errors.Is(err, agentpersonastore.ErrPublicationEvidenceRequired) {
		t.Fatalf("unconfigured store accepted evidence: %v", err)
	}
	configured, err := composeAgentPersonaStore(conn, mapTenant, string(encoded), clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := configured.RecordPersonaEvaluation(ctx, tenant, evidence); err != nil {
		t.Fatalf("serving composition lost evaluation authority: %v", err)
	}
	restarted, err := composeAgentPersonaStore(conn, mapTenant, string(encoded), clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RecordPersonaEvaluation(ctx, tenant, evidence); !errors.Is(err, agentpersonastore.ErrConflict) {
		t.Fatalf("restart lost durable evidence or verifier: %v", err)
	}
	evidence.Claim.RunID = "tampered-run"
	if err := restarted.RecordPersonaEvaluation(ctx, tenant, evidence); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("tampered seal accepted: %v", err)
	}
	if _, err := composeAgentPersonaStore(conn, mapTenant, `[{"private_key":"do-not-accept"}]`, clock); !errors.Is(err, ErrPersonaEvaluationKeyConfiguration) {
		t.Fatalf("invalid serving keyring: %v", err)
	}
}

func TestTodo_AGENTP_006_EvaluationVerifierConfiguration(t *testing.T) {
	seed := sha256.Sum256([]byte("test-only public key configuration"))
	private := ed25519.NewKeyFromSeed(seed[:])
	encoded, _ := json.Marshal([]PersonaEvaluationVerificationKeyConfig{{TenantID: "tenant-a", SuiteID: "suite-a", KeyID: "v1", PublicKey: base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey))}})
	parsed, err := bootstrap.ParseConfig(nil, func(name string) (string, bool) {
		if name == EnvPersonaEvaluationPublicKeys {
			return string(encoded), true
		}
		return "", false
	}, ServeConfigFields())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ServeConfigFromValues(parsed)
	if err != nil || cfg.PersonaEvaluationPublicKeys != string(encoded) {
		t.Fatalf("evaluation keyring lost in serve configuration: %v", err)
	}
	cfg.DevHMACKey, cfg.PageCursorKey = testDevKey, testPageCursorKey
	cfg.DatabaseURL = "postgres://test:unused@db.invalid:5432/core"
	cfg.ExecutionAuthority = false
	cfg.Scheduler = false
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.PersonaEvaluationPublicKeys = `[{"private_key":"invalid"}]`
	if err := cfg.Validate(); !errors.Is(err, ErrPersonaEvaluationKeyConfiguration) {
		t.Fatalf("invalid keyring passed validation: %v", err)
	}
}

func TestServeConfigRejectsAgentDatabaseSharingLogicalDatabase(t *testing.T) {
	base := ServeConfig{DatabaseURL: "postgres://core:secret@db.internal:5432/hcm", DevHMACKey: testDevKey, PageCursorKey: testPageCursorKey}
	for name, mutate := range map[string]func(*ServeConfig){
		"core": func(c *ServeConfig) { c.AgentDatabaseURL = "postgres://agent:other@db.internal:5432/hcm" },
		"chat": func(c *ServeConfig) {
			c.ChatDatabaseURL = "postgres://chat:other@db.internal:5432/chat"
			c.AgentDatabaseURL = "postgres://agent:other@db.internal:5432/chat"
		},
		"document": func(c *ServeConfig) {
			c.DocumentDatabaseURL = "postgres://docs:other@db.internal:5432/docs"
			c.AgentDatabaseURL = "postgres://agent:other@db.internal:5432/docs"
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			err := cfg.validateAgentDatabaseURL()
			if err == nil || !strings.Contains(err.Error(), FieldAgentDatabaseURL) {
				t.Fatalf("validateAgentDatabaseURL()=%v, want isolation error", err)
			}
		})
	}
}

func TestComposeAgentDatabaseWithoutDSNIsDisabled(t *testing.T) {
	got, err := composeAgentDatabase(context.Background(), ServeConfig{})
	if err != nil {
		t.Fatalf("composeAgentDatabase(empty) error: %v", err)
	}
	if got.store != nil || got.personas != nil || got.close != nil {
		t.Fatalf("empty configuration composed agent runtime: %+v", got)
	}
}

func TestLocalDevAgentDatabaseBootstrapIsStrictlyScoped(t *testing.T) {
	if !localDevAgentDatabaseRequired(ServeConfig{Profile: ServeProfileLocalDev, AgentDatabaseURL: LocalDevAgentDatabaseURL}) {
		t.Fatal("built-in local-dev agent database should be eligible for bootstrap")
	}
	for name, cfg := range map[string]ServeConfig{
		"standard profile": {Profile: ServeProfileStandard, AgentDatabaseURL: LocalDevAgentDatabaseURL},
		"custom dsn":       {Profile: ServeProfileLocalDev, AgentDatabaseURL: "postgres://postgres:postgres@127.0.0.1:5432/custom_agents"},
	} {
		t.Run(name, func(t *testing.T) {
			if localDevAgentDatabaseRequired(cfg) {
				t.Fatal("database bootstrap escaped its exact local-dev scope")
			}
		})
	}
}
