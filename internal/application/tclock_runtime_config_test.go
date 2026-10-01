package application

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

func validClockRuntimeInput() ClockRuntimeConfigInput {
	public, _, _ := ed25519.GenerateKey(nil)
	return ClockRuntimeConfigInput{
		TimeDatabaseURL:              "postgres://time:time-secret@127.0.0.1:5432/hcm_time",
		TimeSchema:                   "hcmnext_time",
		CoreDatabaseURL:              "postgres://core:core-secret@127.0.0.1:5432/hcm_next",
		WorkerTokenKey:               strings.Repeat("worker-key-", 4),
		SignedRegistryPath:           "definitions/time/registry.json",
		SignedRegistryKey:            base64.StdEncoding.EncodeToString(public),
		SignedRegistryPinnedRevision: "r1",
	}
}

func TestParseClockRuntimeConfig_requiresExplicitTrustedInputs(t *testing.T) {
	input := validClockRuntimeInput()
	cfg, err := ParseClockRuntimeConfig(input)
	if err != nil {
		t.Fatalf("ParseClockRuntimeConfig() error = %v", err)
	}
	if len(cfg.SignedRegistryKey) != ed25519.PublicKeySize || len(cfg.WorkerTokenKey) < clockWorkerTokenMinimumBytes {
		t.Fatalf("parsed key lengths = registry %d, worker %d", len(cfg.SignedRegistryKey), len(cfg.WorkerTokenKey))
	}
	input.SignedRegistryKey = ""
	if _, err := ParseClockRuntimeConfig(input); err == nil {
		t.Fatal("empty registry key unexpectedly accepted")
	}
}

func TestClockRuntimeConfig_rejectsCredentialReuseAndMissingFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ClockRuntimeConfigInput)
	}{
		{name: "same database credential", mutate: func(in *ClockRuntimeConfigInput) { in.CoreDatabaseURL = in.TimeDatabaseURL }},
		{name: "weak worker key", mutate: func(in *ClockRuntimeConfigInput) { in.WorkerTokenKey = "short" }},
		{name: "missing schema", mutate: func(in *ClockRuntimeConfigInput) { in.TimeSchema = "" }},
		{name: "missing registry path", mutate: func(in *ClockRuntimeConfigInput) { in.SignedRegistryPath = "" }},
		{name: "missing pinned revision", mutate: func(in *ClockRuntimeConfigInput) { in.SignedRegistryPinnedRevision = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validClockRuntimeInput()
			tc.mutate(&input)
			if _, err := ParseClockRuntimeConfig(input); err == nil {
				t.Fatal("configuration unexpectedly accepted")
			}
		})
	}
}

func TestClockRuntimeConfig_rejectsMalformedRegistryKey(t *testing.T) {
	input := validClockRuntimeInput()
	input.SignedRegistryKey = base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize-1))
	_, err := ParseClockRuntimeConfig(input)
	if err == nil || !strings.Contains(err.Error(), "signed registry key") {
		t.Fatalf("error = %v, want signed registry key error", err)
	}
}

func TestClockRuntimeConfig_validateZeroValue(t *testing.T) {
	if err := (ClockRuntimeConfig{}).Validate(); err == nil {
		t.Fatal("zero configuration unexpectedly valid")
	}
}

func TestValidateClockRuntimeConfig_disabledSchemaDefaultDoesNotEnable(t *testing.T) {
	if err := ValidateClockRuntimeConfig(ClockRuntimeConfigInput{TimeSchema: "hcmnext_time"}, "dev", "page", "oidc", "chat"); err != nil {
		t.Fatalf("schema-only default enabled clock runtime: %v", err)
	}
	if err := ValidateClockRuntimeConfig(ClockRuntimeConfigInput{CoreDatabaseURL: "postgres://core:secret@127.0.0.1:5432/hcm_next"}, "dev", "page", "oidc", "chat"); err != nil {
		t.Fatalf("core DSN metadata enabled clock runtime: %v", err)
	}
}

