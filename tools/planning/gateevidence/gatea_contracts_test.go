package gateevidence

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

func gateTopology() DownstreamTopology {
	return DownstreamTopology{
		SchemaVersion: 1, SourceSystem: "synthetic-incumbent", SourceAuthority: "customer-hr-authority",
		DownstreamSystem: "synthetic-payroll-observer", DownstreamAuthority: "independent-operations-owner",
		HandoffRef: "handoff:promotion-observation:v1", AcknowledgementMechanism: "signed-receipt",
		ObservationMechanism: "read-only-reconciliation", AccountableOwner: "placeholder-customer-operations",
	}
}

func gateFields(topology DownstreamTopology) FieldManifest {
	return FieldManifest{
		SchemaVersion: 1, ManifestID: "field-manifest:promotion:v1", TopologyDigest: topology.Digest(), GateAReadOnly: true,
		Fields: []FieldSpec{
			{Path: "job.code", Authority: "incumbent", Classification: "CONFIDENTIAL", Purpose: "promotion simulation", Source: topology.SourceSystem, EffectiveTime: "effective_at", PhaseDepth: "GATE_A", Access: "READ"},
			{Path: "manager.ref", Authority: "incumbent", Classification: "CONFIDENTIAL", Purpose: "promotion simulation", Source: topology.SourceSystem, EffectiveTime: "effective_at", PhaseDepth: "GATE_A", Access: "READ"},
			{Path: "organization.ref", Authority: "incumbent", Classification: "CONFIDENTIAL", Purpose: "promotion simulation", Source: topology.SourceSystem, EffectiveTime: "effective_at", PhaseDepth: "GATE_A", Access: "READ"},
			{Path: "position.ref", Authority: "incumbent", Classification: "CONFIDENTIAL", Purpose: "promotion simulation", Source: topology.SourceSystem, EffectiveTime: "effective_at", PhaseDepth: "GATE_A", Access: "READ"},
			{Path: "compensation.base_pay", Authority: "incumbent", Classification: "RESTRICTED", Purpose: "promotion simulation", Source: topology.SourceSystem, EffectiveTime: "effective_at", PhaseDepth: "GATE_A", Access: "READ"},
		},
	}
}

func gateApproval(topology DownstreamTopology, fields FieldManifest) DataProcessingApproval {
	treatments := make([]DataTreatment, 0, len(fields.Fields))
	for _, field := range fields.Fields {
		treatments = append(treatments, DataTreatment{
			System: field.Source, FieldPath: field.Path, Region: "PLACEHOLDER_REGION",
			Purpose: field.Purpose, Processor: "PLACEHOLDER_PROCESSOR", Retention: "PLACEHOLDER_RETENTION",
			Classification: field.Classification,
		})
	}
	return DataProcessingApproval{
		SchemaVersion: 1, ApprovalID: "approval:placeholder:v1", Decision: "APPROVED",
		TopologyDigest: topology.Digest(), FieldManifestDigest: fields.Digest(), Treatments: treatments,
		ApprovedBy: "PLACEHOLDER_SECURITY_PRIVACY_APPROVER", ApprovedAt: "2026-09-06", ExpiresAt: "2026-12-06",
	}
}

func concretePartner() PartnerManifest {
	topology := gateTopology()
	fields := gateFields(topology)
	return PartnerManifest{
		SchemaVersion: 1, ManifestID: "partner-manifest:synthetic:v1", ProblemClass: "promotion cross-system visibility",
		Incumbent: IncumbentAssessment{System: topology.SourceSystem, Edition: "synthetic-enterprise", Version: "2026.1", License: "licensed-fixture", ConfiguredWorkflow: "promotion", NativeCoverage: "partial", Gap: "cross-system observation", EvidenceDate: "2026-09-06"},
		Topology:  topology, Fields: fields,
		NativeCapability: IncumbentAssessment{System: topology.SourceSystem, Edition: "synthetic-enterprise", Version: "2026.1", License: "licensed-fixture", ConfiguredWorkflow: "promotion", NativeCoverage: "partial", Gap: "cross-system observation", EvidenceDate: "2026-09-06"},
		Commercial:       CommercialTerms{PriceMinor: 10000, Currency: "USD", BillingBasis: "monthly pilot", EligibleVolume: 10, AdoptionDenominator: 10, BypassSources: "synthetic-manual-bypass", CustomerLaborByRole: "synthetic-hr-operations", CostToServeMinor: 2500},
		StopCriteria:     []StopCriterion{{Metric: "visibility_improvement", Operator: "<", Threshold: 0, Unit: "ratio"}},
		SignedBy:         "synthetic-reviewer", SignedAt: "2026-09-06", SignatureDigest: "sha256:synthetic-partner-signature",
	}
}

