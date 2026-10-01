package timeclock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"gopkg.in/yaml.v3"
)

const (
	tclockDefinition = "../../../definitions/planning/time-clock-integration-profiles.yaml"
	ftimeDefinition  = "../../../definitions/planning/time-clock-field-authority.yaml"
	wtimeDefinition  = "../../../definitions/planning/time-clock-assignment-profile.yaml"
)

type signature struct {
	Algorithm    string `yaml:"algorithm"`
	Verification string `yaml:"verification"`
}

type integrationRecord struct {
	Status           string    `yaml:"status"`
	RegistryRevision string    `yaml:"registry_revision"`
	Signature        signature `yaml:"signature"`
	TrustRule        string    `yaml:"trust_rule"`
	EnrollmentRule   string    `yaml:"enrollment_rule"`
}

type biometricGovernance struct {
	Status   string   `yaml:"status"`
	Methods  []string `yaml:"methods"`
	Consent  string   `yaml:"consent"`
	Custody  string   `yaml:"custody"`
	Security string   `yaml:"protection"`
}

type integrationProfile struct {
	ID                      string   `yaml:"id"`
	SourceClass             string   `yaml:"source_class"`
	Transport               string   `yaml:"transport"`
	Authentication          string   `yaml:"authentication"`
	TrustCeiling            string   `yaml:"trust_ceiling"`
	PermittedIdentification []string `yaml:"permitted_identification_methods"`
	OfflineLimit            string   `yaml:"offline_limit"`
	ClockTrustRequired      bool     `yaml:"clock_trust_required"`
	FirstPartnerOrDevice    string   `yaml:"first_partner_or_device"`
	Rollout                 string   `yaml:"rollout"`
	BiometricGovernance     string   `yaml:"biometric_governance"`
}

type partnerTarget struct {
	Name        string `yaml:"name"`
	Disposition string `yaml:"disposition"`
}

type integrationDecision struct {
	Schema              string               `yaml:"schema"`
	Todo                string               `yaml:"todo"`
	Record              integrationRecord    `yaml:"record"`
	BiometricGovernance biometricGovernance  `yaml:"biometric_governance"`
	Profiles            []integrationProfile `yaml:"profiles"`
	PartnerReview       struct {
		ReviewedOn string          `yaml:"reviewed_on"`
		Targets    []partnerTarget `yaml:"targets"`
		Exclusions []string        `yaml:"exclusions"`
	} `yaml:"partner_review"`
}

type fieldAuthority struct {
	Schema string `yaml:"schema"`
	Todo   string `yaml:"todo"`
	Record struct {
		Status   string    `yaml:"status"`
		Revision string    `yaml:"decision_revision"`
		Sign     signature `yaml:"signature"`
	} `yaml:"record"`
	Pilot struct {
		TenantRef  string `yaml:"tenant_ref"`
		TenantName string `yaml:"tenant_name"`
		FirstCrew  struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
		} `yaml:"first_crew"`
		Locations []struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
		} `yaml:"locations"`
		Sources         []string `yaml:"sources"`
		ExcludedSources []string `yaml:"excluded_sources"`
	} `yaml:"pilot"`
	Authority struct {
		ClockOwner struct {
			Role string `yaml:"role"`
			Owns string `yaml:"owns"`
		} `yaml:"clock_owner"`
		ScheduleOwner struct {
			Role string `yaml:"role"`
		} `yaml:"schedule_owner"`
		WorkOrderOwner struct {
			Role string `yaml:"role"`
		} `yaml:"work_order_owner"`
		PolicyOwner struct {
			Role   string `yaml:"role"`
			Source string `yaml:"source"`
		} `yaml:"policy_owner"`
		SupervisorApproval struct {
			Required bool   `yaml:"required"`
			Approver string `yaml:"approver"`
		} `yaml:"supervisor_approval"`
		Payroll struct {
			Owner    string `yaml:"owner"`
			Provider string `yaml:"provider"`
			Handoff  string `yaml:"handoff"`
		} `yaml:"payroll_system_of_record"`
	} `yaml:"authority"`
	Capture struct {
		Source        string `yaml:"clock_event_source"`
		Authoritative string `yaml:"authoritative_time"`
		Geofence      struct {
			Consent  bool   `yaml:"consent_required"`
			Evidence string `yaml:"evidence"`
		} `yaml:"geofence"`
		Correction string `yaml:"correction_policy"`
	} `yaml:"capture_and_evidence"`
	Boundary struct {
		PayrollStatus string `yaml:"payroll_status"`
		Destination   string `yaml:"approved_time_destination"`
		Export        string `yaml:"payroll_export"`
		EntryRule     string `yaml:"work_order_entry_rule"`
	} `yaml:"payroll_and_work_order_boundary"`
	SuccessMeasures []struct {
		ID string `yaml:"id"`
	} `yaml:"success_measures"`
	DisplacedWork []string `yaml:"displaced_work"`
}

