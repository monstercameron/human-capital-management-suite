package vulnimpact_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/sbom"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/vulnimpact"
)

func slaDocument() vulnimpact.Artifact {
	return vulnimpact.Artifact{ID: "hcmnext-release", SBOM: sbom.Document{
		Metadata:   sbom.Metadata{Component: sbom.Component{Name: "hcmnext-release", Version: "v1.0.0"}},
		Components: []sbom.Component{{Name: "example/tls", Version: "v1.4.2"}},
	}}
}

func slaCritical() vulnimpact.Vulnerability {
	return vulnimpact.Vulnerability{
		ID:                   "GHSA-sla-critical",
		Module:               "example/tls",
		AffectedVersionRange: "=v1.4.2",
		Severity:             vulnimpact.SeverityCritical,
	}
}

func slaAdmission() vulnimpact.AdmissionEvidence {
	return vulnimpact.AdmissionEvidence{
		Admitted: true, Status: "ADMIT", Scope: "prod/pilot-cell",
		ManifestDigest: "sha256:emergency-candidate",
		EvaluatedAt:    time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
		Digest:         "sha256:admission-decision",
	}
}

func slaRollback() vulnimpact.RollbackPlan {
	return vulnimpact.RollbackPlan{
		Steps:      []string{"halt rollout", "restore prior manifest", "verify health"},
		Verified:   true,
		VerifiedBy: "release-captain",
	}
}

