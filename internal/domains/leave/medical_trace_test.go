package leave

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	wiredigest "github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

// LEAVE-015 RED: the canonical Medical Leave trace proof does not exist
// yet. Everything below must fail to compile until medical_trace.go lands.

func traceSnapshotEntries() []InputEntry {
	inputs := []string{
		"worker", "employment", "assignment", "location", "manager",
		"schedule", "calendar", "balances", "benefits",
		"payroll-context", "legal-context", "evidence-usability",
		"source-authority", "freshness", "effective-time", "known-time",
	}
	entries := make([]InputEntry, 0, len(inputs))
	for _, in := range inputs {
		entries = append(entries, InputEntry{
			Input: in, Revision: "rev:" + in + ":1", Watermark: "tick:100",
			Status: InputReady, EvidenceRef: "medical-compartment:" + in,
			Authority: "hris", Effective: "2026-10-01", Known: "2026-09-16",
		})
	}
	return entries
}

func traceRequest(t *testing.T) RequestLeave {
	t.Helper()
	start, err := values.ParseLocalDate("2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2026-10-08")
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "business", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	return RequestLeave{WorkerID: "worker:abc-123", LeaveType: "medical", Interval: iv,
		Mode: ModeContinuous, Reason: "planned absence", ExpectedWorkerRevision: "rev:worker:v1",
		ClientRequestID: "client-1234", EvidenceRefs: []string{"evidence:request-1"}}
}

func traceFixture(t *testing.T) TraceInput {
	t.Helper()
	return TraceInput{
		Request:         traceRequest(t),
		Context:         TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"},
		SnapshotEntries: traceSnapshotEntries(),
		Queries: []ProgramQuery{
			{ProgramID: "fmla", Authority: AuthorityStatutory, Release: "fmla-2026.1", Rule: tenureRule{}, RequireFacts: []string{"service.months"}, Obligations: []string{"protect:job"}},
		},
		Facts:            map[string]string{"service.months": "18"},
		PlanStartDay:     10,
		PlanEndDay:       17,
		PlanHoursPerDay:  8,
		PlanBalance:      72,
		PlanTimezone:     "America/Los_Angeles",
		ProposalRevision: 1,
		LoopID:           "loop:w1", LoopTask: "task:review:w1", LoopPolicy: "policy:review:v1",
		Finding: workitem.Finding{TaskID: "task:review:w1", Verdict: workitem.ReviewMoreInfo, RequirementID: "req:certification", Reason: "certification needed"},
		Resume:  EvidenceRef{Ref: "medical-compartment:certification", Quarantined: true, Classified: true, AuthorityCurrent: true},
		Determination: DeterminationInput{
			LegalRelease: "legal-2026.1", Interval: "2026-10-01/2026-10-08",
			PaidHours: 64, UnpaidHours: 0, Rights: []string{"job-protection"},
			Obligations: []string{"recertify"}, Explanation: "FMLA covers 8 continuous days",
			TemplateID: "tmpl:determination", TemplateVersion: "v4", TemplateApproved: true,
			Locale: "en-US", Recipient: "worker:abc-123",
		},
		StartCommitKey: "commit:leave:w1:start",
		Notice: delivery.NoticeRequirement{
			ID: "notice:leave:w1", Recipient: "worker:abc-123", RecipientVerified: true,
			RecipientProof: "proof:recipient", ContentDigest: "tmpl:determination@v4",
			ContentVersion: "v4", Timestamp: 100, JurisdictionRule: "hypothetical-fmla",
			AckProof: "ack:worker", SignatureDigest: "sig:admin",
		},
		NoticeRules: map[string]bool{"hypothetical-fmla": true},
		NoticeAck:   true,
		LeaveStart: LeaveStartInput{
			Proposal: intent.ProposalRevision{
				ProposalRevisionID: "rev:leave:w1:1", IntentID: "intent:leave:w1",
				MaterialDigest: wiredigest.Reference{Digest: "material:leave:w1"}, Tenant: values.TenantId("acme"),
			},
			Definition: intent.Definition{
				Ref:          intent.Ref{TypeID: "hcmnext.leave.start", Version: 1},
				AllowedModes: []intent.Mode{intent.ModeSimulate},
			},
			EmploymentStatus: "ACTIVE", BalanceHead: 7, AvailabilityHead: 3,
			Governance: intent.GovernanceSnapshot{
				SnapshotDigest: "governance-1", AuthZDecision: "PERMIT",
				LegalDecision: "PERMIT", PolicyDecision: "PERMIT", RiskDecision: "ACCEPT",
			},
			Conflict: intent.ConflictSnapshot{
				SnapshotDigest: "conflict-1", FenceToken: "fence-1",
				FootprintRef: "leave_affected_fields_and_effective_interval/v1",
			},
			ExpiresAt: values.NewInstant(time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)),
			IDs:       leaveStartIDs(),
		},
		StartReceipts: map[string]string{
			"leave-revision": "receipt:rev", "absence-relationship": "receipt:rel",
			"availability-interval": "receipt:avail", "balance-entries": "receipt:bal",
			"ledger-events": "receipt:ledger", "projections": "receipt:proj", "effect-outbox": "receipt:outbox",
		},
		StartEffects: []ExternalEffect{
			{System: "payroll", Mandatory: true, FreshnessTick: 100, DeadlineTick: 200, State: EffectPass, Observation: "obs:payroll-1", Owner: "payroll-ops"},
			{System: "benefits", Mandatory: true, FreshnessTick: 100, DeadlineTick: 200, State: EffectUnknown, Owner: "benefits-ops"},
			{System: "wfm", Mandatory: false, FreshnessTick: 100, DeadlineTick: 200, State: EffectPass, Observation: "obs:wfm-1", Owner: "wfm-ops"},
		},
		Funding:           []FundingChange{{SourceID: "bucket:pto", OldAvailable: 72, NewAvailable: 56, NewRevision: "rev-2"}},
		RetainedDecisions: []string{"protect:job"},
		ReplanPolicy:      "retain-unaffected",
		Successor: SuccessorIntent{
			Kind: SuccessorExtend, NewStart: 10, NewEnd: 19,
			ConflictRule: "earliest-wins", ConflictWinner: "extension",
		},
		Readiness: ReturnToWorkInput{
			LeaveRevision: "rev:leave:7", EvidenceRevision: "rev:evidence:7",
			JobRevision: "rev:job:3", ScheduleRevision: "rev:schedule:3", AccessRevision: "rev:access:2",
			Clearance: ClearanceValid, RestrictionState: RestrictionConditional,
			Restrictions:   []StructuredRestriction{{ID: "r:lift", Code: "LIFT-10KG", Source: "MEDICAL"}},
			JobRequirement: JobAvailable, Schedule: ScheduleConfirmed,
			Access: AccessRestored, Qualification: QualificationQualified,
		},
		Return: ReturnCommitInput{
			IdempotencyKey:   "commit:leave:w1:return",
			EmploymentState:  "ACTIVE",
			Restrictions:     []StructuredRestriction{{ID: "r:lift", Code: "LIFT-10KG", Source: "MEDICAL"}},
			QueuedEffects:    []string{"payroll", "benefits", "schedule", "access"},
			ObligationPolicy: "policy:v3",
			StepReceipts: map[string]string{
				"leave-ended-revision": "receipt:ended", "availability-restoration": "receipt:avail",
				"restriction-carryover": "receipt:restr", "ledger-events": "receipt:ledger",
				"projections": "receipt:proj", "effect-outbox": "receipt:outbox",
				"obligation-policy": "receipt:policy",
			},
			ReturnEffects: []ExternalEffect{
				{System: "payroll", Mandatory: true, FreshnessTick: 300, DeadlineTick: 400, State: EffectPass, Observation: "obs:payroll-r1", Owner: "payroll-ops"},
				{System: "benefits", Mandatory: true, FreshnessTick: 300, DeadlineTick: 400, State: EffectPass, Observation: "obs:benefits-r1", Owner: "benefits-ops"},
				{System: "schedule", Mandatory: true, FreshnessTick: 300, DeadlineTick: 400, State: EffectPass, Observation: "obs:schedule-r1", Owner: "wfm-ops"},
				{System: "access", Mandatory: false, FreshnessTick: 300, DeadlineTick: 400, State: EffectPass, Observation: "obs:access-r1", Owner: "iam-ops"},
			},
		},
		ReceiptTenant: "tenant-a", ReceiptIntent: "intent:leave:w1", ReceiptLineage: "lineage:leave:w1",
		ReceiptAuthority: []string{"hris", "legal-plane"},
	}
}

