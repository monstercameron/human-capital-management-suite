package i18n

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidScope     = errors.New("i18n: tenant and product scope are required")
	ErrNoActiveRevision = errors.New("i18n: no active catalog revision")
)

// Scope binds a reviewed catalog to one tenant and one product surface.
type Scope struct{ Tenant, Product string }

func (s Scope) Validate() error {
	if strings.TrimSpace(s.Tenant) == "" || strings.TrimSpace(s.Product) == "" {
		return ErrInvalidScope
	}
	return nil
}

// ActivatedCatalogStore is the durable publication boundary. Implementations
// append immutable revisions and activation records, then recover the latest
// active revision after process restart.
type ActivatedCatalogStore interface {
	Publish(context.Context, Scope, CatalogRevision) error
	Activate(context.Context, Scope, string, string) error
	Active(context.Context, Scope, string, time.Time) (CatalogRevision, error)
}
