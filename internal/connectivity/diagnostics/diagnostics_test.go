package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/health"
)

// The INTG-003 matrix is kept in this package so every provider adapter is
// exercised against the same normalized diagnostic contract.
func TestTodo_INTG_003(t *testing.T) {
	probe := &fakeProbe{scopes: []string{"worker.read", "worker.read"}}
	runner, err := NewRunner(probe)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	report, err := runner.Run(context.Background(), Request{Connection: testConnection(t), RequiredScopes: []string{"worker.read"}})
	if err != nil {
		t.Fatalf("run diagnostics: %v", err)
	}
	if !report.Healthy() || len(report.Findings) != 3 || report.Findings[2].Code != "SCOPES_OK" || len(report.Findings[2].Actual) != 1 {
		t.Fatalf("unexpected normalized report: %+v", report)
	}
}

func TestTodo_INTG_003_Golden(t *testing.T) {
	p := &fakeProbe{scopes: []string{"worker.read"}}
	report, err := Diagnose(context.Background(), p, Request{
		Connection:     testConnection(t),
		RequiredScopes: []string{"worker.read"},
		Capabilities:   []CapabilityImpact{{Capability: connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead}, Workflows: []string{"Onboarding"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Report{ConnectionID: "conn-1", TenantID: "tenant-1", Findings: []Finding{
		{Check: CheckAuthentication, Status: Pass, Code: "AUTHENTICATION_OK", Detail: "authentication check passed"},
		{Check: CheckReachability, Status: Pass, Code: "REACHABILITY_OK", Detail: "reachability check passed"},
		{Check: CheckScopes, Status: Pass, Code: "SCOPES_OK", Detail: "required provider scopes are granted", Required: []string{"worker.read"}, Actual: []string{"worker.read"}},
		{Check: CheckCapability, Status: Pass, Code: "CAPABILITY_OK", Detail: "capability check passed", Capability: connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead}, Workflows: []string{"Onboarding"}},
	}}
	if !reflect.DeepEqual(report, want) {
		t.Fatalf("golden report mismatch: got=%+v want=%+v", report, want)
	}
	// Also ensure the public representation remains deterministic and valid JSON.
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("report is not serializable: %v", err)
	}
}

func TestTodo_INTG_003_Integration(t *testing.T) {
	connection := testConnection(t)
	probe := &fakeProbe{scopes: []string{"worker.read"}}
	runner, err := NewRunner(probe)
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Run(context.Background(), Request{Connection: connection, RequiredScopes: []string{"worker.read"}, Capabilities: []CapabilityImpact{{Capability: connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead}}}})
	if err != nil {
		t.Fatal(err)
	}
	if report.ConnectionID != connection.ID() || report.TenantID != connection.TenantID() || len(probe.checked) != 1 || report.Findings[len(report.Findings)-1].Check != CheckCapability {
		t.Fatalf("diagnostic integration mismatch: report=%+v checked=%v", report, probe.checked)
	}
}
func TestTodo_INTG_003_Fault(t *testing.T) {
	runner, err := NewRunner(&fakeProbe{scopeErr: errors.New("scope service unavailable")})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Run(context.Background(), Request{Connection: testConnection(t), RequiredScopes: []string{"worker.read"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Healthy() || report.Findings[2].Status != Unknown || report.Findings[2].Code != "PROVIDER_CHECK_FAILED" {
		t.Fatalf("scope probe failure was not reported as unknown: %+v", report.Findings)
	}
}
func TestTodo_INTG_003_Security(t *testing.T) {
	secret := "client_secret=diagnostic-sensitive"
	runner, err := NewRunner(&fakeProbe{auth: errors.New(secret), reach: errors.New(secret), scopeErr: errors.New(secret), capErr: errors.New(secret)})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Run(context.Background(), Request{Connection: testConnection(t), RequiredScopes: []string{"worker.read"}, Capabilities: []CapabilityImpact{{Capability: connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		if strings.Contains(finding.Code, secret) || strings.Contains(finding.Detail, secret) {
			t.Fatalf("provider error leaked: %+v", finding)
		}
	}
}

func TestTodo_REV_013_02_Integration(t *testing.T) {
	connection := testConnection(t)
	probe := &fakeProbe{scopes: []string{"worker.read"}}
	diagnostic, err := Diagnose(context.Background(), probe, Request{
		Connection:     connection,
		RequiredScopes: []string{"worker.read"},
		Capabilities: []CapabilityImpact{{
			Capability: connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead},
			Workflows:  []string{"Onboarding v9"},
		}},
	})
	if err != nil {
		t.Fatalf("diagnose test connection: %v", err)
	}
	if !diagnostic.Healthy() || diagnostic.ConnectionID != connection.ID() || diagnostic.TenantID != connection.TenantID() {
		t.Fatalf("diagnostic does not describe the test connection: %+v", diagnostic)
	}

	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	projection, err := health.Project(health.Input{
		TenantID: connection.TenantID(), ConnectionID: connection.ID(), Now: now,
		Signals: []health.Signal{{Kind: health.Authentication, Status: health.Healthy, Watermark: now}},
	})
	if err != nil {
		t.Fatalf("project test connection health: %v", err)
	}
	if projection.Status != health.Healthy || projection.ConnectionID != diagnostic.ConnectionID || projection.TenantID != diagnostic.TenantID {
		t.Fatalf("health projection does not align with the diagnostic: diagnostic=%+v health=%+v", diagnostic, projection)
	}
}

func TestTodo_REV_013_02(t *testing.T) {
	connection := testConnection(t)
	diagnostic, err := Diagnose(context.Background(), &fakeProbe{scopes: []string{"worker.read"}}, Request{
		Connection:     connection,
		RequiredScopes: []string{"worker.read"},
	})
	if err != nil {
		t.Fatalf("diagnose test connection: %v", err)
	}
	if !diagnostic.Healthy() || diagnostic.ConnectionID != "conn-1" || diagnostic.TenantID != "tenant-1" {
		t.Fatalf("unexpected operator diagnostic result: %+v", diagnostic)
	}
	projection, err := health.Project(health.Input{
		TenantID: connection.TenantID(), ConnectionID: connection.ID(), Now: time.Unix(1, 0),
		Signals: []health.Signal{{Kind: health.Observation, Status: health.Healthy}},
	})
	if err != nil {
		t.Fatalf("project test connection health: %v", err)
	}
	if projection.Status != health.Healthy || projection.ConnectionID != diagnostic.ConnectionID || projection.TenantID != diagnostic.TenantID {
		t.Fatalf("operator health result does not describe the diagnosed connection: %+v", projection)
	}
}

func FuzzTodo_INTG_003(f *testing.F) {
	f.Add(" worker.read,worker.read,", "worker.read")
	f.Fuzz(func(t *testing.T, required, actual string) {
		req := normalize(strings.Split(required, ","))
		act := normalize(strings.Split(actual, ","))
		got := difference(req, act)
		seen := map[string]bool{}
		for i, scope := range got {
			if scope == "" || seen[scope] || (i > 0 && got[i-1] >= scope) {
				t.Fatalf("difference must be sorted and unique: %q", got)
			}
			seen[scope] = true
			for _, have := range act {
				if scope == have {
					t.Fatalf("reported granted scope %q as missing", scope)
				}
			}
		}
	})
}

type fakeProbe struct {
	auth, reach, scopeErr, capErr error
	scopes                        []string
	checked                       []connectivity.Capability
}

func (p *fakeProbe) Authenticate(context.Context) error              { return p.auth }
func (p *fakeProbe) Reachable(context.Context) error                 { return p.reach }
func (p *fakeProbe) GrantedScopes(context.Context) ([]string, error) { return p.scopes, p.scopeErr }
func (p *fakeProbe) CheckCapability(_ context.Context, c connectivity.Capability) error {
	p.checked = append(p.checked, c)
	return p.capErr
}

func testConnection(t *testing.T) *connectivity.ConnectorConnection {
	t.Helper()
	v := connectivity.Version{Major: 1, Minor: 0, Patch: 0}
	d := connectivity.ConnectorDefinition{ConnectorID: "acme", Version: v, AuthModes: []connectivity.AuthMode{connectivity.AuthAPIKey}, Capabilities: connectivity.ReadCapabilities(connectivity.ObjectWorker), Bounds: connectivity.Bounds{MaxPageSize: 10, MaxPagesPerRun: 1, MaxRecordsPerRun: 10, MaxRecordBytes: 100, MinRequestInterval: 0}}
	d.Vendor, d.Product, d.Maturity = "Acme", "HR", connectivity.MaturityCertified
	d.Objects = []connectivity.ObjectKind{connectivity.ObjectWorker}
	d.ReadModes = []connectivity.ReadMode{connectivity.ReadFull}
	d.WriteModes = []string{}
	d.EventModes = []string{}
	d.SchemaRefs = []connectivity.SchemaRef{{Object: connectivity.ObjectWorker, SchemaID: "worker", SchemaVer: "1"}}
	d.Pagination = connectivity.PaginationContract{Style: "KEYSET", SortKeyField: "updated", TieBreakField: "id"}
	d.Rate = connectivity.RateContract{RequestsPerMinute: 60, ConcurrentReads: 1}
	d.Idempotency = connectivity.IdempotencyContract{ReadsAreIdempotent: true}
	d.Observation = connectivity.ObservationContract{WatermarkField: "updated", FreshnessBudget: time.Hour}
	d.Reconciliation = connectivity.ReconciliationContract{KeyFields: []string{"id"}, ComparableFields: []string{"name"}}
	d.Health = connectivity.HealthContract{ProbeObject: connectivity.ObjectWorker, Interval: time.Minute, DegradedAfterFailures: 2}
	registry := connectivity.NewRegistry()
	p, err := registry.Publish(d, connectivity.PublicationMeta{PublishedBy: "test", PublishedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := connectivity.ParseCredentialRef("vault://tenant/acme")
	if err != nil {
		t.Fatal(err)
	}
	c, err := connectivity.NewConnection(p, connectivity.ConnectionSpec{ConnectionID: "conn-1", TenantID: "tenant-1", OrgID: "org-1", SystemID: "acme", Environment: connectivity.EnvironmentProduction, Residency: "us", ConnectorID: "acme", ConnectorVersion: v, AuthMode: connectivity.AuthAPIKey, CredentialRef: r, Scopes: []string{"worker.read"}, EndpointPolicy: connectivity.EndpointPolicy{AllowedHosts: []string{"api.acme.test"}, RequireTLS: true, EgressProfile: "egress/us"}, Capabilities: connectivity.ReadCapabilities(connectivity.ObjectWorker), Bounds: d.Bounds, CreatedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRunNormalizesFindingsAndImpacts(t *testing.T) {
	p := &fakeProbe{scopes: []string{"worker.read", "worker.read"}}
	runner, _ := NewRunner(p)
	c := testConnection(t)
	report, err := runner.Run(context.Background(), Request{Connection: c, RequiredScopes: []string{"worker.read", "worker.write", "worker.write"}, Capabilities: []CapabilityImpact{{Capability: connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead}, Workflows: []string{"Transfer v12", "Onboarding v9", "Transfer v12"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Healthy() {
		t.Fatal("missing scope reported healthy")
	}
	if len(report.Findings) != 4 {
		t.Fatalf("findings=%d", len(report.Findings))
	}
	if got := report.Findings[2].Required; !reflect.DeepEqual(got, []string{"worker.write"}) {
		t.Fatalf("missing=%v", got)
	}
	if got := report.Findings[3].Workflows; !reflect.DeepEqual(got, []string{"Onboarding v9", "Transfer v12"}) {
		t.Fatalf("workflows=%v", got)
	}
	if len(p.checked) != 1 {
		t.Fatal("capability probe not called")
	}
}

func TestRunRedactsProviderErrorsAndDoesNotMutateConnection(t *testing.T) {
	secret := "Bearer super-secret-token"
	p := &fakeProbe{auth: connectivity.Fail("probe", connectivity.ErrCredential, "%s", secret), reach: errors.New(secret), scopeErr: connectivity.Fail("probe", connectivity.ErrPermission, "scope token=%s", secret), capErr: errors.New(secret)}
	runner, _ := NewRunner(p)
	c := testConnection(t)
	before := c.StateVersion()
	report, err := runner.Run(context.Background(), Request{Connection: c, RequiredScopes: []string{"worker.read"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.StateVersion() != before || c.State() != connectivity.StateDraft {
		t.Fatal("diagnosis mutated connection")
	}
	b := strings.Builder{}
	for _, f := range report.Findings {
		b.WriteString(f.Detail)
		b.WriteString(f.Code)
	}
	if strings.Contains(b.String(), secret) {
		t.Fatal("secret leaked in report")
	}
	if report.Findings[0].Code != "AUTHENTICATION_FAILED" || report.Findings[1].Status != Unknown || report.Findings[2].Code != "PERMISSION_DENIED" {
		t.Fatalf("unexpected findings=%+v", report.Findings)
	}
}

func TestRunRejectsInvalidCapabilityAndNilInputs(t *testing.T) {
	runner, _ := NewRunner(&fakeProbe{})
	if _, err := runner.Run(context.Background(), Request{}); err == nil {
		t.Fatal("nil connection accepted")
	}
	c := testConnection(t)
	if _, err := runner.Run(context.Background(), Request{Connection: c, Capabilities: []CapabilityImpact{{Capability: connectivity.Capability{Object: connectivity.ObjectKind("NOPE"), Operation: connectivity.OperationRead}}}}); err == nil {
		t.Fatal("invalid capability accepted")
	}
}
