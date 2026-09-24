package pipeline

// This file implements LEGAL-015's authoring and review pipeline state
// machine on top of the definition/candidate/release family pipeline.go
// already coordinates. Where pipeline.go's Ingest/Review/Publish check
// separation of duties by hand (an author-equals-reviewer string compare),
// [Pipeline] proves it through [sod.Evaluate]: author, reviewer and
// publisher are the requester, sole approver candidate and executor of one
// separation-of-duties decision, so "any two roles coincide" is a single
// evaluator call away from a stable, testable refusal rather than three
// separate comparisons that could drift out of sync with each other.
//
// Every stage transition appends one [Event] to the pipeline's own hash
// chain: each event's digest folds in the previous event's digest along with
// the stage, the acting principal, their signing role and the digest of
// whatever artifact they signed at that stage (a draft candidate, a
// [legal.ReviewRecord], or a [legal.PackRelease]). [Pipeline.VerifyChain]
// recomputes the whole chain from genesis, so a tampered, reordered, or
// forged-signature event is detectable without trusting the in-memory Stage
// field alone.

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/sod"
)

// Stage is one state in the rule-pack authoring and release state machine.
type Stage string

// Pipeline stages, in the order the contract's section 7.1 runs them.
// PUBLISHED is a fork point: a pipeline there may later reach SUPERSEDED
// (a successor release replaced it) or WITHDRAWN (it was pulled without a
// successor); once either is reached the pipeline accepts no further
// transitions.
const (
	StageAuthored   Stage = "AUTHORED"
	StageReviewed   Stage = "REVIEWED"
	StagePublished  Stage = "PUBLISHED"
	StageSuperseded Stage = "SUPERSEDED"
	StageWithdrawn  Stage = "WITHDRAWN"
)

// Pipeline state-machine errors. All are matchable with errors.Is.
var (
	// ErrInvalidTransition is returned when a stage method is called from a
	// stage the state machine does not allow it from.
	ErrInvalidTransition = errors.New("legal pipeline: invalid stage transition")
	// ErrChainBroken is returned by [Pipeline.VerifyChain] when an event's
	// recorded digest does not match its recomputed content, its signature
	// does not verify, or its prev_digest does not equal the digest of the
	// event before it.
	ErrChainBroken  = errors.New("legal pipeline: event chain does not verify")
	ErrDraftChanged = errors.New("legal pipeline: draft changed after author signature")
)

// Event is one digested, signed transition in a [Pipeline]'s state machine.
type Event struct {
	Stage Stage
	// PrincipalID is the subject who acted at this stage: the author,
	// reviewer, publisher, or whoever withdrew the release.
	PrincipalID string
	Role        legal.SigningRole
	// ArtifactDigest is the digest of whatever this stage put a signature
	// over: the draft candidate's digest at AUTHORED, the
	// [legal.ReviewRecord]'s digest at REVIEWED, the [legal.PackRelease]'s
	// digest at PUBLISHED, the successor release's digest at SUPERSEDED, or
	// a withdrawal record's digest at WITHDRAWN.
	ArtifactDigest string
	// PrevDigest is the previous event's Digest, or "" for the genesis
	// (AUTHORED) event. It is what makes the sequence a hash chain rather
	// than a bag of independently signed events.
	PrevDigest string
	// Digest is this event's own digest: the sha256 of
	// [eventBytes](Stage, PrincipalID, Role, ArtifactDigest, PrevDigest).
	Digest string
	// Signature is PrincipalID's detached ed25519 signature over the same
	// bytes Digest was computed from.
	Signature legal.Signature
}

// appendField mirrors internal/governance/legal/canonical.go's framing:
// a length-prefixed label followed by a length-prefixed value, so that two
// adjacent fields can never be reparsed as a different split.
func appendField(dst []byte, label, value string) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(label)))
	dst = append(dst, label...)
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}

// eventBytes returns the canonical encoding one [Event]'s digest and
// signature cover.
func eventBytes(stage Stage, principalID string, role legal.SigningRole, artifactDigest, prevDigest string) []byte {
	var b []byte
	b = append(b, 0x50, 0x45, 0x31) // "PE1" - Pipeline Event, encoding version 1.
	b = appendField(b, "stage", string(stage))
	b = appendField(b, "principal_id", principalID)
	b = appendField(b, "role", string(role))
	b = appendField(b, "artifact_digest", artifactDigest)
	b = appendField(b, "prev_digest", prevDigest)
	return b
}