func TestTodo_LEAVE_015(t *testing.T) {
	trace, err := TraceMedicalLeave(traceFixture(t), nil)
	if err != nil {
		t.Fatalf("TraceMedicalLeave: %v", err)
	}
	want := []string{
		"anchor", "snapshot", "eligibility", "plan", "simulation", "proposal",
		"review", "determination", "compile-start", "commit-start", "extension",
		"reconcile-start", "readiness", "return", "receipt",
	}
	if strings.Join(trace.StageNames(), ",") != strings.Join(want, ",") {
		t.Fatalf("stages=%v", trace.StageNames())
	}
	if trace.StartCommit.BusinessState != BusinessLeaveActive {
		t.Fatalf("start=%+v", trace.StartCommit)
	}
	if trace.Return.BusinessState != BusinessLeaveEnded {
		t.Fatalf("return=%+v", trace.Return)
	}
	// Degraded benefits observation is recovered, never trusted: the
	// trace keeps valid leave with a PENDING obligation and a repair.
	if trace.StartDimensions.ObligationState != "PENDING" {
		t.Fatalf("dimensions=%+v", trace.StartDimensions)
	}
	if err := trace.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: raw medical bytes outside a typed compartment never enter.
	raw := traceFixture(t)
	raw.SnapshotEntries[0].EvidenceRef = "raw-medical-bytes-no-compartment"
	if _, err := TraceMedicalLeave(raw, nil); err == nil {
		t.Fatal("raw medical bytes entered the trace")
	}
	// RED: overlapping leave refuses at compile-start.
	overlap := traceFixture(t)
	overlap.LeaveStart.OverlappingLeaves = []string{"leave:other"}
	if _, err := TraceMedicalLeave(overlap, nil); err == nil {
		t.Fatal("overlapping leave compiled")
	}
	// RED: terminated employment refuses at commit-start.
	terminated := traceFixture(t)
	terminated.LeaveStart.EmploymentStatus = "TERMINATED"
	if _, err := TraceMedicalLeave(terminated, nil); err == nil {
		t.Fatal("terminated employment committed a start")
	}
	// RED: return before readiness refuses at the return stage.
	unready := traceFixture(t)
	unready.Readiness.Clearance = ClearanceMissing
	if _, err := TraceMedicalLeave(unready, nil); err == nil {
		t.Fatal("return before readiness traced")
	}
	// Revisions advance instead of overwriting: a new proposal revision
	// seals a different trace.
	advanced := traceFixture(t)
	advanced.ProposalRevision = 2
	second, err := TraceMedicalLeave(advanced, nil)
	if err != nil {
		t.Fatalf("advanced revision refused: %v", err)
	}
	if second.Digest == trace.Digest {
		t.Fatal("revision overwrite kept the trace seal")
	}
}

