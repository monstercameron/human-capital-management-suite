package privacy

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dataclass"
)

// This file implements planning/todos.md PRIV-008: a federal-tax-information
// processing boundary under IRS Publication 1075.
//
// An FTI boundary has four parts, and all four must verify together (see
// [VerifyBoundary]):
//
//  1. Classification: a PRIV-001 processing activity that explicitly names
//     FTI data gains an [FTIClassification] binding it to the FTI key scope.
//     An activity carrying only ordinary employee PII classifies as an
//     error -- FTI must never flow through the ordinary path by default.
//  2. Access: [AuthorizeFTIAccess] grants only with a TRUST-028 envelope key
//     scope bound to FTI, an explicit purpose, and step-up assurance
//     ([trust.AssuranceHigh]) for remote access.
//  3. Disclosure: [FTIDisclosureLog] is an append-only, hash-chained log of
//     every disclosure and redisclosure, each citing its Publication 1075
//     section 9 authority and carrying only a payload digest, never payload.
//  4. Review: [SafeguardReview] is the annual safeguard-review record;
//     Publication 1075 section 9.3 requires it at least yearly, so a review
//     whose next-due date falls more than 366 days after review, or a
//     boundary evaluated past its review's due date, fails closed.

// ErrFTIBlocked is returned when an FTI boundary input cannot prove it
// belongs on the FTI path: a non-FTI activity, an authority-less or
// misaddressed disclosure, a dangling redisclosure, or an out-of-window
// safeguard review.
var ErrFTIBlocked = errors.New("privacy: FTI boundary blocked")

// ErrFTIAccessDenied is returned by [AuthorizeFTIAccess] when the presented
// key scope, assurance, principal or purpose does not satisfy the FTI
// boundary. It is distinct from [ErrFTIBlocked] so callers can separate an
// access decision (audited, appealable) from a malformed boundary input.
var ErrFTIAccessDenied = errors.New("privacy: FTI access denied")

const (
	ftiEvidencePrefix = "ev:privacy:fti:"
	// ftiDataCategory is the inventory category token an activity must
	// carry for [ClassifyFTI] to treat it as FTI processing. The token is
	// explicit on purpose: an activity that never names FTI data stays on
	// the ordinary-PII path, which is exactly the RED failure PRIV-008
	// exists to prevent.
	ftiDataCategory = "FTI"
	// ftiReviewWindow is the maximum safeguard-review interval: Publication
	// 1075 requires the review at least annually, and 366 days keeps the
	// bound leap-year safe.
	ftiReviewWindow = 366 * 24 * time.Hour
)

// requiredFTIKeyScope returns the TRUST-028 envelope key scope FTI access
// must present. It is read from the live government-data policy table --
// the same table that scopes FieldTaxID to FTI -- so a drift between the
// classification overlay and this boundary fails closed here instead of
// silently minting grants under a stale token.
func requiredFTIKeyScope() (string, error) {
	policy, err := dataclass.ClassPolicyFor(dataclass.FTI)
	if err != nil {
		return "", fmt.Errorf("%w: FTI class policy unavailable: %v", ErrFTIBlocked, err)
	}
	if policy.Key == "" {
		return "", fmt.Errorf("%w: FTI class policy names no key scope", ErrFTIBlocked)
	}
	return string(policy.Key), nil
}

// FTIClassification binds one approved PRIV-001 processing activity to the
// FTI boundary: its approved recipient set (the only legal disclosure
// destinations), the required TRUST-028 key scope, and the digest of the
// exact activity snapshot classified.
type FTIClassification struct {
	ActivityID      string   `json:"activity_id"`
	ActivityVersion string   `json:"activity_version"`
	ActivityDigest  string   `json:"activity_digest"`
	KeyScope        string   `json:"key_scope"`
	Recipients      []string `json:"recipients"`
	EvidenceID      string   `json:"evidence_id"`
}

