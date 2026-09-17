// State comprehensive-privacy-law release gate (PRIV-010).
//
// There is no global consent/rights toggle: California (CCPA/CPRA),
// Colorado (CPA), Virginia (VCDPA), Texas (TDPSA) and other enacted state
// laws differ in employee-data rights, exemptions, sensitive-data rules,
// processor duties and opt-out/consent signals, and the gate resolves each
// state from a signed, versioned jurisdiction roster with effective dates.
// A new or amended state law is honored only after a primary-source review
// covers its exact version: unreviewed versions fail closed rather than
// silently inheriting another state's answers.
package privacy

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
	// ErrStateLawRefused reports a per-state resolution that cannot be
	// honored: unknown state, unsigned roster, unreviewed version or a law
	// not yet effective at the evaluation date.
	ErrStateLawRefused = errors.New("privacy: state privacy law not honored")
	// ErrStateLawConflict reports an amended roster version that drops or
	// contradicts a reviewed predecessor without a fresh review.
	ErrStateLawConflict = errors.New("privacy: state roster version conflict")
)

// StateCode is the closed enacted-law vocabulary of the release gate.
type StateCode string

const (
	StateCA StateCode = "CA"
	StateCO StateCode = "CO"
	StateVA StateCode = "VA"
	StateTX StateCode = "TX"
)

// ValidStateLaw reports whether s is a gated state.
func ValidStateLaw(s StateCode) bool {
	switch s {
	case StateCA, StateCO, StateVA, StateTX:
		return true
	}
	return false
}

// StateLaw is one reviewed state-law version: the exact rights,
// exemptions, sensitive-data rules, processor duties and opt-out/consent
// signals that differ by state, with the date the version takes effect.
type StateLaw struct {
	State           StateCode
	LawID           string
	Version         string
	Effective       time.Time
	Rights          []string
	Exemptions      []string
	SensitiveRules  []string
	ProcessorDuties []string
	OptOutSignal    string
	ConsentSignal   string
}

func (l StateLaw) validate() error {
	if !ValidStateLaw(l.State) {
		return fmt.Errorf("%w: state %q is not gated", ErrStateLawRefused, l.State)
	}
	for field, value := range map[string]string{
		"law_id": l.LawID, "version": l.Version,
		"opt_out_signal": l.OptOutSignal, "consent_signal": l.ConsentSignal,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s %s/%s is incomplete", ErrStateLawRefused, field, l.State, l.Version)
		}
	}
	if l.Effective.IsZero() {
		return fmt.Errorf("%w: %s/%s has no effective date", ErrStateLawRefused, l.State, l.Version)
	}
	if len(l.Rights) == 0 || len(l.ProcessorDuties) == 0 {
		return fmt.Errorf("%w: %s/%s names no rights or processor duties", ErrStateLawRefused, l.State, l.Version)
	}
	return nil
}

// PrimaryReview is the primary-source review that admits one roster
// version: the reviewer consulted the statute/regulation text itself, not
// a summary, before the version's effective date is honored.
type PrimaryReview struct {
	ReviewedAt   time.Time
	Reviewer     string
	SourceRef    string
	CoversRoster string
}

func (r PrimaryReview) validate(rosterVersion string) error {
	if strings.TrimSpace(r.Reviewer) == "" || strings.TrimSpace(r.SourceRef) == "" {
		return fmt.Errorf("%w: primary-source review names no reviewer or source", ErrStateLawRefused)
	}
	if r.ReviewedAt.IsZero() {
		return fmt.Errorf("%w: primary-source review has no date", ErrStateLawRefused)
	}
	if r.CoversRoster != rosterVersion {
		return fmt.Errorf("%w: review covers %q, not roster %q", ErrStateLawRefused, r.CoversRoster, rosterVersion)
	}
	return nil
}

// Roster is the signed, versioned jurisdiction roster that drives the gate.
type Roster struct {
	Version   string
	Laws      []StateLaw
	Signature string
	SignedBy  string
	SignedAt  time.Time
	Review    PrimaryReview
}