// TestTodo_SUPPLY_004 is the PRIMARY contract: each report severity
// resolves an SLA deadline, an unresolved deadline escalates, and a
// critical actively-exploited finding may take the emergency-change lane
// only with admission, compensating control, approver and verified
// rollback plan.
func TestTodo_SUPPLY_004(t *testing.T) {
	report, err := vulnimpact.Analyze(
		[]vulnimpact.Artifact{slaDocument()}, slaCritical(),
		vulnimpact.DeploymentMap{"hcmnext-release": {"tenant-a"}},
	)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(report.Findings))
	}
	detectedAt := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	policy := vulnimpact.DefaultSLAPolicy()
	for severity, wantAfter := range map[vulnimpact.Severity]time.Duration{
		vulnimpact.SeverityCritical: policy.Critical,
		vulnimpact.SeverityHigh:     policy.High,
		vulnimpact.SeverityModerate: policy.Moderate,
		vulnimpact.SeverityLow:      policy.Low,
	} {
		probe := report
		probe.Vulnerability.Severity = severity
		clock, err := vulnimpact.StartClock(probe, detectedAt, policy)
		if err != nil {
			t.Fatalf("StartClock(%s): %v", severity, err)
		}
		if !clock.Deadline.Equal(detectedAt.Add(wantAfter)) {
			t.Fatalf("StartClock(%s) deadline = %s, want %s", severity, clock.Deadline, detectedAt.Add(wantAfter))
		}
	}

	clock, err := vulnimpact.StartClock(report, detectedAt, policy)
	if err != nil {
		t.Fatalf("StartClock: %v", err)
	}
	if got := clock.Status(detectedAt.Add(time.Hour)); got != vulnimpact.SLAOpen {
		t.Fatalf("Status before deadline = %s, want OPEN", got)
	}
	if got := clock.Status(detectedAt.Add(25 * time.Hour)); got != vulnimpact.SLAOverdue {
		t.Fatalf("Status after deadline = %s, want OVERDUE", got)
	}
	escalation, err := vulnimpact.Escalate(clock, report.RemediationOwner, detectedAt.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	if escalation.Owner == "" || escalation.OverdueBy != time.Hour {
		t.Fatalf("escalation = %+v, want named owner and 1h overdue", escalation)
	}

	auth, err := vulnimpact.AuthorizeEmergencyChange(vulnimpact.EmergencyChangeRequest{
		VulnerabilityID:     report.Vulnerability.ID,
		FindingDigest:       report.Findings[0].Digest,
		Severity:            vulnimpact.SeverityCritical,
		ActivelyExploited:   true,
		CompensatingControl: "block affected artifacts from release admission",
		Requester:           "dependency-security",
		Approver:            "security-approver",
		Rollback:            slaRollback(),
		Admission:           slaAdmission(),
	}, detectedAt.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("AuthorizeEmergencyChange: %v", err)
	}
	if auth.Digest == "" || auth.Lane != "emergency" {
		t.Fatalf("authorization = %+v, want digested emergency lane", auth)
	}
	if err := auth.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// emergencyFixture authorizes the standard critical exploited finding.
func emergencyFixture(t *testing.T) vulnimpact.EmergencyAuthorization {
	t.Helper()
	report, err := vulnimpact.Analyze(
		[]vulnimpact.Artifact{slaDocument()}, slaCritical(),
		vulnimpact.DeploymentMap{"hcmnext-release": {"tenant-a"}},
	)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	auth, err := vulnimpact.AuthorizeEmergencyChange(vulnimpact.EmergencyChangeRequest{
		VulnerabilityID:     report.Vulnerability.ID,
		FindingDigest:       report.Findings[0].Digest,
		Severity:            vulnimpact.SeverityCritical,
		ActivelyExploited:   true,
		CompensatingControl: "block affected artifacts from release admission",
		Requester:           "dependency-security",
		Approver:            "security-approver",
		Rollback:            slaRollback(),
		Admission:           slaAdmission(),
	}, time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("AuthorizeEmergencyChange: %v", err)
	}
	return auth
}

const pinnedEmergencyDigest = "5bbfb4badcce82fd138dffc04196556a1e83b16dfb16a187878812733464acf3"

// TestTodo_SUPPLY_004_Golden pins the emergency authorization digest. Any
// field, ordering or serializer change that silently alters the authorized
// record fails here.
func TestTodo_SUPPLY_004_Golden(t *testing.T) {
	auth := emergencyFixture(t)
	if auth.Digest != pinnedEmergencyDigest {
		t.Fatalf("authorization digest = %s, want pinned %s", auth.Digest, pinnedEmergencyDigest)
	}
	if err := auth.Verify(); err != nil {
		t.Fatalf("Verify pinned authorization: %v", err)
	}
	if !strings.Contains(auth.Explain(), "GHSA-sla-critical") || !strings.Contains(auth.Explain(), "security-approver") {
		t.Fatalf("Explain() = %q, want finding identity and approver", auth.Explain())
	}
}

// TestTodo_SUPPLY_004_Security proves the emergency lane refuses every
// degraded request: non-critical severity, no exploitation, missing
// admission, missing control, self-approval and unverified rollback.
func TestTodo_SUPPLY_004_Security(t *testing.T) {
	report, err := vulnimpact.Analyze(
		[]vulnimpact.Artifact{slaDocument()}, slaCritical(),
		vulnimpact.DeploymentMap{"hcmnext-release": {"tenant-a"}},
	)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	base := func() vulnimpact.EmergencyChangeRequest {
		return vulnimpact.EmergencyChangeRequest{
			VulnerabilityID:     report.Vulnerability.ID,
			FindingDigest:       report.Findings[0].Digest,
			Severity:            vulnimpact.SeverityCritical,
			ActivelyExploited:   true,
			CompensatingControl: "block affected artifacts from release admission",
			Requester:           "dependency-security",
			Approver:            "security-approver",
			Rollback:            slaRollback(),
			Admission:           slaAdmission(),
		}
	}
	now := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	unadmitted := slaAdmission()
	unadmitted.Admitted = false
	unadmitted.Status = "REJECT"
	emptyDigest := slaAdmission()
	emptyDigest.Digest = ""
	selfVerified := slaRollback()
	selfVerified.VerifiedBy = "dependency-security"
	cases := map[string]func(*vulnimpact.EmergencyChangeRequest){
		"high severity":        func(r *vulnimpact.EmergencyChangeRequest) { r.Severity = vulnimpact.SeverityHigh },
		"unknown severity":     func(r *vulnimpact.EmergencyChangeRequest) { r.Severity = vulnimpact.SeverityUnknown },
		"not exploited":        func(r *vulnimpact.EmergencyChangeRequest) { r.ActivelyExploited = false },
		"missing control":      func(r *vulnimpact.EmergencyChangeRequest) { r.CompensatingControl = "" },
		"missing approver":     func(r *vulnimpact.EmergencyChangeRequest) { r.Approver = "" },
		"self approval":        func(r *vulnimpact.EmergencyChangeRequest) { r.Approver = r.Requester },
		"unverified rollback":  func(r *vulnimpact.EmergencyChangeRequest) { r.Rollback.Verified = false },
		"empty rollback steps": func(r *vulnimpact.EmergencyChangeRequest) { r.Rollback.Steps = nil },
		"self verified rollback": func(r *vulnimpact.EmergencyChangeRequest) {
			r.Rollback = selfVerified
		},
		"unadmitted candidate": func(r *vulnimpact.EmergencyChangeRequest) { r.Admission = unadmitted },
		"anonymous admission":  func(r *vulnimpact.EmergencyChangeRequest) { r.Admission = emptyDigest },
		"missing finding":      func(r *vulnimpact.EmergencyChangeRequest) { r.FindingDigest = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := base()
			mutate(&req)
			if _, err := vulnimpact.AuthorizeEmergencyChange(req, now); !errors.Is(err, vulnimpact.ErrEmergencyRefused) {
				t.Fatalf("AuthorizeEmergencyChange = %v, want ErrEmergencyRefused", err)
			}
		})
	}

	// A tampered admission digest on an otherwise valid authorization is
	// detected by Verify.
	auth := emergencyFixture(t)
	auth.Admission.Digest = "sha256:forged"
	if err := auth.Verify(); !errors.Is(err, vulnimpact.ErrAuthorizationTampered) {
		t.Fatalf("Verify forged admission = %v, want ErrAuthorizationTampered", err)
	}
}

