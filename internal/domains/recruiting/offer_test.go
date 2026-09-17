package recruiting

// RECRUIT-003 RED: immutable OfferRevisions — create, approve, sign and
// accept with exact approval/signature/candidacy binding. These tests are
// written before the production code in offer.go and must fail until it
// lands.

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func offerTerms(t *testing.T) OfferTerms {
	t.Helper()
	return OfferTerms{
		PositionRef: "position:registered-nurse/n4",
		Salary:      values.MustDecimal("120000", 2, values.RoundingHalfUp),
		StartDate:   recruitInstant(t, 30),
		ExpiresAt:   recruitInstant(t, 20),
	}
}

// offerCandidacy advances a real ATS aggregate to the OFFER stage and
// returns the candidacy binding an offer must carry.
func offerCandidacy(t *testing.T, agg *Aggregate) (candidacyID, candidateID string, rev uint64) {
	t.Helper()
	*agg = submitRecruiting(t, *agg, "app-1", "candidate-1")
	if err := agg.CreateCandidacy("cand-1", "app-1", "candidate-1", "consent:1", "hiring", "source:ats", recruitInstant(t, 5), recruitKnown(t, 5)); err != nil {
		t.Fatal(err)
	}
	rev = 1
	for i, stage := range []CandidacyStage{StageScreening, StageInterview, StageOffer} {
		if err := agg.TransitionCandidacy("cand-1", rev, stage, recruitInstant(t, 6+i), recruitKnown(t, 6+i)); err != nil {
			t.Fatal(err)
		}
		rev++
	}
	return "cand-1", "candidate-1", rev
}

func createOfferFixture(t *testing.T, ledger *OfferLedger, offerID string, agg *Aggregate) (candidacyID, candidateID string, candidacyRev uint64) {
	t.Helper()
	candidacyID, candidateID, candidacyRev = offerCandidacy(t, agg)
	cmd := CreateOfferCmd{
		OfferID: offerID, CandidacyID: candidacyID, CandidateID: candidateID,
		CandidacyRevision: candidacyRev, Terms: offerTerms(t),
		EffectiveAt: recruitInstant(t, 10), KnownAt: recruitKnown(t, 10),
	}
	if _, err := ledger.CreateOffer(cmd); err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	return candidacyID, candidateID, candidacyRev
}

// approvedSignedOffer drives one offer to SIGNED from whatever its current
// revision is, signing exactly the current canonical digest.
func approvedSignedOffer(t *testing.T, ledger *OfferLedger, offerID string) OfferRevision {
	t.Helper()
	current, ok := ledger.Offer(offerID)
	if !ok {
		t.Fatal("offer missing before approval")
	}
	if err := ledger.ApproveOffer(offerID, current.Revision, "hiring-manager-1", "authority:hire-decision", recruitInstant(t, 11), recruitKnown(t, 11)); err != nil {
		t.Fatalf("ApproveOffer: %v", err)
	}
	current, ok = ledger.Offer(offerID)
	if !ok {
		t.Fatal("offer missing after approval")
	}
	if err := ledger.SignOffer(offerID, current.Revision, "candidate-1", "signature:candidate-acceptance", current.CanonicalDigest, recruitInstant(t, 12), recruitKnown(t, 12)); err != nil {
		t.Fatalf("SignOffer: %v", err)
	}
	signed, ok := ledger.Offer(offerID)
	if !ok || signed.Status != OfferSigned {
		t.Fatalf("signed offer = %+v ok=%v", signed, ok)
	}
	return signed
}

func offerCounts(ledger *OfferLedger) (events, hires int) {
	return len(ledger.EventKinds()), len(ledger.HireIntents())
}

