package scenario

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// rev040IDs mints deterministic identifiers. Real plans use UUIDv7; the
// bridge tests need reproducible inputs, which is why the id source is a
// parameter rather than a package-level call.
func rev040IDs(prefix string) intent.IDSource {
	n := 0
	return func() (string, error) {
		n++
		return fmt.Sprintf("%s-%08d-0000-7000-8000-000000000000", prefix, n), nil
	}
}

func rev040Clock() intent.Clock {
	at := values.NewInstant(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	return func() values.Instant { return at }
}

func rev040ResourceKey(t *testing.T, segments ...string) values.ResourceKey {
	t.Helper()
	k, err := values.NewResourceKey(values.TenantId("acme-eu"), values.Kind("assignment"), segments...)
	if err != nil {
		t.Fatalf("build resource key: %v", err)
	}
	return k
}

func rev040Revision(t *testing.T, stream string, seq uint64) values.RevisionToken {
	t.Helper()
	rev, err := values.NewSequenceRevision(stream, seq)
	if err != nil {
		t.Fatalf("build revision token: %v", err)
	}
	return rev
}

func rev040Interval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	iv, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatalf("build effective interval: %v", err)
	}
	return iv
}

func rev040ControlSnapshots() intent.ControlSnapshots {
	return intent.ControlSnapshots{
		CapabilityRegistryDigest:     "cap-registry-1",
		PolicyBundleDigest:           "policy-bundle-1",
		LegalContextDigest:           "legal-1",
		EntitlementDigest:            "entitlement-1",
		ReferenceDataDigest:          "reference-1",
		ClassificationTaxonomyDigest: "taxonomy-1",
		ClassificationLabelSetDigest: "labels-1",
		DLPDecisionDigest:            "dlp-1",
	}
}

func rev040Principal() intent.PrincipalReference {
	return intent.PrincipalReference{
		PrincipalID:          "principal:hr-partner-7",
		Kind:                 intent.InitiatorHuman,
		IdentityAssuranceRef: "assurance.mfa_session/v1",
	}
}

// rev040ProposalSpec mirrors the valid promotion proposal the intent package
// tests mint: one planned write with its authority decision and baseline, a
// bound child intent, a reservation, the required approval, the revalidation
// plan and the pinned control context.
func rev040ProposalSpec(t *testing.T, intentID string) intent.ProposalSpec {
	t.Helper()
	key := rev040ResourceKey(t, "employment", "9001", "primary")
	return intent.ProposalSpec{
		IntentID:            intentID,
		Revision:            1,
		Tenant:              values.TenantId("acme-eu"),
		OrganizationScopeID: "org:acme-eu:engineering",
		LegalEntityID:       "legal:acme-eu-gmbh",
		Subjects: []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
		},
		EffectiveTime: rev040Interval(t),
		CurrentState: []intent.StateAssertion{{
			Subject:       intent.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			ResourceKey:   key,
			FieldPath:     "assignment.position_ref",
			CanonicalText: "position:senior-engineer",
		}},
		ProposedState: []intent.StateAssertion{{
			Subject:       intent.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			ResourceKey:   key,
			FieldPath:     "assignment.position_ref",
			CanonicalText: "position:staff-engineer",
		}},
		Writes: []intent.PlannedWrite{{
			Subject:                 intent.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			ResourceKey:             key,
			FieldPath:               "assignment.position_ref",
			CurrentCanonicalText:    "position:senior-engineer",
			ProposedCanonicalText:   "position:staff-engineer",
			SourceAuthorityDecision: "authority.local_master/v1",
			ExpectedRevision:        rev040Revision(t, "people.employment.9001", 42),
		}},
		Children: []intent.ChildIntentBinding{{
			Definition:          intent.Ref{TypeID: "hcmnext.rewards.change_base_pay", Version: 1},
			ChildIntentID:       "child:1",
			Ordinal:             1,
			MaterialInputDigest: "child-material-1",
		}},
		Reservations: []intent.Reservation{{
			ReservationID: "reservation:1", Kind: "BUDGET",
			Expiry: values.NewInstant(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
		}},
		RequiredApprovals: []intent.RequiredApproval{{
			RequirementID: "req.promotion_manager/v1", SeparationConstraint: "not_requester",
		}},
		SourceBaselines: []intent.SourceBaseline{{
			StreamID:         "people.employment.9001",
			ExpectedRevision: rev040Revision(t, "people.employment.9001", 42),
		}},
		Attachments: []intent.AttachmentRef{{
			ArtifactID: "artifact:justification", AlgorithmID: "sha256", Digest: "abcd",
		}},
		Purpose: intent.PurposeDecision{
			Purpose:        "promotion.annual_cycle",
			RecipientRef:   "recipient:hr-ops",
			DestinationRef: "destination:internal",
			ResidencyRef:   "residency:eu",
		},
		Revalidation:     intent.RevalidationPlan{Rules: []string{"promotion_execution_revalidation/v1"}},
		ControlSnapshots: rev040ControlSnapshots(),
		CreatedBy:        rev040Principal(),
		DetectedChildRefs: []intent.Ref{
			{TypeID: "hcmnext.rewards.change_base_pay", Version: 1},
		},
	}
}

