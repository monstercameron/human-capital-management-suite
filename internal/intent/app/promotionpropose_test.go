package app

import (
	"errors"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// promotionProposeFixture is one complete, valid intent-only promotion.propose
// request for the corpus worker every journey fixture in this package uses.
//
// It is deliberately a request that says everything a caller may say,
// optional fields included, so that a test which drops one field is testing
// that field and not an omission it inherited from the fixture.
func promotionProposeFixture() *journeyv1.ProposePromotionRequest {
	return &journeyv1.ProposePromotionRequest{
		SubjectWorkerRef:        "omar-reyes",
		DesiredJobCode:          "OPS-HRBP3",
		DesiredGrade:            "P3",
		DesiredPositionId:       "POS-HRBP-301",
		DesiredOrgUnit:          "people-ops",
		DesiredManagerRef:       "rel_mgr_9f2a",
		DesiredBasePay:          "98000.00",
		DesiredPayCurrency:      "USD",
		EffectiveDate:           "2026-06-01",
		Reason:                  "promotion_into_senior_hrbp",
		ExpectedSubjectRevision: PromotionSubjectRevision("omar-reyes"),
		ClientRequestId:         "req-0191f3c4-1",
	}
}

// smuggledFieldBytes is one syntactically valid but undefined Protobuf field,
// hand-encoded so that this package still names no Protobuf runtime type.
//
// It is field number 900, wire type 2 (length-delimited), carrying the text
// "93000.00" - which is exactly the shape of the smuggling this contract
// exists to refuse: a caller asserting the subject's current base pay in a
// message that has nowhere to put it.
//
//	key   = 900<<3 | 2 = 7202, varint 0xA2 0x38
//	len   = 8,             byte 0x08
//	value = "93000.00"
var smuggledFieldBytes = append([]byte{0xA2, 0x38, 0x08}, []byte("93000.00")...)

// withSmuggledField returns req carrying those undefined bytes, exactly as a
// decoded wire message from a caller that appended them would.
func withSmuggledField(req *journeyv1.ProposePromotionRequest) *journeyv1.ProposePromotionRequest {
	// SetUnknown takes protoreflect.RawFields, whose underlying type is
	// []byte; passing an unnamed []byte is assignable to it, so this stays
	// inside LIB-003's rule that internal/intent/app imports no Protobuf
	// runtime package.
	req.ProtoReflect().SetUnknown(smuggledFieldBytes)
	return req
}

// ---------------------------------------------------------------------------
// PRIMARY
// ---------------------------------------------------------------------------

// TestTodo_PROMO_007 is the contract itself: the generated request message
// accepts a caller's intention and nothing else.
//
// Three claims, and they are separate. The message's field set is exactly the
// closed allowlist, so nothing server-owned has been added to the .proto
// without being reconciled here. None of those field names is a server-owned
// fact or a security-context selector, checked by name rather than by
// inspection, so "we looked and it seemed fine" is not the evidence. And a
// complete request validates while an incomplete one is refused by field
// name, including the two fields - the expected subject revision and the
// client request id - whose absence is the difference between a governed
// proposal and an unattributable one.
func TestTodo_PROMO_007(t *testing.T) {
	t.Run("the message's fields are exactly the closed allowlist", func(t *testing.T) {
		if owned := promotionProposeContractError(); owned != nil {
			t.Fatalf("the generated promotion.propose request has drifted from the contract: %v", owned)
		}
		got := promotionProposeMessageFields()
		want := PromotionProposeContractFields()
		if len(got) != len(want) {
			t.Fatalf("message fields = %v, allowlist = %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("field %d = %q, want %q (the allowlist also fixes the digest order)", i, got[i], want[i])
			}
		}
	})

	t.Run("no field names a server-owned fact or a security context", func(t *testing.T) {
		// Every token here names something this cell resolves itself: the
		// subject's current state, the finance and position authority behind
		// the change, or the identity the change is made under. A field whose
		// name contains one is a field a caller could use to assert it.
		forbidden := []string{
			"current", "salary", "vacancy", "headcount", "budget", "authority",
			"approver", "principal", "tenant", "organization_scope", "session",
			"delegation", "purpose", "locale", "jurisdiction", "legal",
			"initiator", "actor", "role", "assurance", "classification",
		}
		for _, field := range promotionProposeMessageFields() {
			for _, token := range forbidden {
				if strings.Contains(field, token) {
					t.Errorf("field %q names %q, which is a server-resolved fact or a security context", field, token)
				}
			}
		}
	})

	t.Run("a complete request validates", func(t *testing.T) {
		if err := validatePromotionPropose(promotionProposeFixture()); err != nil {
			t.Fatalf("validatePromotionPropose(complete) = %v, want nil", err)
		}
	})

	t.Run("a missing required field is refused by name", func(t *testing.T) {
		clear := map[string]func(*journeyv1.ProposePromotionRequest){
			"subject_worker_ref":        func(r *journeyv1.ProposePromotionRequest) { r.SubjectWorkerRef = "  " },
			"desired_job_code":          func(r *journeyv1.ProposePromotionRequest) { r.DesiredJobCode = "" },
			"desired_grade":             func(r *journeyv1.ProposePromotionRequest) { r.DesiredGrade = "" },
			"desired_base_pay":          func(r *journeyv1.ProposePromotionRequest) { r.DesiredBasePay = "" },
			"effective_date":            func(r *journeyv1.ProposePromotionRequest) { r.EffectiveDate = "" },
			"reason":                    func(r *journeyv1.ProposePromotionRequest) { r.Reason = "" },
			"expected_subject_revision": func(r *journeyv1.ProposePromotionRequest) { r.ExpectedSubjectRevision = "" },
			"client_request_id":         func(r *journeyv1.ProposePromotionRequest) { r.ClientRequestId = "" },
		}
		for field, mutate := range clear {
			t.Run(field, func(t *testing.T) {
				req := promotionProposeFixture()
				mutate(req)
				err := validatePromotionPropose(req)
				if !errors.Is(err, workspace.ErrJourneyInput) {
					t.Fatalf("validatePromotionPropose = %v, want ErrJourneyInput", err)
				}
				if !strings.Contains(err.Error(), field) {
					t.Fatalf("refusal %q does not name %q", err, field)
				}
			})
		}
	})

	t.Run("the four optional fields stay optional", func(t *testing.T) {
		req := promotionProposeFixture()
		req.DesiredPositionId = ""
		req.DesiredOrgUnit = ""
		req.DesiredManagerRef = ""
		req.DesiredPayCurrency = ""
		if err := validatePromotionPropose(req); err != nil {
			t.Fatalf("validatePromotionPropose(no optionals) = %v, want nil", err)
		}
	})

	t.Run("a malformed effective date is refused by name", func(t *testing.T) {
		req := promotionProposeFixture()
		req.EffectiveDate = "01/06/2026"
		err := validatePromotionPropose(req)
		if !errors.Is(err, workspace.ErrJourneyInput) || !strings.Contains(err.Error(), "effective_date") {
			t.Fatalf("validatePromotionPropose(bad date) = %v, want ErrJourneyInput naming effective_date", err)
		}
	})

	t.Run("the declared subject revision is the coordinate the payload records", func(t *testing.T) {
		// PromotionSubjectRevision is what a client is told to send and what
		// the engine compares against; it has to be the same coordinate the
		// request payload's own compensation sides carry, or a caller would
		// be asked for a revision the proposal does not record.
		want := promotionRevisionStream("omar-reyes") + "@" + promotionRevisionSequence
		if got := PromotionSubjectRevision("omar-reyes"); got != want {
			t.Fatalf("PromotionSubjectRevision = %q, want %q", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// SECURITY
// ---------------------------------------------------------------------------

// TestTodo_PROMO_007_Security is the smuggling half of "intent-only": with
// nowhere in the message to assert a server-owned fact, the remaining move is
// to append bytes the contract does not define, and that is refused rather
// than ignored.
//
// Ignoring them would be worse than refusing them. A caller that appended a
// current base pay and got a proposal back would have no way to tell whether
// the number was used, and would be entitled to believe it was.
func TestTodo_PROMO_007_Security(t *testing.T) {
	t.Run("unknown wire fields are refused", func(t *testing.T) {
		err := validatePromotionPropose(withSmuggledField(promotionProposeFixture()))
		owned, ok := envelope.As(err)
		if !ok {
			t.Fatalf("validatePromotionPropose(smuggled) = %v, want an owned refusal", err)
		}
		if owned.Code() != envelope.CodeInvalidArgument {
			t.Fatalf("Code() = %s, want INVALID_ARGUMENT", owned.Code())
		}
		if owned.ReasonRef() != reasonPromotionSmuggledFields {
			t.Fatalf("ReasonRef() = %q, want %q", owned.ReasonRef(), reasonPromotionSmuggledFields)
		}
	})

	t.Run("the smuggled bytes are seen, not silently dropped", func(t *testing.T) {
		clean := promotionProposeFixture()
		if n := promotionProposeUnknownBytes(clean); n != 0 {
			t.Fatalf("a clean request already carries %d unknown bytes", n)
		}
		if n := promotionProposeUnknownBytes(withSmuggledField(clean)); n != len(smuggledFieldBytes) {
			t.Fatalf("unknown bytes = %d, want %d", n, len(smuggledFieldBytes))
		}
	})

	t.Run("a smuggled fact never reaches the digest", func(t *testing.T) {
		// The canonical digest is over the contract's own fields, so bytes
		// that were refused could not have changed what a proposal is bound
		// to even if the refusal were removed.
		if got, want := PromotionProposeRequestDigest(withSmuggledField(promotionProposeFixture())),
			PromotionProposeRequestDigest(promotionProposeFixture()); got != want {
			t.Fatalf("smuggled bytes changed the request digest: %s != %s", got, want)
		}
	})

	t.Run("a contract drift fails closed", func(t *testing.T) {
		// The structural check is what makes the absence of server-owned
		// fields a property of the build. Simulating a drift - the allowlist
		// naming a field the message does not have - must produce a refusal,
		// not a shrug.
		saved := promotionProposeContractFields
		t.Cleanup(func() { promotionProposeContractFields = saved })
		promotionProposeContractFields = append(append([]string{}, saved...), "current_base_pay")
		owned := checkPromotionProposeContract()
		if owned == nil {
			t.Fatal("a contract naming a field the message does not carry was accepted")
		}
		if owned.ReasonRef() != reasonPromotionContractDrift {
			t.Fatalf("ReasonRef() = %q, want %q", owned.ReasonRef(), reasonPromotionContractDrift)
		}
	})
}

// ---------------------------------------------------------------------------
// GOLDEN
// ---------------------------------------------------------------------------

// promotionProposeGoldenDigest is the pinned canonical digest of
// [promotionProposeFixture].
//
// It is pinned because the digest is what a proposal's identity is compared
// by across transports and across releases: if it can change without anybody
// noticing, then "the browser and the connector proposed the same promotion"
// is not a checkable statement. A deliberate change to the contract's field
// set, field order or encoding changes this value, and changing it is the
// point at which somebody has to say why.
const promotionProposeGoldenDigest = "sha256:23386b0b8aee1f7cf2c995feacfcc2ec3c19cb1ec8ec00d92b07669b51697b8e"

// TestTodo_PROMO_007_Golden pins the canonical request digest and the
// properties that make it worth pinning.
func TestTodo_PROMO_007_Golden(t *testing.T) {
	t.Run("the digest of the fixture is the pinned one", func(t *testing.T) {
		if got := PromotionProposeRequestDigest(promotionProposeFixture()); got != promotionProposeGoldenDigest {
			t.Fatalf("PromotionProposeRequestDigest = %q, want the pinned %q", got, promotionProposeGoldenDigest)
		}
	})

	t.Run("the digest is stable across equal requests", func(t *testing.T) {
		firstDigest, secondDigest := PromotionProposeRequestDigest(promotionProposeFixture()),
			PromotionProposeRequestDigest(promotionProposeFixture())
		if firstDigest != secondDigest {
			t.Fatal("two equal requests digested differently")
		}
	})

	t.Run("surrounding whitespace is not content", func(t *testing.T) {
		padded := promotionProposeFixture()
		padded.Reason = "  " + padded.Reason + "\t"
		if got, want := PromotionProposeRequestDigest(padded),
			PromotionProposeRequestDigest(promotionProposeFixture()); got != want {
			t.Fatalf("padding changed the digest: %s != %s", got, want)
		}
	})

	t.Run("the client request id is not content", func(t *testing.T) {
		other := promotionProposeFixture()
		other.ClientRequestId = "req-0191f3c4-2"
		if got, want := PromotionProposeRequestDigest(other),
			PromotionProposeRequestDigest(promotionProposeFixture()); got != want {
			t.Fatalf("the dedup coordinate changed the content digest: %s != %s", got, want)
		}
	})

	t.Run("field boundaries are not forgeable", func(t *testing.T) {
		// The encoding is length-prefixed rather than delimited precisely so
		// that shifting text across a field boundary is a different request.
		// A delimited encoding would digest these two identically.
		left := promotionProposeFixture()
		left.DesiredJobCode = "OPS-HRBP3X"
		left.DesiredGrade = "P3"
		right := promotionProposeFixture()
		right.DesiredJobCode = "OPS-HRBP3"
		right.DesiredGrade = "XP3"
		if PromotionProposeRequestDigest(left) == PromotionProposeRequestDigest(right) {
			t.Fatal("two different requests share one digest")
		}
	})
}

// ---------------------------------------------------------------------------
// MUTATION
// ---------------------------------------------------------------------------

// TestTodo_PROMO_007_Mutation proves each part of the contract is
// load-bearing: every field a caller may state changes the canonical digest,
// every required field's absence is refused, and the two checks that guard
// the contract itself each fire on their own.
//
// A digest that ignored a field would let two different promotions be treated
// as one proposal, which is the failure mode a canonical digest exists to
// prevent; so the mutation matrix is over the fields, one at a time.
func TestTodo_PROMO_007_Mutation(t *testing.T) {
	mutate := map[string]func(*journeyv1.ProposePromotionRequest){
		"subject_worker_ref":        func(r *journeyv1.ProposePromotionRequest) { r.SubjectWorkerRef = "lena-reyes" },
		"desired_job_code":          func(r *journeyv1.ProposePromotionRequest) { r.DesiredJobCode = "ENG-MGR1" },
		"desired_grade":             func(r *journeyv1.ProposePromotionRequest) { r.DesiredGrade = "M1" },
		"desired_position_id":       func(r *journeyv1.ProposePromotionRequest) { r.DesiredPositionId = "POS-HRBP-999" },
		"desired_org_unit":          func(r *journeyv1.ProposePromotionRequest) { r.DesiredOrgUnit = "eng-platform" },
		"desired_manager_ref":       func(r *journeyv1.ProposePromotionRequest) { r.DesiredManagerRef = "rel_mgr_0000" },
		"desired_base_pay":          func(r *journeyv1.ProposePromotionRequest) { r.DesiredBasePay = "98000.01" },
		"desired_pay_currency":      func(r *journeyv1.ProposePromotionRequest) { r.DesiredPayCurrency = "EUR" },
		"effective_date":            func(r *journeyv1.ProposePromotionRequest) { r.EffectiveDate = "2026-07-01" },
		"reason":                    func(r *journeyv1.ProposePromotionRequest) { r.Reason = "retention" },
		"expected_subject_revision": func(r *journeyv1.ProposePromotionRequest) { r.ExpectedSubjectRevision = "rewards.package.omar-reyes@2" },
	}
	base := PromotionProposeRequestDigest(promotionProposeFixture())
	for field, apply := range mutate {
		t.Run("digest reacts to "+field, func(t *testing.T) {
			req := promotionProposeFixture()
			apply(req)
			if got := PromotionProposeRequestDigest(req); got == base {
				t.Fatalf("changing %s did not change the canonical request digest", field)
			}
		})
	}

	t.Run("the idempotency coordinate is the client request id", func(t *testing.T) {
		// Two calls carrying the same client request id must key to one
		// intent; two carrying different ones must not.
		firstKey, secondKey := promotionProposeIdempotencyKey("req-a"), promotionProposeIdempotencyKey("req-a")
		if firstKey != secondKey {
			t.Fatal("the same client request id produced two idempotency keys")
		}
		if promotionProposeIdempotencyKey("req-a") == promotionProposeIdempotencyKey("req-b") {
			t.Fatal("two client request ids produced one idempotency key")
		}
	})

	t.Run("a nil request is refused rather than dereferenced", func(t *testing.T) {
		if err := validatePromotionPropose(nil); !errors.Is(err, workspace.ErrJourneyInput) {
			t.Fatalf("validatePromotionPropose(nil) = %v, want ErrJourneyInput", err)
		}
	})

	t.Run("an unexpected message field is reported as drift", func(t *testing.T) {
		saved := promotionProposeContractFields
		t.Cleanup(func() { promotionProposeContractFields = saved })
		// Dropping a field from the allowlist is how "somebody added a field
		// to the .proto and did not reconcile it" looks from here.
		promotionProposeContractFields = saved[:len(saved)-1]
		owned := checkPromotionProposeContract()
		if owned == nil {
			t.Fatal("a message field outside the allowlist was accepted")
		}
		if owned.ReasonRef() != reasonPromotionContractDrift {
			t.Fatalf("ReasonRef() = %q, want %q", owned.ReasonRef(), reasonPromotionContractDrift)
		}
	})

	t.Run("PromotionProposeContractFields hands out a copy", func(t *testing.T) {
		out := PromotionProposeContractFields()
		out[0] = "mutated"
		if promotionProposeContractFields[0] == "mutated" {
			t.Fatal("a caller mutated the enforced allowlist")
		}
	})
}