// Pipeline runs one rule pack through the authoring and release state
// machine, holding the artifacts each stage produced and the digested event
// chain that records how it got there.
type Pipeline struct {
	Stage Stage

	AuthorID    string
	ReviewerID  string
	PublisherID string

	Draft          Draft
	ReviewRecord   legal.ReviewRecord
	Release        legal.PackRelease
	AuthoredDigest string

	Events []Event
}

// sodEvaluate is [sod.Evaluate] wired for this package's three-principal
// shapes, factored out so every stage's separation check runs the same rule
// id and quorum.
func sodEvaluate(ctx sod.DecisionContext, constraints sod.Constraints) (sod.Result, error) {
	constraints.RuleID = "LEGAL-015-SOD"
	return sod.Evaluate(ctx, constraints, 1)
}

// appendEvent signs eventBytes(stage, principalID, role, artifactDigest,
// prevDigest) with signer, appends the resulting [Event], and advances
// p.Stage. prevDigest is always the chain's current head, computed from
// p.Events rather than taken as a parameter, so a caller can never construct
// an event out of chain order.
func (p *Pipeline) appendEvent(stage Stage, principalID string, role legal.SigningRole, artifactDigest string, signer *legal.Signer) error {
	prev := ""
	if n := len(p.Events); n > 0 {
		prev = p.Events[n-1].Digest
	}
	bytes := eventBytes(stage, principalID, role, artifactDigest, prev)
	digest, sig, err := signer.SignDigestChecked(bytes)
	if err != nil {
		return fmt.Errorf("%w: sign %s event: %w", ErrNotPublishable, stage, err)
	}
	p.Events = append(p.Events, Event{
		Stage:          stage,
		PrincipalID:    principalID,
		Role:           role,
		ArtifactDigest: artifactDigest,
		PrevDigest:     prev,
		Digest:         digest,
		Signature:      sig,
	})
	p.Stage = stage
	return nil
}

// Author starts a pipeline: it ingests data as authorID's draft definition,
// computes the draft candidate's digest exactly as it stands (before any
// reviewer raises its review status, which the contract's section 3.2 folds
// into the digest), and records the genesis AUTHORED event signed by
// authorSigner.
func Author(data []byte, authorID string, authorSigner *legal.Signer) (*Pipeline, error) {
	if authorSigner == nil {
		return nil, fmt.Errorf("%w: author signer is required", ErrNotPublishable)
	}
	d, err := Ingest(data, authorID)
	if err != nil {
		return nil, err
	}
	digest, err := draftDigest(d)
	if err != nil {
		return nil, err
	}
	p := &Pipeline{AuthorID: authorID, Draft: d, AuthoredDigest: digest}
	if err := p.appendEvent(StageAuthored, authorID, legal.SigningRoleRuleAuthor, digest, authorSigner); err != nil {
		return nil, err
	}
	return p, nil
}

// draftDigest computes the digest of the draft's current candidate. It is
// its own function because the same computation is needed both at Author
// time and, with a possibly different declared review status, at Review
// time.
func draftDigest(d Draft) (string, error) {
	c, err := d.Definition.Candidate()
	if err != nil {
		return "", err
	}
	return c.Pack().ComputeDigest(), nil
}

// reviewRole picks the signing role a review at status raises to. Customer
// counsel owns both COUNSEL_APPROVED and CUSTOMER_DEFINED promotions; vendor
// legal review owns VENDOR_BASELINE.
func reviewRole(status legal.ReviewStatus) legal.SigningRole {
	if requiresCounselSignature(status) {
		return legal.SigningRoleCustomerCounsel
	}
	return legal.SigningRoleVendorLegalReviewer
}