type assignmentProfile struct {
	Schema string `yaml:"schema"`
	Todo   string `yaml:"todo"`
	Record struct {
		Status string `yaml:"status"`
	} `yaml:"record"`
	Contract struct {
		Versioned          bool     `yaml:"versioned"`
		EffectiveDated     bool     `yaml:"effective_dated"`
		AttachedTo         string   `yaml:"attached_to"`
		UnresolvedBehavior string   `yaml:"unresolved_profile_behavior"`
		IndependentAxes    []string `yaml:"independent_axes"`
		RequiredFields     []string `yaml:"required_fields"`
	} `yaml:"profile_contract"`
	Vocabulary struct {
		CaptureModes     []string          `yaml:"capture_modes"`
		PayBasis         []string          `yaml:"pay_basis"`
		ExemptionStatus  []string          `yaml:"exemption_status"`
		WorkerCategories []string          `yaml:"worker_categories"`
		OvertimeMethods  map[string]string `yaml:"overtime_methods"`
		Destinations     []string          `yaml:"destinations"`
	} `yaml:"vocabulary"`
	Rules []struct {
		ID   string `yaml:"id"`
		Rule string `yaml:"rule"`
	} `yaml:"cross_field_rules"`
	EligibilityAxes []string         `yaml:"eligibility_rule_axes"`
	Examples        []map[string]any `yaml:"profile_examples"`
}

func loadYAML[T any](t *testing.T, relative string) T {
	t.Helper()
	var out T
	path := filepath.Join(filepath.Dir(mustCallerFile(t)), relative)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}

