package timeprofile

import "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"

// Version reports this package's contract version (ARCH-GO-009).
func Version() int { return schemaVersion }

// ProfileExplanation is the audit-facing summary of a resolved profile: the
// classification it carries, the template it resolves to (when it resolves
// to one) and the digest that identifies this exact version. It is for
// inspectors and logs, never for programmatic branching.
type ProfileExplanation struct {
	ID               string
	Version          uint64
	TenantRef        values.TenantId
	Category         WorkerCategory
	Capture          CaptureMode
	Exemption        ExemptionStatus
	Destination      Destination
	ResolvedTemplate Template // "" when the profile has no time template
	Digest           string
	Authority        string
}

// Explain reports p's classification, its resolved template and its digest.
// It fails exactly when Validate fails; a profile that has no time template
// (CaptureNone outside contractor/agency) still explains, with
// ResolvedTemplate left empty.
func (p TimeProfile) Explain() (ProfileExplanation, error) {
	if err := p.Validate(); err != nil {
		return ProfileExplanation{}, err
	}
	digest, err := p.Digest()
	if err != nil {
		return ProfileExplanation{}, err
	}
	exp := ProfileExplanation{
		ID: p.ID, Version: p.Version, TenantRef: p.TenantRef,
		Category: p.Category, Capture: p.Capture, Exemption: p.Exemption, Destination: p.Destination,
		Digest:    digest,
		Authority: "definitions data resolved by an eligibility rule; adapters never read profile fields directly",
	}
	if tmpl, tmplErr := TemplateFor(p); tmplErr == nil {
		exp.ResolvedTemplate = tmpl
	}
	return exp, nil
}

// Explain is the package-level form of TimeProfile.Explain.
func Explain(p TimeProfile) (ProfileExplanation, error) { return p.Explain() }
