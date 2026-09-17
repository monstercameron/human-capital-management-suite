package legal_test

// LEGAL-016 resistance matrix: FUZZ, SECURITY and MUTATION.
//
// The PRIMARY, GOLDEN, CONFORMANCE and PROPERTY classes live in
// legal_016_test.go. This file proves the same canonical promotion vector
// resists hostile input (FUZZ), forged or foreign authority (SECURITY) and
// silent rule changes (MUTATION). Every test evaluates only packs that are
// available in the tree and asserts receipt properties, never state-rule
// content, so state-data edits cannot weaken these oracles.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal/extract"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// l16RMFreqs is the fuzz-controlled pay-frequency vocabulary: the four real
// frequencies, the empty (unstated) frequency, and three hostile spellings
// the evaluation must survive without panicking.
var l16RMFreqs = []string{"SEMIMONTHLY", "WEEKLY", "BIWEEKLY", "MONTHLY", "", "DAILY", "semimonthly", "SEMI MONTHLY"}

// l16RMWorker and l16RMEntity are the fixed canonical identities the fuzz
// and resistance vectors run under. Identity validation has its own pinned
// vectors below; the corpus varies facts, not principals.
const (
	l16RMWorker = "legal-016-canonical-worker"
	l16RMEntity = "legal-016-canonical-entity"
)

// errL16RMDenied marks an input the selection or evaluation layer refused.
// It separates a legitimate denial (the oracle passes) from broken harness
// plumbing (the oracle fails).
var errL16RMDenied = errors.New("legal-016 resistance: input denied")

// l16RMMoney builds an exact-decimal USD amount from integer cents. A false
// second result means the values layer refused the figure, which the driver
// treats as a boundary rejection, never as an acceptance.
func l16RMMoney(cents int64) (values.Money, bool) {
	abs := cents
	sign := ""
	if abs < 0 {
		sign = "-"
		abs = -abs
	}
	text := fmt.Sprintf("%s%d.%02d", sign, abs/100, abs%100)
	m, err := values.NewMoney(text, "USD", 2, values.RoundingHalfUp)
	if err != nil {
		return values.Money{}, false
	}
	return m, true
}

// l16RMProposal decodes one 24-byte fuzz vector into a promotion proposal:
//
//	[0:8]   current base pay, uint64 LE cents mapped to -$10,000..+$30,000
//	[8:16]  new base pay, same mapping
//	[16]    pay-frequency vocabulary index
//	[17]    fact flags (promotion, salary history, leave, covenant, ...)
//	[18]    year, 2020 + b%16, so the corpus straddles the 2026-01-01 draft window
//	[19]    month, [20] day (capped at 28, always a real date)
//	[21]    concurrent workforce-reduction count
//	[22]    protected-activity count, [23] leave-balance mask
//
// Short inputs and values-layer refusals are boundary rejections: the figure
// never reaches legal evaluation.
func l16RMProposal(data []byte, workerID, entityID string) (legal.PromotionProposalSnapshot, bool) {
	if len(data) < 24 {
		return legal.PromotionProposalSnapshot{}, false
	}
	toCents := func(v uint64) int64 { return int64(v%4000001) - 1000000 }
	current, ok := l16RMMoney(toCents(binary.LittleEndian.Uint64(data[0:8])))
	if !ok {
		return legal.PromotionProposalSnapshot{}, false
	}
	raised, ok := l16RMMoney(toCents(binary.LittleEndian.Uint64(data[8:16])))
	if !ok {
		return legal.PromotionProposalSnapshot{}, false
	}
	effective, err := values.NewLocalDate(2020+int(data[18]%16), time.Month(data[19]%12+1), int(data[20]%28+1))
	if err != nil {
		return legal.PromotionProposalSnapshot{}, false
	}
	flags := data[17]
	at := func(bit uint) bool { return flags&(1<<bit) != 0 }
	var balances []string
	if data[23]&1 != 0 {
		balances = append(balances, "accrued paid sick leave")
	}
	if data[23]&2 != 0 {
		balances = append(balances, "paid family and medical leave")
	}
	var activities []legal.RecordedProtectedActivity
	if data[22]%3 > 0 {
		activities = append(activities, legal.RecordedProtectedActivity{Kind: "workers_compensation_claim", DaysBeforeEffectiveDate: 10})
	}
	if data[22]%3 > 1 {
		activities = append(activities, legal.RecordedProtectedActivity{Kind: "whistleblower_report", DaysBeforeEffectiveDate: 30})
	}
	return legal.PromotionProposalSnapshot{
		WorkerID:                    workerID,
		LegalEntityID:               entityID,
		EffectiveDate:               effective,
		CurrentBasePay:              current,
		NewBasePay:                  raised,
		PayFrequency:                l16RMFreqs[int(data[16])%len(l16RMFreqs)],
		IsInternalPromotion:         at(0),
		CollectsSalaryHistory:       at(1),
		OnProtectedLeave:            at(2),
		HasExistingNonCompete:       at(3),
		WorkforceReductionCount:     int(data[21]),
		LeaveProgramBalances:        balances,
		RoleChanged:                 at(4),
		RecordedProtectedActivities: activities,
		RoleBecomesSafetySensitive:  at(5),
		DrugTestOrdered:             at(6),
		BreachIncidentOpened:        at(7),
		DataCategoriesTouched:       []string{"biometric_identifiers"},
	}, true
}

