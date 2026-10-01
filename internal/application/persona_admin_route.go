package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrPersonaAdminRouteUnavailable identifies a route that cannot obtain both
// a trusted request principal and an isolated persona-admin client.
var ErrPersonaAdminRouteUnavailable = errors.New("application: persona admin route unavailable")

// PersonaAdminRoute binds the metadata-only PersonaAdminClient to one
// admitted request. The workspace must obtain a new client for every request;
// this value never stores a principal or tenant shared between requests.
type PersonaAdminRoute struct {
	client productui.PersonaAdminClient
}

// NewPersonaAdminRoute creates a route binder over an already composed client.
// A nil client is rejected so an absent isolated store cannot become a
// synthetic or request-supplied data source.
func NewPersonaAdminRoute(client productui.PersonaAdminClient) (PersonaAdminRoute, error) {
	if client == nil {
		return PersonaAdminRoute{}, ErrPersonaAdminRouteUnavailable
	}
	return PersonaAdminRoute{client: client}, nil
}

// ClientForRequest returns a client only when ctx carries a verified human
// principal. Request fields are replaced with that principal before they
// reach the underlying client, so route query values cannot select a tenant or
// principal.
func (r PersonaAdminRoute) ClientForRequest(ctx context.Context) productui.PersonaAdminClient {
	if r.client == nil || ctx == nil {
		return nil
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Subject() == "" || p.Tenant().Validate() != nil || strings.TrimSpace(p.Subject()) != p.Subject() {
		return nil
	}
	return personaAdminRequestClient{client: r.client, ctx: ctx, tenant: string(p.Tenant()), principal: p.Subject()}
}

type personaAdminRequestClient struct {
	client    productui.PersonaAdminClient
	ctx       context.Context
	tenant    string
	principal string
}

func (c personaAdminRequestClient) Snapshot(_ context.Context, _ productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	return c.client.Snapshot(c.ctx, productui.PersonaAdminSnapshotRequest{TenantID: c.tenant, Principal: c.principal})
}

func (c personaAdminRequestClient) Preview(_ context.Context, req productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	return c.client.Preview(c.ctx, req)
}

// Lifecycle operations are intentionally unavailable on the route client.
// Publication writes require their reviewed command service and a separate
// authorization path.
func (c personaAdminRequestClient) RequestReview(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}
func (c personaAdminRequestClient) PublishPersona(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}
func (c personaAdminRequestClient) RollbackPersona(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}
func (c personaAdminRequestClient) SuspendPersona(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}
func (c personaAdminRequestClient) RetirePersona(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}

var _ productui.PersonaAdminClient = personaAdminRequestClient{}
