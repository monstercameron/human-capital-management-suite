package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrSelectionInvalid is returned when a rule-pack selection request is
// malformed, when a barred release is asked to evaluate, or when a signed
// selection or rollback plan fails verification. Uncertain inputs do not
// produce this error: they produce a signed UNKNOWN, CONFLICT or
// REVIEW_REQUIRED selection that pins nothing.
var ErrSelectionInvalid = errors.New("legal: rule-pack selection is invalid")

// Rule-pack selection statuses. Only RESOLVED pins releases; every other
// status is a zero-effect verdict that records why nothing was pinned.
const (
	SelectionResolved       = "RESOLVED"
	SelectionUnknown        = "UNKNOWN"
	SelectionConflict       = "CONFLICT"
	SelectionReviewRequired = "REVIEW_REQUIRED"
)

// Pack families. A release's family follows its source type, so government
// law, collective agreements, contracts and company policy are never weighed
// against each other by an unstated rule.
const (
	FamilyGovernment = "government"
	FamilyCollective = "collective"
	FamilyContract   = "contract"
	FamilyCompany    = "company"
)

// FamilyStrategy is the tenant's explicit per-family composition choice.
// There is no default: a present family without a declared strategy routes
// the whole selection to review.
type FamilyStrategy string

// Per-family strategies.
const (
	FamilyStrategyInclude FamilyStrategy = "INCLUDE"
	FamilyStrategyExclude FamilyStrategy = "EXCLUDE"
)

// familyOf maps a release's source type to its pack family. The empty string
// means the release declares no usable source and can never be selected.
func familyOf(source SourceType) string {
	switch source {
	case SourceTypeStatute, SourceTypeRegulation, SourceTypeAgencyGuidance:
		return FamilyGovernment
	case SourceTypeCBA:
		return FamilyCollective
	case SourceTypeContract:
		return FamilyContract
	case SourceTypeCustomerPolicy:
		return FamilyCompany
	}
	return ""
}

// familyRank orders families inside a pinned selection: government law
// first, company policy last. It is a display and digest order, not a
// precedence claim; precedence is decided by evaluation and composition.
func familyRank(family string) int {
	switch family {
	case FamilyGovernment:
		return 0
	case FamilyCollective:
		return 1
	case FamilyContract:
		return 2
	case FamilyCompany:
		return 3
	}
	return 4
}

// CounselApproval is one current, qualified counsel approval covering an
// exact release for a decision scope. Only a matching, unexpired approval
// lets a material interpretation evaluate.
type CounselApproval struct {
	Release  RulePackRelease
	Approver string
	Scope    string
	Expires  values.LocalDate
}

// SelectionRequest is one rule-pack selection question. BusinessDate is the
// transaction's business effective date: selection uses business time, never
// execution time. Replay pins exact historical releases and bypasses window
// and supersession checks; it never bypasses the barred set.
type SelectionRequest struct {
	Set          JurisdictionSet
	BusinessDate values.LocalDate
	KnownAt      values.KnownAt
	// Strategies names the composition choice per family. Every family
	// present among the candidates must be named.
	Strategies map[string]FamilyStrategy
	// Scope is the decision scope counsel approvals must cover, e.g.
	// "promotion-base-pay".
	Scope string
	// ReviewFloor is the tenant's minimum review status. It must be an
	// explicit floor; there is no platform default.
	ReviewFloor ReviewStatus
	// CounselApprovals are the current qualified approvals the tenant holds.
	CounselApprovals []CounselApproval
	// Barred lists quarantined or withdrawn releases that must never
	// evaluate, including under replay.
	Barred []RulePackRelease
	// Replay lists exact historical releases to re-fetch instead of
	// resolving current ones.
	Replay []RulePackRelease
}

// SelectedRelease is one pinned release inside a resolved selection.
type SelectedRelease struct {
	Release      RulePackRelease
	Family       string
	Digest       string
	ReviewStatus ReviewStatus
}

// ExcludedRelease is one candidate release the selection did not pin, with
// the exact reason. Silence about an excluded candidate is a bug, not a "no".
type ExcludedRelease struct {
	Release RulePackRelease
	Family  string
	Reason  string
}

// ReleaseConflict is one set of releases that cannot govern together: two
// versions of the same pack whose effective windows both cover the business
// date. A chain defect is reported, never resolved by order.
type ReleaseConflict struct {
	Jurisdiction Jurisdiction
	PackID       string
	Releases     []RulePackRelease
	Reason       string
}

// CounselScope records the review posture a selection decided under: the
// tenant floor, the decision scope, and the approvals that covered the
// included releases.
type CounselScope struct {
	Floor     ReviewStatus
	Scope     string
	Approvals []CounselApproval
}

