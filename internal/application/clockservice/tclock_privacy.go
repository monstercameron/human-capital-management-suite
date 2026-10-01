package clockservice

import (
	"context"
	"strings"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// BiometricPolicyEvidence is a resolver-produced, current policy snapshot.
// A non-current or tenant-mismatched snapshot can never authorize a write.
type BiometricPolicyEvidence struct {
	Tenant       string
	Jurisdiction string
	Digest       string
	Current      bool
	Policy       clockdomain.BiometricPolicy
}

// PhotoPolicyEvidence is the equivalent current snapshot for one site.
type PhotoPolicyEvidence struct {
	Tenant  string
	SiteID  string
	Digest  string
	Current bool
	Policy  clockdomain.PhotoPolicy
}

// BiometricPolicyResolver resolves a policy from the authoritative policy
// store. The caller supplies no policy claims other than the jurisdiction.
type BiometricPolicyResolver interface {
	BiometricPolicy(context.Context, string, string) (BiometricPolicyEvidence, error)
}

// PhotoPolicyResolver resolves the current site policy.
type PhotoPolicyResolver interface {
	PhotoPolicy(context.Context, string, string) (PhotoPolicyEvidence, error)
}

// PrivacyAuthorizer evaluates current worker and supervisor scope. It must
// resolve authority from the principal and tenant, rather than request flags.
type PrivacyAuthorizer interface {
	AuthorizeWorker(context.Context, *trust.Principal, string, string) error
	AuthorizeSupervisor(context.Context, *trust.Principal, string, string, string) error
	AuthorizeDevice(context.Context, *trust.Principal, string, string, string) error
}

// BiometricAdmissionRecord is the durable lifecycle projection. Admission is
// digest-only; TemplateCustodyRef is an opaque device or separately keyed ref.
type BiometricAdmissionRecord struct {
	ID, Tenant, Jurisdiction, PolicyDigest, TemplateCustodyRef, DeviceRef, State string
	Admission                                                                    clockdomain.BiometricAdmission
}

// BiometricAdmissionStore owns consent and template-admission lifecycle.
type BiometricAdmissionStore interface {
	RecordBiometric(context.Context, string, BiometricAdmissionRecord, string) (BiometricAdmissionRecord, error)
	GetBiometric(context.Context, string, string) (BiometricAdmissionRecord, error)
	DestroyBiometric(context.Context, string, string, string) error
}

// BiometricEraser is an optional crypto-erasure adapter for separately keyed
// custody. Device-held templates can be destroyed by the admission store.
type BiometricEraser interface {
	DestroyBiometricKey(context.Context, string, string, string) error
}

// BiometricRetentionChecker performs the hold/exception preflight before key
// destruction. A keyed template cannot be destroyed without this evidence.
type BiometricRetentionChecker interface {
	BiometricHeld(context.Context, string, string) (bool, error)
}

// PunchReceiptBinding is authoritative receipt ownership resolved before a
// photo upload. Request claims are never used as the binding source.
type PunchReceiptBinding struct{ Tenant, ReceiptRef, WorkerRef, SiteID string }

// PunchReceiptResolver resolves immutable punch ownership and site.
type PunchReceiptResolver interface {
	ResolvePunchReceipt(context.Context, string, string) (PunchReceiptBinding, error)
}

// PunchPhotoRecord is the durable photo reference and its worker scope.
// Bytes are never returned by this application port.
type PunchPhotoRecord struct {
	Tenant, ID, WorkerRef, PunchReceiptRef, ArtifactRef, SiteID string
	Capture                                                     clockdomain.PhotoCapture
}

// PunchPhotoStore owns immutable receipt links and append-only view audit.
type PunchPhotoStore interface {
	RecordPunchPhoto(context.Context, string, PunchPhotoRecord) (PunchPhotoRecord, error)
	GetPunchPhoto(context.Context, string, string) (PunchPhotoRecord, error)
	RecordPhotoView(context.Context, string, string, string, clockdomain.PhotoScope) error
	DeletePunchPhoto(context.Context, string, string) error
}

// PhotoArtifactStore is the only port allowed to receive or destroy photo
// bytes. It is deliberately separate from PunchPhotoStore.
type PhotoArtifactStore interface {
	UploadPhoto(context.Context, string, string, []byte, string) (string, error)
	DeletePhoto(context.Context, string, string) error
}

// PrivacyService integrates TCLOCK-006 and TCLOCK-007 without changing the
// existing clock Service contract. Missing ports fail closed before effects.
type PrivacyService struct {
	Policies  BiometricPolicyResolver
	Photos    PhotoPolicyResolver
	Authorize PrivacyAuthorizer
	Biometric BiometricAdmissionStore
	Eraser    BiometricEraser
	Retention BiometricRetentionChecker
	Receipts  PunchReceiptResolver
	Punches   PunchPhotoStore
	Artifacts PhotoArtifactStore
	Clock     func() time.Time
}

// BiometricEnrollmentRequest asks to admit a device-held or separately keyed
// biometric template. Raw image/template bytes are intentionally impossible.
type BiometricEnrollmentRequest struct {
	WorkerRef, Jurisdiction, TemplateCustodyRef, DeviceRef string
	Method                                                 clockdomain.IdentificationMethod
	Consent                                                clockdomain.BiometricConsent
	DevicePADDeclared                                      bool
}

// BiometricEnrollmentResult returns only auditable admission evidence.
type BiometricEnrollmentResult struct {
	Record BiometricAdmissionRecord
}

// EnrollBiometric validates current tenant policy and consent before recording
// a template reference. Authorization and all validation precede the write.
func (s PrivacyService) EnrollBiometric(ctx context.Context, p *trust.Principal, req BiometricEnrollmentRequest) (BiometricEnrollmentResult, error) {
	now, err := s.requireNow(p)
	if err != nil {
		return BiometricEnrollmentResult{}, err
	}
	if s.Policies == nil || s.Authorize == nil || s.Biometric == nil {
		return BiometricEnrollmentResult{}, ErrUnavailable
	}
	if strings.TrimSpace(req.WorkerRef) == "" || strings.TrimSpace(req.Jurisdiction) == "" || strings.TrimSpace(req.DeviceRef) == "" || !validCustodyRef(req.TemplateCustodyRef) {
		return BiometricEnrollmentResult{}, reject(ErrInvalidRequest, "biometric", "INVALID", "worker, jurisdiction and opaque template custody reference are required")
	}
	if req.Consent.WorkerRef != req.WorkerRef {
		return BiometricEnrollmentResult{}, reject(ErrInvalidRequest, "consent.worker", "MISMATCH", "consent must belong to the enrolled worker")
	}
	tenant := tenantOf(p)
	if err := s.Authorize.AuthorizeWorker(ctx, p, tenant, req.WorkerRef); err != nil {
		return BiometricEnrollmentResult{}, err
	}
	if err := s.Authorize.AuthorizeDevice(ctx, p, tenant, req.DeviceRef, req.WorkerRef); err != nil {
		return BiometricEnrollmentResult{}, err
	}
	evidence, err := s.Policies.BiometricPolicy(ctx, tenant, req.Jurisdiction)
	if err != nil {
		return BiometricEnrollmentResult{}, err
	}
	if err := validateBiometricEvidence(evidence, tenant, req.Jurisdiction); err != nil {
		return BiometricEnrollmentResult{}, err
	}
	admission, err := clockdomain.AdmitBiometricEnrollment(clockdomain.EnrollmentGateRequest{Policy: evidence.Policy, Consent: req.Consent, Method: req.Method, DevicePADDeclared: req.DevicePADDeclared, Now: now})
	if err != nil {
		return BiometricEnrollmentResult{}, err
	}
	record := BiometricAdmissionRecord{Tenant: tenant, Jurisdiction: req.Jurisdiction, PolicyDigest: evidence.Digest, TemplateCustodyRef: req.TemplateCustodyRef, DeviceRef: req.DeviceRef, State: "GRANTED", Admission: admission}
	stored, err := s.Biometric.RecordBiometric(ctx, tenant, record, p.Subject())
	if err != nil {
		return BiometricEnrollmentResult{}, err
	}
	if strings.TrimSpace(stored.ID) == "" || stored.Tenant != tenant || stored.Jurisdiction != req.Jurisdiction || stored.PolicyDigest != evidence.Digest || stored.TemplateCustodyRef != req.TemplateCustodyRef || stored.DeviceRef != req.DeviceRef || stored.Admission.Digest != admission.Digest {
		return BiometricEnrollmentResult{}, reject(ErrUnavailable, "biometric", "INVALID_STORE_RESULT", "store returned unbound admission")
	}
	return BiometricEnrollmentResult{Record: stored}, nil
}

// DestroyBiometric destroys a template key or device reference after the
// domain's earliest destruction date. The store enforces its hold lifecycle.
func (s PrivacyService) DestroyBiometric(ctx context.Context, p *trust.Principal, id, workerRef string) error {
	now, err := s.requireNow(p)
	if err != nil {
		return err
	}
	if s.Biometric == nil || s.Authorize == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(workerRef) == "" {
		return ErrUnavailable
	}
	if err := s.Authorize.AuthorizeWorker(ctx, p, tenantOf(p), workerRef); err != nil {
		return err
	}
	record, err := s.Biometric.GetBiometric(ctx, tenantOf(p), id)
	if err != nil {
		return err
	}
	if err := validateBiometricRecord(record, tenantOf(p), id); err != nil {
		return err
	}
	if record.Admission.WorkerRef != workerRef {
		return reject(ErrInvalidRequest, "biometric", "TENANT_OR_WORKER_MISMATCH", "admission is not bound to this tenant and worker")
	}
	if _, err := clockdomain.ExplainBiometricAdmission(record.Admission); err != nil {
		return err
	}
	if !now.Before(record.Admission.DestructionAt) {
		if strings.HasPrefix(strings.ToLower(record.TemplateCustodyRef), "keyed://") && (s.Eraser == nil || s.Retention == nil) {
			return ErrUnavailable
		}
		if s.Retention != nil {
			held, err := s.Retention.BiometricHeld(ctx, tenantOf(p), id)
			if err != nil {
				return err
			}
			if held {
				return reject(ErrInvalidRequest, "biometric", "HELD", "template destruction is protected by a hold or open exception")
			}
		}
		if s.Eraser != nil && strings.HasPrefix(strings.ToLower(record.TemplateCustodyRef), "keyed://") {
			if err := s.Eraser.DestroyBiometricKey(ctx, tenantOf(p), record.TemplateCustodyRef, p.Subject()); err != nil {
				return err
			}
		}
		return s.Biometric.DestroyBiometric(ctx, tenantOf(p), id, p.Subject())
	}
	return reject(ErrInvalidRequest, "biometric", "RETENTION_ACTIVE", "template destruction is not yet due")
}

// VerifyFaceComparison admits an already-enrolled face only while its
// recorded policy is current and only on the device that enrolled it.
func (s PrivacyService) VerifyFaceComparison(ctx context.Context, p *trust.Principal, id, workerRef, deviceRef string) error {
	now, err := s.requireNow(p)
	if err != nil {
		return err
	}
	if s.Biometric == nil || s.Policies == nil || s.Authorize == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(workerRef) == "" || strings.TrimSpace(deviceRef) == "" {
		return ErrUnavailable
	}
	if err := s.Authorize.AuthorizeWorker(ctx, p, tenantOf(p), workerRef); err != nil {
		return err
	}
	if err := s.Authorize.AuthorizeDevice(ctx, p, tenantOf(p), deviceRef, workerRef); err != nil {
		return err
	}
	record, err := s.Biometric.GetBiometric(ctx, tenantOf(p), id)
	if err != nil {
		return err
	}
	if err := validateBiometricRecord(record, tenantOf(p), id); err != nil {
		return err
	}
	if record.Admission.WorkerRef != workerRef || record.DeviceRef != deviceRef || record.Admission.Method != clockdomain.MethodFace || record.State != "GRANTED" {
		return reject(ErrInvalidRequest, "biometric", "DEVICE_OR_WORKER_MISMATCH", "face matching is restricted to the enrolled worker and device")
	}
	evidence, err := s.Policies.BiometricPolicy(ctx, tenantOf(p), record.Jurisdiction)
	if err != nil {
		return err
	}
	if err := validateBiometricEvidence(evidence, tenantOf(p), record.Jurisdiction); err != nil {
		return err
	}
	if evidence.Digest != record.PolicyDigest {
		return reject(ErrInvalidRequest, "biometric.policy", "DIGEST_MISMATCH", "admission is bound to a stale policy")
	}
	return clockdomain.ValidateBiometricAdmission(record.Admission, now)
}

// PunchPhotoRequest contains the raw photo only at the upload boundary.
type PunchPhotoRequest struct {
	WorkerRef, SiteID, PunchReceiptRef, MediaType string
	Bytes                                         []byte
}

// CapturePunchPhoto authorizes and validates policy before uploading bytes,
// then immutably links the resulting artifact to the punch receipt.
func (s PrivacyService) CapturePunchPhoto(ctx context.Context, p *trust.Principal, req PunchPhotoRequest) (PunchPhotoRecord, error) {
	now, err := s.requireNow(p)
	if err != nil {
		return PunchPhotoRecord{}, err
	}
	if s.Photos == nil || s.Authorize == nil || s.Punches == nil || s.Artifacts == nil || s.Receipts == nil {
		return PunchPhotoRecord{}, ErrUnavailable
	}
	if strings.TrimSpace(req.WorkerRef) == "" || strings.TrimSpace(req.SiteID) == "" || strings.TrimSpace(req.PunchReceiptRef) == "" || len(req.Bytes) == 0 || !strings.HasPrefix(strings.ToLower(req.MediaType), "image/") {
		return PunchPhotoRecord{}, reject(ErrInvalidRequest, "photo", "INVALID", "worker, site, receipt, image media type and bytes are required")
	}
	tenant := tenantOf(p)
	if err := s.Authorize.AuthorizeWorker(ctx, p, tenant, req.WorkerRef); err != nil {
		return PunchPhotoRecord{}, err
	}
	receipt, err := s.Receipts.ResolvePunchReceipt(ctx, tenant, req.PunchReceiptRef)
	if err != nil {
		return PunchPhotoRecord{}, err
	}
	if receipt.Tenant != tenant || receipt.ReceiptRef != req.PunchReceiptRef || receipt.WorkerRef != req.WorkerRef || receipt.SiteID != req.SiteID {
		return PunchPhotoRecord{}, reject(ErrInvalidRequest, "receipt", "MISMATCH", "receipt is not bound to the requested worker and site")
	}
	evidence, err := s.Photos.PhotoPolicy(ctx, tenant, req.SiteID)
	if err != nil {
		return PunchPhotoRecord{}, err
	}
	if err := validatePhotoEvidence(evidence, tenant, req.SiteID); err != nil {
		return PunchPhotoRecord{}, err
	}
	if err := evidence.Policy.Validate(); err != nil || !evidence.Policy.Enabled {
		return PunchPhotoRecord{}, reject(ErrInvalidRequest, "photo.policy", "INVALID", "enabled photo policy is required before upload")
	}
	if _, err := clockdomain.CapturePunchPhoto(evidence.Policy, req.PunchReceiptRef, "artifact://pending", now); err != nil {
		return PunchPhotoRecord{}, err
	}
	artifact, err := s.Artifacts.UploadPhoto(ctx, tenant, req.MediaType, append([]byte(nil), req.Bytes...), p.Subject())
	if err != nil {
		return PunchPhotoRecord{}, err
	}
	if !validArtifactRef(artifact) {
		return PunchPhotoRecord{}, reject(ErrUnavailable, "artifact", "INVALID_STORE_RESULT", "artifact store returned no opaque reference")
	}
	capture, err := clockdomain.CapturePunchPhoto(evidence.Policy, req.PunchReceiptRef, artifact, now)
	if err != nil {
		return PunchPhotoRecord{}, err
	}
	stored, err := s.Punches.RecordPunchPhoto(ctx, tenant, PunchPhotoRecord{Tenant: tenant, WorkerRef: req.WorkerRef, PunchReceiptRef: req.PunchReceiptRef, ArtifactRef: artifact, SiteID: req.SiteID, Capture: capture})
	if err != nil {
		_ = s.Artifacts.DeletePhoto(ctx, tenant, artifact)
		return PunchPhotoRecord{}, err
	}
	if stored.WorkerRef != req.WorkerRef || stored.SiteID != req.SiteID || stored.PunchReceiptRef != req.PunchReceiptRef || stored.ArtifactRef != artifact {
		_ = s.Artifacts.DeletePhoto(ctx, tenant, artifact)
		return PunchPhotoRecord{}, reject(ErrUnavailable, "photo", "MISMATCHED_STORE_RESULT", "photo store changed the authorized receipt binding")
	}
	if err := validatePhotoRecord(stored, tenant, stored.ID); err != nil {
		_ = s.Artifacts.DeletePhoto(ctx, tenant, artifact)
		return PunchPhotoRecord{}, err
	}
	return stored, nil
}

// ViewPunchPhoto authorizes against the stored worker/site binding before
// returning the opaque artifact reference and appending a view audit event.
func (s PrivacyService) ViewPunchPhoto(ctx context.Context, p *trust.Principal, id string, scope clockdomain.PhotoScope) (PunchPhotoRecord, error) {
	if _, err := s.requireNow(p); err != nil {
		return PunchPhotoRecord{}, err
	}
	if s.Punches == nil || s.Authorize == nil || strings.TrimSpace(id) == "" {
		return PunchPhotoRecord{}, ErrUnavailable
	}
	record, err := s.Punches.GetPunchPhoto(ctx, tenantOf(p), id)
	if err != nil {
		return PunchPhotoRecord{}, err
	}
	if err := validatePhotoRecord(record, tenantOf(p), id); err != nil {
		return PunchPhotoRecord{}, err
	}
	isWorker := false
	if scope == clockdomain.PhotoScopeWorkerSelf {
		if err := s.Authorize.AuthorizeWorker(ctx, p, tenantOf(p), record.WorkerRef); err != nil {
			return PunchPhotoRecord{}, err
		}
		isWorker = true
	}
	hasScope := false
	if scope == clockdomain.PhotoScopeSupervisorReview {
		if err := s.Authorize.AuthorizeSupervisor(ctx, p, tenantOf(p), record.SiteID, record.WorkerRef); err != nil {
			return PunchPhotoRecord{}, err
		}
		hasScope = true
	}
	if err := clockdomain.AuthorizeView(scope, isWorker, hasScope); err != nil {
		return PunchPhotoRecord{}, err
	}
	if err := s.Punches.RecordPhotoView(ctx, tenantOf(p), id, p.Subject(), scope); err != nil {
		return PunchPhotoRecord{}, err
	}
	return record, nil
}

// DestroyPunchPhoto deletes the artifact only after current policy, hold and
// exception checks pass, then clears the durable receipt reference.
func (s PrivacyService) DestroyPunchPhoto(ctx context.Context, p *trust.Principal, id string) error {
	now, err := s.requireNow(p)
	if err != nil {
		return err
	}
	if s.Punches == nil || s.Photos == nil || s.Artifacts == nil || s.Authorize == nil || strings.TrimSpace(id) == "" {
		return ErrUnavailable
	}
	record, err := s.Punches.GetPunchPhoto(ctx, tenantOf(p), id)
	if err != nil {
		return err
	}
	if err := validatePhotoRecord(record, tenantOf(p), id); err != nil {
		return err
	}
	if err := s.Authorize.AuthorizeSupervisor(ctx, p, tenantOf(p), record.SiteID, record.WorkerRef); err != nil {
		return err
	}
	evidence, err := s.Photos.PhotoPolicy(ctx, tenantOf(p), record.SiteID)
	if err != nil {
		return err
	}
	if err := validatePhotoEvidence(evidence, tenantOf(p), record.SiteID); err != nil {
		return err
	}
	due, err := record.Capture.DueForDeletion(evidence.Policy, now)
	if err != nil {
		return err
	}
	if !due {
		return reject(ErrInvalidRequest, "photo", "RETENTION_ACTIVE_OR_HELD", "photo is still retained or protected by a hold/exception")
	}
	if err := s.Artifacts.DeletePhoto(ctx, tenantOf(p), record.ArtifactRef); err != nil {
		return err
	}
	return s.Punches.DeletePunchPhoto(ctx, tenantOf(p), id)
}

func (s PrivacyService) requireNow(p *trust.Principal) (time.Time, error) {
	if err := validPrincipal(p); err != nil {
		return time.Time{}, err
	}
	if s.Clock == nil {
		return time.Time{}, ErrUnavailable
	}
	now := s.Clock().UTC()
	if now.IsZero() || now.Before(p.IssuedAt()) || !now.Before(p.ExpiresAt()) {
		return time.Time{}, ErrInvalidPrincipal
	}
	return now, nil
}

func (s PrivacyService) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func validateBiometricRecord(r BiometricAdmissionRecord, tenant, id string) error {
	if r.ID != id || r.Tenant != tenant || strings.TrimSpace(r.Jurisdiction) == "" || strings.TrimSpace(r.PolicyDigest) == "" || strings.TrimSpace(r.TemplateCustodyRef) == "" || strings.TrimSpace(r.DeviceRef) == "" || r.State == "" || (id == "") {
		return reject(ErrInvalidRequest, "biometric", "UNBOUND_STORE_RESULT", "stored admission is not tenant and lifecycle bound")
	}
	if _, err := clockdomain.ExplainBiometricAdmission(r.Admission); err != nil {
		return err
	}
	return nil
}

func validatePhotoRecord(r PunchPhotoRecord, tenant, id string) error {
	if strings.TrimSpace(id) == "" || r.Tenant != tenant || r.ID != id || strings.TrimSpace(r.WorkerRef) == "" || strings.TrimSpace(r.PunchReceiptRef) == "" || strings.TrimSpace(r.ArtifactRef) == "" || strings.TrimSpace(r.SiteID) == "" || r.Capture.PunchReceiptRef != r.PunchReceiptRef || r.Capture.ArtifactRef != r.ArtifactRef || r.Capture.SiteRef != r.SiteID {
		return reject(ErrInvalidRequest, "photo", "UNBOUND_STORE_RESULT", "stored photo is not tenant, receipt and artifact bound")
	}
	if _, err := clockdomain.ExplainPhotoCapture(r.Capture); err != nil {
		return err
	}
	return nil
}

func validateBiometricEvidence(e BiometricPolicyEvidence, tenant, jurisdiction string) error {
	if !e.Current || e.Tenant != tenant || e.Jurisdiction != jurisdiction || strings.TrimSpace(e.Digest) == "" || e.Policy.Tenant.String() != tenant || e.Policy.Jurisdiction != jurisdiction {
		return reject(ErrInvalidRequest, "biometric.policy", "STALE_OR_UNBOUND", "current tenant and jurisdiction policy evidence is required")
	}
	return nil
}

func validatePhotoEvidence(e PhotoPolicyEvidence, tenant, site string) error {
	if !e.Current || e.Tenant != tenant || e.SiteID != site || strings.TrimSpace(e.Digest) == "" || e.Policy.SiteRef != site {
		return reject(ErrInvalidRequest, "photo.policy", "STALE_OR_UNBOUND", "current tenant and site policy evidence is required")
	}
	return nil
}

func validCustodyRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	return (strings.HasPrefix(ref, "device://") || strings.HasPrefix(ref, "keyed://")) && !strings.Contains(ref, "image") && !strings.Contains(ref, "raw")
}

func validArtifactRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	return ref != "" && !strings.ContainsAny(ref, "\r\n")
}
