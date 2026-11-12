package attestation

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	bindingSchema = "hcmnext.domains.attestation.Binding"
)

// ContextDigest represents the digest of the context under which a statement
// was bound (subject state as-of, jurisdiction, locale, rendered text).
type ContextDigest struct {
	SubjectAsOf      string // digest of subject state at time of binding
	Jurisdiction     string // identifier of the jurisdiction
	Locale           string // locale code (e.g., "en-US")
	RenderedTextHash string // digest of the rendered statement text
}

// Validate reports whether the context digest is well-formed.
func (c ContextDigest) Validate() error {
	if c.SubjectAsOf == "" {
		return fmt.Errorf("attestation: subject as-of digest is required")
	}
	if c.Jurisdiction == "" {
		return fmt.Errorf("attestation: jurisdiction in context digest is required")
	}
	if c.Locale == "" {
		return fmt.Errorf("attestation: locale in context digest is required")
	}
	if c.RenderedTextHash == "" {
		return fmt.Errorf("attestation: rendered text hash in context digest is required")
	}
	return nil
}

// EvidenceBinding represents a bound reference to evidence.
type EvidenceBinding struct {
	EvidenceID   string // identifier of the evidence
	EvidenceHash string // exact digest of the evidence at binding time
}

// Validate reports whether the evidence binding is well-formed.
func (e EvidenceBinding) Validate() error {
	if e.EvidenceID == "" {
		return fmt.Errorf("attestation: evidence id in binding is required")
	}
	if e.EvidenceHash == "" {
		return fmt.Errorf("attestation: evidence hash in binding is required")
	}
	return nil
}

// Binding binds an AttestationStatement to exact evidence digests and
// context digests. Any drift in evidence or context invalidates the binding.
type Binding struct {
	// StatementID identifies the statement being bound.
	StatementID string
	// StatementVersion is the version of the statement at binding time.
	StatementVersion uint64
	// ContextDigest captures the exact subject state, jurisdiction, locale, and text
	// at the time the statement was created.
	ContextDigest ContextDigest
	// EvidenceBindings are the exact digests of all evidence presented at binding time.
	EvidenceBindings []EvidenceBinding
	// BindingVersion tracks rebindings; initially 1, incremented on each rebind.
	BindingVersion uint64
	// BoundAt is the instant when this binding was created.
	BoundAt values.Instant
	// Signer identifies who made this binding.
	Signer values.EntityRef
}

// Validation errors for bindings.
var (
	ErrBindingStatementEmpty    = errors.New("attestation: binding statement id is empty")
	ErrBindingStatementVersion  = errors.New("attestation: binding statement version is zero")
	ErrBindingVersionZero       = errors.New("attestation: binding version is zero")
	ErrBindingBoundAtEmpty      = errors.New("attestation: binding bound-at instant is required")
	ErrBindingSignerEmpty       = errors.New("attestation: binding signer is required")
	ErrBindingEvidenceEmpty     = errors.New("attestation: binding requires at least one evidence binding")
	ErrBindingContextInvalid    = errors.New("attestation: binding context digest is invalid")
	ErrBindingEvidenceDuplicate = errors.New("attestation: binding lists an evidence id more than once")
)

// Validate reports whether the binding is well-formed.
func (b Binding) Validate() error {
	if b.StatementID == "" {
		return fmt.Errorf("%w", ErrBindingStatementEmpty)
	}
	if b.StatementVersion == 0 {
		return fmt.Errorf("%w", ErrBindingStatementVersion)
	}
	if b.BindingVersion == 0 {
		return fmt.Errorf("%w", ErrBindingVersionZero)
	}
	if err := b.BoundAt.Validate(); err != nil {
		return fmt.Errorf("%w", ErrBindingBoundAtEmpty)
	}
	if err := b.Signer.Validate(); err != nil {
		return fmt.Errorf("%w", ErrBindingSignerEmpty)
	}
	if len(b.EvidenceBindings) == 0 {
		return fmt.Errorf("%w", ErrBindingEvidenceEmpty)
	}
	seenEvidence := make(map[string]struct{}, len(b.EvidenceBindings))
	for i, eb := range b.EvidenceBindings {
		if err := eb.Validate(); err != nil {
			return fmt.Errorf("attestation: evidence binding[%d]: %w", i, err)
		}
		if _, dup := seenEvidence[eb.EvidenceID]; dup {
			return fmt.Errorf("%w: %q", ErrBindingEvidenceDuplicate, eb.EvidenceID)
		}
		seenEvidence[eb.EvidenceID] = struct{}{}
	}
	if err := b.ContextDigest.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrBindingContextInvalid, err)
	}
	return nil
}

