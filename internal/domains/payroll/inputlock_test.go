package payroll

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustPayrollInstant(sec int64) values.Instant {
	in, err := values.NewInstantFromUnix(sec, 0)
	if err != nil {
		panic(err)
	}
	return in
}

func inputCutoffFixture(t *testing.T) InputCutoff {
	t.Helper()
	cutoff, err := NewInputCutoff(payrollAsOf(t, 1798848000), payrollAsOf(t, 1798761600))
	if err != nil {
		t.Fatalf("NewInputCutoff: %v", err)
	}
	return cutoff
}

func submittedInput(id, worker string, category InputCategory) SubmittedPayrollInput {
	return SubmittedPayrollInput{
		InputID:           id,
		WorkerRef:         worker,
		Category:          category,
		SourceDigest:      "sha256:source-" + id,
		ApprovalDigest:    "sha256:approval-" + id,
		AttestationDigest: "sha256:attestation-" + id,
	}
}

func submittedInputAt(id, worker string, category InputCategory, observed, watermark int64) SubmittedPayrollInput {
	in := submittedInput(id, worker, category)
	in.ObservedAt = mustPayrollInstant(observed)
	in.SourceWatermark = mustPayrollInstant(watermark)
	return in
}

func lockInputsFixture(t *testing.T) (FrozenPopulation, InputCutoff, []SubmittedPayrollInput) {
	t.Helper()
	pop := frozenPayrollPopulation(t)
	cutoff := inputCutoffFixture(t)
	inputs := []SubmittedPayrollInput{
		submittedInputAt("in-benefits", "worker-a", InputCategoryBenefits, 1798800000, 1798761600),
		submittedInputAt("in-corrections", "worker-a", InputCategoryCorrections, 1798800000, 1798761600),
		submittedInputAt("in-deductions", "worker-a", InputCategoryDeductions, 1798800000, 1798761600),
		submittedInputAt("in-earnings", "worker-a", InputCategoryEarnings, 1798800000, 1798761600),
		submittedInputAt("in-tax", "worker-a", InputCategoryTax, 1798800000, 1798761600),
		submittedInputAt("in-time", "worker-a", InputCategoryTime, 1798800000, 1798761600),
		// Each RED exclusion reason is classified, never silently included.
		func() SubmittedPayrollInput {
			in := submittedInputAt("in-stale", "worker-b", InputCategoryEarnings, 1798800000, 1798600000)
			return in
		}(),
		func() SubmittedPayrollInput {
			in := submittedInputAt("in-late", "worker-b", InputCategoryTime, 1798900000, 1798761600)
			return in
		}(),
		func() SubmittedPayrollInput {
			in := submittedInputAt("in-unapproved", "worker-b", InputCategoryDeductions, 1798800000, 1798761600)
			in.ApprovalDigest = ""
			return in
		}(),
		func() SubmittedPayrollInput {
			in := submittedInputAt("in-unattested", "worker-b", InputCategoryBenefits, 1798800000, 1798761600)
			in.AttestationDigest = ""
			return in
		}(),
	}
	return pop, cutoff, inputs
}

