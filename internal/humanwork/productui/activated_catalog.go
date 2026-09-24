package productui

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
)

// CatalogPublicationAuthorizer checks the trusted actor against the tenant's
// catalog publication grant before any immutable revision is stored.
type CatalogPublicationAuthorizer interface {
	AuthorizeCatalogPublication(context.Context, i18n.Scope, string) error
}

// PublishActivatedCatalog validates product semantics, authorizes the
// publisher, stores the immutable revision, then appends its activation event.
// If activation fails, the previous activation remains the serving revision.
func PublishActivatedCatalog(ctx context.Context, store i18n.ActivatedCatalogStore, authorizer CatalogPublicationAuthorizer, scope i18n.Scope, actor string, revision i18n.CatalogRevision) error {
	if store == nil || authorizer == nil {
		return errors.New("productui: catalog store and publication authorizer are required")
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(actor) == "" {
		return errors.New("productui: publisher identity is required")
	}
	if _, ok := productMessages[canonicalProductLocale(revision.Locale)]; !ok {
		return i18n.ErrUnsupportedLocale
	}
	if err := revision.Validate(); err != nil {
		return err
	}
	if !revision.VerifyDigest() {
		return i18n.ErrInvalidRevision
	}
	known := make(map[string]bool)
	for _, key := range ProductCatalogKeys() {
		known[key] = true
	}
	for _, translation := range revision.Translations {
		if !known[translation.Key] || translation.MeaningID != translation.Key {
			return i18n.ErrMeaningChanged
		}
	}
	if err := authorizer.AuthorizeCatalogPublication(ctx, scope, actor); err != nil {
		return err
	}
	if err := store.Publish(ctx, scope, revision); err != nil {
		return err
	}
	return store.Activate(ctx, scope, revision.ID, actor)
}
