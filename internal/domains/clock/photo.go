// TCLOCK-007: capture punch photos for buddy-punch review under a
// declared purpose and retention.
//
// A tenant turns photo capture on per site with notice text; a captured
// photo is a reference to bytes held by the artifact service (this package
// never carries raw image bytes), bound to one punch receipt. Viewing is
// authorized only for the worker themselves or a supervisor holding
// time-review scope. A photo is due for deletion once its site's review
// window has elapsed, unless a legal hold or an open exception references
// it. No automated face comparison may run against a photo unless a
// TCLOCK-006 biometric admission already exists for the same worker and
// method. The package is pure: no clock, no storage, no network.
package clock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrPhotoRejected is the TCLOCK-007 seeded-defect sentinel. A test that
	// probes capture with photo policy off, an unauthorized viewer or a
	// face comparison without an admission must see this error.
	ErrPhotoRejected = errors.New("TCLOCK_007_REJECTED")
	// ErrPhotoEvidence identifies an invalid photo capture that cannot be
	// used as evidence.
	ErrPhotoEvidence = errors.New("clock: punch photo evidence is invalid")
)

// PhotoRejection is the stable TCLOCK-007 failure shape.
type PhotoRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *PhotoRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrPhotoRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the TCLOCK_007_REJECTED sentinel to errors.Is.
func (r *PhotoRejection) Unwrap() error { return ErrPhotoRejected }

func photoReject(field, state, version, reason string) error {
	return &PhotoRejection{Field: field, State: state, Version: version, Reason: reason}
}

const photoVersion = "tclock-photo/v1"

// PhotoPolicy is the per-site punch-photo enablement.
type PhotoPolicy struct {
	SiteRef      string
	Enabled      bool
	NoticeText   string
	ReviewWindow time.Duration
	Version      string
}

func (p PhotoPolicy) Validate() error {
	if strings.TrimSpace(p.SiteRef) == "" {
		return photoReject("policy.site_ref", "", p.Version, "site reference is required")
	}
	if !p.Enabled {
		return nil
	}
	if strings.TrimSpace(p.NoticeText) == "" {
		return photoReject("policy.notice_text", "ENABLED", p.Version, "notice text is required once photo capture is enabled")
	}
	if p.ReviewWindow <= 0 {
		return photoReject("policy.review_window", "ENABLED", p.Version, "review window must be positive")
	}
	if strings.TrimSpace(p.Version) == "" {
		return photoReject("policy.version", "ENABLED", p.Version, "policy version is required")
	}
	return nil
}

// PhotoCapture is the artifact reference bound to a punch receipt.
// LegalHold and OpenExceptionRef are the only facts that suspend deletion
// past the review window.
type PhotoCapture struct {
	PunchReceiptRef  string
	ArtifactRef      string
	SiteRef          string
	CapturedAt       time.Time
	LegalHold        bool
	OpenExceptionRef string
	Digest           string
}

func (c PhotoCapture) digestBody() string {
	return strings.Join([]string{c.PunchReceiptRef, c.ArtifactRef, c.SiteRef, c.CapturedAt.UTC().Format(time.RFC3339Nano)}, "\x00")
}

// CapturePunchPhoto binds an artifact reference to a punch receipt under an
// enabled site policy.
func CapturePunchPhoto(policy PhotoPolicy, punchReceiptRef, artifactRef string, capturedAt time.Time) (PhotoCapture, error) {
	if err := policy.Validate(); err != nil {
		return PhotoCapture{}, err
	}
	if !policy.Enabled {
		return PhotoCapture{}, photoReject("policy.enabled", "OFF", photoVersion, "punch photo capture is off for this site")
	}
	if strings.TrimSpace(punchReceiptRef) == "" {
		return PhotoCapture{}, photoReject("punch_receipt_ref", "", photoVersion, "punch receipt reference is required")
	}
	if strings.TrimSpace(artifactRef) == "" {
		return PhotoCapture{}, photoReject("artifact_ref", "", photoVersion, "artifact reference is required")
	}
	if capturedAt.IsZero() {
		return PhotoCapture{}, photoReject("captured_at", "", photoVersion, "captured instant is required")
	}
	capture := PhotoCapture{PunchReceiptRef: punchReceiptRef, ArtifactRef: artifactRef, SiteRef: policy.SiteRef, CapturedAt: capturedAt.UTC()}
	sum := sha256.Sum256([]byte(capture.digestBody()))
	capture.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return capture, nil
}

