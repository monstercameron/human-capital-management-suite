package application

// This file is the ARCH-GO-020 configuration contract for the application
// composition root. It is deliberately separate from the wiring in serve.go:
// a composition root that reads flags while it constructs adapters has no
// point at which the configuration is "validated", and every later component
// then has to re-decide what an empty string meant.
//
// The rule this file encodes is that a role is composed from a *value*. The
// command parses arguments into bootstrap.Values, ValidateServeValues rejects
// a configuration a listener must not start on, ServeConfigFromValues turns
// what survived into a ServeConfig, and every constructor downstream reads
// that struct. Nothing downstream reads a flag, an environment variable or a
// package-level default.

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile"
)

// EnvDatabaseURL names the server the serve role connects to, matching
// cmd/migrate's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

// EnvDevHMACKey carries the development signing key, so it need not appear in
// a process listing.
const EnvDevHMACKey = "HCMNEXT_DEV_HMAC_KEY"

// EnvHealthAddr is the loopback host:port the serve role's health and
// readiness endpoint listens on when -health-addr is not passed.
const EnvHealthAddr = "HCMNEXT_HEALTH_ADDR"

// EnvLegalEvidenceIssuerKeys carries the deployment's explicit legal
// evidence issuer allowlist.
const EnvLegalEvidenceIssuerKeys = "HCMNEXT_LEGAL_EVIDENCE_ISSUER_KEYS"

// EnvPublicOrigin carries the absolute origin the cell is publicly reached
// at, for deployments behind a TLS-terminating or Host-rewriting proxy.
const EnvPublicOrigin = "HCMNEXT_PUBLIC_ORIGIN"

// Configuration field names. They are constants because ServeConfigFields
// declares them and ServeConfigFromValues reads them back: a typo between the
// two is a startup failure rather than a silently defaulted value.
const (
	FieldProfile         = "profile"
	FieldGRPCListen      = "grpc-listen"
	FieldHTTPListen      = "http-listen"
	FieldDatabaseURL     = "database-url"
	FieldDevHMACKey      = "dev-hmac-key"
	FieldIssuer          = "issuer"
	FieldAudience        = "audience"
	FieldTenant          = "tenant"
	FieldCellID          = "cell-id"
	FieldMaxDeadline     = "max-deadline"
	FieldMigrate         = "migrate"
	FieldWorkspace       = "workspace"
	FieldDevBrowserLogin = "dev-browser-login"
	FieldOTelExporter    = "otel-exporter"
	FieldOTelEndpoint    = "otel-endpoint"

	// FieldExecutionAuthority is the P1B execution authority gate family.
	// Every field below defaults to off/empty; a cell composed with no
	// -execution-authority behaves byte-for-byte like the P1A cell of today
	// (internal/intent/app.ExecutionAuthority is nil, and ExecuteIntent
	// refuses exactly as every other governed write in this release does).
	FieldExecutionAuthority               = "execution-authority"
	FieldExecutionAuthorityDigest         = "execution-authority-digest"
	FieldExecutionAuthorityRole           = "execution-authority-role"
	FieldExecutionApprover                = "execution-authority-approver"
	FieldExecutionManagerApprover         = "execution-authority-manager-approver"
	FieldExecutionFinancePartner          = "execution-finance-partner"
	FieldTimerTzdbVersion                 = "timer-tzdb-version"
	FieldTimerCalendarVersion             = "timer-calendar-version"
	FieldScheduler                        = "scheduler"
	FieldHealthAddr                       = "health-addr"
	FieldWorkflowPlan                     = "workflow-plan"
	FieldLegalEvidenceIssuerKeys          = "legal-evidence-issuer-keys"
	FieldExecutionRetry                   = "execution-retry"
	FieldExecutionRetryVersion            = "execution-retry-version"
	FieldExecutionRetryMaxAttempts        = "execution-retry-max-attempts"
	FieldExecutionRetryResolutionAttempts = "execution-retry-resolution-attempts"
	FieldPublicOrigin                     = "public-origin"
	FieldLocalDevNow                      = "local-dev-now"
)