// rev040BaseInput is a complete, compilable plan input. The scenario bridge
// only adds scenario-derived entries to it; everything the intent pipeline
// requires (proposal digest, governance, conflict, participant, read,
// append, idempotency, approval, revalidation, expiry) comes from here.
func rev040BaseInput(t *testing.T, def intent.Definition, rev intent.ProposalRevision) intent.PlanInput {
	t.Helper()
	expiry := values.NewInstant(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	return intent.PlanInput{
		Proposal:   rev,
		Definition: def,
		Mode:       intent.ModeSimulate,
		Governance: intent.GovernanceSnapshot{
			SnapshotDigest: "governance-1",
			AuthZDecision:  "PERMIT",
			LegalDecision:  "PERMIT",
			PolicyDecision: "PERMIT",
			RiskDecision:   "ACCEPT",
		},
		Conflict: intent.ConflictSnapshot{
			SnapshotDigest: "conflict-1",
			FenceToken:     "fence-1",
			FootprintRef:   "promotion_affected_fields_and_effective_interval/v1",
		},
		Participants: []intent.PlanParticipant{{
			ParticipantID: "participant:people",
			StreamID:      "people.employment.9001",
			StorageClass:  "LOCAL_POSTGRES",
			Local:         true,
		}},
		Reads: []intent.PlannedRead{{
			ResourceKey:      rev040ResourceKey(t, "employment", "9001", "primary"),
			ExpectedRevision: rev040Revision(t, "people.employment.9001", 42),
		}},
		Appends: []intent.PlannedAppend{{
			StreamID:         "people.employment.9001",
			ExpectedSequence: 43,
			EventType:        "people.assignment_position_changed/v1",
			PayloadDigest:    "append-payload-1",
		}},
		ProjectionMutations: []intent.ProjectionMutation{{
			ProjectionID: "projection.worker_state/v1",
			ResourceKey:  rev040ResourceKey(t, "employment", "9001", "primary"),
			Operation:    "UPSERT",
		}},
		Preconditions: []intent.CommitPrecondition{{
			Kind: "SOURCE_AUTHORITY", Ref: "authority.local_master/v1",
		}},
		Reservations: []intent.ReservationBinding{{
			ReservationID: "reservation:1", Expiry: expiry,
		}},
		IdempotencyRecordRef:   "idempotency:promote:9001",
		ApprovalRequirementIDs: []string{"req.promotion_manager/v1"},
		RevalidationRuleRefs:   []string{"promotion_execution_revalidation/v1"},
		ExpiresAt:              expiry,
	}
}

func rev040Setup(t *testing.T, intentID string) (intent.Definition, intent.ProposalRevision) {
	t.Helper()
	reg, err := definitions.NewRegistry()
	if err != nil {
		t.Fatalf("compile catalog registry: %v", err)
	}
	def, err := reg.ResolveText("hcmnext.people.promote_worker/v1")
	if err != nil {
		t.Fatalf("resolve promote_worker: %v", err)
	}
	d, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("build digester: %v", err)
	}
	rev, err := intent.NewProposalRevision(rev040ProposalSpec(t, intentID), def, d,
		rev040IDs("01234567"), rev040Clock())
	if err != nil {
		t.Fatalf("mint proposal: %v", err)
	}
	return def, rev
}