func rosterDigest(version string, laws []StateLaw) string {
	ordered := append([]StateLaw(nil), laws...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].State != ordered[j].State {
			return ordered[i].State < ordered[j].State
		}
		return ordered[i].Version < ordered[j].Version
	})
	parts := make([]string, 0, len(ordered)+1)
	parts = append(parts, version)
	for _, l := range ordered {
		rights := append([]string(nil), l.Rights...)
		sort.Strings(rights)
		parts = append(parts, strings.Join([]string{
			string(l.State), l.LawID, l.Version,
			l.Effective.UTC().Format(time.RFC3339Nano),
			strings.Join(rights, ","), strings.Join(l.Exemptions, ","),
			strings.Join(l.SensitiveRules, ","), strings.Join(l.ProcessorDuties, ","),
			l.OptOutSignal, l.ConsentSignal,
		}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Seal validates and signs a roster: every law version must validate, no
// state may carry two versions, and the primary-source review must cover
// the sealed version.
func Seal(version string, laws []StateLaw, review PrimaryReview, signer string, at time.Time) (Roster, error) {
	if strings.TrimSpace(version) == "" || strings.TrimSpace(signer) == "" || at.IsZero() {
		return Roster{}, fmt.Errorf("%w: roster version, signer and signing time are required", ErrStateLawRefused)
	}
	seen := map[StateCode]bool{}
	for _, l := range laws {
		if err := l.validate(); err != nil {
			return Roster{}, err
		}
		if seen[l.State] {
			return Roster{}, fmt.Errorf("%w: state %s carries two versions", ErrStateLawConflict, l.State)
		}
		seen[l.State] = true
	}
	if err := review.validate(version); err != nil {
		return Roster{}, err
	}
	digest := rosterDigest(version, laws)
	return Roster{
		Version: version, Laws: append([]StateLaw(nil), laws...),
		Signature: digest + ":" + signer, SignedBy: signer, SignedAt: at, Review: review,
	}, nil
}

// Resolution is the per-state release answer.
type Resolution struct {
	State           StateCode
	LawID           string
	Version         string
	RosterVersion   string
	Rights          []string
	Exemptions      []string
	SensitiveRules  []string
	ProcessorDuties []string
	OptOutSignal    string
	ConsentSignal   string
	Digest          string
}

// Resolve answers one state at one evaluation date from a sealed roster.
// A law whose effective date is after the evaluation date is not honored:
// upcoming obligations never leak into current releases.
func (r Roster) Resolve(state StateCode, asOf time.Time) (Resolution, error) {
	if strings.TrimSpace(r.Signature) == "" || strings.TrimSpace(r.SignedBy) == "" {
		return Resolution{}, fmt.Errorf("%w: roster %q is unsigned", ErrStateLawRefused, r.Version)
	}
	// The signature is tamper-evident: any post-seal edit to the law set
	// breaks the digest and fails closed here, not at release time.
	if want := rosterDigest(r.Version, r.Laws); !strings.HasPrefix(r.Signature, want+":"+r.SignedBy) {
		return Resolution{}, fmt.Errorf("%w: roster %q signature does not match its laws", ErrStateLawRefused, r.Version)
	}
	if err := r.Review.validate(r.Version); err != nil {
		return Resolution{}, err
	}
	if asOf.IsZero() {
		return Resolution{}, fmt.Errorf("%w: evaluation date is required", ErrStateLawRefused)
	}
	for _, l := range r.Laws {
		if l.State != state {
			continue
		}
		if asOf.Before(l.Effective) {
			return Resolution{}, fmt.Errorf("%w: %s/%s is not effective until %s", ErrStateLawRefused, state, l.Version, l.Effective.Format("2006-01-02"))
		}
		sum := sha256.Sum256([]byte(strings.Join([]string{r.Version, r.Signature, string(state), l.Version, asOf.UTC().Format(time.RFC3339Nano)}, "\x00")))
		return Resolution{
			State: state, LawID: l.LawID, Version: l.Version, RosterVersion: r.Version,
			Rights:          append([]string(nil), l.Rights...),
			Exemptions:      append([]string(nil), l.Exemptions...),
			SensitiveRules:  append([]string(nil), l.SensitiveRules...),
			ProcessorDuties: append([]string(nil), l.ProcessorDuties...),
			OptOutSignal:    l.OptOutSignal, ConsentSignal: l.ConsentSignal,
			Digest: "sha256:" + hex.EncodeToString(sum[:]),
		}, nil
	}
	return Resolution{}, fmt.Errorf("%w: state %q has no reviewed law version", ErrStateLawRefused, state)
}