// l16RMFixture is the shared resistance input: one available pack and the
// fixed vector signer.
type l16RMFixture struct {
	pack   legal.RulePack
	signer *legal.Signer
	err    error
}

// l16RMSetup loads the available California pack and the fixed vector signer
// once per process. Both are pure functions of checked-in bytes and a fixed
// seed, and every evaluation registers into a fresh registry without
// mutating the shared pack, so sharing them across iterations is sound.
var l16RMSetup = sync.OnceValue(func() l16RMFixture {
	path, err := legal.PackDefinitionPath("states", extract.DefinitionFileName("CA"))
	if err != nil {
		return l16RMFixture{err: err}
	}
	def, err := legal.LoadPackDefinitionFile(path)
	if err != nil {
		return l16RMFixture{err: err}
	}
	candidate, err := def.Candidate()
	if err != nil {
		return l16RMFixture{err: err}
	}
	signer, err := legal.NewSigner(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x16}, ed25519.SeedSize)))
	if err != nil {
		return l16RMFixture{err: err}
	}
	return l16RMFixture{pack: candidate.Pack(), signer: signer}
})

// l16RMVerdict evaluates one proposal against the shared pack in a fresh
// registry. Accepted=false means selection or evaluation denied the input.
// A non-nil violation means the harness itself is broken: a setup failure,
// a plumbing error, or a re-evaluation that disagrees with the first.
func l16RMVerdict(proposal legal.PromotionProposalSnapshot) (legal.LegalEvaluationReceipt, bool, error) {
	fixture := l16RMSetup()
	if fixture.err != nil {
		return legal.LegalEvaluationReceipt{}, false, fmt.Errorf("resistance setup: %w", fixture.err)
	}
	pack, signer := fixture.pack, fixture.signer
	eval := func() (legal.LegalEvaluationReceipt, error) {
		registry := legal.NewRegistry()
		if err := registry.Register(pack); err != nil {
			return legal.LegalEvaluationReceipt{}, fmt.Errorf("resistance register: %w", err)
		}
		knownInstant, err := values.NewInstantFromUnix(1_770_000_000, 0)
		if err != nil {
			return legal.LegalEvaluationReceipt{}, fmt.Errorf("resistance clock: %w", err)
		}
		at, err := values.NewInstantFromUnix(1_770_200_000, 0)
		if err != nil {
			return legal.LegalEvaluationReceipt{}, fmt.Errorf("resistance clock: %w", err)
		}
		known, err := values.NewKnownAt(knownInstant)
		if err != nil {
			return legal.LegalEvaluationReceipt{}, fmt.Errorf("resistance clock: %w", err)
		}
		ctx, err := legal.Resolve(legal.LegalContextInput{
			LegalEntityID:          proposal.LegalEntityID,
			WorkLocation:           pack.Jurisdiction,
			EmploymentJurisdiction: pack.Jurisdiction,
			EffectiveDate:          proposal.EffectiveDate,
			KnownAt:                known,
		}, registry, signer, at)
		if err != nil {
			return legal.LegalEvaluationReceipt{}, fmt.Errorf("%w: resolve: %v", errL16RMDenied, err)
		}
		receipt, err := legal.EvaluateReceipt(ctx, proposal, registry, signer, at)
		if err != nil {
			return legal.LegalEvaluationReceipt{}, fmt.Errorf("%w: evaluate: %v", errL16RMDenied, err)
		}
		return receipt, nil
	}
	first, err := eval()
	if errors.Is(err, errL16RMDenied) {
		return legal.LegalEvaluationReceipt{}, false, nil
	}
	if err != nil {
		return legal.LegalEvaluationReceipt{}, false, err
	}
	second, err := eval()
	if errors.Is(err, errL16RMDenied) {
		return legal.LegalEvaluationReceipt{}, false,
			fmt.Errorf("resistance nondeterministic: first evaluation accepted, second denied")
	}
	if err != nil {
		return legal.LegalEvaluationReceipt{}, false, err
	}
	if second.Digest != first.Digest || !bytes.Equal(second.CanonicalBytes(), first.CanonicalBytes()) {
		return legal.LegalEvaluationReceipt{}, false,
			fmt.Errorf("resistance nondeterministic: re-evaluation moved digest %q to %q", first.Digest, second.Digest)
	}
	return first, true, nil
}