func fullFixtureSet() FixtureSet {
	kinds := RequiredFixtureKinds
	fixtures := make([]PartnerFixture, 0, len(kinds))
	for i, kind := range kinds {
		fixtures = append(fixtures, PartnerFixture{
			ID: "fixture-" + strings.ToLower(string(kind)), Kind: kind, Provenance: "synthetic://promotion/v1",
			ExpectedResultDigest: "sha256:expected-" + strings.ToLower(string(kind)), Synthetic: true,
			Facts: []FixtureFact{{Field: "worker.ref", Value: "synthetic-worker-" + string(rune('a'+i)), Redacted: true}},
		})
	}
	return FixtureSet{SchemaVersion: 1, SetID: "fixtures:promotion:v1", Provenance: "synthetic://promotion/v1", ManifestDigest: "sha256:placeholder-manifest", Fixtures: fixtures}
}

func paidEvent(simulationDigest string, class PaidUseActorClass) PaidUseEvent {
	return PaidUseEvent{
		SchemaVersion: 1, EventID: "paid-use-event-1", TenantRefHash: strings.Repeat("a", 64), ActorRefHash: strings.Repeat("b", 64),
		ActorClass: class, CustomerAuthorized: true, Licensed: true, EligibleTransaction: true,
		TransactionRef: "transaction:synthetic-1", WorkflowStage: "SIMULATION_COMPLETED", OutcomeRef: "outcome:synthetic-1",
		ObservedAt: "2026-09-06", SimulationDigest: simulationDigest,
	}
}

func signedP1AEvidence(t *testing.T) (P1AManifest, []P1AEvidenceRequirement, []P1AEvidenceResult, time.Time) {
	t.Helper()
	manifest := baseManifest()
	manifest.Evidence = []EvidenceEntry{{TodoID: "WEDGE-012", Test: "TestPromotionStoryRejectsHiddenEffect", Package: "./tools/planning/gateevidence"}}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := manifest.CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := SignDigest(private, digest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Signature = &Signature{Algorithm: "ed25519", PublicKey: hexPublic(private), Value: signature}
	requirement := P1AEvidenceRequirement{
		TodoID: "WEDGE-012", Test: "TestPromotionStoryRejectsHiddenEffect", Package: "./tools/planning/gateevidence", Owner: "tools/planning/gateevidence",
		Command: "go test -count=1 ./tools/planning/gateevidence/", FixtureArtifact: "tools/planning/gateevidence/gatea_contracts_test.go",
		RestoreArtifact: "testdata/restore-rehearsal.txt", SLOArtifact: "testdata/slo.txt", AccessibilityArtifact: "testdata/accessibility.txt",
		PrivacyArtifact: "testdata/privacy.txt", RetentionDays: 30, ExpiresAt: "2026-12-06",
	}
	result := P1AEvidenceResult{
		TodoID: requirement.TodoID, Test: requirement.Test, Package: requirement.Package, ManifestDigest: digest, Status: "PASS", ResultDigest: "sha256:result",
		Timestamp: "2026-09-06", Owner: requirement.Owner, Command: requirement.Command, FixtureArtifact: requirement.FixtureArtifact,
		RestoreArtifact: requirement.RestoreArtifact, SLOArtifact: requirement.SLOArtifact, AccessibilityArtifact: requirement.AccessibilityArtifact,
		PrivacyArtifact: requirement.PrivacyArtifact, RetentionDays: requirement.RetentionDays, ExpiresAt: requirement.ExpiresAt, RestoreVerified: true,
	}
	return manifest, []P1AEvidenceRequirement{requirement}, []P1AEvidenceResult{result}, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
}

func hexPublic(private ed25519.PrivateKey) string {
	public := private.Public().(ed25519.PublicKey)
	const hex = "0123456789abcdef"
	out := make([]byte, len(public)*2)
	for i, b := range public {
		out[i*2], out[i*2+1] = hex[b>>4], hex[b&15]
	}
	return string(out)
}

func TestPilotApprovalRejectsUnclassifiedFlow(t *testing.T) {
	topology := gateTopology()
	fields := gateFields(topology)
	approval := gateApproval(topology, fields)
	if got := ValidateDataProcessingApproval(approval, topology, fields, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)); len(got) != 0 {
		t.Fatalf("complete approval rejected: %v", got)
	}
	approval.Treatments = approval.Treatments[:len(approval.Treatments)-1]
	violations := ValidateDataProcessingApproval(approval, topology, fields, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC))
	if !hasContractCode(violations, "UNCLASSIFIED_FLOW") {
		t.Fatalf("missing field treatment was accepted: %v", violations)
	}
	approval.FieldManifestDigest = "wrong"
	if !hasContractCode(ValidateDataProcessingApproval(approval, topology, fields, time.Now()), "APPROVAL_FIELD_MANIFEST_MISMATCH") {
		t.Fatal("field-manifest digest mismatch was accepted")
	}
}