// Serve profiles are named sets of defaults, not alternate implementations.
// The standard profile remains the fail-closed deployment shape. local-dev
// keeps the real database, authentication, authorization and transports, but
// removes repetitive setup from a loopback-only developer process.
const (
	ServeProfileStandard = "standard"
	ServeProfileLocalDev = devprofile.Name

	LocalDevDatabaseURL = "postgres://postgres:postgres@127.0.0.1:5432/hcm_next?sslmode=disable"
	LocalDevHMACKey     = devprofile.HMACKey
	LocalDevTenant      = devprofile.Tenant
	LocalDevSubject     = devprofile.Subject
	LocalDevOrgScope    = devprofile.OrgScope
	LocalDevRoles       = devprofile.Roles
	LocalDevPurpose     = devprofile.Purpose
	// LocalDevFinancePartner is the local-dev profile's default
	// -execution-finance-partner: HarborCare's Finance Director worker, the
	// finance partner the demo tenant's promotion approvals route to
	// (PROMOUX-015). It is a profile default, never a literal in routing logic.
	LocalDevFinancePartner = "hc-054-thomas-baker"
)

const (
	WorkflowPlanPrototype = "prototype"
	WorkflowPlanExecute   = "execute"
)

// OTelExporterNone, OTelExporterStdout and OTelExporterOTLPHTTP are the
// allowed values of -otel-exporter. OTelExporterNone is the default: a
// listener started with no telemetry flag at all publishes no spans or
// metrics, rather than exporting to stdout by surprise.
const (
	OTelExporterNone     = "none"
	OTelExporterStdout   = "stdout"
	OTelExporterOTLPHTTP = "otlphttp"
)

// DefaultIssuer and DefaultAudience are the serve role's own
// -issuer/-audience defaults. cmd/hcmnext's token command shares these same
// constants for its own defaults, so a credential minted with no flags beyond
// -dev-hmac-key/-tenant/-subject verifies against a serve process started
// with no flags beyond its own -dev-hmac-key: the two commands cannot drift
// apart by one of them changing a literal the other did not.
const (
	DefaultIssuer   = devprofile.Issuer
	DefaultAudience = devprofile.Audience
)

// DefaultTimerTzdbVersion and DefaultTimerCalendarVersion are the dataset
// releases a serve process composes its durable timers against when the
// deployment names none. They match the releases the workflow WAIT fixtures
// pin (test/workflow), so a plan compiled against the reference dataset
// parks and resumes on this process without a flag; a deployment on a newer
// tzdb or calendar release sets both flags explicitly.
const (
	DefaultTimerTzdbVersion     = "2026a"
	DefaultTimerCalendarVersion = "2026.1"
)

// MinimumHMACKeyBytes is the shortest development signing key this listener
// will start with.
const MinimumHMACKeyBytes = 32

// ShutdownGrace bounds the whole ordered shutdown sequence.
const ShutdownGrace = 20 * time.Second

// TelemetryShutdownGrace bounds only flushing the process-local OTel
// provider. It remains inside the whole service shutdown deadline above, and
// failed export is intentionally reported without changing the process's
// business shutdown outcome.
const TelemetryShutdownGrace = 5 * time.Second

