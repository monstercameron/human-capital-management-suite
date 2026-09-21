package application

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// testDevKey is long enough to satisfy MinimumHMACKeyBytes. It signs nothing
// outside this test binary.
const testDevKey = "application-composition-root-test-signing-key"

// parseServe parses args against the serve role's declared fields with an
// empty environment, so a case's outcome depends only on what it passed.
func parseServe(t *testing.T, args ...string) *bootstrap.Values {
	t.Helper()
	values, err := bootstrap.ParseConfig(args, func(string) (string, bool) { return "", false }, ServeConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig(%v): %v", args, err)
	}
	return values
}

// TestServeConfigFieldsDeclareEveryConfigurationTheRoleReads is the
// configuration half of "no hidden dependencies": every field the composition
// reads has to be declared, or a deployment can set it and be ignored.
func TestServeConfigFieldsDeclareEveryConfigurationTheRoleReads(t *testing.T) {
	declared := map[string]bootstrap.Field{}
	for _, field := range ServeConfigFields() {
		if _, duplicate := declared[field.Name]; duplicate {
			t.Fatalf("field %q declared twice", field.Name)
		}
		declared[field.Name] = field
	}
	for _, name := range []string{
		FieldProfile, FieldGRPCListen, FieldHTTPListen, FieldDatabaseURL, FieldDevHMACKey,
		FieldIssuer, FieldAudience, FieldTenant, FieldCellID, FieldMaxDeadline,
		FieldMigrate, FieldWorkspace, FieldDevBrowserLogin, FieldOTelExporter,
		FieldOTelEndpoint, FieldExecutionAuthority, FieldExecutionAuthorityDigest,
		FieldExecutionAuthorityRole, FieldExecutionApprover, FieldExecutionManagerApprover, FieldExecutionFinancePartner,
		FieldWorkflowPlan, FieldLegalEvidenceIssuerKeys,
		FieldExecutionRetry, FieldExecutionRetryVersion, FieldExecutionRetryMaxAttempts,
		FieldExecutionRetryResolutionAttempts, FieldPublicOrigin, FieldLocalDevNow,
	} {
		if _, ok := declared[name]; !ok {
			t.Errorf("field %q is read by the composition but not declared", name)
		}
	}
	if !declared[FieldDevHMACKey].Secret {
		t.Error("the signing key is not marked Secret; it would reach the config fingerprint and the startup log")
	}
	if declared[FieldDevHMACKey].Default != "" {
		t.Error("the signing key has a default; a listener with a default signing key is one anyone can forge against")
	}
	if declared[FieldExecutionAuthority].Default != "true" {
		t.Errorf("-%s defaults to %q, want true: promotions run through the execution engine unless opted out",
			FieldExecutionAuthority, declared[FieldExecutionAuthority].Default)
	}
	if declared[FieldWorkflowPlan].Default != WorkflowPlanExecute {
		t.Errorf("-%s defaults to %q, want %q", FieldWorkflowPlan, declared[FieldWorkflowPlan].Default, WorkflowPlanExecute)
	}
	if declared[FieldScheduler].Default != "true" {
		t.Errorf("-%s defaults to %q, want true: the execute plan's durable WAITs need the dispatcher",
			FieldScheduler, declared[FieldScheduler].Default)
	}
	if declared[FieldOTelExporter].Default != OTelExporterNone {
		t.Errorf("-%s defaults to %q, want %q", FieldOTelExporter, declared[FieldOTelExporter].Default, OTelExporterNone)
	}
}

