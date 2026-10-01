package otel

import (
	"github.com/monstercameron/human-capital-management-suite/internal/platform/observability"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/backends"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/diagnostic"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/queue"
)

// RuntimeContracts is the process-local operational contract composition that
// travels with a serving telemetry provider. Keeping it on the provider makes
// the contracts part of the serving composition rather than library-only
// values. Each process gets a fresh bundle; no package-level registry is
// involved.
type RuntimeContracts struct {
	Backends   *backends.Stack
	Logs       *observability.Registry
	Diagnostic *diagnostic.Controller
	Queue      *queue.Pipeline
}

func newServingContracts() (RuntimeContracts, error) {
	stack, _, err := backends.Default()
	if err != nil {
		return RuntimeContracts{}, err
	}
	logs, err := observability.NewRegistry(observability.Definition{
		Name:    "telemetry.signal",
		Version: observability.SchemaVersion,
		Templates: map[observability.Outcome]string{
			observability.OutcomeSuccess:   "telemetry signal accepted",
			observability.OutcomeFailure:   "telemetry signal failed",
			observability.OutcomePartial:   "telemetry signal partially processed",
			observability.OutcomeUnknown:   "telemetry signal outcome unknown",
			observability.OutcomeDenied:    "telemetry signal denied",
			observability.OutcomeCancelled: "telemetry signal cancelled",
			observability.OutcomeDegraded:  "telemetry signal degraded",
		},
		Severity: map[observability.Outcome]observability.Severity{
			observability.OutcomeSuccess:   observability.SeverityInfo,
			observability.OutcomeFailure:   observability.SeverityError,
			observability.OutcomePartial:   observability.SeverityWarn,
			observability.OutcomeUnknown:   observability.SeverityWarn,
			observability.OutcomeDenied:    observability.SeverityWarn,
			observability.OutcomeCancelled: observability.SeverityInfo,
			observability.OutcomeDegraded:  observability.SeverityWarn,
		},
	})
	if err != nil {
		return RuntimeContracts{}, err
	}
	telemetryQueue, err := queue.New(queue.Config{Capacity: 2048, RetryBudget: 2})
	if err != nil {
		return RuntimeContracts{}, err
	}
	return RuntimeContracts{
		Backends:   stack,
		Logs:       logs,
		Diagnostic: diagnostic.NewController(),
		Queue:      telemetryQueue,
	}, nil
}

// RuntimeContracts returns the contract composition made for this provider.
// The individual contracts retain their own bounded state and are
// intentionally not shared between providers.
func (p *Provider) RuntimeContracts() RuntimeContracts {
	if p == nil {
		return RuntimeContracts{}
	}
	return p.contracts
}