func TestServeConfigValidate_localDevDefaultLeavesClockDisabled(t *testing.T) {
	cfg := ServeConfig{
		Profile:       ServeProfileLocalDev,
		GRPCListen:    "127.0.0.1:8443",
		HTTPListen:    "127.0.0.1:8080",
		DatabaseURL:   "postgres://postgres:postgres@127.0.0.1:5432/hcm_next?sslmode=disable",
		DevHMACKey:    strings.Repeat("d", 32),
		PageCursorKey: strings.Repeat("p", 32),
		OTelExporter:  OTelExporterNone,
		ClockRuntime:  ClockRuntimeConfigInput{TimeSchema: "hcmnext_time"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("local-dev default unexpectedly enabled clock runtime: %v", err)
	}
}

func TestValidateClockRuntimeConfig_rejectsInvalidDSNAndSameUser(t *testing.T) {
	input := validClockRuntimeInput()
	input.TimeDatabaseURL = "not a postgres URL"
	if err := ValidateClockRuntimeConfig(input, "dev", "page", "oidc", "chat"); err == nil {
		t.Fatal("invalid time DSN unexpectedly accepted")
	}
	input = validClockRuntimeInput()
	input.TimeDatabaseURL = "postgres://same:time-secret@127.0.0.1:5432/hcm_time"
	input.CoreDatabaseURL = "postgres://same:core-secret@127.0.0.1:5432/hcm_next"
	if err := ValidateClockRuntimeConfig(input, "dev", "page", "oidc", "chat"); err == nil {
		t.Fatal("same database user unexpectedly accepted")
	}
}

func TestValidateClockRuntimeConfig_rejectsSigningKeyReuse(t *testing.T) {
	input := validClockRuntimeInput()
	if err := ValidateClockRuntimeConfig(input, input.WorkerTokenKey, "page", "oidc", "chat"); err == nil {
		t.Fatal("worker key reused as dev key unexpectedly accepted")
	}
	if err := ValidateClockRuntimeConfig(input, "dev", input.WorkerTokenKey, "oidc", "chat"); err == nil {
		t.Fatal("worker key reused as page key unexpectedly accepted")
	}
	if err := ValidateClockRuntimeConfig(input, "dev", "page", "oidc", "chat", input.WorkerTokenKey); err == nil {
		t.Fatal("worker key reused as retired page key unexpectedly accepted")
	}
}

func TestServeConfigValidate_usesActualCoreDatabaseURL(t *testing.T) {
	input := validClockRuntimeInput()
	input.CoreDatabaseURL = "postgres://other:secret@127.0.0.1:5432/other"
	cfg := ServeConfig{
		DatabaseURL:   "postgres://time:actual-secret@127.0.0.1:5432/hcm_next",
		DevHMACKey:    strings.Repeat("d", 32),
		PageCursorKey: strings.Repeat("p", 32),
		OTelExporter:  OTelExporterNone,
		ClockRuntime:  input,
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "distinct core credential") {
		t.Fatalf("ServeConfig.Validate() error = %v, want actual core credential isolation error", err)
	}
}

func TestClockConfigFields_declaresSecretsAndSchemaDefault(t *testing.T) {
	fields := ClockConfigFields()
	if len(fields) != 6 {
		t.Fatalf("clock field count = %d, want 6", len(fields))
	}
	seen := map[string]bool{}
	for _, field := range fields {
		seen[field.Name] = true
	}
	for _, name := range []string{FieldTimeDatabaseURL, FieldTimeSchema, FieldTimeWorkerTokenKey, FieldTimeRegistryPath, FieldTimeRegistryKey, FieldTimeRegistryRevision} {
		if !seen[name] {
			t.Fatalf("missing clock field %q", name)
		}
	}
	if fields[1].Default != "hcmnext_time" {
		t.Fatalf("schema default = %q, want hcmnext_time", fields[1].Default)
	}
}

func TestClockRuntimeConfig_loadsPinnedSignedRegistry(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	payload := clockdomain.SignedRegistryPayload{
		Schema: clockdomain.ProfileRegistryVersion, Revision: "r1",
		ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour),
		Profiles: []clockdomain.SignedProfile{{
			Class: clockdomain.SourceDevicePushHTTP, Transport: "HTTPS_PUSH", Authentication: "DEVICE_CERT",
			TrustCeiling: clockdomain.TrustCeilingMedium, PermittedMethods: []clockdomain.IdentificationMethod{clockdomain.MethodBadge},
			OfflineLimit: "24h", ClockTrustRequired: true, FirstPartner: "test", Version: "v1",
		}},
	}
	data, err := clockdomain.SignSignedRegistry(payload, private, "test")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	input := validClockRuntimeInput()
	input.SignedRegistryPath = path
	input.SignedRegistryKey = base64.StdEncoding.EncodeToString(public)
	cfg, err := ParseClockRuntimeConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := cfg.LoadSignedRegistry(now)
	if err != nil {
		t.Fatalf("LoadSignedRegistry() error = %v", err)
	}
	if verified.Revision != "r1" {
		t.Fatalf("revision = %q, want r1", verified.Revision)
	}
}
