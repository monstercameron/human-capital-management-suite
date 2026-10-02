package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const personaBackgroundPollInterval = time.Second
const personaBackgroundTenantBatch = 16

// personaInvocationBackgroundWorkload dispatches only the configured tenant
// set through the runtime's concrete durable worker and current authorities.
func personaInvocationBackgroundWorkload(runtime *PersonaInvocationProductionRuntime, tenants []string, logger personaInvocationLogger) *bootstrap.Workload {
	if runtime == nil || runtime.Background == nil {
		return nil
	}
	scoped := make([]string, 0, len(tenants))
	seen := make(map[string]bool, len(tenants))
	for _, tenant := range tenants {
		if strings.TrimSpace(tenant) != tenant || values.TenantId(tenant).Validate() != nil {
			return nil
		}
		if !seen[tenant] {
			seen[tenant] = true
			scoped = append(scoped, tenant)
		}
	}
	if len(scoped) == 0 {
		return nil
	}
	return &bootstrap.Workload{Name: "persona-invocation-background", Run: func(ctx context.Context) error {
		return runPersonaInvocationBackgroundWorkload(ctx, scoped, logger, runtime.Background.DispatchTenantRecovering)
	}}
}

func runPersonaInvocationBackgroundWorkload(ctx context.Context, tenants []string, logger personaInvocationLogger, dispatch func(context.Context, string, int) error) error {
	if ctx == nil || dispatch == nil {
		return errPersonaInvocationProductionComposition
	}
	ticker := time.NewTicker(personaBackgroundPollInterval)
	defer ticker.Stop()
	for {
		for _, tenant := range tenants {
			if ctx.Err() != nil {
				return nil
			}
			if err := dispatch(ctx, tenant, personaBackgroundTenantBatch); err != nil && ctx.Err() == nil && !isNilPersonaOutputPort(logger) {
				logger.Error("hcmnext.persona_invocation_background_failed", "tenant_id", tenant, "error_type", fmt.Sprintf("%T", err))
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