// TestTodo_PAYRUN_003 is the primary acceptance case: inputs collected before
// the cutoff lock into a manifest binding every payroll category while stale,
// late, unapproved, and unattested inputs are classified, never silently
// included.
func TestTodo_PAYRUN_003(t *testing.T) {
	run := validPayrollRun(t)
	pop, cutoff, inputs := lockInputsFixture(t)
	manifest, err := LockPayrollInputs(run, pop, cutoff, inputs)
	if err != nil {
		t.Fatalf("LockPayrollInputs: %v", err)
	}
	if len(manifest.Included) != 6 {
		t.Fatalf("included = %d, want 6", len(manifest.Included))
	}
	seen := map[InputCategory]bool{}
	for _, line := range manifest.Included {
		seen[line.Category] = true
	}
	for _, category := range []InputCategory{InputCategoryEarnings, InputCategoryTime, InputCategoryDeductions, InputCategoryTax, InputCategoryBenefits, InputCategoryCorrections} {
		if !seen[category] {
			t.Fatalf("locked manifest does not bind category %s", category)
		}
	}
	if len(manifest.Excluded) != 4 {
		t.Fatalf("excluded = %d, want 4", len(manifest.Excluded))
	}
	want := map[string]InputDisposition{
		"in-stale": InputDispositionStale, "in-late": InputDispositionLate,
		"in-unapproved": InputDispositionUnapproved, "in-unattested": InputDispositionUnattested,
	}
	for _, excluded := range manifest.Excluded {
		if want[excluded.InputID] != excluded.Disposition || excluded.Reason == "" {
			t.Fatalf("excluded %s classified as %s (%q)", excluded.InputID, excluded.Disposition, excluded.Reason)
		}
	}
	for _, excluded := range manifest.Excluded {
		for _, line := range manifest.Included {
			if excluded.InputID == line.InputID {
				t.Fatalf("excluded input %s is also included", excluded.InputID)
			}
		}
	}
	if manifest.RunID != run.RunID || manifest.RunRevision != run.Revision || manifest.PopulationDigest != pop.Digest || manifest.ManifestDigest == "" {
		t.Fatalf("manifest does not bind run and population: %+v", manifest)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	explanation, err := manifest.Explain()
	if err != nil || explanation.IncludedCount != 6 || explanation.ExcludedCount != 4 {
		t.Fatalf("explanation = %+v, err = %v", explanation, err)
	}
}

// TestTodo_PAYRUN_003_Property proves classification precedence, cutoff and
// watermark boundaries, and caller-order independence of the locked digest.
func TestTodo_PAYRUN_003_Property(t *testing.T) {
	run := validPayrollRun(t)
	pop, cutoff, _ := lockInputsFixture(t)

	// An input failing every gate reports the highest-precedence disposition.
	worst := submittedInputAt("in-worst", "worker-a", InputCategoryEarnings, 1798900000, 1798600000)
	worst.ApprovalDigest, worst.AttestationDigest = "", ""
	manifest, err := LockPayrollInputs(run, pop, cutoff, []SubmittedPayrollInput{worst})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Excluded) != 1 || manifest.Excluded[0].Disposition != InputDispositionStale {
		t.Fatalf("precedence = %+v", manifest.Excluded)
	}

	lateUnapproved := submittedInputAt("in-lu", "worker-a", InputCategoryTime, 1798900000, 1798761600)
	lateUnapproved.ApprovalDigest, lateUnapproved.AttestationDigest = "", ""
	manifest, err = LockPayrollInputs(run, pop, cutoff, []SubmittedPayrollInput{lateUnapproved})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Excluded[0].Disposition != InputDispositionLate {
		t.Fatalf("late precedence = %+v", manifest.Excluded)
	}

	unapprovedUnattested := submittedInputAt("in-uu", "worker-a", InputCategoryTax, 1798800000, 1798761600)
	unapprovedUnattested.ApprovalDigest, unapprovedUnattested.AttestationDigest = "", ""
	manifest, err = LockPayrollInputs(run, pop, cutoff, []SubmittedPayrollInput{unapprovedUnattested})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Excluded[0].Disposition != InputDispositionUnapproved {
		t.Fatalf("approval precedence = %+v", manifest.Excluded)
	}

	// Exact-boundary instants are on time and fresh, never late or stale.
	edge := submittedInputAt("in-edge", "worker-a", InputCategoryEarnings, 1798848000, 1798761600)
	manifest, err = LockPayrollInputs(run, pop, cutoff, []SubmittedPayrollInput{edge})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Included) != 1 {
		t.Fatalf("boundary input excluded: %+v", manifest.Excluded)
	}

	// Caller ordering never changes the locked digest.
	_, _, inputs := lockInputsFixture(t)
	first, err := LockPayrollInputs(run, pop, cutoff, inputs)
	if err != nil {
		t.Fatal(err)
	}
	reversed := append([]SubmittedPayrollInput(nil), inputs...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	second, err := LockPayrollInputs(run, pop, cutoff, reversed)
	if err != nil {
		t.Fatal(err)
	}
	if first.ManifestDigest != second.ManifestDigest {
		t.Fatal("input ordering changed the locked digest")
	}
}

// TestTodo_PAYRUN_003_Golden pins byte-identical manifest evidence for the
// same run, population, cutoff, and submissions.
func TestTodo_PAYRUN_003_Golden(t *testing.T) {
	run := validPayrollRun(t)
	pop, cutoff, inputs := lockInputsFixture(t)
	first, err := LockPayrollInputs(run, pop, cutoff, inputs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LockPayrollInputs(run, pop, cutoff, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if first.ManifestDigest != second.ManifestDigest || !bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("identical input collections did not produce identical manifest evidence")
	}
	digest, err := second.Digest()
	if err != nil || digest != second.ManifestDigest {
		t.Fatalf("Digest() = %q, err = %v", digest, err)
	}
}

// TestTodo_PAYRUN_003_Race proves concurrent locks over shared submissions do
// not mutate caller state and concurrent manifest readers observe one value.
func TestTodo_PAYRUN_003_Race(t *testing.T) {
	run := validPayrollRun(t)
	pop, cutoff, inputs := lockInputsFixture(t)
	before := inputs[0].SourceDigest
	var wg sync.WaitGroup
	digests := make([]string, 8)
	errs := make([]error, 8)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			manifest, err := LockPayrollInputs(run, pop, cutoff, inputs)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = manifest.ManifestDigest
			_ = manifest.Validate()
			_, _ = manifest.Digest()
			_ = manifest.Canonical()
			_, _ = manifest.Explain()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if digests[i] != digests[0] {
			t.Fatal("concurrent locks diverged")
		}
	}
	if inputs[0].SourceDigest != before {
		t.Fatal("lock mutated caller submissions")
	}
}

