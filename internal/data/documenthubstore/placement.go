// Official team and channel Docs-tab placement (HUB-014). A placement is a
// deployment to a team or channel scope that additionally names a
// custodian and a review due date, so a pinned chat link can never
// masquerade as official policy: PlaceDocument requires the actor to hold
// MANAGE (on top of Deploy's own DEPLOY check) and refuses an unbound
// custodian or review date. Placement rows never overwrite a prior one;
// each call appends a fresh, separately versioned deployment through the
// normal HUB-009 atomic Deploy path.
package documenthubstore

import (
	"context"
	"errors"
	"time"
)

// ErrPlacementInput is returned when a placement is missing its scope,
// custodian or review due date.
var ErrPlacementInput = errors.New("document placement: scope, custodian and review due date are required")

// PlaceInput names one official team or channel placement: a reviewed
// version deployed into a placement scope with a custodian and review due
// date bound to it.
type PlaceInput struct {
	DocumentID, VersionID, ScopeKind, ScopeID, ActorID, ExpectedLive string
	CustodianID                                                      string
	ReviewDueAt                                                      time.Time
}

// PlaceDocument deploys a reviewed version into a team or channel scope as
// an official placement. The actor must hold MANAGE; Deploy separately
// requires DEPLOY, so placing needs both.
func (s *Store) PlaceDocument(ctx context.Context, tenantID string, in PlaceInput) (Deployment, error) {
	if in.ScopeKind != "placement" || in.ScopeID == "" || in.CustodianID == "" || in.ReviewDueAt.IsZero() {
		return Deployment{}, ErrPlacementInput
	}
	if err := s.Authorize(ctx, tenantID, in.DocumentID, "person", in.ActorID, ActionManage); err != nil {
		return Deployment{}, err
	}
	return s.Deploy(ctx, tenantID, DeployInput{
		DocumentID: in.DocumentID, VersionID: in.VersionID, ScopeKind: in.ScopeKind, ScopeID: in.ScopeID,
		DeployerID: in.ActorID, ExpectedLive: in.ExpectedLive,
		CustodianID: in.CustodianID, ReviewDueAt: in.ReviewDueAt,
	})
}

// CurrentPlacement resolves the official placement live in one team or
// channel scope. It returns ErrNoDeployment when nothing is deployed there,
// and a deployment that resolves but IsOfficialPlacement()==false when the
// scope holds a bare deployment that never went through PlaceDocument.
func (s *Store) CurrentPlacement(ctx context.Context, tenantID, docID, scopeKind, scopeID string) (Deployment, error) {
	return s.ResolveDeployment(ctx, tenantID, docID, scopeKind, scopeID)
}
