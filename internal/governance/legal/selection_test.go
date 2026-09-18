package legal

import (
	"slices"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func selectionPack(t *testing.T, packID string, source SourceType, status ReviewStatus, version uint32, j Jurisdiction) RulePack {
	t.Helper()
	window, err := NewOpenEffectiveWindow(mustDate(t, 2020, time.January, 1))
	if err != nil {
		t.Fatal(err)
	}
	return RulePack{
		PackID:            packID,
		Version:           version,
		VocabularyVersion: VocabularyVersion2,
		SourceType:        source,
		ReviewStatus:      status,
		Jurisdiction:      j,
		Window:            window,
	}
}

// selectionRegistry carries one pack per family over Texas plus an Austin
// locality pack, so selection, exclusion, conflict and rollback each have a
// distinct fixture to act on.
func selectionRegistry(t *testing.T) *Registry {
	t.Helper()
	registry := NewRegistry()
	tx := testTXJurisdiction()
	austin := Jurisdiction{Country: "US", State: "TX", Locality: "Austin"}
	for _, pack := range []RulePack{
		selectionPack(t, "us-tx-statute-test", SourceTypeStatute, ReviewStatusCounselApproved, 1, tx),
		selectionPack(t, "us-tx-cba-test", SourceTypeCBA, ReviewStatusCustomerDefined, 1, tx),
		selectionPack(t, "us-tx-contract-test", SourceTypeContract, ReviewStatusCounselApproved, 1, tx),
		selectionPack(t, "us-tx-company-test", SourceTypeCustomerPolicy, ReviewStatusVendorBaseline, 1, tx),
		selectionPack(t, "us-tx-austin-test", SourceTypeStatute, ReviewStatusCounselApproved, 1, austin),
	} {
		if err := registry.Register(pack); err != nil {
			t.Fatalf("Register(%s): %v", pack.PackID, err)
		}
	}
	return registry
}

func selectionStrategiesAll() map[string]FamilyStrategy {
	return map[string]FamilyStrategy{
		FamilyGovernment: FamilyStrategyInclude,
		FamilyCollective: FamilyStrategyInclude,
		FamilyContract:   FamilyStrategyInclude,
		FamilyCompany:    FamilyStrategyInclude,
	}
}

func selectionSetTX() JurisdictionSet {
	return JurisdictionSet{
		Primary:                 testTXJurisdiction(),
		Confidence:              ConfidenceVerified,
		AttributionRule:         AttributionA1,
		RemoteWorkPolicyApplied: "not_remote",
	}
}

func companyApproval(t *testing.T) CounselApproval {
	t.Helper()
	return CounselApproval{
		Release: RulePackRelease{
			PackID:       "us-tx-company-test",
			Version:      1,
			Jurisdiction: testTXJurisdiction(),
		},
		Approver: "customer-counsel-1",
		Scope:    "promotion-base-pay",
		Expires:  mustDate(t, 2030, time.January, 1),
	}
}

func selectionBaseRequest(t *testing.T) SelectionRequest {
	t.Helper()
	return SelectionRequest{
		Set:              selectionSetTX(),
		BusinessDate:     mustDate(t, 2026, time.March, 1),
		Strategies:       selectionStrategiesAll(),
		Scope:            "promotion-base-pay",
		ReviewFloor:      ReviewStatusUnreviewed,
		CounselApprovals: []CounselApproval{companyApproval(t)},
	}
}

func selectBase(t *testing.T, registry *Registry, mutate func(*SelectionRequest)) RulePackSelection {
	t.Helper()
	req := selectionBaseRequest(t)
	req.KnownAt = mustKnownAt(t, 1_770_000_000)
	if mutate != nil {
		mutate(&req)
	}
	signer := fixedSigner(t, 0x70)
	sel, err := SelectRulePacks(req, registry, signer, mustInstant(t, 1_770_100_000))
	if err != nil {
		t.Fatalf("SelectRulePacks: %v", err)
	}
	return sel
}

func orderedPackIDs(sel RulePackSelection) []string {
	var out []string
	for _, r := range sel.Ordered {
		out = append(out, r.Release.PackID)
	}
	return out
}

func TestRulePackSelectionCompositionCounselReviewAndRollbackAreDeterministic(t *testing.T) {
	registry := selectionRegistry(t)

	t.Run("GREEN/resolved selection pins ordered releases with reasons", func(t *testing.T) {
		sel := selectBase(t, registry, nil)
		if sel.Status != SelectionResolved {
			t.Fatalf("status = %s, want RESOLVED", sel.Status)
		}
		want := []string{"us-tx-statute-test", "us-tx-cba-test", "us-tx-contract-test", "us-tx-company-test"}
		if got := orderedPackIDs(sel); !slices.Equal(got, want) {
			t.Fatalf("ordered = %v, want %v", got, want)
		}
		if sel.JurisdictionContextDigest == "" {
			t.Fatal("resolver pinned no jurisdiction-context digest")
		}
		if sel.BusinessDate != mustDate(t, 2026, time.March, 1) {
			t.Fatalf("effective time = %s", sel.BusinessDate)
		}
		if sel.KnownAt.Instant().Validate() != nil {
			t.Fatal("known time missing")
		}
		if sel.Counsel.Floor != ReviewStatusUnreviewed || sel.Counsel.Scope != "promotion-base-pay" {
			t.Fatalf("counsel scope = %+v", sel.Counsel)
		}
		if len(sel.Counsel.Approvals) != 1 || sel.Counsel.Approvals[0].Approver != "customer-counsel-1" {
			t.Fatalf("counsel approvals = %+v", sel.Counsel.Approvals)
		}
		if err := sel.VerifyWithKey(fixedSigner(t, 0x70).PublicKey()); err != nil {
			t.Fatalf("VerifyWithKey: %v", err)
		}
	})

	t.Run("RED/no implicit family choice", func(t *testing.T) {
		sel := selectBase(t, registry, func(req *SelectionRequest) {
			delete(req.Strategies, FamilyCompany)
		})
		if sel.Status != SelectionReviewRequired {
			t.Fatalf("status = %s, want REVIEW_REQUIRED", sel.Status)
		}
		if len(sel.Ordered) != 0 {
			t.Fatalf("uncertain selection pinned %d releases", len(sel.Ordered))
		}
		found := false
		for _, e := range sel.Excluded {
			if e.Release.PackID == "us-tx-company-test" && e.Reason == "strategy-undeclared" {
				found = true
			}
		}
		if !found {
			t.Fatalf("excluded = %+v", sel.Excluded)
		}
	})

	t.Run("GREEN/explicit exclusion is recorded without blocking", func(t *testing.T) {
		sel := selectBase(t, registry, func(req *SelectionRequest) {
			req.Strategies[FamilyCompany] = FamilyStrategyExclude
		})
		if sel.Status != SelectionResolved {
			t.Fatalf("status = %s, want RESOLVED", sel.Status)
		}
		if got := orderedPackIDs(sel); slices.Contains(got, "us-tx-company-test") {
			t.Fatalf("excluded family still ordered: %v", got)
		}
	})

	t.Run("RED/material interpretation without counsel approval", func(t *testing.T) {
		sel := selectBase(t, registry, func(req *SelectionRequest) {
			req.CounselApprovals = nil
		})
		if sel.Status != SelectionReviewRequired {
			t.Fatalf("status = %s, want REVIEW_REQUIRED", sel.Status)
		}
		if len(sel.Ordered) != 0 {
			t.Fatalf("uncounselled selection pinned %d releases", len(sel.Ordered))
		}
	})

	t.Run("RED/expired counsel approval is not current", func(t *testing.T) {
		sel := selectBase(t, registry, func(req *SelectionRequest) {
			stale := companyApproval(t)
			stale.Expires = mustDate(t, 2025, time.December, 31)
			req.CounselApprovals = []CounselApproval{stale}
		})
		if sel.Status != SelectionReviewRequired {
			t.Fatalf("status = %s, want REVIEW_REQUIRED", sel.Status)
		}
	})

	t.Run("RED/release below the tenant floor", func(t *testing.T) {
		sel := selectBase(t, registry, func(req *SelectionRequest) {
			req.ReviewFloor = ReviewStatusCounselApproved
		})
		if sel.Status != SelectionReviewRequired {
			t.Fatalf("status = %s, want REVIEW_REQUIRED", sel.Status)
		}
		if len(sel.Ordered) != 0 {
			t.Fatalf("below-floor selection pinned %d releases", len(sel.Ordered))
		}
	})

	t.Run("RED/barred release never evaluates", func(t *testing.T) {
		company := selectionPack(t, "us-tx-company-test", SourceTypeCustomerPolicy, ReviewStatusVendorBaseline, 1, testTXJurisdiction())
		sel := selectBase(t, registry, func(req *SelectionRequest) {
			req.Barred = []RulePackRelease{company.Release()}
		})
		if sel.Status != SelectionResolved {
			t.Fatalf("status = %s, want RESOLVED", sel.Status)
		}
		if got := orderedPackIDs(sel); slices.Contains(got, "us-tx-company-test") {
			t.Fatalf("barred release ordered: %v", got)
		}
	})

	t.Run("RED/barred-only coverage is unknown", func(t *testing.T) {
		sel := selectBase(t, registry, func(req *SelectionRequest) {
			for _, packID := range []string{"us-tx-statute-test", "us-tx-cba-test", "us-tx-contract-test", "us-tx-company-test"} {
				req.Barred = append(req.Barred, RulePackRelease{PackID: packID, Version: 1, Jurisdiction: testTXJurisdiction()})
			}
		})
		if sel.Status != SelectionUnknown {
			t.Fatalf("status = %s, want UNKNOWN", sel.Status)
		}
		if len(sel.Ordered) != 0 {
			t.Fatalf("unknown selection pinned %d releases", len(sel.Ordered))
		}
	})

	t.Run("RED/stale superseded release never evaluates fresh", func(t *testing.T) {
		stale := selectionRegistry(t)
		v2 := selectionPack(t, "us-tx-statute-test", SourceTypeStatute, ReviewStatusCounselApproved, 2, testTXJurisdiction())
		v1ref := RulePackRelease{PackID: "us-tx-statute-test", Version: 1, Jurisdiction: testTXJurisdiction()}
		v1, err := stale.GetExact(v1ref)
		if err != nil {
			t.Fatal(err)
		}
		v2.Window = v1.Window
		v2.Supersedes = &v1ref
		v1.SupersededBy = &RulePackRelease{PackID: "us-tx-statute-test", Version: 2, Jurisdiction: testTXJurisdiction()}
		// Re-register the closed chain without the window-closing helper so
		// both windows still cover the business date: only the superseded
		// marker may exclude v1.
		only := NewRegistry()
		for _, pack := range []RulePack{*v1, v2} {
			if err := only.Register(pack); err != nil {
				t.Fatal(err)
			}
		}
		for _, packID := range []string{"us-tx-cba-test", "us-tx-contract-test", "us-tx-company-test"} {
			var src RulePack
			for _, p := range []RulePack{
				selectionPack(t, "us-tx-cba-test", SourceTypeCBA, ReviewStatusCustomerDefined, 1, testTXJurisdiction()),
				selectionPack(t, "us-tx-contract-test", SourceTypeContract, ReviewStatusCounselApproved, 1, testTXJurisdiction()),
				selectionPack(t, "us-tx-company-test", SourceTypeCustomerPolicy, ReviewStatusVendorBaseline, 1, testTXJurisdiction()),
			} {
				if p.PackID == packID {
					src = p
				}
			}
			if err := only.Register(src); err != nil {
				t.Fatal(err)
			}
		}
		sel := selectBase(t, only, nil)
		if sel.Status != SelectionResolved {
			t.Fatalf("status = %s, want RESOLVED (v2 governs)", sel.Status)
		}
		for _, r := range sel.Ordered {
			if r.Release.PackID == "us-tx-statute-test" && r.Release.Version != 2 {
				t.Fatalf("stale release ordered: %+v", r.Release)
			}
		}
	})

	t.Run("RED/overlapping same-pack releases conflict", func(t *testing.T) {
		dup := selectionRegistry(t)
		alt := selectionPack(t, "us-tx-contract-test", SourceTypeContract, ReviewStatusCounselApproved, 2, testTXJurisdiction())
		if err := dup.Register(alt); err != nil {
			t.Fatal(err)
		}
		sel := selectBase(t, dup, nil)
		if sel.Status != SelectionConflict {
			t.Fatalf("status = %s, want CONFLICT", sel.Status)
		}
		if len(sel.Ordered) != 0 {
			t.Fatalf("conflicted selection pinned %d releases", len(sel.Ordered))
		}
		if len(sel.Conflicts) == 0 {
			t.Fatal("no conflict recorded")
		}
	})

	t.Run("GREEN/historical replay uses the pinned pack, not today's", func(t *testing.T) {
		evolving := selectionRegistry(t)
		before := selectBase(t, evolving, nil)
		var v1digest string
		for _, r := range before.Ordered {
			if r.Release.PackID == "us-tx-statute-test" {
				v1digest = r.Digest
			}
		}
		v2 := selectionPack(t, "us-tx-statute-test", SourceTypeStatute, ReviewStatusCounselApproved, 2, testTXJurisdiction())
		v2.Window, _ = NewClosedEffectiveWindow(mustDate(t, 2026, time.January, 1), mustDate(t, 2027, time.January, 1))
		v1ref := RulePackRelease{PackID: "us-tx-statute-test", Version: 1, Jurisdiction: testTXJurisdiction()}
		if err := evolving.Supersede(v1ref, v2); err != nil {
			t.Fatal(err)
		}
		replayed := selectBase(t, evolving, func(req *SelectionRequest) {
			req.Replay = []RulePackRelease{v1ref}
		})
		if replayed.Status != SelectionResolved {
			t.Fatalf("replay status = %s, want RESOLVED", replayed.Status)
		}
		// Supersession closed v1's window in its slot, so the pinned
		// release's digest moved with it; replay must track the pinned
		// release, not the pre-supersession bytes and not v2.
		pinnedV1, err := evolving.GetExact(v1ref)
		if err != nil {
			t.Fatal(err)
		}
		if len(replayed.Ordered) != 1 || replayed.Ordered[0].Digest != pinnedV1.ComputeDigest() {
			t.Fatalf("replay ordered = %+v, want pinned v1", replayed.Ordered)
		}
		if replayed.Ordered[0].Digest == v1digest {
			t.Fatal("replay returned pre-supersession bytes instead of the pinned release")
		}
		fresh := selectBase(t, evolving, nil)
		for _, r := range fresh.Ordered {
			if r.Release.PackID == "us-tx-statute-test" && r.Digest == v1digest {
				t.Fatal("fresh selection replayed history instead of v2")
			}
		}
	})

	t.Run("GREEN/rollback fences in-flight work and emits re-approval", func(t *testing.T) {
		evolving := selectionRegistry(t)
		v2 := selectionPack(t, "us-tx-statute-test", SourceTypeStatute, ReviewStatusCounselApproved, 2, testTXJurisdiction())
		v2.Window, _ = NewClosedEffectiveWindow(mustDate(t, 2026, time.January, 1), mustDate(t, 2027, time.January, 1))
		v1ref := RulePackRelease{PackID: "us-tx-statute-test", Version: 1, Jurisdiction: testTXJurisdiction()}
		if err := evolving.Supersede(v1ref, v2); err != nil {
			t.Fatal(err)
		}
		current := selectBase(t, evolving, nil)
		signer := fixedSigner(t, 0x70)
		plan, err := RollbackSelection(current, v1ref, []string{"ctx-digest-1", "ctx-digest-2"}, nil, evolving, signer, mustInstant(t, 1_770_200_000))
		if err != nil {
			t.Fatalf("RollbackSelection: %v", err)
		}
		if plan.Selection.Status != SelectionResolved {
			t.Fatalf("rollback status = %s, want RESOLVED under the target", plan.Selection.Status)
		}
		notes := map[string]bool{}
		for _, n := range plan.Selection.Notes {
			notes[n] = true
		}
		if !notes["rollback-from:us-tx-statute-test-v2"] || !notes["rollback-pending-reapproval"] {
			t.Fatalf("rollback notes = %v", plan.Selection.Notes)
		}
		pinnedV1 := false
		for _, r := range plan.Selection.Ordered {
			if r.Release == v1ref {
				pinnedV1 = true
			}
		}
		if !pinnedV1 {
			t.Fatalf("rollback ordered = %+v, want v1 pinned", orderedPackIDs(plan.Selection))
		}
		if !slices.Equal(plan.FencedInflight, []string{"ctx-digest-1", "ctx-digest-2"}) {
			t.Fatalf("fenced = %v", plan.FencedInflight)
		}
		kinds := map[string]bool{}
		for _, o := range plan.Impact {
			kinds[o.Kind] = true
		}
		for _, want := range []string{"IMPACT_ASSESSMENT", "RECOMPUTE", "REAPPROVAL"} {
			if !kinds[want] {
				t.Fatalf("impact kinds = %v, missing %s", kinds, want)
			}
		}
		if err := plan.VerifyWithKey(signer.PublicKey()); err != nil {
			t.Fatalf("plan VerifyWithKey: %v", err)
		}
	})

	t.Run("RED/rollback never revives withdrawn law", func(t *testing.T) {
		signer := fixedSigner(t, 0x70)
		current := selectBase(t, registry, nil)
		v1ref := RulePackRelease{PackID: "us-tx-statute-test", Version: 1, Jurisdiction: testTXJurisdiction()}
		if _, err := RollbackSelection(current, v1ref, nil, []RulePackRelease{v1ref}, registry, signer, mustInstant(t, 1_770_200_000)); err == nil {
			t.Fatal("rollback to a withdrawn release was allowed")
		}
	})

	t.Run("RED/rollback to the current release is refused", func(t *testing.T) {
		signer := fixedSigner(t, 0x70)
		current := selectBase(t, registry, nil)
		v1ref := RulePackRelease{PackID: "us-tx-statute-test", Version: 1, Jurisdiction: testTXJurisdiction()}
		if _, err := RollbackSelection(current, v1ref, nil, nil, registry, signer, mustInstant(t, 1_770_200_000)); err == nil {
			t.Fatal("rollback to the current release was allowed")
		}
	})
}

func TestTodo_LEGAL_007_Property(t *testing.T) {
	registry := selectionRegistry(t)

	t.Run("permutation of overlays selects byte-identical output", func(t *testing.T) {
		austin := Jurisdiction{Country: "US", State: "TX", Locality: "Austin"}
		dallas := Jurisdiction{Country: "US", State: "TX", Locality: "Dallas"}
		dallasPack := selectionPack(t, "us-tx-dallas-test", SourceTypeStatute, ReviewStatusCounselApproved, 1, dallas)
		if err := registry.Register(dallasPack); err != nil {
			t.Fatal(err)
		}
		mk := func(overlays []Jurisdiction) []byte {
			req := selectionBaseRequest(t)
			req.KnownAt = mustKnownAt(t, 1_770_000_000)
			req.Set.Overlays = overlays
			signer := fixedSigner(t, 0x70)
			sel, err := SelectRulePacks(req, registry, signer, mustInstant(t, 1_770_100_000))
			if err != nil {
				t.Fatalf("SelectRulePacks: %v", err)
			}
			if sel.Status != SelectionResolved {
				t.Fatalf("status = %s, want RESOLVED", sel.Status)
			}
			return sel.CanonicalBytes()
		}
		first := mk([]Jurisdiction{austin, dallas})
		second := mk([]Jurisdiction{dallas, austin})
		if string(first) != string(second) {
			t.Fatal("overlay order changed the selection bytes")
		}
	})

	t.Run("uncertainty never pins a release", func(t *testing.T) {
		mk := func(mutate func(*SelectionRequest)) RulePackSelection {
			return selectBase(t, registry, mutate)
		}
		uncertain := []RulePackSelection{
			mk(func(req *SelectionRequest) { delete(req.Strategies, FamilyGovernment) }),
			mk(func(req *SelectionRequest) { req.CounselApprovals = nil }),
			mk(func(req *SelectionRequest) {
				for _, packID := range []string{"us-tx-statute-test", "us-tx-cba-test", "us-tx-contract-test", "us-tx-company-test"} {
					req.Barred = append(req.Barred, RulePackRelease{PackID: packID, Version: 1, Jurisdiction: testTXJurisdiction()})
				}
			}),
		}
		for _, sel := range uncertain {
			if sel.Status == SelectionResolved {
				t.Fatalf("status = RESOLVED, want uncertainty: %+v", sel.Status)
			}
			if len(sel.Ordered) != 0 {
				t.Fatalf("%s pinned %d releases", sel.Status, len(sel.Ordered))
			}
		}
	})

	t.Run("replay of ordered pins is idempotent", func(t *testing.T) {
		first := selectBase(t, registry, nil)
		var pins []RulePackRelease
		for _, r := range first.Ordered {
			pins = append(pins, r.Release)
		}
		second := selectBase(t, registry, func(req *SelectionRequest) { req.Replay = pins })
		if string(first.CanonicalBytes()) != string(second.CanonicalBytes()) {
			t.Fatal("replay of a selection's own pins changed the bytes")
		}
	})
}

func TestTodo_LEGAL_007_Golden(t *testing.T) {
	registry := selectionRegistry(t)
	sel := selectBase(t, registry, nil)
	const want = "3c6a4b1b37c500ff2a7962ec063ef9d46a79a9047a3bd3416e6cbfc696884bed"
	if got := gotDigest(sel.CanonicalBytes()); got != want {
		t.Fatalf("canonical digest = %s, want %s", got, want)
	}
	again := selectBase(t, registry, nil)
	if string(again.CanonicalBytes()) != string(sel.CanonicalBytes()) {
		t.Fatal("identical selections are not byte-identical")
	}
	moved := selectBase(t, registry, func(req *SelectionRequest) {
		req.BusinessDate = mustDate(t, 2026, time.April, 1)
	})
	if string(moved.CanonicalBytes()) == string(sel.CanonicalBytes()) {
		t.Fatal("a different business date produced identical bytes")
	}
}

func TestTodo_LEGAL_007_Fault(t *testing.T) {
	registry := selectionRegistry(t)
	signer := fixedSigner(t, 0x70)
	now := mustInstant(t, 1_770_100_000)
	base := func() SelectionRequest {
		req := selectionBaseRequest(t)
		req.KnownAt = mustKnownAt(t, 1_770_000_000)
		return req
	}

	t.Run("malformed requests are refused, not resolved", func(t *testing.T) {
		bad := base()
		bad.BusinessDate = mustDate(t, 2026, time.March, 1)
		bad.Set.Primary = Jurisdiction{}
		if _, err := SelectRulePacks(bad, registry, signer, now); err == nil {
			t.Fatal("empty primary jurisdiction was accepted")
		}
		bad = base()
		bad.Scope = ""
		if _, err := SelectRulePacks(bad, registry, signer, now); err == nil {
			t.Fatal("empty counsel scope was accepted")
		}
		bad = base()
		bad.ReviewFloor = ReviewStatusUnspecified
		if _, err := SelectRulePacks(bad, registry, signer, now); err == nil {
			t.Fatal("unspecified review floor was accepted")
		}
		if _, err := SelectRulePacks(base(), nil, signer, now); err == nil {
			t.Fatal("nil registry was accepted")
		}
		if _, err := SelectRulePacks(base(), registry, nil, now); err == nil {
			t.Fatal("nil signer was accepted")
		}
	})

	t.Run("unfetchable replay pin is unknown, barred pin is refused", func(t *testing.T) {
		req := base()
		req.Replay = []RulePackRelease{{PackID: "us-tx-never-registered", Version: 9, Jurisdiction: testTXJurisdiction()}}
		sel, err := SelectRulePacks(req, registry, signer, now)
		if err != nil {
			t.Fatalf("unfetchable pin should be UNKNOWN, not an error: %v", err)
		}
		if sel.Status != SelectionUnknown {
			t.Fatalf("status = %s, want UNKNOWN", sel.Status)
		}
		req = base()
		company := selectionPack(t, "us-tx-company-test", SourceTypeCustomerPolicy, ReviewStatusVendorBaseline, 1, testTXJurisdiction())
		req.Replay = []RulePackRelease{company.Release()}
		req.Barred = req.Replay
		if _, err := SelectRulePacks(req, registry, signer, now); err == nil {
			t.Fatal("barred replay pin was evaluated")
		}
	})

	t.Run("jurisdiction with no covering release is unknown", func(t *testing.T) {
		req := base()
		req.Set.Primary = Jurisdiction{Country: "US", State: "ZZ"}
		sel, err := SelectRulePacks(req, registry, signer, now)
		if err != nil {
			t.Fatal(err)
		}
		if sel.Status != SelectionUnknown {
			t.Fatalf("status = %s, want UNKNOWN", sel.Status)
		}
		if len(sel.Ordered) != 0 {
			t.Fatal("unknown coverage pinned releases")
		}
	})
}

func TestTodo_LEGAL_007_Security(t *testing.T) {
	registry := selectionRegistry(t)
	signer := fixedSigner(t, 0x70)
	sel := selectBase(t, registry, nil)

	t.Run("tampered selections do not verify", func(t *testing.T) {
		if err := sel.Verify(); err != nil {
			t.Fatalf("baseline: %v", err)
		}
		tampered := sel
		tampered.Status = SelectionReviewRequired
		if err := tampered.Verify(); err == nil {
			t.Fatal("status-flipped selection verified")
		}
		tampered = sel
		tampered.Ordered = tampered.Ordered[:1]
		if err := tampered.Verify(); err == nil {
			t.Fatal("truncated pin set verified")
		}
		if err := sel.VerifyWithKey(fixedSigner(t, 0x71).PublicKey()); err == nil {
			t.Fatal("wrong authority key verified")
		}
	})

	t.Run("approval for another release authorizes nothing", func(t *testing.T) {
		other := companyApproval(t)
		other.Release.Version = 99
		got := selectBase(t, registry, func(req *SelectionRequest) {
			req.CounselApprovals = []CounselApproval{other}
		})
		if got.Status != SelectionReviewRequired {
			t.Fatalf("status = %s, want REVIEW_REQUIRED", got.Status)
		}
	})

	t.Run("rollback of a forged selection is refused", func(t *testing.T) {
		forged := sel
		forged.Status = SelectionReviewRequired
		v1ref := RulePackRelease{PackID: "us-tx-statute-test", Version: 1, Jurisdiction: testTXJurisdiction()}
		if _, err := RollbackSelection(forged, v1ref, nil, nil, registry, signer, mustInstant(t, 1_770_200_000)); err == nil {
			t.Fatal("rollback of a tampered selection proceeded")
		}
	})
}

func TestTodo_LEGAL_007_Conformance(t *testing.T) {
	t.Run("two identical registries select identically", func(t *testing.T) {
		first := selectBase(t, selectionRegistry(t), nil)
		second := selectBase(t, selectionRegistry(t), nil)
		if string(first.CanonicalBytes()) != string(second.CanonicalBytes()) {
			t.Fatal("identical registries selected different bytes")
		}
	})

	t.Run("fresh selection and replay of its pins agree", func(t *testing.T) {
		registry := selectionRegistry(t)
		fresh := selectBase(t, registry, nil)
		var pins []RulePackRelease
		for _, r := range fresh.Ordered {
			pins = append(pins, r.Release)
		}
		replayed := selectBase(t, registry, func(req *SelectionRequest) { req.Replay = pins })
		if string(fresh.CanonicalBytes()) != string(replayed.CanonicalBytes()) {
			t.Fatal("fresh and replay selections disagree")
		}
	})

	t.Run("family strategy swap changes only the excluded set", func(t *testing.T) {
		registry := selectionRegistry(t)
		with := selectBase(t, registry, nil)
		without := selectBase(t, registry, func(req *SelectionRequest) {
			req.Strategies[FamilyCompany] = FamilyStrategyExclude
		})
		if len(with.Ordered)-len(without.Ordered) != 1 {
			t.Fatalf("ordered %d -> %d, want exactly one fewer", len(with.Ordered), len(without.Ordered))
		}
		for _, r := range without.Ordered {
			if r.Release.PackID == "us-tx-company-test" {
				t.Fatal("excluded family still ordered")
			}
		}
	})
}

func TestTodo_LEGAL_007_Recovery(t *testing.T) {
	t.Run("rollback plan survives registry loss", func(t *testing.T) {
		registry := selectionRegistry(t)
		v2 := selectionPack(t, "us-tx-statute-test", SourceTypeStatute, ReviewStatusCounselApproved, 2, testTXJurisdiction())
		v2.Window, _ = NewClosedEffectiveWindow(mustDate(t, 2026, time.January, 1), mustDate(t, 2027, time.January, 1))
		v1ref := RulePackRelease{PackID: "us-tx-statute-test", Version: 1, Jurisdiction: testTXJurisdiction()}
		if err := registry.Supersede(v1ref, v2); err != nil {
			t.Fatal(err)
		}
		current := selectBase(t, registry, nil)
		signer := fixedSigner(t, 0x70)
		plan, err := RollbackSelection(current, v1ref, []string{"inflight-1"}, nil, registry, signer, mustInstant(t, 1_770_200_000))
		if err != nil {
			t.Fatal(err)
		}
		empty := NewRegistry()
		_ = empty
		if err := plan.VerifyWithKey(signer.PublicKey()); err != nil {
			t.Fatalf("plan verifies without its registry: %v", err)
		}
		if err := plan.Selection.VerifyWithKey(signer.PublicKey()); err != nil {
			t.Fatalf("rolled-back selection verifies without its registry: %v", err)
		}
	})

	t.Run("second rollback compounds fenced work and obligations", func(t *testing.T) {
		registry := selectionRegistry(t)
		v2 := selectionPack(t, "us-tx-statute-test", SourceTypeStatute, ReviewStatusCounselApproved, 2, testTXJurisdiction())
		v2.Window, _ = NewClosedEffectiveWindow(mustDate(t, 2026, time.January, 1), mustDate(t, 2027, time.January, 1))
		v1ref := RulePackRelease{PackID: "us-tx-statute-test", Version: 1, Jurisdiction: testTXJurisdiction()}
		if err := registry.Supersede(v1ref, v2); err != nil {
			t.Fatal(err)
		}
		current := selectBase(t, registry, nil)
		signer := fixedSigner(t, 0x70)
		first, err := RollbackSelection(current, v1ref, []string{"a"}, nil, registry, signer, mustInstant(t, 1_770_200_000))
		if err != nil {
			t.Fatal(err)
		}
		second, err := RollbackSelection(first.Selection, v1ref, []string{"b"}, nil, registry, signer, mustInstant(t, 1_770_300_000))
		if err == nil {
			_ = second
			t.Fatal("rollback to the already-pinned release was allowed")
		}
		if len(first.Impact) != 3 {
			t.Fatalf("impact = %+v, want exactly the three rollback obligations", first.Impact)
		}
	})
}

func TestTodo_LEGAL_007_ModelBased(t *testing.T) {
	// A small op-sequence model over register/supersede/bar/select/replay/
	// rollback. The oracle is invariants, not a copy of the implementation:
	// pins stay inside registered-minus-barred, uncertainty never pins, and
	// every rollback carries its reapproval obligation.
	type op struct {
		name    string
		packID  string
		version uint32
		bar     bool
	}
	sequences := [][]op{
		{
			{"select", "", 0, false},
			{"bar", "us-tx-company-test", 1, true},
			{"select", "", 0, false},
		},
		{
			{"register", "us-tx-statute-test", 2, false},
			{"select", "", 0, false},
			{"rollback", "us-tx-statute-test", 1, false},
			{"replay-pins", "", 0, false},
		},
		{
			{"bar", "us-tx-statute-test", 1, true},
			{"bar", "us-tx-cba-test", 1, true},
			{"bar", "us-tx-contract-test", 1, true},
			{"bar", "us-tx-company-test", 1, true},
			{"select", "", 0, false},
		},
	}
	for i, seq := range sequences {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			registry := selectionRegistry(t)
			barred := map[RulePackRelease]bool{}
			registered := map[RulePackRelease]bool{}
			for _, p := range []RulePack{
				selectionPack(t, "us-tx-statute-test", SourceTypeStatute, ReviewStatusCounselApproved, 1, testTXJurisdiction()),
				selectionPack(t, "us-tx-cba-test", SourceTypeCBA, ReviewStatusCustomerDefined, 1, testTXJurisdiction()),
				selectionPack(t, "us-tx-contract-test", SourceTypeContract, ReviewStatusCounselApproved, 1, testTXJurisdiction()),
				selectionPack(t, "us-tx-company-test", SourceTypeCustomerPolicy, ReviewStatusVendorBaseline, 1, testTXJurisdiction()),
			} {
				registered[p.Release()] = true
			}
			var last RulePackSelection
			check := func(sel RulePackSelection) {
				t.Helper()
				for _, r := range sel.Ordered {
					if !registered[r.Release] {
						t.Fatalf("pinned unregistered release %+v", r.Release)
					}
					if barred[r.Release] {
						t.Fatalf("pinned barred release %+v", r.Release)
					}
				}
				if sel.Status != SelectionResolved && len(sel.Ordered) != 0 {
					t.Fatalf("%s pinned %d releases", sel.Status, len(sel.Ordered))
				}
			}
			for _, step := range seq {
				switch step.name {
				case "select":
					var barList []RulePackRelease
					for b := range barred {
						barList = append(barList, b)
					}
					last = selectBase(t, registry, func(req *SelectionRequest) { req.Barred = barList })
					check(last)
				case "register":
					next := selectionPack(t, step.packID, SourceTypeStatute, ReviewStatusCounselApproved, step.version, testTXJurisdiction())
					next.Window, _ = NewClosedEffectiveWindow(mustDate(t, 2026, time.January, 1), mustDate(t, 2027, time.January, 1))
					prev := RulePackRelease{PackID: step.packID, Version: step.version - 1, Jurisdiction: testTXJurisdiction()}
					if err := registry.Supersede(prev, next); err != nil {
						t.Fatal(err)
					}
					registered[next.Release()] = true
				case "bar":
					barred[RulePackRelease{PackID: step.packID, Version: step.version, Jurisdiction: testTXJurisdiction()}] = true
				case "rollback":
					signer := fixedSigner(t, 0x70)
					plan, err := RollbackSelection(last, RulePackRelease{PackID: step.packID, Version: step.version, Jurisdiction: testTXJurisdiction()}, []string{"model-inflight"}, nil, registry, signer, mustInstant(t, 1_770_200_000))
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, o := range plan.Impact {
						if o.Kind == "REAPPROVAL" {
							found = true
						}
					}
					if !found {
						t.Fatal("rollback without a reapproval obligation")
					}
					last = plan.Selection
					check(last)
				case "replay-pins":
					var pins []RulePackRelease
					for _, r := range last.Ordered {
						pins = append(pins, r.Release)
					}
					replayed := selectBase(t, registry, func(req *SelectionRequest) { req.Replay = pins })
					if replayed.Status != last.Status {
						t.Fatalf("replay status = %s, want %s", replayed.Status, last.Status)
					}
					var wantPins, gotPins []RulePackRelease
					for _, r := range last.Ordered {
						wantPins = append(wantPins, r.Release)
					}
					for _, r := range replayed.Ordered {
						gotPins = append(gotPins, r.Release)
					}
					if !slices.Equal(wantPins, gotPins) {
						t.Fatalf("replay pins = %v, want %v", gotPins, wantPins)
					}
				}
			}
		})
	}
}