// l16RMDrive runs one raw vector through decode and double evaluation.
func l16RMDrive(data []byte, workerID, entityID string) (legal.LegalEvaluationReceipt, bool, error) {
	proposal, ok := l16RMProposal(data, workerID, entityID)
	if !ok {
		return legal.LegalEvaluationReceipt{}, false, nil
	}
	return l16RMVerdict(proposal)
}

// l16RMCheckVerified asserts an accepted receipt is complete, signed evidence.
func l16RMCheckVerified(t *testing.T, receipt legal.LegalEvaluationReceipt, signer *legal.Signer) {
	t.Helper()
	if receipt.Digest == "" {
		t.Fatal("accepted receipt carries no digest")
	}
	if len(receipt.PinnedReleases) == 0 {
		t.Fatal("accepted receipt pins no release")
	}
	if err := receipt.VerifyWithKey(signer.PublicKey()); err != nil {
		t.Fatalf("accepted receipt does not verify: %v", err)
	}
}

// l16RMEncode builds one 24-byte vector for the layout l16RMProposal reads.
// Pay figures are integer cents in -$10,000..+$30,000; year and month are raw
// selector bytes decoded with the same modulus the driver uses.
func l16RMEncode(curCents, newCents int64, freqIdx, flags byte, year, month, day, workforce, acts, balances byte) []byte {
	out := make([]byte, 24)
	binary.LittleEndian.PutUint64(out[0:8], uint64(curCents+1000000))
	binary.LittleEndian.PutUint64(out[8:16], uint64(newCents+1000000))
	out[16], out[17] = freqIdx, flags
	out[18], out[19], out[20] = year, month, day
	out[21], out[22], out[23] = workforce, acts, balances
	return out
}

// l16RMCanonical is the byte encoding of the shared canonical vector:
// $32 to $36, semimonthly, every fact flag set, 2026-03-01, both leave
// balances and both recorded activities on record.
func l16RMCanonical() []byte {
	return l16RMEncode(3200, 3600, 0, 0xFF, 6, 2, 0, 0, 2, 3)
}