// PhotoScope is the closed set of roles that may view a punch photo.
type PhotoScope string

const (
	PhotoScopeWorkerSelf       PhotoScope = "WORKER_SELF"
	PhotoScopeSupervisorReview PhotoScope = "SUPERVISOR_TIME_REVIEW"
)

func (s PhotoScope) Valid() bool {
	return s == PhotoScopeWorkerSelf || s == PhotoScopeSupervisorReview
}

// AuthorizeView reports whether a viewer holding the given facts may view a
// punch photo under scope.
func AuthorizeView(scope PhotoScope, requesterIsTheWorker, requesterHasTimeReviewScope bool) error {
	if !scope.Valid() {
		return photoReject("scope", "", photoVersion, "photo view scope is not declared")
	}
	switch scope {
	case PhotoScopeWorkerSelf:
		if !requesterIsTheWorker {
			return photoReject("scope", "DENIED", photoVersion, "only the worker may view their own punch photo under worker scope")
		}
	case PhotoScopeSupervisorReview:
		if !requesterHasTimeReviewScope {
			return photoReject("scope", "DENIED", photoVersion, "viewer lacks time-review scope")
		}
	}
	return nil
}

// DueForDeletion reports whether the capture must be deleted at now: the
// site's review window has elapsed and no legal hold or open exception
// protects it.
func (c PhotoCapture) DueForDeletion(policy PhotoPolicy, now time.Time) (bool, error) {
	if err := policy.Validate(); err != nil {
		return false, err
	}
	if now.IsZero() {
		return false, photoReject("now", "", photoVersion, "server clock is required")
	}
	if c.LegalHold || strings.TrimSpace(c.OpenExceptionRef) != "" {
		return false, nil
	}
	return !now.Before(c.CapturedAt.Add(policy.ReviewWindow)), nil
}

// RequireFaceComparisonAdmission gates automated face comparison against a
// punch photo: it is refused unless a TCLOCK-006 biometric admission
// already exists for the same worker and the FACE method.
func RequireFaceComparisonAdmission(admission BiometricAdmission, workerRef string) error {
	if admission.Digest == "" || admission.WorkerRef != workerRef || admission.Method != MethodFace {
		return photoReject("face_comparison", "DENIED", photoVersion, "no admitted biometric enrollment authorizes face comparison against this photo")
	}
	return nil
}

// PhotoExplanation is the audit-safe summary of a punch photo capture.
type PhotoExplanation struct {
	PunchReceiptRef  string
	SiteRef          string
	HasLegalHold     bool
	HasOpenException bool
	Digest           string
}

// ExplainPhotoCapture validates and summarizes a capture for operator
// display.
func ExplainPhotoCapture(c PhotoCapture) (PhotoExplanation, error) {
	if c.Digest == "" {
		return PhotoExplanation{}, ErrPhotoEvidence
	}
	sum := sha256.Sum256([]byte(c.digestBody()))
	if "sha256:"+hex.EncodeToString(sum[:]) != c.Digest {
		return PhotoExplanation{}, fmt.Errorf("%w: digest mismatch", ErrPhotoEvidence)
	}
	return PhotoExplanation{PunchReceiptRef: c.PunchReceiptRef, SiteRef: c.SiteRef, HasLegalHold: c.LegalHold, HasOpenException: c.OpenExceptionRef != "", Digest: c.Digest}, nil
}