func TestPartnerManifestRejectsUnboundedWedge(t *testing.T) {
	placeholderManifest := PlaceholderPartnerManifest()
	if got := QualifyPartner(placeholderManifest); got.Status != PartnerUndecided {
		t.Fatalf("placeholder qualification = %+v, want UNDECIDED", got)
	}
	qualified := QualifyPartner(concretePartner())
	if qualified.Status != PartnerQualified {
		t.Fatalf("complete synthetic partner qualification = %+v, want QUALIFIED", qualified)
	}
	partner := concretePartner()
	partner.StopCriteria = nil
	if got := QualifyPartner(partner); got.Status != PartnerReselect {
		t.Fatalf("unbounded partner qualification = %+v, want RESELECT", got)
	}
}

func TestTodo_WEDGE_001_Golden(t *testing.T) {
	manifest := PlaceholderPartnerManifest()
	firstManifestDigest, secondManifestDigest := manifest.Digest(), manifest.Digest()
	if len(firstManifestDigest) != 64 || firstManifestDigest != secondManifestDigest {
		t.Fatal("partner manifest digest is not deterministic")
	}
}

func TestPilotFieldManifestRejectsImplicitField(t *testing.T) {
	topology := gateTopology()
	fields := gateFields(topology)
	if got := ValidateFieldManifest(fields, topology); len(got) != 0 {
		t.Fatalf("complete field manifest rejected: %v", got)
	}
	fields.Fields[0].Classification = ""
	if !hasContractCode(ValidateFieldManifest(fields, topology), "FIELD_UNCLASSIFIED") {
		t.Fatal("unclassified implicit field was accepted")
	}
}

func TestTodo_WEDGE_004_Golden(t *testing.T) {
	fields := gateFields(gateTopology())
	if got := fields.Digest(); len(got) != 64 || got != fields.Digest() {
		t.Fatal("field manifest digest is not deterministic")
	}
}

func TestTodo_WEDGE_004_Security(t *testing.T) {
	topology := gateTopology()
	fields := gateFields(topology)
	fields.Fields[0].Access = "WRITE"
	if !hasContractCode(ValidateFieldManifest(fields, topology), "FIELD_WRITE_AUTHORITY") {
		t.Fatal("Gate A write field was accepted")
	}
}

func TestTopologyRejectsSingleSystemCrossSystemClaim(t *testing.T) {
	topology := gateTopology()
	topology.DownstreamSystem, topology.DownstreamAuthority = topology.SourceSystem, topology.SourceAuthority
	if !hasContractCode(ValidateTopology(topology), "SINGLE_SYSTEM_CROSS_SYSTEM_CLAIM") {
		t.Fatal("same-authority topology was accepted as independent")
	}
	topology.NarrowedSingleSystemClaim = true
	if hasContractCode(ValidateTopology(topology), "SINGLE_SYSTEM_CROSS_SYSTEM_CLAIM") {
		t.Fatal("explicit narrowed single-system claim was rejected as cross-system")
	}
}

