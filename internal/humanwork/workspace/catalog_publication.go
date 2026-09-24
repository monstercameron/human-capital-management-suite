package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PathCatalogPublication accepts an immutable reviewed catalog revision from
// an authenticated HCM administrator. Tenant and actor identity always come
// from the verified principal, never from the request body.
const PathCatalogPublication = "/workspace/i18n/catalog"

const maxCatalogPublicationBytes = 2 << 20

var errCatalogPublisherDenied = errors.New("workspace: catalog publication denied")

type workspaceCatalogPublisherAuthorizer struct{ roles roleaccess.Store }

func (a workspaceCatalogPublisherAuthorizer) AuthorizeCatalogPublication(ctx context.Context, scope i18n.Scope, actor string) error {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal.Tenant() == "" || string(principal.Tenant()) != scope.Tenant || principal.Subject() != actor {
		return errCatalogPublisherDenied
	}
	roles := principal.Roles()
	if a.roles != nil {
		snapshot, err := a.roles.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
		if err != nil {
			return fmt.Errorf("workspace: load catalog publisher grants: %w", err)
		}
		roles = roleaccess.AssignedRoles(snapshot, principal.Subject(), roles)
	}
	for _, role := range roles {
		if role == productui.RoleHCMAdmin {
			return nil
		}
	}
	return errCatalogPublisherDenied
}

func (h *Handler) publishCatalog(w http.ResponseWriter, r *http.Request) {
	// The dev-only cookie may stand in for a bearer on read routes. A durable
	// catalog write always requires the explicit credential header.
	if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		writeCatalogPublicationError(w, http.StatusUnauthorized, "authenticated bearer credential required")
		return
	}
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	if h.catalogs == nil {
		writeCatalogPublicationError(w, http.StatusServiceUnavailable, "catalog publication is not configured")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCatalogPublicationBytes))
	decoder.DisallowUnknownFields()
	var revision i18n.CatalogRevision
	if err := decoder.Decode(&revision); err != nil {
		writeCatalogPublicationError(w, http.StatusBadRequest, "invalid catalog revision")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeCatalogPublicationError(w, http.StatusBadRequest, "request must contain one catalog revision")
		return
	}
	principal, ok := trust.FromContext(admitted.Context())
	if !ok {
		writeCatalogPublicationError(w, http.StatusUnauthorized, "authenticated principal required")
		return
	}
	scope := i18n.Scope{Tenant: string(principal.Tenant()), Product: "workspace"}
	if err := productui.PublishActivatedCatalog(admitted.Context(), h.catalogs, workspaceCatalogPublisherAuthorizer{roles: h.roleAccess}, scope, principal.Subject(), revision); err != nil {
		if errors.Is(err, errCatalogPublisherDenied) {
			writeCatalogPublicationError(w, http.StatusForbidden, "HCM administrator role required")
			return
		}
		if errors.Is(err, i18n.ErrUnsupportedLocale) || errors.Is(err, i18n.ErrInvalidRevision) || errors.Is(err, i18n.ErrMeaningChanged) || errors.Is(err, i18n.ErrLegalReviewRequired) || errors.Is(err, i18n.ErrInvalidScope) {
			writeCatalogPublicationError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeCatalogPublicationError(w, http.StatusConflict, "catalog revision could not be activated")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(struct {
		Revision string `json:"revision"`
		Locale   string `json:"locale"`
	}{Revision: revision.ID, Locale: revision.Locale})
}

func writeCatalogPublicationError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: message})
}
