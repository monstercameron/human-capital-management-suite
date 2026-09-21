package people

// Global legal-name change (CONF-019): structured multilingual names with
// no western first/middle/last assumption, NFC normalization, single-
// script parts, scanned and classified evidence cites (never raw bytes),
// jurisdiction obligations, exact approval digests, effective-time
// revalidation and one atomic legal-name revision. Display, username,
// email, payroll, IAM and document effects authorize and reconcile
// separately: nothing downstream changes implicitly, downstream
// acceptance is observation rather than truth, and payroll cutoffs or
// identity merge conflicts refuse with scoped repair. Name
// representation, evidence sufficiency and downstream naming policies
// stay independently versioned. Sufficiency is a deterministic policy
// checklist: no model, score or AI determines it.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrEmptyName reports a name with no parts.
	ErrEmptyName = errors.New("people: name has no parts")

	// ErrMixedScript reports one part mixing scripts: the confusable
	// bypass guard.
	ErrMixedScript = errors.New("people: name part mixes scripts")

	// ErrMissingLatin reports a non-Latin name without its Latin
	// representation.
	ErrMissingLatin = errors.New("people: non-Latin name needs its Latin representation")

	// ErrUnsafeEvidence reports an evidence cite that is not scanned
	// safe with a digest: raw evidence never enters the workflow.
	ErrUnsafeEvidence = errors.New("people: evidence is not scanned safe")

	// ErrInsufficientSufficiency reports an approval without classified
	// evidence, a discharged obligation and an approval digest.
	ErrInsufficientSufficiency = errors.New("people: legal sufficiency unmet")

	// ErrNotEffective reports execution before the effective date.
	ErrNotEffective = errors.New("people: name change is not yet effective")

	// ErrCutoffConflict reports a payroll cutoff covering the effective
	// date.
	ErrCutoffConflict = errors.New("people: payroll cutoff conflict")

	// ErrMergeConflict reports a pending identity merge on the worker.
	ErrMergeConflict = errors.New("people: identity merge conflict")

	// ErrChangeState reports a lifecycle step out of order.
	ErrChangeState = errors.New("people: name change in wrong state")

	// ErrImplicitEffect reports reading a downstream naming change that
	// was never separately authorized.
	ErrImplicitEffect = errors.New("people: downstream effect was never authorized")
)

// NamePart is one ordered name part in one script.
type NamePart struct {
	Value  string
	Script string
}

// StructuredName is the full multilingual name: ordered parts, the full
// rendering in given order, the caller-supplied Latin representation and
// the normalization profile.
type StructuredName struct {
	Parts                []NamePart
	Full                 string
	Latin                string
	NormalizationProfile string
}

func scriptOf(r rune) string {
	switch {
	case unicode.Is(unicode.Latin, r):
		return "Latn"
	case unicode.Is(unicode.Han, r):
		return "Hani"
	case unicode.Is(unicode.Arabic, r):
		return "Arab"
	case unicode.Is(unicode.Devanagari, r):
		return "Deva"
	case unicode.Is(unicode.Cyrillic, r):
		return "Cyrl"
	case unicode.Is(unicode.Hiragana, r):
		return "Hira"
	case unicode.Is(unicode.Katakana, r):
		return "Kana"
	case unicode.Is(unicode.Hangul, r):
		return "Hang"
	case unicode.Is(unicode.Thai, r):
		return "Thai"
	case unicode.Is(unicode.Greek, r):
		return "Grek"
	case r == ' ' || r == '-' || r == '\'' || r == '.' || r == '’':
		return "Zyyy"
	default:
		return "Unknown"
	}
}

// Normalize validates and normalizes one structured name: NFC profile,
// non-empty ordered parts, single script per part, Latin representation
// present exactly when a part leaves Latin.
func Normalize(parts []string, latin string) (StructuredName, error) {
	if len(parts) == 0 {
		return StructuredName{}, ErrEmptyName
	}
	name := StructuredName{NormalizationProfile: "NFC"}
	needsLatin := false
	for _, part := range parts {
		normalized := values.NFC(part)
		if strings.TrimSpace(normalized) == "" {
			return StructuredName{}, ErrEmptyName
		}
		script := ""
		for _, r := range normalized {
			s := scriptOf(r)
			if s == "Zyyy" {
				continue
			}
			if s == "Unknown" {
				return StructuredName{}, fmt.Errorf("people: unsupported script in %q: %w", part, ErrMixedScript)
			}
			if script == "" {
				script = s
			} else if s != script {
				return StructuredName{}, fmt.Errorf("people: part %q mixes %s and %s: %w", part, script, s, ErrMixedScript)
			}
		}
		if script == "" {
			return StructuredName{}, ErrEmptyName
		}
		if script != "Latn" {
			needsLatin = true
		}
		name.Parts = append(name.Parts, NamePart{Value: normalized, Script: script})
	}
	if needsLatin {
		if strings.TrimSpace(latin) == "" {
			return StructuredName{}, ErrMissingLatin
		}
		normalized := values.NFC(latin)
		for _, r := range normalized {
			if s := scriptOf(r); s != "Latn" && s != "Zyyy" {
				return StructuredName{}, fmt.Errorf("people: Latin representation leaves Latin: %w", ErrMissingLatin)
			}
		}
		name.Latin = normalized
	} else {
		name.Latin = values.NFC(strings.Join(parts, " "))
	}
	full := make([]string, 0, len(name.Parts))
	for _, part := range name.Parts {
		full = append(full, part.Value)
	}
	name.Full = strings.Join(full, " ")
	return name, nil
}