// RulePackSelection is the immutable, signed answer to one selection
// question: the pinned jurisdiction-context digest, the ordered releases a
// RESOLVED selection governs under, and the excluded, conflicting and
// counsel evidence for every other outcome.
type RulePackSelection struct {
	JurisdictionContextDigest string
	Primary                   Jurisdiction
	Overlays                  []Jurisdiction
	Confidence                Confidence
	AttributionRule           AttributionRule
	RemoteWorkPolicyApplied   string
	Strategies                map[string]FamilyStrategy
	Ordered                   []SelectedRelease
	Excluded                  []ExcludedRelease
	Conflicts                 []ReleaseConflict
	Counsel                   CounselScope
	BusinessDate              values.LocalDate
	KnownAt                   values.KnownAt
	EvaluatedAt               values.Instant
	Notes                     []string
	Status                    string
	Digest                    string
	Signature                 Signature
}

// SelectRulePacks resolves which rule-pack releases govern the request's
// jurisdiction set as of its business date. Overlapping packs never choose
// implicitly: an undeclared family strategy, a missing or expired counsel
// approval, a below-floor release, a barred release, or an unresolvable
// jurisdiction each produce a signed zero-effect selection instead of a
// guess.
func SelectRulePacks(req SelectionRequest, registry *Registry, signer *Signer, now values.Instant) (RulePackSelection, error) {
	if registry == nil {
		return RulePackSelection{}, fmt.Errorf("%w: no rule-pack registry supplied", ErrSelectionInvalid)
	}
	if signer == nil {
		return RulePackSelection{}, fmt.Errorf("%w: no signer supplied", ErrSelectionInvalid)
	}
	if err := now.Validate(); err != nil {
		return RulePackSelection{}, fmt.Errorf("%w: evaluation clock: %v", ErrSelectionInvalid, err)
	}
	if err := req.validate(); err != nil {
		return RulePackSelection{}, err
	}
	sel := RulePackSelection{
		Primary:                 req.Set.Primary,
		Overlays:                sortedOverlays(req.Set),
		Confidence:              req.Set.Confidence,
		AttributionRule:         req.Set.AttributionRule,
		RemoteWorkPolicyApplied: req.Set.RemoteWorkPolicyApplied,
		Strategies:              cloneStrategies(req.Strategies),
		Counsel:                 CounselScope{Floor: req.ReviewFloor, Scope: req.Scope},
		BusinessDate:            req.BusinessDate,
		KnownAt:                 req.KnownAt,
		EvaluatedAt:             now,
	}
	sel.JurisdictionContextDigest = selectionContextDigest(sel, req.Scope)
	barred := map[RulePackRelease]bool{}
	for _, b := range req.Barred {
		barred[b] = true
	}

	if len(req.Replay) > 0 {
		if err := sel.replay(req, registry, barred); err != nil {
			return RulePackSelection{}, err
		}
		return signSelection(sel, signer)
	}
	sel.resolveFresh(req, registry, barred)
	return signSelection(sel, signer)
}

// validate refuses a malformed selection question.
func (req SelectionRequest) validate() error {
	if err := req.Set.Primary.Validate(); err != nil || req.Set.Primary.State == "" {
		return fmt.Errorf("%w: primary jurisdiction is required", ErrSelectionInvalid)
	}
	for _, overlay := range req.Set.Overlays {
		if err := overlay.Validate(); err != nil {
			return fmt.Errorf("%w: overlay jurisdiction: %v", ErrSelectionInvalid, err)
		}
	}
	if err := req.BusinessDate.Validate(); err != nil {
		return fmt.Errorf("%w: business date: %v", ErrSelectionInvalid, err)
	}
	if err := req.KnownAt.Instant().Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrSelectionInvalid, err)
	}
	if req.Scope == "" {
		return fmt.Errorf("%w: counsel scope is required", ErrSelectionInvalid)
	}
	if !floorable(req.ReviewFloor) {
		return fmt.Errorf("%w: an explicit review floor is required", ErrSelectionInvalid)
	}
	for family, strategy := range req.Strategies {
		switch family {
		case FamilyGovernment, FamilyCollective, FamilyContract, FamilyCompany:
		default:
			return fmt.Errorf("%w: unknown family %q", ErrSelectionInvalid, family)
		}
		switch strategy {
		case FamilyStrategyInclude, FamilyStrategyExclude:
		default:
			return fmt.Errorf("%w: unknown strategy %q for family %q", ErrSelectionInvalid, strategy, family)
		}
	}
	return nil
}

// floorable reports whether the status can serve as a tenant review floor.
func floorable(floor ReviewStatus) bool {
	switch floor {
	case ReviewStatusUnreviewed, ReviewStatusVendorBaseline, ReviewStatusCounselApproved, ReviewStatusCustomerDefined:
		return true
	}
	return false
}