// ServeConfig is the validated configuration one serve role is composed
// from. It is a plain value with no behaviour and no defaults applied lazily
// at read time: ServeConfigFromValues resolves every field once, and the
// composition reads only this struct afterwards.
type ServeConfig struct {
	Profile     string
	GRPCListen  string
	HTTPListen  string
	DatabaseURL string
	// DevHMACKey is the development signing key. It is carried, never
	// logged: bootstrap.Field marks it Secret so the config fingerprint and
	// the startup log attributes redact it.
	DevHMACKey      string
	Issuer          string
	Audience        string
	Tenant          string
	CellID          string
	MaxDeadline     time.Duration
	Migrate         bool
	Workspace       bool
	DevBrowserLogin bool
	OTelExporter    string
	OTelEndpoint    string

	ExecutionAuthority       bool
	ExecutionAuthorityDigest string
	ExecutionAuthorityRole   string
	ExecutionApprover        string
	ExecutionManagerApprover string
	// ExecutionFinancePartner is the principal the executable promotion
	// plan's finance approval routes to (PROMOUX-015). Empty keeps the
	// class-scoped derivation of ExecutionApprover.
	ExecutionFinancePartner string
	// TimerTzdbVersion and TimerCalendarVersion are the dataset releases the
	// execution driver's durable timers resolve wake instants against
	// (WF-RUN-004). Both set composes the timer ports; both empty composes
	// none; one of the two set is refused by Validate.
	TimerTzdbVersion                 string
	TimerCalendarVersion             string
	Scheduler                        bool
	WorkflowPlan                     string
	LegalEvidenceIssuerKeys          string
	ExecutionRetry                   bool
	ExecutionRetryVersion            string
	ExecutionRetryMaxAttempts        int
	ExecutionRetryResolutionAttempts int
	// HealthAddr is the loopback host:port the bootstrap health and
	// readiness endpoint listens on (STARTING, READY, DRAINING as
	// {"state":...}); empty serves none. It is a separate listener from the
	// HTTP edge on purpose: a probe must answer while the edge is draining.
	HealthAddr string
	// PublicOrigin is the canonical http(s) origin this cell is publicly
	// reached at, canonicalized by ServeConfigFromValues. Empty means
	// browsers reach this listener directly and every browser-facing
	// authority is derived from each request, which is the localhost and
	// bare-VPS default. Set it for complex deployments: a proxy that
	// terminates TLS or rewrites Host, a tunnel, a preview gateway.
	PublicOrigin string
	// LocalDevNow pins the application clock used by authentication, workflow
	// execution and scheduling; the database keeps its own physical audit clock.
	// It is accepted only by the loopback-only local-dev profile and can never
	// override production time.
	LocalDevNow string
}

