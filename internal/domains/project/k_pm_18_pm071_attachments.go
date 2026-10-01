package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/artifactstore"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/records"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

var (
	ErrAttachmentInvalid      = errors.New("project: invalid attachment")
	ErrAttachmentUnauthorized = errors.New("project: attachment task access denied")
	ErrAttachmentSecurity     = errors.New("project: attachment security check failed")
	ErrAttachmentMalware      = errors.New("project: attachment malware detected")
	ErrAttachmentDLPDenied    = errors.New("project: attachment denied by DLP")
	ErrAttachmentExpired      = errors.New("project: attachment retention expired")
	ErrAttachmentRevoked      = errors.New("project: attachment access revoked")
	ErrAttachmentStore        = errors.New("project: attachment store unavailable")
)

const AttachmentRecordSeries RecordSeries = RecordSeriesAttachments

type AttachmentPolicy struct {
	MaxBytes            int64
	AllowedContentTypes map[string]bool
	Retention           time.Duration
}

func (p AttachmentPolicy) validate() error {
	if p.MaxBytes <= 0 || p.Retention <= 0 || len(p.AllowedContentTypes) == 0 {
		return ErrAttachmentInvalid
	}
	for contentType, allowed := range p.AllowedContentTypes {
		if strings.TrimSpace(contentType) == "" || !allowed {
			return ErrAttachmentInvalid
		}
	}
	return nil
}

type TaskAttachmentAccess struct {
	TenantID  string
	ProjectID string
	TaskID    string
	Principal string
	Operation string
}

type TaskAttachmentAuthorizer interface {
	AuthorizeTask(context.Context, TaskAttachmentAccess) error
}

type AttachmentSecurityResult struct {
	Inspection dlp.Inspection
	Decision   dlp.Decision
	Malware    bool
}

type AttachmentSecurity interface {
	Check(context.Context, []byte) (AttachmentSecurityResult, error)
}

type ProtectedAttachmentStore interface {
	Put(context.Context, artifactstore.PutRequest, []byte) error
	Get(context.Context, artifactstore.Object) ([]byte, error)
}

type AttachmentService struct {
	Policy    AttachmentPolicy
	Artifact  artifactstore.Policy
	Authorize TaskAttachmentAuthorizer
	Security  AttachmentSecurity
	Store     ProtectedAttachmentStore
}

type UploadAttachmentRequest struct {
	TenantID     string
	ProjectID    string
	TaskID       string
	Principal    string
	AttachmentID string
	VersionID    string
	ContentType  string
	Region       string
	KeyReference string
	Bytes        []byte
	CreatedAt    time.Time
}

type ProtectedAttachment struct {
	ID               string
	TenantID         string
	ProjectID        string
	TaskID           string
	ContentType      string
	Size             int64
	Object           artifactstore.Object
	RecordSeries     RecordSeries
	RecordCopy       records.DeletableCopy
	InspectionDigest string
	Revision         uint64
	RevokedAt        time.Time
}

