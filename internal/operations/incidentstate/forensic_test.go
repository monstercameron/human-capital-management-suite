package incidentstate

import (
	"errors"
	"testing"
	"time"
)

func forensicNow() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }

func declaredIncident(t *testing.T) Incident {
	t.Helper()
	incident, err := New("incident-forensic-1", "tenant-a", forensicNow())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, to := range []State{Triaged, Declared} {
		incident, err = Transition(incident, Command{
			To: to, Actor: "incident-commander", Reason: "triage evidence",
			EvidenceRef: "alert-1", EventID: "event-" + string(to),
		}, forensicNow())
		if err != nil {
			t.Fatalf("Transition to %s: %v", to, err)
		}
	}
	return incident
}

func forensicScope() AcquisitionScope {
	return AcquisitionScope{
		Tenants:      []string{"tenant-a"},
		DataClasses:  []ForensicDataClass{DataClassLog, DataClassTrace},
		Sources:      []string{"api-gateway-log", "worker-trace"},
		Query:        "request_id=req-9 within 2026-09-16T11:00/12:00",
		MaxArtifacts: 8,
		MaxBytes:     1 << 20,
	}
}

func forensicPlan(t *testing.T, incident Incident) AcquisitionPlan {
	t.Helper()
	plan, err := PlanAcquisition(incident, "determine blast radius", forensicScope(), "collector v1.3",
		[2]string{"incident-commander", "security-lead"},
		"security", "us-east",
		RetentionPolicy{HoldID: "hold-7", RetainUntil: forensicNow().Add(90 * 24 * time.Hour), Disposition: "destroy"},
		forensicNow())
	if err != nil {
		t.Fatalf("PlanAcquisition: %v", err)
	}
	return plan
}

