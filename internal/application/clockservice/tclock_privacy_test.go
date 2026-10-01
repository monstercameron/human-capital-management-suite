package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type privacyPolicyFake struct {
	bio   BiometricPolicyEvidence
	photo PhotoPolicyEvidence
}

func (f privacyPolicyFake) BiometricPolicy(context.Context, string, string) (BiometricPolicyEvidence, error) {
	return f.bio, nil
}
func (f privacyPolicyFake) PhotoPolicy(context.Context, string, string) (PhotoPolicyEvidence, error) {
	return f.photo, nil
}

type privacyAuthFake struct{}

func (privacyAuthFake) AuthorizeWorker(context.Context, *trust.Principal, string, string) error {
	return nil
}
func (privacyAuthFake) AuthorizeSupervisor(context.Context, *trust.Principal, string, string, string) error {
	return nil
}
func (privacyAuthFake) AuthorizeDevice(context.Context, *trust.Principal, string, string, string) error {
	return nil
}

type privacyReceiptFake struct{ mismatch bool }

func (f privacyReceiptFake) ResolvePunchReceipt(_ context.Context, tenant, receipt string) (PunchReceiptBinding, error) {
	worker := "worker-1"
	if f.mismatch {
		worker = "worker-2"
	}
	return PunchReceiptBinding{Tenant: tenant, ReceiptRef: receipt, WorkerRef: worker, SiteID: "site-1"}, nil
}

type privacyBioFake struct {
	record    BiometricAdmissionRecord
	destroyed bool
}

func (f *privacyBioFake) RecordBiometric(_ context.Context, _ string, r BiometricAdmissionRecord, _ string) (BiometricAdmissionRecord, error) {
	r.ID = "consent-1"
	r.State = "GRANTED"
	f.record = r
	return r, nil
}
func (f *privacyBioFake) GetBiometric(context.Context, string, string) (BiometricAdmissionRecord, error) {
	return f.record, nil
}
func (f *privacyBioFake) DestroyBiometric(context.Context, string, string, string) error {
	f.destroyed = true
	return nil
}

type privacyPhotoFake struct {
	record          PunchPhotoRecord
	viewed, deleted bool
}

func (f *privacyPhotoFake) RecordPunchPhoto(_ context.Context, _ string, r PunchPhotoRecord) (PunchPhotoRecord, error) {
	r.ID = "photo-1"
	f.record = r
	return r, nil
}
func (f *privacyPhotoFake) GetPunchPhoto(context.Context, string, string) (PunchPhotoRecord, error) {
	return f.record, nil
}
func (f *privacyPhotoFake) RecordPhotoView(context.Context, string, string, string, clockdomain.PhotoScope) error {
	f.viewed = true
	return nil
}
func (f *privacyPhotoFake) DeletePunchPhoto(context.Context, string, string) error {
	f.deleted = true
	return nil
}

type privacyArtifactFake struct{ uploaded, deleted bool }

func (f *privacyArtifactFake) UploadPhoto(context.Context, string, string, []byte, string) (string, error) {
	f.uploaded = true
	return "artifact://photo-1", nil
}
func (f *privacyArtifactFake) DeletePhoto(context.Context, string, string) error {
	f.deleted = true
	return nil
}

func privacyPrincipal(t *testing.T, now time.Time) *trust.Principal {
	return privacyPrincipalAt(t, now.Add(-time.Minute), now.Add(time.Hour))
}