func TestTodo_WEDGE_005(t *testing.T) {
	if got := ValidateTopology(gateTopology()); len(got) != 0 {
		t.Fatalf("complete topology rejected: %v", got)
	}
}

func TestTodo_WEDGE_010_Golden(t *testing.T) {
	v := ContractViolation{Code: "UNCLASSIFIED_FLOW", Field: "treatments[0].region", Detail: "missing approved treatment value"}
	if got, want := v.String(), "UNCLASSIFIED_FLOW: treatments[0].region: missing approved treatment value"; got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
}

func TestTodo_WEDGE_010_Security(t *testing.T) {
	topology := gateTopology()
	fields := gateFields(topology)
	approval := gateApproval(topology, fields)
	approval.Treatments[0].Classification = "PUBLIC"
	if !hasContractCode(ValidateDataProcessingApproval(approval, topology, fields, time.Now()), "UNBOUND_FLOW") {
		t.Fatal("classification downgrade escaped approval binding")
	}
}

func TestFixtureCoverageRejectsMissingBoundaryCase(t *testing.T) {
	fixtures := fullFixtureSet()
	coverage := EvaluateFixtureCoverage(fixtures)
	if len(coverage.Missing) != 0 || len(coverage.Present) != len(RequiredFixtureKinds) {
		t.Fatalf("complete fixture set coverage = %+v", coverage)
	}
	fixtures.Fixtures = fixtures.Fixtures[:len(fixtures.Fixtures)-1]
	coverage = EvaluateFixtureCoverage(fixtures)
	if len(coverage.Missing) != 1 || coverage.Missing[0] != FixturePartialObservation {
		t.Fatalf("missing boundary case = %+v", coverage)
	}
	fixtures.Fixtures[0].ContainsPII = true
	if !hasContractCode(ValidateFixtureSet(fixtures), "FIXTURE_PII_RISK") {
		t.Fatal("PII-bearing fixture was accepted")
	}
}

func TestTodo_WEDGE_011_Golden(t *testing.T) {
	coverage := EvaluateFixtureCoverage(fullFixtureSet())
	if got := coverage.SetDigest; len(got) != 64 {
		t.Fatalf("fixture digest length = %d, want 64", len(got))
	}
	if coverage.SetDigest != EvaluateFixtureCoverage(fullFixtureSet()).SetDigest {
		t.Fatal("fixture coverage digest is not deterministic")
	}
}