// TestTodo_PAYRUN_003_Integration proves the locked inputs sit on the frozen
// population path that a later calculation accepts.
func TestTodo_PAYRUN_003_Integration(t *testing.T) {
	run := validPayrollRun(t)
	pop, cutoff, inputs := lockInputsFixture(t)
	manifest, err := LockPayrollInputs(run, pop, cutoff, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.PopulationDigest != pop.Digest {
		t.Fatal("manifest is not bound to the frozen population under test")
	}
	calculated, err := CalculateAgainstPopulation(run, pop, manifest.ManifestDigest)
	if err != nil {
		t.Fatalf("CalculateAgainstPopulation: %v", err)
	}
	if calculated.State != Calculated {
		t.Fatalf("state = %s", calculated.State)
	}
}

// TestTodo_PAYRUN_003_Fault proves every unsafe lock context is refused with
// the typed PAYRUN-003 rejection and yields no manifest.
func TestTodo_PAYRUN_003_Fault(t *testing.T) {
	run := validPayrollRun(t)
	pop, cutoff, inputs := lockInputsFixture(t)
	calculated, err := CalculateAgainstPopulation(run, pop, "sha256:calculation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockPayrollInputs(calculated, pop, cutoff, inputs); !errors.Is(err, ErrInputLockRejected) {
		t.Fatalf("post-calculation lock error = %v", err)
	}
	amendment, err := NewPopulationAmendment(PopulationAmendmentLateEntry,
		PopulationMember{WorkerRef: "worker-c", EmploymentRef: "employment-c", PayGroupRef: "monthly"},
		"hire effective after cutoff", payrollAsOf(t, 1798848000))
	if err != nil {
		t.Fatal(err)
	}
	next, err := pop.Amend(amendment)
	if err != nil {
		t.Fatal(err)
	}
	superseded, err := pop.SupersededRevision(next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockPayrollInputs(run, superseded, cutoff, inputs); !errors.Is(err, ErrPopulationSuperseded) {
		t.Fatalf("superseded population error = %v", err)
	}
	ghost := append(append([]SubmittedPayrollInput(nil), inputs...),
		submittedInputAt("in-ghost", "worker-ghost", InputCategoryEarnings, 1798800000, 1798761600))
	if _, err := LockPayrollInputs(run, pop, cutoff, ghost); !errors.Is(err, ErrInputLockRejected) {
		t.Fatalf("unknown worker error = %v", err)
	}
	duplicated := append(append([]SubmittedPayrollInput(nil), inputs...), inputs[0])
	if _, err := LockPayrollInputs(run, pop, cutoff, duplicated); !errors.Is(err, ErrInputLockRejected) {
		t.Fatalf("duplicate input error = %v", err)
	}
	unknown := append(append([]SubmittedPayrollInput(nil), inputs[:1]...), inputs[1:]...)
	unknown[0].Category = "BONUS"
	if _, err := LockPayrollInputs(run, pop, cutoff, unknown); !errors.Is(err, ErrInputLockRejected) {
		t.Fatalf("unknown category error = %v", err)
	}
	if _, err := NewInputCutoff(payrollAsOf(t, 1798848000), values.Instant{}); !errors.Is(err, ErrInputLockRejected) {
		t.Fatalf("invalid cutoff error = %v", err)
	}
	var refusal *InputLockError
	if _, err := LockPayrollInputs(calculated, pop, cutoff, inputs); !errors.As(err, &refusal) || refusal.Field == "" || refusal.Reason == "" {
		t.Fatalf("refusal = %+v", err)
	}
}

// TestTodo_PAYRUN_003_Mutation proves the manifest digest binds every locked
// line and category: any change yields a different digest and a tampered
// manifest fails validation.
func TestTodo_PAYRUN_003_Mutation(t *testing.T) {
	run := validPayrollRun(t)
	pop, cutoff, inputs := lockInputsFixture(t)
	base, err := LockPayrollInputs(run, pop, cutoff, inputs)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]SubmittedPayrollInput(nil), inputs...)
	changed[0].SourceDigest = "sha256:tampered"
	mutated, err := LockPayrollInputs(run, pop, cutoff, changed)
	if err != nil {
		t.Fatal(err)
	}
	if mutated.ManifestDigest == base.ManifestDigest {
		t.Fatal("changed source digest did not change the manifest digest")
	}
	dropped := append([]SubmittedPayrollInput(nil), inputs[1:]...)
	narrower, err := LockPayrollInputs(run, pop, cutoff, dropped)
	if err != nil {
		t.Fatal(err)
	}
	if narrower.ManifestDigest == base.ManifestDigest {
		t.Fatal("dropped category line did not change the manifest digest")
	}
	tampered := base
	tampered.Included[0].SourceDigest = "sha256:tampered"
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered manifest validated")
	}
}

