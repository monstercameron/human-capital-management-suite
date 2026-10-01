package application

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const clockWorkerTokenMinimumBytes = 32

const (
	EnvTimeDatabaseURL        = "HCMNEXT_TIME_DATABASE_URL"
	EnvTimeWorkerTokenKey     = "HCMNEXT_TIME_WORKER_TOKEN_KEY"
	EnvTimeRegistryPath       = "HCMNEXT_TIME_REGISTRY_PATH"
	EnvTimeRegistryKey        = "HCMNEXT_TIME_REGISTRY_KEY"
	EnvTimeRegistryRevision   = "HCMNEXT_TIME_REGISTRY_REVISION"
	FieldTimeDatabaseURL      = "time-database-url"
	FieldTimeSchema           = "time-schema"
	FieldTimeWorkerTokenKey   = "time-worker-token-key"
	FieldTimeRegistryPath     = "time-registry-path"
	FieldTimeRegistryKey      = "time-registry-key"
	FieldTimeRegistryRevision = "time-registry-revision"
)

const maxClockRegistryBytes = 1 << 20

// ClockRuntimeConfig is the trusted, parsed configuration required to compose
// the production clock device service. Every credential and registry input is
// explicit; the zero value is intentionally not a runnable configuration.
type ClockRuntimeConfig struct {
	TimeDatabaseURL              string
	TimeSchema                   string
	CoreDatabaseURL              string
	WorkerTokenKey               []byte
	SignedRegistryPath           string
	SignedRegistryKey            ed25519.PublicKey
	SignedRegistryPinnedRevision string
}

// ClockRuntimeConfigInput is the string-valued boundary populated by command
// flags and environment variables. The registry public key is standard
// base64, while the worker token key is an opaque secret string.
type ClockRuntimeConfigInput struct {
	TimeDatabaseURL              string
	TimeSchema                   string
	CoreDatabaseURL              string
	WorkerTokenKey               string
	SignedRegistryPath           string
	SignedRegistryKey            string
	SignedRegistryPinnedRevision string
}

// ClockConfigFields returns the optional clock fields accepted by the serve
// role. The schema default alone does not enable clock composition.
func ClockConfigFields() []bootstrap.Field {
	return []bootstrap.Field{
		{Name: FieldTimeDatabaseURL, Env: EnvTimeDatabaseURL, Usage: "dedicated PostgreSQL connection URL for clock persistence", Secret: true},
		{Name: FieldTimeSchema, Usage: "PostgreSQL schema for clock persistence", Default: timestore.SchemaName},
		{Name: FieldTimeWorkerTokenKey, Env: EnvTimeWorkerTokenKey, Usage: "dedicated HMAC key for short-lived clock worker tokens; at least 32 bytes", Secret: true},
		{Name: FieldTimeRegistryPath, Env: EnvTimeRegistryPath, Usage: "signed clock profile registry path"},
		{Name: FieldTimeRegistryKey, Env: EnvTimeRegistryKey, Usage: "standard-base64 Ed25519 public key for the signed clock profile registry", Secret: true},
		{Name: FieldTimeRegistryRevision, Env: EnvTimeRegistryRevision, Usage: "pinned signed clock profile registry revision"},
	}
}

func (i ClockRuntimeConfigInput) enabled() bool {
	return strings.TrimSpace(i.TimeDatabaseURL) != "" ||
		strings.TrimSpace(i.WorkerTokenKey) != "" || strings.TrimSpace(i.SignedRegistryPath) != "" ||
		strings.TrimSpace(i.SignedRegistryKey) != "" || strings.TrimSpace(i.SignedRegistryPinnedRevision) != ""
}

