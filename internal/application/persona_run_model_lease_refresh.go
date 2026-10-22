package application

import (
	"context"
	"fmt"
	"time"
)

// personaModelLeaseRefresher issues a fresh credential lease for a request that
// is already built. A credential lease is single use: the egress dispatcher
// consumes it before the provider is called, so a second provider call for the
// same step (the transient retry) needs a lease of its own.
type personaModelLeaseRefresher interface {
	RefreshPersonaRunModelLease(context.Context, *AgentModelExecutorRequest) error
}

// RefreshPersonaRunModelLease mints a new lease with the scope the request was
// built with: its task, provider, destination, purpose and region.
func (s *DatabasePersonaRunModelWorkSource) RefreshPersonaRunModelLease(ctx context.Context, request *AgentModelExecutorRequest) error {
	if s == nil || s.leases == nil || ctx == nil || request == nil {
		return errPersonaRunModelWork
	}
	selection := request.Route.Pin.Primary
	value, err := s.leases.Issue(ctx, ModelLeaseRequest{Task: request.Task, ProviderID: selection.Identity.ProviderID, Destination: selection.ProfileID, Purpose: request.Outbound.Purpose, Region: request.Route.Task.Region, TTL: time.Minute})
	if err != nil {
		return fmt.Errorf("%w: model credential lease unavailable", errPersonaRunModelWork)
	}
	request.Lease = value
	return nil
}

// RefreshPersonaRunModelLease passes the request to the source the fence wraps.
// Issuing a lease is not a security step of the run, so it is not fenced. A
// source that cannot issue leases leaves the request as it was.
func (s fencedPersonaRunModelWorkSource) RefreshPersonaRunModelLease(ctx context.Context, request *AgentModelExecutorRequest) error {
	refresher, ok := s.inner.(personaModelLeaseRefresher)
	if !ok {
		return nil
	}
	return refresher.RefreshPersonaRunModelLease(ctx, request)
}