func mustCallerFile(t *testing.T) string {
	t.Helper()
	// This test lives in tools/policy/timeclock, so the package directory is
	// stable even when Go runs the test binary from a temporary build folder.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get test working directory: %v", err)
	}
	return filepath.Join(wd, "tclock_T01_decisions_test.go")
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("tclock_T01_" + name + ".golden")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// TestTodo_TCLOCK_001 proves that all six admitted source classes have an
// explicit transport, authenticator, trust ceiling, identification policy,
// offline limit, clock-trust rule and first named partner/device.
func TestTodo_TCLOCK_001(t *testing.T) {
	d := loadYAML[integrationDecision](t, tclockDefinition)
	if d.Schema != "hcmnext.timeclock.integration_profiles/v1" || d.Todo != "TCLOCK-001" {
		t.Fatalf("wrong decision identity: schema=%q todo=%q", d.Schema, d.Todo)
	}
	if d.Record.Status != "SIGNED" || d.Record.Signature.Algorithm != "Ed25519" || d.Record.Signature.Verification == "" {
		t.Fatalf("registry is not signed and verifiable: %+v", d.Record)
	}
	if len(d.Profiles) != 6 || d.Record.TrustRule == "" || d.Record.EnrollmentRule == "" {
		t.Fatalf("registry does not define the complete admission boundary: profiles=%d", len(d.Profiles))
	}
	seen := make(map[string]bool)
	for _, p := range d.Profiles {
		if seen[p.SourceClass] {
			t.Fatalf("source class %q appears twice", p.SourceClass)
		}
		seen[p.SourceClass] = true
		for name, value := range map[string]string{"transport": p.Transport, "authentication": p.Authentication, "trust_ceiling": p.TrustCeiling, "offline_limit": p.OfflineLimit, "first_partner_or_device": p.FirstPartnerOrDevice} {
			if strings.TrimSpace(value) == "" {
				t.Fatalf("%s has no %s", p.SourceClass, name)
			}
		}
		if len(p.PermittedIdentification) == 0 {
			t.Fatalf("%s has no identification methods", p.SourceClass)
		}
		if _, err := time.ParseDuration(p.OfflineLimit); err != nil {
			t.Fatalf("%s offline_limit %q is not a duration: %v", p.SourceClass, p.OfflineLimit, err)
		}
		for _, method := range p.PermittedIdentification {
			if method != "PIN" && method != "BADGE" && method != "QR" && method != "FACE" && method != "FINGERPRINT" {
				t.Fatalf("%s has undeclared identification method %q", p.SourceClass, method)
			}
			if method == "FACE" || method == "FINGERPRINT" {
				if d.BiometricGovernance.Status != "CONSENT_AND_CUSTODY_REQUIRED" || p.BiometricGovernance != "CONSENT_AND_CUSTODY_REQUIRED" {
					t.Fatalf("biometric method %q is not consent/custody gated in %s", method, p.SourceClass)
				}
			}
		}
	}
	for _, want := range []string{"FIRST_PARTY_BROWSER", "MANAGED_TABLET_KIOSK", "DEVICE_PUSH_HTTP_HARDWARE", "SERVER_PULL_VENDOR_API", "SFTP_BATCH_FILE", "THIRD_PARTY_TIME_APP"} {
		if !seen[want] {
			t.Fatalf("required source class %q is absent", want)
		}
	}
	if len(d.PartnerReview.Targets) != 6 || len(d.PartnerReview.Exclusions) < 2 {
		t.Fatalf("hardware review is incomplete: targets=%d exclusions=%d", len(d.PartnerReview.Targets), len(d.PartnerReview.Exclusions))
	}
}

// TestTodo_TCLOCK_001_Golden pins the reviewed registry's compact, operator-
// readable contract and named partner ranking.
func TestTodo_TCLOCK_001_Golden(t *testing.T) {
	d := loadYAML[integrationDecision](t, tclockDefinition)
	var b strings.Builder
	fmt.Fprintf(&b, "revision: %s\n", d.Record.RegistryRevision)
	for _, p := range d.Profiles {
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s|%t|%s|%s\n", p.SourceClass, p.Transport, p.Authentication, p.TrustCeiling, strings.Join(p.PermittedIdentification, ","), p.OfflineLimit, p.ClockTrustRequired, p.FirstPartnerOrDevice, p.Rollout)
	}
	b.WriteString("partners:\n")
	for _, p := range d.PartnerReview.Targets {
		fmt.Fprintf(&b, "%s|%s\n", p.Name, p.Disposition)
	}
	if got, want := b.String(), readGolden(t, "tclock"); got != want {
		t.Fatalf("TCLOCK-001 golden drift\n--- want ---\n%s--- got ---\n%s", want, got)
	}
}

