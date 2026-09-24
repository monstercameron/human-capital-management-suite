// Package pipeline coordinates the RulePack authoring and release stages.
// It deliberately keeps orchestration separate from the legal domain types:
// definitions are parsed and validated, review is a distinct transition, and
// publication is the only operation that registers a signed release.
package pipeline

import (
	"errors"
	"fmt"
	"strings"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

var (
	ErrAuthorReviewerSame = errors.New("legal pipeline: author and reviewer must be different people")
	ErrCounselUncertain   = errors.New("legal pipeline: counsel cannot approve VERIFY or DISPUTED rules")
	ErrReviewFloor        = errors.New("legal pipeline: REVIEW_STATUS_INSUFFICIENT")
	ErrReviewStatus       = errors.New("legal pipeline: review status cannot be promoted")
	ErrBlockingFinding    = errors.New("legal pipeline: blocking finding prevents promotion")
	ErrFindingObligation  = errors.New("legal pipeline: finding references an unknown obligation")
	ErrFindingMissing     = errors.New("legal pipeline: review finding is required for every obligation")
	ErrFindingEvidence    = errors.New("legal pipeline: review finding requires a substantive note")
	ErrPublisherReviewer  = errors.New("legal pipeline: publisher must be different from reviewer")
	ErrNotPublishable     = errors.New("legal pipeline: candidate is not publishable")
)

// Draft is the parsed, author-owned definition and its author identity.
type Draft struct {
	Definition legal.PackDefinition
	AuthorID   string
}

// Reviewed is a validated candidate with an auditable reviewer identity.
type Reviewed struct {
	Candidate  legal.PackCandidate
	AuthorID   string
	ReviewerID string
}

// Ingest parses and schema-validates a definition. It performs no review and
// never returns an evaluable release.
func Ingest(data []byte, authorID string) (Draft, error) {
	if authorID == "" {
		return Draft{}, fmt.Errorf("%w: author identity is required", legal.ErrPackDefinitionField)
	}
	d, err := legal.LoadPackDefinition(data)
	if err != nil {
		return Draft{}, err
	}
	// Candidate validates typed bodies and citations while preserving the
	// definition as the source of the review transition.
	if _, err := d.Candidate(); err != nil {
		return Draft{}, err
	}
	return Draft{Definition: d, AuthorID: authorID}, nil
}

// Review validates a draft and raises its release status. Reviewer and author
// separation is checked before any candidate is produced. Counsel approval is
// forbidden while any rule remains uncertain or disputed.
func Review(d Draft, reviewerID string, status legal.ReviewStatus) (Reviewed, error) {
	if d.AuthorID == "" || reviewerID == "" {
		return Reviewed{}, ErrAuthorReviewerSame
	}
	if d.AuthorID == reviewerID {
		return Reviewed{}, ErrAuthorReviewerSame
	}
	if status != legal.ReviewStatusVendorBaseline && status != legal.ReviewStatusCounselApproved && status != legal.ReviewStatusCustomerDefined {
		return Reviewed{}, fmt.Errorf("%w: %s", ErrReviewStatus, status)
	}
	if status == legal.ReviewStatusCounselApproved || status == legal.ReviewStatusCustomerDefined {
		for _, o := range d.Definition.Obligations {
			marker := strings.ToUpper(strings.TrimSpace(o.Citation.ConfidenceMarker))
			if marker == "VERIFY" || marker == "DISPUTED" || marker == "" {
				return Reviewed{}, fmt.Errorf("%w: obligation %s", ErrCounselUncertain, o.ID)
			}
		}
	}
	d.Definition.ReviewStatus = status.String()
	c, err := d.Definition.Candidate()
	if err != nil {
		return Reviewed{}, err
	}
	return Reviewed{Candidate: c, AuthorID: d.AuthorID, ReviewerID: reviewerID}, nil
}

// signAndVerify is the signing half of publication, shared by [Publish] and
// [PublishSupersession]: sign the reviewed candidate as publisher, add the
// customer-counsel signature when the release claims COUNSEL_APPROVED or
// CUSTOMER_DEFINED, and
// verify the fully-signed result before either caller registers it. It never
// touches a [legal.Registry]: the two callers differ only in which registry
// call closes the loop (a fresh registration versus a supersession that also
// closes the predecessor's window), and that difference belongs to them.
func signAndVerify(r Reviewed, publisherID string, publisher *legal.Signer, counselID string, counsel *legal.Signer) (legal.PackRelease, error) {
	if publisherID == "" || publisher == nil {
		return legal.PackRelease{}, ErrNotPublishable
	}
	if publisherID == r.ReviewerID {
		return legal.PackRelease{}, ErrPublisherReviewer
	}
	release, err := r.Candidate.Sign(legal.SigningRoleReleasePublisher, publisher)
	if err != nil {
		return legal.PackRelease{}, err
	}
	if requiresCounselSignature(release.ReviewStatus) {
		if counselID == "" || counsel == nil || counselID != r.ReviewerID || counselID == r.AuthorID || counselID == publisherID {
			return legal.PackRelease{}, ErrNotPublishable
		}
		release, err = legal.AddSignature(release, legal.SigningRoleCustomerCounsel, counsel)
		if err != nil {
			return legal.PackRelease{}, err
		}
	}
	if err := release.Verify(); err != nil {
		return legal.PackRelease{}, err
	}
	return release, nil
}

func requiresCounselSignature(status legal.ReviewStatus) bool {
	return status == legal.ReviewStatusCounselApproved || status == legal.ReviewStatusCustomerDefined
}

// Publish signs and registers a reviewed candidate. Vendor baseline requires
// the publisher signature; counsel-approved and customer-defined releases
// also require the reviewer's customer-counsel signature. The returned
// release is immutable by value and is verified before registration.
func Publish(r Reviewed, publisherID string, publisher *legal.Signer, counselID string, counsel *legal.Signer, registry *legal.Registry) (legal.PackRelease, error) {
	if registry == nil {
		return legal.PackRelease{}, ErrNotPublishable
	}
	release, err := signAndVerify(r, publisherID, publisher, counselID, counsel)
	if err != nil {
		return legal.PackRelease{}, err
	}
	if err := registry.Register(release); err != nil {
		return legal.PackRelease{}, err
	}
	return release, nil
}

// PublishSupersession signs a reviewed candidate exactly as [Publish] does,
// then registers it as the successor to predecessor through
// [legal.Registry.Supersede] instead of a fresh [legal.Registry.Register]:
// the predecessor's effective window is closed at the successor's start and
// the two link through Supersedes/SupersededBy, per the contract's section
// 3.3. r.Candidate's own definition must already declare a matching
// Supersedes reference (see [legal.PackDefinition]'s "supersedes" field) so
// that reference is part of the signed digest, not bolted on afterwards.
func PublishSupersession(r Reviewed, predecessor legal.RulePackRelease, publisherID string, publisher *legal.Signer, counselID string, counsel *legal.Signer, registry *legal.Registry) (legal.PackRelease, error) {
	if registry == nil {
		return legal.PackRelease{}, ErrNotPublishable
	}
	release, err := signAndVerify(r, publisherID, publisher, counselID, counsel)
	if err != nil {
		return legal.PackRelease{}, err
	}
	if release.Supersedes == nil || *release.Supersedes != predecessor {
		return legal.PackRelease{}, fmt.Errorf("%w: release does not declare predecessor %s v%d.%d as its supersedes reference",
			ErrNotPublishable, predecessor.PackID, predecessor.Version, predecessor.MinorVersion)
	}
	if err := registry.Supersede(predecessor, release); err != nil {
		return legal.PackRelease{}, err
	}
	return release, nil
}

// MeetsReviewFloor reports whether a release's status satisfies a tenant's
// minimum. Unspecified is never a valid floor; ordering follows the contract's
// assurance progression and treats customer-defined as counsel-approved.
func MeetsReviewFloor(actual, floor legal.ReviewStatus) bool {
	if floor == legal.ReviewStatusUnspecified || actual == legal.ReviewStatusUnspecified {
		return false
	}
	rank := func(s legal.ReviewStatus) int {
		switch s {
		case legal.ReviewStatusUnreviewed:
			return 0
		case legal.ReviewStatusVendorBaseline:
			return 1
		case legal.ReviewStatusRequiresCustomerCounselConfiguration:
			return 1
		case legal.ReviewStatusCounselApproved, legal.ReviewStatusCustomerDefined:
			return 2
		default:
			return -1
		}
	}
	return rank(actual) >= rank(floor)
}

// RequireReviewFloor fails closed when a release is below the tenant floor.
func RequireReviewFloor(release legal.PackRelease, floor legal.ReviewStatus) error {
	if !MeetsReviewFloor(release.ReviewStatus, floor) {
		return fmt.Errorf("%w: release=%s floor=%s", ErrReviewFloor, release.ReviewStatus, floor)
	}
	return nil
}