// Validate reports whether c is internally consistent: every field present,
// the key scope equal to the live FTI requirement, and EvidenceID matching
// the classification's own digest. A forged classification that merely
// copies the shape but rewrites the scope or recipients fails here.
func (c FTIClassification) Validate() error {
	if c.ActivityID == "" || c.ActivityVersion == "" {
		return fmt.Errorf("%w: classification names no activity", ErrFTIBlocked)
	}
	if c.ActivityDigest == "" {
		return fmt.Errorf("%w: classification has no activity digest", ErrFTIBlocked)
	}
	required, err := requiredFTIKeyScope()
	if err != nil {
		return err
	}
	if c.KeyScope != required {
		return fmt.Errorf("%w: key scope %q is not the FTI scope %q", ErrFTIBlocked, c.KeyScope, required)
	}
	if len(c.Recipients) == 0 {
		return fmt.Errorf("%w: classification names no approved recipients", ErrFTIBlocked)
	}
	if c.EvidenceID != ftiEvidencePrefix+c.Digest() {
		return fmt.Errorf("%w: classification evidence id does not match its own digest", ErrFTIBlocked)
	}
	return nil
}

// Digest is the canonical content digest of this classification.
func (c FTIClassification) Digest() string {
	dst := appendFields(nil,
		"activity_id", c.ActivityID,
		"activity_version", c.ActivityVersion,
		"activity_digest", c.ActivityDigest,
		"key_scope", c.KeyScope,
	)
	dst = appendStringSlice(dst, "recipient", slices.Clone(c.Recipients))
	return digestHex(dst)
}

// ClassifyFTI classifies activity onto the FTI boundary. The activity must
// validate, must be APPROVED (draft processing of FTI is never an
// authorized boundary), and must explicitly carry the FTI data category --
// an ordinary-PII activity is refused, not silently promoted.
func ClassifyFTI(activity inventory.ProcessingActivity) (FTIClassification, error) {
	if err := activity.Validate(); err != nil {
		return FTIClassification{}, fmt.Errorf("%w: activity invalid: %v", ErrFTIBlocked, err)
	}
	if activity.Status != inventory.StatusApproved {
		return FTIClassification{}, fmt.Errorf("%w: only an APPROVED activity classifies onto the FTI boundary (status %s)", ErrFTIBlocked, activity.Status)
	}
	if !slices.Contains(activity.DataCategories, ftiDataCategory) {
		return FTIClassification{}, fmt.Errorf("%w: activity %q names no FTI data: ordinary-PII path", ErrFTIBlocked, activity.ID)
	}
	required, err := requiredFTIKeyScope()
	if err != nil {
		return FTIClassification{}, err
	}
	activityParts := append([]string{activity.ID, activity.Version}, slices.Sorted(slices.Values(activity.DataCategories))...)
	activityDigest := digestString(activityParts...)
	c := FTIClassification{
		ActivityID:      activity.ID,
		ActivityVersion: activity.Version,
		ActivityDigest:  activityDigest,
		KeyScope:        required,
		Recipients:      slices.Clone(activity.Recipients),
	}
	c.EvidenceID = ftiEvidencePrefix + c.Digest()
	if err := c.Validate(); err != nil {
		return FTIClassification{}, err
	}
	return c, nil
}

// FTIAccessSpec is everything [AuthorizeFTIAccess] decides on.
type FTIAccessSpec struct {
	Classification FTIClassification
	Principal      string
	Assurance      trust.Assurance
	// Remote marks access over an untrusted network: Publication 1075
	// section 4 access control requires stepped-up authentication there.
	Remote   bool
	KeyScope string
	Purpose  string
}

// FTIAccessGrant is one authorized FTI access, bound to the classification,
// principal, purpose, key scope and assurance that authorized it.
type FTIAccessGrant struct {
	ActivityDigest string `json:"activity_digest"`
	Principal      string `json:"principal"`
	Assurance      string `json:"assurance"`
	Remote         bool   `json:"remote"`
	KeyScope       string `json:"key_scope"`
	Purpose        string `json:"purpose"`
	EvidenceID     string `json:"evidence_id"`
}