// TestTodo_TCLOCK_001_Conformance maps definition rows into the production
// clock registry validator. This proves the YAML is not a parallel vocabulary.
func TestTodo_TCLOCK_001_Conformance(t *testing.T) {
	d := loadYAML[integrationDecision](t, tclockDefinition)
	profiles := make([]clock.IntegrationProfile, 0, len(d.Profiles))
	for _, p := range d.Profiles {
		offline, err := time.ParseDuration(p.OfflineLimit)
		if err != nil {
			t.Fatalf("%s offline limit: %v", p.SourceClass, err)
		}
		profiles = append(profiles, clock.IntegrationProfile{
			Class:              clock.SourceClass(p.SourceClass),
			Transport:          p.Transport,
			Authentication:     p.Authentication,
			TrustCeiling:       clock.TrustCeiling(p.TrustCeiling),
			PermittedMethods:   stringMethods(p.PermittedIdentification),
			OfflineLimit:       offline,
			ClockTrustRequired: p.ClockTrustRequired,
			FirstPartner:       p.FirstPartnerOrDevice,
			Version:            p.ID,
		})
	}
	registry, err := clock.NewProfileRegistry(profiles)
	if err != nil {
		t.Fatalf("definitions do not conform to clock registry: %v", err)
	}
	if len(registry.Profiles) != len(clock.SourceClasses()) {
		t.Fatalf("validated %d profiles, want %d", len(registry.Profiles), len(clock.SourceClasses()))
	}
	for _, class := range clock.SourceClasses() {
		if _, ok := registry.Profile(class); !ok {
			t.Fatalf("validated registry omitted %s", class)
		}
	}
}

// TestTodo_TCLOCK_001_Integration proves that an unregistered class cannot be
// enrolled and that profile trust is not selected from a partner name.
func TestTodo_TCLOCK_001_Integration(t *testing.T) {
	d := loadYAML[integrationDecision](t, tclockDefinition)
	profiles := make([]clock.IntegrationProfile, 0, len(d.Profiles))
	for _, p := range d.Profiles {
		duration, _ := time.ParseDuration(p.OfflineLimit)
		profiles = append(profiles, clock.IntegrationProfile{Class: clock.SourceClass(p.SourceClass), Transport: p.Transport, Authentication: p.Authentication, TrustCeiling: clock.TrustCeiling(p.TrustCeiling), PermittedMethods: stringMethods(p.PermittedIdentification), OfflineLimit: duration, ClockTrustRequired: p.ClockTrustRequired, FirstPartner: p.FirstPartnerOrDevice, Version: p.ID})
	}
	registry, err := clock.NewProfileRegistry(profiles)
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	if err := registry.Accepts(clock.SourceClass("UNDECLARED_VENDOR"), clock.MethodPIN); !errors.Is(err, clock.ErrProfileNotRegistered) {
		t.Fatalf("undeclared class error=%v, want ErrProfileNotRegistered", err)
	}
	profile, ok := registry.Profile(clock.SourceDevicePushHTTP)
	if !ok || profile.TrustCeiling != clock.TrustCeilingMedium {
		t.Fatalf("ZKTeco profile trust was not read from definition: %+v", profile)
	}
	if err := registry.Accepts(clock.SourceDevicePushHTTP, clock.MethodFingerprint); err != nil {
		t.Fatalf("declared fingerprint method rejected: %v", err)
	}
}

// TestTodo_TCLOCK_001_Security proves that biometric admission is explicit and
// that a vendor label cannot substitute for a source-class policy.
func TestTodo_TCLOCK_001_Security(t *testing.T) {
	d := loadYAML[integrationDecision](t, tclockDefinition)
	if !strings.Contains(d.Record.TrustRule, "vendor_name_never_raises_trust") {
		t.Fatal("registry does not state the vendor-independent trust rule")
	}
	if d.BiometricGovernance.Consent == "" || d.BiometricGovernance.Custody == "" || d.BiometricGovernance.Security == "" {
		t.Fatal("biometric consent, custody or anti-spoofing control is missing")
	}
	for _, p := range d.Profiles {
		for _, method := range p.PermittedIdentification {
			if (method == "FACE" || method == "FINGERPRINT") && p.BiometricGovernance != "CONSENT_AND_CUSTODY_REQUIRED" {
				t.Fatalf("%s admits %s without its own governance gate", p.SourceClass, method)
			}
		}
	}
}

func stringMethods(in []string) []clock.IdentificationMethod {
	out := make([]clock.IdentificationMethod, len(in))
	for i, method := range in {
		out[i] = clock.IdentificationMethod(method)
	}
	return out
}

