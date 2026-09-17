// CBA-003: generate representation/grievance obligations and analyze
// agreement changes.
//
// An agreement change never silently alters approved schedules, pay, leave
// or discipline: every changed clause yields notice plus reevaluation
// intents, consultation where representation must be heard, and
// grievance/arbitration windows where contest rights attach. The affected
// population is frozen — id, freeze time, digest and member count travel
// with the impact, and any substitution changes the impact digest. Missing
// deadlines or a waived (empty) representation reference refuse the whole
// analysis. Prior composition stays historically explainable through a
// recomputable prior digest. This package judges the change; it emits
// intents, never effects.
package cba

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidAgreementChange = errors.New("cba: invalid agreement change")
	ErrRepresentationRequired = errors.New("cba: representation is required and cannot be waived")
	ErrChangeDeadlineRequired = errors.New("cba: agreement change requires grievance and arbitration deadlines")
)

// ChangeIntentKind is the closed intent vocabulary one agreement change
// creates.
type ChangeIntentKind string

// Change intent kinds.
const (
	IntentNotice            ChangeIntentKind = "NOTICE"
	IntentReevaluation      ChangeIntentKind = "REEVALUATION"
	IntentConsultation      ChangeIntentKind = "CONSULTATION"
	IntentGrievanceWindow   ChangeIntentKind = "GRIEVANCE_WINDOW"
	IntentArbitrationWindow ChangeIntentKind = "ARBITRATION_WINDOW"
)

func intentRank(k ChangeIntentKind) int {
	switch k {
	case IntentArbitrationWindow:
		return 0
	case IntentGrievanceWindow:
		return 1
	case IntentNotice:
		return 2
	case IntentReevaluation:
		return 3
	case IntentConsultation:
		return 4
	}
	return 5
}

// ClauseChange is one clause revision inside an agreement change.
type ClauseChange struct {
	ClauseRef          string
	Kind               ConstraintKind
	FromValue, ToValue string
}

// PopulationPin freezes the represented population an agreement change was
// analyzed against.
type PopulationPin struct {
	ID          string
	FrozenAt    time.Time
	Digest      string
	MemberCount int
}

// AgreementChange is the governed input: one agreement revision step with
// its clause diff, frozen population, contest deadlines, representation,
// calendar and evidence.
type AgreementChange struct {
	AgreementID, FromRevision, ToRevision        string
	ChangedClauses                               []ClauseChange
	AffectedPopulation                           PopulationPin
	GrievanceDeadlineRef, ArbitrationDeadlineRef string
	RepresentationRef, CalendarRef               string
	EffectiveAt                                  time.Time
	EvidenceDigest                               string
}

// ChangeIntent is one typed obligation a change creates, bound to its
// clause, calendar and evidence.
type ChangeIntent struct {
	Kind        ChangeIntentKind
	ClauseRef   string
	CalendarRef string
	EvidenceRef string
	DueRef      string
}

// AgreementImpact is the bounded analysis of one agreement change.
type AgreementImpact struct {
	AgreementID, FromRevision, ToRevision string
	AffectedKinds                         []ConstraintKind
	AffectedPopulation                    PopulationPin
	Intents                               []ChangeIntent
	PriorOutcome                          CompositionOutcome
	PriorDigest                           string
	RepresentationRef                     string
	Digest                                string
}

// Validate recomputes the impact digest and refuses tampered impacts.
func (m AgreementImpact) Validate() error {
	if m.Digest == "" {
		return fmt.Errorf("%w: impact digest is missing", ErrInvalidAgreementChange)
	}
	if m.Digest != m.computeDigest() {
		return fmt.Errorf("%w: impact digest mismatch", ErrInvalidAgreementChange)
	}
	return nil
}