// Canonical returns the deterministic byte encoding of the binding.
func (b Binding) Canonical() []byte {
	// Sort evidence bindings by ID for deterministic ordering.
	evidenceBindings := make([]EvidenceBinding, len(b.EvidenceBindings))
	copy(evidenceBindings, b.EvidenceBindings)
	sort.Slice(evidenceBindings, func(i, j int) bool {
		return evidenceBindings[i].EvidenceID < evidenceBindings[j].EvidenceID
	})

	cb := canonicalbytes.New(bindingSchema, 1).
		String("statement_id", b.StatementID).
		Int("statement_version", int64(b.StatementVersion)).
		String("context.subject_as_of", b.ContextDigest.SubjectAsOf).
		String("context.jurisdiction", b.ContextDigest.Jurisdiction).
		String("context.locale", b.ContextDigest.Locale).
		String("context.rendered_text_hash", b.ContextDigest.RenderedTextHash).
		Int("binding_version", int64(b.BindingVersion))

	for i := range evidenceBindings {
		cb = cb.String(fmt.Sprintf("evidence[%d].id", i), evidenceBindings[i].EvidenceID).
			String(fmt.Sprintf("evidence[%d].hash", i), evidenceBindings[i].EvidenceHash)
	}

	cb = cb.Value("bound_at", b.BoundAt).
		String("signer.tenant", string(b.Signer.Tenant)).
		String("signer.kind", string(b.Signer.Kind)).
		String("signer.id", b.Signer.Id)

	raw, err := cb.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical binding encoding.
func (b Binding) Digest() string {
	raw := b.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// VerifyDriftReason describes what aspect of a binding drifted during verification.
type VerifyDriftReason string

const (
	DriftNone              VerifyDriftReason = "NONE"
	DriftSubjectAsOf       VerifyDriftReason = "SUBJECT_AS_OF_CHANGED"
	DriftJurisdiction      VerifyDriftReason = "JURISDICTION_CHANGED"
	DriftLocale            VerifyDriftReason = "LOCALE_CHANGED"
	DriftRenderedText      VerifyDriftReason = "RENDERED_TEXT_CHANGED"
	DriftEvidenceAdded     VerifyDriftReason = "EVIDENCE_ADDED"
	DriftEvidenceRemoved   VerifyDriftReason = "EVIDENCE_REMOVED"
	DriftEvidenceModified  VerifyDriftReason = "EVIDENCE_MODIFIED"
	DriftStatementModified VerifyDriftReason = "STATEMENT_MODIFIED"
)

// VerifyResult reports whether the binding and statement are coherent,
// and if not, what aspect drifted.
type VerifyResult struct {
	Valid  bool              // whether the binding is still valid
	Reason VerifyDriftReason // reason for invalidity, if any
	Detail string            // additional details
}

// Verify checks whether the binding is still valid against current
// context and statement digests. It returns NONE if valid, or names
// the specific component that drifted.
func (b Binding) Verify(stmt AttestationStatement, currentContext ContextDigest, currentEvidenceRefs []EvidenceRef) VerifyResult {
	// Check if the statement itself has changed (version mismatch or digest change).
	if stmt.Version != b.StatementVersion {
		return VerifyResult{
			Valid:  false,
			Reason: DriftStatementModified,
			Detail: fmt.Sprintf("statement version changed from %d to %d", b.StatementVersion, stmt.Version),
		}
	}

	// Check context digests for drift.
	if currentContext.SubjectAsOf != b.ContextDigest.SubjectAsOf {
		return VerifyResult{
			Valid:  false,
			Reason: DriftSubjectAsOf,
			Detail: fmt.Sprintf("subject as-of changed from %q to %q", b.ContextDigest.SubjectAsOf, currentContext.SubjectAsOf),
		}
	}

	if currentContext.Jurisdiction != b.ContextDigest.Jurisdiction {
		return VerifyResult{
			Valid:  false,
			Reason: DriftJurisdiction,
			Detail: fmt.Sprintf("jurisdiction changed from %q to %q", b.ContextDigest.Jurisdiction, currentContext.Jurisdiction),
		}
	}

	if currentContext.Locale != b.ContextDigest.Locale {
		return VerifyResult{
			Valid:  false,
			Reason: DriftLocale,
			Detail: fmt.Sprintf("locale changed from %q to %q", b.ContextDigest.Locale, currentContext.Locale),
		}
	}

	if currentContext.RenderedTextHash != b.ContextDigest.RenderedTextHash {
		return VerifyResult{
			Valid:  false,
			Reason: DriftRenderedText,
			Detail: fmt.Sprintf("rendered text hash changed from %q to %q", b.ContextDigest.RenderedTextHash, currentContext.RenderedTextHash),
		}
	}

	// Check evidence for drift.
	currentEvidenceSet := make(map[string]string)
	for _, ref := range currentEvidenceRefs {
		currentEvidenceSet[ref.ID] = ref.Digest
	}

	boundEvidenceSet := make(map[string]string)
	for _, eb := range b.EvidenceBindings {
		boundEvidenceSet[eb.EvidenceID] = eb.EvidenceHash
	}

	// Check for removed evidence.
	for evidenceID := range boundEvidenceSet {
		if _, ok := currentEvidenceSet[evidenceID]; !ok {
			return VerifyResult{
				Valid:  false,
				Reason: DriftEvidenceRemoved,
				Detail: fmt.Sprintf("evidence %q was removed", evidenceID),
			}
		}
	}

	// Check for added or modified evidence.
	for evidenceID, currentHash := range currentEvidenceSet {
		if boundHash, ok := boundEvidenceSet[evidenceID]; !ok {
			return VerifyResult{
				Valid:  false,
				Reason: DriftEvidenceAdded,
				Detail: fmt.Sprintf("evidence %q was added", evidenceID),
			}
		} else if boundHash != currentHash {
			return VerifyResult{
				Valid:  false,
				Reason: DriftEvidenceModified,
				Detail: fmt.Sprintf("evidence %q digest changed from %q to %q", evidenceID, boundHash, currentHash),
			}
		}
	}

	return VerifyResult{
		Valid:  true,
		Reason: DriftNone,
		Detail: "",
	}
}
