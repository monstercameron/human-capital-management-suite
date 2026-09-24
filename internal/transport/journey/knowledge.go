package journey

import (
	"context"
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// SearchKnowledge is the authorized WEB-194 read path. Tenant and role scopes
// come from the admitted principal and current durable assignment snapshot;
// the request supplies only text and locale.
func (s *server) SearchKnowledge(ctx context.Context, req *journeyv1.SearchKnowledgeRequest) (*journeyv1.SearchKnowledgeResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if req == nil || len(req.GetQuery()) > 256 || len(req.GetLocale()) > 16 || strings.TrimSpace(req.GetQuery()) == "" {
		return nil, envelope.New(envelope.CodeInvalidArgument, "journey.knowledge.input", "the request input is not acceptable").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
	}
	snapshot, err := s.roleSnapshot(ctx, principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	if err := s.requireSnapshotServedCall(snapshot, principal, inv, "SearchKnowledge"); err != nil {
		return nil, err
	}
	roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
	if len(roles) == 0 {
		return nil, featureAccessDenied(principal, inv)
	}
	outcome, err := s.deps.Knowledge.SearchDetailed(ctx, app.KnowledgeSearchRequest{
		TenantID: principal.Tenant().String(), Query: req.GetQuery(), Locale: req.GetLocale(),
		Audience: app.KnowledgeAudienceForRoles(roles), Roles: roles, At: s.deps.nowFunc()(),
	})
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable, "journey.knowledge.search_unavailable", "knowledge search is temporarily unavailable").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
	}
	response := &journeyv1.SearchKnowledgeResponse{
		Matches:        make([]*journeyv1.KnowledgeArticleMatch, 0, len(outcome.Matches)),
		EnvelopeDigest: outcome.EnvelopeDigest, SuppressedDigest: outcome.SuppressedDigest,
	}
	for _, result := range outcome.Matches {
		response.Matches = append(response.Matches, &journeyv1.KnowledgeArticleMatch{
			ArticleId: result.ArticleID, Revision: result.Revision, Locale: result.Locale,
			Title: result.Title, Summary: result.Summary, SourceDigest: result.SourceDigest,
			PolicyDigest: result.PolicyDigest, AuthorizationEvidence: result.AuthorizationEvidence, Score: result.Score,
		})
	}
	return response, nil
}

var _ interface {
	SearchKnowledge(context.Context, *journeyv1.SearchKnowledgeRequest) (*journeyv1.SearchKnowledgeResponse, error)
} = (*server)(nil)
