// OUTCOME-001: link decisions to outcomes without causal overclaim.
//
// RecordOutcomeLink binds one decision to one observed outcome under an
// explicit attribution class. Descriptive evidence is never labelled
// causal: a causal claim requires a trial or quasi-experimental basis,
// and a missing window, population or source authority is refused. A
// correction supersedes an analysis with a new link; the original is
// never rewritten. The function is kernel-pure and cannot rewrite the
// original decision.
package intelligence

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// OutcomeVersion is the rejection version for outcome links.
const OutcomeVersion = "intelligence-outcome/v1"

var (
	// ErrOutcomeRejected is the OUTCOME-001 sentinel. A missing window,
	// population or source authority, or descriptive evidence labelled
	// causal, fails with this error.
	ErrOutcomeRejected = errors.New("OUTCOME_001_REJECTED")
)

// OutcomeRejection is the stable OUTCOME-001 failure shape.
type OutcomeRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *OutcomeRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrOutcomeRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the OUTCOME_001_REJECTED sentinel to errors.Is.
func (r *OutcomeRejection) Unwrap() error { return ErrOutcomeRejected }

func outcomeReject(field, state, reason string) error {
	return &OutcomeRejection{Field: field, State: state, Version: OutcomeVersion, Reason: reason}
}

// AttributionClass is the closed OUTCOME-001 causal-claim vocabulary.
type AttributionClass string

const (
	AttributionDescriptive       AttributionClass = "DESCRIPTIVE"
	AttributionQuasiExperimental AttributionClass = "QUASI_EXPERIMENTAL"
	AttributionCausalTrial       AttributionClass = "CAUSAL_TRIAL"
)

// Valid reports whether the class is declared.
func (a AttributionClass) Valid() bool {
	switch a {
	case AttributionDescriptive, AttributionQuasiExperimental, AttributionCausalTrial:
		return true
	default:
		return false
	}
}

// CausalClaim reports whether the class asserts causation.
func (a AttributionClass) CausalClaim() bool {
	return a == AttributionQuasiExperimental || a == AttributionCausalTrial
}

// OutcomeWindow bounds the observation period.
type OutcomeWindow struct {
	Start time.Time
	End   time.Time
}

// OutcomeLinkInput is one decision-to-outcome link request.
type OutcomeLinkInput struct {
	Tenant            string
	DecisionRef       string
	OutcomeRef        string
	Definition        string
	DefinitionVersion string
	Window            OutcomeWindow
	PopulationRef     string
	SourceAuthority   string
	Attribution       AttributionClass
	BasisRef          string
	Confounders       []string
	Censoring         string
	Watermark         string
	Confidence        string
	Limitations       string
	Supersedes        string
}

// OutcomeLink is the sealed link record.
type OutcomeLink struct {
	Tenant            string
	DecisionRef       string
	OutcomeRef        string
	Definition        string
	DefinitionVersion string
	Window            OutcomeWindow
	PopulationRef     string
	SourceAuthority   string
	Attribution       AttributionClass
	BasisRef          string
	Confounders       []string
	Censoring         string
	Watermark         string
	Confidence        string
	Limitations       string
	Supersedes        string
	LinkedAt          time.Time
	Digest            string
}