// TestOfferRevisionAcceptanceRequiresExactApprovalSignatureAndCurrentCandidacy
// is the RECRUIT-003 PRIMARY: acceptance CAS binds the current
// OfferRevision, proposal/approval/signature digests, candidate/candidacy
// and expiry; exactly one OfferAccepted event emits exactly one
// deterministic Hire child intent while worker creation stays separately
// governed.
func TestOfferRevisionAcceptanceRequiresExactApprovalSignatureAndCurrentCandidacy(t *testing.T) {
	agg := openRecruiting(t)
	ledger := NewOfferLedger()
	candidacyID, candidateID, candidacyRev := createOfferFixture(t, ledger, "offer-1", &agg)

	// Terms stay mutable while DRAFT.
	better := offerTerms(t)
	better.Salary = values.MustDecimal("125000", 2, values.RoundingHalfUp)
	if err := ledger.UpdateOfferTerms("offer-1", 1, better, recruitInstant(t, 10), recruitKnown(t, 10)); err != nil {
		t.Fatalf("UpdateOfferTerms on DRAFT: %v", err)
	}
	signed := approvedSignedOffer(t, ledger, "offer-1")

	// Post-approval term changes are refused with zero effect.
	events, hires := offerCounts(ledger)
	for name, mutate := range map[string]func(*OfferTerms){
		"salary":   func(tm *OfferTerms) { tm.Salary = values.MustDecimal("999999", 2, values.RoundingHalfUp) },
		"position": func(tm *OfferTerms) { tm.PositionRef = "position:charge-nurse/n5" },
		"start":    func(tm *OfferTerms) { tm.StartDate = recruitInstant(t, 40) },
	} {
		changed := better
		mutate(&changed)
		err := ledger.UpdateOfferTerms("offer-1", signed.Revision, changed, recruitInstant(t, 13), recruitKnown(t, 13))
		if offerCodeOf(err) != CodeApprovalMismatch {
			t.Fatalf("%s change err = %v", name, err)
		}
		if !errors.Is(err, ErrOfferRefused) {
			t.Fatalf("%s change must match ErrOfferRefused: %v", name, err)
		}
	}
	if gotEvents, gotHires := offerCounts(ledger); gotEvents != events || gotHires != hires {
		t.Fatal("post-approval changes appended events/hires")
	}

	// Acceptance binds the current revision, digests, candidacy and expiry.
	hire, err := ledger.AcceptOffer(AcceptOfferCmd{
		OfferID: "offer-1", ExpectedRevision: signed.Revision,
		CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: candidateID,
		SignatureDigest: signed.SignatureDigest,
		AcceptedAt:      recruitInstant(t, 14), KnownAt: recruitKnown(t, 14),
	})
	if err != nil {
		t.Fatalf("AcceptOffer: %v", err)
	}
	if hire.OfferID != "offer-1" || hire.OfferRevision != signed.Revision || hire.CandidacyID != candidacyID || hire.CandidateID != candidateID {
		t.Fatalf("hire intent binds the wrong offer: %+v", hire)
	}
	if hire.IntentID == "" {
		t.Fatal("hire intent must carry a deterministic intent id")
	}
	if hire.WorkerCreated {
		t.Fatal("acceptance must not create a Worker: worker creation is separately governed")
	}
	accepted, ok := ledger.Offer("offer-1")
	if !ok || accepted.Status != OfferAccepted {
		t.Fatalf("accepted offer = %+v ok=%v", accepted, ok)
	}
	kinds := ledger.EventKinds()
	if len(kinds) != 5 {
		t.Fatalf("event kinds = %v, want 5 (created, terms, approved, signed, accepted)", kinds)
	}
	if kinds[len(kinds)-1] != "OFFER_ACCEPTED" {
		t.Fatalf("last event = %q, want OFFER_ACCEPTED", kinds[len(kinds)-1])
	}
	if len(ledger.HireIntents()) != 1 {
		t.Fatalf("hires = %d, want exactly one Hire child intent", len(ledger.HireIntents()))
	}

	// A second acceptance loses CAS with zero extra effect.
	events, hires = offerCounts(ledger)
	second := AcceptOfferCmd{
		OfferID: "offer-1", ExpectedRevision: signed.Revision,
		CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: candidateID,
		SignatureDigest: signed.SignatureDigest,
		AcceptedAt:      recruitInstant(t, 15), KnownAt: recruitKnown(t, 15),
	}
	if err := mustAccept(ledger, second); offerCodeOf(err) != CodeOfferConflict {
		t.Fatalf("double accept err = %v", err)
	}
	if gotEvents, gotHires := offerCounts(ledger); gotEvents != events || gotHires != hires {
		t.Fatal("double acceptance appended events/hires")
	}
}

func mustAccept(ledger *OfferLedger, cmd AcceptOfferCmd) error {
	_, err := ledger.AcceptOffer(cmd)
	return err
}