// TestServeConfigFromValuesResolvesEveryFieldOnce proves the composition
// reads a value, not a flag: what ServeConfigFromValues returns is the whole
// input every constructor downstream sees.
func TestServeConfigFromValuesResolvesEveryFieldOnce(t *testing.T) {
	values := parseServe(t,
		"-grpc-listen=127.0.0.1:1", "-http-listen=127.0.0.1:2",
		"-database-url=postgres://x", "-dev-hmac-key="+testDevKey,
		"-issuer=https://issuer.test", "-audience=aud", "-tenant=acme",
		"-cell-id=cell-9", "-max-deadline=7s", "-migrate=false",
		"-workspace=false", "-dev-browser-login=true",
		"-otel-exporter=otlphttp", "-otel-endpoint=http://collector:4318",
		"-execution-authority=true", "-execution-authority-digest=sha256:abc",
		"-execution-authority-role=promo_op", "-execution-authority-approver=principal:approver",
		"-execution-authority-manager-approver=principal:manager-approver",
		"-timer-tzdb-version=2026b", "-timer-calendar-version=2026.2", "-health-addr=127.0.0.1:9",
		"-workflow-plan=execute",
		"-execution-retry=true", "-execution-retry-version=retry-v1",
		"-execution-retry-max-attempts=3", "-execution-retry-resolution-attempts=4",
		"-legal-evidence-issuer-keys=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"-public-origin=https://HCM.example.com:8443",
	)
	cfg, err := ServeConfigFromValues(values)
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	want := ServeConfig{
		Profile:    ServeProfileStandard,
		GRPCListen: "127.0.0.1:1", HTTPListen: "127.0.0.1:2",
		DatabaseURL: "postgres://x", DevHMACKey: testDevKey,
		Issuer: "https://issuer.test", Audience: "aud", Tenant: "acme",
		CellID: "cell-9", MaxDeadline: 7 * time.Second, Migrate: false,
		Workspace: false, DevBrowserLogin: true,
		OTelExporter: OTelExporterOTLPHTTP, OTelEndpoint: "http://collector:4318",
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:abc",
		ExecutionAuthorityRole: "promo_op", ExecutionApprover: "principal:approver",
		ExecutionManagerApprover: "principal:manager-approver",
		WorkflowPlan:             WorkflowPlanExecute, Scheduler: true,
		ExecutionRetry: true, ExecutionRetryVersion: "retry-v1",
		ExecutionRetryMaxAttempts: 3, ExecutionRetryResolutionAttempts: 4,
		LegalEvidenceIssuerKeys: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		TimerTzdbVersion:        "2026b", TimerCalendarVersion: "2026.2", HealthAddr: "127.0.0.1:9",
		PublicOrigin: "https://hcm.example.com:8443",
	}
	if cfg != want {
		t.Errorf("ServeConfigFromValues =\n %+v\nwant\n %+v", cfg, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate on a complete configuration: %v", err)
	}
}

// TestServeConfigPublicOriginIsCanonicalizedAndValidated pins the one
// parse the flag accepts: an absolute http(s) origin, canonicalized to
// lowercase scheme://host so every consumer binds the same authority.
func TestServeConfigPublicOriginIsCanonicalizedAndValidated(t *testing.T) {
	// The engine defaults need a tenant and an authority digest to
	// validate; the assertions below are about the origin parse.
	cfg, err := ServeConfigFromValues(parseServe(t,
		"-database-url=postgres://x", "-dev-hmac-key="+testDevKey,
		"-tenant=acme", "-execution-authority-digest=sha256:abc"))
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	if cfg.PublicOrigin != "" {
		t.Fatalf("default PublicOrigin = %q, want empty", cfg.PublicOrigin)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with no public origin: %v", err)
	}

	cfg, err = ServeConfigFromValues(parseServe(t,
		"-database-url=postgres://x", "-dev-hmac-key="+testDevKey,
		"-tenant=acme", "-execution-authority-digest=sha256:abc",
		"-public-origin=https://HCM.Example.com:443"))
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	if cfg.PublicOrigin != "https://hcm.example.com:443" {
		t.Fatalf("PublicOrigin = %q, want the canonical form", cfg.PublicOrigin)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with a public origin: %v", err)
	}

	for _, raw := range []string{
		"hcm.example.com", "https://", "ftp://hcm.example.com",
		"https://hcm.example.com/app", "https://user@hcm.example.com",
		"https://hcm.example.com?q=1",
	} {
		_, err := ServeConfigFromValues(parseServe(t,
			"-database-url=postgres://x", "-dev-hmac-key="+testDevKey,
			"-public-origin="+raw))
		if err == nil {
			t.Errorf("-public-origin=%q accepted", raw)
		}
	}
}

func TestExecutionRetryConfigurationFailsClosed(t *testing.T) {
	base := stubServeConfig()
	base.ExecutionRetry = true
	base.ExecutionRetryVersion = "retry-v1"
	base.ExecutionRetryMaxAttempts = 2
	base.ExecutionRetryResolutionAttempts = 1
	for name, mutate := range map[string]func(*ServeConfig){
		"missing version":       func(c *ServeConfig) { c.ExecutionRetryVersion = "" },
		"one total attempt":     func(c *ServeConfig) { c.ExecutionRetryMaxAttempts = 1 },
		"no resolution attempt": func(c *ServeConfig) { c.ExecutionRetryResolutionAttempts = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("invalid retry configuration accepted")
			}
		})
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid retry configuration rejected: %v", err)
	}
}