// TestTodo_LEGAL_016_Fuzz runs the deterministic resistance corpus: every
// vector is either refused at the boundary or evaluates to a verified,
// deterministic receipt. The pinned accept/refuse directions rest on the
// selection contract (an empty legal entity is refused by Resolve; drafts
// open their window at 2026-01-01, so 2020 predates every release and 2035
// falls inside the open window) and on documented evaluation behavior (zero
// money is an indeterminate pay change, not a refusal).
func TestTodo_LEGAL_016_Fuzz(t *testing.T) {
	fixture := l16RMSetup()
	if fixture.err != nil {
		t.Fatalf("resistance setup: %v", fixture.err)
	}
	signer := fixture.signer
	const (
		wantAccept = "accept"
		wantReject = "reject"
		wantEither = "either"
	)
	vectors := []struct {
		name   string
		data   []byte
		worker string
		entity string
		want   string
	}{
		{"canonical vector is accepted", l16RMCanonical(), l16RMWorker, l16RMEntity, wantAccept},
		{"zero pay is indeterminate, not refused", l16RMEncode(0, 0, 0, 0, 6, 2, 0, 0, 0, 0), l16RMWorker, l16RMEntity, wantAccept},
		{"pre-window date is denied", l16RMEncode(3200, 3600, 0, 0xFF, 0, 0, 0, 0, 2, 3), l16RMWorker, l16RMEntity, wantReject},
		{"open-window future date is accepted", l16RMEncode(3200, 3600, 0, 0xFF, 15, 11, 27, 0, 2, 3), l16RMWorker, l16RMEntity, wantAccept},
		{"pay decrease evaluates", l16RMEncode(3600, 3200, 0, 0xFF, 6, 2, 0, 0, 2, 3), l16RMWorker, l16RMEntity, wantAccept},
		{"negative pay is contained", l16RMEncode(-5000, 3200, 0, 0xFF, 6, 2, 0, 0, 0, 0), l16RMWorker, l16RMEntity, wantEither},
		{"unknown frequency is contained", l16RMEncode(3200, 3600, 5, 0xFF, 6, 2, 0, 0, 2, 3), l16RMWorker, l16RMEntity, wantEither},
		{"empty frequency is contained", l16RMEncode(3200, 3600, 4, 0, 6, 2, 0, 0, 0, 0), l16RMWorker, l16RMEntity, wantEither},
		{"mass reduction count is contained", l16RMEncode(3200, 3600, 0, 0xFF, 6, 2, 0, 200, 2, 3), l16RMWorker, l16RMEntity, wantEither},
		{"empty worker is contained", l16RMCanonical(), "", l16RMEntity, wantEither},
		{"empty legal entity is refused", l16RMCanonical(), l16RMWorker, "", wantReject},
		{"short input is refused", []byte{1, 2, 3}, l16RMWorker, l16RMEntity, wantReject},
		{"all-ones hostile is contained", bytes.Repeat([]byte{0xFF}, 24), l16RMWorker, l16RMEntity, wantEither},
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			receipt, accepted, violation := l16RMDrive(v.data, v.worker, v.entity)
			if violation != nil {
				t.Fatalf("evaluation plumbing failed: %v", violation)
			}
			switch v.want {
			case wantAccept:
				if !accepted {
					t.Fatal("vector was denied, want an accepted evaluation")
				}
			case wantReject:
				if accepted {
					t.Fatal("vector was accepted, want a boundary denial")
				}
				return
			}
			if accepted {
				l16RMCheckVerified(t, receipt, signer)
			}
		})
	}

	t.Run("pay change is visible in the receipt", func(t *testing.T) {
		rise, accepted, violation := l16RMDrive(l16RMCanonical(), l16RMWorker, l16RMEntity)
		if violation != nil || !accepted {
			t.Fatalf("canonical drive = accepted=%v, violation=%v", accepted, violation)
		}
		flat, accepted, violation := l16RMDrive(l16RMEncode(3200, 3200, 0, 0xFF, 6, 2, 0, 0, 2, 3), l16RMWorker, l16RMEntity)
		if violation != nil || !accepted {
			t.Fatalf("flat-pay drive = accepted=%v, violation=%v", accepted, violation)
		}
		if rise.Digest == flat.Digest {
			t.Fatal("a pay change and no pay change share a receipt digest; the notice trigger is invisible")
		}
	})
}

