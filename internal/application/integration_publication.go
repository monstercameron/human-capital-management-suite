package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/integrationregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const IntegrationDefinitionPublishPath = "/v1/integration/connector-definitions"

var (
	errIntegrationPublisherUnavailable = errors.New("integration publication service unavailable")
	errIntegrationPublisherDenied      = errors.New("integration publication requires hcm_admin")
	errIntegrationPublisherInvalid     = errors.New("invalid connector definition")
)

type integrationDefinitionWriter interface {
	Publish(context.Context, integrationregistry.Scope, connectivity.Publication) error
}

type integrationDefinitionPublisher struct {
	store    integrationDefinitionWriter
	tenantID func(string) uuid.UUID
	now      func() time.Time
}

func newIntegrationDefinitionPublisher(store integrationDefinitionWriter, tenantID func(string) uuid.UUID, now func() time.Time) *integrationDefinitionPublisher {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &integrationDefinitionPublisher{store: store, tenantID: tenantID, now: now}
}

func (p *integrationDefinitionPublisher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	principal, ok := trust.FromContext(r.Context())
	if !ok || principal == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if !principal.HasRole("hcm_admin") {
		http.Error(w, "administrator role required", http.StatusForbidden)
		return
	}
	if p == nil || p.store == nil || p.tenantID == nil {
		http.Error(w, errIntegrationPublisherUnavailable.Error(), http.StatusServiceUnavailable)
		return
	}
	if r.Body == nil {
		http.Error(w, errIntegrationPublisherInvalid.Error(), http.StatusBadRequest)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var definition connectivity.ConnectorDefinition
	if err := decoder.Decode(&definition); err != nil {
		http.Error(w, errIntegrationPublisherInvalid.Error(), http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, errIntegrationPublisherInvalid.Error(), http.StatusBadRequest)
		return
	}
	if err := definition.Validate(); err != nil {
		http.Error(w, errIntegrationPublisherInvalid.Error(), http.StatusBadRequest)
		return
	}
	tenantID := p.tenantID(principal.Tenant().String())
	if tenantID == uuid.Nil {
		http.Error(w, errIntegrationPublisherUnavailable.Error(), http.StatusServiceUnavailable)
		return
	}
	publication, err := connectivity.NewRegistry().Publish(definition, connectivity.PublicationMeta{
		PublishedBy: principal.Subject(), PublishedAt: p.now().UTC(),
	})
	if err != nil {
		http.Error(w, errIntegrationPublisherInvalid.Error(), http.StatusBadRequest)
		return
	}
	scope := integrationregistry.Scope{TenantID: tenantID, OrganizationScopeID: strings.TrimSpace(principal.OrganizationScopeID())}
	if err := p.store.Publish(r.Context(), scope, publication); err != nil {
		if errors.Is(err, connectivity.ErrImmutable) {
			http.Error(w, "connector version is immutable", http.StatusConflict)
			return
		}
		http.Error(w, errIntegrationPublisherUnavailable.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(struct {
		ConnectorID string    `json:"connector_id"`
		Version     string    `json:"version"`
		Digest      string    `json:"digest"`
		PublishedBy string    `json:"published_by"`
		PublishedAt time.Time `json:"published_at"`
	}{publication.Definition.ConnectorID, publication.Definition.Version.String(), publication.Digest, publication.PublishedBy, publication.PublishedAt.UTC()})
}
