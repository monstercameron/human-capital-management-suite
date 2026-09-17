package legal

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Rule-pack and registry errors. All are matchable with errors.Is.
var (
	ErrRulePackID           = errors.New("legal: rule pack id is required")
	ErrRulePackVersion      = errors.New("legal: rule pack version must be positive")
	ErrRulePackJurisdiction = errors.New("legal: rule pack jurisdiction must resolve to at least country and state")
	ErrRulePackWindow       = errors.New("legal: rule pack effective window is invalid")
	ErrRulePackDuplicate    = errors.New("legal: rule pack id and version is already registered for this jurisdiction")
	ErrRuleCoverageUnknown  = errors.New("legal: RULE_COVERAGE_UNKNOWN")
	// ErrRulePackSupersession is returned when a supersession would leave a
	// gap or an overlap in the release chain, or would link two releases that
	// are not the same pack in the same jurisdiction.
	ErrRulePackSupersession = errors.New("legal: rule pack supersession chain is invalid")
)

// EffectiveWindow is a half-open [Start, End) calendar-date window: Start is
// inclusive, End is exclusive, and no End means open-ended. It is a
// deliberately small statute-effective-date type: unlike
// values.EffectiveInterval it does not require a governing business
// calendar, because a statute's effective date is not a working-day concept.
type EffectiveWindow struct {
	Start  values.LocalDate
	End    values.LocalDate
	HasEnd bool
}

// NewOpenEffectiveWindow builds an open-ended window starting at start.
func NewOpenEffectiveWindow(start values.LocalDate) (EffectiveWindow, error) {
	if err := start.Validate(); err != nil {
		return EffectiveWindow{}, fmt.Errorf("%w: %v", ErrRulePackWindow, err)
	}
	return EffectiveWindow{Start: start}, nil
}

// NewClosedEffectiveWindow builds a closed [start, end) window.
func NewClosedEffectiveWindow(start, end values.LocalDate) (EffectiveWindow, error) {
	if err := start.Validate(); err != nil {
		return EffectiveWindow{}, fmt.Errorf("%w: %v", ErrRulePackWindow, err)
	}
	if err := end.Validate(); err != nil {
		return EffectiveWindow{}, fmt.Errorf("%w: %v", ErrRulePackWindow, err)
	}
	if start.Compare(end) >= 0 {
		return EffectiveWindow{}, fmt.Errorf("%w: [%s,%s) is empty or inverted", ErrRulePackWindow, start, end)
	}
	return EffectiveWindow{Start: start, End: end, HasEnd: true}, nil
}

// Validate reports whether the window is well formed.
func (w EffectiveWindow) Validate() error {
	if err := w.Start.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrRulePackWindow, err)
	}
	if w.HasEnd {
		if err := w.End.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrRulePackWindow, err)
		}
		if w.Start.Compare(w.End) >= 0 {
			return fmt.Errorf("%w: [%s,%s) is empty or inverted", ErrRulePackWindow, w.Start, w.End)
		}
	}
	return nil
}

// Contains reports whether d falls in the half-open window.
func (w EffectiveWindow) Contains(d values.LocalDate) bool {
	if w.Validate() != nil || d.Validate() != nil {
		return false
	}
	if d.Compare(w.Start) < 0 {
		return false
	}
	if w.HasEnd && d.Compare(w.End) >= 0 {
		return false
	}
	return true
}

// String returns "[start,end)" or "[start,)" for an open end.
func (w EffectiveWindow) String() string {
	end := ""
	if w.HasEnd {
		end = w.End.String()
	}
	return "[" + w.Start.String() + "," + end + ")"
}

// RulePack is one versioned, jurisdiction-scoped, effective-dated bundle of
// typed obligations for a promotion-and-base-pay-change transaction. It is a
// fixture skeleton: [CaliforniaPromotionPack] and [NewYorkPromotionPack] seed
// it from drafted, unreviewed state research, and every rule inside carries
// its own [Citation] back to that research.
// RulePack is also the PackRelease shape of the contract's section 3.1; see
// the [PackRelease] alias and packdefinition.go for the definition ->
// candidate -> release pipeline that produces one.
type RulePack struct {
	PackID       string
	Version      uint32
	Jurisdiction Jurisdiction
	Window       EffectiveWindow

	Notices                 []NoticeObligation
	FieldRestrictions       []FieldRestriction
	RetentionRules          []RetentionRule
	LeaveInteractions       []LeaveInteraction
	PayFrequencyConstraints []PayFrequencyConstraint
	FinalPayDeadlines       []FinalPayDeadline
	PayTransparencyDuties   []PayTransparencyDuty
	NonCompeteThresholds    []NonCompeteThreshold
	EVerifyChecks           []EVerifyStatusCheck
	MiniWARNTriggers        []MiniWARNTrigger

	// The fields below are LEGAL-010's and LEGAL-011's additions. Every one
	// is optional on a pack built directly in Go, so LEGAL-001's hand-built
	// fixtures keep validating and evaluating unchanged; the definition-file
	// loader in packdefinition.go requires them.

	// MinorVersion is the minor half of the contract's major.minor version.
	// A minor bump is a review-status or confidence-marker change; it is a
	// new, separately registered release, never an edit.
	MinorVersion uint32
	// VocabularyVersion is the [ObligationType] vocabulary this release was
	// typed against. Zero reads as [VocabularyVersion1].
	VocabularyVersion VocabularyVersion
	// SourceType names what authority the release encodes.
	SourceType SourceType
	// ReviewStatus is the release's own status, distinct from the per-rule
	// citation status.
	ReviewStatus ReviewStatus

	WageFloors           []WageFloorRule
	PayEquityReviews     []PayEquityReviewRule
	PayStatements        []PayStatementRule
	Classifications      []ClassificationRule
	PersonnelFileRules   []PersonnelFileRule
	AntiRetaliationRules []AntiRetaliationRule
	JobSecurityRules     []JobSecurityRule
	SeparationFilings    []SeparationFilingRule
	DrugTestingRules     []DrugTestingRule
	BreachNotifications  []BreachNotificationRule
	AutomatedDecisions   []AutomatedDecisionRule
	MonitoringConsents   []MonitoringConsentRule

	// PreemptionAssertions are this subdivision's claims that it preempts
	// locality-level rules of a named kind.
	PreemptionAssertions []PreemptionAssertion

	// Supersedes and SupersededBy link the release chain. A superseding
	// release closes the prior window: the predecessor's End equals the
	// successor's Start, checked by [Registry.Supersede].
	Supersedes   *RulePackRelease
	SupersededBy *RulePackRelease

	// Digest and Signatures are set by [PackCandidate.Sign] and are empty on
	// a pack built directly in Go. They are excluded from the canonical
	// encoding they cover.
	Digest     string
	Signatures []RoleSignature
}

