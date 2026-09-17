package dsr

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PRIV_006 is the PRIMARY test for planning/todos.md PRIV-006:
// "Resolve every subject-request item and legal exception."
//
// RED (todos.md PRIV-006): "projection/search/vector/cache/telemetry/
// export/backup/provider/privileged/retained copy is omitted or exception
// has no authority."
//
// GREEN (todos.md PRIV-006): "each item returns fulfill/partial/deny/
// restrict/retain/anonymize with authority, redaction reason, completeness
// digest and appeal route."
//
// REFACTOR (todos.md PRIV-006): "unknown copy prevents false completeness."
func TestTodo_PRIV_006(t *testing.T) {
	t.Run("GREEN: erasure over a complete copy set resolves every item with authority, reason, digest and appeal route", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-erase-1", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, allRedClasses())
		res, err := Resolve(spec)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(res.Items) != len(allRedClasses()) {
			t.Fatalf("resolved %d items, want one per presented copy (%d)", len(res.Items), len(allRedClasses()))
		}
		for _, item := range res.Items {
			if item.Outcome == "" || item.Authority == "" || item.AppealRoute == "" {
				t.Errorf("item %q is missing outcome/authority/appeal route: %+v", item.CopyID, item)
			}
			if err := item.Outcome.Validate(); err != nil {
				t.Errorf("item %q outcome invalid: %v", item.CopyID, err)
			}
		}
		if res.EvidenceID == "" || res.EvidenceID != resolutionEvidencePrefix+res.Digest() {
			t.Errorf("EvidenceID = %q, want it to match the resolution's own digest", res.EvidenceID)
		}
		if err := res.Validate(); err != nil {
			t.Errorf("Resolve produced an invalid resolution: %v", err)
		}
	})

	t.Run("GREEN: a held copy is retained under the hold authority, never destroyed", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-erase-held", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyBackup})
		spec.Copies[0].Held = true
		spec.Copies[0].HoldAuthority = "litigation-2026-04"
		res, err := Resolve(spec)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Items[0].Outcome != OutcomeRetain {
			t.Errorf("held copy outcome = %s, want %s", res.Items[0].Outcome, OutcomeRetain)
		}
		if res.Items[0].Authority != "hold:litigation-2026-04" {
			t.Errorf("held copy authority = %q, want the hold authority", res.Items[0].Authority)
		}
	})

	t.Run("GREEN: a legal exception retains with its authority; erasure still fulfills elsewhere", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-erase-exc", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative, CopyBackup})
		spec.Exceptions = []LegalException{{
			CopyIDs:   []string{"copy-backup"},
			Authority: "26-U.S.C.-6103-tax-retention",
			Basis:     ExceptionTaxRetention,
		}}
		res, err := Resolve(spec)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		byID := resolutionByID(res)
		if byID["copy-backup"].Outcome != OutcomeRetain {
			t.Errorf("excepted copy outcome = %s, want %s", byID["copy-backup"].Outcome, OutcomeRetain)
		}
		if byID["copy-backup"].Authority != "26-U.S.C.-6103-tax-retention" {
			t.Errorf("excepted copy authority = %q, want the exception authority", byID["copy-backup"].Authority)
		}
		if byID["copy-authoritative"].Outcome != OutcomeFulfill {
			t.Errorf("unexcepted copy outcome = %s, want %s", byID["copy-authoritative"].Outcome, OutcomeFulfill)
		}
	})

	t.Run("RED: omitting a declared-complete copy class blocks the resolution", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-erase-omit", KindErasure, trust.AssuranceHigh)
		// The caller certifies VECTOR complete for this subject (per the
		// RECORDS-COPY-001 inventory) but presents no VECTOR copy.
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative})
		spec.CompleteClasses = []CopyClass{CopyAuthoritative, CopyVector}
		_, err := Resolve(spec)
		if !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(omitted VECTOR) = %v, want %v", err, ErrResolutionBlocked)
		}
	})

	t.Run("RED: an exception with no authority blocks the resolution", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-erase-noauth", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative})
		spec.Exceptions = []LegalException{{
			CopyIDs: []string{"copy-authoritative"},
			Basis:   ExceptionTaxRetention,
		}}
		_, err := Resolve(spec)
		if !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(authority-less exception) = %v, want %v", err, ErrResolutionBlocked)
		}
	})

	t.Run("RED: an unverified request never resolves", func(t *testing.T) {
		req := fixtureRequest(t) // UNVERIFIED
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative})
		_, err := Resolve(spec)
		if !errors.Is(err, ErrResolutionRefused) {
			t.Fatalf("Resolve(unverified) = %v, want %v", err, ErrResolutionRefused)
		}
	})

	t.Run("RED: a duplicate-linked request never resolves, even when verified", func(t *testing.T) {
		first := fixtureRequest(t)
		second, err := Intake(fixtureIntakeSpec(t, "dsr-dup", KindAccess), DefaultClockTable(), []DataSubjectRequest{first}, fxWindow)
		if err != nil {
			t.Fatalf("Intake: %v", err)
		}
		verified, err := second.Verify(fixtureEvidence(t, trust.AssuranceHigh))
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		spec := fixtureResolutionSpec(t, verified, []CopyClass{CopyAuthoritative})
		_, err = Resolve(spec)
		if !errors.Is(err, ErrResolutionRefused) {
			t.Fatalf("Resolve(duplicate) = %v, want %v", err, ErrResolutionRefused)
		}
	})

	t.Run("REFACTOR: an unknown copy class blocks instead of certifying false completeness", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-erase-unknown", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyAuthoritative})
		spec.Copies = append(spec.Copies, CopyDescriptor{
			CopyID:     "copy-mystery",
			Class:      CopyClass("AGENT_MEMORY"),
			Tenant:     fxTenant,
			SubjectKey: fxClaims().Key(),
			Capability: DeletionDelete,
		})
		_, err := Resolve(spec)
		if !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(unknown class) = %v, want %v", err, ErrResolutionBlocked)
		}
	})
}
