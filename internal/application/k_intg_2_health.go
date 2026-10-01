package application

import (
	"fmt"
	"sort"
	"strings"
	"time"

	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/diagnostics"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/health"
)

// projectIntegrationHealth adapts read-only test-bench findings to the shared
// connector-health contract. The projection has no authority side effects.
func projectIntegrationHealth(report diagnostics.Report, now time.Time) (health.Report, error) {
	signals := make([]health.Signal, 0, len(report.Findings))
	for _, finding := range report.Findings {
		capability := ""
		if finding.Capability.Valid() {
			capability = finding.Capability.String()
		}
		workflows := append([]string(nil), finding.Workflows...)
		if len(workflows) == 0 {
			workflows = []string{""}
		}
		for _, workflow := range workflows {
			signals = append(signals, health.Signal{
				Kind: integrationHealthKind(finding.Check), Status: integrationHealthStatus(finding),
				Cause: finding.Detail, Watermark: now, Capability: capability,
				Workflow: strings.TrimSpace(workflow),
			})
		}
	}
	return health.Project(health.Input{TenantID: report.TenantID, ConnectionID: report.ConnectionID, Now: now, Signals: signals})
}

func integrationHealthKind(check diagnostics.Check) health.Kind {
	switch check {
	case diagnostics.CheckAuthentication:
		return health.Authentication
	case diagnostics.CheckScopes, diagnostics.CheckCapability:
		return health.Permission
	case diagnostics.CheckReachability:
		return health.Error
	default:
		return health.Observation
	}
}

func integrationHealthStatus(finding diagnostics.Finding) health.Status {
	switch finding.Status {
	case diagnostics.Pass:
		return health.Healthy
	case diagnostics.Fail:
		if finding.Check == diagnostics.CheckCapability || finding.Check == diagnostics.CheckReachability {
			return health.Degraded
		}
		return health.Incident
	default:
		return health.Unknown
	}
}

func appendIntegrationHealth(diagnostic *integrationv1.ConnectionDiagnostic, report health.Report) {
	if diagnostic == nil {
		return
	}
	diagnostic.Impact = append(diagnostic.Impact, "health_status:"+string(report.Status))
	for _, cause := range report.Causes {
		diagnostic.Impact = append(diagnostic.Impact, "health_cause:"+cause.Code)
	}
	for _, impact := range report.Impacts {
		if impact.Capability != "" {
			diagnostic.Impact = append(diagnostic.Impact, "capability:"+impact.Capability)
		}
		if impact.Workflow != "" {
			diagnostic.AffectedWorkflowRefs = append(diagnostic.AffectedWorkflowRefs, impact.Workflow)
		}
		if !impact.Deadline.IsZero() {
			diagnostic.Impact = append(diagnostic.Impact, fmt.Sprintf("deadline:%s", impact.Deadline.UTC().Format(time.RFC3339Nano)))
		}
	}
	diagnostic.Impact = uniqueSortedStrings(diagnostic.Impact)
	diagnostic.AffectedWorkflowRefs = uniqueSortedStrings(diagnostic.AffectedWorkflowRefs)
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