func (m AgreementImpact) computeDigest() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%s", m.AgreementID, m.FromRevision, m.ToRevision, m.AffectedPopulation.ID, m.AffectedPopulation.Digest, m.PriorDigest, m.RepresentationRef)
	for _, k := range m.AffectedKinds {
		fmt.Fprintf(h, "|kind=%s", k)
	}
	for _, in := range m.Intents {
		fmt.Fprintf(h, "|%s=%s@%s#%s", in.ClauseRef, in.Kind, in.CalendarRef, in.DueRef)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// AffectedKinds re-derives the affected constraint kinds from the clause
// list alone: the independent path the conformance test checks the impact
// against.
func AffectedKinds(change AgreementChange) []ConstraintKind {
	seen := map[ConstraintKind]bool{}
	out := []ConstraintKind{}
	for _, c := range change.ChangedClauses {
		if !seen[c.Kind] {
			seen[c.Kind] = true
			out = append(out, c.Kind)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// PriorDigestOf recomputes the historical digest of a prior composition
// result: obligations, prohibitions, calculations, outcome and reason.
func PriorDigestOf(prior CompositionResult) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s", prior.Outcome, prior.Reason)
	obligations := append([]CompositionObligation(nil), prior.Obligations...)
	sort.Slice(obligations, func(i, j int) bool { return obligations[i].ID < obligations[j].ID })
	for _, o := range obligations {
		fmt.Fprintf(h, "|obligation=%s:%s:%s:%s", o.ID, o.Kind, o.ClauseRef, o.ReleaseRef)
	}
	prohibitions := append([]CompositionObligation(nil), prior.Prohibitions...)
	sort.Slice(prohibitions, func(i, j int) bool { return prohibitions[i].ID < prohibitions[j].ID })
	for _, o := range prohibitions {
		fmt.Fprintf(h, "|prohibition=%s:%s:%s:%s", o.ID, o.Kind, o.ClauseRef, o.ReleaseRef)
	}
	calculations := append([]CompositionCalculation(nil), prior.Calculations...)
	sort.Slice(calculations, func(i, j int) bool { return calculations[i].Kind < calculations[j].Kind })
	for _, c := range calculations {
		fmt.Fprintf(h, "|calculation=%s:%d:%d", c.Kind, c.Minimum, c.Maximum)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// AnalyzeAgreementChange validates one agreement change and generates its
// typed intents. It is pure and deterministic: identical inputs yield
// byte-identical impacts.
func AnalyzeAgreementChange(change AgreementChange, prior CompositionResult) (AgreementImpact, error) {
	if strings.TrimSpace(change.AgreementID) == "" {
		return AgreementImpact{}, fmt.Errorf("%w: agreement id is required", ErrInvalidAgreementChange)
	}
	if strings.TrimSpace(change.FromRevision) == "" || strings.TrimSpace(change.ToRevision) == "" || change.FromRevision == change.ToRevision {
		return AgreementImpact{}, fmt.Errorf("%w: a distinct revision step is required", ErrInvalidAgreementChange)
	}
	if len(change.ChangedClauses) == 0 {
		return AgreementImpact{}, fmt.Errorf("%w: at least one changed clause is required", ErrInvalidAgreementChange)
	}
	seen := map[string]bool{}
	for i, c := range change.ChangedClauses {
		if strings.TrimSpace(c.ClauseRef) == "" {
			return AgreementImpact{}, fmt.Errorf("%w: clause %d ref is required", ErrInvalidAgreementChange, i)
		}
		switch c.Kind {
		case ConstraintWage, ConstraintSchedule, ConstraintLeave, ConstraintSeniority, ConstraintDiscipline:
		default:
			return AgreementImpact{}, fmt.Errorf("%w: clause %q kind %q", ErrInvalidAgreementChange, c.ClauseRef, c.Kind)
		}
		if seen[c.ClauseRef] {
			return AgreementImpact{}, fmt.Errorf("%w: duplicate clause %q", ErrInvalidAgreementChange, c.ClauseRef)
		}
		seen[c.ClauseRef] = true
	}
	pop := change.AffectedPopulation
	if strings.TrimSpace(pop.ID) == "" || strings.TrimSpace(pop.Digest) == "" || pop.MemberCount <= 0 {
		return AgreementImpact{}, fmt.Errorf("%w: a frozen non-empty affected population is required", ErrInvalidAgreementChange)
	}
	if pop.FrozenAt.IsZero() || change.EffectiveAt.IsZero() || pop.FrozenAt.After(change.EffectiveAt) {
		return AgreementImpact{}, fmt.Errorf("%w: the population freeze must precede effectiveness", ErrInvalidAgreementChange)
	}
	if strings.TrimSpace(change.RepresentationRef) == "" {
		return AgreementImpact{}, fmt.Errorf("%w: representation cannot be waived", ErrRepresentationRequired)
	}
	if strings.TrimSpace(change.GrievanceDeadlineRef) == "" || strings.TrimSpace(change.ArbitrationDeadlineRef) == "" {
		return AgreementImpact{}, fmt.Errorf("%w: grievance and arbitration deadlines are required", ErrChangeDeadlineRequired)
	}
	if strings.TrimSpace(change.CalendarRef) == "" || strings.TrimSpace(change.EvidenceDigest) == "" {
		return AgreementImpact{}, fmt.Errorf("%w: calendar and evidence are required", ErrInvalidAgreementChange)
	}
	impact := AgreementImpact{
		AgreementID: change.AgreementID, FromRevision: change.FromRevision, ToRevision: change.ToRevision,
		AffectedKinds: AffectedKinds(change), AffectedPopulation: pop,
		PriorOutcome: prior.Outcome, PriorDigest: PriorDigestOf(prior),
		RepresentationRef: change.RepresentationRef,
	}
	for _, c := range change.ChangedClauses {
		impact.Intents = append(impact.Intents, ChangeIntent{Kind: IntentNotice, ClauseRef: c.ClauseRef, CalendarRef: change.CalendarRef, EvidenceRef: change.EvidenceDigest})
		impact.Intents = append(impact.Intents, ChangeIntent{Kind: IntentReevaluation, ClauseRef: c.ClauseRef, CalendarRef: change.CalendarRef, EvidenceRef: change.EvidenceDigest})
		switch c.Kind {
		case ConstraintWage, ConstraintSchedule, ConstraintLeave:
			impact.Intents = append(impact.Intents, ChangeIntent{Kind: IntentConsultation, ClauseRef: c.ClauseRef, CalendarRef: change.CalendarRef, EvidenceRef: change.EvidenceDigest, DueRef: change.EffectiveAt.UTC().Format("2006-01-02")})
		case ConstraintSeniority, ConstraintDiscipline:
			impact.Intents = append(impact.Intents, ChangeIntent{Kind: IntentGrievanceWindow, ClauseRef: c.ClauseRef, CalendarRef: change.CalendarRef, EvidenceRef: change.EvidenceDigest, DueRef: change.GrievanceDeadlineRef})
		}
		if c.Kind == ConstraintDiscipline {
			impact.Intents = append(impact.Intents, ChangeIntent{Kind: IntentArbitrationWindow, ClauseRef: c.ClauseRef, CalendarRef: change.CalendarRef, EvidenceRef: change.EvidenceDigest, DueRef: change.ArbitrationDeadlineRef})
		}
	}
	sort.SliceStable(impact.Intents, func(i, j int) bool {
		if impact.Intents[i].ClauseRef != impact.Intents[j].ClauseRef {
			return impact.Intents[i].ClauseRef < impact.Intents[j].ClauseRef
		}
		return intentRank(impact.Intents[i].Kind) < intentRank(impact.Intents[j].Kind)
	})
	impact.Digest = impact.computeDigest()
	return impact, nil
}