// PackRelease is the immutable, digested, signed artifact [Evaluate] reads.
// The contract's section 3.1 states that RulePack is the PackRelease shape,
// so this is an alias rather than a parallel type: there is exactly one
// release struct in this package.
type PackRelease = RulePack

// EffectiveVocabulary returns the vocabulary the pack was typed against,
// reading the zero value as [VocabularyVersion1] so a pack built in Go before
// LEGAL-011 is treated as "the author considered the original ten kinds",
// never as "the author considered all twenty-two".
func (p RulePack) EffectiveVocabulary() VocabularyVersion {
	if p.VocabularyVersion == VocabularyVersionUnspecified {
		return VocabularyVersion1
	}
	return p.VocabularyVersion
}

// Release returns the version-pinned reference to this pack.
func (p RulePack) Release() RulePackRelease {
	return RulePackRelease{
		PackID:       p.PackID,
		Version:      p.Version,
		MinorVersion: p.MinorVersion,
		Jurisdiction: p.Jurisdiction,
	}
}

// obligationRule is the behaviour every typed body shares: it validates, it
// digests, it says whether it fires, and it renders a one-line human
// description. Evaluation walks packs through this interface so that adding a
// kind never adds a switch statement to a call site.
type obligationRule interface {
	obligationID() string
	obligationCitation() Citation
	validate() error
	canonicalBody(dst []byte) []byte
	trigger(p PromotionProposalSnapshot) (bool, NotApplicableReason)
	describe() string
}

// typedObligation pairs a rule with the kind it was declared under, in the
// pack's declared order. The digest and the receipt both walk this list.
type typedObligation struct {
	Type ObligationType
	Rule obligationRule
}

// obligations returns every obligation in the pack, in kind-ordinal order and
// within a kind in declared order. That order is the digest's order and the
// receipt's order, so it is produced in exactly one place.
func (p RulePack) obligations() []typedObligation {
	var out []typedObligation
	add := func(t ObligationType, rules ...obligationRule) {
		for _, r := range rules {
			out = append(out, typedObligation{Type: t, Rule: r})
		}
	}
	for _, o := range p.Notices {
		add(ObligationTypeNotice, o)
	}
	for _, o := range p.FieldRestrictions {
		add(ObligationTypeFieldRestriction, o)
	}
	for _, o := range p.RetentionRules {
		add(ObligationTypeRetention, o)
	}
	for _, o := range p.LeaveInteractions {
		add(ObligationTypeLeaveInteraction, o)
	}
	for _, o := range p.PayFrequencyConstraints {
		add(ObligationTypePayFrequency, o)
	}
	for _, o := range p.FinalPayDeadlines {
		add(ObligationTypeFinalPayDeadline, o)
	}
	for _, o := range p.PayTransparencyDuties {
		add(ObligationTypePayTransparency, o)
	}
	for _, o := range p.NonCompeteThresholds {
		add(ObligationTypeNonCompete, o)
	}
	for _, o := range p.EVerifyChecks {
		add(ObligationTypeEVerify, o)
	}
	for _, o := range p.MiniWARNTriggers {
		add(ObligationTypeMiniWARN, o)
	}
	for _, o := range p.WageFloors {
		add(ObligationTypeWageFloor, o)
	}
	for _, o := range p.PayEquityReviews {
		add(ObligationTypePayEquityReview, o)
	}
	for _, o := range p.PayStatements {
		add(ObligationTypePayStatement, o)
	}
	for _, o := range p.Classifications {
		add(ObligationTypeClassification, o)
	}
	for _, o := range p.PersonnelFileRules {
		add(ObligationTypePersonnelFile, o)
	}
	for _, o := range p.AntiRetaliationRules {
		add(ObligationTypeAntiRetaliation, o)
	}
	for _, o := range p.JobSecurityRules {
		add(ObligationTypeJobSecurity, o)
	}
	for _, o := range p.SeparationFilings {
		add(ObligationTypeSeparationFiling, o)
	}
	for _, o := range p.DrugTestingRules {
		add(ObligationTypeDrugTesting, o)
	}
	for _, o := range p.BreachNotifications {
		add(ObligationTypeBreachNotification, o)
	}
	for _, o := range p.AutomatedDecisions {
		add(ObligationTypeAutomatedDecision, o)
	}
	for _, o := range p.MonitoringConsents {
		add(ObligationTypeMonitoringConsent, o)
	}
	return out
}

// KindCounts returns how many obligations the pack carries per kind. It is
// what the conformance oracle compares against the contract's section 5
// matrix.
func (p RulePack) KindCounts() map[ObligationType]int {
	counts := map[ObligationType]int{}
	for _, o := range p.obligations() {
		counts[o.Type]++
	}
	return counts
}

