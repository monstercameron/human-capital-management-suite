package dsr

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// instantOneSecondBefore returns the instant one second before unixSec. It
// keeps the mutation test's time-boundary setup next to the test instead of
// hiding it in shared fixtures.
func instantOneSecondBefore(unixSec int64) (values.Instant, error) {
	return values.NewInstantFromUnix(unixSec-1, 0)
}

// TestTodo_PRIV_006_Mutation is the MUTATION matrix test for PRIV-006. Each
// case flips one semantic input across a decision boundary and proves the
// certificate moves with it: a boundary that does not move the outcome is
// a seeded mutant this suite would let survive.
func TestTodo_PRIV_006_Mutation(t *testing.T) {
	t.Run("held versus unheld flips RETAIN to FULFILL for erasure", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-mut-held", KindErasure, trust.AssuranceHigh)
		held := fixtureResolutionSpec(t, req, []CopyClass{CopyBackup})
		held.Copies[0].Held = true
		held.Copies[0].HoldAuthority = "hold-1"
		heldRes, err := Resolve(held)
		if err != nil {
			t.Fatalf("Resolve(held): %v", err)
		}
		free, err := Resolve(fixtureResolutionSpec(t, req, []CopyClass{CopyBackup}))
		if err != nil {
			t.Fatalf("Resolve(unheld): %v", err)
		}
		if heldRes.Items[0].Outcome != OutcomeRetain {
			t.Errorf("held outcome = %s, want %s", heldRes.Items[0].Outcome, OutcomeRetain)
		}
		if free.Items[0].Outcome != OutcomeFulfill {
			t.Errorf("unheld outcome = %s, want %s", free.Items[0].Outcome, OutcomeFulfill)
		}
		if heldRes.Digest() == free.Digest() {
			t.Error("held and unheld resolutions digest identically: the hold bit is not bound into the certificate")
		}
	})

	t.Run("an immutable copy retains only under a named retention authority", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-mut-immutable", KindErasure, trust.AssuranceHigh)
		anonymous := fixtureResolutionSpec(t, req, []CopyClass{CopyRetained})
		anonymous.Copies[0].Capability = DeletionNone
		if _, err := Resolve(anonymous); !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(immutable, no retention authority) = %v, want %v", err, ErrResolutionBlocked)
		}
		named := fixtureResolutionSpec(t, req, []CopyClass{CopyRetained})
		named.Copies[0].Capability = DeletionNone
		named.Copies[0].RetentionAuthority = "schedule-7y"
		res, err := Resolve(named)
		if err != nil {
			t.Fatalf("Resolve(immutable, named authority): %v", err)
		}
		if res.Items[0].Outcome != OutcomeRetain || res.Items[0].Authority != "retention:schedule-7y" {
			t.Errorf("immutable item = (%s, %q), want (RETAIN, retention:schedule-7y)", res.Items[0].Outcome, res.Items[0].Authority)
		}
	})

	t.Run("redacting versus non-redacting flips PARTIAL to FULFILL for access", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-mut-redact", KindAccess, trust.AssuranceSubstantial)
		redacting := fixtureResolutionSpec(t, req, []CopyClass{CopyExport})
		redacting.Exceptions = []LegalException{{
			CopyIDs: []string{"copy-export"}, Authority: "in-re-4417",
			Basis: ExceptionLitigation, RedactsAccess: true,
		}}
		redacted, err := Resolve(redacting)
		if err != nil {
			t.Fatalf("Resolve(redacting): %v", err)
		}
		silent := fixtureResolutionSpec(t, req, []CopyClass{CopyExport})
		silent.Exceptions = []LegalException{{
			CopyIDs: []string{"copy-export"}, Authority: "26-U.S.C.-6103",
			Basis: ExceptionTaxRetention, RedactsAccess: false,
		}}
		whole, err := Resolve(silent)
		if err != nil {
			t.Fatalf("Resolve(non-redacting): %v", err)
		}
		if redacted.Items[0].Outcome != OutcomePartial {
			t.Errorf("redacting outcome = %s, want %s", redacted.Items[0].Outcome, OutcomePartial)
		}
		if whole.Items[0].Outcome != OutcomeFulfill {
			t.Errorf("non-redacting outcome = %s, want %s", whole.Items[0].Outcome, OutcomeFulfill)
		}
	})

	t.Run("tax retention claiming to redact access is contradictory and blocks", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-mut-tax", KindAccess, trust.AssuranceSubstantial)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyExport})
		spec.Exceptions = []LegalException{{
			CopyIDs: []string{"copy-export"}, Authority: "26-U.S.C.-6103",
			Basis: ExceptionTaxRetention, RedactsAccess: true,
		}}
		if _, err := Resolve(spec); !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(tax redaction) = %v, want %v", err, ErrResolutionBlocked)
		}
	})

	t.Run("overlapping exceptions on one copy block as ambiguous authority", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-mut-overlap", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyBackup})
		spec.Exceptions = []LegalException{
			{CopyIDs: []string{"copy-backup"}, Authority: "ground-a", Basis: ExceptionTaxRetention},
			{CopyIDs: []string{"copy-backup"}, Authority: "ground-b", Basis: ExceptionPublicSafety},
		}
		if _, err := Resolve(spec); !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(overlapping exceptions) = %v, want %v", err, ErrResolutionBlocked)
		}
	})

	t.Run("a dangling exception covering no presented copy blocks", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-mut-dangle", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyBackup})
		spec.Exceptions = []LegalException{
			{CopyIDs: []string{"copy-elsewhere"}, Authority: "ground-a", Basis: ExceptionTaxRetention},
		}
		if _, err := Resolve(spec); !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(dangling exception) = %v, want %v", err, ErrResolutionBlocked)
		}
	})

	t.Run("resolution at the intake instant is allowed, one instant before is not", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-mut-time", KindErasure, trust.AssuranceHigh)
		atIntake := fixtureResolutionSpec(t, req, []CopyClass{CopyBackup})
		atIntake.At = req.ReceivedAt
		if _, err := Resolve(atIntake); err != nil {
			t.Errorf("Resolve(at == received_at): %v, want success (the boundary is inclusive)", err)
		}
		sec, _ := req.ReceivedAt.Unix()
		before, err := instantOneSecondBefore(sec)
		if err != nil {
			t.Fatalf("test setup: %v", err)
		}
		tooEarly := fixtureResolutionSpec(t, req, []CopyClass{CopyBackup})
		tooEarly.At = before
		if _, err := Resolve(tooEarly); !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(before intake) = %v, want %v", err, ErrResolutionBlocked)
		}
	})

	t.Run("dropping one copy changes the completeness digest", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-mut-count", KindErasure, trust.AssuranceHigh)
		full, err := Resolve(fixtureResolutionSpec(t, req, allRedClasses()))
		if err != nil {
			t.Fatalf("Resolve(full): %v", err)
		}
		subset := allRedClasses()[:len(allRedClasses())-1]
		partial, err := Resolve(fixtureResolutionSpec(t, req, subset))
		if err != nil {
			t.Fatalf("Resolve(subset): %v", err)
		}
		if full.Digest() == partial.Digest() {
			t.Error("full and subset resolutions digest identically: the item count is not bound into the certificate")
		}
	})
}