// TestTodo_SUPPLY_004_Integration wires the whole chain: SUPPLY-002 impact
// analysis starts the SLA clock, the overdue clock escalates, and the
// escalation's critical finding takes the emergency lane with admission.
func TestTodo_SUPPLY_004_Integration(t *testing.T) {
	detectedAt := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	report, err := vulnimpact.Analyze(
		[]vulnimpact.Artifact{slaDocument()}, slaCritical(),
		vulnimpact.DeploymentMap{"hcmnext-release": {"tenant-a", "tenant-b"}},
	)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	clock, err := vulnimpact.StartClock(report, detectedAt, vulnimpact.DefaultSLAPolicy())
	if err != nil {
		t.Fatalf("StartClock: %v", err)
	}
	if clock.ReportDigest != report.Digest {
		t.Fatalf("clock binds %s, report is %s", clock.ReportDigest, report.Digest)
	}
	now := detectedAt.Add(26 * time.Hour)
	if clock.Status(now) != vulnimpact.SLAOverdue {
		t.Fatalf("Status = %s, want OVERDUE", clock.Status(now))
	}
	escalation, err := vulnimpact.Escalate(clock, report.RemediationOwner, now)
	if err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	if err := escalation.Verify(); err != nil {
		t.Fatalf("escalation Verify: %v", err)
	}
	auth, err := vulnimpact.AuthorizeEmergencyChange(vulnimpact.EmergencyChangeRequest{
		VulnerabilityID:     report.Vulnerability.ID,
		FindingDigest:       report.Findings[0].Digest,
		Severity:            escalation.Severity,
		ActivelyExploited:   true,
		CompensatingControl: report.CompensatingControl,
		Requester:           report.RemediationOwner,
		Approver:            "security-approver",
		Rollback:            slaRollback(),
		Admission:           slaAdmission(),
	}, now)
	if err != nil {
		t.Fatalf("AuthorizeEmergencyChange: %v", err)
	}
	if auth.FindingDigest != report.Findings[0].Digest || auth.CompensatingControl != report.CompensatingControl {
		t.Fatalf("authorization = %+v, want it bound to the analyzed finding", auth)
	}
	if err := auth.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestTodo_SUPPLY_004_Fault covers malformed clocks, policies and requests:
// each one fails closed with a typed sentinel.
func TestTodo_SUPPLY_004_Fault(t *testing.T) {
	report, err := vulnimpact.Analyze(
		[]vulnimpact.Artifact{slaDocument()}, slaCritical(),
		vulnimpact.DeploymentMap{"hcmnext-release": {"tenant-a"}},
	)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	detectedAt := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	policy := vulnimpact.DefaultSLAPolicy()

	t.Run("clock faults", func(t *testing.T) {
		unscored := report
		unscored.Digest = ""
		unknown := report
		unknown.Vulnerability.Severity = vulnimpact.SeverityUnknown
		zeroPolicy := policy
		zeroPolicy.Critical = 0
		for name, args := range map[string]struct {
			report vulnimpact.Report
			at     time.Time
			policy vulnimpact.SLAPolicy
		}{
			"unscored report":  {report: unscored, at: detectedAt, policy: policy},
			"unknown severity": {report: unknown, at: detectedAt, policy: policy},
			"zero time":        {report: report, at: time.Time{}, policy: policy},
			"zero duration":    {report: report, at: detectedAt, policy: zeroPolicy},
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := vulnimpact.StartClock(args.report, args.at, args.policy); !errors.Is(err, vulnimpact.ErrInvalidSLA) {
					t.Fatalf("StartClock = %v, want ErrInvalidSLA", err)
				}
			})
		}
		if _, err := policy.DeadlineFor(vulnimpact.SeverityUnknown); !errors.Is(err, vulnimpact.ErrInvalidSLA) {
			t.Fatalf("DeadlineFor(UNKNOWN) = %v, want ErrInvalidSLA", err)
		}
	})

	t.Run("escalation faults", func(t *testing.T) {
		clock, err := vulnimpact.StartClock(report, detectedAt, policy)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := vulnimpact.Escalate(clock, report.RemediationOwner, detectedAt.Add(time.Hour)); !errors.Is(err, vulnimpact.ErrSLANotOverdue) {
			t.Fatalf("early Escalate = %v, want ErrSLANotOverdue", err)
		}
		if _, err := vulnimpact.Escalate(clock, "", detectedAt.Add(25*time.Hour)); !errors.Is(err, vulnimpact.ErrInvalidSLA) {
			t.Fatalf("ownerless Escalate = %v, want ErrInvalidSLA", err)
		}
		if _, err := vulnimpact.Escalate(clock, report.RemediationOwner, time.Time{}); !errors.Is(err, vulnimpact.ErrInvalidSLA) {
			t.Fatalf("timeless Escalate = %v, want ErrInvalidSLA", err)
		}
		remediated, err := clock.MarkRemediated("fix-commit-1")
		if err != nil {
			t.Fatal(err)
		}
		if got := remediated.Status(detectedAt.Add(1000 * time.Hour)); got != vulnimpact.SLARemediated {
			t.Fatalf("remediated Status = %s, want REMEDIATED", got)
		}
		if _, err := vulnimpact.Escalate(remediated, report.RemediationOwner, detectedAt.Add(1000*time.Hour)); !errors.Is(err, vulnimpact.ErrSLANotOverdue) {
			t.Fatalf("remediated Escalate = %v, want ErrSLANotOverdue", err)
		}
		if _, err := clock.MarkRemediated(""); !errors.Is(err, vulnimpact.ErrInvalidSLA) {
			t.Fatalf("evidenceless remediation = %v, want ErrInvalidSLA", err)
		}
		if clock.Explain() == "" {
			t.Fatal("clock Explain is empty")
		}
	})

	t.Run("authorization time", func(t *testing.T) {
		req := vulnimpact.EmergencyChangeRequest{
			VulnerabilityID: "GHSA-x", FindingDigest: "d",
			Severity: vulnimpact.SeverityCritical, ActivelyExploited: true,
			CompensatingControl: "c", Requester: "r", Approver: "a",
			Rollback: slaRollback(), Admission: slaAdmission(),
		}
		if _, err := vulnimpact.AuthorizeEmergencyChange(req, time.Time{}); !errors.Is(err, vulnimpact.ErrInvalidSLA) {
			t.Fatalf("timeless authorize = %v, want ErrInvalidSLA", err)
		}
	})
}