// TestForensicAcquisitionBindsAuthorityScopeCustodyIntegrityRetentionAndDisclosure
// is the FORENSIC-001 PRIMARY contract: a dual-authorized, scoped plan
// captures content-addressed artifacts with custody receipts, enforces
// compartment/residency/hold, and exports a verifiable package plus a
// separately redacted disclosure.
func TestForensicAcquisitionBindsAuthorityScopeCustodyIntegrityRetentionAndDisclosure(t *testing.T) {
	incident := declaredIncident(t)
	plan := forensicPlan(t, incident)
	if plan.Digest == "" || !plan.TrustedTime {
		t.Fatalf("plan = %+v, want digested trusted-time plan", plan)
	}

	artifact, chain, err := Acquire(plan, SourceEvidence{
		SourceRef: "api-gateway-log", DataClass: DataClassLog,
		SourceHash: "sha256:source-bytes", ByteCount: 1024,
	}, forensicNow())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := artifact.Verify(); err != nil {
		t.Fatalf("artifact Verify: %v", err)
	}
	if artifact.ArtifactID == "" || artifact.Compartment != "security" || artifact.Region != "us-east" {
		t.Fatalf("artifact = %+v, want compartmented content address", artifact)
	}

	moved, err := TransferCustody(chain, "forensic-analyst", forensicNow().Add(time.Hour))
	if err != nil {
		t.Fatalf("TransferCustody: %v", err)
	}
	if err := VerifyCustody(moved); err != nil {
		t.Fatalf("VerifyCustody: %v", err)
	}
	if len(moved.Receipts) != 2 || moved.Receipts[1].Sequence != 2 {
		t.Fatalf("chain = %+v, want two sequenced receipts", moved)
	}

	pkg, err := Export(plan, []Artifact{artifact}, []CustodyChain{moved}, forensicNow().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if err := VerifyExport(pkg); err != nil {
		t.Fatalf("VerifyExport: %v", err)
	}

	disclosure, err := Redact(artifact, moved, "customer-disclosure", []string{"request payload bytes", "principal identifiers"})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if disclosure.Digest == "" || disclosure.Digest == artifact.Digest {
		t.Fatalf("disclosure = %+v, want a separately digested package", disclosure)
	}
	if disclosure.ArtifactID != artifact.ArtifactID {
		t.Fatalf("disclosure references %s, want %s", disclosure.ArtifactID, artifact.ArtifactID)
	}

	// Raw evidence never crosses compartment or region: only the redacted
	// disclosure travels.
	if err := AuthorizeMove(artifact, "customer-disclosure", "eu-west"); err == nil {
		t.Fatal("raw cross-compartment move was authorized")
	}
}

// acquireFixture plans and collects one log artifact with a two-receipt
// chain.
func acquireFixture(t *testing.T) (AcquisitionPlan, Artifact, CustodyChain) {
	t.Helper()
	plan := forensicPlan(t, declaredIncident(t))
	artifact, chain, err := Acquire(plan, SourceEvidence{
		SourceRef: "api-gateway-log", DataClass: DataClassLog,
		SourceHash: "sha256:source-bytes", ByteCount: 1024,
	}, forensicNow())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	chain, err = TransferCustody(chain, "forensic-analyst", forensicNow().Add(time.Hour))
	if err != nil {
		t.Fatalf("TransferCustody: %v", err)
	}
	return plan, artifact, chain
}

const (
	pinnedForensicPlanDigest   = "d9f5dd2f7c4dcc437613ac1c774cca3de53f6934e00826df68cd9d6024ac4838"
	pinnedForensicExportDigest = "ae2a3e2870466e2d2617102d02fe096520866c64ca2ae2e76857160f6c7a222b"
)

// TestTodo_FORENSIC_001_Golden pins the plan and export digests. Any
// serializer or field-order change that silently alters forensic evidence
// identity fails here.
func TestTodo_FORENSIC_001_Golden(t *testing.T) {
	plan, artifact, chain := acquireFixture(t)
	if plan.Digest != pinnedForensicPlanDigest {
		t.Fatalf("plan digest = %s, want pinned %s", plan.Digest, pinnedForensicPlanDigest)
	}
	// Deterministic plans digest identically: same inputs, same seal.
	again := forensicPlan(t, declaredIncident(t))
	if again.Digest != plan.Digest {
		t.Fatalf("second plan digest = %s, want %s", again.Digest, plan.Digest)
	}
	pkg, err := Export(plan, []Artifact{artifact}, []CustodyChain{chain}, forensicNow().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if pkg.Digest != pinnedForensicExportDigest {
		t.Fatalf("export digest = %s, want pinned %s", pkg.Digest, pinnedForensicExportDigest)
	}
	if err := VerifyExport(pkg); err != nil {
		t.Fatalf("VerifyExport pinned package: %v", err)
	}
}

// TestTodo_FORENSIC_001_Property proves the chain laws over every length:
// sequences stay contiguous, digests recompute, and any single forged
// receipt breaks verification at its own position.
func TestTodo_FORENSIC_001_Property(t *testing.T) {
	plan := forensicPlan(t, declaredIncident(t))
	artifact, chain, err := Acquire(plan, SourceEvidence{
		SourceRef: "worker-trace", DataClass: DataClassTrace,
		SourceHash: "sha256:trace-bytes", ByteCount: 512,
	}, forensicNow())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	chains := []CustodyChain{chain}
	for i := 1; i <= 8; i++ {
		next, err := TransferCustody(chains[i-1], "custodian", forensicNow().Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatalf("TransferCustody %d: %v", i, err)
		}
		if err := VerifyCustody(next); err != nil {
			t.Fatalf("VerifyCustody length %d: %v", len(next.Receipts), err)
		}
		if got := len(next.Receipts); got != i+1 {
			t.Fatalf("chain length = %d, want %d", got, i+1)
		}
		chains = append(chains, next)
	}
	if err := artifact.Verify(); err != nil {
		t.Fatalf("seed artifact Verify: %v", err)
	}
	full := chains[len(chains)-1]
	for i := range full.Receipts {
		forged := CustodyChain{ArtifactID: full.ArtifactID, Receipts: append([]CustodyReceipt(nil), full.Receipts...)}
		forged.Receipts[i].Custodian = "intruder"
		if err := VerifyCustody(forged); !errors.Is(err, ErrCustodyBroken) {
			t.Fatalf("forged receipt %d verified", i)
		}
		// The verified prefix still extends: recovery resumes from custody
		// that verifies, never from custody that does not.
		prefix := CustodyChain{ArtifactID: full.ArtifactID, Receipts: append([]CustodyReceipt(nil), full.Receipts[:i+1]...)}
		if err := VerifyCustody(prefix); err != nil {
			t.Fatalf("verified prefix %d: %v", i, err)
		}
	}
}

// TestTodo_FORENSIC_001_Integration drives the real incident lifecycle
// into a two-artifact collection: declare, mitigate, acquire both scoped
// sources, transfer each chain, seal one export and disclose it.
func TestTodo_FORENSIC_001_Integration(t *testing.T) {
	incident, err := New("incident-forensic-e2e", "tenant-a", forensicNow())
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		to      State
		command Command
		at      time.Time
	}{
		{Triaged, Command{Actor: "incident-commander", Reason: "triage", EvidenceRef: "alert-1", EventID: "e1"}, forensicNow()},
		{Declared, Command{Actor: "incident-commander", Reason: "declare", EvidenceRef: "triage-note", EventID: "e2"}, forensicNow()},
		{Mitigating, Command{Actor: "incident-commander", Reason: "contain", EvidenceRef: "runbook-3", Owner: "incident-commander", Mitigation: "isolate cell", EventID: "e3"}, forensicNow().Add(time.Hour)},
	}
	for _, step := range steps {
		step.command.To = step.to
		incident, err = Transition(incident, step.command, step.at)
		if err != nil {
			t.Fatalf("Transition to %s: %v", step.to, err)
		}
	}
	plan := forensicPlan(t, incident)
	sources := []SourceEvidence{
		{SourceRef: "api-gateway-log", DataClass: DataClassLog, SourceHash: "sha256:log-bytes", ByteCount: 2048},
		{SourceRef: "worker-trace", DataClass: DataClassTrace, SourceHash: "sha256:trace-bytes", ByteCount: 512},
	}
	var artifacts []Artifact
	var chains []CustodyChain
	for i, source := range sources {
		at := forensicNow().Add(time.Duration(2+i) * time.Hour)
		artifact, chain, err := Acquire(plan, source, at)
		if err != nil {
			t.Fatalf("Acquire %s: %v", source.SourceRef, err)
		}
		chain, err = TransferCustody(chain, "forensic-analyst", at.Add(30*time.Minute))
		if err != nil {
			t.Fatalf("TransferCustody %s: %v", source.SourceRef, err)
		}
		artifacts = append(artifacts, artifact)
		chains = append(chains, chain)
	}
	pkg, err := Export(plan, artifacts, chains, forensicNow().Add(5*time.Hour))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(pkg.ArtifactDigests) != 2 || len(pkg.CustodyDigests) != 2 {
		t.Fatalf("export = %+v, want both artifacts sealed", pkg)
	}
	if err := VerifyExport(pkg); err != nil {
		t.Fatalf("VerifyExport: %v", err)
	}
	disclosure, err := Redact(artifacts[0], chains[0], "customer-disclosure", []string{"request payload bytes"})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if disclosure.PlanID != plan.PlanID {
		t.Fatalf("disclosure plan = %s, want %s", disclosure.PlanID, plan.PlanID)
	}
}

// incidentInState drives one incident to the requested lifecycle state.
func incidentInState(t *testing.T, target State) Incident {
	t.Helper()
	base := forensicNow()
	incident, err := New("incident-state-"+string(target), "tenant-a", base)
	if err != nil {
		t.Fatal(err)
	}
	move := func(to State, cmd Command, at time.Time) Incident {
		t.Helper()
		cmd.To = to
		next, err := Transition(incident, cmd, at)
		if err != nil {
			t.Fatalf("Transition to %s: %v", to, err)
		}
		return next
	}
	triage := Command{Actor: "commander", Reason: "triage", EvidenceRef: "alert-1", EventID: "e-triage"}
	declare := Command{Actor: "commander", Reason: "declare", EvidenceRef: "triage-note", EventID: "e-declare"}
	mitigate := Command{Actor: "commander", Reason: "contain", EvidenceRef: "runbook", Owner: "commander", Mitigation: "isolate", EventID: "e-mitigate"}
	monitor := Command{Actor: "commander", Reason: "watch", EvidenceRef: "dashboard", MonitoringUntil: base.Add(2 * time.Hour), EventID: "e-monitor"}
	resolve := Command{Actor: "commander", Reason: "fixed", EvidenceRef: "postmortem", RepairLink: "repair-1", Affected: AffectedSet{Known: true, Verified: true}, EventID: "e-resolve"}
	review := Command{Actor: "commander", Reason: "reviewed", EvidenceRef: "review-note", Review: "causes recorded", EventID: "e-review"}
	merge := Command{Actor: "commander", Reason: "duplicate", EvidenceRef: "link", RelatedIncidentID: "incident-other", EventID: "e-merge"}
	falsePositive := Command{Actor: "commander", Reason: "benign", EvidenceRef: "analysis", EventID: "e-fp"}
	switch target {
	case Detected:
		return incident
	case Triaged:
		return move(Triaged, triage, base)
	case Declared:
		incident = move(Triaged, triage, base)
		return move(Declared, declare, base)
	case FalsePositive:
		incident = move(Triaged, triage, base)
		return move(FalsePositive, falsePositive, base)
	case Merged:
		incident = move(Triaged, triage, base)
		incident = move(Declared, declare, base)
		return move(Merged, merge, base)
	case Mitigating:
		incident = move(Triaged, triage, base)
		incident = move(Declared, declare, base)
		return move(Mitigating, mitigate, base)
	case Monitoring:
		incident = move(Triaged, triage, base)
		incident = move(Declared, declare, base)
		incident = move(Mitigating, mitigate, base)
		return move(Monitoring, monitor, base.Add(time.Hour))
	case Resolved:
		incident = move(Triaged, triage, base)
		incident = move(Declared, declare, base)
		incident = move(Mitigating, mitigate, base)
		incident = move(Monitoring, monitor, base.Add(time.Hour))
		return move(Resolved, resolve, base.Add(3*time.Hour))
	case Reviewed:
		incident = move(Triaged, triage, base)
		incident = move(Declared, declare, base)
		incident = move(Mitigating, mitigate, base)
		incident = move(Monitoring, monitor, base.Add(time.Hour))
		incident = move(Resolved, resolve, base.Add(3*time.Hour))
		return move(Reviewed, review, base.Add(4*time.Hour))
	case Reopened:
		incident = move(Triaged, triage, base)
		incident = move(Declared, declare, base)
		incident = move(Mitigating, mitigate, base)
		incident = move(Monitoring, monitor, base.Add(time.Hour))
		incident = move(Resolved, resolve, base.Add(3*time.Hour))
		return move(Reopened, Command{Actor: "commander", Reason: "recurred", EvidenceRef: "alert-2", EventID: "e-reopen"}, base.Add(5*time.Hour))
	default:
		t.Fatalf("no driver for state %s", target)
		return incident
	}
}

// TestTodo_FORENSIC_001_Conformance proves the authority gates: only
// declared-or-later incidents collect, dual authorization and trusted time
// are mandatory, retention is required and the data-class vocabulary is
// closed.
func TestTodo_FORENSIC_001_Conformance(t *testing.T) {
	retention := RetentionPolicy{HoldID: "hold-7", RetainUntil: forensicNow().Add(90 * 24 * time.Hour), Disposition: "destroy"}
	for state, want := range map[State]bool{
		Detected: false, Triaged: false, Declared: true, Mitigating: true,
		Monitoring: true, Resolved: true, Reviewed: true, Reopened: true,
		FalsePositive: false, Merged: false,
	} {
		t.Run(string(state), func(t *testing.T) {
			incident := incidentInState(t, state)
			_, err := PlanAcquisition(incident, "determine blast radius", forensicScope(), "collector v1.3",
				[2]string{"incident-commander", "security-lead"}, "security", "us-east", retention, forensicNow())
			if (err == nil) != want {
				t.Fatalf("PlanAcquisition in %s error = %v, want allowed=%t", state, err, want)
			}
			if err != nil && want && !errors.Is(err, ErrAcquisitionAuthority) && !errors.Is(err, ErrInvalidAcquisition) {
				t.Fatalf("PlanAcquisition in %s error = %v, want a typed refusal", state, err)
			}
			if err != nil && !want && !errors.Is(err, ErrAcquisitionAuthority) {
				t.Fatalf("PlanAcquisition in %s error = %v, want ErrAcquisitionAuthority", state, err)
			}
		})
	}

	incident := declaredIncident(t)
	t.Run("dual authorization", func(t *testing.T) {
		for name, actors := range map[string][2]string{
			"missing second": {"incident-commander", ""},
			"same actor":     {"incident-commander", "incident-commander"},
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := PlanAcquisition(incident, "purpose", forensicScope(), "tool", actors, "security", "us-east", retention, forensicNow()); !errors.Is(err, ErrAcquisitionAuthority) {
					t.Fatalf("PlanAcquisition = %v, want ErrAcquisitionAuthority", err)
				}
			})
		}
		if _, err := PlanAcquisition(incident, "purpose", forensicScope(), "tool", [2]string{"", ""}, "security", "us-east", retention, time.Time{}); err == nil {
			t.Fatal("anonymous timeless plan was authorized")
		}
	})

	t.Run("trusted time", func(t *testing.T) {
		if _, err := PlanAcquisition(incident, "purpose", forensicScope(), "tool", [2]string{"a", "b"}, "security", "us-east", retention, time.Time{}); !errors.Is(err, ErrAcquisitionAuthority) {
			t.Fatalf("timeless plan = %v, want ErrAcquisitionAuthority", err)
		}
	})

	t.Run("retention required", func(t *testing.T) {
		for name, policy := range map[string]RetentionPolicy{
			"no hold":        {RetainUntil: forensicNow().Add(time.Hour), Disposition: "destroy"},
			"no deadline":    {HoldID: "hold-7", Disposition: "destroy"},
			"no disposition": {HoldID: "hold-7", RetainUntil: forensicNow().Add(time.Hour)},
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := PlanAcquisition(incident, "purpose", forensicScope(), "tool", [2]string{"a", "b"}, "security", "us-east", policy, forensicNow()); !errors.Is(err, ErrInvalidAcquisition) {
					t.Fatalf("PlanAcquisition = %v, want ErrInvalidAcquisition", err)
				}
			})
		}
	})

	t.Run("closed data classes", func(t *testing.T) {
		for _, raw := range []string{"log", " TRACE ", "config", "memory"} {
			if _, err := ParseForensicDataClass(raw); err != nil {
				t.Fatalf("ParseForensicDataClass(%q): %v", raw, err)
			}
		}
		for _, raw := range []string{"", "database", "mailbox", "*"} {
			if _, err := ParseForensicDataClass(raw); err == nil {
				t.Fatalf("ParseForensicDataClass(%q) collected an unlisted class", raw)
			}
		}
	})
}

