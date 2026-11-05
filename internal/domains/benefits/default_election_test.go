// TestTodo_WF_CAP_013 proves benefits.apply_default_election applies a
// plan's declared default with evidence of the missed window.
package benefits

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func wfCap013Costs(t *testing.T) (values.Decimal, values.Decimal) {
	t.Helper()
	return values.MustDecimal("240.00", 2, values.RoundingHalfUp),
		values.MustDecimal("760.00", 2, values.RoundingHalfUp)
}

func wfCap013Window() MissedWindow {
	return MissedWindow{
		Tenant: "acme", WorkerRef: "worker-1", WindowID: "oe-2027",
		Kind: WindowOpenEnrollment, Status: WindowClosed,
		ClosedAt:      time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
		EffectiveDate: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func wfCap013Prior(t *testing.T) *ElectionRevision {
	t.Helper()
	employee, employer := wfCap013Costs(t)
	head, err := RecordElection(nil, ElectionInput{
		Tenant: "acme", WorkerRef: "worker-1", PlanRef: "plan-hd-2026",
		PlanRevision: "rev-3", RateVersion: "rates-2026.01", WindowID: "oe-2026",
		Tier: TierFamily, Dependents: []string{"dep-child-1", "dep-spouse"},
		EffectiveDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EmployeeCost:  employee, EmployerCost: employer,
	})
	if err != nil {
		t.Fatalf("RecordElection prior: %v", err)
	}
	return &head
}

func TestTodo_WF_CAP_013(t *testing.T) {
	t.Run("rollover_carries_the_prior_election_forward", func(t *testing.T) {
		prior := wfCap013Prior(t)
		policy := DefaultElectionPolicy{PlanRef: "plan-hd-2026", PlanRevision: "rev-3", Kind: DefaultRollover}
		res, err := ApplyDefaultElection(prior, policy, wfCap013Window())
		if err != nil {
			t.Fatalf("ApplyDefaultElection rollover: %v", err)
		}
		rev := res.Revision
		if rev.Tier != TierFamily || rev.Waiver {
			t.Fatalf("rollover changed tier or waived: %+v", rev)
		}
		if len(rev.Dependents) != 2 || rev.Dependents[0] != "dep-child-1" || rev.Dependents[1] != "dep-spouse" {
			t.Fatalf("rollover dropped dependents: %+v", rev.Dependents)
		}
		employee, employer := wfCap013Costs(t)
		if rev.EmployeeCost.String() != employee.String() || rev.EmployerCost.String() != employer.String() {
			t.Fatalf("rollover changed costs: %s/%s", rev.EmployeeCost, rev.EmployerCost)
		}
		if rev.WindowID != "oe-2027" || rev.Revision != 2 || rev.PriorDigest != prior.CanonicalDigest || rev.Supersedes != prior.CanonicalDigest {
			t.Fatalf("rollover broke the chain: %+v", rev)
		}
		if res.Kind != DefaultRollover || res.PolicyDigest == "" || res.Evidence.WindowID != "oe-2027" {
			t.Fatalf("rollover result lost its provenance: %+v", res)
		}
		if err := rev.Verify(); err != nil {
			t.Fatalf("rollover revision does not verify: %v", err)
		}
	})

	t.Run("waive_records_a_waiver_citing_the_missed_window", func(t *testing.T) {
		policy := DefaultElectionPolicy{PlanRef: "plan-hd-2026", PlanRevision: "rev-3", Kind: DefaultWaive}
		res, err := ApplyDefaultElection(nil, policy, wfCap013Window())
		if err != nil {
			t.Fatalf("ApplyDefaultElection waive: %v", err)
		}
		rev := res.Revision
		if !rev.Waiver || rev.WaiverEvidence == "" {
			t.Fatalf("waive recorded no waiver evidence: %+v", rev)
		}
		if rev.WaiverEvidence != wfCap013Window().EvidenceRef() {
			t.Fatalf("waiver evidence does not cite the missed window: %q", rev.WaiverEvidence)
		}
		if rev.Revision != 1 || rev.WindowID != "oe-2027" {
			t.Fatalf("waive chained wrongly: %+v", rev)
		}
		if err := rev.Verify(); err != nil {
			t.Fatalf("waive revision does not verify: %v", err)
		}
	})

	t.Run("core_coverage_enrolls_the_declared_core_tier", func(t *testing.T) {
		employee, employer := wfCap013Costs(t)
		policy := DefaultElectionPolicy{
			PlanRef: "plan-hd-2026", PlanRevision: "rev-3", Kind: DefaultCoreCoverage,
			CoreTier: TierEmployeeOnly, EmployeeCost: employee, EmployerCost: employer, RateVersion: "rates-2027.01",
		}
		res, err := ApplyDefaultElection(nil, policy, wfCap013Window())
		if err != nil {
			t.Fatalf("ApplyDefaultElection core: %v", err)
		}
		rev := res.Revision
		if rev.Waiver || rev.Tier != TierEmployeeOnly || rev.RateVersion != "rates-2027.01" {
			t.Fatalf("core coverage applied wrongly: %+v", rev)
		}
		if res.Kind != DefaultCoreCoverage || res.PolicyDigest != policy.Digest() {
			t.Fatalf("core result lost its provenance: %+v", res)
		}
		if err := rev.Verify(); err != nil {
			t.Fatalf("core revision does not verify: %v", err)
		}
	})

	t.Run("responders_and_open_windows_are_refused", func(t *testing.T) {
		policy := DefaultElectionPolicy{PlanRef: "plan-hd-2026", PlanRevision: "rev-3", Kind: DefaultWaive}
		active, err := RecordElection(nil, ben005Input())
		if err != nil {
			t.Fatalf("RecordElection active: %v", err)
		}
		sameWindow := wfCap013Window()
		sameWindow.WindowID = "oe-2026"
		if _, err := ApplyDefaultElection(&active, policy, sameWindow); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("a worker who elected must not receive a default, got %v", err)
		}
		open := wfCap013Window()
		open.Status = WindowOpen
		if _, err := ApplyDefaultElection(nil, policy, open); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("an open window must not trigger a default, got %v", err)
		}
		rollover := DefaultElectionPolicy{PlanRef: "plan-hd-2026", PlanRevision: "rev-3", Kind: DefaultRollover}
		if _, err := ApplyDefaultElection(nil, rollover, wfCap013Window()); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("rollover with no prior election must fail, got %v", err)
		}
		foreign := wfCap013Prior(t)
		foreign.WindowID = "oe-2026"
		otherPlan := DefaultElectionPolicy{PlanRef: "plan-other", PlanRevision: "rev-9", Kind: DefaultRollover}
		if _, err := ApplyDefaultElection(foreign, otherPlan, wfCap013Window()); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("rollover across plans must fail, got %v", err)
		}
	})

	t.Run("life_event_windows_apply_defaults_too", func(t *testing.T) {
		policy := DefaultElectionPolicy{PlanRef: "plan-hd-2026", PlanRevision: "rev-3", Kind: DefaultWaive}
		window := wfCap013Window()
		window.Kind = WindowLifeEvent
		window.WindowID = "le-birth-1"
		res, err := ApplyDefaultElection(nil, policy, window)
		if err != nil {
			t.Fatalf("ApplyDefaultElection life event: %v", err)
		}
		if res.Revision.WindowID != "le-birth-1" || !res.Revision.Waiver {
			t.Fatalf("life-event default applied wrongly: %+v", res.Revision)
		}
	})

	t.Run("policies_and_windows_validate", func(t *testing.T) {
		if err := (DefaultElectionPolicy{}).Validate(); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("empty policy must fail, got %v", err)
		}
		core := DefaultElectionPolicy{PlanRef: "p", PlanRevision: "r", Kind: DefaultCoreCoverage}
		if err := core.Validate(); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("core without tier and costs must fail, got %v", err)
		}
		core.CoreTier = "PETS_ONLY"
		if err := core.Validate(); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("core with undeclared tier must fail, got %v", err)
		}
		if err := (MissedWindow{}).Validate(); !errors.Is(err, ErrElectionRejected) {
			t.Fatalf("empty window must fail, got %v", err)
		}
		w := wfCap013Window()
		a, b := w.Digest(), wfCap013Window().Digest()
		if a == "" || a != b {
			t.Fatal("identical missed windows must digest identically")
		}
	})
}

// TestTodo_WF_CAP_013_Golden pins the exact default-election output for one
// waived non-responder: revision seal, waiver citation and policy version.
func TestTodo_WF_CAP_013_Golden(t *testing.T) {
	policy := DefaultElectionPolicy{PlanRef: "plan-hd-2026", PlanRevision: "rev-3", Kind: DefaultWaive}
	res, err := ApplyDefaultElection(nil, policy, wfCap013Window())
	if err != nil {
		t.Fatalf("ApplyDefaultElection: %v", err)
	}
	if res.Revision.CanonicalDigest != "sha256:ab195055a00e527c243538042475bc1ade42b2f2a3a7131dd81cc212ecf9a106" {
		t.Fatalf("default election seal changed: %s", res.Revision.CanonicalDigest)
	}
	if res.Revision.WaiverEvidence != "missed-window:oe-2027:2026-11-15T00:00:00Z" {
		t.Fatalf("waiver citation changed: %q", res.Revision.WaiverEvidence)
	}
	if res.PolicyDigest != policy.Digest() || res.PolicyDigest == "" {
		t.Fatal("result lost its policy version")
	}
}