// TestTodo_SUPPLY_004_Recovery proves the overdue path stays governed: the
// escalation verifies after the deadline and an emergency authorization
// issued late still verifies, so recovery never runs on unaudited state.
func TestTodo_SUPPLY_004_Recovery(t *testing.T) {
	detectedAt := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	report, err := vulnimpact.Analyze(
		[]vulnimpact.Artifact{slaDocument()}, slaCritical(),
		vulnimpact.DeploymentMap{"hcmnext-release": {"tenant-a"}},
	)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	clock, err := vulnimpact.StartClock(report, detectedAt, vulnimpact.DefaultSLAPolicy())
	if err != nil {
		t.Fatal(err)
	}
	late := detectedAt.Add(72 * time.Hour)
	escalation, err := vulnimpact.Escalate(clock, report.RemediationOwner, late)
	if err != nil {
		t.Fatalf("late Escalate: %v", err)
	}
	if escalation.OverdueBy != 48*time.Hour {
		t.Fatalf("OverdueBy = %s, want 48h", escalation.OverdueBy)
	}
	if err := escalation.Verify(); err != nil {
		t.Fatalf("late escalation Verify: %v", err)
	}
	auth, err := vulnimpact.AuthorizeEmergencyChange(vulnimpact.EmergencyChangeRequest{
		VulnerabilityID: report.Vulnerability.ID, FindingDigest: report.Findings[0].Digest,
		Severity: vulnimpact.SeverityCritical, ActivelyExploited: true,
		CompensatingControl: report.CompensatingControl, Requester: report.RemediationOwner,
		Approver: "security-approver", Rollback: slaRollback(), Admission: slaAdmission(),
	}, late)
	if err != nil {
		t.Fatalf("late authorize: %v", err)
	}
	if err := auth.Verify(); err != nil {
		t.Fatalf("late authorization Verify: %v", err)
	}
}