func rev040AssertBridged(t *testing.T, plan intent.TransactionPlan, compiled IntentCompilation) {
	t.Helper()
	keys := BridgedIntentKeys(plan)
	wantKeys := make([]string, 0, len(compiled.Intents))
	for _, in := range compiled.Intents {
		wantKeys = append(wantKeys, in.Key)
	}
	sort.Strings(wantKeys)
	if fmt.Sprintf("%q", keys) != fmt.Sprintf("%q", wantKeys) {
		t.Fatalf("bridged keys = %q, want %q", keys, wantKeys)
	}
	writeSets := BridgedIntentWriteSets(plan)
	dependencies := BridgedIntentDependencies(plan)
	for _, in := range compiled.Intents {
		gotWrites := append([]string(nil), writeSets[in.Key]...)
		wantWrites := append([]string(nil), in.WriteSet...)
		sort.Strings(gotWrites)
		sort.Strings(wantWrites)
		if fmt.Sprintf("%q", gotWrites) != fmt.Sprintf("%q", wantWrites) {
			t.Fatalf("key %q write set = %q, want %q", in.Key, gotWrites, wantWrites)
		}
		gotDeps, ok := dependencies[in.Key]
		if !ok {
			t.Fatalf("key %q has no bridged dependency entry; a field was silently dropped", in.Key)
		}
		wantDeps := append([]string(nil), in.DependsOn...)
		sort.Strings(gotDeps)
		sort.Strings(wantDeps)
		if fmt.Sprintf("%q", gotDeps) != fmt.Sprintf("%q", wantDeps) {
			t.Fatalf("key %q dependencies = %q, want %q", in.Key, gotDeps, wantDeps)
		}
	}
	if len(writeSets) != len(compiled.Intents) || len(dependencies) != len(compiled.Intents) {
		t.Fatalf("bridged maps carry %d write sets and %d dependency entries for %d intents; nothing may be added or dropped",
			len(writeSets), len(dependencies), len(compiled.Intents))
	}
}

// TestTodo_REV_040_02 is the PRIMARY test: a validated scenario
// IntentCompilation bridges into the intent pipeline with no field silently
// dropped. Keys, write sets and dependencies survive the crossing into the
// compiled TransactionPlan; an invalid compilation never bridges.
func TestTodo_REV_040_02(t *testing.T) {
	t.Run("RED: an invalid compilation never bridges", func(t *testing.T) {
		bad := mustCompile006(t, planWithTwoDeltas(t), spec006())
		bad.Intents = nil
		if _, err := bad.ToIntentPlanInput(intent.PlanInput{}); err == nil {
			t.Fatal("an invalid compilation bridged into a plan input")
		}
		if _, err := bad.CompileIntentPlan(intent.PlanInput{}, rev040IDs("red")); err == nil {
			t.Fatal("an invalid compilation bridged into a transaction plan")
		}
	})

	def, rev := rev040Setup(t, "intent:rev040-primary")
	compiled := mustCompile006(t, planWithTwoDeltas(t), spec006())
	base := rev040BaseInput(t, def, rev)

	t.Run("GREEN: the bridge preserves base and intent content", func(t *testing.T) {
		in, err := compiled.ToIntentPlanInput(base)
		if err != nil {
			t.Fatalf("ToIntentPlanInput: %v", err)
		}
		if in.Proposal.MaterialDigest.Digest != rev.MaterialDigest.Digest {
			t.Fatal("the bridged input is not bound to the exact proposal digest")
		}
		if in.IdempotencyRecordRef != base.IdempotencyRecordRef {
			t.Fatal("the bridge rewrote the caller's idempotency record")
		}
		if len(in.Appends) != len(base.Appends)+len(compiled.Intents) {
			t.Fatalf("bridged appends = %d, want base %d + %d intent appends",
				len(in.Appends), len(base.Appends), len(compiled.Intents))
		}
	})

	t.Run("GREEN: the transaction plan carries every key, write set and dependency", func(t *testing.T) {
		plan, err := compiled.CompileIntentPlan(base, rev040IDs("rev040"))
		if err != nil {
			t.Fatalf("CompileIntentPlan: %v", err)
		}
		rev040AssertBridged(t, plan, compiled)
		if plan.ProposalDigest != rev.MaterialDigest.Digest {
			t.Fatal("the plan is not bound to the exact proposal digest")
		}
		if err := plan.VerifyDigest(); err != nil {
			t.Fatalf("the plan digest does not verify: %v", err)
		}
	})
}