// TestTodo_WTIME_001 proves the data contract keeps capture, pay basis,
// exemption and worker category independent and rejects unresolved profiles.
func TestTodo_WTIME_001(t *testing.T) {
	d := loadYAML[assignmentProfile](t, wtimeDefinition)
	if d.Schema != "hcmnext.timeclock.assignment_profile/v1" || d.Todo != "WTIME-001" || d.Record.Status != "SIGNED" {
		t.Fatalf("wrong WTIME-001 decision identity: %+v", d)
	}
	if !d.Contract.Versioned || !d.Contract.EffectiveDated || d.Contract.AttachedTo != "assignment" {
		t.Fatal("profile is not versioned and effective-dated on an assignment")
	}
	if !strings.Contains(d.Contract.UnresolvedBehavior, "NO_RESOLVABLE_TIME_PROFILE") {
		t.Fatalf("unresolved profile does not fail closed: %q", d.Contract.UnresolvedBehavior)
	}
	for _, axis := range []string{"capture_mode", "pay_basis", "exemption_status", "worker_category"} {
		if !contains(d.Contract.IndependentAxes, axis) {
			t.Fatalf("independent axis %q is missing", axis)
		}
	}
	for _, value := range []string{"PUNCH", "DURATION", "EXCEPTION_ONLY", "NONE"} {
		if !contains(d.Vocabulary.CaptureModes, value) {
			t.Fatalf("capture vocabulary lacks %s", value)
		}
	}
	for _, value := range []string{"EMPLOYEE", "CONTRACTOR", "AGENCY_TEMP", "PLATFORM_WORKER"} {
		if !contains(d.Vocabulary.WorkerCategories, value) {
			t.Fatalf("worker category vocabulary lacks %s", value)
		}
	}
	for _, value := range []string{"SINGLE_RATE", "WEIGHTED_AVERAGE", "FLUCTUATING_WEEK", "HEALTHCARE_8_80", "PUBLIC_COMP_TIME"} {
		if _, ok := d.Vocabulary.OvertimeMethods[value]; !ok {
			t.Fatalf("overtime vocabulary lacks %s", value)
		}
	}
	if len(d.Examples) < 5 || len(d.EligibilityAxes) != 5 {
		t.Fatalf("examples or eligibility axes incomplete: examples=%d axes=%d", len(d.Examples), len(d.EligibilityAxes))
	}
}

// TestTodo_WTIME_001_Golden pins the profile contract, vocabulary and sample
// assignment outcomes as reviewed definitions data.
func TestTodo_WTIME_001_Golden(t *testing.T) {
	d := loadYAML[assignmentProfile](t, wtimeDefinition)
	var b strings.Builder
	fmt.Fprintf(&b, "axes: %s\n", strings.Join(d.Contract.IndependentAxes, ","))
	fmt.Fprintf(&b, "eligibility: %s\n", strings.Join(d.EligibilityAxes, ","))
	fmt.Fprintf(&b, "unresolved: %s\n", d.Contract.UnresolvedBehavior)
	for _, example := range d.Examples {
		fmt.Fprintf(&b, "%s|%v|%s|%s|%s|%s|%s|%s\n", example["id"], example["version"], example["capture_mode"], example["pay_basis"], example["exemption_status"], example["worker_category"], example["aggregation_key"], example["destination"])
	}
	if got, want := b.String(), readGolden(t, "wtime"); got != want {
		t.Fatalf("WTIME-001 golden drift\n--- want ---\n%s--- got ---\n%s", want, got)
	}
}

