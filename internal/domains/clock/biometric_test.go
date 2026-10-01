package clock

import (
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var updateBiometricGolden = flag.Bool("update-biometric-golden", false, "rewrite the biometric admission golden file")

func enabledBiometricPolicy() BiometricPolicy {
	return BiometricPolicy{
		Tenant: values.TenantId("acme-co"), Jurisdiction: "US-IL", Enabled: true,
		LegalBasis: LegalBasisIllinoisBIPA, NoticeVersion: "notice-2026-1",
		RetentionPeriod: 90 * 24 * time.Hour, StatutoryLimit: 3 * 365 * 24 * time.Hour,
		NonBiometricAlternative: "PIN or badge", RequiresPAD: true, Version: "v1",
	}
}

// TestTodo_TCLOCK_006 is the PRIMARY test: an enabled policy with consent
// bound to its exact notice version admits a fingerprint enrollment, and a
// face enrollment requires declared presentation-attack detection when the
// policy requires it.
func TestTodo_TCLOCK_006(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	policy := enabledBiometricPolicy()
	consent := BiometricConsent{WorkerRef: "worker-1", NoticeVersion: policy.NoticeVersion, ConsentedAt: now.Add(-time.Hour)}

	admission, err := AdmitBiometricEnrollment(EnrollmentGateRequest{Policy: policy, Consent: consent, Method: MethodFingerprint, Now: now})
	if err != nil {
		t.Fatalf("AdmitBiometricEnrollment (fingerprint): %v", err)
	}
	if admission.WorkerRef != "worker-1" || admission.DestructionAt.IsZero() {
		t.Fatalf("unexpected admission %+v", admission)
	}

	if _, err := AdmitBiometricEnrollment(EnrollmentGateRequest{Policy: policy, Consent: consent, Method: MethodFace, Now: now, DevicePADDeclared: false}); !errors.Is(err, ErrBiometricRejected) {
		t.Fatalf("expected face enrollment without declared PAD to be rejected, got %v", err)
	}
	faceAdmission, err := AdmitBiometricEnrollment(EnrollmentGateRequest{Policy: policy, Consent: consent, Method: MethodFace, Now: now, DevicePADDeclared: true})
	if err != nil {
		t.Fatalf("AdmitBiometricEnrollment (face with PAD): %v", err)
	}
	if faceAdmission.Method != MethodFace {
		t.Fatalf("unexpected face admission %+v", faceAdmission)
	}

	// Statutory limit (3 years) is shorter than retention here would be if
	// retention were longer; verify the earlier-of rule directly.
	shortStatute := policy
	shortStatute.StatutoryLimit = 24 * time.Hour
	destruction, err := shortStatute.DestructionDate(now)
	if err != nil {
		t.Fatalf("DestructionDate: %v", err)
	}
	if !destruction.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("expected statutory limit to govern, got %v", destruction)
	}
}