// Digest is the canonical content digest of this grant.
func (g FTIAccessGrant) Digest() string {
	return digestHex(appendFields(nil,
		"activity_digest", g.ActivityDigest,
		"principal", g.Principal,
		"assurance", g.Assurance,
		"remote", boolString(g.Remote),
		"key_scope", g.KeyScope,
		"purpose", g.Purpose,
	))
}

// AuthorizeFTIAccess grants FTI access only when every boundary condition
// holds: a valid classification, a named principal and purpose, the FTI key
// scope (never the ordinary tenant scope), substantial assurance locally,
// and high (stepped-up) assurance remotely.
func AuthorizeFTIAccess(spec FTIAccessSpec) (FTIAccessGrant, error) {
	deny := func(format string, args ...any) (FTIAccessGrant, error) {
		return FTIAccessGrant{}, fmt.Errorf("%w: %s", ErrFTIAccessDenied, fmt.Sprintf(format, args...))
	}
	if err := spec.Classification.Validate(); err != nil {
		return deny("classification invalid: %v", err)
	}
	if strings.TrimSpace(spec.Principal) == "" {
		return deny("no principal")
	}
	if strings.TrimSpace(spec.Purpose) == "" {
		return deny("no purpose")
	}
	required, err := requiredFTIKeyScope()
	if err != nil {
		return FTIAccessGrant{}, err
	}
	if spec.KeyScope != required {
		return deny("key scope %q is not the FTI scope %q", spec.KeyScope, required)
	}
	floor := trust.AssuranceSubstantial
	if spec.Remote {
		floor = trust.AssuranceHigh
	}
	if !spec.Assurance.AtLeast(floor) {
		return deny("assurance %s does not meet the %s floor (remote=%v)", spec.Assurance, floor, spec.Remote)
	}
	g := FTIAccessGrant{
		ActivityDigest: spec.Classification.ActivityDigest,
		Principal:      spec.Principal,
		Assurance:      spec.Assurance.String(),
		Remote:         spec.Remote,
		KeyScope:       spec.KeyScope,
		Purpose:        spec.Purpose,
	}
	g.EvidenceID = ftiEvidencePrefix + "grant:" + g.Digest()
	return g, nil
}

// FTIDisclosureSpec is one disclosure or redisclosure to append.
type FTIDisclosureSpec struct {
	DisclosureID string
	At           values.Instant
	Recipient    string
	// Authority is the Publication 1075 section 9 disclosure authority
	// (an approval reference). Empty is refused: every FTI movement must
	// cite the authority that allowed it.
	Authority string
	// Redisclosure marks a downstream re-share of previously disclosed
	// FTI; PriorDisclosure must then name the log entry it derives from.
	Redisclosure    bool
	PriorDisclosure string
	// PayloadDigest is the sha256 hex digest of the disclosed payload.
	// Raw payload never enters the log: a non-digest value is refused.
	PayloadDigest string
}

// FTIDisclosure is one chained log entry.
type FTIDisclosure struct {
	DisclosureID    string         `json:"disclosure_id"`
	At              values.Instant `json:"at"`
	Recipient       string         `json:"recipient"`
	Authority       string         `json:"authority"`
	Redisclosure    bool           `json:"redisclosure"`
	PriorDisclosure string         `json:"prior_disclosure,omitempty"`
	PayloadDigest   string         `json:"payload_digest"`
	PrevDigest      string         `json:"prev_digest"`
	Digest          string         `json:"digest"`
}

