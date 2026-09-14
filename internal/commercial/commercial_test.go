package commercial

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

// TestP1AManifestDigestIsTheLiveSignedManifestDigest ties the golden
// P1AManifestDigest to the signed manifest it names: the checked-in
// p1a-manifest.yaml must verify, and its live CanonicalDigest must equal the
// constant, so re-signing the manifest without updating this package fails
// here instead of drifting silently.
func TestP1AManifestDigestIsTheLiveSignedManifestDigest(t *testing.T) {
	m, err := gateevidence.LoadP1AManifest("../../" + gateevidence.P1AManifestPath)
	if err != nil {
		t.Fatalf("LoadP1AManifest: %v", err)
	}
	if ok, err := gateevidence.VerifyManifestSignature(*m); err != nil || !ok {
		t.Fatalf("checked-in P1A manifest does not verify: ok=%v err=%v", ok, err)
	}
	live, err := m.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if live != P1AManifestDigest {
		t.Fatalf("P1AManifestDigest = %s, but the signed P1A manifest digests to %s - update the golden constant with the manifest", P1AManifestDigest, live)
	}
	if got := DefaultPilotCommercialPackage().ManifestDigest; got != live {
		t.Fatalf("DefaultPilotCommercialPackage binds %s, live manifest is %s", got, live)
	}
}

func TestPilotCommercialPackageMatchesReleaseEntitlementsCostsRisksAndExitTerms(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Release != ReleaseP1A || p.ManifestDigest != P1AManifestDigest {
		t.Fatalf("package is not bound to signed P1A manifest")
	}
	if len(p.Entitlements) != 8 || p.Pricing.MinimumCents != 1500000 || p.Pricing.MaximumCents != 4000000 {
		t.Fatalf("pilot scope or price drifted: %+v", p)
	}
	if p.Authority.WriteAuthority || p.Authority.Topology != "EXTERNAL_OBSERVATION" {
		t.Fatalf("P1A must not grant write authority")
	}
	if p.Exit.RetentionDays == 0 || !p.Exit.DeletionCertificate || p.StopThresholdPct <= p.RepriceThresholdPct {
		t.Fatalf("exit and stop terms are incomplete")
	}
}

func TestPilotCommercialPackageCanonicalRoundTrip(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	b, err := p.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.ManifestDigest != p.ManifestDigest || got.Pricing.Currency != "USD" {
		t.Fatalf("round trip changed contract")
	}
}

func TestPilotCommercialPackageRejectsAuthorityExpansion(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	p.Authority.WriteAuthority = true
	if err := p.Validate(); !errors.Is(err, ErrAuthority) {
		t.Fatalf("expected authority error, got %v", err)
	}
}

func TestPilotCommercialPackageRejectsPriceDrift(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	p.Pricing.MaximumCents++
	if err := p.Validate(); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("expected package error, got %v", err)
	}
}

func TestPilotCommercialPackageAcceptsJSONWhitespace(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	b, err := p.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if _, err := Parse(b); err != nil {
		t.Fatalf("whitespace should not alter valid JSON, got %v", err)
	}
}
