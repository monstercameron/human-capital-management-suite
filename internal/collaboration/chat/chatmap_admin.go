package chat

import "context"

// The workspace settings page for location sharing. A channel's settings are
// read through Policy; the workspace's are read here, by an administrator only,
// because they include where sharing is switched off and why.

// LocationJurisdictionLister reads every country row of a workspace.
type LocationJurisdictionLister interface {
	ListLocationJurisdictions(ctx context.Context, tenant string) ([]LocationJurisdiction, error)
}

// WorkspaceLocationSettings is what the settings page shows.
type WorkspaceLocationSettings struct {
	Policy        LocationPolicy
	Jurisdictions []LocationJurisdiction
}

// WorkspaceSettings returns the workspace's own settings (not narrowed by any
// channel or country) to a workspace administrator.
func (s LiveLocationService) WorkspaceSettings(ctx context.Context, p Principal, tenant string) (WorkspaceLocationSettings, error) {
	l := s.Locations
	if l == nil || l.Repo == nil {
		return WorkspaceLocationSettings{}, ErrLocationUnavailable
	}
	repo, ok := l.Repo.(LocationPolicyRepository)
	if !ok {
		return WorkspaceLocationSettings{}, ErrLocationUnavailable
	}
	if err := l.checkActor(ctx, p, tenant, "workspace"); err != nil {
		return WorkspaceLocationSettings{}, err
	}
	admin := l.adminPort()
	if admin == nil || p.TenantID != tenant || !admin.IsLocationAdmin(ctx, p, tenant, "") {
		return WorkspaceLocationSettings{}, ErrPermissionDenied
	}
	workspace, _, err := repo.ReadLocationPolicy(ctx, tenant, "")
	if err != nil {
		return WorkspaceLocationSettings{}, ErrLocationUnavailable
	}
	out := WorkspaceLocationSettings{Policy: workspace, Jurisdictions: []LocationJurisdiction{}}
	if lister, ok := l.Repo.(LocationJurisdictionLister); ok {
		rows, err := lister.ListLocationJurisdictions(ctx, tenant)
		if err != nil {
			return WorkspaceLocationSettings{}, ErrLocationUnavailable
		}
		out.Jurisdictions = rows
	}
	return out, nil
}