// TestTodo_PAYRUN_003_Refusals pins the typed refusal for every malformed
// lock context, submission field, and manifest shape.
func TestTodo_PAYRUN_003_Refusals(t *testing.T) {
	run := validPayrollRun(t)
	pop, cutoff, inputs := lockInputsFixture(t)

	refusal := inputLockRefusal("input_id", "input id is required", ErrInvalidPayrollInput)
	var lockErr *InputLockError
	if !errors.As(refusal, &lockErr) || lockErr.Field != "input_id" || lockErr.Reason == "" {
		t.Fatalf("refusal = %+v", refusal)
	}
	if !errors.Is(refusal, ErrInputLockRejected) || !errors.Is(refusal, ErrInvalidPayrollInput) {
		t.Fatalf("refusal identity = %v", refusal)
	}
	if refusal.Error() == "" || lockErr.Unwrap() != ErrInvalidPayrollInput {
		t.Fatal("refusal text or cause is missing")
	}
	bare := inputLockRefusal("run", "run is invalid", nil)
	if !errors.Is(bare, ErrInputLockRejected) || bare.(*InputLockError).Unwrap() != nil {
		t.Fatalf("bare refusal = %v", bare)
	}

	if InputCategory("BONUS").Valid() || InputDisposition("PENDING").Valid() {
		t.Fatal("unknown category or disposition validated")
	}
	if _, err := NewInputCutoff(values.Instant{}, payrollAsOf(t, 1798761600)); !errors.Is(err, ErrInputLockRejected) {
		t.Fatalf("zero cutoff error = %v", err)
	}
	if _, err := NewInputCutoff(payrollAsOf(t, 1798848000), values.Instant{}); !errors.Is(err, ErrInputLockRejected) {
		t.Fatalf("zero watermark error = %v", err)
	}

	if _, err := LockPayrollInputs(PayrollRun{}, pop, cutoff, inputs); !errors.Is(err, ErrInputLockRejected) {
		t.Fatalf("invalid run error = %v", err)
	}
	if _, err := LockPayrollInputs(run, FrozenPopulation{}, cutoff, inputs); !errors.Is(err, ErrPopulationUnfrozen) {
		t.Fatalf("unfrozen population error = %v", err)
	}
	otherRun, err := NewPayrollRun("run-other", "monthly",
		PeriodRef{ID: "period-2026-11-b", Version: "v1", Digest: "sha256:period"},
		PopulationBindingRef{DefinitionID: "population-2026-11", RevisionVersion: "v3", Digest: "sha256:population"},
		"sha256:inputs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockPayrollInputs(otherRun, pop, cutoff, inputs); !errors.Is(err, ErrPopulationBindingMismatch) {
		t.Fatalf("binding mismatch error = %v", err)
	}
	broken := pop
	broken.Digest = "sha256:forged"
	if _, err := LockPayrollInputs(run, broken, cutoff, inputs); err == nil {
		t.Fatal("forged population locked")
	}
	if _, err := LockPayrollInputs(run, pop, InputCutoff{}, inputs); !errors.Is(err, ErrInputLockRejected) {
		t.Fatalf("invalid cutoff error = %v", err)
	}
	fieldCases := []struct {
		name   string
		mutate func(*SubmittedPayrollInput)
	}{
		{"input_id", func(in *SubmittedPayrollInput) { in.InputID = "" }},
		{"worker_ref", func(in *SubmittedPayrollInput) { in.WorkerRef = "" }},
		{"source_digest", func(in *SubmittedPayrollInput) { in.SourceDigest = "" }},
		{"observed_at", func(in *SubmittedPayrollInput) { in.ObservedAt = values.Instant{} }},
		{"source_watermark", func(in *SubmittedPayrollInput) { in.SourceWatermark = values.Instant{} }},
	}
	for _, tc := range fieldCases {
		t.Run(tc.name, func(t *testing.T) {
			bad := append([]SubmittedPayrollInput(nil), inputs...)
			tc.mutate(&bad[0])
			if _, err := LockPayrollInputs(run, pop, cutoff, bad); !errors.Is(err, ErrInputLockRejected) {
				t.Fatalf("%s error = %v", tc.name, err)
			}
		})
	}

	var zero LockedInputManifest
	if err := zero.Validate(); err == nil || zero.Canonical() != nil {
		t.Fatal("zero manifest validated")
	}
	if _, err := zero.Digest(); err == nil {
		t.Fatal("zero manifest digested")
	}
	if _, err := zero.Explain(); err == nil {
		t.Fatal("zero manifest explained")
	}

	manifest, err := LockPayrollInputs(run, pop, cutoff, inputs)
	if err != nil {
		t.Fatal(err)
	}
	shapeCases := []struct {
		name   string
		mutate func(*LockedInputManifest)
	}{
		{"run", func(m *LockedInputManifest) { m.RunID = "" }},
		{"population", func(m *LockedInputManifest) { m.PopulationDigest = "" }},
		{"cutoff", func(m *LockedInputManifest) { m.Cutoff = values.Instant{} }},
		{"watermark", func(m *LockedInputManifest) { m.MinWatermark = values.Instant{} }},
		{"line", func(m *LockedInputManifest) { m.Included[0].WorkerRef = "" }},
		{"governance", func(m *LockedInputManifest) { m.Included[0].ApprovalDigest = "" }},
		{"duplicate", func(m *LockedInputManifest) { m.Included = append(m.Included, m.Included[0]) }},
		{"excluded", func(m *LockedInputManifest) { m.Excluded[0].Disposition = InputDispositionIncluded }},
		{"excluded_reason", func(m *LockedInputManifest) { m.Excluded[0].Reason = "" }},
	}
	for _, tc := range shapeCases {
		t.Run(tc.name, func(t *testing.T) {
			bad := manifest
			bad.Included = append([]LockedInputLine(nil), manifest.Included...)
			bad.Excluded = append([]ClassifiedInput(nil), manifest.Excluded...)
			tc.mutate(&bad)
			bad.ManifestDigest = bad.computedDigest()
			if err := bad.Validate(); err == nil {
				t.Fatalf("%s manifest validated", tc.name)
			}
		})
	}
	dupExcluded := manifest
	dupExcluded.Excluded = append(append([]ClassifiedInput(nil), manifest.Excluded...), manifest.Excluded[0])
	dupExcluded.ManifestDigest = dupExcluded.computedDigest()
	if err := dupExcluded.Validate(); err == nil {
		t.Fatal("duplicated exclusion validated")
	}

	empty, err := LockPayrollInputs(run, pop, cutoff, nil)
	if err != nil {
		t.Fatalf("empty lock: %v", err)
	}
	if len(empty.Included) != 0 || len(empty.Excluded) != 0 || empty.Canonical() == nil {
		t.Fatalf("empty manifest = %+v", empty)
	}
	if _, err := empty.Explain(); err != nil {
		t.Fatalf("empty Explain: %v", err)
	}

	// Adjacent lifecycle spellings stay usable from the lock path.
	draft, err := NewDraft("run-2026-11-b", "monthly",
		PeriodRef{ID: "period-2026-11-b", Version: "v1", Digest: "sha256:period"},
		PopulationBindingRef{DefinitionID: "population-2026-11", RevisionVersion: "v3", Digest: "sha256:population"},
		"sha256:inputs")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := draft.TransitionTo(PayrollRunStateCalculated, "sha256:calculation")
	if err != nil || moved.State != Calculated {
		t.Fatalf("TransitionTo = %+v, %v", moved, err)
	}
	if _, err := Explain(draft); err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if _, err := draft.Explain(); err != nil {
		t.Fatalf("method Explain: %v", err)
	}
	again, err := FreezePayrollPopulation(validPayrollRun(t), payrollAsOf(t, 1798761600), payrollMembers(), LateEntryPolicyExplicitAmendment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExplainPopulation(again); err != nil {
		t.Fatalf("ExplainPopulation: %v", err)
	}
}
