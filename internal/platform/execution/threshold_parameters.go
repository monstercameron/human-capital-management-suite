package execution

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// ThresholdConsumerID is the governed consumer identity the promotion
// threshold evaluation reads tenant parameters under. Tenant threshold key
// definitions must allowlist this consumer or resolution refuses.
const ThresholdConsumerID = "hcmnext.rules.promotion-thresholds"

// ThresholdParameterReader resolves one tenant's published promotion
// approval thresholds. A nil reader means no tenant parameter substrate is
// composed and resolves to the zero resolution, which answers the reference
// table; callers must treat a nil reader as "unpublished", never as an
// error.
type ThresholdParameterReader interface {
	ResolveThresholds(ctx context.Context, tenant string) (rules.TenantThresholdResolution, error)
}

// ConfigThresholdReader is the production ThresholdParameterReader: it
// reads the finance and executive threshold keys through the governed
// WF-DATA-036/037 substrate. Snapshots supplies the active tenant catalog,
// Revisions the durable value store, Environment the cell's fixed
// environment, and Consumer the allowlisted reader identity.
//
// Neither key published resolves to the zero resolution (reference table).
// Exactly one key published is a half-published override and fails closed:
// a finance cut point without its executive partner must never half-apply.
// The cited definition version is the finance key's; both keys share the
// tenant catalog activation in practice, and the revision pair is fixed at
// the read.
type ConfigThresholdReader struct {
	Snapshots   config.ParameterDefinitionSource
	Revisions   config.ParameterRevisionRepository
	Environment config.ParameterEnvironment
	Consumer    config.Consumer
}

var _ ThresholdParameterReader = (*ConfigThresholdReader)(nil)

// ResolveThresholds implements ThresholdParameterReader.
func (r *ConfigThresholdReader) ResolveThresholds(ctx context.Context, tenant string) (rules.TenantThresholdResolution, error) {
	if r == nil || r.Snapshots == nil || r.Revisions == nil {
		return rules.TenantThresholdResolution{}, fmt.Errorf("platform execution: threshold parameter reader is not composed")
	}
	if strings.TrimSpace(tenant) == "" {
		return rules.TenantThresholdResolution{}, fmt.Errorf("platform execution: threshold resolution needs a tenant")
	}
	if r.Environment != config.EnvironmentSandbox && r.Environment != config.EnvironmentProduction {
		return rules.TenantThresholdResolution{}, fmt.Errorf("platform execution: threshold resolution needs a cell environment")
	}
	if err := ctx.Err(); err != nil {
		return rules.TenantThresholdResolution{}, err
	}
	snapshot, err := r.Snapshots.LoadParameterSnapshot(ctx, tenant)
	if err != nil {
		return rules.TenantThresholdResolution{}, err
	}
	path := config.ParameterScopePath{{Kind: config.ScopeTenant, ID: tenant}}
	decimal := workflow.ValueType{Kind: workflow.KindDecimal}
	finance, err := r.Revisions.ResolveParameterValue(ctx, tenant, snapshot, rules.ThresholdFinanceKey, path, r.Environment, r.Consumer, decimal)
	if err != nil {
		return rules.TenantThresholdResolution{}, err
	}
	executive, err := r.Revisions.ResolveParameterValue(ctx, tenant, snapshot, rules.ThresholdExecutiveKey, path, r.Environment, r.Consumer, decimal)
	if err != nil {
		return rules.TenantThresholdResolution{}, err
	}
	if !finance.Found && !executive.Found {
		return rules.TenantThresholdResolution{}, nil
	}
	if !finance.Found || !executive.Found {
		return rules.TenantThresholdResolution{}, fmt.Errorf("platform execution: tenant %q published a half threshold override", tenant)
	}
	return rules.TenantThresholdResolution{
		FinanceText:       finance.Value,
		ExecutiveText:     executive.Value,
		Revision:          finance.Revision,
		DefinitionVersion: finance.DefinitionVersion,
		Found:             true,
	}, nil
}