func privacyPrincipalAt(t *testing.T, issued, expires time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "worker-1", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: issued, ExpiresAt: expires, CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_TCLOCK_006_Security_ExpiredPrincipalAndKeyedEraseFailClosed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	policies := privacyPolicies(now)
	bio := &privacyBioFake{}
	s := PrivacyService{Policies: policies, Authorize: privacyAuthFake{}, Biometric: bio, Clock: func() time.Time { return now }}
	longPrincipal := privacyPrincipalAt(t, now.Add(-time.Minute), now.Add(4*time.Hour))
	_, err := s.EnrollBiometric(context.Background(), privacyPrincipalAt(t, now.Add(-2*time.Hour), now.Add(-time.Hour)), BiometricEnrollmentRequest{WorkerRef: "worker-1", Jurisdiction: "US-IL", TemplateCustodyRef: "keyed://tenant-a/key-1", DeviceRef: "clock-1", Method: clockdomain.MethodFace, DevicePADDeclared: true, Consent: clockdomain.BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-7", ConsentedAt: now.Add(-time.Minute)}})
	if !errors.Is(err, ErrInvalidPrincipal) {
		t.Fatalf("expired principal err=%v", err)
	}
	// A keyed template cannot be destroyed without both crypto-erasure and a hold preflight.
	_, err = s.EnrollBiometric(context.Background(), longPrincipal, BiometricEnrollmentRequest{WorkerRef: "worker-1", Jurisdiction: "US-IL", TemplateCustodyRef: "keyed://tenant-a/key-1", DeviceRef: "clock-1", Method: clockdomain.MethodFace, DevicePADDeclared: true, Consent: clockdomain.BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-7", ConsentedAt: now.Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	s.Clock = func() time.Time { return now.Add(2 * time.Hour) }
	if err := s.DestroyBiometric(context.Background(), longPrincipal, "consent-1", "worker-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing eraser err=%v", err)
	}
	if bio.destroyed {
		t.Fatal("missing eraser destroyed biometric")
	}
}

func TestTodo_TCLOCK_007_Security_InvalidPolicyAndReceiptMismatchDoNotUpload(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	policies := privacyPolicies(now)
	artifacts := &privacyArtifactFake{}
	photos := &privacyPhotoFake{}
	s := PrivacyService{Photos: policies, Authorize: privacyAuthFake{}, Punches: photos, Artifacts: artifacts, Receipts: privacyReceiptFake{}, Clock: func() time.Time { return now }}
	policies.photo.Policy.Enabled = false
	s.Photos = policies
	_, err := s.CapturePunchPhoto(context.Background(), privacyPrincipal(t, now), PunchPhotoRequest{WorkerRef: "worker-1", SiteID: "site-1", PunchReceiptRef: "receipt-1", MediaType: "image/jpeg", Bytes: []byte("image")})
	if !errors.Is(err, ErrInvalidRequest) || artifacts.uploaded {
		t.Fatalf("invalid policy err=%v uploaded=%v", err, artifacts.uploaded)
	}
	policies.photo.Policy.Enabled = true
	s.Photos = policies
	s.Receipts = privacyReceiptFake{mismatch: true}
	_, err = s.CapturePunchPhoto(context.Background(), privacyPrincipal(t, now), PunchPhotoRequest{WorkerRef: "worker-1", SiteID: "site-1", PunchReceiptRef: "receipt-1", MediaType: "image/jpeg", Bytes: []byte("image")})
	if !errors.Is(err, ErrInvalidRequest) || artifacts.uploaded {
		t.Fatalf("receipt mismatch err=%v uploaded=%v", err, artifacts.uploaded)
	}
}

func TestTodo_TCLOCK_007_Security_CrossTenantStoreResultIsRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	policies := privacyPolicies(now)
	photos := &privacyPhotoFake{}
	artifacts := &privacyArtifactFake{}
	s := PrivacyService{Photos: policies, Authorize: privacyAuthFake{}, Punches: photos, Artifacts: artifacts, Receipts: privacyReceiptFake{}, Clock: func() time.Time { return now }}
	// The fake simulates a corrupt adapter returning a row for another tenant.
	photos.record = PunchPhotoRecord{ID: "photo-1", Tenant: "tenant-b", WorkerRef: "worker-1", PunchReceiptRef: "receipt-1", ArtifactRef: "artifact://photo-1", SiteID: "site-1", Capture: clockdomain.PhotoCapture{PunchReceiptRef: "receipt-1", ArtifactRef: "artifact://photo-1", SiteRef: "site-1", CapturedAt: now, Digest: "sha256:bad"}}
	if _, err := s.ViewPunchPhoto(context.Background(), privacyPrincipal(t, now), "photo-1", clockdomain.PhotoScopeWorkerSelf); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross tenant result err=%v", err)
	}
	if photos.viewed {
		t.Fatal("cross tenant result was audited")
	}
}

func privacyPolicies(now time.Time) privacyPolicyFake {
	bio := clockdomain.BiometricPolicy{Tenant: values.TenantId("tenant-a"), Jurisdiction: "US-IL", Enabled: true, LegalBasis: clockdomain.LegalBasisIllinoisBIPA, NoticeVersion: "notice-7", RetentionPeriod: time.Hour, StatutoryLimit: 2 * time.Hour, NonBiometricAlternative: "badge", RequiresPAD: true, Version: "policy-3"}
	return privacyPolicyFake{bio: BiometricPolicyEvidence{Tenant: "tenant-a", Jurisdiction: "US-IL", Digest: "policy-digest", Current: true, Policy: bio}, photo: PhotoPolicyEvidence{Tenant: "tenant-a", SiteID: "site-1", Digest: "photo-policy", Current: true, Policy: clockdomain.PhotoPolicy{SiteRef: "site-1", Enabled: true, NoticeText: "photo notice", ReviewWindow: time.Hour, Version: "photo-1"}}}
}

func TestTodo_TCLOCK_006_Application_EnrollmentBindsCurrentPolicyAndDevice(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	policies := privacyPolicies(now)
	bio := &privacyBioFake{}
	s := PrivacyService{Policies: policies, Authorize: privacyAuthFake{}, Biometric: bio, Clock: func() time.Time { return now }}
	result, err := s.EnrollBiometric(context.Background(), privacyPrincipal(t, now), BiometricEnrollmentRequest{WorkerRef: "worker-1", Jurisdiction: "US-IL", TemplateCustodyRef: "device://clock-1/template-1", DeviceRef: "clock-1", Method: clockdomain.MethodFace, DevicePADDeclared: true, Consent: clockdomain.BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-7", ConsentedAt: now.Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Record.PolicyDigest != "policy-digest" || result.Record.Tenant != "tenant-a" {
		t.Fatalf("record=%+v", result.Record)
	}
	if err := s.VerifyFaceComparison(context.Background(), privacyPrincipal(t, now), "consent-1", "worker-1", "clock-2"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("wrong device err=%v", err)
	}
	// The fake has no separate id; verify the admission path with its stored record.
	if err := s.VerifyFaceComparison(context.Background(), privacyPrincipal(t, now), "consent-1", "worker-1", "clock-1"); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_TCLOCK_006_Security_RawCustodyAndStalePolicyFailClosed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	policies := privacyPolicies(now)
	s := PrivacyService{Policies: policies, Authorize: privacyAuthFake{}, Biometric: &privacyBioFake{}, Clock: func() time.Time { return now }}
	_, err := s.EnrollBiometric(context.Background(), privacyPrincipal(t, now), BiometricEnrollmentRequest{WorkerRef: "worker-1", Jurisdiction: "US-IL", TemplateCustodyRef: "raw:image-bytes", DeviceRef: "clock-1", Method: clockdomain.MethodFace, Consent: clockdomain.BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-7", ConsentedAt: now.Add(-time.Minute)}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("raw custody err=%v", err)
	}
	policies.bio.Current = false
	s.Policies = policies
	_, err = s.EnrollBiometric(context.Background(), privacyPrincipal(t, now), BiometricEnrollmentRequest{WorkerRef: "worker-1", Jurisdiction: "US-IL", TemplateCustodyRef: "keyed://tenant-a/key-1", DeviceRef: "clock-1", Method: clockdomain.MethodFace, DevicePADDeclared: true, Consent: clockdomain.BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-7", ConsentedAt: now.Add(-time.Minute)}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("stale policy err=%v", err)
	}
}

func TestTodo_TCLOCK_007_Application_PhotoUploadViewAndHeldDeletion(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	policies := privacyPolicies(now)
	photos := &privacyPhotoFake{}
	artifacts := &privacyArtifactFake{}
	s := PrivacyService{Photos: policies, Authorize: privacyAuthFake{}, Punches: photos, Artifacts: artifacts, Receipts: privacyReceiptFake{}, Clock: func() time.Time { return now }}
	p := privacyPrincipal(t, now)
	record, err := s.CapturePunchPhoto(context.Background(), p, PunchPhotoRequest{WorkerRef: "worker-1", SiteID: "site-1", PunchReceiptRef: "receipt-1", MediaType: "image/jpeg", Bytes: []byte("image")})
	if err != nil {
		t.Fatal(err)
	}
	if !artifacts.uploaded || record.ArtifactRef == "" {
		t.Fatalf("record=%+v uploaded=%v", record, artifacts.uploaded)
	}
	if _, err := s.ViewPunchPhoto(context.Background(), p, record.ID, clockdomain.PhotoScopeWorkerSelf); err != nil {
		t.Fatal(err)
	}
	if !photos.viewed {
		t.Fatal("photo view was not audited")
	}
	photos.record.Capture.LegalHold = true
	if err := s.DestroyPunchPhoto(context.Background(), p, record.ID); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("held deletion err=%v", err)
	}
	if artifacts.deleted || photos.deleted {
		t.Fatal("held photo was destroyed")
	}
}

type redirectedPrivacyPhoto struct{ privacyPhotoFake }

func (f *redirectedPrivacyPhoto) RecordPunchPhoto(_ context.Context, _ string, r PunchPhotoRecord) (PunchPhotoRecord, error) {
	r.ID = "redirected-photo"
	r.WorkerRef = "another-worker"
	f.record = r
	return r, nil
}
func TestTodo_TCLOCK_007_RejectsRedirectedStoreBinding(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	policies := privacyPolicies(now)
	photos := &redirectedPrivacyPhoto{}
	artifacts := &privacyArtifactFake{}
	service := PrivacyService{Photos: policies, Authorize: privacyAuthFake{}, Punches: photos, Artifacts: artifacts, Receipts: privacyReceiptFake{}, Clock: func() time.Time { return now }}
	_, err := service.CapturePunchPhoto(context.Background(), privacyPrincipal(t, now), PunchPhotoRequest{WorkerRef: "worker-1", SiteID: "site-1", PunchReceiptRef: "receipt-1", MediaType: "image/jpeg", Bytes: []byte("image")})
	if !errors.Is(err, ErrUnavailable) || !artifacts.deleted {
		t.Fatalf("redirected photo err=%v artifact cleaned=%v", err, artifacts.deleted)
	}
}

func TestTodo_TCLOCK_006(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	policies := privacyPolicies(now)
	bio := &privacyBioFake{}
	service := PrivacyService{Policies: policies, Authorize: privacyAuthFake{}, Biometric: bio, Clock: func() time.Time { return now }}
	result, err := service.EnrollBiometric(context.Background(), privacyPrincipal(t, now), BiometricEnrollmentRequest{
		WorkerRef: "worker-1", Jurisdiction: "US-IL", TemplateCustodyRef: "device://clock-1/template-1", DeviceRef: "clock-1", Method: clockdomain.MethodFace, DevicePADDeclared: true,
		Consent: clockdomain.BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-7", ConsentedAt: now.Add(-time.Minute)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Record.Admission.NoticeVersion != "notice-7" || !result.Record.Admission.DestructionAt.Equal(now.Add(time.Hour)) || result.Record.TemplateCustodyRef == "" || bio.record.Admission.Digest == "" {
		t.Fatalf("biometric admission=%+v", result.Record)
	}
}

func TestTodo_TCLOCK_006_Golden(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	policies := privacyPolicies(now)
	service := PrivacyService{Policies: policies, Authorize: privacyAuthFake{}, Biometric: &privacyBioFake{}, Clock: func() time.Time { return now }}
	result, err := service.EnrollBiometric(context.Background(), privacyPrincipal(t, now), BiometricEnrollmentRequest{
		WorkerRef: "worker-1", Jurisdiction: "US-IL", TemplateCustodyRef: "device://clock-1/template-1", DeviceRef: "clock-1", Method: clockdomain.MethodFace, DevicePADDeclared: true,
		Consent: clockdomain.BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-7", ConsentedAt: now.Add(-time.Minute)},
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:2b78f305f8eff6b7b23fa38696bf98f479c80b87eb35750cdcaf65329457e420"
	if result.Record.Admission.Digest != wantDigest {
		t.Fatalf("admission digest=%q, want golden %q", result.Record.Admission.Digest, wantDigest)
	}
}

func TestTodo_TCLOCK_006_Security(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	policies := privacyPolicies(now)
	service := PrivacyService{Policies: policies, Biometric: &privacyBioFake{}, Clock: func() time.Time { return now }}
	if err := service.VerifyFaceComparison(context.Background(), privacyPrincipal(t, now), "consent-1", "worker-1", "clock-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing authorizer err=%v, want ErrUnavailable", err)
	}
	service.Authorize = privacyAuthFake{}
	_, err := service.EnrollBiometric(context.Background(), privacyPrincipal(t, now), BiometricEnrollmentRequest{WorkerRef: "worker-1", Jurisdiction: "US-IL", TemplateCustodyRef: "raw:image-bytes", DeviceRef: "clock-1", Method: clockdomain.MethodFace, DevicePADDeclared: true, Consent: clockdomain.BiometricConsent{WorkerRef: "worker-1", NoticeVersion: "notice-7", ConsentedAt: now}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("raw custody err=%v, want ErrInvalidRequest", err)
	}
}

func TestTodo_TCLOCK_006_Property(t *testing.T) {
	policy := privacyPolicies(time.Unix(1_700_000_000, 0).UTC()).bio.Policy
	for _, enrolledAt := range []time.Time{time.Unix(1_700_000_000, 0).UTC(), time.Unix(1_700_003_600, 0).UTC(), time.Unix(1_700_007_200, 0).UTC()} {
		got, err := policy.DestructionDate(enrolledAt)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(enrolledAt.Add(time.Hour)) {
			t.Fatalf("enrolled=%s destruction=%s, want purpose end", enrolledAt, got)
		}
	}
}

func TestTodo_TCLOCK_006_Recovery(t *testing.T) {
	policy := privacyPolicies(time.Unix(1_700_000_000, 0).UTC()).bio.Policy
	instants := []time.Time{time.Unix(1_700_000_000, 0).UTC(), time.Unix(1_700_003_600, 0).UTC(), time.Unix(1_700_007_200, 0).UTC()}
	replayed, err := clockdomain.ReplayDestructionSchedule(policy, instants)
	if err != nil || len(replayed) != len(instants) {
		t.Fatalf("replay=%v err=%v", replayed, err)
	}
	for i, at := range instants {
		want, err := policy.DestructionDate(at)
		if err != nil || !replayed[i].Equal(want) {
			t.Fatalf("replayed[%d]=%s want=%s err=%v", i, replayed[i], want, err)
		}
	}
}

func TestTodo_TCLOCK_007(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	photos := &privacyPhotoFake{}
	artifacts := &privacyArtifactFake{}
	service := PrivacyService{Photos: privacyPolicies(now), Authorize: privacyAuthFake{}, Punches: photos, Artifacts: artifacts, Receipts: privacyReceiptFake{}, Clock: func() time.Time { return now }}
	record, err := service.CapturePunchPhoto(context.Background(), privacyPrincipal(t, now), PunchPhotoRequest{WorkerRef: "worker-1", SiteID: "site-1", PunchReceiptRef: "receipt-1", MediaType: "image/jpeg", Bytes: []byte("image")})
	if err != nil {
		t.Fatal(err)
	}
	if record.ArtifactRef == "" || record.Capture.ArtifactRef != record.ArtifactRef || record.Capture.PunchReceiptRef != record.PunchReceiptRef || !artifacts.uploaded {
		t.Fatalf("photo record=%+v uploaded=%v", record, artifacts.uploaded)
	}
	if _, err := service.ViewPunchPhoto(context.Background(), privacyPrincipal(t, now), record.ID, clockdomain.PhotoScopeWorkerSelf); err != nil || !photos.viewed {
		t.Fatalf("photo view err=%v audited=%v", err, photos.viewed)
	}
}

func TestTodo_TCLOCK_007_Golden(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	service := PrivacyService{Photos: privacyPolicies(now), Authorize: privacyAuthFake{}, Punches: &privacyPhotoFake{}, Artifacts: &privacyArtifactFake{}, Receipts: privacyReceiptFake{}, Clock: func() time.Time { return now }}
	record, err := service.CapturePunchPhoto(context.Background(), privacyPrincipal(t, now), PunchPhotoRequest{WorkerRef: "worker-1", SiteID: "site-1", PunchReceiptRef: "receipt-1", MediaType: "image/jpeg", Bytes: []byte("image")})
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:903ee911f53309f66b4b725f25204f066ef0496cda2abce621a95f979fb68ff5"
	if record.Capture.Digest != wantDigest {
		t.Fatalf("photo digest=%q, want golden %q", record.Capture.Digest, wantDigest)
	}
}

func TestTodo_TCLOCK_007_Security(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	artifacts := &privacyArtifactFake{}
	service := PrivacyService{Photos: privacyPolicies(now), Punches: &privacyPhotoFake{}, Artifacts: artifacts, Receipts: privacyReceiptFake{}, Clock: func() time.Time { return now }}
	if _, err := service.CapturePunchPhoto(context.Background(), privacyPrincipal(t, now), PunchPhotoRequest{WorkerRef: "worker-1", SiteID: "site-1", PunchReceiptRef: "receipt-1", MediaType: "image/jpeg", Bytes: []byte("image")}); !errors.Is(err, ErrUnavailable) || artifacts.uploaded {
		t.Fatalf("missing authorizer err=%v uploaded=%v", err, artifacts.uploaded)
	}
	photos := &privacyPhotoFake{}
	photos.record = PunchPhotoRecord{Tenant: "tenant-a", WorkerRef: "worker-1", PunchReceiptRef: "receipt-1", ArtifactRef: "artifact://photo-1", SiteID: "site-1", Capture: clockdomain.PhotoCapture{PunchReceiptRef: "receipt-1", ArtifactRef: "artifact://photo-1", SiteRef: "site-1", CapturedAt: now}}
	service.Authorize = privacyAuthFake{}
	service.Punches = photos
	if _, err := service.ViewPunchPhoto(context.Background(), privacyPrincipal(t, now), "photo-1", clockdomain.PhotoScopeWorkerSelf); !errors.Is(err, ErrInvalidRequest) || photos.viewed {
		t.Fatalf("unbound photo err=%v audited=%v", err, photos.viewed)
	}
}