// Review raises the pipeline from AUTHORED to REVIEWED. It proves, through
// [sod.Evaluate], that reviewerID differs from the pipeline's author before
// building anything, rejects an uncertain rule reaching COUNSEL_APPROVED
// exactly as [Review] does, records a [legal.ReviewRecord] with findings
// pinned to the raised candidate's digest, and has reviewerSigner sign that
// record's own digest.
func (p *Pipeline) Review(reviewerID string, status legal.ReviewStatus, findings []legal.ReviewFinding, reviewerSigner *legal.Signer) error {
	if p.Stage != StageAuthored {
		return fmt.Errorf("%w: Review from stage %s, want %s", ErrInvalidTransition, p.Stage, StageAuthored)
	}
	if reviewerSigner == nil {
		return fmt.Errorf("%w: reviewer signer is required", ErrNotPublishable)
	}
	if _, err := sodEvaluate(
		sod.DecisionContext{
			Requester: sod.Actor{Subject: p.AuthorID},
			Approvers: []sod.Actor{{Subject: reviewerID}},
		},
		sod.Constraints{RequesterMayNotApprove: true},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrAuthorReviewerSame, err)
	}
	currentDraftDigest, err := draftDigest(p.Draft)
	if err != nil {
		return err
	}
	if currentDraftDigest != p.AuthoredDigest {
		return ErrDraftChanged
	}

	reviewed, err := Review(p.Draft, reviewerID, status)
	if err != nil {
		return err
	}
	knownObligations := make(map[string]struct{}, len(p.Draft.Definition.Obligations))
	for _, obligation := range p.Draft.Definition.Obligations {
		knownObligations[obligation.ID] = struct{}{}
	}
	for _, finding := range findings {
		if finding.ObligationID != "" {
			if _, ok := knownObligations[finding.ObligationID]; !ok {
				return fmt.Errorf("%w: %s", ErrFindingObligation, finding.ObligationID)
			}
		}
		if finding.Severity == legal.FindingSeverityBlocking {
			return ErrBlockingFinding
		}
	}
	pack := reviewed.Candidate.Pack()
	record := legal.ReviewRecord{
		PackDigest: pack.ComputeDigest(),
		AuthorID:   p.AuthorID,
		ReviewerID: reviewerID,
		Status:     status,
		Findings:   findings,
	}
	if err := record.Validate(); err != nil {
		return err
	}

	if err := p.appendEvent(StageReviewed, reviewerID, reviewRole(status), record.ComputeDigest(), reviewerSigner); err != nil {
		return err
	}
	p.ReviewerID = reviewerID
	p.Draft.Definition.ReviewStatus = status.String()
	p.ReviewRecord = record
	return nil
}

// checkThreeDistinctPrincipals proves, through one [sod.Evaluate] call, that
// authorID, reviewerID and publisherID are three distinct principals: the
// contract's section 7.1 rule that "the Release Publisher's signing key is
// not held by the author or by either reviewer". It is shared by [Publish]
// and [PublishSupersession] so the same rule id and constraint set decide
// both.
func checkThreeDistinctPrincipals(authorID, reviewerID, publisherID string) error {
	_, err := sodEvaluate(
		sod.DecisionContext{
			Requester: sod.Actor{Subject: authorID},
			Approvers: []sod.Actor{{Subject: reviewerID}},
			Executor:  sod.Actor{Subject: publisherID},
		},
		sod.Constraints{
			RequesterMayNotApprove: true,
			ExecutorMayNotApprove:  true,
			RequesterMayNotExecute: true,
		},
	)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPublisherReviewer, err)
	}
	return nil
}

// Publish raises the pipeline from REVIEWED to PUBLISHED. It proves the
// three principals are distinct via [checkThreeDistinctPrincipals], signs
// and registers the release exactly as the package-level [Publish] does, and
// records the PUBLISHED event over the release's own digest.
func (p *Pipeline) Publish(publisherID string, publisherSigner *legal.Signer, counselID string, counselSigner *legal.Signer, registry *legal.Registry) (legal.PackRelease, error) {
	if p.Stage != StageReviewed {
		return legal.PackRelease{}, fmt.Errorf("%w: Publish from stage %s, want %s", ErrInvalidTransition, p.Stage, StageReviewed)
	}
	if err := checkThreeDistinctPrincipals(p.AuthorID, p.ReviewerID, publisherID); err != nil {
		return legal.PackRelease{}, err
	}

	c, err := p.Draft.Definition.Candidate()
	if err != nil {
		return legal.PackRelease{}, err
	}
	reviewed := Reviewed{Candidate: c, AuthorID: p.AuthorID, ReviewerID: p.ReviewerID}
	release, err := Publish(reviewed, publisherID, publisherSigner, counselID, counselSigner, registry)
	if err != nil {
		return legal.PackRelease{}, err
	}

	if err := p.appendEvent(StagePublished, publisherID, legal.SigningRoleReleasePublisher, release.ComputeDigest(), publisherSigner); err != nil {
		return legal.PackRelease{}, err
	}
	p.PublisherID = publisherID
	p.Release = release
	return release, nil
}