// TestTodo_TCLOCK_006_Golden pins a biometric admission's rendered
// explanation so a change to the destruction-date computation is a
// deliberate, reviewed change.
func TestTodo_TCLOCK_006_Golden(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	policy := enabledBiometricPolicy()
	consent := BiometricConsent{WorkerRef: "worker-1", NoticeVersion: policy.NoticeVersion, ConsentedAt: now.Add(-time.Hour)}
	admission, err := AdmitBiometricEnrollment(EnrollmentGateRequest{Policy: policy, Consent: consent, Method: MethodFingerprint, Now: now})
	if err != nil {
		t.Fatalf("AdmitBiometricEnrollment: %v", err)
	}
	exp, err := ExplainBiometricAdmission(admission)
	if err != nil {
		t.Fatalf("ExplainBiometricAdmission: %v", err)
	}
	got := fmt.Sprintf("method=%s notice_version=%s destruction_at=%s digest=%s\n", exp.Method, exp.NoticeVersion, exp.DestructionAt.Format(time.RFC3339), exp.Digest)
	path := filepath.Join("testdata", "golden", "biometric_admission.txt")
	if *updateBiometricGolden {
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
		t.Fatalf("read golden %s: %v (run with -update-biometric-golden to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestTodo_TCLOCK_006_Security proves that a disabled or unconfigured
// policy, a consent bound to a stale notice version and an enrollment that
// precedes consent are all rejected.
func TestTodo_TCLOCK_006_Security(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	off := BiometricPolicy{Tenant: values.TenantId("acme-co"), Jurisdiction: "US-IL", Enabled: false}
	consent := BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-2026-1", ConsentedAt: now.Add(-time.Hour)}
	if _, err := AdmitBiometricEnrollment(EnrollmentGateRequest{Policy: off, Consent: consent, Method: MethodFace, Now: now, DevicePADDeclared: true}); !errors.Is(err, ErrBiometricRejected) {
		t.Fatalf("expected off policy to be rejected, got %v", err)
	}

	unconfigured := BiometricPolicy{Tenant: values.TenantId("acme-co"), Jurisdiction: "US-IL", Enabled: true}
	if err := unconfigured.Validate(); !errors.Is(err, ErrBiometricRejected) {
		t.Fatalf("expected unconfigured enabled policy to fail validation, got %v", err)
	}

	policy := enabledBiometricPolicy()
	staleConsent := BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-2020-old", ConsentedAt: now.Add(-time.Hour)}
	if _, err := AdmitBiometricEnrollment(EnrollmentGateRequest{Policy: policy, Consent: staleConsent, Method: MethodFingerprint, Now: now}); !errors.Is(err, ErrBiometricRejected) {
		t.Fatalf("expected stale notice version to be rejected, got %v", err)
	}

	futureConsent := BiometricConsent{WorkerRef: "worker-1", NoticeVersion: policy.NoticeVersion, ConsentedAt: now.Add(time.Hour)}
	if _, err := AdmitBiometricEnrollment(EnrollmentGateRequest{Policy: policy, Consent: futureConsent, Method: MethodFingerprint, Now: now}); !errors.Is(err, ErrBiometricRejected) {
		t.Fatalf("expected enrollment preceding consent to be rejected, got %v", err)
	}
}

// TestTodo_TCLOCK_006_Property proves, over many random retention and
// statutory-limit pairs, that DestructionDate always returns the earlier
// of the two derived instants.
func TestTodo_TCLOCK_006_Property(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	enrolledAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 300; i++ {
		retention := time.Duration(1+rng.IntN(1000)) * 24 * time.Hour
		statutory := time.Duration(1+rng.IntN(1000)) * 24 * time.Hour
		policy := enabledBiometricPolicy()
		policy.RetentionPeriod, policy.StatutoryLimit = retention, statutory
		destruction, err := policy.DestructionDate(enrolledAt)
		if err != nil {
			t.Fatalf("case %d: DestructionDate: %v", i, err)
		}
		wantEarlier := enrolledAt.Add(retention)
		if enrolledAt.Add(statutory).Before(wantEarlier) {
			wantEarlier = enrolledAt.Add(statutory)
		}
		if !destruction.Equal(wantEarlier) {
			t.Fatalf("case %d: retention=%v statutory=%v got %v want %v", i, retention, statutory, destruction, wantEarlier)
		}
	}
}

// TestTodo_TCLOCK_006_Recovery proves that destruction dates rebuilt from
// an ordered list of enrollment instants (ReplayDestructionSchedule) equal
// recomputing DestructionDate one at a time.
func TestTodo_TCLOCK_006_Recovery(t *testing.T) {
	policy := enabledBiometricPolicy()
	enrolledAts := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
	}
	incremental := make([]time.Time, 0, len(enrolledAts))
	for _, at := range enrolledAts {
		d, err := policy.DestructionDate(at)
		if err != nil {
			t.Fatalf("incremental DestructionDate: %v", err)
		}
		incremental = append(incremental, d)
	}
	replayed, err := ReplayDestructionSchedule(policy, enrolledAts)
	if err != nil {
		t.Fatalf("ReplayDestructionSchedule: %v", err)
	}
	if len(replayed) != len(incremental) {
		t.Fatalf("length mismatch: %d vs %d", len(replayed), len(incremental))
	}
	for i := range replayed {
		if !replayed[i].Equal(incremental[i]) {
			t.Fatalf("index %d: replayed %v != incremental %v", i, replayed[i], incremental[i])
		}
	}
}