// ServeConfigFields declares every flag/env-backed configuration value the
// serve role accepts. It is the single declaration: the command does not
// add, rename or re-default one.
func ServeConfigFields() []bootstrap.Field {
	return []bootstrap.Field{
		{Name: FieldProfile, Usage: "runtime default profile: standard or local-dev", Default: ServeProfileStandard},
		{Name: FieldGRPCListen, Usage: "address the canonical gRPC surface listens on", Default: "127.0.0.1:8443"},
		{Name: FieldHTTPListen, Usage: "address the HTTP edge listens on", Default: "127.0.0.1:8080"},
		{Name: FieldDatabaseURL, Env: EnvDatabaseURL, Usage: "PostgreSQL connection URL"},
		{Name: FieldDevHMACKey, Env: EnvDevHMACKey, Usage: "development HMAC signing key, at least 32 bytes", Secret: true},
		{Name: FieldIssuer, Usage: "the only credential issuer this listener accepts", Default: DefaultIssuer},
		{Name: FieldAudience, Usage: "the audience this listener answers to", Default: DefaultAudience},
		{Name: FieldTenant, Usage: "tenant slug to register on start; empty registers none"},
		{Name: FieldCellID, Usage: "cell identifier a registered tenant is bound to", Default: "cell-local"},
		{Name: FieldMaxDeadline, Usage: "server-imposed cap on every request deadline", Default: "30s", Kind: bootstrap.KindDuration},
		{Name: FieldMigrate, Usage: "apply pending migrations before the listeners start", Default: "true", Kind: bootstrap.KindBool},
		{Name: FieldWorkspace, Usage: "serve the human-facing Promotion workspace on the HTTP edge", Default: "true", Kind: bootstrap.KindBool},
		{Name: FieldDevBrowserLogin, Usage: "dev-only: serve a pasted-token sign-in form for the workspace at " + workspace.PathLogin, Default: "false", Kind: bootstrap.KindBool},
		{Name: FieldOTelExporter, Usage: "OTel exporter: none, stdout, or otlphttp", Default: OTelExporterNone},
		{Name: FieldOTelEndpoint, Usage: "OTLP/HTTP collector endpoint; required when -" + FieldOTelExporter + "=" + OTelExporterOTLPHTTP},
		{Name: FieldExecutionAuthority, Usage: "P1B gate: compose this cell with the caller-driven promotion execution driver, so ExecuteIntent can run instead of refusing (planning/next-steps.md \"P1B exists only after a signed Gate A PROCEED\")", Default: "false", Kind: bootstrap.KindBool},
		{Name: FieldExecutionAuthorityDigest, Usage: "the signed P1B authority amendment digest this cell asserts; carried through as evidence, never verified by this process"},
		{Name: FieldExecutionAuthorityRole, Usage: "the principal role ExecuteIntent additionally requires under -" + FieldExecutionAuthority, Default: "promotion_operator"},
		{Name: FieldExecutionApprover, Usage: "the principal the composed promotion workflow routes its finance approval WorkItem to", Default: "principal:promotion-approver"},
		{Name: FieldExecutionManagerApprover, Usage: "the distinct principal the composed execute promotion workflow routes its current-manager approval WorkItem to", Default: "principal:promotion-manager-approver"},
		{Name: FieldExecutionFinancePartner, Usage: "the principal the executable promotion plan's FinancePartnerFor(cost_center) approval routes to; empty derives a class-scoped identity from -" + FieldExecutionApprover},
		{Name: FieldTimerTzdbVersion, Usage: "tzdb release the execution driver's durable timers resolve wake instants against; with -" + FieldTimerCalendarVersion + " it composes the WAIT-node timer ports, empty composes none", Default: DefaultTimerTzdbVersion},
		{Name: FieldTimerCalendarVersion, Usage: "business-calendar release the execution driver's durable timers resolve wake instants against", Default: DefaultTimerCalendarVersion},
		{Name: FieldScheduler, Usage: "run the in-process workflow timer/ready-work dispatcher", Default: "false", Kind: bootstrap.KindBool},
		{Name: FieldHealthAddr, Env: EnvHealthAddr, Usage: "loopback host:port (127.0.0.1, localhost or ::1) to serve the health/readiness endpoint on; empty disables it"},
		{Name: FieldWorkflowPlan, Usage: "promotion workflow plan: prototype or execute", Default: WorkflowPlanPrototype},
		{Name: FieldLegalEvidenceIssuerKeys, Env: EnvLegalEvidenceIssuerKeys, Usage: "comma-separated standard-base64 public keys trusted for governed legal evidence"},
		{Name: FieldExecutionRetry, Usage: "enable persisted admission for execution START retries", Default: "false", Kind: bootstrap.KindBool},
		{Name: FieldExecutionRetryVersion, Usage: "persisted execution retry budget version; required when retries are enabled"},
		{Name: FieldExecutionRetryMaxAttempts, Usage: "maximum execution START attempts including the initial attempt; required when retries are enabled", Default: "1", Kind: bootstrap.KindInt},
		{Name: FieldExecutionRetryResolutionAttempts, Usage: "maximum bounded attempts to resolve uncertain retry consumption", Default: "2", Kind: bootstrap.KindInt},
		{Name: FieldPublicOrigin, Env: EnvPublicOrigin, Usage: "absolute http(s) origin (e.g. https://hcm.example.com) browsers reach this cell at; required behind a TLS-terminating or Host-rewriting proxy"},
		{Name: FieldLocalDevNow, Usage: "local-dev only: pin the application clock to an RFC3339 instant so future effective-date workflows can be completed safely"},
	}
}

// ServeConfigFieldsForArgs returns the same declared field set with the
// requested profile's defaults applied. Profile defaults remain below both
// environment values and explicit flags in bootstrap's normal precedence.
func ServeConfigFieldsForArgs(args []string) []bootstrap.Field {
	fields := ServeConfigFields()
	if requestedServeProfile(args) != ServeProfileLocalDev {
		return fields
	}
	defaults := map[string]string{
		FieldDatabaseURL:              LocalDevDatabaseURL,
		FieldDevHMACKey:               LocalDevHMACKey,
		FieldTenant:                   LocalDevTenant,
		FieldMigrate:                  "false",
		FieldDevBrowserLogin:          "true",
		FieldExecutionAuthority:       "true",
		FieldExecutionAuthorityDigest: "sha256:local-dev-profile-authority",
		FieldExecutionApprover:        "hc-054-thomas-baker",
		FieldExecutionManagerApprover: "hc-052-dominic-collins",
		// The executable plan contains a durable effective-date WAIT. Leaving
		// its dispatcher disabled produces a half-enabled development profile:
		// approvals succeed and a due-today timer is written, but nothing is
		// present to settle and resume it. Keep the real timer path in local
		// development and start its bounded in-process scheduler with the plan.
		FieldScheduler:    "true",
		FieldWorkflowPlan: WorkflowPlanExecute,
		// PROMOUX-015: the demo tenant's finance approvals route to its
		// Finance Director, so a local promotion is approved by real personas.
		FieldExecutionFinancePartner: LocalDevFinancePartner,
	}
	for i := range fields {
		if value, ok := defaults[fields[i].Name]; ok {
			fields[i].Default = value
		}
	}
	return fields
}