func TestTodo_WEDGE_011_Race(t *testing.T) {
	const workers = 16
	type result struct {
		coverage FixtureCoverage
	}
	results := make(chan result, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- result{coverage: EvaluateFixtureCoverage(fullFixtureSet())}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var want FixtureCoverage
	for i := 0; i < workers; i++ {
		got := (<-results).coverage
		if len(got.Missing) != 0 || len(got.Present) != len(RequiredFixtureKinds) || len(got.SetDigest) != 64 {
			t.Fatalf("worker %d coverage = %+v, want complete fixture coverage with a SHA-256 digest", i, got)
		}
		if i == 0 {
			want = got
			continue
		}
		if got.SetDigest != want.SetDigest || strings.Join(stringKinds(got.Present), ",") != strings.Join(stringKinds(want.Present), ",") {
			t.Fatalf("worker %d coverage differs from first result: got %+v, want %+v", i, got, want)
		}
	}
}

func TestTodo_WEDGE_011_Security(t *testing.T) {
	fixtures := fullFixtureSet()
	fixtures.Fixtures[0].Synthetic = false
	if !hasContractCode(ValidateFixtureSet(fixtures), "FIXTURE_PII_RISK") {
		t.Fatal("non-synthetic fixture was accepted")
	}
}

func TestPromotionStoryRejectsHiddenEffect(t *testing.T) {
	contract := PromotionSimulationFixture()
	if got := ValidateWorkflowSimulation(contract); len(got) != 0 {
		t.Fatalf("complete simulation rejected: %v", got)
	}
	contract.SideEffects = nil
	if !hasContractCode(ValidateWorkflowSimulation(contract), "SIMULATION_SECTION_MISSING") {
		t.Fatal("omitted side effects were accepted")
	}
	contract = PromotionSimulationFixture()
	contract.ProviderCalls = 1
	if !hasContractCode(ValidateWorkflowSimulation(contract), "HIDDEN_EFFECT") {
		t.Fatal("provider effect was accepted")
	}
}

func TestTodo_WEDGE_012_Golden(t *testing.T) {
	contract := PromotionSimulationFixture()
	first, second := contract.Digest(), contract.Digest()
	if first == "" || first != second || len(first) != 64 {
		t.Fatalf("simulation digest = %q, repeated = %q", first, second)
	}
}

func TestPaidUseRejectsDemoOrInternalActor(t *testing.T) {
	simulationDigest := PromotionSimulationFixture().Digest()
	good := paidEvent(simulationDigest, PaidUseCustomerActor)
	if got := ValidatePaidUseEvent(good); len(got) != 0 {
		t.Fatalf("customer event rejected: %v", got)
	}
	for _, class := range []PaidUseActorClass{PaidUseDemoActor, PaidUseInternalActor} {
		event := paidEvent(simulationDigest, class)
		if !hasContractCode(ValidatePaidUseEvent(event), "INELIGIBLE_ACTOR") {
			t.Errorf("actor class %s counted as paid use", class)
		}
	}
	unlicensed := good
	unlicensed.Licensed = false
	if !hasContractCode(ValidatePaidUseEvent(unlicensed), "INELIGIBLE_ACTOR") {
		t.Fatal("unlicensed actor counted as paid use")
	}
}

func TestTodo_WEDGE_013_Integration(t *testing.T) {
	good := paidEvent(PromotionSimulationFixture().Digest(), PaidUseCustomerActor)
	evidence := PaidUseEvidence{SchemaVersion: 1, EvidenceID: "paid-use:synthetic:v1", SimulationDigest: good.SimulationDigest, Events: []PaidUseEvent{good}}
	if got := len(evidence.EligibleEvents()); got != 1 {
		t.Fatalf("eligible paid-use events = %d, want 1", got)
	}
	if got := ValidatePaidUseEvidence(evidence); len(got) != 0 {
		t.Fatalf("paid-use evidence rejected: %v", got)
	}
}

func TestTodo_WEDGE_013_Security(t *testing.T) {
	event := paidEvent(PromotionSimulationFixture().Digest(), PaidUseCustomerActor)
	event.ActorRefHash = "raw-person-name"
	if !hasContractCode(ValidatePaidUseEvent(event), "ACTOR_DATA_NOT_MINIMIZED") {
		t.Fatal("raw actor data was accepted")
	}
}

func TestTodo_WEDGE_013_Mutation(t *testing.T) {
	event := paidEvent(PromotionSimulationFixture().Digest(), PaidUseCustomerActor)
	event.EligibleTransaction = false
	if !hasContractCode(ValidatePaidUseEvent(event), "INELIGIBLE_TRANSACTION") {
		t.Fatal("ineligible transaction was accepted")
	}
}

func TestTodo_WEDGE_009_Integration(t *testing.T) {
	plan := PilotExitPlan{SchemaVersion: 1, TenantRefHash: strings.Repeat("a", 64), CredentialRevocation: "revoke", PendingWorkDisposition: "export pending", EvidenceExport: "encrypted export", RetentionAndHolds: "retain holds", DestructionResponsibility: "custodians", ConnectorAuthority: "none after revocation"}
	result, violations := DryRunPilotExit(plan, "sha256:evidence")
	if len(violations) != 0 || result.ExportDigest == "" || result.ActiveConnectorAuthority != 0 {
		t.Fatalf("exit integration result = %+v, violations = %v", result, violations)
	}
}

func TestTodo_WEDGE_009_Security(t *testing.T) {
	plan := PilotExitPlan{SchemaVersion: 1, TenantRefHash: strings.Repeat("a", 64), CredentialRevocation: "revoke", PendingWorkDisposition: "export pending", EvidenceExport: "encrypted export", RetentionAndHolds: "retain holds", DestructionResponsibility: "custodians", ConnectorAuthority: "active"}
	if !hasContractCode(ValidatePilotExitPlan(plan), "EXIT_AUTHORITY_ACTIVE") {
		t.Fatal("active connector authority escaped exit validation")
	}
}

func TestPilotExitPlanRejectsMissingRevocationOrExport(t *testing.T) {
	plan := PilotExitPlan{SchemaVersion: 1, TenantRefHash: strings.Repeat("a", 64), CredentialRevocation: "revoke all connector leases", PendingWorkDisposition: "cancel and export pending work", EvidenceExport: "encrypted tenant export", RetentionAndHolds: "retain legal holds; expire non-held evidence", DestructionResponsibility: "customer and Human Capital Management Suite custodians", ConnectorAuthority: "none after revocation"}
	if got := ValidatePilotExitPlan(plan); len(got) != 0 {
		t.Fatalf("complete exit plan rejected: %v", got)
	}
	plan.CredentialRevocation = ""
	if !hasContractCode(ValidatePilotExitPlan(plan), "EXIT_PLAN_INCOMPLETE") {
		t.Fatal("missing credential revocation was accepted")
	}
	plan.CredentialRevocation = "revoke all connector leases"
	result, violations := DryRunPilotExit(plan, "sha256:evidence")
	if len(violations) != 0 || result.ActiveConnectorAuthority != 0 {
		t.Fatalf("exit dry run = %+v, violations = %v", result, violations)
	}
}

func TestGateADecisionBlocksMissingEvidence(t *testing.T) {
	receipt := P1AEvidenceReceipt{SchemaVersion: 1, ManifestTodoID: "NEXT-003", ManifestDigest: "sha256:manifest", AsOf: "2026-09-06", Decision: GateDecisionBlocked, Findings: []P1AEvidenceFinding{{TodoID: "WEDGE-012", Test: "TestPromotionStoryRejectsHiddenEffect", Code: "RESULT_STALE", Detail: "stale"}}}
	record := EvaluateGateADecision(receipt)
	if record.Decision != GateARemediate || len(record.MissingEvidence) != 1 || record.WriteAuthority {
		t.Fatalf("missing evidence decision = %+v", record)
	}
	clear := receipt
	clear.Decision, clear.Findings = GateDecisionClear, nil
	record = EvaluateGateADecision(clear)
	if record.Decision != GateAProceed || record.WriteAuthority {
		t.Fatalf("complete evidence decision = %+v", record)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignGateADecision(record, "PLACEHOLDER_GATE_A_SIGNER", "2026-09-06", private)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyGateADecision(signed); err != nil || !ok {
		t.Fatalf("signed Gate A decision verification = %v, %v", ok, err)
	}
}

func TestTodo_WEDGE_014_Fault(t *testing.T) {
	record := EvaluateGateADecision(P1AEvidenceReceipt{Decision: GateDecisionBlocked})
	if record.Decision != GateARemediate {
		t.Fatalf("faulted receipt decision = %s", record.Decision)
	}
	if _, err := VerifyGateADecision(record); err == nil {
		t.Fatal("unsigned decision was verified")
	}
}

func TestTodo_WEDGE_014_Mutation(t *testing.T) {
	clear := P1AEvidenceReceipt{SchemaVersion: 1, Decision: GateDecisionClear, ManifestDigest: "one"}
	first := EvaluateGateADecision(clear)
	clear.ManifestDigest = "two"
	second := EvaluateGateADecision(clear)
	if first.EvidenceDigest == second.EvidenceDigest {
		t.Fatal("decision evidence snapshot ignored receipt mutation")
	}
}

func TestP1AEvidenceCompilerRejectsMissingStaleOutOfManifestOrEffectfulEvidenceExtended(t *testing.T) {
	manifest, requirements, results, now := signedP1AEvidence(t)
	receipt := CompileP1AEvidence(manifest, requirements, results, now)
	if receipt.Decision != GateDecisionClear || len(receipt.Findings) != 0 {
		t.Fatalf("complete evidence receipt = %+v", receipt)
	}
	results[0].Timestamp = "2020-01-01"
	if got := CompileP1AEvidence(manifest, requirements, results, now); !hasEvidenceCode(got.Findings, "RESULT_STALE") {
		t.Fatal("stale evidence was accepted")
	}
	results[0].Timestamp = "2026-09-06"
	results = append(results, P1AEvidenceResult{TodoID: "OTHER", Test: "TestOther", Package: "./other", Status: "PASS"})
	if got := CompileP1AEvidence(manifest, requirements, results, now); !hasEvidenceCode(got.Findings, "OUT_OF_MANIFEST") {
		t.Fatal("out-of-manifest evidence was accepted")
	}
}

func TestTodo_NEXT_003_Property(t *testing.T) {
	manifest, requirements, results, now := signedP1AEvidence(t)
	one := CompileP1AEvidence(manifest, requirements, results, now)
	two := CompileP1AEvidence(manifest, requirements, results, now)
	if one.Digest() != two.Digest() {
		t.Fatal("evidence receipt digest is not deterministic")
	}
}

func TestTodo_NEXT_003_Golden(t *testing.T) {
	manifest, requirements, results, now := signedP1AEvidence(t)
	receipt := CompileP1AEvidence(manifest, requirements, results, now)
	if receipt.Decision != GateDecisionClear || receipt.ManifestTodoID != "NEXT-002" || len(receipt.ManifestDigest) != 64 {
		t.Fatalf("evidence golden shape = %+v", receipt)
	}
}

func TestTodo_NEXT_003_Fault(t *testing.T) {
	manifest, requirements, results, now := signedP1AEvidence(t)
	results[0].RestoreVerified = false
	receipt := CompileP1AEvidence(manifest, requirements, results, now)
	if !hasEvidenceCode(receipt.Findings, "EFFECTFUL_OR_UNRESTORED") {
		t.Fatal("unrestored evidence was accepted")
	}
}

func TestTodo_NEXT_003_Security(t *testing.T) {
	manifest, requirements, results, now := signedP1AEvidence(t)
	results[0].ProviderEffects = 1
	receipt := CompileP1AEvidence(manifest, requirements, results, now)
	if !hasEvidenceCode(receipt.Findings, "EFFECTFUL_OR_UNRESTORED") {
		t.Fatal("provider effect was accepted")
	}
}

func TestTodo_NEXT_003_Conformance(t *testing.T) {
	manifest, requirements, results, now := signedP1AEvidence(t)
	results[0].PrivacyArtifact = "different"
	receipt := CompileP1AEvidence(manifest, requirements, results, now)
	if !hasEvidenceCode(receipt.Findings, "RESULT_METADATA_MISMATCH") {
		t.Fatal("privacy metadata mismatch was accepted")
	}
}

func TestTodo_NEXT_003_Recovery(t *testing.T) {
	manifest, requirements, results, now := signedP1AEvidence(t)
	results[0].RestoreArtifact = ""
	receipt := CompileP1AEvidence(manifest, requirements, results, now)
	if !hasEvidenceCode(receipt.Findings, "RESULT_METADATA_MISMATCH") {
		t.Fatal("missing restore evidence was accepted")
	}
}

func TestTodo_NEXT_003_Mutation(t *testing.T) {
	manifest, requirements, results, now := signedP1AEvidence(t)
	first := CompileP1AEvidence(manifest, requirements, results, now)
	manifest.Evidence[0].Test = "TestOther"
	second := CompileP1AEvidence(manifest, requirements, results, now)
	if bytes.Equal([]byte(first.Digest()), []byte(second.Digest())) {
		t.Fatal("manifest evidence mutation did not change receipt")
	}
}

func hasContractCode(violations []ContractViolation, code string) bool {
	for _, v := range violations {
		if v.Code == code {
			return true
		}
	}
	return false
}

func hasEvidenceCode(findings []P1AEvidenceFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func stringKinds(kinds []FixtureKind) []string {
	out := make([]string, len(kinds))
	for i, kind := range kinds {
		out[i] = string(kind)
	}
	return out
}
