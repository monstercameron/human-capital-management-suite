package clock

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var updateProfileGolden = flag.Bool("update-profile-golden", false, "rewrite the profile registry golden file")

func sampleProfiles() []IntegrationProfile {
	return []IntegrationProfile{
		{
			Class: SourceFirstPartyBrowser, Transport: "HTTPS", Authentication: "SESSION_TOKEN",
			TrustCeiling: TrustCeilingHigh, PermittedMethods: []IdentificationMethod{MethodPIN, MethodBadge, MethodQR},
			OfflineLimit: 0, ClockTrustRequired: true, FirstPartner: "first-party web client", Version: "v1",
		},
		{
			Class: SourceManagedKiosk, Transport: "HTTPS", Authentication: "MDM_MANAGED_APP",
			TrustCeiling: TrustCeilingHigh, PermittedMethods: []IdentificationMethod{MethodPIN, MethodBadge, MethodQR, MethodFace},
			OfflineLimit: 8 * time.Hour, ClockTrustRequired: true, FirstPartner: "reference kiosk", Version: "v1",
		},
		{
			Class: SourceDevicePushHTTP, Transport: "HTTPS_PUSH", Authentication: "DEVICE_CERT",
			TrustCeiling: TrustCeilingMedium, PermittedMethods: []IdentificationMethod{MethodBadge, MethodFingerprint},
			OfflineLimit: 24 * time.Hour, ClockTrustRequired: true, FirstPartner: "ZKTeco PUSH/ADMS", Version: "v1",
		},
		{
			Class: SourceServerPullVendorAPI, Transport: "HTTPS_REST", Authentication: "OAUTH2_CLIENT_CREDENTIALS",
			TrustCeiling: TrustCeilingMedium, PermittedMethods: []IdentificationMethod{MethodBadge, MethodFingerprint},
			OfflineLimit: 0, ClockTrustRequired: false, FirstPartner: "Suprema BioStar 2 T&A API", Version: "v1",
		},
		{
			Class: SourceSFTPBatchFile, Transport: "SFTP", Authentication: "SSH_KEY",
			TrustCeiling: TrustCeilingLow, PermittedMethods: []IdentificationMethod{MethodBadge},
			OfflineLimit: 168 * time.Hour, ClockTrustRequired: false, FirstPartner: "Acroprint timeQplus export", Version: "v1",
		},
		{
			Class: SourceThirdPartyApp, Transport: "HTTPS_WEBHOOK", Authentication: "SIGNED_WEBHOOK",
			TrustCeiling: TrustCeilingMedium, PermittedMethods: []IdentificationMethod{MethodPIN, MethodQR},
			OfflineLimit: 0, ClockTrustRequired: false, FirstPartner: "Deputy webhooks", Version: "v1",
		},
	}
}

// TestTodo_TCLOCK_001 is the PRIMARY test: a registry built from the six
// accepted source classes accepts a declared class and method, and rejects
// a class or method outside what was registered.
func TestTodo_TCLOCK_001(t *testing.T) {
	reg, err := NewProfileRegistry(sampleProfiles())
	if err != nil {
		t.Fatalf("NewProfileRegistry: %v", err)
	}
	if len(reg.Profiles) != len(SourceClasses()) {
		t.Fatalf("expected %d profiles, got %d", len(SourceClasses()), len(reg.Profiles))
	}
	for _, class := range SourceClasses() {
		if _, ok := reg.Profile(class); !ok {
			t.Fatalf("class %s missing from registry", class)
		}
	}
	if err := reg.Accepts(SourceManagedKiosk, MethodFace); err != nil {
		t.Fatalf("expected managed kiosk to accept face: %v", err)
	}
	if err := reg.Accepts(SourceManagedKiosk, MethodFingerprint); err == nil {
		t.Fatalf("expected managed kiosk to reject fingerprint (not permitted)")
	} else if !errors.Is(err, ErrProfileRejected) {
		t.Fatalf("expected ErrProfileRejected, got %v", err)
	}
	if err := reg.Accepts(SourceClass("UNDECLARED_CLASS"), ""); !errors.Is(err, ErrProfileNotRegistered) {
		t.Fatalf("expected ErrProfileNotRegistered, got %v", err)
	}
}

// TestTodo_TCLOCK_001_Golden pins the registry's canonical digest bytes so
// a change to the accepted profile set is a deliberate, reviewed change.
func TestTodo_TCLOCK_001_Golden(t *testing.T) {
	reg, err := NewProfileRegistry(sampleProfiles())
	if err != nil {
		t.Fatalf("NewProfileRegistry: %v", err)
	}
	var got string
	for _, p := range reg.Profiles {
		exp, err := ExplainProfile(p)
		if err != nil {
			t.Fatalf("ExplainProfile: %v", err)
		}
		got += fmt.Sprintf("class=%s ceiling=%s methods=%d clock_trust=%v partner=%s digest=%s\n",
			exp.Class, exp.TrustCeiling, exp.MethodCount, exp.ClockTrustRequired, exp.FirstPartner, exp.Digest)
	}
	got += fmt.Sprintf("registry_digest=%s\n", reg.Digest)
	path := filepath.Join("testdata", "golden", "profile_registry.txt")
	if *updateProfileGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-profile-golden to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestTodo_TCLOCK_001_Security proves an undeclared source class is
// rejected at the registry boundary rather than silently accepted with an
// inferred trust ceiling, and that duplicate classes never merge silently.
func TestTodo_TCLOCK_001_Security(t *testing.T) {
	profiles := sampleProfiles()
	profiles = append(profiles, IntegrationProfile{
		Class: SourceFirstPartyBrowser, Transport: "HTTPS", Authentication: "SESSION_TOKEN",
		TrustCeiling: TrustCeilingHigh, PermittedMethods: []IdentificationMethod{MethodPIN}, FirstPartner: "duplicate", Version: "v1",
	})
	if _, err := NewProfileRegistry(profiles); !errors.Is(err, ErrProfileRejected) {
		t.Fatalf("expected duplicate class to be rejected, got %v", err)
	}

	reg, err := NewProfileRegistry(sampleProfiles())
	if err != nil {
		t.Fatalf("NewProfileRegistry: %v", err)
	}
	if err := reg.Accepts(SourceClass("ROGUE_ADAPTER"), MethodPIN); !errors.Is(err, ErrProfileNotRegistered) {
		t.Fatalf("expected rogue class to be rejected, got %v", err)
	}

	// A profile that cannot declare a trust ceiling must fail closed, not
	// default to the highest ceiling.
	bad := IntegrationProfile{Class: SourceThirdPartyApp, Transport: "HTTPS", Authentication: "X", PermittedMethods: []IdentificationMethod{MethodPIN}, FirstPartner: "x", Version: "v1"}
	if err := bad.Validate(); !errors.Is(err, ErrProfileRejected) {
		t.Fatalf("expected missing trust ceiling to reject, got %v", err)
	}
}