func TestTodo_LEAVE_015_Property(t *testing.T) {
	first, err := TraceMedicalLeave(traceFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := TraceMedicalLeave(traceFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("trace is not deterministic")
	}
	for i := range first.Stages {
		if first.Stages[i].Digest != second.Stages[i].Digest {
			t.Fatalf("stage %s is not deterministic", first.Stages[i].Name)
		}
	}
}

func TestTodo_LEAVE_015_Golden(t *testing.T) {
	trace, err := TraceMedicalLeave(traceFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{"stages=" + strings.Join(trace.StageNames(), ",")}
	for _, stage := range trace.Stages {
		lines = append(lines, stage.Name+"="+stage.Digest)
	}
	lines = append(lines, "digest="+trace.Digest)
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "leave015_trace.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_LEAVE_015_Race(t *testing.T) {
	const racers = 4
	digests := make([]string, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			trace, err := TraceMedicalLeave(traceFixture(t), nil)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = trace.Digest
		}(i)
	}
	wg.Wait()
	for i := 0; i < racers; i++ {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if digests[i] != digests[0] {
			t.Fatalf("racer %d diverged", i)
		}
	}
}

func TestTodo_LEAVE_015_Fault(t *testing.T) {
	// A fault at commit-start aborts the trace with the stage named and
	// commits nothing: the clean retry traces exactly once.
	if _, err := TraceMedicalLeave(traceFixture(t), func(stage string) error {
		if stage == "commit-start" {
			return errTraceFault
		}
		return nil
	}); err == nil {
		t.Fatal("faulted trace completed")
	} else if !strings.Contains(err.Error(), "commit-start") {
		t.Fatalf("fault names no stage: %v", err)
	}
	if _, err := TraceMedicalLeave(traceFixture(t), nil); err != nil {
		t.Fatalf("clean retry refused: %v", err)
	}
}

func TestTodo_LEAVE_015_Security(t *testing.T) {
	trace, err := TraceMedicalLeave(traceFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Medical evidence never reaches the worker-facing or manager copies.
	for _, body := range []string{trace.Determination.Artifact, trace.Determination.ManagerCopy} {
		if strings.Contains(body, "diagnosis") || strings.Contains(body, "medical-compartment") {
			t.Fatalf("medical detail leaked: %q", body)
		}
	}
	// Free-text restrictions never mutate the return path.
	leaking := traceFixture(t)
	leaking.Readiness.FreeTextRestriction = "bad back, take it easy"
	if _, err := TraceMedicalLeave(leaking, nil); err == nil {
		t.Fatal("free-text restriction traced")
	}
}

func TestTodo_LEAVE_015_Conformance(t *testing.T) {
	trace, err := TraceMedicalLeave(traceFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Every stage seal re-verifies from the trace alone: hypothetical
	// rules and public capabilities only, no jurisdiction certified.
	checks := []error{
		trace.Anchor.Verify(), trace.Snapshot.Verify(), trace.Resolution.Verify(),
		trace.Plan.Verify(), trace.Simulation.Verify(), trace.Proposal.Verify(),
		trace.Loop.Verify(), trace.Replan.Verify(), trace.Successor.Verify(),
		trace.StartDimensions.Verify(), trace.Readiness.Verify(),
		trace.Return.Verify(trace.ReturnInput), trace.Receipt.Verify(),
	}
	for _, err := range checks {
		if err != nil {
			t.Fatalf("stage seal broken: %v", err)
		}
	}
	if err := trace.StartCommit.Verify("commit:leave:w1:start", trace.StartReceiptsEcho); err != nil {
		t.Fatalf("start commit seal broken: %v", err)
	}
	if !trace.Determination.LeaveStartAllowed {
		t.Fatal("conforming determination blocks leave start")
	}
}