// ParseClockRuntimeConfig parses and validates the production clock settings.
// It has no development fallback and performs no I/O.
func ParseClockRuntimeConfig(input ClockRuntimeConfigInput) (ClockRuntimeConfig, error) {
	cfg := ClockRuntimeConfig{
		TimeDatabaseURL:              strings.TrimSpace(input.TimeDatabaseURL),
		TimeSchema:                   strings.TrimSpace(input.TimeSchema),
		CoreDatabaseURL:              strings.TrimSpace(input.CoreDatabaseURL),
		WorkerTokenKey:               []byte(input.WorkerTokenKey),
		SignedRegistryPath:           strings.TrimSpace(input.SignedRegistryPath),
		SignedRegistryPinnedRevision: strings.TrimSpace(input.SignedRegistryPinnedRevision),
	}
	if strings.TrimSpace(input.SignedRegistryKey) == "" {
		return ClockRuntimeConfig{}, errors.New("clock runtime: signed registry key is required")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(input.SignedRegistryKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return ClockRuntimeConfig{}, fmt.Errorf("clock runtime: signed registry key must be a %d-byte standard-base64 Ed25519 public key", ed25519.PublicKeySize)
	}
	cfg.SignedRegistryKey = append(ed25519.PublicKey(nil), key...)
	if err := cfg.Validate(); err != nil {
		return ClockRuntimeConfig{}, err
	}
	return cfg, nil
}

// ValidateClockRuntimeConfig validates the optional clock configuration in a
// serve config. A schema default by itself leaves clock composition disabled.
func ValidateClockRuntimeConfig(input ClockRuntimeConfigInput, devHMACKey, pageCursorKey, oidcSessionKey, chatCursorKey string, retiredPageCursorKey ...string) error {
	if !input.enabled() {
		return nil
	}
	if input.WorkerTokenKey == devHMACKey || input.WorkerTokenKey == pageCursorKey || input.WorkerTokenKey == oidcSessionKey || input.WorkerTokenKey == chatCursorKey || (len(retiredPageCursorKey) > 0 && input.WorkerTokenKey == retiredPageCursorKey[0]) {
		return errors.New("clock runtime: worker token key must be distinct from every other signing key")
	}
	if err := validateClockDatabaseURLs(input.TimeDatabaseURL, input.CoreDatabaseURL); err != nil {
		return err
	}
	_, err := ParseClockRuntimeConfig(input)
	return err
}

// Validate rejects incomplete or mixed-credential clock configuration before
// any database pool, listener, or service is constructed.
func (c ClockRuntimeConfig) Validate() error {
	if c.TimeDatabaseURL == "" {
		return errors.New("clock runtime: time database URL is required")
	}
	if c.CoreDatabaseURL == "" {
		return errors.New("clock runtime: core database URL is required")
	}
	if err := validateClockDatabaseURLs(c.TimeDatabaseURL, c.CoreDatabaseURL); err != nil {
		return err
	}
	if !validClockSchema(c.TimeSchema) {
		return errors.New("clock runtime: time schema must be a PostgreSQL identifier")
	}
	if sameDatabaseCredential(c.TimeDatabaseURL, c.CoreDatabaseURL) {
		return errors.New("clock runtime: time database must use a distinct core credential")
	}
	if len(c.WorkerTokenKey) < clockWorkerTokenMinimumBytes {
		return fmt.Errorf("clock runtime: worker token key must be at least %d bytes", clockWorkerTokenMinimumBytes)
	}
	if strings.TrimSpace(c.SignedRegistryPath) == "" || filepath.Clean(c.SignedRegistryPath) == "." {
		return errors.New("clock runtime: signed registry path is required")
	}
	if len(c.SignedRegistryKey) != ed25519.PublicKeySize {
		return fmt.Errorf("clock runtime: signed registry key must be %d bytes", ed25519.PublicKeySize)
	}
	if c.SignedRegistryPinnedRevision == "" {
		return errors.New("clock runtime: signed registry pinned revision is required")
	}
	return nil
}

// NewTimeStore opens the dedicated clock persistence store after validation.
func (c ClockRuntimeConfig) NewTimeStore(ctx context.Context) (*timestore.Store, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return timestore.New(ctx, timestore.Config{DSN: c.TimeDatabaseURL, CoreDSN: c.CoreDatabaseURL, Schema: c.TimeSchema})
}

// LoadSignedRegistry reads and verifies the pinned clock profile registry.
func (c ClockRuntimeConfig) LoadSignedRegistry(now time.Time) (clockdomain.VerifiedProfileRegistry, error) {
	if err := c.Validate(); err != nil {
		return clockdomain.VerifiedProfileRegistry{}, err
	}
	file, err := os.Open(c.SignedRegistryPath)
	if err != nil {
		return clockdomain.VerifiedProfileRegistry{}, fmt.Errorf("clock runtime: read signed registry: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxClockRegistryBytes+1))
	if err != nil {
		return clockdomain.VerifiedProfileRegistry{}, fmt.Errorf("clock runtime: read signed registry: %w", err)
	}
	if len(data) > maxClockRegistryBytes {
		return clockdomain.VerifiedProfileRegistry{}, fmt.Errorf("clock runtime: signed registry exceeds %d bytes", maxClockRegistryBytes)
	}
	return clockdomain.VerifySignedProfileRegistry(data, c.SignedRegistryKey, now, c.SignedRegistryPinnedRevision)
}

func validClockSchema(schema string) bool {
	if len(schema) == 0 || len(schema) > 63 {
		return false
	}
	for i, r := range schema {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func sameDatabaseCredential(a, b string) bool {
	ca, ea := pgconn.ParseConfig(a)
	cb, eb := pgconn.ParseConfig(b)
	if ea != nil || eb != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return ca.User == cb.User
}

func validateClockDatabaseURLs(timeURL, coreURL string) error {
	if strings.TrimSpace(timeURL) == "" || strings.TrimSpace(coreURL) == "" {
		return errors.New("clock runtime: time and core database URLs are required")
	}
	if _, err := pgconn.ParseConfig(timeURL); err != nil {
		return fmt.Errorf("clock runtime: invalid time database URL: %w", err)
	}
	if _, err := pgconn.ParseConfig(coreURL); err != nil {
		return fmt.Errorf("clock runtime: invalid core database URL: %w", err)
	}
	if sameDatabaseCredential(timeURL, coreURL) {
		return errors.New("clock runtime: time database must use a distinct core credential")
	}
	return nil
}