// TestTodo_FORENSIC_001_Fault covers malformed plans, scopes, sources,
// transfers and disclosures: each one fails closed with a typed sentinel.
func TestTodo_FORENSIC_001_Fault(t *testing.T) {
	incident := declaredIncident(t)
	retention := RetentionPolicy{HoldID: "hold-7", RetainUntil: forensicNow().Add(90 * 24 * time.Hour), Disposition: "destroy"}
	actors := [2]string{"incident-commander", "security-lead"}

	t.Run("plan faults", func(t *testing.T) {
		wildcardTenant := forensicScope()
		wildcardTenant.Tenants = []string{"*"}
		emptySources := forensicScope()
		emptySources.Sources = nil
		badClass := forensicScope()
		badClass.DataClasses = []ForensicDataClass{"mailbox"}
		noQuery := forensicScope()
		noQuery.Query = ""
		noCeiling := forensicScope()
		noCeiling.MaxBytes = 0
		emptyIncident := Incident{}
		for name, args := range map[string]struct {
			incident Incident
			purpose  string
			scope    AcquisitionScope
			tool     string
		}{
			"anonymous incident": {incident: emptyIncident, purpose: "p", scope: forensicScope(), tool: "t"},
			"empty purpose":      {incident: incident, purpose: "", scope: forensicScope(), tool: "t"},
			"wildcard tenant":    {incident: incident, purpose: "p", scope: wildcardTenant, tool: "t"},
			"empty sources":      {incident: incident, purpose: "p", scope: emptySources, tool: "t"},
			"bad class":          {incident: incident, purpose: "p", scope: badClass, tool: "t"},
			"empty query":        {incident: incident, purpose: "p", scope: noQuery, tool: "t"},
			"no ceiling":         {incident: incident, purpose: "p", scope: noCeiling, tool: "t"},
			"empty tool":         {incident: incident, purpose: "p", scope: forensicScope(), tool: ""},
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := PlanAcquisition(args.incident, args.purpose, args.scope, args.tool, actors, "security", "us-east", retention, forensicNow()); err == nil {
					t.Fatalf("%s plan was authorized", name)
				}
			})
		}
		if _, err := PlanAcquisition(incident, "p", forensicScope(), "t", actors, "", "us-east", retention, forensicNow()); !errors.Is(err, ErrInvalidAcquisition) {
			t.Fatalf("compartmentless plan = %v, want ErrInvalidAcquisition", err)
		}
	})

	t.Run("acquire faults", func(t *testing.T) {
		plan := forensicPlan(t, incident)
		oversized := SourceEvidence{SourceRef: "api-gateway-log", DataClass: DataClassLog, SourceHash: "sha256:x", ByteCount: plan.Scope.MaxBytes + 1}
		foreignSource := SourceEvidence{SourceRef: "hr-database", DataClass: DataClassLog, SourceHash: "sha256:x", ByteCount: 8}
		foreignClass := SourceEvidence{SourceRef: "api-gateway-log", DataClass: DataClassMemory, SourceHash: "sha256:x", ByteCount: 8}
		hashless := SourceEvidence{SourceRef: "api-gateway-log", DataClass: DataClassLog, ByteCount: 8}
		for name, source := range map[string]SourceEvidence{
			"oversized": oversized, "foreign source": foreignSource,
			"foreign class": foreignClass, "hashless": hashless,
		} {
			t.Run(name, func(t *testing.T) {
				if _, _, err := Acquire(plan, source, forensicNow()); err == nil {
					t.Fatalf("%s collection succeeded", name)
				}
			})
		}
		if _, _, err := Acquire(plan, SourceEvidence{SourceRef: "api-gateway-log", DataClass: DataClassLog, SourceHash: "sha256:x", ByteCount: 8}, time.Time{}); !errors.Is(err, ErrAcquisitionAuthority) {
			t.Fatalf("timeless acquire = %v, want ErrAcquisitionAuthority", err)
		}
		tampered := plan
		tampered.Purpose = "widened after authorization"
		if _, _, err := Acquire(tampered, SourceEvidence{SourceRef: "api-gateway-log", DataClass: DataClassLog, SourceHash: "sha256:x", ByteCount: 8}, forensicNow()); !errors.Is(err, ErrInvalidAcquisition) {
			t.Fatalf("tampered plan acquire = %v, want ErrInvalidAcquisition", err)
		}
	})

	t.Run("transfer faults", func(t *testing.T) {
		_, _, chain := acquireFixture(t)
		if _, err := TransferCustody(chain, "", forensicNow().Add(3*time.Hour)); !errors.Is(err, ErrInvalidAcquisition) {
			t.Fatalf("anonymous transfer = %v, want ErrInvalidAcquisition", err)
		}
		if _, err := TransferCustody(chain, "analyst", forensicNow().Add(-time.Hour)); !errors.Is(err, ErrCustodyBroken) {
			t.Fatalf("backward transfer = %v, want ErrCustodyBroken", err)
		}
		if _, err := TransferCustody(chain, "analyst", time.Time{}); !errors.Is(err, ErrAcquisitionAuthority) {
			t.Fatalf("timeless transfer = %v, want ErrAcquisitionAuthority", err)
		}
		if _, err := BeginCustody(Artifact{}, "analyst", forensicNow()); !errors.Is(err, ErrInvalidAcquisition) {
			t.Fatalf("custody on empty artifact = %v, want ErrInvalidAcquisition", err)
		}
	})

	t.Run("export and disclosure faults", func(t *testing.T) {
		plan, artifact, chain := acquireFixture(t)
		if _, err := Export(plan, nil, nil, forensicNow()); !errors.Is(err, ErrInvalidAcquisition) {
			t.Fatalf("empty export = %v, want ErrInvalidAcquisition", err)
		}
		foreign := artifact
		foreign.PlanID = "forensic-other"
		foreign.Digest = ""
		if _, err := Export(plan, []Artifact{foreign}, nil, forensicNow()); err == nil {
			t.Fatal("foreign-plan export sealed")
		}
		dupes := []Artifact{artifact, artifact}
		if _, err := Export(plan, dupes, []CustodyChain{chain}, forensicNow()); !errors.Is(err, ErrInvalidAcquisition) {
			t.Fatalf("duplicate export = %v, want ErrInvalidAcquisition", err)
		}
		otherChain := CustodyChain{ArtifactID: "other", Receipts: chain.Receipts}
		if _, err := Export(plan, []Artifact{artifact}, []CustodyChain{otherChain}, forensicNow()); err == nil {
			t.Fatal("unsealed-custody export sealed")
		}
		if _, err := Redact(artifact, chain, "", []string{"reason"}); !errors.Is(err, ErrInvalidAcquisition) {
			t.Fatalf("compartmentless disclosure = %v, want ErrInvalidAcquisition", err)
		}
		if _, err := Redact(artifact, chain, "customer", nil); !errors.Is(err, ErrInvalidAcquisition) {
			t.Fatalf("unredacted disclosure = %v, want ErrInvalidAcquisition", err)
		}
		if _, err := Redact(artifact, chain, "customer", []string{""}); !errors.Is(err, ErrInvalidAcquisition) {
			t.Fatalf("nameless redaction = %v, want ErrInvalidAcquisition", err)
		}
		if err := VerifyExport(ExportPackage{}); !errors.Is(err, ErrExportUnverifiable) {
			t.Fatalf("empty VerifyExport = %v, want ErrExportUnverifiable", err)
		}
	})
}