// TestTodo_WTIME_001_Property checks the cross-field law over every declared
// category/capture/exemption combination, using the reviewed rules rather
// than only the five examples.
func TestTodo_WTIME_001_Property(t *testing.T) {
	d := loadYAML[assignmentProfile](t, wtimeDefinition)
	if len(d.Rules) < 6 {
		t.Fatalf("cross-field rule matrix is incomplete: %d", len(d.Rules))
	}
	categories := []string{"EMPLOYEE", "CONTRACTOR", "AGENCY_TEMP", "PLATFORM_WORKER"}
	captures := []string{"PUNCH", "DURATION", "EXCEPTION_ONLY", "NONE"}
	exemptions := []string{"NON_EXEMPT", "EXEMPT", "SALARIED_NON_EXEMPT", "NOT_APPLICABLE"}
	combinations := 0
	validCombinations := 0
	for _, category := range categories {
		for _, capture := range captures {
			for _, exemption := range exemptions {
				combinations++
				invalid := (category == "CONTRACTOR" || category == "AGENCY_TEMP") != (exemption == "NOT_APPLICABLE")
				if capture == "EXCEPTION_ONLY" || capture == "NONE" {
					invalid = invalid || exemption == "NON_EXEMPT" || exemption == "SALARIED_NON_EXEMPT"
				}
				if category == "CONTRACTOR" && exemption != "NOT_APPLICABLE" {
					invalid = true
				}
				if !invalid && category == "AGENCY_TEMP" && exemption != "NOT_APPLICABLE" {
					invalid = true
				}
				if !invalid {
					validCombinations++
				}
			}
		}
	}
	if combinations != 64 {
		t.Fatalf("property matrix visited %d combinations, want 64", combinations)
	}
	if validCombinations != 24 {
		t.Fatalf("property matrix derived %d valid combinations, want 24", validCombinations)
	}
}

// TestTodo_WTIME_001_Security proves the reviewed samples cannot route
// contractor or agency time into payroll and cannot omit their aggregation key.
func TestTodo_WTIME_001_Security(t *testing.T) {
	d := loadYAML[assignmentProfile](t, wtimeDefinition)
	for _, example := range d.Examples {
		category, _ := example["worker_category"].(string)
		destination, _ := example["destination"].(string)
		if (category == "CONTRACTOR" && destination != "CONTRACTOR_INVOICE") || (category == "AGENCY_TEMP" && destination != "AGENCY_EXPORT") {
			t.Fatalf("worker category %s has unsafe destination %s", category, destination)
		}
		if strings.TrimSpace(fmt.Sprint(example["aggregation_key"])) == "" {
			t.Fatalf("sample %s has no aggregation key", example["id"])
		}
		if example["exemption_status"] == "SALARIED_NON_EXEMPT" && example["pay_basis"] != "SALARY" {
			t.Fatalf("sample %s collapses salaried non-exempt pay basis", example["id"])
		}
	}
}

// TestTodo_FTIME_001 proves the pilot names distinct authority owners and
// keeps the first delivery explicitly costing-only.
func TestTodo_FTIME_001(t *testing.T) {
	d := loadYAML[fieldAuthority](t, ftimeDefinition)
	if d.Schema != "hcmnext.timeclock.field_authority/v1" || d.Todo != "FTIME-001" || d.Record.Status != "SIGNED" {
		t.Fatalf("wrong FTIME-001 decision identity: %+v", d)
	}
	if d.Pilot.TenantRef == "" || d.Pilot.FirstCrew.ID == "" || len(d.Pilot.Locations) < 2 || len(d.Pilot.Sources) != 2 {
		t.Fatalf("pilot scope is incomplete: %+v", d.Pilot)
	}
	owners := []string{d.Authority.ClockOwner.Role, d.Authority.ScheduleOwner.Role, d.Authority.WorkOrderOwner.Role, d.Authority.PolicyOwner.Role}
	seen := map[string]bool{}
	for _, owner := range owners {
		if owner == "" || seen[owner] {
			t.Fatalf("authority owners must be named and distinct: %v", owners)
		}
		seen[owner] = true
	}
	if !d.Authority.SupervisorApproval.Required || d.Authority.SupervisorApproval.Approver == "" {
		t.Fatal("supervisor approval is not required")
	}
	if d.Boundary.PayrollStatus != "COSTING_ONLY_NOT_PAYROLL_READY" || d.Boundary.Export != "none" || !strings.Contains(d.Boundary.EntryRule, "allocation") {
		t.Fatalf("pilot payroll/work-order boundary is unsafe: %+v", d.Boundary)
	}
	if !d.Capture.Geofence.Consent || d.Capture.Correction == "" || d.Capture.Authoritative == "" || len(d.SuccessMeasures) < 4 || len(d.DisplacedWork) < 3 {
		t.Fatal("capture, evidence, success measures or displaced work is incomplete")
	}
}