// FuzzTodo_LEGAL_016_Evaluate fuzzes the canonical promotion evaluation over
// hostile proposals: no input may panic the evaluator, every accepted input
// must yield a verified deterministic receipt, and every refused input must
// fail closed with an error rather than a half-built receipt.
func FuzzTodo_LEGAL_016_Evaluate(f *testing.F) {
	f.Add(l16RMCanonical())
	f.Add(l16RMEncode(0, 0, 0, 0, 6, 2, 0, 0, 0, 0))
	f.Add(l16RMEncode(3600, 3200, 0, 0xFF, 6, 2, 0, 100, 2, 3))
	f.Add(l16RMEncode(-5000, 3200, 5, 0xFF, 6, 2, 0, 0, 0, 0))
	f.Add(l16RMEncode(3200, 3600, 0, 0xFF, 0, 0, 0, 0, 0, 0))
	f.Add(make([]byte, 24))
	f.Add(bytes.Repeat([]byte{0xFF}, 24))
	f.Fuzz(func(t *testing.T, data []byte) {
		receipt, accepted, violation := l16RMDrive(data, l16RMWorker, l16RMEntity)
		if violation != nil {
			t.Fatalf("evaluation plumbing failed: %v", violation)
		}
		if !accepted {
			return
		}
		fixture := l16RMSetup()
		if fixture.err != nil {
			t.Fatalf("resistance setup: %v", fixture.err)
		}
		l16RMCheckVerified(t, receipt, fixture.signer)
	})
}