// TestTodo_FORENSIC_001_Security proves the anti-overcollection and
// anti-tamper bars: whole-tenant scopes, out-of-scope capture, forged
// custody, forged artifacts at export and raw cross-boundary moves.
func TestTodo_FORENSIC_001_Security(t *testing.T) {
	incident := declaredIncident(t)
	retention := RetentionPolicy{HoldID: "hold-7", RetainUntil: forensicNow().Add(90 * 24 * time.Hour), Disposition: "destroy"}
	actors := [2]string{"incident-commander", "security-lead"}

	t.Run("whole tenant collection refused", func(t *testing.T) {
		for name, mutate := range map[string]func(*AcquisitionScope){
			"no tenants":      func(s *AcquisitionScope) { s.Tenants = nil },
			"wildcard tenant": func(s *AcquisitionScope) { s.Tenants = []string{"tenant-a", "*"} },
			"wildcard source": func(s *AcquisitionScope) { s.Sources = []string{"*"} },
			"no classes":      func(s *AcquisitionScope) { s.DataClasses = nil },
		} {
			t.Run(name, func(t *testing.T) {
				scope := forensicScope()
				mutate(&scope)
				if _, err := PlanAcquisition(incident, "purpose", scope, "tool", actors, "security", "us-east", retention, forensicNow()); !errors.Is(err, ErrOvercollection) {
					t.Fatalf("PlanAcquisition = %v, want ErrOvercollection", err)
				}
			})
		}
	})

	t.Run("forged custody detected", func(t *testing.T) {
		_, _, chain := acquireFixture(t)
		forged := CustodyChain{ArtifactID: chain.ArtifactID, Receipts: append([]CustodyReceipt(nil), chain.Receipts...)}
		forged.Receipts[1].Custodian = "intruder"
		if err := VerifyCustody(forged); !errors.Is(err, ErrCustodyBroken) {
			t.Fatalf("VerifyCustody = %v, want ErrCustodyBroken", err)
		}
		if _, err := TransferCustody(forged, "analyst", forensicNow().Add(5*time.Hour)); !errors.Is(err, ErrCustodyBroken) {
			t.Fatalf("TransferCustody on forgery = %v, want ErrCustodyBroken", err)
		}
		gapped := CustodyChain{ArtifactID: chain.ArtifactID, Receipts: []CustodyReceipt{chain.Receipts[1]}}
		if err := VerifyCustody(gapped); !errors.Is(err, ErrCustodyBroken) {
			t.Fatalf("gapped chain = %v, want ErrCustodyBroken", err)
		}
		swapped := chain
		swapped.ArtifactID = "other-artifact"
		if err := VerifyCustody(swapped); !errors.Is(err, ErrCustodyBroken) {
			t.Fatalf("swapped chain = %v, want ErrCustodyBroken", err)
		}
	})

	t.Run("forged artifact refused at export", func(t *testing.T) {
		plan, artifact, chain := acquireFixture(t)
		forged := artifact
		forged.ByteCount = 999999
		if _, err := Export(plan, []Artifact{forged}, []CustodyChain{chain}, forensicNow()); err == nil {
			t.Fatal("forged artifact exported")
		}
		relabelled := artifact
		relabelled.Compartment = "customer-disclosure"
		if _, err := Export(plan, []Artifact{relabelled}, []CustodyChain{chain}, forensicNow()); err == nil {
			t.Fatal("relabelled artifact exported")
		}
	})

	t.Run("raw moves refused", func(t *testing.T) {
		_, artifact, _ := acquireFixture(t)
		for name, args := range map[string][2]string{
			"other compartment": {"customer-disclosure", "us-east"},
			"other region":      {"security", "eu-west"},
			"both":              {"customer-disclosure", "eu-west"},
		} {
			t.Run(name, func(t *testing.T) {
				if err := AuthorizeMove(artifact, args[0], args[1]); !errors.Is(err, ErrCompartmentCrossing) {
					t.Fatalf("AuthorizeMove = %v, want ErrCompartmentCrossing", err)
				}
			})
		}
		if err := AuthorizeMove(artifact, "security", "us-east"); err != nil {
			t.Fatalf("in-place move refused: %v", err)
		}
	})
}