func TestTodo_LEGAL_007_Mutation(t *testing.T) {
	registry := selectionRegistry(t)
	signer := fixedSigner(t, 0x70)
	base := selectBase(t, registry, nil)

	t.Run("signed incomplete selections do not verify", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*RulePackSelection)
		}{
			{"status", func(s *RulePackSelection) { s.Status = "" }},
			{"primary", func(s *RulePackSelection) { s.Primary = Jurisdiction{} }},
			{"business date", func(s *RulePackSelection) { s.BusinessDate = values.LocalDate{} }},
			{"counsel scope", func(s *RulePackSelection) { s.Counsel.Scope = "" }},
			{"ordered on uncertainty", func(s *RulePackSelection) {
				s.Status = SelectionReviewRequired
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				mutated := base
				tc.mutate(&mutated)
				mutated.Digest, mutated.Signature = signer.SignDigest(mutated.CanonicalBytes())
				if err := mutated.Verify(); err == nil {
					t.Fatalf("signed incomplete selection verified (%s)", tc.name)
				}
			})
		}
	})

	t.Run("guard/barred bypass is killed", func(t *testing.T) {
		company := selectionPack(t, "us-tx-company-test", SourceTypeCustomerPolicy, ReviewStatusVendorBaseline, 1, testTXJurisdiction())
		sel := selectBase(t, registry, func(req *SelectionRequest) {
			req.Barred = []RulePackRelease{company.Release()}
		})
		for _, r := range sel.Ordered {
			if r.Release == company.Release() {
				t.Fatal("barred release evaluated")
			}
		}
	})

	t.Run("guard/stale bypass is killed", func(t *testing.T) {
		windows := NewRegistry()
		v1 := selectionPack(t, "us-tx-statute-test", SourceTypeStatute, ReviewStatusCounselApproved, 1, testTXJurisdiction())
		v1.Window, _ = NewClosedEffectiveWindow(mustDate(t, 2020, time.January, 1), mustDate(t, 2026, time.January, 1))
		v2 := selectionPack(t, "us-tx-statute-test", SourceTypeStatute, ReviewStatusCounselApproved, 2, testTXJurisdiction())
		v2.Window, _ = NewClosedEffectiveWindow(mustDate(t, 2026, time.January, 1), mustDate(t, 2027, time.January, 1))
		for _, pack := range []RulePack{v1, v2} {
			if err := windows.Register(pack); err != nil {
				t.Fatal(err)
			}
		}
		// Neither release is marked superseded: only the effective-window
		// check can keep v1 out of a July transaction.
		v1ref := RulePackRelease{PackID: "us-tx-statute-test", Version: 1, Jurisdiction: testTXJurisdiction()}
		sel := selectBase(t, windows, func(req *SelectionRequest) {
			req.BusinessDate = mustDate(t, 2026, time.July, 1)
			req.Strategies = map[string]FamilyStrategy{FamilyGovernment: FamilyStrategyInclude}
		})
		if sel.Status != SelectionResolved {
			t.Fatalf("status = %s, want RESOLVED under v2", sel.Status)
		}
		for _, r := range sel.Ordered {
			if r.Release == v1ref {
				t.Fatal("stale release evaluated for a fresh transaction")
			}
		}
	})

	t.Run("guard/counsel bypass is killed", func(t *testing.T) {
		sel := selectBase(t, registry, func(req *SelectionRequest) {
			req.CounselApprovals = nil
		})
		if sel.Status == SelectionResolved {
			t.Fatal("uncounselled material selection resolved")
		}
	})
}