// Validate reports whether the pack and every obligation inside it are well
// formed and cited. It does not check counsel review status beyond requiring
// one be declared; see [ReviewStatus].
func (p RulePack) Validate() error {
	if p.PackID == "" {
		return ErrRulePackID
	}
	if p.Version == 0 {
		return ErrRulePackVersion
	}
	if !p.Jurisdiction.IsStateResolved() {
		return ErrRulePackJurisdiction
	}
	if err := p.Window.Validate(); err != nil {
		return err
	}
	if p.VocabularyVersion > SupportedVocabularyVersion {
		return fmt.Errorf("%w: pack %s declares vocabulary %d, this build supports %d",
			ErrVocabularyVersionUnsupported, p.PackID, p.VocabularyVersion, SupportedVocabularyVersion)
	}
	vocab := p.EffectiveVocabulary()
	seen := map[string]bool{}
	for _, o := range p.obligations() {
		if VocabularyOf(o.Type) > vocab {
			return fmt.Errorf("legal: pack %s declares vocabulary %d but carries a %s obligation added in vocabulary %d",
				p.PackID, vocab, o.Type, VocabularyOf(o.Type))
		}
		if err := o.Rule.validate(); err != nil {
			return err
		}
		if err := o.Rule.obligationCitation().ValidateForVocabulary(vocab); err != nil {
			return err
		}
		id := o.Rule.obligationID()
		if seen[id] {
			return fmt.Errorf("legal: pack %s declares obligation id %q twice", p.PackID, id)
		}
		seen[id] = true
	}
	for _, a := range p.PreemptionAssertions {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateForRelease is [RulePack.Validate] plus the release-only rules the
// contract's sections 3.1 and 7 impose on a published artifact: a declared
// source type, a declared review status, and — for anything claiming
// COUNSEL_APPROVED — no rule still marked VERIFY or DISPUTED.
func (p RulePack) ValidateForRelease() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.SourceType == SourceTypeUnspecified {
		return fmt.Errorf("%w: pack %s", ErrSourceType, p.PackID)
	}
	if p.ReviewStatus == ReviewStatusUnspecified {
		return fmt.Errorf("%w: pack %s declares no review status", ErrCitationStatus, p.PackID)
	}
	if p.ReviewStatus == ReviewStatusCounselApproved {
		for _, o := range p.obligations() {
			if o.Rule.obligationCitation().ConfidenceMarker.BlocksCounselApproval() {
				return fmt.Errorf("legal: pack %s claims COUNSEL_APPROVED but %s %q is still %s",
					p.PackID, o.Type, o.Rule.obligationID(),
					o.Rule.obligationCitation().ConfidenceMarker)
			}
		}
	}
	if p.ReviewStatus.Releasable() {
		for _, o := range p.obligations() {
			completer, ok := o.Rule.(releaseCompleter)
			if !ok {
				continue
			}
			if err := completer.validateComplete(); err != nil {
				return err
			}
		}
	}
	return nil
}

// packKey identifies one rule pack's registration slot. MinorVersion is part
// of the key because a minor bump — a cleared VERIFY marker, a raised review
// status — is a separate release, not an edit of the one already published.
type packKey struct {
	Jurisdiction Jurisdiction
	PackID       string
	Version      uint32
	MinorVersion uint32
}

// Registry publishes [RulePack] versions and answers, for a jurisdiction and
// effective date, which release governs. Registration is immutable: the same
// (jurisdiction, pack id, version) can never be registered twice, so a
// [LegalContext] that pinned a release keeps resolving to the same content
// forever.
type Registry struct {
	mu    sync.RWMutex
	packs map[packKey]*RulePack
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{packs: map[packKey]*RulePack{}}
}

// Register validates and publishes pack. It copies pack's obligation slices
// so a caller's later mutation of its own pack value can never reach the
// registry.
func (r *Registry) Register(pack RulePack) error {
	if err := pack.Validate(); err != nil {
		return err
	}
	stored := pack
	stored.Notices = append([]NoticeObligation(nil), pack.Notices...)
	stored.FieldRestrictions = append([]FieldRestriction(nil), pack.FieldRestrictions...)
	stored.RetentionRules = append([]RetentionRule(nil), pack.RetentionRules...)
	stored.LeaveInteractions = append([]LeaveInteraction(nil), pack.LeaveInteractions...)
	stored.PayFrequencyConstraints = append([]PayFrequencyConstraint(nil), pack.PayFrequencyConstraints...)
	stored.FinalPayDeadlines = append([]FinalPayDeadline(nil), pack.FinalPayDeadlines...)
	stored.PayTransparencyDuties = append([]PayTransparencyDuty(nil), pack.PayTransparencyDuties...)
	stored.NonCompeteThresholds = append([]NonCompeteThreshold(nil), pack.NonCompeteThresholds...)
	stored.EVerifyChecks = append([]EVerifyStatusCheck(nil), pack.EVerifyChecks...)
	stored.MiniWARNTriggers = append([]MiniWARNTrigger(nil), pack.MiniWARNTriggers...)
	stored.WageFloors = append([]WageFloorRule(nil), pack.WageFloors...)
	stored.PayEquityReviews = append([]PayEquityReviewRule(nil), pack.PayEquityReviews...)
	stored.PayStatements = append([]PayStatementRule(nil), pack.PayStatements...)
	stored.Classifications = append([]ClassificationRule(nil), pack.Classifications...)
	stored.PersonnelFileRules = append([]PersonnelFileRule(nil), pack.PersonnelFileRules...)
	stored.AntiRetaliationRules = append([]AntiRetaliationRule(nil), pack.AntiRetaliationRules...)
	stored.JobSecurityRules = append([]JobSecurityRule(nil), pack.JobSecurityRules...)
	stored.SeparationFilings = append([]SeparationFilingRule(nil), pack.SeparationFilings...)
	stored.DrugTestingRules = append([]DrugTestingRule(nil), pack.DrugTestingRules...)
	stored.BreachNotifications = append([]BreachNotificationRule(nil), pack.BreachNotifications...)
	stored.AutomatedDecisions = append([]AutomatedDecisionRule(nil), pack.AutomatedDecisions...)
	stored.MonitoringConsents = append([]MonitoringConsentRule(nil), pack.MonitoringConsents...)
	stored.PreemptionAssertions = append([]PreemptionAssertion(nil), pack.PreemptionAssertions...)
	stored.Signatures = append([]RoleSignature(nil), pack.Signatures...)
	if pack.Supersedes != nil {
		ref := *pack.Supersedes
		stored.Supersedes = &ref
	}
	if pack.SupersededBy != nil {
		ref := *pack.SupersededBy
		stored.SupersededBy = &ref
	}

	key := packKey{
		Jurisdiction: pack.Jurisdiction,
		PackID:       pack.PackID,
		Version:      pack.Version,
		MinorVersion: pack.MinorVersion,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.packs[key]; exists {
		return fmt.Errorf("%w: %s %s v%d.%d", ErrRulePackDuplicate,
			pack.Jurisdiction, pack.PackID, pack.Version, pack.MinorVersion)
	}
	r.packs[key] = &stored
	return nil
}

// Lookup returns the highest-versioned registered pack whose jurisdiction
// matches j (falling back from an exact country/state/locality match to a
// state-level pack when j names a locality no pack targets specifically) and
// whose effective window contains date. It returns [ErrRuleCoverageUnknown]
// when nothing matches: an unregistered jurisdiction is never treated as
// "no obligations apply".
func (r *Registry) Lookup(j Jurisdiction, date values.LocalDate) (*RulePack, error) {
	if err := date.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRuleCoverageUnknown, err)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	candidateKeys := []Jurisdiction{j}
	if j.Locality != "" {
		candidateKeys = append(candidateKeys, Jurisdiction{Country: j.Country, State: j.State})
	}

	var best *RulePack
	for _, key := range candidateKeys {
		for k, pack := range r.packs {
			if k.Jurisdiction != key {
				continue
			}
			if !pack.Window.Contains(date) {
				continue
			}
			if best == nil || pack.Version > best.Version ||
				(pack.Version == best.Version && pack.MinorVersion > best.MinorVersion) {
				best = pack
			}
		}
		if best != nil {
			break
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: no rule pack governs %s as of %s", ErrRuleCoverageUnknown, j, date)
	}
	// Return a defensive copy so a caller can never mutate registry state
	// through the returned pointer.
	out := *best
	return &out, nil
}

// LookupExact returns the highest-versioned release registered for exactly j
// and effective on date. Unlike Lookup it never falls back to a subdivision
// release, which is required when pinning locality overlays.
func (r *Registry) LookupExact(j Jurisdiction, date values.LocalDate) (*RulePack, error) {
	if err := date.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRuleCoverageUnknown, err)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var best *RulePack
	for k, pack := range r.packs {
		if k.Jurisdiction != j || !pack.Window.Contains(date) {
			continue
		}
		if best == nil || pack.Version > best.Version || (pack.Version == best.Version && pack.MinorVersion > best.MinorVersion) {
			best = pack
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: no exact rule pack governs %s as of %s", ErrRuleCoverageUnknown, j, date)
	}
	out := *best
	return &out, nil
}

// LookupAll returns every registered pack for exactly j effective on date,
// in pack-id, version and minor-version order. Unlike Lookup it never falls
// back from a locality to its subdivision and never collapses several packs
// to one: selection across pack families needs the whole candidate set, not
// a winner. The returned packs are defensive copies.
func (r *Registry) LookupAll(j Jurisdiction, date values.LocalDate) []RulePack {
	if r == nil || date.Validate() != nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []RulePack
	for k, pack := range r.packs {
		if k.Jurisdiction != j || !pack.Window.Contains(date) {
			continue
		}
		out = append(out, *pack)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PackID != out[j].PackID {
			return out[i].PackID < out[j].PackID
		}
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		return out[i].MinorVersion < out[j].MinorVersion
	})
	return out
}

// IsRegisteredExact reports whether an exact jurisdiction release is
// registered and effective on date. Unlike Lookup, it never falls back from
// a locality to its subdivision; this distinction is required for locality
// overlay receipts.
func (r *Registry) IsRegisteredExact(j Jurisdiction, date values.LocalDate) bool {
	if r == nil || date.Validate() != nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for k, pack := range r.packs {
		if k.Jurisdiction == j && pack.Window.Contains(date) {
			return true
		}
	}
	return false
}

// GetExact returns the exact (jurisdiction, pack id, version) release, or
// [ErrRuleCoverageUnknown] if it was never registered or has since been
// removed from this registry instance. [Evaluate] uses this to re-fetch the
// precise release a [LegalContext] pinned, rather than re-resolving "latest".
func (r *Registry) GetExact(release RulePackRelease) (*RulePack, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pack, ok := r.packs[packKey{
		Jurisdiction: release.Jurisdiction,
		PackID:       release.PackID,
		Version:      release.Version,
		MinorVersion: release.MinorVersion,
	}]
	if !ok {
		return nil, fmt.Errorf("%w: %s %s v%d.%d is not registered",
			ErrRuleCoverageUnknown, release.Jurisdiction, release.PackID, release.Version, release.MinorVersion)
	}
	out := *pack
	return &out, nil
}

// Supersede publishes successor as the replacement for predecessor: it closes
// the predecessor's effective window at the successor's start date, registers
// the successor, and links the two through Supersedes/SupersededBy.
//
// A published release is never edited, so the closed predecessor is written
// back as the same (jurisdiction, pack id, version) slot's content rather
// than as a new release; the closure is the one mutation the contract's
// section 3.3 sanctions, because "the prior release gains an End equal to the
// new release's Start" is how an amendment is expressed. [Registry.GetExact]
// keeps returning the predecessor for any context that pinned it, which is
// what makes a historical evaluation reproducible.
func (r *Registry) Supersede(predecessor RulePackRelease, successor RulePack) error {
	if err := successor.Validate(); err != nil {
		return err
	}
	if predecessor.PackID != successor.PackID {
		return fmt.Errorf("%w: %s cannot supersede a different pack %s",
			ErrRulePackSupersession, successor.PackID, predecessor.PackID)
	}
	if predecessor.Jurisdiction != successor.Jurisdiction {
		return fmt.Errorf("%w: %s supersedes across jurisdictions %s -> %s",
			ErrRulePackSupersession, successor.PackID, predecessor.Jurisdiction, successor.Jurisdiction)
	}
	if successor.Version < predecessor.Version ||
		(successor.Version == predecessor.Version && successor.MinorVersion <= predecessor.MinorVersion) {
		return fmt.Errorf("%w: successor v%d.%d does not increase on predecessor v%d.%d",
			ErrRulePackSupersession, successor.Version, successor.MinorVersion,
			predecessor.Version, predecessor.MinorVersion)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	prevKey := packKey{
		Jurisdiction: predecessor.Jurisdiction,
		PackID:       predecessor.PackID,
		Version:      predecessor.Version,
		MinorVersion: predecessor.MinorVersion,
	}
	prev, ok := r.packs[prevKey]
	if !ok {
		return fmt.Errorf("%w: predecessor %s v%d.%d is not registered",
			ErrRuleCoverageUnknown, predecessor.PackID, predecessor.Version, predecessor.MinorVersion)
	}
	if prev.Window.Start.Compare(successor.Window.Start) >= 0 {
		return fmt.Errorf("%w: successor starts %s, not after predecessor start %s",
			ErrRulePackSupersession, successor.Window.Start, prev.Window.Start)
	}
	closed, err := NewClosedEffectiveWindow(prev.Window.Start, successor.Window.Start)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRulePackSupersession, err)
	}

	successorRef := successor.Release()
	predecessorRef := prev.Release()
	stored := successor
	stored.Supersedes = &predecessorRef
	newKey := packKey{
		Jurisdiction: successor.Jurisdiction,
		PackID:       successor.PackID,
		Version:      successor.Version,
		MinorVersion: successor.MinorVersion,
	}
	if _, exists := r.packs[newKey]; exists {
		return fmt.Errorf("%w: %s %s v%d.%d", ErrRulePackDuplicate,
			successor.Jurisdiction, successor.PackID, successor.Version, successor.MinorVersion)
	}
	r.packs[newKey] = &stored

	updatedPrev := *prev
	updatedPrev.Window = closed
	updatedPrev.SupersededBy = &successorRef
	r.packs[prevKey] = &updatedPrev
	return nil
}

