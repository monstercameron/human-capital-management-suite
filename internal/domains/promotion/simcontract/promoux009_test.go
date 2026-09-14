package simcontract_test

import (
	"context"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PROMOUX-009: "Canonicalize and deduplicate promotion simulation findings."
//
// RED is a budget observation that appears twice with its internal Code and
// Message swapped between the two occurrences, or whose ordering and identity
// change across a reload. GREEN requires findings to carry a stable semantic
// identity, an owner, a severity and an affected field, plus one canonical
// explanation: exact duplicates collapse, independently sourced corroboration
// stays attributable, and ordering and count survive persistence and reload.
//
// promotion.FindingIdentity is (Code, Field, Severity) and
// promotion.DeduplicateFindings does the canonicalization; [simcontract.Assemble]
// applies it to every AssembleInput.Findings before Status is derived and
// before the contract is digested, which is the boundary these tests
// exercise. Code is part of the identity deliberately -- see the doc comment
// on [promotion.FindingIdentity] for the reasoning and for why a coordinator
// review rejected an earlier (Field, Severity)-only identity as silent data
// loss, not deduplication.

// promoux009Input returns a promotion fixture input with its Findings section
// replaced by findings, leaving every other mandatory section (writes, reads,
// approvals, and so on) exactly as [promotionFixtureInput] states them, so
// every test below is only ever driving the Findings canonicalization, not
// re-deriving the rest of a valid contract.
func promoux009Input(t testing.TB, findings []promotion.Finding) simcontract.AssembleInput {
	t.Helper()
	in := promotionFixtureInput(t)
	in.Findings = findings
	return in
}

// promoux009Assemble is a t.Fatalf-on-error convenience around Assemble for
// the tests below, all of which expect the fixture inputs they build to
// validate.
func promoux009Assemble(t testing.TB, findings []promotion.Finding) simcontract.SimulationResult {
	t.Helper()
	result, err := simcontract.Assemble(promoux009Input(t, findings))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	return result
}

// promoux009RealCompensationFindings drives the real domain rule engine --
// promotion.PreflightPromotion, not a hand-built Finding literal -- to
// reproduce the exact live bug a coordinator review caught: checkCompensation
// (internal/domains/promotion/rules.go) can raise both
// CodeProposedAmountInvalid and CodeNotARaise for one proposal, both on Field
// "proposed.base" at SeverityBlocking, because a present-but-zero proposed
// base is simultaneously an invalid amount (fails the ">0" check) and, being
// no greater than a positive current amount, not a raise. checkCompensation
// only early-returns when a side is *absent* (Get() fails), and a
// present-but-zero amount is present, so both checks run and both findings
// survive out of PreflightPromotion. They describe two different business
// problems, not one observation reported twice, and must never collapse.
func promoux009RealCompensationFindings(t testing.TB) []promotion.Finding {
	t.Helper()

	bonus, err := fixtures.Percent("0.0500")
	if err != nil {
		t.Fatalf("percent fixture: %v", err)
	}

	reader, err := fixtures.NewMemoryWorkerFacts()
	if err != nil {
		t.Fatalf("worker facts fixture: %v", err)
	}
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("worker ref: %v", err)
	}
	known, err := time.Parse(time.RFC3339, "2026-05-15T00:00:00Z")
	if err != nil {
		t.Fatalf("known timestamp: %v", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(known))
	if err != nil {
		t.Fatalf("known at: %v", err)
	}
	fields := promotion.RequiredWorkerFields()
	explanation, err := people.ExplainWorkerState(context.Background(), reader, people.ExplainWorkerStateRequest{
		Tenant:        fixtures.Tenant,
		Worker:        worker,
		AsOf:          people.AsOf{EffectiveOn: promoux009Date(t, "2026-06-01"), KnownAt: knownAt},
		Fields:        fields,
		Authorization: fixtures.AllowAll("authz.people/2026.1", "promotion_preflight", fields),
	})
	if err != nil {
		t.Fatalf("ExplainWorkerState: %v", err)
	}

	policy := promotion.DefaultPolicy()
	policy.RequireBudgetAuthority = false // keep the finding set focused on proposed.base

	req := promotion.PreflightRequest{
		Tenant:      fixtures.Tenant,
		Subject:     worker,
		WorkerState: explanation,
		// omar-reyes' seeded baseline is OPS-HRBP2/P2; OPS-HRBP3/P3 is a
		// genuine promotion (not a same-grade lateral), the same target
		// promotion_test.go's own baseRequest fixture uses.
		Target: promotion.TargetPlacement{JobCode: "OPS-HRBP3", Grade: "P3", OrgUnit: "people-ops", PayZone: "US-EAST"},
		Current: rewards.CompensationSnapshot{
			Base:               values.Value(mustFixtureMoney(t, "100000.00", "USD")),
			PayBasis:           rewards.PayBasisAnnualSalary,
			BonusTargetPercent: values.Value(bonus),
			EffectiveDate:      promoux009Date(t, "2026-06-01"),
			Watermark:          mustRevisionToken(t, "rewards.package.omar", 11),
			Complete:           true,
		},
		Proposed: rewards.CompensationSnapshot{
			// Present but zero: Base.Get() succeeds (proposedOK), so
			// checkCompensation does not early-return on an absent side --
			// yet the amount itself fails the ">0" check
			// (CodeProposedAmountInvalid), and being no greater than the
			// current 100,000.00 also fails RequireIncrease
			// (CodeNotARaise). Both fire from the one call below.
			Base:               values.Value(mustFixtureMoney(t, "0.00", "USD")),
			PayBasis:           rewards.PayBasisAnnualSalary,
			BonusTargetPercent: values.Value(bonus),
			EffectiveDate:      promoux009Date(t, "2026-06-01"),
			Watermark:          mustRevisionToken(t, "rewards.package.omar", 11),
			Complete:           true,
		},
		EffectiveDate:  promoux009Date(t, "2026-06-01"),
		EvaluationDate: promoux009Date(t, "2026-05-15"),
		BusinessReason: "promotion_into_senior_hrbp",
		Policy:         policy,
		Annualization:  rewards.DefaultAnnualization(),
	}

	result, err := promotion.PreflightPromotion(context.Background(), nil, req)
	if err != nil {
		t.Fatalf("PreflightPromotion: %v", err)
	}
	return result.Findings
}

func promoux009Date(t testing.TB, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("date(%q): %v", text, err)
	}
	return d
}

