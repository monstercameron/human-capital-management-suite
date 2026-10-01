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

var updatePhotoGolden = flag.Bool("update-photo-golden", false, "rewrite the punch photo golden file")

func enabledPhotoPolicy() PhotoPolicy {
	return PhotoPolicy{SiteRef: "site-riverside", Enabled: true, NoticeText: "Photos are captured at punch for buddy-punch review.", ReviewWindow: 72 * time.Hour, Version: "v1"}
}

// TestTodo_TCLOCK_007 is the PRIMARY test: a photo captured under an
// enabled site policy is bound to its punch receipt, viewable by the
// worker and by a supervisor with time-review scope, and becomes due for
// deletion only after the review window elapses.
func TestTodo_TCLOCK_007(t *testing.T) {
	policy := enabledPhotoPolicy()
	capturedAt := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	capture, err := CapturePunchPhoto(policy, "punch-1", "artifact-1", capturedAt)
	if err != nil {
		t.Fatalf("CapturePunchPhoto: %v", err)
	}
	if capture.Digest == "" || capture.PunchReceiptRef != "punch-1" {
		t.Fatalf("unexpected capture %+v", capture)
	}

	if err := AuthorizeView(PhotoScopeWorkerSelf, true, false); err != nil {
		t.Fatalf("expected worker self-view to be authorized: %v", err)
	}
	if err := AuthorizeView(PhotoScopeSupervisorReview, false, true); err != nil {
		t.Fatalf("expected supervisor with review scope to be authorized: %v", err)
	}

	due, err := capture.DueForDeletion(policy, capturedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("DueForDeletion: %v", err)
	}
	if due {
		t.Fatalf("expected photo not yet due for deletion within the review window")
	}
	due, err = capture.DueForDeletion(policy, capturedAt.Add(73*time.Hour))
	if err != nil {
		t.Fatalf("DueForDeletion: %v", err)
	}
	if !due {
		t.Fatalf("expected photo due for deletion after the review window")
	}
}

// TestTodo_TCLOCK_007_Golden pins a punch photo capture's rendered
// explanation so a change to the capture digest is deliberate.
func TestTodo_TCLOCK_007_Golden(t *testing.T) {
	policy := enabledPhotoPolicy()
	capturedAt := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	capture, err := CapturePunchPhoto(policy, "punch-1", "artifact-1", capturedAt)
	if err != nil {
		t.Fatalf("CapturePunchPhoto: %v", err)
	}
	exp, err := ExplainPhotoCapture(capture)
	if err != nil {
		t.Fatalf("ExplainPhotoCapture: %v", err)
	}
	got := fmt.Sprintf("punch_receipt=%s site=%s legal_hold=%v open_exception=%v digest=%s\n", exp.PunchReceiptRef, exp.SiteRef, exp.HasLegalHold, exp.HasOpenException, exp.Digest)
	path := filepath.Join("testdata", "golden", "punch_photo.txt")
	if *updatePhotoGolden {
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
		t.Fatalf("read golden %s: %v (run with -update-photo-golden to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestTodo_TCLOCK_007_Security proves that capture is refused when the
// site's photo policy is off, that an unauthorized viewer is denied, that
// a legal hold or open exception suspends deletion, and that face
// comparison against a photo is refused without a matching TCLOCK-006
// biometric admission.
func TestTodo_TCLOCK_007_Security(t *testing.T) {
	off := PhotoPolicy{SiteRef: "site-riverside", Enabled: false}
	if _, err := CapturePunchPhoto(off, "punch-1", "artifact-1", time.Now()); !errors.Is(err, ErrPhotoRejected) {
		t.Fatalf("expected off policy to reject capture, got %v", err)
	}

	if err := AuthorizeView(PhotoScopeWorkerSelf, false, true); !errors.Is(err, ErrPhotoRejected) {
		t.Fatalf("expected worker scope to deny a non-worker viewer even with supervisor scope, got %v", err)
	}
	if err := AuthorizeView(PhotoScopeSupervisorReview, true, false); !errors.Is(err, ErrPhotoRejected) {
		t.Fatalf("expected supervisor scope to deny a viewer without time-review scope, got %v", err)
	}

	policy := enabledPhotoPolicy()
	capturedAt := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	held, err := CapturePunchPhoto(policy, "punch-1", "artifact-1", capturedAt)
	if err != nil {
		t.Fatalf("CapturePunchPhoto: %v", err)
	}
	held.LegalHold = true
	due, err := held.DueForDeletion(policy, capturedAt.Add(200*time.Hour))
	if err != nil {
		t.Fatalf("DueForDeletion: %v", err)
	}
	if due {
		t.Fatalf("expected legal hold to suspend deletion")
	}

	openException := held
	openException.LegalHold = false
	openException.OpenExceptionRef = "exception-1"
	due, err = openException.DueForDeletion(policy, capturedAt.Add(200*time.Hour))
	if err != nil {
		t.Fatalf("DueForDeletion: %v", err)
	}
	if due {
		t.Fatalf("expected open exception to suspend deletion")
	}

	if err := RequireFaceComparisonAdmission(BiometricAdmission{}, "worker-1"); !errors.Is(err, ErrPhotoRejected) {
		t.Fatalf("expected face comparison without an admission to be rejected, got %v", err)
	}
	admission := BiometricAdmission{WorkerRef: "worker-1", Method: MethodFace, Digest: "sha256:deadbeef"}
	if err := RequireFaceComparisonAdmission(admission, "worker-1"); err != nil {
		t.Fatalf("expected face comparison with a matching admission to be authorized: %v", err)
	}
	if err := RequireFaceComparisonAdmission(admission, "worker-2"); !errors.Is(err, ErrPhotoRejected) {
		t.Fatalf("expected face comparison for a different worker to be rejected, got %v", err)
	}
}