// PromotionProposalSnapshot is the minimal, evaluation-time snapshot of a
// promotion-and-base-pay-change proposal that [Evaluate] needs. It is a
// fixture input shape: the real proposal snapshot type belongs to the
// business-intent kernel (see planning/specs/business-intent-and-change-
// request.md), which this package does not own or import.
type PromotionProposalSnapshot struct {
	WorkerID      string
	LegalEntityID string
	// EffectiveDate is the proposed pay-change effective date.
	EffectiveDate values.LocalDate
	// CurrentBasePay and NewBasePay are compared to decide whether a
	// pay-rate-change notice is triggered. Either may be the zero Money when
	// the caller does not have both figures; Evaluate then treats the pay
	// change as indeterminate and does not raise a notice obligation on that
	// basis alone.
	CurrentBasePay values.Money
	NewBasePay     values.Money
	// PayFrequency is the worker's current pay frequency, e.g. "SEMIMONTHLY".
	PayFrequency string
	// IsInternalPromotion marks the transaction as an internal promotion
	// rather than an external hire into the role.
	IsInternalPromotion bool
	// CollectsSalaryHistory marks that the compensation-setting process for
	// this transaction asked for or relied on the worker's salary history.
	CollectsSalaryHistory bool
	// OnProtectedLeave marks that the worker is currently on a protected
	// leave of absence.
	OnProtectedLeave bool
	// HasExistingNonCompete marks that the worker is subject to an existing
	// non-compete or non-solicit agreement.
	HasExistingNonCompete bool
	// IsNewHire marks that this transaction is a new hire rather than a
	// change to an existing worker. Always false for a promotion; carried so
	// [EVerifyStatusCheck] obligations have a fact to key on.
	IsNewHire bool
	// SeparationConcurrent marks that this transaction concurrently
	// separates the worker. Always false for a pure promotion; carried so
	// [FinalPayDeadline] obligations have a fact to key on.
	SeparationConcurrent bool
	// WorkforceReductionCount is the number of workers affected by a
	// concurrent workforce reduction this transaction is part of, if any.
	// Zero for an ordinary promotion.
	WorkforceReductionCount int

	// The facts below are what LEGAL-011's twelve added kinds key on, plus
	// the leave-program balances the corrected LEAVE_INTERACTION trigger
	// needs. Every one is a fact about the proposal, never a conclusion: the
	// snapshot never says "this is retaliation" or "this worker is exempt",
	// only what happened and when.

	// LeaveProgramBalances names every leave program in which the worker
	// holds a balance, e.g. "accrued paid sick leave". The corrected
	// LEAVE_INTERACTION trigger matches a pack's named program against this
	// list; an empty list with OnProtectedLeave false means no leave rule
	// binds.
	LeaveProgramBalances []string
	// RoleChanged, HoursChanged and PayBasisChanged are the non-pay
	// dimensions a CLASSIFICATION rule keys on.
	RoleChanged     bool
	HoursChanged    bool
	PayBasisChanged bool
	// IsDemotion marks a downward role change. With a pay decrease and a
	// concurrent separation it is one of the three adverse changes a
	// JOB_SECURITY rule keys on.
	IsDemotion bool
	// RecordedProtectedActivities lists protected activities already on
	// record for this worker, each with how many days before the effective
	// date it was recorded. ANTI_RETALIATION keys on it.
	RecordedProtectedActivities []RecordedProtectedActivity
	// RoleBecomesSafetySensitive and DrugTestOrdered are what a DRUG_TESTING
	// rule keys on.
	RoleBecomesSafetySensitive bool
	DrugTestOrdered            bool
	// BreachIncidentOpened marks that a personal-data breach incident is open
	// on this transaction. BREACH_NOTIFICATION keys on it.
	BreachIncidentOpened bool
	// AutomatedDecisionApplied marks that a model scored, ranked or
	// recommended the subject of this transaction. AUTOMATED_DECISION keys on
	// it.
	AutomatedDecisionApplied bool
	// DataCategoriesTouched names the worker-data categories this
	// transaction reads or writes. MONITORING_CONSENT keys on it.
	DataCategoriesTouched []string
}