// findingsOnField filters findings to those naming the given Field, for
// assertions that care about one field in an otherwise-noisy real preflight
// result (band evaluation, budget-authority, and so on).
func findingsOnField(findings []promotion.Finding, field string) []promotion.Finding {
	var out []promotion.Finding
	for _, f := range findings {
		if f.Field == field {
			out = append(out, f)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// PRIMARY
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_009 is the PRIMARY test. It drives simcontract.Assemble
// directly -- no UI, no transport, no database -- through the shapes GREEN
// and RED name: a duplicate whose Message was reworded between two
// occurrences of the same Code, independently sourced corroboration of one
// observation, genuinely distinct findings that must never be merged, and
// (the coordinator-caught regression) two different Codes sharing a Field and
// Severity produced by the real rule engine.
func TestTodo_PROMOUX_009(t *testing.T) {
	t.Run("same_owner_duplicate_with_reworded_message_collapses_to_one", func(t *testing.T) {
		// RED, named exactly: one budget observation appears twice with its
		// Code and Message inconsistently paired between the two copies.
		// Code is the stable, rule-assigned identity (preflight.go:68/:72)
		// so it stays fixed across the two copies; what varies here is
		// Message, the operator-facing prose REFACTOR says a rendered
		// string must never gate identity. Both copies share Code, Field,
		// Severity and Owner, so they are one observation reported twice.
		result := promoux009Assemble(t, []promotion.Finding{
			{
				Code: promotion.CodeBudgetObservedShort, Severity: promotion.SeverityAdvisory,
				Field: "budget.available_amount", Owner: "rewards.budget_reader",
				Message: "observed budget from the FY2026 eng-platform pool is short of the annualized cost",
			},
			{
				Code: promotion.CodeBudgetObservedShort, Severity: promotion.SeverityAdvisory,
				Field: "budget.available_amount", Owner: "rewards.budget_reader",
				Message: "available budget is less than the cost of this promotion",
			},
		})
		if len(result.Findings) != 1 {
			t.Fatalf("Findings = %+v, want exactly 1 (a same-code, same-owner duplicate must collapse)", result.Findings)
		}
		f := result.Findings[0]
		if f.Code != promotion.CodeBudgetObservedShort || f.Field != "budget.available_amount" || f.Severity != promotion.SeverityAdvisory {
			t.Fatalf("surviving finding = %+v, want the shared Code/Field/Severity preserved", f)
		}
		if f.Owner != "rewards.budget_reader" {
			t.Fatalf("Owner = %q, want the single reporting owner", f.Owner)
		}
		if len(f.CorroboratedBy) != 0 {
			t.Fatalf("CorroboratedBy = %v, want none: this is one owner reporting twice, not corroboration", f.CorroboratedBy)
		}
		// The canonical explanation is chosen by content, not by which copy
		// happened to arrive first: the lexicographically smaller Message.
		if f.Message != "available budget is less than the cost of this promotion" {
			t.Fatalf("Message = %q, want the deterministically smaller of the two", f.Message)
		}
	})

	t.Run("distinct_owners_corroborate_without_becoming_two_rows", func(t *testing.T) {
		// Two independent sources both raised the same Code for the same
		// Field/Severity. GREEN requires this to collapse to one finding
		// while keeping both sources attributable.
		result := promoux009Assemble(t, []promotion.Finding{
			{
				Code: promotion.CodeBudgetObservedShort, Severity: promotion.SeverityAdvisory,
				Field: "budget.available_amount", Owner: "rewards.budget_reader",
				Message: "observed available budget is less than the annualized cost",
			},
			{
				Code: promotion.CodeBudgetObservedShort, Severity: promotion.SeverityAdvisory,
				Field: "budget.available_amount", Owner: "finance.compensation_pool_snapshot",
				Message: "the finance snapshot independently reports the pool as short",
			},
		})
		if len(result.Findings) != 1 {
			t.Fatalf("Findings = %+v, want exactly 1 (independent corroboration is still one observation)", result.Findings)
		}
		f := result.Findings[0]
		if f.Owner != "finance.compensation_pool_snapshot" {
			t.Fatalf("Owner = %q, want the lexicographically first of the two contributing owners", f.Owner)
		}
		if len(f.CorroboratedBy) != 1 || f.CorroboratedBy[0] != "rewards.budget_reader" {
			t.Fatalf("CorroboratedBy = %v, want [rewards.budget_reader]: corroboration must stay attributable", f.CorroboratedBy)
		}
	})

	t.Run("distinct_fields_are_never_merged", func(t *testing.T) {
		// Two genuinely different observations (different Field) that
		// happen to share Severity must both survive.
		result := promoux009Assemble(t, []promotion.Finding{
			{Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory, Field: "budget", Owner: "promotion.rules", Message: "observation only"},
			{Code: promotion.CodeIncreaseOverThreshold, Severity: promotion.SeverityAdvisory, Field: "proposed.base", Owner: "promotion.rules", Message: "large increase"},
		})
		if len(result.Findings) != 2 {
			t.Fatalf("Findings = %+v, want exactly 2 (distinct fields are distinct observations)", result.Findings)
		}
		// Deterministic order: sorted by Field, so "budget" sorts before
		// "proposed.base".
		if result.Findings[0].Field != "budget" || result.Findings[1].Field != "proposed.base" {
			t.Fatalf("Findings order = %+v, want budget then proposed.base", result.Findings)
		}
	})

	t.Run("distinct_codes_sharing_field_and_severity_are_never_merged", func(t *testing.T) {
		// Hand-built version of the coordinator-caught regression: two
		// different Codes, same Field, same Severity, must both survive
		// rather than have one silently deleted by an identity that only
		// looks at (Field, Severity).
		result := promoux009Assemble(t, []promotion.Finding{
			{Code: promotion.CodeTargetGradeRequired, Severity: promotion.SeverityBlocking, Field: "target.grade", Owner: "promotion.rules", Message: "a promotion must name the target grade"},
			{Code: promotion.CodeSameGrade, Severity: promotion.SeverityBlocking, Field: "target.grade", Owner: "promotion.rules", Message: "target grade equals the current grade"},
		})
		if len(result.Findings) != 2 {
			t.Fatalf("Findings = %+v, want exactly 2: different Codes on the same Field/Severity are different observations", result.Findings)
		}
		codes := map[string]bool{result.Findings[0].Code: true, result.Findings[1].Code: true}
		if !codes[promotion.CodeTargetGradeRequired] || !codes[promotion.CodeSameGrade] {
			t.Fatalf("Findings = %+v, want both %s and %s present", result.Findings, promotion.CodeTargetGradeRequired, promotion.CodeSameGrade)
		}
	})

	t.Run("real_compensation_findings_survive_distinct_codes_on_one_field", func(t *testing.T) {
		// The regression test itself: drive the real rule engine
		// (promotion.PreflightPromotion), not a hand-built literal, through
		// simcontract.Assemble, and require both CodeProposedAmountInvalid
		// and CodeNotARaise to survive with their own Code and Message
		// intact. An identity of (Field, Severity) alone silently drops one
		// of these; this is exactly what would have caught that before it
		// shipped.
		preflightFindings := promoux009RealCompensationFindings(t)
		onProposedBase := findingsOnField(preflightFindings, "proposed.base")
		gotCodes := map[string]bool{}
		for _, f := range onProposedBase {
			gotCodes[f.Code] = true
		}
		if !gotCodes[promotion.CodeProposedAmountInvalid] || !gotCodes[promotion.CodeNotARaise] {
			t.Fatalf("PreflightPromotion did not reproduce the fixture as intended: proposed.base findings = %+v, want both %s and %s (fix the fixture, not the assertion, if this fails)",
				onProposedBase, promotion.CodeProposedAmountInvalid, promotion.CodeNotARaise)
		}

		result := promoux009Assemble(t, preflightFindings)
		surviving := findingsOnField(result.Findings, "proposed.base")
		if len(surviving) != 2 {
			t.Fatalf("Findings on proposed.base after dedup = %+v, want exactly 2 (CodeProposedAmountInvalid and CodeNotARaise are different business problems)", surviving)
		}
		byCode := map[string]promotion.Finding{}
		for _, f := range surviving {
			byCode[f.Code] = f
		}
		invalid, ok := byCode[promotion.CodeProposedAmountInvalid]
		if !ok {
			t.Fatalf("CodeProposedAmountInvalid was dropped by dedup: surviving = %+v", surviving)
		}
		if invalid.Message != "proposed base pay must be a disclosed amount greater than zero" {
			t.Fatalf("CodeProposedAmountInvalid message = %q, want its own explanation intact, not folded into another finding's", invalid.Message)
		}
		notARaise, ok := byCode[promotion.CodeNotARaise]
		if !ok {
			t.Fatalf("CodeNotARaise was dropped by dedup: surviving = %+v", surviving)
		}
		if notARaise.Message != "the proposed amount must be greater than the current amount" {
			t.Fatalf("CodeNotARaise message = %q, want its own explanation intact, not folded into another finding's", notARaise.Message)
		}
	})

	t.Run("status_reflects_the_deduplicated_set_not_the_raw_append_count", func(t *testing.T) {
		// A blocking finding duplicated three times over by the same owner
		// must still resolve BLOCKED (never flip to executable because the
		// duplicates got folded away), and folding must not manufacture a
		// severity that was never present.
		result := promoux009Assemble(t, []promotion.Finding{
			{Code: promotion.CodeBudgetAuthorityMissing, Severity: promotion.SeverityBlocking, Field: "budget", Owner: "promotion.rules", Message: "missing"},
			{Code: promotion.CodeBudgetAuthorityMissing, Severity: promotion.SeverityBlocking, Field: "budget", Owner: "promotion.rules", Message: "missing (again)"},
			{Code: promotion.CodeBudgetAuthorityMissing, Severity: promotion.SeverityBlocking, Field: "budget", Owner: "promotion.rules", Message: "missing (a third time)"},
		})
		if len(result.Findings) != 1 {
			t.Fatalf("Findings = %+v, want exactly 1", result.Findings)
		}
		if result.Status != simcontract.ResultBlocked {
			t.Fatalf("Status = %s, want %s", result.Status, simcontract.ResultBlocked)
		}
	})
}

// ---------------------------------------------------------------------------
// PROPERTY
// ---------------------------------------------------------------------------

// promoux009Observation is one intended, semantically distinct observation
// the property test below builds a randomized append sequence from. code and
// field are both explicit and unique per observation within a trial, because
// (Code, Field, Severity) is the identity under test.
type promoux009Observation struct {
	code     string
	field    string
	severity promotion.Severity
	owners   []string // every owner that will independently report this observation
}

// promoux009AppendsFor expands one observation into the individual Finding
// values a rule engine (or two independently sourced ones) might have
// appended for it. Every owner shares the observation's Code -- that is what
// makes them corroboration of the *same* finding under a Code-inclusive
// identity, distinct from the confusable-neighbour case the trial loop adds
// separately.
func promoux009AppendsFor(o promoux009Observation, tag int) []promotion.Finding {
	out := make([]promotion.Finding, 0, len(o.owners))
	for i, owner := range o.owners {
		out = append(out, promotion.Finding{
			Code:     o.code,
			Severity: o.severity,
			Field:    o.field,
			Owner:    owner,
			Message:  "trial message " + itoa(tag) + "/" + itoa(i),
		})
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestTodo_PROMOUX_009_Property is the PROPERTY test. It generates finding
// sets with shuffled input order, duplicate insertions (same-owner and
// cross-owner, always same-Code) and, every trial, a "confusable neighbour":
// a second finding with a genuinely different Code sharing the first
// observation's Field and Severity. Requiring that neighbour to survive
// alongside the original is what makes this generator capable of catching
// the coordinator's regression: a generator that only ever produces findings
// that are already-genuine duplicates cannot distinguish "deduplicates
// correctly" from "deletes too much" -- it would have passed against the
// (Field, Severity)-only identity this todo rejected.
func TestTodo_PROMOUX_009_Property(t *testing.T) {
	random := rand.New(rand.NewSource(20260913))
	for trial := 0; trial < 200; trial++ {
		observationCount := 2 + random.Intn(4) // 2..5 distinct observations
		observations := make([]promoux009Observation, observationCount)
		for i := range observations {
			owners := []string{"owner.a"}
			if random.Intn(2) == 0 {
				owners = append(owners, "owner.b") // cross-owner corroboration, same Code
			}
			severity := []promotion.Severity{promotion.SeverityAdvisory, promotion.SeverityBlocking, promotion.SeverityNeedsData}[random.Intn(3)]
			observations[i] = promoux009Observation{
				code:     "trial.code." + itoa(trial) + "." + itoa(i),
				field:    "trial.field." + itoa(trial) + "." + itoa(i),
				severity: severity,
				owners:   owners,
			}
		}

		// Build the full set of appends: every observation's owner-appends,
		// each occasionally duplicated with a reworded Message (same
		// Code/Field/Severity/Owner -- the corrected RED case, message
		// drift between two copies of one duplicate). A property test that
		// only shuffles already-distinct findings proves nothing about
		// dedup, so every trial deliberately appends at least one genuine
		// duplicate.
		var appends []promotion.Finding
		for i, o := range observations {
			base := promoux009AppendsFor(o, trial*100+i)
			appends = append(appends, base...)
			extra := random.Intn(3)
			for e := 0; e < extra; e++ {
				dup := base[0]
				dup.Message += " (reworded copy)"
				appends = append(appends, dup)
			}
		}

		// The confusable neighbour: a distinct Code on observations[0]'s
		// Field and Severity. This is the shape the coordinator's review
		// caught being silently deleted -- CodeProposedAmountInvalid vs
		// CodeNotARaise, both Field "proposed.base"/BLOCKING -- so every
		// trial exercises it, not just a hand-built example.
		neighbor := promotion.Finding{
			Code:     "trial.neighbor_code." + itoa(trial),
			Field:    observations[0].field,
			Severity: observations[0].severity,
			Owner:    "owner.a",
			Message:  "a genuinely different observation on the same field and severity",
		}
		appends = append(appends, neighbor)

		wantCount := len(observations) + 1 // +1 for the confusable neighbour

		// Run Assemble twice over two independently shuffled orderings of
		// the identical append multiset.
		first := append([]promotion.Finding(nil), appends...)
		random.Shuffle(len(first), func(i, j int) { first[i], first[j] = first[j], first[i] })
		second := append([]promotion.Finding(nil), appends...)
		random.Shuffle(len(second), func(i, j int) { second[i], second[j] = second[j], second[i] })

		resultA := promoux009Assemble(t, first)
		resultB := promoux009Assemble(t, second)

		if len(resultA.Findings) != wantCount {
			t.Fatalf("trial %d: len(Findings) = %d, want %d (distinct observations, including the confusable neighbour); appends=%+v", trial, len(resultA.Findings), wantCount, appends)
		}
		if !reflect.DeepEqual(resultA.Findings, resultB.Findings) {
			t.Fatalf("trial %d: shuffled orderings of the identical append multiset produced different Findings:\nA=%+v\nB=%+v", trial, resultA.Findings, resultB.Findings)
		}
		if resultA.Digest != resultB.Digest {
			t.Fatalf("trial %d: shuffled orderings produced different digests: %s vs %s", trial, resultA.Digest, resultB.Digest)
		}

		// The confusable neighbour and observations[0] must both survive,
		// distinctly, on the shared field -- the exact property an identity
		// coarser than (Code, Field, Severity) would fail.
		onSharedField := findingsOnField(resultA.Findings, observations[0].field)
		if len(onSharedField) != 2 {
			t.Fatalf("trial %d: field %q carries %d findings, want 2 (the original observation and its confusable neighbour): %+v", trial, observations[0].field, len(onSharedField), onSharedField)
		}
		codesOnField := map[string]bool{onSharedField[0].Code: true, onSharedField[1].Code: true}
		if !codesOnField[observations[0].code] || !codesOnField[neighbor.Code] {
			t.Fatalf("trial %d: shared-field findings = %+v, want codes %q and %q both present", trial, onSharedField, observations[0].code, neighbor.Code)
		}

		// Order is deterministic by content: sorted ascending by
		// (Field, Severity, Code), strictly increasing since no two
		// surviving findings ever share a full identity.
		for i := 1; i < len(resultA.Findings); i++ {
			a, b := resultA.Findings[i-1], resultA.Findings[i]
			less := a.Field < b.Field ||
				(a.Field == b.Field && a.Severity < b.Severity) ||
				(a.Field == b.Field && a.Severity == b.Severity && a.Code < b.Code)
			if !less {
				t.Fatalf("trial %d: Findings not sorted ascending by (Field, Severity, Code): %+v", trial, resultA.Findings)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// GOLDEN
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_009_Golden is the GOLDEN test. It pins the exact
// deduplicated Findings content and the exact digest for one fixed input
// mixing every PROMOUX-009 shape: a same-Code duplicate with a reworded
// Message, cross-owner corroboration, a distinct-Code pair sharing a Field
// and Severity that must both survive, and one solitary finding. The digest
// and the exact Findings slice were computed by running this test, reading
// the failure's reported value, and pinning that value here -- never
// guessed.
func TestTodo_PROMOUX_009_Golden(t *testing.T) {
	findings := []promotion.Finding{
		// Same-Code duplicate, reworded Message between the two copies.
		{Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory, Field: "budget", Owner: "rewards.budget_reader", Message: "observed budget is not a reservation"},
		{Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory, Field: "budget", Owner: "rewards.budget_reader", Message: "budget is an observation, not a reservation"},
		// Cross-owner corroboration, same Code.
		{Code: promotion.CodeIncreaseOverThreshold, Severity: promotion.SeverityAdvisory, Field: "proposed.base", Owner: "promotion.rules", Message: "annualized increase exceeds the review threshold"},
		{Code: promotion.CodeIncreaseOverThreshold, Severity: promotion.SeverityAdvisory, Field: "proposed.base", Owner: "rewards.pay_equity_review", Message: "pay equity review independently flags the same increase"},
		// Distinct Codes sharing a Field and Severity -- must both survive.
		{Code: promotion.CodeBudgetObservedShort, Severity: promotion.SeverityAdvisory, Field: "budget.available_amount", Owner: "rewards.budget_reader", Message: "observed budget is short of the annualized cost"},
		{Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory, Field: "budget.available_amount", Owner: "rewards.budget_reader", Message: "this observation is not a reservation"},
		// A solitary finding, no duplication or corroboration.
		{Code: promotion.CodeSameGrade, Severity: promotion.SeverityBlocking, Field: "target.grade", Owner: "promotion.rules", Message: "target grade equals the current grade"},
	}

	result := promoux009Assemble(t, findings)

	const wantDigest = "sha256:f09ec2d078428f0302ed5cd2431894bf31b8b30ea9e1aa0fd37697c508301625"
	if result.Digest != wantDigest {
		t.Fatalf("digest = %s, want %s", result.Digest, wantDigest)
	}

	want := []promotion.Finding{
		{Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory, Field: "budget", Owner: "rewards.budget_reader", Message: "budget is an observation, not a reservation"},
		{Code: promotion.CodeBudgetObservationOnly, Severity: promotion.SeverityAdvisory, Field: "budget.available_amount", Owner: "rewards.budget_reader", Message: "this observation is not a reservation"},
		{Code: promotion.CodeBudgetObservedShort, Severity: promotion.SeverityAdvisory, Field: "budget.available_amount", Owner: "rewards.budget_reader", Message: "observed budget is short of the annualized cost"},
		{Code: promotion.CodeIncreaseOverThreshold, Severity: promotion.SeverityAdvisory, Field: "proposed.base", Owner: "promotion.rules", CorroboratedBy: []string{"rewards.pay_equity_review"}, Message: "annualized increase exceeds the review threshold"},
		{Code: promotion.CodeSameGrade, Severity: promotion.SeverityBlocking, Field: "target.grade", Owner: "promotion.rules", Message: "target grade equals the current grade"},
	}
	if !reflect.DeepEqual(result.Findings, want) {
		t.Fatalf("Findings =\n%+v\nwant\n%+v", result.Findings, want)
	}
	if result.Status != simcontract.ResultBlocked {
		t.Fatalf("Status = %s, want %s (the SAME_GRADE finding blocks)", result.Status, simcontract.ResultBlocked)
	}
}

// ---------------------------------------------------------------------------
// INTEGRATION
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_009_Integration is the INTEGRATION test. It carries a
// duplicate-laden finding set through the full Assemble -> Persist.Store ->
// Persist.Load round trip against this package's own reference store -- the
// "real store" this kernel-pure package has, the same one
// TestTodo_PROMO_004's own zero-effect proof and the Race test reach through
// -- and requires the deduplicated count, order and digest to survive
// unchanged. Persist never re-derives anything from what it is handed, so
// this is the test that proves dedup happening inside Assemble, not on
// reload, is the right side of that boundary: Persist.Load returns exactly
// what Assemble produced, nothing recomputed.
func TestTodo_PROMOUX_009_Integration(t *testing.T) {
	ctx := context.Background()
	store := simcontract.NewMemoryStore()

	findings := []promotion.Finding{
		{Code: promotion.CodeBudgetObservedShort, Severity: promotion.SeverityAdvisory, Field: "budget.available_amount", Owner: "rewards.budget_reader", Message: "short"},
		{Code: promotion.CodeBudgetObservedShort, Severity: promotion.SeverityAdvisory, Field: "budget.available_amount", Owner: "rewards.budget_reader", Message: "short (reworded copy)"},
		{Code: promotion.CodeIncreaseOverThreshold, Severity: promotion.SeverityAdvisory, Field: "proposed.base", Owner: "finance.snapshot", Message: "corroborated"},
		{Code: promotion.CodeIncreaseOverThreshold, Severity: promotion.SeverityAdvisory, Field: "proposed.base", Owner: "promotion.rules", Message: "primary"},
	}

	assembled := promoux009Assemble(t, findings)
	if len(assembled.Findings) != 2 {
		t.Fatalf("Assemble: len(Findings) = %d, want 2 (deduplicated before Store even sees it)", len(assembled.Findings))
	}

	stored, already, err := store.Store(ctx, assembled)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if already {
		t.Fatal("first Store reported already-stored")
	}
	if !reflect.DeepEqual(stored.Findings, assembled.Findings) {
		t.Fatalf("Store returned Findings = %+v, want the identical deduplicated set %+v", stored.Findings, assembled.Findings)
	}

	loaded, ok, err := store.Load(ctx, assembled.Digest)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load did not find the stored artifact")
	}
	if !reflect.DeepEqual(loaded.Findings, assembled.Findings) {
		t.Fatalf("reload produced different Findings:\nbefore=%+v\nafter=%+v", assembled.Findings, loaded.Findings)
	}
	if loaded.Digest != assembled.Digest {
		t.Fatalf("reload digest = %s, want %s: count/order must survive persistence and reload", loaded.Digest, assembled.Digest)
	}
	if err := loaded.VerifyDigest(); err != nil {
		t.Fatalf("VerifyDigest on the reloaded artifact: %v", err)
	}

	// A second, independently ordered Assemble of the same logical findings
	// must produce a byte-identical artifact and therefore be recognized as
	// already stored -- reload determinism, proven through the store's own
	// digest-identity contract rather than by re-inspecting fields by hand.
	reordered := []promotion.Finding{findings[3], findings[2], findings[1], findings[0]}
	again := promoux009Assemble(t, reordered)
	if again.Digest != assembled.Digest {
		t.Fatalf("reordered input digest = %s, want %s", again.Digest, assembled.Digest)
	}
	_, already, err = store.Store(ctx, again)
	if err != nil {
		t.Fatalf("Store(again): %v", err)
	}
	if !already {
		t.Fatal("Store did not recognize the reordered-input artifact as already stored under the same digest")
	}
}

// ---------------------------------------------------------------------------
// MUTATION
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_009_Mutation is PROMOUX-009's MUTATION matrix test, in the
// style of TestTodo_PROMO_004_Mutation in mutation_test.go: each subtest pins
// an exact boundary a mutant could flip without any other test here noticing.
func TestTodo_PROMOUX_009_Mutation(t *testing.T) {
	t.Run("same_code_exact_duplicate_collapses", func(t *testing.T) {
		// A mutant that dropped deduplication entirely (Findings passed
		// through unchanged) would report 2 here.
		result := promoux009Assemble(t, []promotion.Finding{
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "m"},
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "m"},
		})
		if len(result.Findings) != 1 {
			t.Fatalf("len(Findings) = %d, want 1", len(result.Findings))
		}
	})

	t.Run("distinct_owners_same_code_corroborate_rather_than_split_or_vanish", func(t *testing.T) {
		// A mutant that folded Owner into the identity (so two owners never
		// match) would report 2 rows. A mutant that kept only the first
		// finding seen per identity, discarding the rest instead of
		// recording corroboration, would report an empty CorroboratedBy.
		// Both are distinguishable from the correct answer.
		result := promoux009Assemble(t, []promotion.Finding{
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "b", Message: "m1"},
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "a", Message: "m2"},
		})
		if len(result.Findings) != 1 {
			t.Fatalf("len(Findings) = %d, want 1 (owner must not be part of identity)", len(result.Findings))
		}
		if result.Findings[0].Owner != "a" {
			t.Fatalf("Owner = %q, want %q (lexicographically first)", result.Findings[0].Owner, "a")
		}
		if len(result.Findings[0].CorroboratedBy) != 1 || result.Findings[0].CorroboratedBy[0] != "b" {
			t.Fatalf("CorroboratedBy = %v, want [b] (corroboration must not be discarded)", result.Findings[0].CorroboratedBy)
		}
	})

	t.Run("same_code_with_reworded_message_still_collapses", func(t *testing.T) {
		// The corrected RED case: Code stays fixed (the stable, rule-
		// assigned identity) across two copies of one duplicate, and only
		// the operator-facing Message differs. A mutant that folded Message
		// into the identity would see these as two distinct findings.
		result := promoux009Assemble(t, []promotion.Finding{
			{Code: "code-A", Severity: promotion.SeverityBlocking, Field: "f", Owner: "o", Message: "message-1"},
			{Code: "code-A", Severity: promotion.SeverityBlocking, Field: "f", Owner: "o", Message: "message-2"},
		})
		if len(result.Findings) != 1 {
			t.Fatalf("len(Findings) = %d, want 1: a same-Code duplicate must collapse regardless of Message wording", len(result.Findings))
		}
	})

	t.Run("distinct_codes_sharing_field_and_severity_never_collapse", func(t *testing.T) {
		// The coordinator-caught regression, pinned as its own mutant
		// boundary: a mutant that dropped Code from FindingIdentity
		// (collapsing on Field+Severity alone) would report 1 here and
		// silently discard one of the two findings.
		result := promoux009Assemble(t, []promotion.Finding{
			{Code: promotion.CodeProposedAmountInvalid, Severity: promotion.SeverityBlocking, Field: "proposed.base", Owner: "o", Message: "proposed base pay must be a disclosed amount greater than zero"},
			{Code: promotion.CodeNotARaise, Severity: promotion.SeverityBlocking, Field: "proposed.base", Owner: "o", Message: "the proposed amount must be greater than the current amount"},
		})
		if len(result.Findings) != 2 {
			t.Fatalf("len(Findings) = %d, want 2: different Codes are different observations even on the same Field/Severity", len(result.Findings))
		}
	})

	t.Run("distinct_fields_never_collapse", func(t *testing.T) {
		// A mutant that dropped Field from FindingIdentity (collapsing on
		// Code+Severity alone) would report 1 here.
		result := promoux009Assemble(t, []promotion.Finding{
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f1", Owner: "o", Message: "m"},
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f2", Owner: "o", Message: "m"},
		})
		if len(result.Findings) != 2 {
			t.Fatalf("len(Findings) = %d, want 2", len(result.Findings))
		}
	})

	t.Run("canonical_message_is_order_independent", func(t *testing.T) {
		// A mutant that kept "whichever copy was appended last" (or first)
		// instead of the deterministic Message minimum would produce a
		// different canonical Message depending on append order.
		forward := promoux009Assemble(t, []promotion.Finding{
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "z-message"},
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "a-message"},
		})
		backward := promoux009Assemble(t, []promotion.Finding{
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "a-message"},
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "z-message"},
		})
		if len(forward.Findings) != 1 || len(backward.Findings) != 1 {
			t.Fatalf("expected both orderings to collapse to 1: forward=%+v backward=%+v", forward.Findings, backward.Findings)
		}
		if forward.Findings[0].Message != "a-message" || backward.Findings[0].Message != "a-message" {
			t.Fatalf("canonical Message = %q / %q, want %q regardless of append order", forward.Findings[0].Message, backward.Findings[0].Message, "a-message")
		}
	})

	t.Run("digest_does_not_depend_on_how_many_times_a_duplicate_fired", func(t *testing.T) {
		// The exact failure mode PROMOUX-009 exists to close: a mutant that
		// digested Findings before dedup would make the digest sensitive to
		// how many times a rule happened to fire, which breaks the
		// contract's own "digest is a pure function of content" promise.
		once := promoux009Assemble(t, []promotion.Finding{
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "m"},
		})
		fivefold := promoux009Assemble(t, []promotion.Finding{
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "m"},
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "m"},
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "m"},
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "m"},
			{Code: "c", Severity: promotion.SeverityAdvisory, Field: "f", Owner: "o", Message: "m"},
		})
		if once.Digest != fivefold.Digest {
			t.Fatalf("digest depends on append count: once=%s fivefold=%s, want equal", once.Digest, fivefold.Digest)
		}
	})
}