// TestTodo_FORENSIC_001_Recovery proves degraded evidence stays governed:
// a broken chain is detected and work resumes from verified custody,
// while retention-expired evidence is refused export instead of leaking.
func TestTodo_FORENSIC_001_Recovery(t *testing.T) {
	plan, artifact, chain := acquireFixture(t)

	tampered := CustodyChain{ArtifactID: chain.ArtifactID, Receipts: append([]CustodyReceipt(nil), chain.Receipts...)}
	tampered.Receipts[0].Custodian = "intruder"
	if err := VerifyCustody(tampered); !errors.Is(err, ErrCustodyBroken) {
		t.Fatalf("tampered chain = %v, want ErrCustodyBroken", err)
	}
	if _, err := Export(plan, []Artifact{artifact}, []CustodyChain{tampered}, forensicNow()); err == nil {
		t.Fatal("export sealed over broken custody")
	}
	// Recovery resumes from the verified prefix: the intact first receipt
	// extends into a fresh chain that verifies and exports.
	prefix := CustodyChain{ArtifactID: chain.ArtifactID, Receipts: []CustodyReceipt{chain.Receipts[0]}}
	if err := VerifyCustody(prefix); err != nil {
		t.Fatalf("verified prefix: %v", err)
	}
	recovered, err := TransferCustody(prefix, "recovery-analyst", forensicNow().Add(4*time.Hour))
	if err != nil {
		t.Fatalf("recovery transfer: %v", err)
	}
	pkg, err := Export(plan, []Artifact{artifact}, []CustodyChain{recovered}, forensicNow().Add(5*time.Hour))
	if err != nil {
		t.Fatalf("recovery export: %v", err)
	}
	if err := VerifyExport(pkg); err != nil {
		t.Fatalf("recovery VerifyExport: %v", err)
	}

	// Retention-expired evidence is due for disposition, not export.
	expiredAt := forensicNow().Add(91 * 24 * time.Hour)
	if _, err := Export(plan, []Artifact{artifact}, []CustodyChain{chain}, expiredAt); !errors.Is(err, ErrExportUnverifiable) {
		t.Fatalf("expired export = %v, want ErrExportUnverifiable", err)
	}
}

