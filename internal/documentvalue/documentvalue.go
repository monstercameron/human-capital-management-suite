// Package documentvalue holds the document and signature values that workflow
// input pages carry across the browser boundary. It imports only the pure
// e-signature ceremony (internal/esign), so the browser client can link it.
//
// The one server-only piece, the admission state of an attachment, stays out
// of this package: EvidenceAdmission is the seam through which the server's
// evidence reference answers its fail-closed check.
package documentvalue

import (
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/esign"
)

// ErrAssuranceLevel reports a signature assurance level outside the closed
// vocabulary, or a signature whose ceremony cannot be verified.
var ErrAssuranceLevel = errors.New("documents: invalid signature assurance level")

// EvidenceAdmission is the fail-closed admission guard of a server-issued
// evidence reference. Check consults server state (the security registry and
// the issuing service), so the implementation lives on the server. intake's
// EvidenceRef implements it.
type EvidenceAdmission interface {
	Check() error
}

// AttachmentReference contains no locator or bytes. It is the typed value a
// workflow input may retain after quarantine and admission.
type AttachmentReference struct {
	ID           string
	ArtifactID   string
	DocumentType string
	Evidence     EvidenceAdmission
}

// AssuranceLevel is the closed signature assurance vocabulary.
type AssuranceLevel string

const (
	AssuranceBasic     AssuranceLevel = "BASIC"
	AssuranceEnhanced  AssuranceLevel = "ENHANCED"
	AssuranceQualified AssuranceLevel = "QUALIFIED"
)

// Valid reports whether the level is in the closed vocabulary.
func (a AssuranceLevel) Valid() bool {
	return a == AssuranceBasic || a == AssuranceEnhanced || a == AssuranceQualified
}

// Signature is a proof-bound signature ceremony with its assurance level.
type Signature struct {
	Ceremony       esign.Ceremony
	AssuranceLevel AssuranceLevel
}

// Validate fails closed unless the assurance level is known, the ceremony has
// an identifier, and its seal recomputes.
func (s Signature) Validate() error {
	if !s.AssuranceLevel.Valid() || strings.TrimSpace(s.Ceremony.ID) == "" {
		return ErrAssuranceLevel
	}
	return s.Ceremony.Verify()
}

// AssuranceLabel returns the assurance level as its wire string.
func (s Signature) AssuranceLabel() string { return string(s.AssuranceLevel) }

// Sign advances the ceremony with the signer's proof and returns the updated
// signature. The assurance level travels unchanged.
func (s Signature) Sign(proof, artifactHash, callbackID string, trustedNow int64) (Signature, error) {
	ceremony, err := s.Ceremony.Sign(proof, artifactHash, callbackID, trustedNow)
	if err != nil {
		return Signature{}, err
	}
	return Signature{Ceremony: ceremony, AssuranceLevel: s.AssuranceLevel}, nil
}
