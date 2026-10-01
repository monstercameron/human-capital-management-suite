package application

import (
	"context"
	"time"

	customdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/custom"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServedCustomSurface is the application boundary for governed custom-object
// capabilities. It exposes the domain's generated clients, rebuildable
// projection, and version lifecycle without introducing a process-wide
// registry or moving authorization out of the custom domain.
type ServedCustomSurface struct {
	GenerateCapabilities    func(customdomain.CustomObjectDefinition, string) (customdomain.CapabilityManifest, error)
	NewCapabilityClient     func(customdomain.CapabilityManifest, func(context.Context, customdomain.Capability, customdomain.Invocation) (customdomain.CommitReceipt, error)) (*customdomain.CapabilityClient, error)
	NewGRPCCapabilityClient func(customdomain.CapabilityManifest, func(context.Context, customdomain.Capability, customdomain.Invocation) (customdomain.CommitReceipt, error)) (*customdomain.GRPCCapabilityClient, error)
	CheckClientParity       func(customdomain.CapabilityManifest, customdomain.CapabilityManifest) error
	Commit                  func(context.Context, customdomain.EventProjectionPort, customdomain.MutationRequest) (customdomain.CommitReceipt, error)
	Search                  func(context.Context, customdomain.EventProjectionPort, customdomain.SearchRequest) (customdomain.SearchReport, error)
	Rebuild                 func(context.Context, customdomain.EventProjectionPort, values.TenantId, string, string) (customdomain.ProjectionReport, error)
	PlanMigration           func(customdomain.CustomObjectDefinition, customdomain.CustomObjectDefinition, customdomain.MigrationOptions) (customdomain.TypeMigration, error)
	MigrateRecord           func(customdomain.TypeMigration, customdomain.CustomRecordRevision) (customdomain.CustomRecordRevision, error)
	RollbackPlan            func(customdomain.TypeMigration) (customdomain.TypeMigration, error)
	MarkRetired             func(customdomain.TypeMigration, time.Time, string) (customdomain.TypeRetirement, error)
}

// NewServedCustomSurface returns the custom-object capabilities reachable from
// hcmnext serve. All state remains owned by the caller-provided domain port.
func NewServedCustomSurface() ServedCustomSurface {
	return ServedCustomSurface{
		GenerateCapabilities:    customdomain.GenerateCapabilities,
		NewCapabilityClient:     customdomain.NewCapabilityClient,
		NewGRPCCapabilityClient: customdomain.NewGRPCCapabilityClient,
		CheckClientParity:       customdomain.CheckClientParity,
		Commit: func(ctx context.Context, port customdomain.EventProjectionPort, req customdomain.MutationRequest) (customdomain.CommitReceipt, error) {
			return port.Commit(ctx, req)
		},
		Search: func(ctx context.Context, port customdomain.EventProjectionPort, req customdomain.SearchRequest) (customdomain.SearchReport, error) {
			return port.Search(ctx, req)
		},
		Rebuild: func(ctx context.Context, port customdomain.EventProjectionPort, tenant values.TenantId, kind, objectID string) (customdomain.ProjectionReport, error) {
			return port.Rebuild(ctx, tenant, kind, objectID)
		},
		PlanMigration: customdomain.PlanMigration,
		MigrateRecord: customdomain.MigrateRecord,
		RollbackPlan:  customdomain.RollbackPlan,
		MarkRetired:   customdomain.MarkRetired,
	}
}

// Custom returns the custom-object surface exposed by a composed application.
func (a *App) Custom() ServedCustomSurface {
	if a == nil {
		return ServedCustomSurface{}
	}
	return NewServedCustomSurface()
}