// TestTodo_LEGAL_016_Security proves the signed receipt is unforgeable and
// authority-bound: tampered, stripped or foreign-key evidence fails closed
// with the typed receipt sentinel, and genuine evidence survives a JSON
// export round trip. "Fails closed" means verification refuses without
// trusting any field of the tampered receipt.
func TestTodo_LEGAL_016_Security(t *testing.T) {
	pack := l16LoadPack(t, "CA")
	registry := legal.NewRegistry()
	if err := registry.Register(pack); err != nil {
		t.Fatalf("Register(CA): %v", err)
	}
	signer := l16Signer(t)
	proposal := canonicalProposal(t)
	effective := l16Date(t, 2026, time.March, 1)
	baseline := l16Evaluate(t, registry, signer, "CA", effective, proposal)
	if len(baseline.ObligationsApplied) == 0 {
		t.Fatal("canonical California receipt applies no obligation; tamper cases need a non-empty body")
	}

	t.Run("valid receipt verifies under its key", func(t *testing.T) {
		if err := baseline.VerifyWithKey(signer.PublicKey()); err != nil {
			t.Fatalf("VerifyWithKey: %v", err)
		}
	})

	t.Run("forged obligation identity is rejected", func(t *testing.T) {
		mut := baseline
		mut.ObligationsApplied = append([]legal.ReceiptAppliedObligation(nil), baseline.ObligationsApplied...)
		mut.ObligationsApplied[0].ID += "/forged"
		if err := mut.VerifyWithKey(signer.PublicKey()); !errors.Is(err, legal.ErrReceiptInvalid) {
			t.Fatalf("forged receipt verifies: err=%v", err)
		}
	})

	t.Run("rewritten digest is rejected", func(t *testing.T) {
		mut := baseline
		mut.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		if err := mut.VerifyWithKey(signer.PublicKey()); !errors.Is(err, legal.ErrReceiptInvalid) {
			t.Fatalf("digest-rewritten receipt verifies: err=%v", err)
		}
	})

	t.Run("stripped signature is rejected", func(t *testing.T) {
		mut := baseline
		mut.Signature = legal.Signature{}
		if err := mut.VerifyWithKey(signer.PublicKey()); !errors.Is(err, legal.ErrReceiptInvalid) {
			t.Fatalf("unsigned receipt verifies: err=%v", err)
		}
	})

	t.Run("foreign key is rejected", func(t *testing.T) {
		other, err := legal.NewSigner(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x17}, ed25519.SeedSize)))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		if err := baseline.VerifyWithKey(other.PublicKey()); !errors.Is(err, legal.ErrReceiptInvalid) {
			t.Fatalf("receipt verifies under a foreign key: err=%v", err)
		}
	})

	t.Run("foreign authority cannot evaluate", func(t *testing.T) {
		other, err := legal.NewSigner(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x17}, ed25519.SeedSize)))
		if err != nil {
			t.Fatalf("NewSigner: %v", err)
		}
		at := l16Instant(t, 1_770_200_000)
		ctx, err := legal.Resolve(l16Input("CA", effective, t), registry, signer, at)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if _, err := legal.EvaluateReceipt(ctx, proposal, registry, other, at); !errors.Is(err, legal.ErrReceiptInvalid) {
			t.Fatalf("foreign authority evaluated under another context: err=%v", err)
		}
	})

	t.Run("evidence survives JSON export", func(t *testing.T) {
		raw, err := baseline.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON: %v", err)
		}
		var roundTrip legal.LegalEvaluationReceipt
		if err := roundTrip.UnmarshalJSON(raw); err != nil {
			t.Fatalf("UnmarshalJSON: %v", err)
		}
		if roundTrip.Digest != baseline.Digest {
			t.Fatal("round trip moved the receipt digest")
		}
		if err := roundTrip.VerifyWithKey(signer.PublicKey()); err != nil {
			t.Fatalf("exported receipt does not verify: %v", err)
		}
	})
}

// l16RMFindPack returns the first available pack satisfying pred, in
// extractor state order, so mutant selection is deterministic.
func l16RMFindPack(t *testing.T, what string, pred func(legal.RulePack) bool) (string, legal.RulePack) {
	t.Helper()
	for _, state := range extract.States {
		pack := l16LoadPack(t, state.Code)
		if pred(pack) {
			return state.Code, pack
		}
	}
	t.Fatalf("no available pack %s", what)
	return "", legal.RulePack{}
}

// l16RMEvalSingle evaluates the canonical proposal against one pack alone in
// a fresh registry. Isolation keeps a mutant's blast radius inside the
// subtest: no other state's content can mask the seeded change.
func l16RMEvalSingle(t *testing.T, pack legal.RulePack) legal.LegalEvaluationReceipt {
	t.Helper()
	registry := legal.NewRegistry()
	if err := registry.Register(pack); err != nil {
		t.Fatalf("Register mutant: %v", err)
	}
	proposal := canonicalProposal(t)
	return l16Evaluate(t, registry, l16Signer(t), pack.Jurisdiction.State, proposal.EffectiveDate, proposal)
}