func (d FTIDisclosure) canonical() []byte {
	sec, nsec := d.At.Unix()
	return appendFields(nil,
		"disclosure_id", d.DisclosureID,
		"at_sec", itoa(sec),
		"at_nsec", itoa(int64(nsec)),
		"recipient", d.Recipient,
		"authority", d.Authority,
		"redisclosure", boolString(d.Redisclosure),
		"prior_disclosure", d.PriorDisclosure,
		"payload_digest", d.PayloadDigest,
		"prev_digest", d.PrevDigest,
	)
}

// FTIDisclosureLog is the append-only disclosure/redisclosure log for one
// classified activity. The zero value is not usable: build it with
// [NewFTIDisclosureLog]. Logs are values, never mutated in place --
// [FTIDisclosureLog.Append] returns the extended copy.
type FTIDisclosureLog struct {
	ActivityDigest string          `json:"activity_digest"`
	Recipients     []string        `json:"recipients"`
	Entries        []FTIDisclosure `json:"entries"`
	HeadDigest     string          `json:"head_digest"`
}

// NewFTIDisclosureLog opens an empty log bound to classification. The
// classification's approved recipient set travels with the log so every
// later append can prove its destination was authorized when the boundary
// was classified -- not merely asserted at disclosure time.
func NewFTIDisclosureLog(classification FTIClassification) FTIDisclosureLog {
	return FTIDisclosureLog{ActivityDigest: classification.ActivityDigest, Recipients: slices.Clone(classification.Recipients)}
}

// isPayloadDigest reports whether s is a raw sha256 hex digest (64 lowercase
// hex chars): the only shape allowed to stand in for disclosed payload.
func isPayloadDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Append records one disclosure, returning the extended log. The disclosure
// must cite its authority, address an approved recipient, carry a payload
// digest (never payload), arrive no earlier than the log head
// (append-only temporal order), and -- when marked a redisclosure -- name a
// prior entry already in this log.
func (l FTIDisclosureLog) Append(spec FTIDisclosureSpec) (FTIDisclosureLog, error) {
	blocked := func(format string, args ...any) (FTIDisclosureLog, error) {
		return FTIDisclosureLog{}, fmt.Errorf("%w: %s", ErrFTIBlocked, fmt.Sprintf(format, args...))
	}
	if l.ActivityDigest == "" {
		return blocked("log is not bound to a classified activity")
	}
	if strings.TrimSpace(spec.DisclosureID) == "" {
		return blocked("disclosure has no id")
	}
	for _, e := range l.Entries {
		if e.DisclosureID == spec.DisclosureID {
			return blocked("duplicate disclosure id %q", spec.DisclosureID)
		}
	}
	if !spec.At.IsSet() {
		return blocked("disclosure %q has no instant", spec.DisclosureID)
	}
	if strings.TrimSpace(spec.Recipient) == "" {
		return blocked("disclosure %q names no recipient", spec.DisclosureID)
	}
	if !slices.Contains(l.Recipients, spec.Recipient) {
		return blocked("disclosure %q recipient %q was never approved for this activity", spec.DisclosureID, spec.Recipient)
	}
	if strings.TrimSpace(spec.Authority) == "" {
		return blocked("disclosure %q cites no Publication 1075 authority", spec.DisclosureID)
	}
	if !isPayloadDigest(spec.PayloadDigest) {
		return blocked("disclosure %q payload is not a digest: raw payload never enters the log", spec.DisclosureID)
	}
	if len(l.Entries) > 0 {
		head := l.Entries[len(l.Entries)-1]
		if spec.At.Before(head.At) {
			return blocked("disclosure %q predates the log head: append-only order violated", spec.DisclosureID)
		}
	}
	entry := FTIDisclosure{
		DisclosureID: spec.DisclosureID, At: spec.At,
		Recipient: spec.Recipient, Authority: spec.Authority,
		Redisclosure: spec.Redisclosure, PriorDisclosure: spec.PriorDisclosure,
		PayloadDigest: spec.PayloadDigest, PrevDigest: l.HeadDigest,
	}
	if entry.Redisclosure {
		if entry.PriorDisclosure == "" {
			return blocked("redisclosure %q names no prior disclosure", spec.DisclosureID)
		}
		found := false
		for _, e := range l.Entries {
			if e.DisclosureID == entry.PriorDisclosure {
				found = true
				break
			}
		}
		if !found {
			return blocked("redisclosure %q cites unknown prior disclosure %q", spec.DisclosureID, entry.PriorDisclosure)
		}
	} else if entry.PriorDisclosure != "" {
		return blocked("disclosure %q is not marked a redisclosure but names a prior disclosure", spec.DisclosureID)
	}
	entry.Digest = digestHex(entry.canonical())
	out := FTIDisclosureLog{
		ActivityDigest: l.ActivityDigest,
		Recipients:     l.Recipients,
		Entries:        append(slices.Clone(l.Entries), entry),
		HeadDigest:     entry.Digest,
	}
	return out, nil
}

