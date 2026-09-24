package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/industrypack"
)

// IndustryPackLifecycle is the application entry point for tenant-scoped
// industry pack publication and activation.
type IndustryPackLifecycle struct {
	authority    *industrypack.IndustryEntitlementAuthority
	publications industrypack.PublicationStore
	activations  industrypack.IndustryActivationStore
}

var ErrIndustryPackLifecycleUnavailable = errors.New("application: industry pack lifecycle dependencies are required")

func NewIndustryPackLifecycle(authority *industrypack.IndustryEntitlementAuthority, publications industrypack.PublicationStore, activations industrypack.IndustryActivationStore) (*IndustryPackLifecycle, error) {
	if authority == nil || publications == nil || activations == nil {
		return nil, ErrIndustryPackLifecycleUnavailable
	}
	return &IndustryPackLifecycle{authority: authority, publications: publications, activations: activations}, nil
}

func (s *IndustryPackLifecycle) Publish(ctx context.Context, tenantID string, check industrypack.PublicationCheck) (industrypack.PublicationReport, industrypack.ActivationEffects, error) {
	if s == nil {
		return industrypack.PublicationReport{}, industrypack.ActivationEffects{}, ErrIndustryPackLifecycleUnavailable
	}
	return industrypack.PublishChecked(ctx, s.publications, s.authority, tenantID, check)
}

func (s *IndustryPackLifecycle) Activate(ctx context.Context, spec industrypack.IndustryComposition, activation industrypack.ActivationRequest) (industrypack.IndustryResult, industrypack.ActivationReceipt, industrypack.ActivationEffects, error) {
	if s == nil {
		return industrypack.IndustryResult{}, industrypack.ActivationReceipt{}, industrypack.ActivationEffects{}, ErrIndustryPackLifecycleUnavailable
	}
	return industrypack.ActivateIndustry(ctx, s.activations, s.authority, spec, activation)
}