// RecordedProtectedActivity is one protected activity already on record for
// the worker, and how long before the transaction's effective date it was
// recorded. It is a fact, not a finding: recording a complaint is not a claim
// that an adverse action was retaliation.
type RecordedProtectedActivity struct {
	// Kind names the activity in the source system's own vocabulary, e.g.
	// "workers_compensation_claim".
	Kind string
	// DaysBeforeEffectiveDate is how many days before the effective date the
	// activity was recorded. A negative value means the activity postdates
	// the effective date and never fires a lookback.
	DaysBeforeEffectiveDate int
}

// holdsLeaveBalanceIn reports whether the worker holds a balance in the named
// leave program. Matching is case-insensitive and whitespace-trimmed, and a
// pack may name "*" to mean every program it governs.
func (p PromotionProposalSnapshot) holdsLeaveBalanceIn(program string) bool {
	want := strings.TrimSpace(program)
	if want == "" {
		return false
	}
	for _, held := range p.LeaveProgramBalances {
		if want == "*" || strings.EqualFold(strings.TrimSpace(held), want) {
			return true
		}
	}
	return false
}

// isAdverseChange reports whether the proposal is one of the three adverse
// changes a JOB_SECURITY standard is judged against.
func (p PromotionProposalSnapshot) isAdverseChange() bool {
	return p.payDecreased() || p.IsDemotion || p.SeparationConcurrent
}