// reviewRank orders review statuses for floor comparison. Negative means the
// status never satisfies any floor: the zero value was never declared, and a
// deliberately-unresolved release blocks evaluation by design.
func reviewRank(status ReviewStatus) int {
	switch status {
	case ReviewStatusUnreviewed:
		return 0
	case ReviewStatusVendorBaseline:
		return 1
	case ReviewStatusCounselApproved:
		return 2
	case ReviewStatusCustomerDefined:
		return 3
	}
	return -1
}

// sortedOverlays returns the set's overlays deduplicated with the primary
// removed, in digest order. Selection output never depends on input order.
func sortedOverlays(set JurisdictionSet) []Jurisdiction {
	seen := map[Jurisdiction]bool{set.Primary: true}
	var out []Jurisdiction
	for _, overlay := range set.Overlays {
		if !seen[overlay] {
			seen[overlay] = true
			out = append(out, overlay)
		}
	}
	slices.SortFunc(out, func(a, b Jurisdiction) int {
		if a.String() != b.String() {
			if a.String() < b.String() {
				return -1
			}
			return 1
		}
		return 0
	})
	return out
}

// cloneStrategies copies the strategy map so later caller mutation cannot
// reach a signed selection.
func cloneStrategies(in map[string]FamilyStrategy) map[string]FamilyStrategy {
	out := make(map[string]FamilyStrategy, len(in))
	for family, strategy := range in {
		out[family] = strategy
	}
	return out
}