// Verify checks the log's chain integrity: every entry's digest recomputes,
// every link binds its predecessor, and the head matches the last entry.
// It does not re-check recipient approval -- approval is enforced at
// [FTIDisclosureLog.Append] time, and Verify proves nothing was rewritten
// since.
func (l FTIDisclosureLog) Verify() error {
	if l.ActivityDigest == "" {
		return fmt.Errorf("%w: log is not bound to a classified activity", ErrFTIBlocked)
	}
	prev := ""
	for i, e := range l.Entries {
		if e.PrevDigest != prev {
			return fmt.Errorf("%w: entry %d (%q) does not chain to its predecessor", ErrFTIBlocked, i, e.DisclosureID)
		}
		if digestHex(e.canonical()) != e.Digest {
			return fmt.Errorf("%w: entry %d (%q) digest does not recompute: tampered", ErrFTIBlocked, i, e.DisclosureID)
		}
		prev = e.Digest
	}
	if l.HeadDigest != prev {
		return fmt.Errorf("%w: head digest does not match the last entry", ErrFTIBlocked)
	}
	return nil
}

// SafeguardReviewSpec is everything [RecordSafeguardReview] records.
type SafeguardReviewSpec struct {
	ReviewID    string
	ActivityRef string
	ReviewedAt  values.Instant
	Reviewer    string
	Findings    string
	NextDue     values.Instant
}

// SafeguardReview is the annual safeguard-review record Publication 1075
// requires: who reviewed, when, what they found, and when the next review
// falls due. Findings is never empty -- a review that found nothing records
// NO_FINDINGS explicitly, so an unwritten review cannot pass as a clean one.
type SafeguardReview struct {
	ReviewID    string         `json:"review_id"`
	ActivityRef string         `json:"activity_ref"`
	ReviewedAt  values.Instant `json:"reviewed_at"`
	Reviewer    string         `json:"reviewer"`
	Findings    string         `json:"findings"`
	NextDue     values.Instant `json:"next_due"`
	EvidenceID  string         `json:"evidence_id"`
}

// Digest is the canonical content digest of this review record.
func (r SafeguardReview) Digest() string {
	revSec, revNsec := r.ReviewedAt.Unix()
	dueSec, dueNsec := r.NextDue.Unix()
	return digestHex(appendFields(nil,
		"review_id", r.ReviewID,
		"activity_ref", r.ActivityRef,
		"reviewed_at_sec", itoa(revSec),
		"reviewed_at_nsec", itoa(int64(revNsec)),
		"reviewer", r.Reviewer,
		"findings", r.Findings,
		"next_due_sec", itoa(dueSec),
		"next_due_nsec", itoa(int64(dueNsec)),
	))
}