// PublishSupersession raises the pipeline from REVIEWED to PUBLISHED exactly
// as [Pipeline.Publish] does, except it registers the release as the
// successor to predecessor through the package-level [PublishSupersession]:
// the predecessor's window is closed and the two link through
// Supersedes/SupersededBy (contract section 3.3), instead of a fresh,
// unlinked registration. The draft's definition must already declare a
// matching "supersedes" reference; see [PublishSupersession].
func (p *Pipeline) PublishSupersession(predecessor legal.RulePackRelease, publisherID string, publisherSigner *legal.Signer, counselID string, counselSigner *legal.Signer, registry *legal.Registry) (legal.PackRelease, error) {
	if p.Stage != StageReviewed {
		return legal.PackRelease{}, fmt.Errorf("%w: PublishSupersession from stage %s, want %s", ErrInvalidTransition, p.Stage, StageReviewed)
	}
	if err := checkThreeDistinctPrincipals(p.AuthorID, p.ReviewerID, publisherID); err != nil {
		return legal.PackRelease{}, err
	}

	c, err := p.Draft.Definition.Candidate()
	if err != nil {
		return legal.PackRelease{}, err
	}
	reviewed := Reviewed{Candidate: c, AuthorID: p.AuthorID, ReviewerID: p.ReviewerID}
	release, err := PublishSupersession(reviewed, predecessor, publisherID, publisherSigner, counselID, counselSigner, registry)
	if err != nil {
		return legal.PackRelease{}, err
	}

	if err := p.appendEvent(StagePublished, publisherID, legal.SigningRoleReleasePublisher, release.ComputeDigest(), publisherSigner); err != nil {
		return legal.PackRelease{}, err
	}
	p.PublisherID = publisherID
	p.Release = release
	return release, nil
}

// Supersede records that successor has replaced this pipeline's published
// release. It does not itself call [Registry.Supersede] - the caller does
// that against the shared registry, since that is where the predecessor's
// window closure and the immutability guarantee live (section 3.3) - it only
// logs the SUPERSEDED transition as a digested event once the caller
// confirms the registry-level supersession succeeded.
func (p *Pipeline) Supersede(principalID string, successor legal.PackRelease, signer *legal.Signer) error {
	if p.Stage != StagePublished {
		return fmt.Errorf("%w: Supersede from stage %s, want %s", ErrInvalidTransition, p.Stage, StagePublished)
	}
	if signer == nil {
		return fmt.Errorf("%w: signer is required", ErrNotPublishable)
	}
	return p.appendEvent(StageSuperseded, principalID, legal.SigningRoleReleasePublisher, successor.ComputeDigest(), signer)
}

// withdrawalDigest digests a withdrawal's reason and the release it withdrew,
// so the WITHDRAWN event's ArtifactDigest identifies exactly what was pulled
// and why, without inventing a new canonical-encoding type for one field.
func withdrawalDigest(releaseDigest, reason string) string {
	bytes := appendField(appendField(nil, "release_digest", releaseDigest), "reason", reason)
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:])
}

// Withdraw pulls a published release from further evaluation without a
// successor. Registration itself is immutable (section 3.3: no release is
// ever edited in place), so Withdraw never mutates the registered
// [legal.PackRelease]; it only records, as a digested, signed event, that
// principalID withdrew it and why. A caller that must stop evaluation
// against the release enforces that by checking [Pipeline.Stage] (or an
// equivalent durable record) before evaluating, not by expecting the
// registry entry itself to change shape.
func (p *Pipeline) Withdraw(principalID, reason string, signer *legal.Signer) error {
	if p.Stage != StagePublished {
		return fmt.Errorf("%w: Withdraw from stage %s, want %s", ErrInvalidTransition, p.Stage, StagePublished)
	}
	if signer == nil {
		return fmt.Errorf("%w: signer is required", ErrNotPublishable)
	}
	digest := withdrawalDigest(p.Release.ComputeDigest(), reason)
	return p.appendEvent(StageWithdrawn, principalID, legal.SigningRoleReleasePublisher, digest, signer)
}