// selectionContextDigest pins the jurisdiction decision the resolver made:
// primary, overlays, business and known time, and the decision scope. The
// digest travels inside the signed selection so a later replay can prove
// which jurisdiction question the pins answered.
func selectionContextDigest(sel RulePackSelection, scope string) string {
	var raw []byte
	raw = append(raw, 'R', 'S', '1')
	raw = sel.Primary.canonicalBytes(appendField(raw, "primary", ""))
	for _, overlay := range sel.Overlays {
		raw = overlay.canonicalBytes(appendField(raw, "overlay", ""))
	}
	raw = appendField(raw, "business_date", sel.BusinessDate.String())
	raw = appendField(raw, "known_at", sel.KnownAt.String())
	raw = appendField(raw, "scope", scope)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// replay re-fetches exact historical pins and adjudicates them under the
// same gates as a fresh selection, minus window and supersession checks:
// windows and supersession markers are history, not defects, under replay.
// The barred set still refuses, and an unfetchable pin is UNKNOWN rather
// than an error, because a lost pin is a coverage failure, not a malformed
// question.
func (sel *RulePackSelection) replay(req SelectionRequest, registry *Registry, barred map[RulePackRelease]bool) error {
	seen := map[RulePackRelease]bool{}
	var packs []RulePack
	for _, pin := range req.Replay {
		if seen[pin] {
			continue
		}
		seen[pin] = true
		if barred[pin] {
			return fmt.Errorf("%w: replay of barred release %s v%d is refused", ErrSelectionInvalid, pin.PackID, pin.Version)
		}
		pack, err := registry.GetExact(pin)
		if err != nil {
			sel.Status = SelectionUnknown
			sel.Excluded = append(sel.Excluded, ExcludedRelease{Release: pin, Reason: "pin-unfetchable"})
			return nil
		}
		packs = append(packs, *pack)
	}
	sel.adjudicate(req, packs, barred, true)
	return nil
}

// resolveFresh pins the current releases governing each jurisdiction in the
// set. Exclusions are recorded before conflicts are grouped, so a stale
// release never contributes to a conflict it no longer belongs in.
func (sel *RulePackSelection) resolveFresh(req SelectionRequest, registry *Registry, barred map[RulePackRelease]bool) {
	jurisdictions := append([]Jurisdiction{sel.Primary}, sel.Overlays...)
	unknown := false
	var packs []RulePack
	for _, j := range jurisdictions {
		candidates := registry.LookupAll(j, req.BusinessDate)
		if len(candidates) == 0 {
			unknown = true
			sel.Notes = append(sel.Notes, "no-release-covers-jurisdiction:"+j.String())
			continue
		}
		anyUsable := false
		for _, pack := range candidates {
			if !barred[pack.Release()] {
				anyUsable = true
				break
			}
		}
		if !anyUsable {
			unknown = true
			sel.Notes = append(sel.Notes, "all-releases-barred:"+j.String())
		}
		packs = append(packs, candidates...)
	}
	sel.adjudicate(req, packs, barred, false)
	if unknown {
		sel.Status = SelectionUnknown
		sel.Ordered = nil
	}
}

// adjudicate runs the per-candidate gates over gathered packs and computes
// the selection status. Historical adjudication (replay) skips only the
// supersession check; every other gate still applies.
func (sel *RulePackSelection) adjudicate(req SelectionRequest, packs []RulePack, barred map[RulePackRelease]bool, historical bool) {
	conflict, review := false, false
	usedApprovals := map[CounselApproval]bool{}
	for _, pack := range packs {
		ref := pack.Release()
		family := familyOf(pack.SourceType)
		if barred[ref] {
			sel.Excluded = append(sel.Excluded, ExcludedRelease{Release: ref, Family: family, Reason: "barred"})
			continue
		}
		if reason, ok := gateCandidate(pack, req, usedApprovals, historical); !ok {
			sel.Excluded = append(sel.Excluded, ExcludedRelease{Release: ref, Family: family, Reason: reason})
			if reason != "stale-superseded" && reason != "strategy-excluded" {
				review = true
			}
			continue
		}
		sel.Ordered = append(sel.Ordered, SelectedRelease{
			Release:      ref,
			Family:       family,
			Digest:       packDigest(pack),
			ReviewStatus: pack.ReviewStatus,
		})
	}
	sel.extractConflicts(&conflict)
	for approval := range usedApprovals {
		sel.Counsel.Approvals = append(sel.Counsel.Approvals, approval)
	}
	slices.SortFunc(sel.Counsel.Approvals, func(a, b CounselApproval) int {
		if a.Release.PackID != b.Release.PackID {
			if a.Release.PackID < b.Release.PackID {
				return -1
			}
			return 1
		}
		if a.Approver != b.Approver {
			if a.Approver < b.Approver {
				return -1
			}
			return 1
		}
		return 0
	})
	sel.sortOrdered()
	switch {
	case conflict:
		sel.Status = SelectionConflict
		sel.Ordered = nil
	case review:
		sel.Status = SelectionReviewRequired
		sel.Ordered = nil
	case len(sel.Ordered) == 0:
		sel.Status = SelectionUnknown
		sel.Notes = append(sel.Notes, "no-release-selected")
	default:
		sel.Status = SelectionResolved
	}
}

// gateCandidate applies the per-candidate gates in order: staleness, family
// knowledge, explicit strategy, review floor, blocking review status, and
// counsel currency. The empty reason with true means included. Historical
// adjudication skips only the supersession gate.
func gateCandidate(pack RulePack, req SelectionRequest, usedApprovals map[CounselApproval]bool, historical bool) (string, bool) {
	if pack.SupersededBy != nil && !historical {
		return "stale-superseded", false
	}
	family := familyOf(pack.SourceType)
	if family == "" {
		return "family-unknown", false
	}
	strategy, declared := req.Strategies[family]
	if !declared {
		return "strategy-undeclared", false
	}
	if strategy == FamilyStrategyExclude {
		return "strategy-excluded", false
	}
	if pack.ReviewStatus == ReviewStatusRequiresCustomerCounselConfiguration {
		return "review-status-blocks-evaluation", false
	}
	if reviewRank(pack.ReviewStatus) < reviewRank(req.ReviewFloor) {
		return "below-review-floor", false
	}
	if needsCounsel(pack) {
		approval, ok := currentApproval(pack.Release(), req.CounselApprovals, req.Scope, req.BusinessDate)
		if !ok {
			return "counsel-approval-missing", false
		}
		usedApprovals[approval] = true
	}
	return "", true
}

// needsCounsel reports whether the release needs a current counsel approval
// for a material decision: anything below customer approval, or any rule
// whose own marker admits uncertainty.
func needsCounsel(pack RulePack) bool {
	if pack.ReviewStatus != ReviewStatusCounselApproved && pack.ReviewStatus != ReviewStatusCustomerDefined {
		return true
	}
	for _, item := range pack.obligations() {
		if marker := item.Rule.obligationCitation().ConfidenceMarker; marker == ConfidenceMarkerVerify || marker == ConfidenceMarkerDisputed {
			return true
		}
	}
	return false
}

// currentApproval finds a qualified approval covering exactly this release
// for the decision scope, unexpired as of the business date.
func currentApproval(ref RulePackRelease, approvals []CounselApproval, scope string, date values.LocalDate) (CounselApproval, bool) {
	for _, approval := range approvals {
		if approval.Release == ref && approval.Scope == scope && approval.Approver != "" && approval.Expires.Compare(date) >= 0 {
			return approval, true
		}
	}
	return CounselApproval{}, false
}

// extractConflicts groups included releases by jurisdiction and pack: two
// versions of one pack covering the same date is a chain defect, reported
// with both releases and never resolved by order.
func (sel *RulePackSelection) extractConflicts(conflict *bool) {
	byPack := map[string][]SelectedRelease{}
	order := []string{}
	for _, r := range sel.Ordered {
		key := r.Release.Jurisdiction.String() + "\x00" + r.Release.PackID
		if _, seen := byPack[key]; !seen {
			order = append(order, key)
		}
		byPack[key] = append(byPack[key], r)
	}
	kept := sel.Ordered[:0]
	for _, key := range order {
		group := byPack[key]
		if len(group) < 2 {
			kept = append(kept, group...)
			continue
		}
		*conflict = true
		releases := make([]RulePackRelease, 0, len(group))
		for _, r := range group {
			releases = append(releases, r.Release)
		}
		slices.SortFunc(releases, func(a, b RulePackRelease) int {
			if a.Version != b.Version {
				if a.Version < b.Version {
					return -1
				}
				return 1
			}
			if a.MinorVersion < b.MinorVersion {
				return -1
			}
			if a.MinorVersion > b.MinorVersion {
				return 1
			}
			return 0
		})
		sel.Conflicts = append(sel.Conflicts, ReleaseConflict{
			Jurisdiction: group[0].Release.Jurisdiction,
			PackID:       group[0].Release.PackID,
			Releases:     releases,
			Reason:       "overlapping-effective-window",
		})
	}
	sel.Ordered = kept
	slices.SortFunc(sel.Excluded, func(a, b ExcludedRelease) int {
		if a.Release.PackID != b.Release.PackID {
			if a.Release.PackID < b.Release.PackID {
				return -1
			}
			return 1
		}
		if a.Reason != b.Reason {
			if a.Reason < b.Reason {
				return -1
			}
			return 1
		}
		return 0
	})
}

// sortOrdered pins the deterministic order: primary jurisdiction first, then
// overlays in digest order, then family rank, pack id and version.
func (sel *RulePackSelection) sortOrdered() {
	rank := map[Jurisdiction]int{sel.Primary: 0}
	for i, overlay := range sel.Overlays {
		if _, seen := rank[overlay]; !seen {
			rank[overlay] = i + 1
		}
	}
	slices.SortFunc(sel.Ordered, func(a, b SelectedRelease) int {
		if rank[a.Release.Jurisdiction] != rank[b.Release.Jurisdiction] {
			if rank[a.Release.Jurisdiction] < rank[b.Release.Jurisdiction] {
				return -1
			}
			return 1
		}
		if familyRank(a.Family) != familyRank(b.Family) {
			if familyRank(a.Family) < familyRank(b.Family) {
				return -1
			}
			return 1
		}
		if a.Release.PackID != b.Release.PackID {
			if a.Release.PackID < b.Release.PackID {
				return -1
			}
			return 1
		}
		if a.Release.Version != b.Release.Version {
			if a.Release.Version < b.Release.Version {
				return -1
			}
			return 1
		}
		if a.Release.MinorVersion < b.Release.MinorVersion {
			return -1
		}
		if a.Release.MinorVersion > b.Release.MinorVersion {
			return 1
		}
		return 0
	})
}

// packDigest prefers a release's own signed digest and recomputes it for a
// pack built directly in Go.
func packDigest(pack RulePack) string {
	if pack.Digest != "" {
		return pack.Digest
	}
	return pack.ComputeDigest()
}

// signSelection digests and signs the selection.
func signSelection(sel RulePackSelection, signer *Signer) (RulePackSelection, error) {
	digest, sig, err := signer.SignDigestChecked(sel.CanonicalBytes())
	if err != nil {
		return RulePackSelection{}, fmt.Errorf("%w: signing selection: %v", ErrSelectionInvalid, err)
	}
	sel.Digest, sel.Signature = digest, sig
	return sel, nil
}

// Verify recomputes the selection's digest and checks the embedded
// signature, refusing any result that is semantically incomplete even when
// re-signed. In particular, a non-RESOLVED selection that pins releases
// never verifies: uncertainty is zero-effect by construction and by check.
func (s RulePackSelection) Verify() error { return s.verify(nil) }

// VerifyWithKey behaves like [RulePackSelection.Verify] and additionally
// requires the embedded public key to equal the trusted key.
func (s RulePackSelection) VerifyWithKey(key []byte) error { return s.verify(key) }

func (s RulePackSelection) verify(key []byte) error {
	if s.Primary.Validate() != nil || s.Primary.State == "" {
		return ErrSelectionInvalid
	}
	for _, overlay := range s.Overlays {
		if err := overlay.Validate(); err != nil {
			return ErrSelectionInvalid
		}
	}
	if !knownSelectionStatus(s.Status) {
		return ErrSelectionInvalid
	}
	if s.JurisdictionContextDigest == "" || s.Counsel.Scope == "" || !floorable(s.Counsel.Floor) {
		return ErrSelectionInvalid
	}
	if s.BusinessDate.Validate() != nil || s.KnownAt.Instant().Validate() != nil || s.EvaluatedAt.Validate() != nil {
		return ErrSelectionInvalid
	}
	if s.Status == SelectionResolved && len(s.Ordered) == 0 {
		return ErrSelectionInvalid
	}
	if s.Status != SelectionResolved && len(s.Ordered) != 0 {
		return ErrSelectionInvalid
	}
	for _, r := range s.Ordered {
		if r.Release.PackID == "" || r.Release.Version == 0 || r.Release.Jurisdiction.Validate() != nil || r.Family == "" || r.Digest == "" {
			return ErrSelectionInvalid
		}
	}
	for _, e := range s.Excluded {
		if e.Release.PackID == "" || e.Reason == "" {
			return ErrSelectionInvalid
		}
	}
	for _, c := range s.Conflicts {
		if c.PackID == "" || len(c.Releases) < 2 || c.Reason == "" {
			return ErrSelectionInvalid
		}
	}
	if key != nil && (len(key) != len(s.Signature.PublicKey) || !slices.Equal(key, s.Signature.PublicKey)) {
		return ErrSelectionInvalid
	}
	if err := VerifySignature(s.CanonicalBytes(), s.Digest, s.Signature); err != nil {
		return fmt.Errorf("%w: %v", ErrSelectionInvalid, err)
	}
	return nil
}

// knownSelectionStatus reports whether the status is in the closed
// vocabulary.
func knownSelectionStatus(status string) bool {
	switch status {
	case SelectionResolved, SelectionUnknown, SelectionConflict, SelectionReviewRequired:
		return true
	}
	return false
}

// CanonicalBytes is the deterministic encoding the selection's digest and
// signature cover. The replay pin list is deliberately outside it: replaying
// a selection's own pins reproduces the same decision bytes.
func (s RulePackSelection) CanonicalBytes() []byte {
	var out = []byte{'L', 'S', '7'}
	out = appendField(out, "jurisdiction_context_digest", s.JurisdictionContextDigest)
	out = s.Primary.canonicalBytes(appendField(out, "primary", ""))
	for _, overlay := range s.Overlays {
		out = overlay.canonicalBytes(appendField(out, "overlay", ""))
	}
	out = appendField(out, "confidence", s.Confidence.String())
	out = appendField(out, "attribution_rule", string(s.AttributionRule))
	out = appendField(out, "remote_work_policy", s.RemoteWorkPolicyApplied)
	families := make([]string, 0, len(s.Strategies))
	for family := range s.Strategies {
		families = append(families, family)
	}
	slices.Sort(families)
	for _, family := range families {
		out = appendField(out, "strategy_"+family, string(s.Strategies[family]))
	}
	out = appendUint32Field(out, "ordered_count", uint32(len(s.Ordered)))
	for _, r := range s.Ordered {
		out = appendField(out, "ordered_pack_id", r.Release.PackID)
		out = appendUint32Field(out, "ordered_version", r.Release.Version)
		out = appendUint32Field(out, "ordered_minor", r.Release.MinorVersion)
		out = r.Release.Jurisdiction.canonicalBytes(appendField(out, "ordered_jurisdiction", ""))
		out = appendField(out, "ordered_family", r.Family)
		out = appendField(out, "ordered_digest", r.Digest)
		out = appendField(out, "ordered_review", r.ReviewStatus.String())
	}
	for _, e := range s.Excluded {
		out = appendField(out, "excluded_pack_id", e.Release.PackID)
		out = appendUint32Field(out, "excluded_version", e.Release.Version)
		out = e.Release.Jurisdiction.canonicalBytes(appendField(out, "excluded_jurisdiction", ""))
		out = appendField(out, "excluded_reason", e.Reason)
	}
	for _, c := range s.Conflicts {
		out = appendField(out, "conflict_pack_id", c.PackID)
		out = c.Jurisdiction.canonicalBytes(appendField(out, "conflict_jurisdiction", ""))
		for _, r := range c.Releases {
			out = appendField(out, "conflict_version_pack", r.PackID)
			out = appendUint32Field(out, "conflict_version", r.Version)
		}
		out = appendField(out, "conflict_reason", c.Reason)
	}
	out = appendField(out, "counsel_floor", s.Counsel.Floor.String())
	out = appendField(out, "counsel_scope", s.Counsel.Scope)
	for _, approval := range s.Counsel.Approvals {
		out = appendField(out, "counsel_approver", approval.Approver)
		out = appendField(out, "counsel_release", approval.Release.PackID)
		out = appendUint32Field(out, "counsel_release_version", approval.Release.Version)
	}
	out = appendField(out, "business_date", s.BusinessDate.String())
	out = appendField(out, "known_at", s.KnownAt.String())
	out = appendField(out, "evaluated_at", s.EvaluatedAt.String())
	for _, note := range s.Notes {
		out = appendField(out, "note", note)
	}
	out = appendField(out, "status", s.Status)
	return out
}

// RollbackObligation is one duty a rollback emits: assessing the exposure
// the superseding release created, recomputing what it touched, and getting
// the reverted law reapproved.
type RollbackObligation struct {
	Kind    string
	Detail  string
	Release RulePackRelease
}

// Rollback obligation kinds. All three are always emitted together: a
// rollback without a reapproval is a quiet law change.
const (
	RollbackImpactAssessment = "IMPACT_ASSESSMENT"
	RollbackRecompute        = "RECOMPUTE"
	RollbackReapproval       = "REAPPROVAL"
)

// RollbackPlan is the immutable, signed answer to rolling one pack in a
// verified selection back to an earlier release: the re-pinned selection,
// the in-flight work fenced by the change, and the three obligations the
// rollback emits.
type RollbackPlan struct {
	Selection      RulePackSelection
	Target         RulePackRelease
	FencedInflight []string
	Impact         []RollbackObligation
	Reasons        []string
	EvaluatedAt    values.Instant
	Digest         string
	Signature      Signature
}

// RollbackSelection reverts one pack inside a verified selection to an
// earlier registered release. The target must be registered, must belong to
// the selection's pack and jurisdiction, must be older than the current pin,
// and must not be barred: rolling back onto withdrawn law is refused. The
// re-pinned selection replays the historical release; the plan fences the
// named in-flight evaluations and emits impact, recompute and reapproval
// obligations for the law change.
func RollbackSelection(sel RulePackSelection, target RulePackRelease, inflight []string, barred []RulePackRelease, registry *Registry, signer *Signer, now values.Instant) (RollbackPlan, error) {
	if err := sel.Verify(); err != nil {
		return RollbackPlan{}, fmt.Errorf("%w: selection authority: %v", ErrSelectionInvalid, err)
	}
	if registry == nil {
		return RollbackPlan{}, fmt.Errorf("%w: no rule-pack registry supplied", ErrSelectionInvalid)
	}
	if signer == nil {
		return RollbackPlan{}, fmt.Errorf("%w: no signer supplied", ErrSelectionInvalid)
	}
	if err := now.Validate(); err != nil {
		return RollbackPlan{}, fmt.Errorf("%w: evaluation clock: %v", ErrSelectionInvalid, err)
	}
	if target.PackID == "" || target.Version == 0 || target.Jurisdiction.Validate() != nil {
		return RollbackPlan{}, fmt.Errorf("%w: rollback target is not a release", ErrSelectionInvalid)
	}
	for _, b := range barred {
		if b == target {
			return RollbackPlan{}, fmt.Errorf("%w: rollback revives barred release %s v%d", ErrSelectionInvalid, target.PackID, target.Version)
		}
	}
	current, ok := rollbackCurrent(sel, target)
	if !ok {
		return RollbackPlan{}, fmt.Errorf("%w: target %s v%d is outside the selection", ErrSelectionInvalid, target.PackID, target.Version)
	}
	if target == current {
		return RollbackPlan{}, fmt.Errorf("%w: target %s v%d is already pinned", ErrSelectionInvalid, target.PackID, target.Version)
	}
	if target.Version > current.Version || (target.Version == current.Version && target.MinorVersion >= current.MinorVersion) {
		return RollbackPlan{}, fmt.Errorf("%w: target %s v%d is not older than pinned v%d", ErrSelectionInvalid, target.PackID, target.Version, current.Version)
	}
	if _, err := registry.GetExact(target); err != nil {
		return RollbackPlan{}, fmt.Errorf("%w: rollback target unavailable: %v", ErrSelectionInvalid, err)
	}
	pins := make([]RulePackRelease, 0, len(sel.Ordered))
	for _, r := range sel.Ordered {
		if r.Release.PackID == target.PackID && r.Release.Jurisdiction == target.Jurisdiction {
			pins = append(pins, target)
		} else {
			pins = append(pins, r.Release)
		}
	}
	repinned, err := SelectRulePacks(SelectionRequest{
		Set: JurisdictionSet{
			Primary:                 sel.Primary,
			Overlays:                slices.Clone(sel.Overlays),
			Confidence:              sel.Confidence,
			AttributionRule:         sel.AttributionRule,
			RemoteWorkPolicyApplied: sel.RemoteWorkPolicyApplied,
		},
		BusinessDate:     sel.BusinessDate,
		KnownAt:          sel.KnownAt,
		Strategies:       cloneStrategies(sel.Strategies),
		Scope:            sel.Counsel.Scope,
		ReviewFloor:      sel.Counsel.Floor,
		CounselApprovals: slices.Clone(sel.Counsel.Approvals),
		Barred:           slices.Clone(barred),
		Replay:           pins,
	}, registry, signer, now)
	if err != nil {
		return RollbackPlan{}, err
	}
	repinned.Notes = append(repinned.Notes,
		fmt.Sprintf("rollback-from:%s-v%d", current.PackID, current.Version),
		"rollback-pending-reapproval",
	)
	repinned, err = signSelection(repinned, signer)
	if err != nil {
		return RollbackPlan{}, err
	}
	plan := RollbackPlan{
		Selection:      repinned,
		Target:         target,
		FencedInflight: dedupeStrings(inflight),
		Impact: []RollbackObligation{
			{Kind: RollbackImpactAssessment, Detail: fmt.Sprintf("assess exposure decided under %s v%d after revert to v%d", current.PackID, current.Version, target.Version), Release: target},
			{Kind: RollbackRecompute, Detail: fmt.Sprintf("recompute fenced and future evaluations under %s v%d", target.PackID, target.Version), Release: target},
			{Kind: RollbackReapproval, Detail: fmt.Sprintf("counsel reapproval in scope %s", sel.Counsel.Scope), Release: target},
		},
		Reasons: []string{
			fmt.Sprintf("rollback-from:%s-v%d", current.PackID, current.Version),
			fmt.Sprintf("rollback-to:%s-v%d", target.PackID, target.Version),
		},
		EvaluatedAt: now,
	}
	digest, sig, err := signer.SignDigestChecked(plan.CanonicalBytes())
	if err != nil {
		return RollbackPlan{}, fmt.Errorf("%w: signing rollback plan: %v", ErrSelectionInvalid, err)
	}
	plan.Digest, plan.Signature = digest, sig
	return plan, nil
}

// rollbackCurrent finds the selected release the target would replace: same
// pack in the same jurisdiction.
func rollbackCurrent(sel RulePackSelection, target RulePackRelease) (RulePackRelease, bool) {
	for _, r := range sel.Ordered {
		if r.Release.PackID == target.PackID && r.Release.Jurisdiction == target.Jurisdiction {
			return r.Release, true
		}
	}
	return RulePackRelease{}, false
}

// dedupeStrings copies inflight identifiers in digest order. Fencing names
// the evaluations that must be recomputed, so the list is evidence, not a
// side effect.
func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// Verify recomputes the plan's digest and checks the embedded signature,
// refusing any plan whose selection, target, obligations or reasons are
// incomplete even when re-signed.
func (p RollbackPlan) Verify() error { return p.verify(nil) }

// VerifyWithKey behaves like [RollbackPlan.Verify] and additionally requires
// the embedded public key to equal the trusted key.
func (p RollbackPlan) VerifyWithKey(key []byte) error { return p.verify(key) }

func (p RollbackPlan) verify(key []byte) error {
	if err := p.Selection.Verify(); err != nil {
		return fmt.Errorf("%w: rolled-back selection: %v", ErrSelectionInvalid, err)
	}
	if p.Target.PackID == "" || p.Target.Version == 0 || p.Target.Jurisdiction.Validate() != nil {
		return ErrSelectionInvalid
	}
	kinds := map[string]bool{}
	for _, o := range p.Impact {
		if o.Kind == "" || o.Detail == "" || o.Release.PackID == "" {
			return ErrSelectionInvalid
		}
		kinds[o.Kind] = true
	}
	for _, want := range []string{RollbackImpactAssessment, RollbackRecompute, RollbackReapproval} {
		if !kinds[want] {
			return ErrSelectionInvalid
		}
	}
	for _, fenced := range p.FencedInflight {
		if fenced == "" {
			return ErrSelectionInvalid
		}
	}
	if len(p.Reasons) == 0 || p.EvaluatedAt.Validate() != nil {
		return ErrSelectionInvalid
	}
	if key != nil && (len(key) != len(p.Signature.PublicKey) || !slices.Equal(key, p.Signature.PublicKey)) {
		return ErrSelectionInvalid
	}
	if err := VerifySignature(p.CanonicalBytes(), p.Digest, p.Signature); err != nil {
		return fmt.Errorf("%w: %v", ErrSelectionInvalid, err)
	}
	return nil
}

// CanonicalBytes is the deterministic encoding the plan's digest and
// signature cover.
func (p RollbackPlan) CanonicalBytes() []byte {
	var out = []byte{'L', 'R', '7'}
	out = appendField(out, "target_pack_id", p.Target.PackID)
	out = appendUint32Field(out, "target_version", p.Target.Version)
	out = appendUint32Field(out, "target_minor", p.Target.MinorVersion)
	out = p.Target.Jurisdiction.canonicalBytes(appendField(out, "target_jurisdiction", ""))
	out = appendField(out, "selection_digest", p.Selection.Digest)
	for _, fenced := range p.FencedInflight {
		out = appendField(out, "fenced", fenced)
	}
	for _, o := range p.Impact {
		out = appendField(out, "impact_kind", o.Kind)
		out = appendField(out, "impact_detail", o.Detail)
	}
	for _, reason := range p.Reasons {
		out = appendField(out, "reason", reason)
	}
	out = appendField(out, "evaluated_at", p.EvaluatedAt.String())
	return out
}