// RecordSafeguardReview validates and binds one annual review: every field
// present, NextDue after ReviewedAt but within the annual window. A review
// that schedules its successor beyond the window, or backdates it, is
// refused -- the annual cadence is the control.
func RecordSafeguardReview(spec SafeguardReviewSpec) (SafeguardReview, error) {
	blocked := func(format string, args ...any) (SafeguardReview, error) {
		return SafeguardReview{}, fmt.Errorf("%w: %s", ErrFTIBlocked, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(spec.ReviewID) == "" {
		return blocked("review has no id")
	}
	if strings.TrimSpace(spec.ActivityRef) == "" {
		return blocked("review %q is not bound to a classified activity", spec.ReviewID)
	}
	if !spec.ReviewedAt.IsSet() {
		return blocked("review %q has no reviewed_at", spec.ReviewID)
	}
	if strings.TrimSpace(spec.Reviewer) == "" {
		return blocked("review %q names no reviewer", spec.ReviewID)
	}
	if strings.TrimSpace(spec.Findings) == "" {
		return blocked("review %q records no findings (record NO_FINDINGS explicitly)", spec.ReviewID)
	}
	if !spec.NextDue.IsSet() {
		return blocked("review %q has no next-due date", spec.ReviewID)
	}
	if !spec.NextDue.After(spec.ReviewedAt) {
		return blocked("review %q next-due is not after review", spec.ReviewID)
	}
	if spec.NextDue.Time().After(spec.ReviewedAt.Time().Add(ftiReviewWindow)) {
		return blocked("review %q next-due exceeds the annual safeguard window", spec.ReviewID)
	}
	r := SafeguardReview{
		ReviewID: spec.ReviewID, ActivityRef: spec.ActivityRef,
		ReviewedAt: spec.ReviewedAt, Reviewer: spec.Reviewer,
		Findings: spec.Findings, NextDue: spec.NextDue,
	}
	r.EvidenceID = ftiEvidencePrefix + "review:" + r.Digest()
	return r, nil
}

// ReviewStatus is the timeliness of a safeguard review at one instant.
type ReviewStatus string

// Review timeliness states.
const (
	ReviewCurrent ReviewStatus = "CURRENT"
	ReviewOverdue ReviewStatus = "OVERDUE"
)

// Status reports whether r is still current at `at`. The due instant itself
// is still current (inclusive boundary); anything after it is overdue.
func (r SafeguardReview) Status(at values.Instant) ReviewStatus {
	if at.After(r.NextDue) {
		return ReviewOverdue
	}
	return ReviewCurrent
}

// FTIBoundary is the complete FTI boundary for one classified activity.
type FTIBoundary struct {
	Classification FTIClassification
	Log            FTIDisclosureLog
	Review         SafeguardReview
}

// VerifyBoundary verifies the whole boundary at `at`: a valid
// classification, a verifying disclosure log bound to that classification's
// activity, a review bound to the same activity, and a review still current
// at `at`. Every failure is reported: a boundary that verifies with a
// stale review or a foreign log is no boundary at all.
func VerifyBoundary(b FTIBoundary, at values.Instant) error {
	if err := b.Classification.Validate(); err != nil {
		return fmt.Errorf("%w: classification: %v", ErrFTIBlocked, err)
	}
	if err := b.Log.Verify(); err != nil {
		return fmt.Errorf("%w: disclosure log: %v", ErrFTIBlocked, err)
	}
	if b.Log.ActivityDigest != b.Classification.ActivityDigest {
		return fmt.Errorf("%w: disclosure log is bound to another activity", ErrFTIBlocked)
	}
	if b.Review.ActivityRef != b.Classification.ActivityDigest {
		return fmt.Errorf("%w: safeguard review is bound to another activity", ErrFTIBlocked)
	}
	if b.Review.EvidenceID == "" || b.Review.EvidenceID != ftiEvidencePrefix+"review:"+b.Review.Digest() {
		return fmt.Errorf("%w: safeguard review evidence does not match its digest", ErrFTIBlocked)
	}
	if b.Review.Status(at) != ReviewCurrent {
		return fmt.Errorf("%w: safeguard review is overdue at %s", ErrFTIBlocked, at.String())
	}
	return nil
}

// --- small canonical helpers (this file's own copies) -----------------------

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