// TestTodo_RECRUIT_003_Property: post-approval immutability holds for every
// term, digests are stable and deterministic, and revisions only move
// forward on accepted commands.
func TestTodo_RECRUIT_003_Property(t *testing.T) {
	t.Run("every post-approval term change is refused", func(t *testing.T) {
		agg := openRecruiting(t)
		ledger := NewOfferLedger()
		createOfferFixture(t, ledger, "offer-1", &agg)
		approvedSignedOffer(t, ledger, "offer-1")
		signed, _ := ledger.Offer("offer-1")
		base := offerTerms(t)
		cases := map[string]func(*OfferTerms){
			"salary":   func(tm *OfferTerms) { tm.Salary = values.MustDecimal("1", 2, values.RoundingHalfUp) },
			"position": func(tm *OfferTerms) { tm.PositionRef = "position:other/n1" },
			"start":    func(tm *OfferTerms) { tm.StartDate = recruitInstant(t, 60) },
			"expiry":   func(tm *OfferTerms) { tm.ExpiresAt = recruitInstant(t, 90) },
		}
		for name, mutate := range cases {
			changed := base
			mutate(&changed)
			if err := ledger.UpdateOfferTerms("offer-1", signed.Revision, changed, recruitInstant(t, 13), recruitKnown(t, 13)); offerCodeOf(err) != CodeApprovalMismatch {
				t.Fatalf("%s: err = %v", name, err)
			}
		}
		after, _ := ledger.Offer("offer-1")
		if after.CanonicalDigest != signed.CanonicalDigest || after.Revision != signed.Revision {
			t.Fatal("refused updates mutated the signed revision")
		}
	})

	t.Run("identical inputs yield identical digests", func(t *testing.T) {
		build := func() OfferRevision {
			agg := openRecruiting(t)
			ledger := NewOfferLedger()
			createOfferFixture(t, ledger, "offer-1", &agg)
			approvedSignedOffer(t, ledger, "offer-1")
			rev, _ := ledger.Offer("offer-1")
			return rev
		}
		first, second := build(), build()
		if first.CanonicalDigest == "" || first.CanonicalDigest != second.CanonicalDigest {
			t.Fatalf("digests %q vs %q must be stable", first.CanonicalDigest, second.CanonicalDigest)
		}
		if first.ProposalDigest != second.ProposalDigest || first.ApprovalDigest != second.ApprovalDigest {
			t.Fatal("proposal/approval digests must be deterministic")
		}
	})
}

// TestTodo_RECRUIT_003_Golden: the canonical digest of the fixed fixture is
// pinned, and the digest is sensitive to every material term.
func TestTodo_RECRUIT_003_Golden(t *testing.T) {
	agg := openRecruiting(t)
	ledger := NewOfferLedger()
	createOfferFixture(t, ledger, "offer-1", &agg)
	approvedSignedOffer(t, ledger, "offer-1")
	signed, _ := ledger.Offer("offer-1")
	const wantDigest = "sha256:d4c3ccb7a5948ae0ed308131c03b542a98f67e5ce9ec4b0217ff3846b344b192"
	if signed.CanonicalDigest != wantDigest {
		t.Fatalf("golden digest = %q, want %q", signed.CanonicalDigest, wantDigest)
	}
	for name, mutate := range map[string]func(OfferTerms) OfferTerms{
		"salary": func(base OfferTerms) OfferTerms {
			base.Salary = values.MustDecimal("1", 2, values.RoundingHalfUp)
			return base
		},
		"position": func(base OfferTerms) OfferTerms { base.PositionRef = "position:other/n1"; return base },
		"start":    func(base OfferTerms) OfferTerms { base.StartDate = recruitInstant(t, 61); return base },
	} {
		other := mutate(offerTerms(t))
		agg2 := openRecruiting(t)
		ledger2 := NewOfferLedger()
		candidacyID, candidateID, candidacyRev := offerCandidacy(t, &agg2)
		rev, err := ledger2.CreateOffer(CreateOfferCmd{
			OfferID: "offer-1", CandidacyID: candidacyID, CandidateID: candidateID,
			CandidacyRevision: candidacyRev, Terms: other,
			EffectiveAt: recruitInstant(t, 10), KnownAt: recruitKnown(t, 10),
		})
		if err != nil {
			t.Fatal(err)
		}
		if rev.CanonicalDigest == signed.CanonicalDigest {
			t.Fatalf("%s change left the digest untouched", name)
		}
	}
}

