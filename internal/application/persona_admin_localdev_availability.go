package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type personaAdminLocalDevFactory interface {
	ClientForRequest(context.Context) productui.PersonaAdminClient
}

type personaAdminLocalDevAvailability struct {
	inner     personaAdminLocalDevFactory
	available bool
}

func withLocalDevPersonaBootstrapAvailability(inner personaAdminLocalDevFactory, available bool) personaAdminLocalDevFactory {
	if inner == nil {
		return nil
	}
	return personaAdminLocalDevAvailability{inner: inner, available: available}
}

func (f personaAdminLocalDevAvailability) ClientForRequest(ctx context.Context) productui.PersonaAdminClient {
	client := f.inner.ClientForRequest(ctx)
	if client == nil {
		return nil
	}
	return personaAdminLocalDevClient{PersonaAdminClient: client, available: f.available}
}

// Preserve the existing command boundary while decorating metadata only.
func (f personaAdminLocalDevAvailability) ExecutePersonaAdminCommand(ctx context.Context, request productui.PersonaAdminCommandRequest) error {
	if commands, ok := f.inner.(interface {
		ExecutePersonaAdminCommand(context.Context, productui.PersonaAdminCommandRequest) error
	}); ok {
		return commands.ExecutePersonaAdminCommand(ctx, request)
	}
	return ErrPersonaAdminLifecycleUnavailable
}

type personaAdminLocalDevClient struct {
	productui.PersonaAdminClient
	available bool
}

func (c personaAdminLocalDevClient) Snapshot(ctx context.Context, request productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	snapshot, err := c.PersonaAdminClient.Snapshot(ctx, request)
	if err != nil {
		return snapshot, err
	}
	snapshot.LocalDevBootstrapAvailable = c.available && snapshot.Available
	return snapshot, nil
}

func (c personaAdminLocalDevClient) ReviewPersona(id, decision string) error {
	if review, ok := c.PersonaAdminClient.(productui.PersonaAdminReviewClient); ok {
		return review.ReviewPersona(id, decision)
	}
	return ErrPersonaAdminLifecycleUnavailable
}