func TestLocalDevProfileAppliesFastSafeDefaultsAndKeepsExplicitOverrides(t *testing.T) {
	fields := ServeConfigFieldsForArgs([]string{"-profile=local-dev", "-migrate=true"})
	values, err := bootstrap.ParseConfig(
		[]string{"-profile=local-dev", "-migrate=true"},
		func(string) (string, bool) { return "", false }, fields,
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ServeConfigFromValues(values)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != ServeProfileLocalDev || cfg.DatabaseURL != LocalDevDatabaseURL || cfg.DevHMACKey != LocalDevHMACKey || cfg.Tenant != LocalDevTenant {
		t.Fatalf("local profile identity defaults = %+v", cfg)
	}
	if !cfg.Migrate {
		t.Fatal("explicit -migrate=true did not override the profile default")
	}
	if !cfg.DevBrowserLogin || !cfg.ExecutionAuthority || !cfg.Scheduler || cfg.WorkflowPlan != WorkflowPlanExecute {
		t.Fatalf("local profile runtime defaults = %+v", cfg)
	}
	// PROMOUX-015: the demo tenant's finance approvals route to its Finance
	// Director by profile default; the standard profile names no partner.
	if cfg.ExecutionFinancePartner != LocalDevFinancePartner {
		t.Fatalf("local profile finance partner = %q, want %q", cfg.ExecutionFinancePartner, LocalDevFinancePartner)
	}
	for _, field := range ServeConfigFields() {
		if field.Name == FieldExecutionFinancePartner && field.Default != "" {
			t.Fatalf("standard profile -%s defaults to %q, want empty", FieldExecutionFinancePartner, field.Default)
		}
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("local profile defaults do not validate: %v", err)
	}
}

func TestLocalDevClockIsExplicitStrictAndUnavailableToProduction(t *testing.T) {
	const pinned = "2026-12-01T12:00:00Z"
	values, err := bootstrap.ParseConfig(
		[]string{"-profile=local-dev", "-local-dev-now=" + pinned},
		func(string) (string, bool) { return "", false },
		ServeConfigFieldsForArgs([]string{"-profile=local-dev"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ServeConfigFromValues(values)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LocalDevNow != pinned {
		t.Fatalf("LocalDevNow = %q, want %q", cfg.LocalDevNow, pinned)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid local development clock rejected: %v", err)
	}

	production := cfg
	production.Profile = ServeProfileStandard
	if err := production.Validate(); err == nil || !strings.Contains(err.Error(), FieldLocalDevNow) {
		t.Fatalf("production clock override Validate() = %v, want a %s refusal", err, FieldLocalDevNow)
	}
	malformed := cfg
	malformed.LocalDevNow = "1 December 2026"
	if err := malformed.Validate(); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("malformed clock Validate() = %v, want an RFC3339 refusal", err)
	}
}

func TestLocalDevProfileRejectsEveryNonLoopbackBoundary(t *testing.T) {
	base, err := ServeConfigFromValues(func() *bootstrap.Values {
		values, parseErr := bootstrap.ParseConfig([]string{"-profile=local-dev"}, func(string) (string, bool) { return "", false }, ServeConfigFieldsForArgs([]string{"-profile=local-dev"}))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		return values
	}())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ServeConfig)
	}{
		{"http listener", func(c *ServeConfig) { c.HTTPListen = "0.0.0.0:8080" }},
		{"grpc listener", func(c *ServeConfig) { c.GRPCListen = "[::]:8443" }},
		{"database", func(c *ServeConfig) { c.DatabaseURL = "postgres://db.example/hcm_next" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "loopback") {
				t.Fatalf("Validate() = %v, want a loopback refusal", err)
			}
		})
	}
}