// requestedServeProfile performs only enough parsing to select defaults.
// bootstrap.ParseConfig remains the sole parser and reports malformed or
// duplicate inputs in the usual way.
func requestedServeProfile(args []string) string {
	profile := ServeProfileStandard
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-"+FieldProfile || arg == "--"+FieldProfile:
			if i+1 < len(args) {
				profile = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "-"+FieldProfile+"="):
			profile = strings.TrimPrefix(arg, "-"+FieldProfile+"=")
		case strings.HasPrefix(arg, "--"+FieldProfile+"="):
			profile = strings.TrimPrefix(arg, "--"+FieldProfile+"=")
		}
	}
	return profile
}

// ServeConfigFromValues resolves parsed configuration into the value the
// composition reads. It returns only the typed-parse failures
// bootstrap.Values reports; semantic rejection is ServeConfig.Validate's job,
// so the two cannot disagree about what "valid" means.
func ServeConfigFromValues(values *bootstrap.Values) (ServeConfig, error) {
	if values == nil {
		return ServeConfig{}, fmt.Errorf("application: serve configuration needs parsed values")
	}
	cfg := ServeConfig{
		Profile:                  values.String(FieldProfile),
		GRPCListen:               values.String(FieldGRPCListen),
		HTTPListen:               values.String(FieldHTTPListen),
		DatabaseURL:              values.String(FieldDatabaseURL),
		DevHMACKey:               values.String(FieldDevHMACKey),
		Issuer:                   values.String(FieldIssuer),
		Audience:                 values.String(FieldAudience),
		Tenant:                   values.String(FieldTenant),
		CellID:                   values.String(FieldCellID),
		OTelExporter:             values.String(FieldOTelExporter),
		OTelEndpoint:             values.String(FieldOTelEndpoint),
		ExecutionAuthorityDigest: values.String(FieldExecutionAuthorityDigest),
		ExecutionAuthorityRole:   values.String(FieldExecutionAuthorityRole),
		ExecutionApprover:        values.String(FieldExecutionApprover),
		ExecutionManagerApprover: values.String(FieldExecutionManagerApprover),
		ExecutionFinancePartner:  values.String(FieldExecutionFinancePartner),
		TimerTzdbVersion:         values.String(FieldTimerTzdbVersion),
		TimerCalendarVersion:     values.String(FieldTimerCalendarVersion),
		HealthAddr:               values.String(FieldHealthAddr),
		WorkflowPlan:             values.String(FieldWorkflowPlan),
		LegalEvidenceIssuerKeys:  values.String(FieldLegalEvidenceIssuerKeys),
		ExecutionRetryVersion:    values.String(FieldExecutionRetryVersion),
		LocalDevNow:              values.String(FieldLocalDevNow),
	}
	var err error
	if cfg.PublicOrigin, err = canonicalPublicOrigin(values.String(FieldPublicOrigin)); err != nil {
		return ServeConfig{}, err
	}
	if cfg.MaxDeadline, err = values.Duration(FieldMaxDeadline); err != nil {
		return ServeConfig{}, err
	}
	if cfg.Migrate, err = values.Bool(FieldMigrate); err != nil {
		return ServeConfig{}, err
	}
	if cfg.Workspace, err = values.Bool(FieldWorkspace); err != nil {
		return ServeConfig{}, err
	}
	if cfg.DevBrowserLogin, err = values.Bool(FieldDevBrowserLogin); err != nil {
		return ServeConfig{}, err
	}
	if cfg.ExecutionAuthority, err = values.Bool(FieldExecutionAuthority); err != nil {
		return ServeConfig{}, err
	}
	if cfg.Scheduler, err = values.Bool(FieldScheduler); err != nil {
		return ServeConfig{}, err
	}
	if cfg.ExecutionRetry, err = values.Bool(FieldExecutionRetry); err != nil {
		return ServeConfig{}, err
	}
	if cfg.ExecutionRetryMaxAttempts, err = values.Int(FieldExecutionRetryMaxAttempts); err != nil {
		return ServeConfig{}, err
	}
	if cfg.ExecutionRetryResolutionAttempts, err = values.Int(FieldExecutionRetryResolutionAttempts); err != nil {
		return ServeConfig{}, err
	}
	return cfg, nil
}