// VerifyChain recomputes every event's bytes from its recorded fields,
// checks that its Signature verifies over its Digest, and checks that its
// PrevDigest equals the digest of the event immediately before it. It is
// the read side of the hash chain [appendEvent] builds: a caller that
// receives a serialized [Pipeline] (or one event mutated after the fact)
// can prove the whole history is intact, not just trust the Stage field.
func (p *Pipeline) VerifyChain() error {
	if len(p.Events) == 0 || p.Stage != p.Events[len(p.Events)-1].Stage {
		return fmt.Errorf("%w: empty chain or stage does not match chain head", ErrChainBroken)
	}
	prev := ""
	for i, ev := range p.Events {
		if ev.PrevDigest != prev {
			return fmt.Errorf("%w: event %d (%s) prev_digest %q, want %q", ErrChainBroken, i, ev.Stage, ev.PrevDigest, prev)
		}
		want := eventBytes(ev.Stage, ev.PrincipalID, ev.Role, ev.ArtifactDigest, ev.PrevDigest)
		if err := legal.VerifySignature(want, ev.Digest, ev.Signature); err != nil {
			return fmt.Errorf("%w: event %d (%s): %v", ErrChainBroken, i, ev.Stage, err)
		}
		prev = ev.Digest
	}
	if p.Events[0].Stage != StageAuthored || p.Events[0].PrincipalID != p.AuthorID || p.Events[0].Role != legal.SigningRoleRuleAuthor || p.Events[0].ArtifactDigest != p.AuthoredDigest {
		return fmt.Errorf("%w: authored event does not match author evidence", ErrChainBroken)
	}
	if len(p.Events) == 1 {
		current, err := draftDigest(p.Draft)
		if err != nil || current != p.AuthoredDigest {
			return fmt.Errorf("%w: authored event does not match current draft", ErrChainBroken)
		}
	}
	if len(p.Events) > 1 {
		if p.Events[1].Stage != StageReviewed || p.Events[1].PrincipalID != p.ReviewerID || p.Events[1].Role != reviewRole(p.ReviewRecord.Status) || p.Events[1].ArtifactDigest != p.ReviewRecord.ComputeDigest() {
			return fmt.Errorf("%w: review event does not match review evidence", ErrChainBroken)
		}
		if err := p.ReviewRecord.Validate(); err != nil {
			return fmt.Errorf("%w: review record: %v", ErrChainBroken, err)
		}
		candidate, err := p.Draft.Definition.Candidate()
		definitionStatus, statusErr := legal.ParseReviewStatus(p.Draft.Definition.ReviewStatus)
		if err != nil || statusErr != nil || p.ReviewRecord.PackDigest != candidate.Pack().ComputeDigest() || p.ReviewRecord.AuthorID != p.AuthorID || p.ReviewRecord.ReviewerID != p.ReviewerID || p.ReviewRecord.Status != definitionStatus {
			return fmt.Errorf("%w: review record does not bind the current candidate", ErrChainBroken)
		}
	}
	if len(p.Events) > 2 {
		if p.Events[2].Stage != StagePublished || p.Events[2].PrincipalID != p.PublisherID || p.Events[2].Role != legal.SigningRoleReleasePublisher || p.Events[2].ArtifactDigest != p.Release.ComputeDigest() || p.Release.Digest != p.Events[2].ArtifactDigest || p.Release.ReviewStatus != p.ReviewRecord.Status {
			return fmt.Errorf("%w: publish event does not match release evidence", ErrChainBroken)
		}
		if err := p.Release.Verify(); err != nil {
			return fmt.Errorf("%w: release: %v", ErrChainBroken, err)
		}
		hasPublisher, hasCounsel := false, false
		for _, signature := range p.Release.Signatures {
			hasPublisher = hasPublisher || signature.Role == legal.SigningRoleReleasePublisher
			hasCounsel = hasCounsel || signature.Role == legal.SigningRoleCustomerCounsel
		}
		if !hasPublisher || (requiresCounselSignature(p.Release.ReviewStatus) && !hasCounsel) {
			return fmt.Errorf("%w: required release approval signatures are missing", ErrChainBroken)
		}
	}
	return nil
}

// Explain renders a deterministic, human-readable one-line summary of the
// pipeline's current state: pack id (once known), stage, the three
// principals seen so far, and how many events the chain carries.
func (p *Pipeline) Explain() string {
	packID := "unknown"
	if p.Draft.Definition.PackID != "" {
		packID = p.Draft.Definition.PackID
	}
	return fmt.Sprintf(
		"pipeline pack=%s stage=%s author=%s reviewer=%s publisher=%s events=%d",
		packID, p.Stage, p.AuthorID, p.ReviewerID, p.PublisherID, len(p.Events))
}
