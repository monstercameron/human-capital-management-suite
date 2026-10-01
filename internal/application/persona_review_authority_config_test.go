package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func TestPersonaReviewAuthorityDatabaseConfigIsOptInAndSecret(t *testing.T) {
	var field bootstrap.Field
	for _, candidate := range ServeConfigFields() {
		if candidate.Name == FieldPersonaReviewAuthorityDatabaseURL {
			field = candidate
			break
		}
	}
	if field.Name == "" || field.Env != EnvPersonaReviewAuthorityDatabaseURL || !field.Secret || field.Default != "" {
		t.Fatalf("persona review authority field=%+v, want secret env-backed field with no default", field)
	}
	if got := (ServeConfig{}).PersonaReviewAuthorityDatabaseURL; got != "" {
		t.Fatalf("zero-value persona review authority DSN=%q, want empty", got)
	}

	const dsn = "postgres://review_reader:private@db.internal:5432/hcm_next_agents?sslmode=require"
	values, err := bootstrap.ParseConfig(nil, func(name string) (string, bool) {
		if name == EnvPersonaReviewAuthorityDatabaseURL {
			return dsn, true
		}
		return "", false
	}, ServeConfigFields())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ServeConfigFromValues(values)
	if err != nil || cfg.PersonaReviewAuthorityDatabaseURL != dsn {
		t.Fatalf("config DSN=%q err=%v", cfg.PersonaReviewAuthorityDatabaseURL, err)
	}
	if strings.Contains(cfg.ConfigFingerprint, "private") {
		t.Fatal("persona review authority password leaked into config fingerprint")
	}
}

func TestPersonaReviewAuthorityDatabaseRequiresDistinctLoginRole(t *testing.T) {
	base := ServeConfig{
		DatabaseURL:      "postgres://core_user:core_pw@db.internal:5432/hcm_next",
		AgentDatabaseURL: "postgres://agent_user:agent_pw@db.internal:5432/hcm_next_agents",
	}
	for _, tc := range []struct {
		name string
		dsn  string
		want string
	}{
		{"same core role", "postgres://core_user:review_pw@db.internal:5432/hcm_next", FieldDatabaseURL},
		{"same agent role", "postgres://agent_user:review_pw@db.internal:5432/hcm_next_agents", FieldAgentDatabaseURL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.PersonaReviewAuthorityDatabaseURL = tc.dsn
			err := cfg.validatePersonaReviewAuthorityDatabaseURL()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation error=%v, want distinct role from %s", err, tc.want)
			}
			if strings.Contains(err.Error(), "review_pw") {
				t.Fatal("validation error exposed DSN credential")
			}
		})
	}
}

func TestPersonaReviewAuthorityDatabaseMayShareAgentDatabaseWithSeparateRole(t *testing.T) {
	cfg := ServeConfig{
		AgentDatabaseURL:                  "postgres://agent_user:agent_pw@db.internal:5432/hcm_next_agents",
		PersonaReviewAuthorityDatabaseURL: "postgres://review_user:review_pw@db.internal:5432/hcm_next_agents",
	}
	if err := cfg.validatePersonaReviewAuthorityDatabaseURL(); err != nil {
		t.Fatalf("separate read role on agent database rejected: %v", err)
	}
	cfg.PersonaReviewAuthorityDatabaseURL = "not-a-postgres-url"
	if err := cfg.validatePersonaReviewAuthorityDatabaseURL(); err == nil || strings.Contains(err.Error(), "not-a-postgres-url") {
		t.Fatalf("malformed DSN error=%v; want safe validation error", err)
	}
}