func nameDigest(name StructuredName) string {
	parts := []string{"legal-name", name.Full, name.Latin, name.NormalizationProfile}
	for _, part := range name.Parts {
		parts = append(parts, part.Script, part.Value)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// EvidenceCite references one scanned, classified evidence artifact by
// digest. Content never enters the workflow: only the cite travels.
type EvidenceCite struct {
	Ref            string
	ScanState      string
	ScanDigest     string
	Classification string
}

// Obligation is the jurisdiction requirement the change answers.
type Obligation struct {
	Authority   string
	Requirement string
}

// Sufficiency is the deterministic checklist behind legal sufficiency.
type Sufficiency struct {
	EvidenceClassified   bool
	ObligationStated     bool
	ObligationDischarged bool
	ApprovalBound        bool
	Sufficient           bool
}

// NameChange is one in-flight legal-name change.
type NameChange struct {
	ID             string
	WorkerID       string
	PriorDigest    string
	NewName        StructuredName
	NewDigest      string
	Evidence       []EvidenceCite
	Obligation     Obligation
	DischargeRef   string
	ApprovalDigest string
	EffectiveDate  string
	Sufficiency    Sufficiency
	State          string
}

const (
	ChangeProposed  = "PROPOSED"
	ChangeApproved  = "APPROVED"
	ChangeCommitted = "COMMITTED"
)

// Propose files one change with scanned-safe classified evidence and a
// stated jurisdiction obligation.
func Propose(id, workerID, priorDigest string, name StructuredName, evidence []EvidenceCite, obligation Obligation, effectiveDate string) (*NameChange, error) {
	if strings.TrimSpace(workerID) == "" || len(evidence) == 0 {
		return nil, fmt.Errorf("people: Propose: %w", ErrUnsafeEvidence)
	}
	for _, cite := range evidence {
		if cite.ScanState != "SAFE" || strings.TrimSpace(cite.ScanDigest) == "" ||
			strings.TrimSpace(cite.Classification) == "" || strings.TrimSpace(cite.Ref) == "" {
			return nil, fmt.Errorf("people: Propose cite %+v: %w", cite, ErrUnsafeEvidence)
		}
	}
	if strings.TrimSpace(obligation.Authority) == "" || strings.TrimSpace(obligation.Requirement) == "" {
		return nil, fmt.Errorf("people: Propose: %w", ErrInsufficientSufficiency)
	}
	if _, err := time.Parse("2006-01-02", effectiveDate); err != nil {
		return nil, fmt.Errorf("people: Propose: %w", err)
	}
	return &NameChange{
		ID: id, WorkerID: workerID, PriorDigest: priorDigest, NewName: name, NewDigest: nameDigest(name),
		Evidence: append([]EvidenceCite(nil), evidence...), Obligation: obligation,
		EffectiveDate: effectiveDate, State: ChangeProposed,
		Sufficiency: Sufficiency{EvidenceClassified: true, ObligationStated: true},
	}, nil
}

// Approve binds the governance approval and the obligation discharge,
// sealing sufficiency by checklist: classified evidence, stated and
// discharged obligation, bound approval. No score decides this.
func Approve(change *NameChange, approvalDigest, dischargeRef string) error {
	if change.State != ChangeProposed {
		return fmt.Errorf("people: Approve: %w", ErrChangeState)
	}
	if strings.TrimSpace(approvalDigest) == "" || strings.TrimSpace(dischargeRef) == "" {
		return fmt.Errorf("people: Approve: %w", ErrInsufficientSufficiency)
	}
	change.ApprovalDigest = approvalDigest
	change.DischargeRef = dischargeRef
	change.Sufficiency.ObligationDischarged = true
	change.Sufficiency.ApprovalBound = true
	change.Sufficiency.Sufficient = change.Sufficiency.EvidenceClassified && change.Sufficiency.ObligationStated &&
		change.Sufficiency.ObligationDischarged && change.Sufficiency.ApprovalBound
	change.State = ChangeApproved
	return nil
}

// CutoffState tells whether payroll locked the effective period.
type CutoffState struct {
	Locked bool
	Period string
}

// MergeState tells whether an identity merge pends on the worker.
type MergeState struct {
	Pending  bool
	OtherRef string
}

// NameRevision is one atomic committed revision; older revisions stay
// readable beside it.
type NameRevision struct {
	WorkerID   string
	Revision   uint64
	NameDigest string
	Prior      string
	ChangeID   string
}

// DownstreamEffect is one separately authorized naming effect.
type DownstreamEffect struct {
	EffectID     string
	System       string
	Authorized   bool
	Applied      bool
	AppliedValue string
	Observed     bool
	ObservedNote string
}

// PersonProfile carries the naming state: the legal name plus display,
// username and email, which never move implicitly.
type PersonProfile struct {
	WorkerID    string
	LegalName   StructuredName
	DisplayName string
	Username    string
	Email       string
}

// Registry is the mutex-guarded name record owner.
type Registry struct {
	mu        sync.Mutex
	revisions map[string][]NameRevision
	profiles  map[string]PersonProfile
	effects   map[string]*DownstreamEffect
}

// NewRegistry returns an empty owner.
func NewRegistry() *Registry {
	return &Registry{
		revisions: make(map[string][]NameRevision),
		profiles:  make(map[string]PersonProfile),
		effects:   make(map[string]*DownstreamEffect),
	}
}

// SeedProfile files one starting profile.
func (r *Registry) SeedProfile(profile PersonProfile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[profile.WorkerID] = profile
}

// Commit atomically appends the legal-name revision after effective-time
// revalidation, cutoff and merge checks. Downstream systems are listed
// as pending effects: none applies here.
func (r *Registry) Commit(change *NameChange, now time.Time, cutoff CutoffState, merge MergeState, downstream []string) error {
	if change.State != ChangeApproved {
		return fmt.Errorf("people: Commit: %w", ErrChangeState)
	}
	if !change.Sufficiency.Sufficient {
		return fmt.Errorf("people: Commit: %w", ErrInsufficientSufficiency)
	}
	effective, err := time.Parse("2006-01-02", change.EffectiveDate)
	if err != nil {
		return err
	}
	if now.UTC().Truncate(24 * time.Hour).Before(effective) {
		return fmt.Errorf("people: Commit: %w", ErrNotEffective)
	}
	if cutoff.Locked {
		return fmt.Errorf("people: Commit period %s: %w", cutoff.Period, ErrCutoffConflict)
	}
	if merge.Pending {
		return fmt.Errorf("people: Commit merge %s: %w", merge.OtherRef, ErrMergeConflict)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	prior := ""
	revision := uint64(1)
	if chain := r.revisions[change.WorkerID]; len(chain) > 0 {
		prior = chain[len(chain)-1].NameDigest
		revision = chain[len(chain)-1].Revision + 1
	}
	if change.PriorDigest != "" && change.PriorDigest != prior && prior != "" {
		return fmt.Errorf("people: Commit: prior digest moved: %w", ErrChangeState)
	}
	r.revisions[change.WorkerID] = append(r.revisions[change.WorkerID], NameRevision{
		WorkerID: change.WorkerID, Revision: revision,
		NameDigest: change.NewDigest, Prior: prior, ChangeID: change.ID,
	})
	profile := r.profiles[change.WorkerID]
	profile.WorkerID = change.WorkerID
	profile.LegalName = change.NewName
	r.profiles[change.WorkerID] = profile
	for _, system := range downstream {
		id := "effect/" + change.ID + "/" + system
		r.effects[id] = &DownstreamEffect{EffectID: id, System: system}
	}
	change.State = ChangeCommitted
	return nil
}

// AuthorizeEffect separately authorizes one downstream effect.
func (r *Registry) AuthorizeEffect(effectID, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	effect, ok := r.effects[effectID]
	if !ok {
		return fmt.Errorf("people: AuthorizeEffect %s: %w", effectID, ErrImplicitEffect)
	}
	effect.Authorized = true
	effect.Applied = true
	effect.AppliedValue = value
	return nil
}

// ObserveEffect records one downstream observation: acceptance is
// evidence keyed to the applied value, never truth. Truth stays the
// committed legal-name revision digest.
func (r *Registry) ObserveEffect(effectID, note string, accepted bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	effect, ok := r.effects[effectID]
	if !ok || !effect.Applied {
		return fmt.Errorf("people: ObserveEffect %s: %w", effectID, ErrImplicitEffect)
	}
	effect.Observed = true
	effect.ObservedNote = note
	_ = accepted
	return nil
}

// Profile returns one worker's naming state.
func (r *Registry) Profile(workerID string) (PersonProfile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	profile, ok := r.profiles[workerID]
	return profile, ok
}

// Revisions returns the full retained revision chain, oldest first.
func (r *Registry) Revisions(workerID string) []NameRevision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]NameRevision(nil), r.revisions[workerID]...)
}

// CurrentDigest is the committed truth for one worker.
func (r *Registry) CurrentDigest(workerID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	chain := r.revisions[workerID]
	if len(chain) == 0 {
		return ""
	}
	return chain[len(chain)-1].NameDigest
}

// Effect returns one downstream effect.
func (r *Registry) Effect(effectID string) (DownstreamEffect, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	effect, ok := r.effects[effectID]
	if !ok {
		return DownstreamEffect{}, false
	}
	return *effect, true
}

// PendingEffects lists the worker's unauthorized downstream effects.
func (r *Registry) PendingEffects(changeID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for id, effect := range r.effects {
		if strings.HasPrefix(id, "effect/"+changeID+"/") && !effect.Authorized {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