// TestTodo_RECRUIT_003_Race: concurrent acceptances elect exactly one
// winner; the loser reports OFFER_CONFLICT with zero extra effect.
func TestTodo_RECRUIT_003_Race(t *testing.T) {
	agg := openRecruiting(t)
	ledger := NewOfferLedger()
	candidacyID, candidateID, candidacyRev := createOfferFixture(t, ledger, "offer-1", &agg)
	signed := approvedSignedOffer(t, ledger, "offer-1")
	beforeEvents, beforeHires := offerCounts(ledger)

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ledger.AcceptOffer(AcceptOfferCmd{
				OfferID: "offer-1", ExpectedRevision: signed.Revision,
				CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: candidateID,
				SignatureDigest: signed.SignatureDigest,
				AcceptedAt:      recruitInstant(t, 14), KnownAt: recruitKnown(t, 14),
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var won, conflicted int
	for result := range results {
		if result == nil {
			won++
		} else if offerCodeOf(result) == CodeOfferConflict {
			conflicted++
		} else {
			t.Fatalf("concurrent accept err = %v", result)
		}
	}
	if won != 1 || conflicted != 1 {
		t.Fatalf("won=%d conflicted=%d, want exactly one winner", won, conflicted)
	}
	if gotEvents, gotHires := offerCounts(ledger); gotEvents != beforeEvents+1 || gotHires != beforeHires+1 {
		t.Fatalf("events=%d hires=%d, want exactly one acceptance effect", gotEvents, gotHires)
	}
}

// TestTodo_RECRUIT_003_Integration: the offer ledger binds a real ATS
// aggregate — the current candidacy revision accepts, a stale one is
// refused, and the Hire intent names the governed candidacy.
func TestTodo_RECRUIT_003_Integration(t *testing.T) {
	agg := openRecruiting(t)
	ledger := NewOfferLedger()
	candidacyID, candidateID, candidacyRev := createOfferFixture(t, ledger, "offer-1", &agg)
	signed := approvedSignedOffer(t, ledger, "offer-1")

	stale := AcceptOfferCmd{
		OfferID: "offer-1", ExpectedRevision: signed.Revision,
		CandidacyID: candidacyID, CandidacyRevision: candidacyRev - 1, CandidateID: candidateID,
		SignatureDigest: signed.SignatureDigest,
		AcceptedAt:      recruitInstant(t, 14), KnownAt: recruitKnown(t, 14),
	}
	if err := mustAccept(ledger, stale); offerCodeOf(err) != CodeCandidacyNotCurrent {
		t.Fatalf("stale candidacy err = %v", err)
	}
	hire, err := ledger.AcceptOffer(AcceptOfferCmd{
		OfferID: "offer-1", ExpectedRevision: signed.Revision,
		CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: candidateID,
		SignatureDigest: signed.SignatureDigest,
		AcceptedAt:      recruitInstant(t, 14), KnownAt: recruitKnown(t, 14),
	})
	if err != nil {
		t.Fatalf("AcceptOffer against the real aggregate: %v", err)
	}
	if hire.CandidacyID != "cand-1" {
		t.Fatalf("hire candidacy = %q", hire.CandidacyID)
	}
	if candidacy := agg.Candidacies["cand-1"]; candidacy.Stage != StageOffer {
		t.Fatalf("aggregate candidacy stage = %q, want OFFER", candidacy.Stage)
	}
}

// TestTodo_RECRUIT_003_Fault: every failpoint — expired offer, rescinded
// offer, signature over a different digest — reaches its allowed durable
// state with zero lost or duplicated effect.
func TestTodo_RECRUIT_003_Fault(t *testing.T) {
	t.Run("expired offer is accepted never", func(t *testing.T) {
		agg := openRecruiting(t)
		ledger := NewOfferLedger()
		candidacyID, candidateID, candidacyRev := createOfferFixture(t, ledger, "offer-1", &agg)
		signed := approvedSignedOffer(t, ledger, "offer-1")
		events, hires := offerCounts(ledger)
		err := mustAccept(ledger, AcceptOfferCmd{
			OfferID: "offer-1", ExpectedRevision: signed.Revision,
			CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: candidateID,
			SignatureDigest: signed.SignatureDigest,
			AcceptedAt:      recruitInstant(t, 21), KnownAt: recruitKnown(t, 21),
		})
		if offerCodeOf(err) != CodeOfferExpired {
			t.Fatalf("expired accept err = %v", err)
		}
		if !errors.Is(err, ErrOfferRefused) {
			t.Fatalf("expired accept must match ErrOfferRefused: %v", err)
		}
		if gotEvents, gotHires := offerCounts(ledger); gotEvents != events || gotHires != hires {
			t.Fatal("expired acceptance had an effect")
		}
	})

	t.Run("rescinded offer is accepted never", func(t *testing.T) {
		agg := openRecruiting(t)
		ledger := NewOfferLedger()
		candidacyID, candidateID, candidacyRev := createOfferFixture(t, ledger, "offer-1", &agg)
		signed := approvedSignedOffer(t, ledger, "offer-1")
		if err := ledger.RescindOffer("offer-1", signed.Revision, "position frozen", recruitInstant(t, 13), recruitKnown(t, 13)); err != nil {
			t.Fatalf("RescindOffer: %v", err)
		}
		events, hires := offerCounts(ledger)
		err := mustAccept(ledger, AcceptOfferCmd{
			OfferID: "offer-1", ExpectedRevision: signed.Revision + 1,
			CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: candidateID,
			SignatureDigest: signed.SignatureDigest,
			AcceptedAt:      recruitInstant(t, 14), KnownAt: recruitKnown(t, 14),
		})
		if offerCodeOf(err) != CodeOfferRescinded {
			t.Fatalf("rescinded accept err = %v", err)
		}
		if gotEvents, gotHires := offerCounts(ledger); gotEvents != events || gotHires != hires {
			t.Fatal("rescinded acceptance had an effect")
		}
	})

	t.Run("signature over a different digest is refused", func(t *testing.T) {
		agg := openRecruiting(t)
		ledger := NewOfferLedger()
		candidacyID, candidateID, candidacyRev := createOfferFixture(t, ledger, "offer-1", &agg)
		signed := approvedSignedOffer(t, ledger, "offer-1")
		events, hires := offerCounts(ledger)
		err := mustAccept(ledger, AcceptOfferCmd{
			OfferID: "offer-1", ExpectedRevision: signed.Revision,
			CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: candidateID,
			SignatureDigest: "sha256:digest-of-something-else",
			AcceptedAt:      recruitInstant(t, 14), KnownAt: recruitKnown(t, 14),
		})
		if offerCodeOf(err) != CodeSignatureMismatch {
			t.Fatalf("foreign signature err = %v", err)
		}
		if gotEvents, gotHires := offerCounts(ledger); gotEvents != events || gotHires != hires {
			t.Fatal("foreign-signature acceptance had an effect")
		}
	})
}

// TestTodo_RECRUIT_003_Security: unauthorized deciders are denied, provider
// acceptance can never mint a Hire, and refusals leak no compensation or
// candidate content.
func TestTodo_RECRUIT_003_Security(t *testing.T) {
	agg := openRecruiting(t)
	ledger := NewOfferLedger()
	candidacyID, _, candidacyRev := createOfferFixture(t, ledger, "offer-1", &agg)

	if err := ledger.ApproveOffer("offer-1", 1, "mallory", "provider:background-check", recruitInstant(t, 11), recruitKnown(t, 11)); offerCodeOf(err) != CodeOfferUnauthorized {
		t.Fatalf("provider approval err = %v", err)
	}
	approvedSignedOffer(t, ledger, "offer-1")
	signed, _ := ledger.Offer("offer-1")

	// A document-provider signature is refused at signing and can never
	// become a Hire through acceptance either.
	if err := ledger.SignOffer("offer-1", signed.Revision, "provider:docsign", "provider:docsign", signed.CanonicalDigest, recruitInstant(t, 13), recruitKnown(t, 13)); offerCodeOf(err) != CodeProviderAcceptance {
		t.Fatalf("provider signature err = %v", err)
	}
	events, hires := offerCounts(ledger)
	err := mustAccept(ledger, AcceptOfferCmd{
		OfferID: "offer-1", ExpectedRevision: signed.Revision,
		CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: "candidate-9",
		SignatureDigest: signed.SignatureDigest,
		AcceptedAt:      recruitInstant(t, 14), KnownAt: recruitKnown(t, 14),
	})
	if offerCodeOf(err) != CodeCandidacyNotCurrent {
		t.Fatalf("wrong-candidate accept err = %v", err)
	}
	if gotEvents, gotHires := offerCounts(ledger); gotEvents != events || gotHires != hires {
		t.Fatal("unauthorized acceptance had an effect")
	}
	for _, refused := range []error{err} {
		if strings.Contains(refused.Error(), "120000") || strings.Contains(refused.Error(), "consent") {
			t.Fatalf("refusal leaks governed content: %q", refused)
		}
	}
	if strings.Contains(ledger.Explain(), "candidate-1") {
		t.Fatalf("explain leaks candidate identity: %q", ledger.Explain())
	}
}

// TestTodo_RECRUIT_003_Mutation: a mutant that checks only signature
// presence (not equality with the approval-bound digest) would accept a
// foreign signature — the oracle kills it by refusing.
func TestTodo_RECRUIT_003_Mutation(t *testing.T) {
	agg := openRecruiting(t)
	ledger := NewOfferLedger()
	candidacyID, candidateID, candidacyRev := createOfferFixture(t, ledger, "offer-1", &agg)
	signed := approvedSignedOffer(t, ledger, "offer-1")

	foreign := AcceptOfferCmd{
		OfferID: "offer-1", ExpectedRevision: signed.Revision,
		CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: candidateID,
		SignatureDigest: "sha256:digest-of-something-else",
		AcceptedAt:      recruitInstant(t, 14), KnownAt: recruitKnown(t, 14),
	}
	mutantWouldAccept := len(foreign.SignatureDigest) > 0
	if err := mustAccept(ledger, foreign); err == nil {
		t.Fatal("foreign signature accepted: approval/signature binding is broken")
	} else if offerCodeOf(err) != CodeSignatureMismatch {
		t.Fatalf("foreign signature err = %v", err)
	}
	if !mutantWouldAccept {
		t.Fatal("mutant fixture is vacuous: presence-only check must accept this input")
	}
	if len(ledger.HireIntents()) != 0 {
		t.Fatal("mutant-equivalent input minted a Hire")
	}
}

// FuzzTodo_RECRUIT_003: attacker-controlled acceptance inputs (forged
// digests, hostile IDs, wild revisions and days) are always refused with a
// typed ErrOfferRefused and zero effect, or accepted with exactly one
// deterministic Hire child intent and no worker creation.
func FuzzTodo_RECRUIT_003(f *testing.F) {
	f.Add("sha256:forged-digest", 14, uint64(3), "cand-1", uint64(4), "candidate-1")
	f.Add("", 14, uint64(0), "", uint64(0), "")
	f.Add("sha256:digest-of-something-else", 99, uint64(1), "cand-9", uint64(9), "candidate-9")
	f.Add("provider:signed-elsewhere", -5, uint64(1<<63), "cand-1\x00", uint64(4), "candidate-1")
	f.Fuzz(func(t *testing.T, sigDigest string, acceptDay int, rev uint64, candidacyID string, candidacyRev uint64, candidateID string) {
		day := acceptDay % 60
		if day < 0 {
			day += 60
		}
		agg := openRecruiting(t)
		ledger := NewOfferLedger()
		createOfferFixture(t, ledger, "offer-1", &agg)
		signed := approvedSignedOffer(t, ledger, "offer-1")
		events, hires := offerCounts(ledger)
		hire, err := ledger.AcceptOffer(AcceptOfferCmd{
			OfferID: "offer-1", ExpectedRevision: rev,
			CandidacyID: candidacyID, CandidacyRevision: candidacyRev, CandidateID: candidateID,
			SignatureDigest: sigDigest,
			AcceptedAt:      recruitInstant(t, day), KnownAt: recruitKnown(t, 14),
		})
		if err != nil {
			if !errors.Is(err, ErrOfferRefused) {
				t.Fatalf("untyped accept error for digest %q rev %d: %v", sigDigest, rev, err)
			}
			if gotEvents, gotHires := offerCounts(ledger); gotEvents != events || gotHires != hires {
				t.Fatal("refused acceptance had an effect")
			}
			return
		}
		if gotEvents, gotHires := offerCounts(ledger); gotEvents != events+1 || gotHires != hires+1 {
			t.Fatalf("accepted offer events=%d hires=%d, want one of each more", gotEvents-events, gotHires-hires)
		}
		if hire.OfferRevision != signed.Revision || hire.CandidacyID != signed.CandidacyID || hire.CandidateID != signed.CandidateID {
			t.Fatalf("hire not bound to the signed revision: %+v", hire)
		}
		if hire.WorkerCreated {
			t.Fatal("acceptance created a worker: worker creation is separately governed")
		}
		if len(hire.IntentID) == 0 {
			t.Fatal("hire intent needs a deterministic id")
		}
	})
}