// TestTodo_SUPPLY_004_Mutation proves authorization and escalation digests
// bind their contents: any post-authorization edit is detected.
func TestTodo_SUPPLY_004_Mutation(t *testing.T) {
	auth := emergencyFixture(t)
	for name, mutate := range map[string]func(*vulnimpact.EmergencyAuthorization){
		"control":  func(a *vulnimpact.EmergencyAuthorization) { a.CompensatingControl = "nothing" },
		"approver": func(a *vulnimpact.EmergencyAuthorization) { a.Approver = "dependency-security" },
		"rollback": func(a *vulnimpact.EmergencyAuthorization) { a.Rollback.Steps = []string{"ship it"} },
		"lane":     func(a *vulnimpact.EmergencyAuthorization) { a.Lane = "ordinary" },
		"digest":   func(a *vulnimpact.EmergencyAuthorization) { a.Digest = "sha256:edited" },
	} {
		t.Run(name, func(t *testing.T) {
			forged := auth
			mutate(&forged)
			if err := forged.Verify(); !errors.Is(err, vulnimpact.ErrAuthorizationTampered) {
				t.Fatalf("Verify = %v, want ErrAuthorizationTampered", err)
			}
		})
	}

	clock, err := vulnimpact.StartClock(vulnimpact.Report{
		Vulnerability: slaCritical(), Digest: "report-digest",
	}, time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC), vulnimpact.DefaultSLAPolicy())
	if err != nil {
		t.Fatal(err)
	}
	escalation, err := vulnimpact.Escalate(clock, "dependency-security", clock.Deadline.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	forged := escalation
	forged.Owner = "intruder"
	if err := forged.Verify(); !errors.Is(err, vulnimpact.ErrAuthorizationTampered) {
		t.Fatalf("escalation Verify = %v, want ErrAuthorizationTampered", err)
	}
	if err := escalation.Verify(); err != nil {
		t.Fatalf("pristine escalation Verify: %v", err)
	}
}