func (l OutcomeLink) computedDigest() string {
	confounders := append([]string(nil), l.Confounders...)
	for i := range confounders {
		confounders[i] = strings.TrimSpace(confounders[i])
	}
	w := canonicalbytes.New("hcmnext.domains.intelligence.OutcomeLink", 1).
		String("tenant", l.Tenant).
		String("decision", l.DecisionRef).
		String("outcome", l.OutcomeRef).
		String("definition", l.Definition).
		String("definition_version", l.DefinitionVersion).
		String("window_start", l.Window.Start.UTC().Format(time.RFC3339)).
		String("window_end", l.Window.End.UTC().Format(time.RFC3339)).
		String("population", l.PopulationRef).
		String("authority", l.SourceAuthority).
		String("attribution", string(l.Attribution)).
		String("basis", l.BasisRef).
		SortedStrings("confounders", confounders).
		String("censoring", l.Censoring).
		String("watermark", l.Watermark).
		String("confidence", l.Confidence).
		String("limitations", l.Limitations).
		String("supersedes", l.Supersedes)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// RecordOutcomeLink seals one decision-to-outcome link at now.
func RecordOutcomeLink(in OutcomeLinkInput, now time.Time) (OutcomeLink, error) {
	if strings.TrimSpace(in.Tenant) == "" {
		return OutcomeLink{}, outcomeReject("outcome.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.DecisionRef) == "" {
		return OutcomeLink{}, outcomeReject("outcome.decision_ref", "MISSING", "decision ref is required")
	}
	if strings.TrimSpace(in.OutcomeRef) == "" {
		return OutcomeLink{}, outcomeReject("outcome.outcome_ref", "MISSING", "outcome ref is required")
	}
	if strings.TrimSpace(in.Definition) == "" || strings.TrimSpace(in.DefinitionVersion) == "" {
		return OutcomeLink{}, outcomeReject("outcome.definition", "MISSING", "outcome definition and version are required")
	}
	if in.Window.Start.IsZero() || in.Window.End.IsZero() || !in.Window.End.After(in.Window.Start) {
		return OutcomeLink{}, outcomeReject("outcome.window", "MISSING", "observation window must be a non-empty interval")
	}
	if strings.TrimSpace(in.PopulationRef) == "" {
		return OutcomeLink{}, outcomeReject("outcome.population_ref", "MISSING", "population ref is required")
	}
	if strings.TrimSpace(in.SourceAuthority) == "" {
		return OutcomeLink{}, outcomeReject("outcome.source_authority", "MISSING", "source authority is required")
	}
	if !in.Attribution.Valid() {
		return OutcomeLink{}, outcomeReject("outcome.attribution", "UNDECLARED", fmt.Sprintf("attribution %q is not declared", in.Attribution))
	}
	if in.Attribution.CausalClaim() {
		if strings.TrimSpace(in.BasisRef) == "" {
			return OutcomeLink{}, outcomeReject("outcome.basis_ref", "MISSING", "causal claims name their trial or quasi-experimental basis")
		}
		if len(in.Confounders) == 0 {
			return OutcomeLink{}, outcomeReject("outcome.confounders", "MISSING", "causal claims enumerate confounders")
		}
		if strings.TrimSpace(in.Limitations) == "" {
			return OutcomeLink{}, outcomeReject("outcome.limitations", "MISSING", "causal claims state limitations")
		}
	}
	if strings.TrimSpace(in.Watermark) == "" {
		return OutcomeLink{}, outcomeReject("outcome.watermark", "MISSING", "watermark is required")
	}
	if strings.TrimSpace(in.Confidence) == "" {
		return OutcomeLink{}, outcomeReject("outcome.confidence", "MISSING", "confidence is required")
	}
	if now.IsZero() {
		return OutcomeLink{}, outcomeReject("outcome.linked_at", "MISSING", "link instant is required")
	}
	link := OutcomeLink{
		Tenant: in.Tenant, DecisionRef: in.DecisionRef, OutcomeRef: in.OutcomeRef,
		Definition: in.Definition, DefinitionVersion: in.DefinitionVersion,
		Window: in.Window, PopulationRef: in.PopulationRef, SourceAuthority: in.SourceAuthority,
		Attribution: in.Attribution, BasisRef: in.BasisRef,
		Confounders: append([]string(nil), in.Confounders...),
		Censoring:   in.Censoring, Watermark: in.Watermark, Confidence: in.Confidence,
		Limitations: in.Limitations, Supersedes: in.Supersedes, LinkedAt: now.UTC(),
	}
	link.Digest = link.computedDigest()
	return link, nil
}
