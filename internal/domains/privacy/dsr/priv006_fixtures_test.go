package dsr

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// --- PRIV-006 shared fixtures ------------------------------------------------
//
// Every PRIV-006 matrix test starts from one verified, advanceable request
// plus a copy inventory about that request's subject, then perturbs one
// thing at a time.

// allRedClasses returns exactly the ten copy classes PRIV-006's RED clause
// enumerates: projection, search, vector, cache, telemetry, export,
// backup, provider, privileged and retained.
func allRedClasses() []CopyClass {
	return []CopyClass{
		CopyProjection,
		CopySearch,
		CopyVector,
		CopyCache,
		CopyTelemetry,
		CopyExport,
		CopyBackup,
		CopyProvider,
		CopyPrivileged,
		CopyRetained,
	}
}

func copyIDForClass(class CopyClass) string {
	return "copy-" + strings.ToLower(string(class))
}

func fixtureVerifiedRequest(t *testing.T, id string, kind Kind, assurance trust.Assurance) DataSubjectRequest {
	t.Helper()
	req, err := Intake(fixtureIntakeSpec(t, id, kind), DefaultClockTable(), nil, fxWindow)
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	verified, err := req.Verify(fixtureEvidence(t, assurance))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if advance, code := verified.CanAdvance(); !advance {
		t.Fatalf("fixture request cannot advance (code=%s): fix the fixture, not the test", code)
	}
	return verified
}

// fixtureResolutionSpec builds a resolution spec for req over one DELETE
// capable copy per class, declaring exactly those classes complete. At is
// fixed after verification so every matrix test shares one digest basis.
func fixtureResolutionSpec(t *testing.T, req DataSubjectRequest, classes []CopyClass) ResolutionSpec {
	t.Helper()
	copies := make([]CopyDescriptor, 0, len(classes))
	for _, class := range classes {
		copies = append(copies, CopyDescriptor{
			CopyID:     copyIDForClass(class),
			Class:      class,
			Tenant:     req.Tenant,
			SubjectKey: req.Claims.Key(),
			Capability: DeletionDelete,
		})
	}
	return ResolutionSpec{
		Request:         req,
		Copies:          copies,
		CompleteClasses: append([]CopyClass(nil), classes...),
		At:              mustInstant(t, fxVerifiedAt+50),
	}
}

func resolutionByID(res Resolution) map[string]ItemResolution {
	out := make(map[string]ItemResolution, len(res.Items))
	for _, item := range res.Items {
		out[item.CopyID] = item
	}
	return out
}
