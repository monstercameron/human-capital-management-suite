package application

import (
	"context"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentStartAvailabilityClient struct {
	productui.AgentClient
	starter *agentStarter
}

func (c agentStartAvailabilityClient) Snapshot(ctx context.Context, request productui.AgentSnapshotRequest) (productui.AgentSnapshot, error) {
	snapshot, err := c.AgentClient.Snapshot(ctx, request)
	if err != nil {
		return snapshot, err
	}
	snapshot.StartAvailable = false
	snapshot.StartUnavailableReason = "unavailable"
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || string(p.Tenant()) != request.TenantID || p.Subject() != request.Principal || p.SubjectKind() != trust.SubjectKindHuman || !p.Assurance().AtLeast(trust.AssuranceLow) {
		snapshot.StartUnavailableReason = "denied"
		return snapshot, nil
	}
	s := c.starter
	if s == nil || s.settings == nil || s.platform == nil || s.authority == nil {
		return snapshot, nil
	}
	enabled, err := s.settings.AgentsEnabled(ctx, p.Tenant())
	if err != nil || !enabled {
		snapshot.StartUnavailableReason = "disabled"
		return snapshot, nil
	}
	if !s.model.Available() {
		snapshot.StartUnavailableReason = "model_unavailable"
		return snapshot, nil
	}
	current, err := s.authority.Resolve(p.Subject(), p.Tenant(), agentPurpose, s.now().UTC())
	if err != nil || !current.Active || current.UserID != p.Subject() || current.Authority.Tenant != p.Tenant() || !slices.Contains(current.Authority.Capabilities, agentReadCapabilityID) || !slices.Contains(current.Authority.Purposes, agentPurpose) {
		snapshot.StartUnavailableReason = "denied"
		return snapshot, nil
	}
	snapshot.StartAvailable, snapshot.StartUnavailableReason = true, ""
	return snapshot, nil
}