func (s AttachmentService) Upload(ctx context.Context, req UploadAttachmentRequest) (ProtectedAttachment, error) {
	if err := s.Policy.validate(); err != nil || s.Authorize == nil || s.Security == nil || s.Store == nil || strings.TrimSpace(req.TenantID) == "" || !safeAttachmentID(req.ProjectID) || !safeAttachmentID(req.TaskID) || strings.TrimSpace(req.Principal) == "" || !safeAttachmentID(req.AttachmentID) || !safeAttachmentID(req.VersionID) || strings.TrimSpace(req.ContentType) == "" || req.CreatedAt.IsZero() || int64(len(req.Bytes)) > s.Policy.MaxBytes {
		return ProtectedAttachment{}, ErrAttachmentInvalid
	}
	if !s.Policy.AllowedContentTypes[req.ContentType] {
		return ProtectedAttachment{}, ErrAttachmentInvalid
	}
	if err := s.Authorize.AuthorizeTask(ctx, TaskAttachmentAccess{TenantID: req.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID, Principal: req.Principal, Operation: "ATTACH"}); err != nil {
		return ProtectedAttachment{}, fmt.Errorf("%w: %v", ErrAttachmentUnauthorized, err)
	}
	security, err := s.Security.Check(ctx, req.Bytes)
	if err != nil {
		return ProtectedAttachment{}, fmt.Errorf("%w: %v", ErrAttachmentSecurity, err)
	}
	if security.Malware {
		return ProtectedAttachment{}, ErrAttachmentMalware
	}
	if security.Decision != dlp.Allow {
		return ProtectedAttachment{}, ErrAttachmentDLPDenied
	}
	object := artifactstore.Object{TenantID: req.TenantID, Key: attachmentObjectKey(req.ProjectID, req.TaskID, req.AttachmentID), VersionID: req.VersionID, Region: req.Region, Digest: artifactstore.Digest(req.Bytes), KeyReference: req.KeyReference, CreatedAt: req.CreatedAt, RetainUntil: req.CreatedAt.Add(s.Policy.Retention)}
	decision := artifactstore.EvaluatePut(s.Artifact, artifactstore.PutRequest{Object: object})
	if !decision.Allowed {
		return ProtectedAttachment{}, fmt.Errorf("%w: %s", ErrAttachmentInvalid, decision.Code)
	}
	if err := s.Store.Put(ctx, artifactstore.PutRequest{Object: object}, req.Bytes); err != nil {
		return ProtectedAttachment{}, fmt.Errorf("%w: %v", ErrAttachmentStore, err)
	}
	return ProtectedAttachment{ID: req.AttachmentID, TenantID: req.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID, ContentType: req.ContentType, Size: int64(len(req.Bytes)), Object: object, RecordSeries: AttachmentRecordSeries, RecordCopy: records.DeletableCopy{ID: "copy:" + req.AttachmentID, Kind: records.CopyKindCanonical, Tenant: req.TenantID}, InspectionDigest: security.Inspection.Digest(), Revision: 1}, nil
}

type DownloadAttachmentRequest struct {
	TenantID  string
	ProjectID string
	TaskID    string
	Principal string
	At        time.Time
}

func (s AttachmentService) Download(ctx context.Context, attachment ProtectedAttachment, req DownloadAttachmentRequest) ([]byte, error) {
	if s.Authorize == nil || s.Store == nil || strings.TrimSpace(req.TenantID) == "" || req.TenantID != attachment.TenantID || req.ProjectID != attachment.ProjectID || req.TaskID != attachment.TaskID || strings.TrimSpace(req.Principal) == "" || req.At.IsZero() {
		return nil, ErrAttachmentInvalid
	}
	if !attachment.RevokedAt.IsZero() {
		return nil, ErrAttachmentRevoked
	}
	if req.At.After(attachment.Object.RetainUntil) {
		return nil, ErrAttachmentExpired
	}
	access := TaskAttachmentAccess{TenantID: req.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID, Principal: req.Principal, Operation: "READ_ATTACHMENT"}
	if err := s.Authorize.AuthorizeTask(ctx, access); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAttachmentUnauthorized, err)
	}
	payload, err := s.Store.Get(ctx, attachment.Object)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAttachmentStore, err)
	}
	// Recheck after the store read. A revoke concurrent with the fetch must not
	// turn an already-started read into a successful response.
	if err := s.Authorize.AuthorizeTask(ctx, access); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAttachmentUnauthorized, err)
	}
	if artifactstore.Digest(payload) != attachment.Object.Digest {
		return nil, ErrAttachmentSecurity
	}
	return append([]byte(nil), payload...), nil
}

func attachmentObjectKey(projectID, taskID, attachmentID string) string {
	return "project/" + projectID + "/task/" + taskID + "/attachment/" + attachmentID
}

func safeAttachmentID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
