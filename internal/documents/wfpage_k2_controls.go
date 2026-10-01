// Package documents exposes the typed document controls consumed by workflow
// input pages. Raw uploads stay in documentsecurity; page inputs receive only
// an admitted evidence reference or a proof-bound signature ceremony.
package documents

import (
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/documents/intake"
	"github.com/monstercameron/human-capital-management-suite/internal/documents/signing"
)

var (
	ErrAttachmentAdmission = errors.New("documents: attachment admission failed")
	ErrAssuranceLevel      = errors.New("documents: invalid signature assurance level")
)

type AttachmentRequest struct {
	ArtifactID  string
	Purpose     string
	SubjectID   string
	RequestedBy string
}

// AttachmentReference contains no locator or bytes. It is the typed value a
// workflow input may retain after quarantine and admission.
type AttachmentReference struct {
	ID           string
	ArtifactID   string
	DocumentType string
	Evidence     intake.EvidenceRef
}

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

type AssuranceLevel string

const (
	AssuranceBasic     AssuranceLevel = "BASIC"
	AssuranceEnhanced  AssuranceLevel = "ENHANCED"
	AssuranceQualified AssuranceLevel = "QUALIFIED"
)

func (a AssuranceLevel) valid() bool {
	return a == AssuranceBasic || a == AssuranceEnhanced || a == AssuranceQualified
}

type SignatureRequest struct {
	ID             string
	ArtifactHash   string
	SignerID       string
	AssuranceLevel AssuranceLevel
	TrustedNow     int64
	ExpiresAt      int64
}

type Signature struct {
	Ceremony       signing.Ceremony
	AssuranceLevel AssuranceLevel
}

func BeginSignature(request SignatureRequest) (Signature, error) {
	if !request.AssuranceLevel.valid() {
		return Signature{}, ErrAssuranceLevel
	}
	ceremony, err := signing.Request(request.ID, request.ArtifactHash, request.SignerID, request.TrustedNow, request.ExpiresAt)
	if err != nil {
		return Signature{}, err
	}
	return Signature{Ceremony: ceremony, AssuranceLevel: request.AssuranceLevel}, nil
}

func (s Signature) Validate() error {
	if !s.AssuranceLevel.valid() || strings.TrimSpace(s.Ceremony.ID) == "" {
		return ErrAssuranceLevel
	}
	return s.Ceremony.Verify()
}

func (s Signature) AssuranceLabel() string { return string(s.AssuranceLevel) }

func (s Signature) Sign(proof, artifactHash, callbackID string, trustedNow int64) (Signature, error) {
	ceremony, err := s.Ceremony.Sign(proof, artifactHash, callbackID, trustedNow)
	if err != nil {
		return Signature{}, err
	}
	return Signature{Ceremony: ceremony, AssuranceLevel: s.AssuranceLevel}, nil
}