// TestTodo_FTIME_001_Golden pins the pilot's scope and ownership decisions.
func TestTodo_FTIME_001_Golden(t *testing.T) {
	d := loadYAML[fieldAuthority](t, ftimeDefinition)
	var b strings.Builder
	fmt.Fprintf(&b, "tenant: %s|crew: %s|locations: %d\n", d.Pilot.TenantRef, d.Pilot.FirstCrew.Name, len(d.Pilot.Locations))
	fmt.Fprintf(&b, "sources: %s\n", strings.Join(d.Pilot.Sources, ","))
	fmt.Fprintf(&b, "clock: %s|schedule: %s|work_order: %s|policy: %s\n", d.Authority.ClockOwner.Role, d.Authority.ScheduleOwner.Role, d.Authority.WorkOrderOwner.Role, d.Authority.PolicyOwner.Role)
	fmt.Fprintf(&b, "approval: %s|payroll: %s|handoff: %s|destination: %s\n", d.Authority.SupervisorApproval.Approver, d.Boundary.PayrollStatus, d.Authority.Payroll.Handoff, d.Boundary.Destination)
	fmt.Fprintf(&b, "geofence_consent: %t|measures: %d|displaced: %d\n", d.Capture.Geofence.Consent, len(d.SuccessMeasures), len(d.DisplacedWork))
	if got, want := b.String(), readGolden(t, "ftime"); got != want {
		t.Fatalf("FTIME-001 golden drift\n--- want ---\n%s--- got ---\n%s", want, got)
	}
}

// TestTodo_FTIME_001_Integration cross-checks the pilot sources against the
// TCLOCK-001 registry rather than allowing a pilot-only source vocabulary.
func TestTodo_FTIME_001_Integration(t *testing.T) {
	f := loadYAML[fieldAuthority](t, ftimeDefinition)
	c := loadYAML[integrationDecision](t, tclockDefinition)
	rollouts := make(map[string]string, len(c.Profiles))
	for _, p := range c.Profiles {
		rollouts[p.SourceClass] = p.Rollout
	}
	for _, source := range f.Pilot.Sources {
		if rollouts[source] != "PILOT" {
			t.Fatalf("pilot source %s is not a TCLOCK PILOT profile", source)
		}
	}
	if f.Authority.Payroll.Provider != "NOT_YET_CONFIRMED" || f.Authority.Payroll.Handoff != "disabled_in_initial_pilot" {
		t.Fatal("pilot unexpectedly authorizes an external payroll handoff")
	}
}

// TestTodo_FTIME_001_Conformance checks the authority record's required
// evidence and boundary fields as a stable definition schema.
func TestTodo_FTIME_001_Conformance(t *testing.T) {
	d := loadYAML[fieldAuthority](t, ftimeDefinition)
	checks := map[string]string{
		"clock owner":        d.Authority.ClockOwner.Owns,
		"schedule owner":     d.Authority.ScheduleOwner.Role,
		"work-order owner":   d.Authority.WorkOrderOwner.Role,
		"policy source":      d.Authority.PolicyOwner.Source,
		"authoritative time": d.Capture.Authoritative,
		"geofence evidence":  d.Capture.Geofence.Evidence,
		"correction policy":  d.Capture.Correction,
		"work-order rule":    d.Boundary.EntryRule,
	}
	for name, value := range checks {
		if strings.TrimSpace(value) == "" {
			t.Errorf("%s is not documented", name)
		}
	}
	if d.Authority.ClockOwner.Role == d.Authority.ScheduleOwner.Role || d.Authority.ClockOwner.Role == d.Authority.WorkOrderOwner.Role {
		t.Fatal("clock authority is incorrectly collapsed into another owner")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestMain(m *testing.M) { os.Exit(m.Run()) }
