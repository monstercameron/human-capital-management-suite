package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrPersonaReviewDenied      = errors.New("application: persona review is not authorized")
	ErrPersonaReviewUnavailable = errors.New("application: persona review evidence issuer is unavailable")
	ErrPersonaReviewInvalid     = errors.New("application: invalid persona review request")
)

// PersonaReviewAuthority authorizes a verified human against the current
// tenant-scoped persona-review grant.
type PersonaReviewAuthority interface {
	AuthorizePersonaReview(context.Context, values.TenantId, string) error
}

// PersonaReviewEvidenceWriter writes evidence using a separately
// credentialed authority connection. It resolves the exact stored profile,
// owner, and currently active reviewer grant again inside the write transaction.
type PersonaReviewEvidenceWriter interface {
	IssuePersonaReview(context.Context, values.TenantId, string, int64, string, string) (agentpersonastore.VerifiedReview, error)
}

// PersonaReviewIssuanceService derives reviewer identity only from the
// authenticated trust context and delegates durable evidence creation.
type PersonaReviewIssuanceService struct {
	authority PersonaReviewAuthority
	writer    PersonaReviewEvidenceWriter
}

// NewPersonaReviewIssuanceService constructs the fail-closed issuance service.
func NewPersonaReviewIssuanceService(authority PersonaReviewAuthority, writer PersonaReviewEvidenceWriter) (*PersonaReviewIssuanceService, error) {
	if authority == nil || writer == nil {
		return nil, ErrPersonaReviewUnavailable
	}
	return &PersonaReviewIssuanceService{authority: authority, writer: writer}, nil
}

// Authorize confirms a current persona-review grant before a caller changes a
// draft into the review queue. Issue repeats this authorization immediately
// before creating evidence.
func (s *PersonaReviewIssuanceService) Authorize(ctx context.Context) error {
	if s == nil || s.authority == nil || s.writer == nil {
		return ErrPersonaReviewUnavailable
	}
	principal, err := personaReviewPrincipal(ctx)
	if err != nil {
		return err
	}
	if err := s.authority.AuthorizePersonaReview(ctx, principal.Tenant(), principal.Subject()); err != nil {
		return fmt.Errorf("%w: %v", ErrPersonaReviewDenied, err)
	}
	return nil
}

// Issue records an approval or rejection for one stored persona version. The
// caller cannot submit a reviewer, grant, or profile digest.
func (s *PersonaReviewIssuanceService) Issue(ctx context.Context, personaID string, version int64, decision string) (agentpersonastore.VerifiedReview, error) {
	if s == nil || s.authority == nil || s.writer == nil {
		return agentpersonastore.VerifiedReview{}, ErrPersonaReviewUnavailable
	}
	if ctx == nil || strings.TrimSpace(personaID) == "" || personaID != strings.TrimSpace(personaID) || version <= 0 || (decision != "APPROVE" && decision != "REJECT") {
		return agentpersonastore.VerifiedReview{}, ErrPersonaReviewInvalid
	}
	principal, err := personaReviewPrincipal(ctx)
	if err != nil {
		return agentpersonastore.VerifiedReview{}, err
	}
	if err := s.Authorize(ctx); err != nil {
		return agentpersonastore.VerifiedReview{}, err
	}
	review, err := s.writer.IssuePersonaReview(ctx, principal.Tenant(), personaID, version, principal.Subject(), decision)
	if err != nil {
		return agentpersonastore.VerifiedReview{}, fmt.Errorf("application: issue persona review evidence: %w", err)
	}
	if review.TenantID != string(principal.Tenant()) || review.PersonaID != personaID || review.PersonaVersion != version || review.ReviewerID != principal.Subject() || review.Permission != "persona:review" || review.Decision != decision || !review.GrantCurrent || strings.TrimSpace(review.ReviewID) == "" || strings.TrimSpace(review.ProfileDigest) == "" || strings.TrimSpace(review.ReviewDigest) == "" {
		return agentpersonastore.VerifiedReview{}, ErrPersonaReviewUnavailable
	}
	return review, nil
}

func personaReviewPrincipal(ctx context.Context) (*trust.Principal, error) {
	if ctx == nil {
		return nil, ErrPersonaReviewDenied
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().Validate() != nil || strings.TrimSpace(principal.Subject()) == "" {
		return nil, ErrPersonaReviewDenied
	}
	return principal, nil
}