// Validate rejects a configuration a listener must not start on. It is the
// semantic half of the contract: everything here is a statement about the
// deployment, not about whether a string parsed.
func (c ServeConfig) Validate() error {
	if _, err := parseLegalEvidenceIssuerKeys(c.LegalEvidenceIssuerKeys); err != nil {
		return err
	}
	if c.ExecutionRetry {
		if strings.TrimSpace(c.ExecutionRetryVersion) == "" || c.ExecutionRetryMaxAttempts < 2 || c.ExecutionRetryResolutionAttempts < 1 {
			return fmt.Errorf("-%s requires a version, max attempts >= 2 and resolution attempts >= 1", FieldExecutionRetry)
		}
	}
	if c.LocalDevNow != "" {
		if c.Profile != ServeProfileLocalDev {
			return fmt.Errorf("-%s is available only with -%s=%s", FieldLocalDevNow, FieldProfile, ServeProfileLocalDev)
		}
		if _, err := time.Parse(time.RFC3339, c.LocalDevNow); err != nil {
			return fmt.Errorf("-%s must be an RFC3339 instant: %w", FieldLocalDevNow, err)
		}
	}
	switch c.Profile {
	case "", ServeProfileStandard:
	case ServeProfileLocalDev:
		if err := validateLocalDevBoundary(c); err != nil {
			return err
		}
	default:
		return fmt.Errorf("-%s must be %q or %q; got %q", FieldProfile, ServeProfileStandard, ServeProfileLocalDev, c.Profile)
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("%s is not set; pass -%s or set the environment variable",
			EnvDatabaseURL, FieldDatabaseURL)
	}
	if len(c.DevHMACKey) < MinimumHMACKeyBytes {
		return fmt.Errorf("-%s must be at least %d bytes; a listener that cannot authenticate must not start",
			FieldDevHMACKey, MinimumHMACKeyBytes)
	}
	switch c.OTelExporter {
	case OTelExporterNone, OTelExporterStdout:
	case OTelExporterOTLPHTTP:
		if c.OTelEndpoint == "" {
			return fmt.Errorf("-%s is required when -%s=%s", FieldOTelEndpoint, FieldOTelExporter, OTelExporterOTLPHTTP)
		}
	default:
		return fmt.Errorf("-%s must be one of %s, %s, %s; got %q",
			FieldOTelExporter, OTelExporterNone, OTelExporterStdout, OTelExporterOTLPHTTP, c.OTelExporter)
	}
	if c.ExecutionAuthority {
		if c.ExecutionAuthorityDigest == "" {
			return fmt.Errorf("-%s is required when -%s=true", FieldExecutionAuthorityDigest, FieldExecutionAuthority)
		}
		if c.ExecutionAuthorityRole == "" {
			return fmt.Errorf("-%s is required when -%s=true", FieldExecutionAuthorityRole, FieldExecutionAuthority)
		}
		if c.ExecutionApprover == "" {
			return fmt.Errorf("-%s is required when -%s=true", FieldExecutionApprover, FieldExecutionAuthority)
		}
		if c.WorkflowPlan == WorkflowPlanExecute {
			if c.ExecutionManagerApprover != "" && c.ExecutionManagerApprover == c.ExecutionApprover {
				return fmt.Errorf("-%s must be distinct from -%s for the execute workflow plan", FieldExecutionManagerApprover, FieldExecutionApprover)
			}
		}
	}
	if c.Scheduler {
		if c.Tenant == "" {
			return fmt.Errorf("-%s requires -%s", FieldScheduler, FieldTenant)
		}
		if !c.ExecutionAuthority {
			return fmt.Errorf("-%s requires -%s=true", FieldScheduler, FieldExecutionAuthority)
		}
	}
	if c.WorkflowPlan != "" && c.WorkflowPlan != WorkflowPlanPrototype && c.WorkflowPlan != WorkflowPlanExecute {
		return fmt.Errorf("-%s must be %q or %q; got %q", FieldWorkflowPlan, WorkflowPlanPrototype, WorkflowPlanExecute, c.WorkflowPlan)
	}
	if (c.TimerTzdbVersion == "") != (c.TimerCalendarVersion == "") {
		return fmt.Errorf("-%s and -%s are set together or not at all; a timer promise names both releases",
			FieldTimerTzdbVersion, FieldTimerCalendarVersion)
	}
	return nil
}

