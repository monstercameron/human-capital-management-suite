// Package intake releases scanned uploads as governed evidence references.
// It intentionally contains no raw bytes: documentsecurity owns scanning and
// derivative generation, while this package owns the document/evidence
// boundary and its access controls.
package intake

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
)

const MedicalSensitive = "MEDICAL_SENSITIVE"

var (
	ErrInvalid       = errors.New("document intake: invalid request")
	ErrNotReleasable = errors.New("document intake: artifact is not releasable")
	ErrRevoked       = errors.New("document intake: evidence reference is revoked")
)

// Request contains only caller-supplied context. Classification, document
// type and governance controls are server-derived and cannot be selected by a
// caller.
type Request struct {
	ArtifactID  string
	Purpose     string
	SubjectID   string
	RequestedBy string
	Now         time.Time
}

// AccessPolicy describes the narrow purpose and compartment in which evidence
// may be disclosed. It is part of the immutable release record.
type AccessPolicy struct {
	Purpose     string
	Compartment string
	Read        bool
	Download    bool
}

type RetentionPolicy struct {
	PolicyRef string
	Hold      bool
}

// EvidenceRef is the only value that downstream domains receive. It contains
// no locator or bytes. Usable consults the current security verdict so a later
// rescan or revoke immediately invalidates it.
type EvidenceRef struct {
	ID               string
	ArtifactID       string
	ArtifactDigest   string
	DerivativeDigest string
	DocumentType     string
	Classification   string
	Compartment      string
	Authority        string
	Provenance       string
	Purpose          string
	Access           AccessPolicy
	Retention        RetentionPolicy
	IssuedAt         time.Time
	registry         *documentsecurity.Registry
	issuer           *Service
}

func (r EvidenceRef) Validate() error {
	if r.ID == "" || r.ArtifactID == "" || r.ArtifactDigest == "" || r.DerivativeDigest == "" ||
		r.DocumentType == "" || r.Classification != MedicalSensitive || r.Compartment != MedicalSensitive ||
		r.Authority == "" || r.Provenance == "" || r.Purpose == "" || !r.Access.Read || r.Access.Download || r.Access.Purpose != r.Purpose ||
		r.Retention.PolicyRef == "" || r.IssuedAt.IsZero() || r.registry == nil || r.issuer == nil {
		return ErrInvalid
	}
	if !r.issuer.issuedReferenceMatches(r) {
		return ErrInvalid
	}
	return nil
}

func (r EvidenceRef) Usable() bool {
	if r.Validate() != nil {
		return false
	}
	a, ok := r.registry.Get(r.ArtifactID)
	return ok && a.State == documentsecurity.Safe && a.DerivativeDigest == r.DerivativeDigest
}

// Check is the explicit fail-closed guard for consumers.
func (r EvidenceRef) Check() error {
	if err := r.Validate(); err != nil {
		return err
	}
	if !r.Usable() {
		return ErrRevoked
	}
	return nil
}

type Package struct {
	ID           string
	Evidence     EvidenceRef
	DocumentType string
	Metadata     map[string]string
}

func (p Package) Validate() error {
	if p.ID == "" || p.DocumentType == "" || p.Evidence.ID == "" || p.Evidence.ID != "evidence:"+p.ID {
		return ErrInvalid
	}
	if err := p.Evidence.Validate(); err != nil {
		return err
	}
	return nil
}

// Service derives governed intake packages from already scanned artifacts.
type Service struct {
	Registry *documentsecurity.Registry
	mu       sync.Mutex
	sequence uint64
	issued   map[string]EvidenceRef
}

func NewService(registry *documentsecurity.Registry) *Service {
	return &Service{Registry: registry, issued: make(map[string]EvidenceRef)}
}

func (s *Service) issuedReferenceMatches(ref EvidenceRef) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	issued, ok := s.issued[ref.ID]
	return ok && issued == ref
}

func (s *Service) Release(req Request) (Package, error) {
	if s == nil || s.Registry == nil || strings.TrimSpace(req.ArtifactID) == "" || strings.TrimSpace(req.Purpose) == "" || strings.TrimSpace(req.SubjectID) == "" || strings.TrimSpace(req.RequestedBy) == "" {
		return Package{}, ErrInvalid
	}
	a, ok := s.Registry.Get(req.ArtifactID)
	if !ok || a.State != documentsecurity.Safe || a.DerivativeDigest == "" {
		return Package{}, ErrNotReleasable
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	} else {
		req.Now = req.Now.UTC()
	}
	s.mu.Lock()
	s.sequence++
	n := s.sequence
	s.mu.Unlock()
	id := fmt.Sprintf("%s-%d", req.ArtifactID, n)
	ref := EvidenceRef{ID: "evidence:" + id, ArtifactID: a.ID, ArtifactDigest: a.OriginalDigest, DerivativeDigest: a.DerivativeDigest,
		DocumentType: deriveType(a.DetectedContentType), Classification: MedicalSensitive, Compartment: MedicalSensitive,
		Authority: "HCM_NEXT_EVIDENCE_AUTHORITY/v1", Provenance: "scanner:" + a.Scanner + "@" + a.ScannerVersion,
		Purpose: req.Purpose, Access: AccessPolicy{Purpose: req.Purpose, Compartment: MedicalSensitive, Read: true},
		Retention: RetentionPolicy{PolicyRef: "records.medical-sensitive/v1"}, IssuedAt: req.Now, registry: s.Registry, issuer: s}
	s.mu.Lock()
	s.issued[ref.ID] = ref
	s.mu.Unlock()
	p := Package{ID: id, Evidence: ref, DocumentType: ref.DocumentType, Metadata: map[string]string{"subject_id": req.SubjectID}}
	if err := p.Validate(); err != nil {
		return Package{}, err
	}
	return p, nil
}

func deriveType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "application/pdf":
		return "PDF"
	case "image/jpeg":
		return "JPEG"
	case "image/png":
		return "PNG"
	case "text/plain":
		return "TEXT"
	}
	return "BINARY"
}