// TestTodo_LEGAL_016_Mutation seeds semantic mutants into in-memory clones of
// available packs and asserts the evaluation notices every one: a dropped
// duty kind, a changed floor amount, a removed preemption, an inverted pay
// direction and an out-of-window release must each move the receipt or fail
// closed. A mutant that evaluates identically to its baseline survives, and
// the subtest fails.
func TestTodo_LEGAL_016_Mutation(t *testing.T) {
	t.Run("dropped transparency duties move the receipt", func(t *testing.T) {
		_, base := l16RMFindPack(t, "carrying pay-transparency duties",
			func(p legal.RulePack) bool { return len(p.PayTransparencyDuties) > 0 })
		baseline := l16RMEvalSingle(t, base)
		mut := base
		mut.PayTransparencyDuties = nil
		mutated := l16RMEvalSingle(t, mut)
		if bytes.Equal(baseline.CanonicalBytes(), mutated.CanonicalBytes()) {
			t.Fatal("dropping every transparency duty left the receipt identical; the mutant survives")
		}
		if baseline.Digest == mutated.Digest {
			t.Fatal("dropping every transparency duty left the receipt digest identical; the mutant survives")
		}
	})

	t.Run("changed wage floor amount moves the digest", func(t *testing.T) {
		_, base := l16RMFindPack(t, "carrying a stated wage floor amount",
			func(p legal.RulePack) bool {
				return len(p.WageFloors) > 0 && p.WageFloors[0].FloorAmount.String() != ""
			})
		baseline := l16RMEvalSingle(t, base)
		floors := append([]legal.WageFloorRule(nil), base.WageFloors...)
		bump := "99.99"
		if floors[0].FloorAmount.String() == "99.99" {
			bump = "98.99"
		}
		raised, ok := l16RMMoney(9999)
		if bump == "98.99" {
			raised, ok = l16RMMoney(9899)
		}
		if !ok {
			t.Fatal("cannot build the mutant floor amount")
		}
		floors[0].FloorAmount = raised
		mut := base
		mut.WageFloors = floors
		mutated := l16RMEvalSingle(t, mut)
		if baseline.Digest == mutated.Digest {
			t.Fatalf("moving the floor from %q to %q left the digest identical; the mutant survives",
				base.WageFloors[0].FloorAmount.String(), bump)
		}
	})

	t.Run("removed preemption lets the locality duty apply", func(t *testing.T) {
		statePack := l16LoadPack(t, "WI")
		preempted := false
		for _, a := range statePack.PreemptionAssertions {
			if a.Kind == legal.ObligationTypeLeaveInteraction && a.Scope == "LOCALITY_ONLY" {
				preempted = true
			}
		}
		if !preempted {
			t.Fatal("Wisconsin draft carries no locality-only LEAVE preemption to mutate")
		}
		milwaukee := legal.Jurisdiction{Country: "US", State: "WI", Locality: "Milwaukee"}
		window, err := legal.NewOpenEffectiveWindow(l16Date(t, 2026, time.January, 1))
		if err != nil {
			t.Fatalf("overlay window: %v", err)
		}
		overlay := legal.RulePack{
			PackID:            "us-wi-milwaukee-paid-leave-fixture",
			Version:           1,
			Jurisdiction:      milwaukee,
			Window:            window,
			VocabularyVersion: legal.VocabularyVersion2,
			SourceType:        legal.SourceTypeStatute,
			ReviewStatus:      legal.ReviewStatusUnreviewed,
			LeaveInteractions: []legal.LeaveInteraction{{
				ID:              "milwaukee-paid-leave",
				LeaveType:       "accrued paid sick leave",
				InteractionRule: "Hypothetical Milwaukee paid-leave ordinance for the boundary vector.",
				Citation: legal.Citation{
					SourceFile:       "planning/research/state-employment-law/wisconsin.md",
					Section:          "Milwaukee Code ch. 112 (hypothetical ordinance for the boundary vector)",
					Status:           legal.ReviewStatusUnreviewed,
					ConfidenceMarker: legal.ConfidenceMarkerVerify,
				},
			}},
		}
		evalOverlay := func(t *testing.T, state legal.RulePack) legal.LegalEvaluationReceipt {
			t.Helper()
			registry := legal.NewRegistry()
			if err := registry.Register(state); err != nil {
				t.Fatalf("Register(state): %v", err)
			}
			if err := registry.Register(overlay); err != nil {
				t.Fatalf("Register(overlay): %v", err)
			}
			signer := l16Signer(t)
			proposal := canonicalProposal(t)
			effective := l16Date(t, 2026, time.March, 1)
			known, err := values.NewKnownAt(l16Instant(t, 1_770_000_000))
			if err != nil {
				t.Fatalf("NewKnownAt: %v", err)
			}
			at := l16Instant(t, 1_770_200_000)
			ctx, err := legal.Resolve(legal.LegalContextInput{
				LegalEntityID:          "legal-016-canonical-entity",
				WorkLocation:           milwaukee,
				EmploymentJurisdiction: milwaukee,
				EffectiveDate:          effective,
				KnownAt:                known,
			}, registry, signer, at)
			if err != nil {
				t.Fatalf("Resolve(Milwaukee): %v", err)
			}
			receipt, err := legal.EvaluateReceipt(ctx, proposal, registry, signer, at)
			if err != nil {
				t.Fatalf("EvaluateReceipt(Milwaukee): %v", err)
			}
			return receipt
		}
		baseline := evalOverlay(t, statePack)
		if l16AppliedIDs(baseline)["milwaukee-paid-leave"] {
			t.Fatal("baseline overlay receipt applies the preempted duty; the boundary is already broken")
		}
		mut := statePack
		mut.PreemptionAssertions = nil
		mutated := evalOverlay(t, mut)
		if !l16AppliedIDs(mutated)["milwaukee-paid-leave"] {
			t.Fatal("removing the preemption left the locality duty removed; the mutant survives")
		}
		if bytes.Equal(baseline.CanonicalBytes(), mutated.CanonicalBytes()) {
			t.Fatal("removing the preemption left the receipt identical; the mutant survives")
		}
	})

	t.Run("removed pay change moves the digest", func(t *testing.T) {
		pack := l16LoadPack(t, "CA")
		rise := l16RMEvalSingle(t, pack)
		proposal := canonicalProposal(t)
		proposal.NewBasePay = proposal.CurrentBasePay
		registry := legal.NewRegistry()
		if err := registry.Register(pack); err != nil {
			t.Fatalf("Register(CA): %v", err)
		}
		flat := l16Evaluate(t, registry, l16Signer(t), "CA", proposal.EffectiveDate, proposal)
		if rise.Digest == flat.Digest {
			t.Fatal("a pay change and no pay change share a receipt digest; the notice-trigger mutant survives")
		}
	})

	t.Run("out-of-window release fails closed", func(t *testing.T) {
		pack := l16LoadPack(t, "CA")
		closed, err := legal.NewClosedEffectiveWindow(l16Date(t, 2020, time.January, 1), l16Date(t, 2020, time.June, 30))
		if err != nil {
			t.Fatalf("closed window: %v", err)
		}
		mut := pack
		mut.Window = closed
		registry := legal.NewRegistry()
		if err := registry.Register(mut); err != nil {
			t.Fatalf("Register(mutant): %v", err)
		}
		signer := l16Signer(t)
		proposal := canonicalProposal(t)
		known, err := values.NewKnownAt(l16Instant(t, 1_770_000_000))
		if err != nil {
			t.Fatalf("NewKnownAt: %v", err)
		}
		_, err = legal.Resolve(legal.LegalContextInput{
			LegalEntityID:          proposal.LegalEntityID,
			WorkLocation:           mut.Jurisdiction,
			EmploymentJurisdiction: mut.Jurisdiction,
			EffectiveDate:          proposal.EffectiveDate,
			KnownAt:                known,
		}, registry, signer, l16Instant(t, 1_770_200_000))
		if err == nil {
			t.Fatal("out-of-window mutant resolved; want no effective release")
		}
		if !errors.Is(err, legal.ErrLegalContextUnknown) {
			t.Fatalf("out-of-window resolve error = %v, want the typed unknown-context sentinel", err)
		}
	})
}
