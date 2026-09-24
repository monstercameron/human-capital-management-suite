package application

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/health"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	adminpolicy "github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	platformcache "github.com/monstercameron/human-capital-management-suite/internal/platform/cache"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

const storeHealthPath = "/admin/ops/store-health"

type storeHealthProbe interface {
	Run(context.Context, uuid.UUID) health.Snapshot
}

type storeHealthTenantResolver struct{ cfg transport.Config }

func (r storeHealthTenantResolver) ResolveHealthTenant(req *http.Request) (uuid.UUID, bool) {
	principal, _, err := transport.PreAdmit(req.Context(), r.cfg, transport.MapMetadata(req.Header), "/hcmnext.admin.v1.AdminService/GetStoreHealth")
	if err != nil || adminpolicy.RequireOperator(principal) != nil {
		return uuid.Nil, false
	}
	tenantID := string(principal.Tenant())
	if tenantID == "" {
		return uuid.Nil, false
	}
	return pgstore.TenantID(tenantID), true
}

type tenantCachedHealthProbe struct {
	cache platformcache.Store[health.Snapshot]
	probe storeHealthProbe
}

func (p tenantCachedHealthProbe) Run(ctx context.Context, tenant uuid.UUID) health.Snapshot {
	reader, err := platformcache.NewTenantReadCache[health.Snapshot](p.cache, tenant.String(), authz.PolicyVersion, "store-health-v1", "ops-store-health")
	if err != nil {
		return health.Snapshot{Tenant: tenant, State: health.StateUnknown}
	}
	snapshot, err := reader.GetOrLoad("snapshot", func() (health.Snapshot, error) {
		if p.probe == nil {
			return health.Snapshot{Tenant: tenant, State: health.StateUnknown}, nil
		}
		return p.probe.Run(ctx, tenant), nil
	}, func(snapshot health.Snapshot) bool { return snapshot.Tenant == tenant })
	if err != nil {
		return health.Snapshot{Tenant: tenant, State: health.StateUnknown}
	}
	return snapshot
}

func overlayStoreHealth(next http.Handler, cfg transport.Config, probe storeHealthProbe, cache platformcache.Store[health.Snapshot]) http.Handler {
	if probe == nil || cache == nil {
		return next
	}
	endpoint := health.EndpointHandler(tenantCachedHealthProbe{cache: cache, probe: probe}, storeHealthTenantResolver{cfg: cfg})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == storeHealthPath {
			endpoint.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var _ health.TenantResolver = storeHealthTenantResolver{}
var _ health.SnapshotProbe = tenantCachedHealthProbe{}