// payDecreased reports whether new base pay is strictly below current base
// pay. Like payRateChanged it returns false, never an error, when the two
// amounts are unset or not comparable.
func (p PromotionProposalSnapshot) payDecreased() bool {
	if p.CurrentBasePay.Validate() != nil || p.NewBasePay.Validate() != nil {
		return false
	}
	cmp, err := p.NewBasePay.Cmp(p.CurrentBasePay)
	if err != nil {
		return false
	}
	return cmp < 0
}

// payRateChanged reports whether the snapshot demonstrates a changed base pay
// rate. It returns false, not an error, when either amount is unset or they
// are in different currencies: an indeterminate pay comparison never manufactures
// a notice obligation on its own.
func (p PromotionProposalSnapshot) payRateChanged() bool {
	if p.CurrentBasePay.Validate() != nil || p.NewBasePay.Validate() != nil {
		return false
	}
	cmp, err := p.NewBasePay.Cmp(p.CurrentBasePay)
	if err != nil {
		return false
	}
	return cmp != 0
}

// LegalEvaluationStatus is the outcome of evaluating a [LegalContext] against
// a proposal, mirroring the vocabulary in
// planning/data/models/kernel-governance-and-evidence.md. This package
// implements the subset [Evaluate] can actually produce at P1B reduced depth;
// the remaining values from that vocabulary are declared for forward
// compatibility with the Phase 2 composition engine and are never returned
// here.
type LegalEvaluationStatus uint8

// Legal evaluation statuses.
const (
	LegalEvaluationStatusUnspecified LegalEvaluationStatus = iota
	// LegalEvaluationStatusResolvedAllow means the pinned rule-pack release
	// produced zero applicable obligations for this proposal.
	LegalEvaluationStatusResolvedAllow
	// LegalEvaluationStatusAllowWithObligations means the transaction may
	// proceed but one or more obligations are attached and must be bound.
	LegalEvaluationStatusAllowWithObligations
	// LegalEvaluationStatusRuleCoverageUnknown means a pinned release could
	// not be re-fetched from the registry at evaluation time.
	LegalEvaluationStatusRuleCoverageUnknown
)

var legalEvaluationStatusWire = map[LegalEvaluationStatus]string{
	LegalEvaluationStatusResolvedAllow:        "RESOLVED_ALLOW",
	LegalEvaluationStatusAllowWithObligations: "ALLOW_WITH_OBLIGATIONS",
	LegalEvaluationStatusRuleCoverageUnknown:  "RULE_COVERAGE_UNKNOWN",
}

// String returns the stable wire token.
func (s LegalEvaluationStatus) String() string {
	if w, ok := legalEvaluationStatusWire[s]; ok {
		return w
	}
	return "LEGAL_EVALUATION_STATUS_UNSPECIFIED"
}

// EvaluationResult is what [Evaluate] returns: the overall status and every
// obligation found applicable, sorted deterministically by type and then id.
type EvaluationResult struct {
	Status           LegalEvaluationStatus
	Jurisdiction     Jurisdiction
	RulePackReleases []RulePackRelease
	Obligations      []AppliedObligation

	// NotApplicable records every obligation whose trigger predicate
	// evaluated false, with the fact that made it false. The contract's
	// section 4.4 forbids silence: an obligation missing from both lists is
	// a bug, not a "no". Together with Obligations it accounts for every
	// obligation in every pinned release, which is what makes the result an
	// audit artifact rather than a list of hits.
	NotApplicable []ConsideredObligation
	// NotConsidered records kinds a release could not answer because it was
	// typed against an older vocabulary than this engine knows. It is never
	// merged into NotApplicable: "not considered" and "does not apply" are
	// different findings.
	NotConsidered []NotConsideredKind
	// PreemptionsApplied records locality obligations removed before trigger
	// evaluation and composition.
	PreemptionsApplied []PreemptionApplied
	// ReceiptNotes makes incomplete locality coverage explicit rather than
	// silently treating an unregistered locality as obligation-free.
	ReceiptNotes []LegalEvaluationNote
	Composition  CompositionReceipt
	pinnedPacks  []RulePack
}

