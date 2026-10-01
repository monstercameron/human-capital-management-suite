package project

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/artifactstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type attachmentAuthorizer struct{ revoked bool }

func (a *attachmentAuthorizer) AuthorizeTask(_ context.Context, _ TaskAttachmentAccess) error {
	if a.revoked {
		return errors.New("revoked")
	}
	return nil
}

type attachmentSecurity struct {
	malware  bool
	decision dlp.Decision
}

func (s attachmentSecurity) Check(_ context.Context, _ []byte) (AttachmentSecurityResult, error) {
	return AttachmentSecurityResult{Decision: s.decision, Malware: s.malware}, nil
}

type attachmentStore struct {
	object  artifactstore.Object
	payload []byte
}

func (s *attachmentStore) Put(_ context.Context, req artifactstore.PutRequest, payload []byte) error {
	if req.ExistingKey {
		return errors.New("overwrite")
	}
	s.object = req.Object
	s.payload = append([]byte(nil), payload...)
	return nil
}

func (s *attachmentStore) Get(_ context.Context, object artifactstore.Object) ([]byte, error) {
	if object.Key != s.object.Key || object.VersionID != s.object.VersionID {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), s.payload...), nil
}

func attachmentService(t *testing.T, auth *attachmentAuthorizer, security attachmentSecurity, store *attachmentStore) AttachmentService {
	t.Helper()
	return AttachmentService{Policy: AttachmentPolicy{MaxBytes: 1024, Retention: 24 * time.Hour, AllowedContentTypes: map[string]bool{"text/plain": true}}, Artifact: artifactstore.Policy{Endpoint: "https://objects", Region: "us-east-1", Private: true, TLSRequired: true, Versioning: true, ObjectLock: true, EncryptionRequired: true, TenantKeyReferences: true, RestoreReadVerification: true, DefaultRetention: time.Hour, Lifecycle: []artifactstore.LifecycleRule{{Class: "project", ExpireAfter: 365 * 24 * time.Hour}}}, Authorize: auth, Security: security, Store: store}
}

func attachmentRequest(now time.Time) UploadAttachmentRequest {
	return UploadAttachmentRequest{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", Principal: "user-a", AttachmentID: "attachment-a", VersionID: "version-1", ContentType: "text/plain", Region: "us-east-1", KeyReference: "kms://tenant-a/project", Bytes: []byte("safe project evidence"), CreatedAt: now}
}

func TestTodo_PM_071(t *testing.T) {
	auth := &attachmentAuthorizer{}
	store := &attachmentStore{}
	service := attachmentService(t, auth, attachmentSecurity{decision: dlp.Allow}, store)
	now := time.Unix(100, 0).UTC()
	attachment, err := service.Upload(context.Background(), attachmentRequest(now))
	if err != nil || attachment.RecordSeries != RecordSeriesAttachments || attachment.RecordCopy.ID == "" || attachment.Object.VersionID != "version-1" {
		t.Fatalf("protected upload = %+v, %v", attachment, err)
	}
	payload, err := service.Download(context.Background(), attachment, DownloadAttachmentRequest{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", Principal: "user-a", At: now.Add(time.Minute)})
	if err != nil || string(payload) != "safe project evidence" {
		t.Fatalf("protected download = %q, %v", payload, err)
	}
}

func TestTodo_PM_071_Security(t *testing.T) {
	auth := &attachmentAuthorizer{}
	store := &attachmentStore{}
	now := time.Unix(100, 0).UTC()
	malware := attachmentService(t, auth, attachmentSecurity{malware: true, decision: dlp.Allow}, store)
	if _, err := malware.Upload(context.Background(), attachmentRequest(now)); !errors.Is(err, ErrAttachmentMalware) {
		t.Fatalf("malware upload = %v", err)
	}
	dlpDenied := attachmentService(t, auth, attachmentSecurity{decision: dlp.Refuse}, store)
	if _, err := dlpDenied.Upload(context.Background(), attachmentRequest(now)); !errors.Is(err, ErrAttachmentDLPDenied) {
		t.Fatalf("DLP denied upload = %v", err)
	}
	service := attachmentService(t, auth, attachmentSecurity{decision: dlp.Allow}, store)
	attachment, err := service.Upload(context.Background(), attachmentRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	auth.revoked = true
	if _, err := service.Download(context.Background(), attachment, DownloadAttachmentRequest{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", Principal: "user-a", At: now.Add(time.Minute)}); !errors.Is(err, ErrAttachmentUnauthorized) {
		t.Fatalf("revoked task download = %v", err)
	}
	tampered := attachment
	store.payload = []byte("unsafe replacement")
	auth.revoked = false
	if _, err := service.Download(context.Background(), tampered, DownloadAttachmentRequest{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", Principal: "user-a", At: now.Add(time.Minute)}); !errors.Is(err, ErrAttachmentSecurity) {
		t.Fatalf("tampered download = %v", err)
	}
}

func TestTodo_PM_071_Integration(t *testing.T) {
	auth := &attachmentAuthorizer{}
	store := &attachmentStore{}
	service := attachmentService(t, auth, attachmentSecurity{decision: dlp.Allow}, store)
	now := time.Unix(100, 0).UTC()
	attachment, err := service.Upload(context.Background(), attachmentRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	if attachment.Size != int64(len(store.payload)) || attachment.Object.Digest != artifactstore.Digest(store.payload) || attachment.InspectionDigest == "" {
		t.Fatalf("stored protected metadata = %+v store=%+v", attachment, store)
	}
	if _, err := service.Upload(context.Background(), func() UploadAttachmentRequest {
		req := attachmentRequest(now)
		req.AttachmentID = "../escape"
		return req
	}()); !errors.Is(err, ErrAttachmentInvalid) {
		t.Fatalf("path escaping attachment = %v", err)
	}
}