// The health address is the one setting bootstrap reads before the config
// parse, so ServeSpec resolves it ahead of time from the same fields; a
// broken flag set resolves to none rather than to a stale value.
func TestHealthAddrOfResolvesTheFlagAheadOfBootstrap(t *testing.T) {
	if got := HealthAddrOf([]string{"-database-url=postgres://x", "-health-addr=127.0.0.1:9"}); got != "127.0.0.1:9" {
		t.Fatalf("HealthAddrOf = %q, want 127.0.0.1:9", got)
	}
	if got := HealthAddrOf([]string{"-database-url=postgres://x"}); got != "" {
		t.Fatalf("HealthAddrOf with no flag = %q, want empty", got)
	}
	if got := HealthAddrOf([]string{"-no-such-flag=1"}); got != "" {
		t.Fatalf("HealthAddrOf on a broken flag set = %q, want empty", got)
	}
}

// The timer dataset is one decision named twice: both releases default
// together, both may be cleared together, and naming only one is refused
// because a timer promise pins both.
func TestServeConfigTimerDatasetIsAllOrNothing(t *testing.T) {
	// The engine defaults need a tenant and an authority digest to
	// validate; the timer assertions below are about the dataset pair.
	defaults, err := ServeConfigFromValues(parseServe(t, "-database-url=postgres://x", "-dev-hmac-key="+testDevKey,
		"-tenant=acme", "-execution-authority-digest=sha256:abc"))
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	if got := defaults.TimerDataset(); got.TzdbVersion != DefaultTimerTzdbVersion || got.CalendarVersion != DefaultTimerCalendarVersion {
		t.Fatalf("default timer dataset = %+v", got)
	}
	if err := defaults.Validate(); err != nil {
		t.Fatalf("Validate with the default dataset: %v", err)
	}

	none := defaults
	none.TimerTzdbVersion, none.TimerCalendarVersion = "", ""
	if err := none.Validate(); err != nil {
		t.Fatalf("Validate with no timer dataset: %v", err)
	}
	if got := none.TimerDataset(); got.Validate() == nil {
		t.Fatalf("an empty pair reported itself as a usable dataset: %+v", got)
	}

	half := defaults
	half.TimerCalendarVersion = ""
	if err := half.Validate(); err == nil {
		t.Fatal("Validate accepted a tzdb release with no calendar release")
	}
}

// TestServeConfigFromValuesRejectsNoValues guards the one call shape a
// composition root must not silently accept.
func TestServeConfigFromValuesRejectsNoValues(t *testing.T) {
	if _, err := ServeConfigFromValues(nil); err == nil {
		t.Fatal("ServeConfigFromValues(nil) returned no error")
	}
	if err := ValidateServeValues(nil); err == nil {
		t.Fatal("ValidateServeValues(nil) returned no error")
	}
}

// TestServeConfigValidateRejectsAConfigurationAListenerMustNotStartOn walks
// every refusal the serve role owns.
func TestServeConfigValidateRejectsAConfigurationAListenerMustNotStartOn(t *testing.T) {
	base := ServeConfig{
		DatabaseURL: "postgres://x", DevHMACKey: testDevKey,
		OTelExporter: OTelExporterNone,
	}
	cases := []struct {
		name    string
		mutate  func(*ServeConfig)
		wantSub string
	}{
		{"no database url", func(c *ServeConfig) { c.DatabaseURL = "" }, EnvDatabaseURL},
		{"short signing key", func(c *ServeConfig) { c.DevHMACKey = "too-short" }, "at least 32 bytes"},
		{"unknown exporter", func(c *ServeConfig) { c.OTelExporter = "jaeger" }, "must be one of"},
		{"otlp without endpoint", func(c *ServeConfig) { c.OTelExporter = OTelExporterOTLPHTTP }, FieldOTelEndpoint},
		{"authority without digest", func(c *ServeConfig) {
			c.ExecutionAuthority = true
			c.ExecutionAuthorityRole = "r"
			c.ExecutionApprover = "a"
		}, FieldExecutionAuthorityDigest},
		{"authority without role", func(c *ServeConfig) {
			c.ExecutionAuthority = true
			c.ExecutionAuthorityDigest = "d"
			c.ExecutionApprover = "a"
		}, FieldExecutionAuthorityRole},
		{"authority without approver", func(c *ServeConfig) {
			c.ExecutionAuthority = true
			c.ExecutionAuthorityDigest = "d"
			c.ExecutionAuthorityRole = "r"
		}, FieldExecutionApprover},
		{"unknown workflow plan", func(c *ServeConfig) { c.WorkflowPlan = "other" }, FieldWorkflowPlan},
		{"scheduler without tenant", func(c *ServeConfig) {
			c.Scheduler = true
			c.ExecutionAuthority = true
			c.ExecutionAuthorityDigest = "d"
			c.ExecutionAuthorityRole = "r"
			c.ExecutionApprover = "a"
		}, FieldTenant},
		{"scheduler without authority", func(c *ServeConfig) {
			c.Scheduler = true
			c.Tenant = "acme"
		}, FieldExecutionAuthority},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %+v", cfg)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("Validate error = %q, want it to name %q", err, tc.wantSub)
			}
		})
	}
	if err := base.Validate(); err != nil {
		t.Errorf("Validate on the minimum valid configuration: %v", err)
	}
}