// LegalEvaluationNote is deterministic non-obligation evidence carried by an
// evaluation receipt.
type LegalEvaluationNote struct {
	Code         string
	Jurisdiction Jurisdiction
}

// Evaluate re-fetches every rule-pack release ctx pinned and returns the
// obligations that apply to proposal under it. It fails closed: an unverified
// context, or a pinned release the registry can no longer produce, is
// reported rather than silently evaluated against nothing.
func Evaluate(ctx *LegalContext, proposal PromotionProposalSnapshot, registry *Registry) (EvaluationResult, error) {
	if ctx == nil {
		return EvaluationResult{}, errors.New("legal: Evaluate needs a resolved LegalContext")
	}
	if registry == nil {
		return EvaluationResult{}, errors.New("legal: Evaluate needs a rule-pack registry")
	}
	if err := ctx.Verify(); err != nil {
		return EvaluationResult{}, fmt.Errorf("legal: context failed signature verification: %w", err)
	}

	releases := ctx.RulePackReleases()
	if len(releases) == 0 {
		return EvaluationResult{}, errors.New("legal: context carries no rule-pack releases")
	}
	receiptNotes := unregisteredLocalityNotes(ctx.UnregisteredLocalities())

	var obligations []AppliedObligation
	var notApplicable []ConsideredObligation
	var notConsidered []NotConsideredKind
	var pinnedPacks []RulePack
	var triggeredInputs []composableObligation
	seenReleases := make(map[string]struct{})
	for _, release := range releases {
		identity := releaseIdentity(release)
		if _, duplicate := seenReleases[identity]; duplicate {
			continue
		}
		seenReleases[identity] = struct{}{}
		pack, err := registry.GetExact(release)
		if err != nil {
			return EvaluationResult{
				Status:           LegalEvaluationStatusRuleCoverageUnknown,
				Jurisdiction:     ctx.Jurisdiction(),
				RulePackReleases: releases,
				ReceiptNotes:     receiptNotes,
			}, nil
		}
		if pack.VocabularyVersion > SupportedVocabularyVersion {
			return EvaluationResult{
					Status:           LegalEvaluationStatusRuleCoverageUnknown,
					Jurisdiction:     ctx.Jurisdiction(),
					RulePackReleases: releases,
					ReceiptNotes:     receiptNotes,
				}, fmt.Errorf("%w: release %s v%d.%d declares vocabulary %d, this build supports %d",
					ErrVocabularyVersionUnsupported, pack.PackID, pack.Version, pack.MinorVersion,
					pack.VocabularyVersion, SupportedVocabularyVersion)
		}
		pinnedPacks = append(pinnedPacks, *pack)
	}
	// Preemption is deliberately a separate stage after release pinning and
	// before trigger evaluation. Construct identity-only inputs so this stage
	// cannot invoke a comparator or inspect proposal facts.
	var preemptionInputs []composableObligation
	for _, pack := range pinnedPacks {
		for _, item := range pack.obligations() {
			preemptionInputs = append(preemptionInputs, composableObligation{
				evidence:     ObligationEvidence{Jurisdiction: pack.Jurisdiction, Type: item.Type, ID: item.Rule.obligationID()},
				jurisdiction: pack.Jurisdiction,
			})
		}
	}
	_, preemptions, err := applyPreemptionsToComposable(preemptionInputs, pinnedPacks)
	if err != nil {
		return EvaluationResult{}, err
	}
	for _, pack := range pinnedPacks {
		applied, considered := applicableObligations(pack, proposal, preemptions)
		obligations = append(obligations, applied...)
		notApplicable = append(notApplicable, considered...)
		notConsidered = append(notConsidered, unconsideredKinds(pack)...)
		for _, item := range pack.obligations() {
			for _, hit := range applied {
				if hit.Jurisdiction == pack.Jurisdiction && hit.PackID == pack.PackID && hit.PackVersion == pack.Version && hit.Type == item.Type && hit.ID == item.Rule.obligationID() {
					triggeredInputs = append(triggeredInputs, composableObligation{evidence: ObligationEvidence{Jurisdiction: pack.Jurisdiction, PackID: pack.PackID, PackVersion: pack.Version, Type: item.Type, ID: item.Rule.obligationID(), Description: item.Rule.describe(), Citation: item.Rule.obligationCitation()}, rule: item.Rule, jurisdiction: pack.Jurisdiction, packKey: releaseIdentity(pack.Release())})
				}
			}
		}
	}
	composition := composeTriggeredInputs(ctx.Jurisdiction(), triggeredInputs, preemptions)
	sort.Slice(obligations, func(i, j int) bool {
		if obligations[i].Type != obligations[j].Type {
			return obligations[i].Type < obligations[j].Type
		}
		return obligations[i].ID < obligations[j].ID
	})
	sort.Slice(notApplicable, func(i, j int) bool {
		if notApplicable[i].Type != notApplicable[j].Type {
			return notApplicable[i].Type < notApplicable[j].Type
		}
		return notApplicable[i].ID < notApplicable[j].ID
	})

	status := LegalEvaluationStatusResolvedAllow
	if len(obligations) > 0 {
		status = LegalEvaluationStatusAllowWithObligations
	}
	return EvaluationResult{
		Status:             status,
		Jurisdiction:       ctx.Jurisdiction(),
		RulePackReleases:   releases,
		Obligations:        obligations,
		NotApplicable:      notApplicable,
		NotConsidered:      notConsidered,
		PreemptionsApplied: preemptions,
		ReceiptNotes:       receiptNotes,
		Composition:        composition,
		pinnedPacks:        pinnedPacks,
	}, nil
}