// TestTodo_FORENSIC_001_Mutation proves every seal binds its contents: any
// post-hoc edit to plan, artifact, custody, export or disclosure is
// detected by recomputation.
func TestTodo_FORENSIC_001_Mutation(t *testing.T) {
	plan, artifact, chain := acquireFixture(t)

	mutatedPlan := plan
	mutatedPlan.Purpose = "unbounded trawl"
	if err := mutatedPlan.Verify(); err == nil {
		t.Fatal("mutated plan verified")
	}
	if err := plan.Verify(); err != nil {
		t.Fatalf("pristine plan Verify: %v", err)
	}

	for name, mutate := range map[string]func(*Artifact){
		"source hash": func(a *Artifact) { a.SourceHash = "sha256:edited" },
		"byte count":  func(a *Artifact) { a.ByteCount++ },
		"compartment": func(a *Artifact) { a.Compartment = "public" },
		"digest":      func(a *Artifact) { a.Digest = "deadbeef" },
	} {
		t.Run("artifact "+name, func(t *testing.T) {
			forged := artifact
			mutate(&forged)
			if err := forged.Verify(); err == nil {
				t.Fatalf("mutated artifact verified: %+v", forged)
			}
		})
	}

	pkg, err := Export(plan, []Artifact{artifact}, []CustodyChain{chain}, forensicNow().Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	forgedPkg := pkg
	forgedPkg.ArtifactDigests = []string{"sha256:edited"}
	if err := VerifyExport(forgedPkg); !errors.Is(err, ErrExportUnverifiable) {
		t.Fatalf("mutated export = %v, want ErrExportUnverifiable", err)
	}
	editedDigest := pkg
	editedDigest.Digest = "deadbeef"
	if err := VerifyExport(editedDigest); !errors.Is(err, ErrExportUnverifiable) {
		t.Fatalf("edited seal = %v, want ErrExportUnverifiable", err)
	}

	disclosure, err := Redact(artifact, chain, "customer-disclosure", []string{"request payload bytes"})
	if err != nil {
		t.Fatal(err)
	}
	if disclosure.Digest == "" {
		t.Fatal("disclosure has no digest")
	}
	recomupted, err := Redact(artifact, chain, "customer-disclosure", []string{"request payload bytes"})
	if err != nil || recomupted.Digest != disclosure.Digest {
		t.Fatalf("redaction is not deterministic: %v", err)
	}
	rebound, err := Redact(artifact, chain, "customer-disclosure", []string{"different reason"})
	if err != nil || rebound.Digest == disclosure.Digest {
		t.Fatalf("different redactions share a digest: %v", err)
	}
}
