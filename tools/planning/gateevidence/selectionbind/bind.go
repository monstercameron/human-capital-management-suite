// Package selectionbind makes NEXT-002's P1A manifest and P1B template
// genuinely selection-bound. [LiveDigest] and [VerifyBindings] recompute the
// digest of every artifact a signed manifest binds and refuse a stale
// binding; [Evaluate] computes selection completeness from the selecting
// artifacts' own real-selection and readiness gates
// (pilotprovider.SatisfiesRealProviderSelectionGate,
// legal.ReviewStatus.Releasable, pilotblueprint.Instantiate,
// pilotcommercial.ConformsToLiveRegistries, topology.Compile,
// threatregister.ReleaseDecision) rather than trusting anything the manifest
// asserts about them.
//
// It lives in its own package because every selecting package imports
// tools/planning/gateevidence for signing; importing them from gateevidence
// itself would be an import cycle.
//
// The PHASE-001 scope ceiling's four selection slots are never filled in the
// ceiling (its own Validate refuses that, and a quietly filled slot breaks
// its signature). [Evaluate] resolves each slot here instead, from the bound
// artifacts' gates, which is PHASE-001 REFACTOR's "concrete selection fills
// slots through NEXT-002".
package selectionbind

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotblueprint"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotcommercial"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister"
)

// expectedDigestKind is the digest kind each bound todo's artifact must use.
// Signed YAML artifacts bind their CanonicalDigest; TOPOLOGY-001 has no
// canonical YAML projection, so it binds raw file bytes.
var expectedDigestKind = map[string]string{
	"PHASE-001":      gateevidence.DigestKindCanonicalJSON,
	"SELECT-001":     gateevidence.DigestKindCanonicalJSON,
	"SELECT-002":     gateevidence.DigestKindCanonicalJSON,
	"CUSTOMER-001":   gateevidence.DigestKindCanonicalJSON,
	"TOPOLOGY-001":   gateevidence.DigestKindFileSHA256,
	"COMMERCIAL-001": gateevidence.DigestKindCanonicalJSON,
	"THREAT-001":     gateevidence.DigestKindCanonicalJSON,
}

// resolve joins a validated repository-relative binding path onto root.
func resolve(root, path string) (string, error) {
	if !gateevidence.ValidBindingPath(path) {
		return "", fmt.Errorf("binding path %q is not a clean repository-relative path", path)
	}
	return filepath.Join(root, filepath.FromSlash(path)), nil
}

func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// LiveDigest recomputes the digest binding b names, from the artifact on
// disk under root, using the digest kind the bound todo requires.
func LiveDigest(root string, b gateevidence.SelectionBinding) (string, error) {
	want, known := expectedDigestKind[b.TodoID]
	if !known {
		return "", fmt.Errorf("%s is not a todo a P1A manifest may bind", b.TodoID)
	}
	if b.DigestKind != want {
		return "", fmt.Errorf("%s must bind with digest kind %s, got %q", b.TodoID, want, b.DigestKind)
	}
	path, err := resolve(root, b.Path)
	if err != nil {
		return "", err
	}
	if want == gateevidence.DigestKindFileSHA256 {
		return fileSHA256(path)
	}
	switch b.TodoID {
	case "PHASE-001":
		m, err := scopeceiling.LoadManifest(path)
		if err != nil {
			return "", err
		}
		return m.CanonicalDigest()
	case "SELECT-001":
		p, err := pilotjurisdiction.LoadProfile(path)
		if err != nil {
			return "", err
		}
		return p.CanonicalDigest()
	case "SELECT-002":
		t, err := pilotprovider.LoadTopology(path)
		if err != nil {
			return "", err
		}
		return t.CanonicalDigest()
	case "CUSTOMER-001":
		bp, err := pilotblueprint.LoadBlueprint(path)
		if err != nil {
			return "", err
		}
		return bp.CanonicalDigest()
	case "COMMERCIAL-001":
		f, err := pilotcommercial.LoadFreeze(path)
		if err != nil {
			return "", err
		}
		return f.CanonicalDigest()
	default: // THREAT-001, the only remaining canonical kind
		r, err := threatregister.LoadRegister(path)
		if err != nil {
			return "", err
		}
		return r.CanonicalDigest()
	}
}

// VerifyBindings recomputes every binding's live digest under root and
// returns one violation per binding that is unreadable or stale. A stale
// binding means the signed manifest pins a selection that no longer exists
// on disk; it must be re-bound and re-signed, never silently accepted.
func VerifyBindings(root string, bindings []gateevidence.SelectionBinding) []gateevidence.Violation {
	var violations []gateevidence.Violation
	for i, b := range bindings {
		field := fmt.Sprintf("selection_bindings[%d](%s)", i, b.TodoID)
		live, err := LiveDigest(root, b)
		if err != nil {
			violations = append(violations, gateevidence.Violation{Field: field, Issue: fmt.Sprintf("cannot recompute live digest: %v", err)})
			continue
		}
		if live != b.Digest {
			violations = append(violations, gateevidence.Violation{Field: field, Issue: fmt.Sprintf("stale binding: %s pins %s but the live artifact digests to %s", b.Path, b.Digest, live)})
		}
	}
	return violations
}