// TestTodo_REV_040_02_Integration drives the real pipeline end to end with
// no mocks of the pipeline itself: scenario approval to scenario compile to
// the intent CompilePlan entry point to a verified TransactionPlan.
func TestTodo_REV_040_02_Integration(t *testing.T) {
	def, rev := rev040Setup(t, "intent:rev040-integration")
	plan := planWithTwoDeltas(t)
	spec := spec006()
	compiled, err := CompilePlan(plan, spec)
	if err != nil {
		t.Fatalf("scenario CompilePlan: %v", err)
	}
	if err := compiled.Validate(); err != nil {
		t.Fatalf("scenario compilation is not valid: %v", err)
	}
	tx, err := compiled.CompileIntentPlan(rev040BaseInput(t, def, rev), rev040IDs("rev040i"))
	if err != nil {
		t.Fatalf("intent pipeline CompilePlan via bridge: %v", err)
	}
	rev040AssertBridged(t, tx, compiled)
	if tx.IntentID != rev.IntentID {
		t.Fatalf("plan intent = %q, want the proposal intent %q", tx.IntentID, rev.IntentID)
	}
	if tx.GovernanceSnapshotDigest != "governance-1" || tx.ConflictFenceToken != "fence-1" {
		t.Fatalf("the plan lost its governance/conflict context: %+v", tx)
	}
	if err := tx.VerifyDigest(); err != nil {
		t.Fatalf("the integrated plan digest does not verify: %v", err)
	}
}

// rev040GenCompilation builds a valid compilation with n intents whose
// dependencies always close over earlier keys, so every draw is gateable.
func rev040GenCompilation(rng *rand.Rand, n int) IntentCompilation {
	const simRef = "simulation:gen"
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		keys = append(keys, fmt.Sprintf("gen.key.%02d", i))
	}
	compiled := IntentCompilation{
		ScenarioID: "scenario:gen", Revision: 1, RevisionDigest: "digest:gen",
		GovernanceRef: "governance:gen", GovernanceVersion: "v1",
		SimulationRef: simRef,
	}
	for i, key := range keys {
		targets := make([]string, 0, 3)
		for j := 0; j < 1+rng.Intn(3); j++ {
			targets = append(targets, fmt.Sprintf("writes.target.%d", rng.Intn(4)))
		}
		var deps []string
		for _, earlier := range keys[:i] {
			if rng.Intn(2) == 0 {
				deps = append(deps, earlier)
			}
		}
		sort.Strings(deps)
		compiled.Intents = append(compiled.Intents, CompiledIntent{
			Key:            key,
			Value:          TextValue("v"),
			Unit:           "unit",
			ProvenanceRefs: []string{"prov:gen"},
			Proposal:       "set " + key + " to v unit",
			WriteSet:       targets,
			DependsOn:      deps,
			SimulationLink: simRef + "#" + key,
		})
	}
	return compiled
}

// TestTodo_REV_040_02_Property round-trips generated compilations through
// the bridge: keys, write sets and dependencies always survive.
func TestTodo_REV_040_02_Property(t *testing.T) {
	def, rev := rev040Setup(t, "intent:rev040-property")
	rng := rand.New(rand.NewSource(42))
	for round := 0; round < 64; round++ {
		compiled := rev040GenCompilation(rng, 1+rng.Intn(6))
		if err := compiled.Validate(); err != nil {
			t.Fatalf("round %d: generated compilation is not valid: %v", round, err)
		}
		plan, err := compiled.CompileIntentPlan(rev040BaseInput(t, def, rev), rev040IDs(fmt.Sprintf("prop%02d", round)))
		if err != nil {
			t.Fatalf("round %d: CompileIntentPlan: %v", round, err)
		}
		rev040AssertBridged(t, plan, compiled)
	}
}
