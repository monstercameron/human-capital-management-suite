package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

var (
	ErrPrincipalUnauthorized = errors.New("legal pipeline: principal is not authorized for tenant role")
	ErrSignerPrincipal       = errors.New("legal pipeline: signer key does not belong to authorized principal")
)

// PrincipalAuthorizer resolves a tenant-scoped legal role to the exact
// public key allowed to sign for that principal. Implementations must derive
// this mapping from the identity/role authority, not from review evidence.
type PrincipalAuthorizer interface {
	AuthorizeLegalPrincipal(context.Context, string, string, legal.SigningRole) ([]byte, error)
}

// AuthorAuthorized binds the draft's genesis event to a tenant role
// authority before the author signature is accepted into the pipeline.
func AuthorAuthorized(ctx context.Context, tenantID, authorID string, data []byte, signer *legal.Signer, authority PrincipalAuthorizer) (*Pipeline, error) {
	if err := authorizedSigner(ctx, authority, tenantID, authorID, legal.SigningRoleRuleAuthor, signer); err != nil {
		return nil, err
	}
	return Author(data, authorID, signer)
}

func authorizedSigner(ctx context.Context, authority PrincipalAuthorizer, tenantID, principalID string, role legal.SigningRole, signer *legal.Signer) error {
	if authority == nil || tenantID == "" || principalID == "" || signer == nil {
		return ErrPrincipalUnauthorized
	}
	key, err := authority.AuthorizeLegalPrincipal(ctx, tenantID, principalID, role)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPrincipalUnauthorized, err)
	}
	if len(key) != len(signer.PublicKey()) || !bytes.Equal(key, signer.PublicKey()) {
		return ErrSignerPrincipal
	}
	return nil
}

// ReviewAuthorized requires the tenant's role authority to approve the
// reviewer's identity and signing key before the signed REVIEWED event is
// appended. The status selects a closed role vocabulary; the record itself
// is not treated as proof of authorization.
func (p *Pipeline) ReviewAuthorized(ctx context.Context, tenantID, reviewerID string, status legal.ReviewStatus, findings []legal.ReviewFinding, signer *legal.Signer, authority PrincipalAuthorizer) error {
	if p == nil {
		return ErrInvalidTransition
	}
	role := reviewRole(status)
	if status != legal.ReviewStatusVendorBaseline && !requiresCounselSignature(status) {
		return fmt.Errorf("%w: %s", ErrReviewStatus, status)
	}
	if err := authorizedSigner(ctx, authority, tenantID, reviewerID, role, signer); err != nil {
		return err
	}
	if len(p.Events) == 0 || bytes.Equal(signer.PublicKey(), p.Events[0].Signature.PublicKey) {
		return ErrSignerPrincipal
	}
	covered := make(map[string]bool, len(p.Draft.Definition.Obligations))
	known := make(map[string]bool, len(p.Draft.Definition.Obligations))
	for _, obligation := range p.Draft.Definition.Obligations {
		known[obligation.ID] = true
	}
	for _, finding := range findings {
		if finding.ObligationID != "" {
			if !known[finding.ObligationID] {
				return fmt.Errorf("%w: %s", ErrFindingObligation, finding.ObligationID)
			}
			covered[finding.ObligationID] = true
		}
		if strings.TrimSpace(finding.Note) == "" {
			return ErrFindingEvidence
		}
	}
	for _, obligation := range p.Draft.Definition.Obligations {
		if !covered[obligation.ID] {
			return fmt.Errorf("%w: %s", ErrFindingMissing, obligation.ID)
		}
	}
	return p.Review(reviewerID, status, findings, signer)
}

// PublishAuthorized verifies the release publisher and, for customer-counsel
// promotions, the same counsel principal who signed the review event. The
// promotion only reaches the registry after both tenant role checks pass.
func (p *Pipeline) PublishAuthorized(ctx context.Context, tenantID, publisherID string, publisher *legal.Signer, counselID string, counsel *legal.Signer, authority PrincipalAuthorizer, registry *legal.Registry) (legal.PackRelease, error) {
	if p == nil || p.Stage != StageReviewed {
		return legal.PackRelease{}, fmt.Errorf("%w: Publish from stage %s, want %s", ErrInvalidTransition, stageOf(p), StageReviewed)
	}
	if err := p.VerifyChain(); err != nil {
		return legal.PackRelease{}, err
	}
	if err := authorizedSigner(ctx, authority, tenantID, publisherID, legal.SigningRoleReleasePublisher, publisher); err != nil {
		return legal.PackRelease{}, err
	}
	if len(p.Events) < 2 || bytes.Equal(publisher.PublicKey(), p.Events[0].Signature.PublicKey) || bytes.Equal(publisher.PublicKey(), p.Events[1].Signature.PublicKey) {
		return legal.PackRelease{}, ErrSignerPrincipal
	}
	status, err := legal.ParseReviewStatus(p.Draft.Definition.ReviewStatus)
	if err != nil {
		return legal.PackRelease{}, err
	}
	if requiresCounselSignature(status) {
		if counselID != p.ReviewerID {
			return legal.PackRelease{}, ErrNotPublishable
		}
		if err := authorizedSigner(ctx, authority, tenantID, counselID, legal.SigningRoleCustomerCounsel, counsel); err != nil {
			return legal.PackRelease{}, err
		}
		if !bytes.Equal(counsel.PublicKey(), p.Events[1].Signature.PublicKey) {
			return legal.PackRelease{}, ErrSignerPrincipal
		}
	}
	return p.Publish(publisherID, publisher, counselID, counsel, registry)
}

func stageOf(p *Pipeline) Stage {
	if p == nil {
		return "<nil>"
	}
	return p.Stage
}