// canonicalPublicOrigin resolves the declared public origin to its canonical
// scheme://host form. Anything that is not an absolute http or https origin
// - no scheme, a path, a query, credentials - is refused: it would still
// "work" through some proxies while silently shifting which origins the
// browser policy admits.
func canonicalPublicOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("-%s must be an absolute http(s) origin like https://hcm.example.com; got %q", FieldPublicOrigin, raw)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

func parseLegalEvidenceIssuerKeys(raw string) ([][]byte, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	keys := make([][]byte, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("-%s contains an empty key at position %d", FieldLegalEvidenceIssuerKeys, i+1)
		}
		key, err := base64.StdEncoding.Strict().DecodeString(part)
		if err != nil || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("-%s key %d must be a %d-byte Ed25519 public key encoded as standard-base64", FieldLegalEvidenceIssuerKeys, i+1, ed25519.PublicKeySize)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func validateLocalDevBoundary(c ServeConfig) error {
	for name, addr := range map[string]string{FieldGRPCListen: c.GRPCListen, FieldHTTPListen: c.HTTPListen} {
		if !devprofile.IsLoopbackAddress(addr) {
			return fmt.Errorf("-%s=%s requires -%s=%q to bind a loopback address", FieldProfile, ServeProfileLocalDev, name, addr)
		}
	}
	database, err := url.Parse(c.DatabaseURL)
	if err != nil || database.Hostname() == "" || !devprofile.IsLoopbackHost(database.Hostname()) {
		return fmt.Errorf("-%s=%s requires a loopback PostgreSQL URL", FieldProfile, ServeProfileLocalDev)
	}
	return nil
}

// TimerDataset is the dataset pair the execution driver's timers are
// composed with; the zero value when neither flag is set.
func (c ServeConfig) TimerDataset() kernelvalues.DatasetVersions {
	return kernelvalues.DatasetVersions{TzdbVersion: c.TimerTzdbVersion, CalendarVersion: c.TimerCalendarVersion}
}

// ValidateServeValues is bootstrap.Spec.Validate for the serve role. The two
// string checks run before the typed parse so the reported failure is the
// same one a reader of the previous cmd/hcmnext implementation would expect:
// a listener with no database URL is told that first, whatever else is also
// wrong.
func ValidateServeValues(values *bootstrap.Values) error {
	if values == nil {
		return fmt.Errorf("application: serve configuration needs parsed values")
	}
	if values.String(FieldDatabaseURL) == "" {
		return fmt.Errorf("%s is not set; pass -%s or set the environment variable",
			EnvDatabaseURL, FieldDatabaseURL)
	}
	if len(values.String(FieldDevHMACKey)) < MinimumHMACKeyBytes {
		return fmt.Errorf("-%s must be at least %d bytes; a listener that cannot authenticate must not start",
			FieldDevHMACKey, MinimumHMACKeyBytes)
	}
	cfg, err := ServeConfigFromValues(values)
	if err != nil {
		return err
	}
	return cfg.Validate()
}