func composeTriggeredInputs(primary Jurisdiction, items []composableObligation, preemptions []PreemptionApplied) CompositionReceipt {
	byKind := make(map[ObligationType][]composableObligation)
	for _, item := range items {
		byKind[item.evidence.Type] = append(byKind[item.evidence.Type], item)
	}
	receipt := CompositionReceipt{Status: CompositionResolved, Jurisdictions: []Jurisdiction{primary}, PreemptionsApplied: slices.Clone(preemptions)}
	for _, item := range items {
		receipt.Inputs = append(receipt.Inputs, item.evidence)
	}
	for _, kind := range sortedKinds(byKind) {
		definition, ok := obligationComparators[kind]
		if !ok {
			definition = comparatorDefinition{Name: ComparatorUnion, Resolve: resolveUnion}
		}
		selected, winner := definition.Resolve(byKind[kind])
		trace := CompositionTrace{Kind: kind, Comparator: definition.Name, Winner: winner}
		for _, item := range byKind[kind] {
			trace.Inputs = append(trace.Inputs, item.evidence)
		}
		receipt.Traces = append(receipt.Traces, trace)
		for _, item := range selected {
			receipt.Obligations = append(receipt.Obligations, ComposedObligation{Type: item.evidence.Type, ID: item.evidence.ID, Description: item.evidence.Description, Citation: item.evidence.Citation, Jurisdiction: item.jurisdiction, Sources: []ObligationEvidence{item.evidence}})
		}
	}
	receipt.Contradictions = findRetentionContradictions(byKind[ObligationTypeRetention])
	if len(receipt.Contradictions) > 0 {
		receipt.Status = CompositionContradictoryRequirements
		receipt.Obligations = nil
	}
	receipt.sort()
	receipt.refreshDigest()
	return receipt
}

func unregisteredLocalityNotes(localities []Jurisdiction) []LegalEvaluationNote {
	if len(localities) == 0 {
		return nil
	}
	notes := make([]LegalEvaluationNote, 0, len(localities))
	for _, locality := range localities {
		notes = append(notes, LegalEvaluationNote{Code: "unregistered_locality", Jurisdiction: locality})
	}
	return notes
}

// unconsideredKinds lists the kinds this engine knows that pack's vocabulary
// predates. A v1 release evaluated by a v2 engine has not said "WAGE_FLOOR
// does not apply"; it has said nothing about WAGE_FLOOR at all, and the
// contract's section 3.2 requires the receipt to say so.
func unconsideredKinds(pack RulePack) []NotConsideredKind {
	vocab := pack.EffectiveVocabulary()
	if vocab >= SupportedVocabularyVersion {
		return nil
	}
	var out []NotConsideredKind
	for _, t := range AllObligationTypes() {
		if VocabularyOf(t) <= vocab {
			continue
		}
		out = append(out, NotConsideredKind{
			Type:              t,
			PackID:            pack.PackID,
			PackVersion:       pack.Version,
			ReleaseVocabulary: vocab,
		})
	}
	return out
}

// applicableObligations walks every obligation the pack declares, in
// kind-ordinal order, and sorts each into applied or CONSIDERED_NOT_APPLICABLE
// by its own pure trigger predicate. There is no switch on kind here: the
// predicate lives on the typed body and the bindings come from
// [ObligationKindSpec], so adding a kind never edits this function.
func applicableObligations(pack RulePack, proposal PromotionProposalSnapshot, preemptionSets ...[]PreemptionApplied) ([]AppliedObligation, []ConsideredObligation) {
	var preemptions []PreemptionApplied
	if len(preemptionSets) > 0 {
		preemptions = preemptionSets[0]
	}
	var applied []AppliedObligation
	var considered []ConsideredObligation
	for _, o := range pack.obligations() {
		if preempted(pack.Jurisdiction, o.Type, o.Rule.obligationID(), preemptions) {
			continue
		}
		fired, reason := o.Rule.trigger(proposal)
		if !fired {
			considered = append(considered, ConsideredObligation{
				Type:     o.Type,
				ID:       o.Rule.obligationID(),
				Reason:   reason,
				Citation: o.Rule.obligationCitation(),
			})
			continue
		}
		bindings := bindingsFor(o.Type, o.Rule.obligationID(), bindingDescriptions[o.Type])
		if len(bindings) == 0 {
			// A kind with no spec row cannot bind, and an unbindable
			// statutory obligation must not be reported as satisfied. It is
			// recorded as not applicable with that as the reason rather than
			// dropped.
			considered = append(considered, ConsideredObligation{
				Type:     o.Type,
				ID:       o.Rule.obligationID(),
				Reason:   NotApplicableReason("no lifecycle binding is declared for kind " + o.Type.String()),
				Citation: o.Rule.obligationCitation(),
			})
			continue
		}
		applied = append(applied, AppliedObligation{
			Type:         o.Type,
			ID:           o.Rule.obligationID(),
			Description:  o.Rule.describe(),
			Citation:     o.Rule.obligationCitation(),
			Jurisdiction: pack.Jurisdiction,
			PackID:       pack.PackID,
			PackVersion:  pack.Version,
			Binding:      bindings[0],
			Bindings:     bindings,
		})
	}
	return applied, considered
}

func preempted(jurisdiction Jurisdiction, kind ObligationType, id string, records []PreemptionApplied) bool {
	if jurisdiction.Locality == "" {
		return false
	}
	for _, record := range records {
		if record.Kind != kind || record.AssertingJurisdiction.Country != jurisdiction.Country || record.AssertingJurisdiction.State != jurisdiction.State {
			continue
		}
		for _, removed := range record.RemovedObligationIDs {
			if removed == id {
				return true
			}
		}
	}
	return false
}

func timingWord(direction string) string {
	if direction == "BEFORE" {
		return "before"
	}
	return "after"
}
