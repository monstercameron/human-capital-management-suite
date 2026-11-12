// Package documents exposes the typed document controls consumed by workflow
// input pages. Raw uploads stay in documentsecurity; page inputs receive only
// an admitted evidence reference or a proof-bound signature ceremony.
//
// The page-facing value types live in internal/documentvalue so the browser
// client can link them; this package re-exports them and owns admission.
package documents

import (
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/documents/intake"
	"github.com/monstercameron/human-capital-management-suite/internal/documentvalue"
	"github.com/monstercameron/human-capital-management-suite/internal/esign"
)

var ErrAttachmentAdmission = errors.New("documents: attachment admission failed")

// ErrAssuranceLevel is the leaf's sentinel, so errors.Is holds across both paths.
var ErrAssuranceLevel = documentvalue.ErrAssuranceLevel

type AttachmentRequest struct {
	ArtifactID  string
	Purpose     string
	SubjectID   string
	RequestedBy string
}

// AttachmentReference is the leaf value. Its Evidence field holds the
// server's intake.EvidenceRef, which answers the fail-closed Check.
type AttachmentReference = documentvalue.AttachmentReference

func AdmitAttachment(service *intake.Service, request AttachmentRequest) (AttachmentReference, error) {
	if service == nil {
		return AttachmentReference{}, ErrAttachmentAdmission
	}
	pkg, err := service.Release(intake.Request{ArtifactID: request.ArtifactID, Purpose: request.Purpose, SubjectID: request.SubjectID, RequestedBy: request.RequestedBy})
	if err != nil {
		return AttachmentReference{}, err
	}
	if err := pkg.Evidence.Check(); err != nil {
		return AttachmentReference{}, err
	}
	return AttachmentReference{ID: pkg.ID, ArtifactID: pkg.Evidence.ArtifactID, DocumentType: pkg.DocumentType, Evidence: pkg.Evidence}, nil
}

type AssuranceLevel = documentvalue.AssuranceLevel

const (
	AssuranceBasic     = documentvalue.AssuranceBasic
	AssuranceEnhanced  = documentvalue.AssuranceEnhanced
	AssuranceQualified = documentvalue.AssuranceQualified
)

type SignatureRequest struct {
	ID             string
	ArtifactHash   string
	SignerID       string
	AssuranceLevel AssuranceLevel
	TrustedNow     int64
	ExpiresAt      int64
}

type Signature = documentvalue.Signature

func BeginSignature(request SignatureRequest) (Signature, error) {
	if !request.AssuranceLevel.Valid() {
		return Signature{}, ErrAssuranceLevel
	}
	ceremony, err := esign.Request(request.ID, request.ArtifactHash, request.SignerID, request.TrustedNow, request.ExpiresAt)
	if err != nil {
		return Signature{}, err
	}
	return Signature{Ceremony: ceremony, AssuranceLevel: request.AssuranceLevel}, nil
}