// TestValidateServeValuesReportsTheMissingDatabaseURLFirst pins the reporting
// order the previous cmd/hcmnext implementation had: a listener with no
// database URL is told that before anything else that is also wrong.
func TestValidateServeValuesReportsTheMissingDatabaseURLFirst(t *testing.T) {
	values := parseServe(t, "-dev-hmac-key=short", "-otel-exporter=nonsense")
	err := ValidateServeValues(values)
	if err == nil {
		t.Fatal("ValidateServeValues accepted a configuration with no database URL")
	}
	if !strings.Contains(err.Error(), EnvDatabaseURL) {
		t.Errorf("first reported failure = %q, want the missing %s", err, EnvDatabaseURL)
	}

	values = parseServe(t, "-database-url=postgres://x", "-dev-hmac-key=short")
	if err := ValidateServeValues(values); err == nil || !strings.Contains(err.Error(), FieldDevHMACKey) {
		t.Errorf("short key failure = %v, want it to name -%s", err, FieldDevHMACKey)
	}

	// The engine is on by default, so the defaults need a tenant for the
	// scheduler and an authority digest for the execution authority before
	// a listener may start.
	values = parseServe(t, "-database-url=postgres://x", "-dev-hmac-key="+testDevKey)
	if err := ValidateServeValues(values); err == nil {
		t.Error("ValidateServeValues accepted the engine defaults with no tenant or authority digest")
	}
	values = parseServe(t, "-database-url=postgres://x", "-dev-hmac-key="+testDevKey,
		"-tenant=acme", "-execution-authority-digest=sha256:abc")
	if err := ValidateServeValues(values); err != nil {
		t.Errorf("ValidateServeValues on the defaults plus a URL, a key, a tenant and a digest: %v", err)
	}
}

// TestServeConfigDefaultsComposeTheExecutingCell proves the defaults alone
// describe the shipped executing cell: workspace on, migrations on,
// telemetry off, and the promotion execution engine on (authority, execute
// plan and scheduler), with only the loopback dev login off.
func TestServeConfigDefaultsComposeTheExecutingCell(t *testing.T) {
	cfg, err := ServeConfigFromValues(parseServe(t,
		"-database-url=postgres://x", "-dev-hmac-key="+testDevKey))
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	if !cfg.Migrate || !cfg.Workspace {
		t.Errorf("defaults = migrate %t workspace %t, want both on", cfg.Migrate, cfg.Workspace)
	}
	if cfg.DevBrowserLogin {
		t.Errorf("defaults = dev-browser-login %t, want off", cfg.DevBrowserLogin)
	}
	if !cfg.ExecutionAuthority {
		t.Error("default execution-authority = false, want on: promotions run through the engine unless opted out")
	}
	if !cfg.Scheduler {
		t.Error("default scheduler = false, want on: the execute plan's durable WAITs need the dispatcher")
	}
	if cfg.OTelExporter != OTelExporterNone {
		t.Errorf("default exporter = %q, want %q", cfg.OTelExporter, OTelExporterNone)
	}
	if cfg.WorkflowPlan != WorkflowPlanExecute {
		t.Errorf("default workflow plan = %q, want %q", cfg.WorkflowPlan, WorkflowPlanExecute)
	}
	if cfg.Issuer != DefaultIssuer || cfg.Audience != DefaultAudience {
		t.Errorf("default issuer/audience = %q/%q, want %q/%q",
			cfg.Issuer, cfg.Audience, DefaultIssuer, DefaultAudience)
	}
	if cfg.MaxDeadline != 30*time.Second {
		t.Errorf("default max deadline = %s, want 30s", cfg.MaxDeadline)
	}
}
