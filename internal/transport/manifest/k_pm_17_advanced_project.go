package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// AdvancedProjectManifestVersion is independent from the existing generated
// ProjectService descriptor. It is the reviewed contract for the later board
// methods while their owning delivery slices add wire messages and handlers.
const AdvancedProjectManifestVersion uint32 = 1

type AdvancedBoardCapability string

const (
	AdvancedCapabilityWIP          AdvancedBoardCapability = "WIP"
	AdvancedCapabilityCycles       AdvancedBoardCapability = "CYCLES"
	AdvancedCapabilityDependencies AdvancedBoardCapability = "DEPENDENCIES"
	AdvancedCapabilityMilestones   AdvancedBoardCapability = "MILESTONES"
	AdvancedCapabilityMigrations   AdvancedBoardCapability = "MIGRATIONS"
	AdvancedCapabilityImports      AdvancedBoardCapability = "IMPORTS"
)

type AdvancedProjectEndpoint struct {
	Name                string
	Version             uint32
	Capability          AdvancedBoardCapability
	GRPCProcedure       string
	HTTPMethod          string
	HTTPPath            string
	CapabilityRef       string
	ClassificationRef   string
	ExpectedRevision    bool
	IdempotencyRequired bool
	PaginationPolicy    string
}

type AdvancedProjectManifest struct {
	Version   uint32
	Endpoints []AdvancedProjectEndpoint
}

// AdvancedProjectEndpoints is a deterministic, versioned contract for the
// advanced board surface. Every mutation binds a project capability, exact
// revision, and idempotency key; read pages remain bounded and cursor-safe.
func AdvancedProjectEndpoints() AdvancedProjectManifest {
	rows := []AdvancedProjectEndpoint{
		{"ConfigureWIP", 1, AdvancedCapabilityWIP, "/hcmnext.project.v1.ProjectService/ConfigureWIP", "POST", "/v1/projects/{project}/wip", "hcmnext.project.configure_wip", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
		{"GetFlowMetrics", 1, AdvancedCapabilityWIP, "/hcmnext.project.v1.ProjectService/GetFlowMetrics", "GET", "/v1/projects/{project}/flow-metrics", "hcmnext.project.read_task", "CONFIDENTIAL_HR", false, false, "OPAQUE_CURSOR_BOUNDED"},
		{"CreateCycle", 1, AdvancedCapabilityCycles, "/hcmnext.project.v1.ProjectService/CreateCycle", "POST", "/v1/projects/{project}/cycles", "hcmnext.project.manage_cycles", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
		{"StartCycle", 1, AdvancedCapabilityCycles, "/hcmnext.project.v1.ProjectService/StartCycle", "POST", "/v1/projects/{project}/cycles/{cycle}:start", "hcmnext.project.manage_cycles", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
		{"CloseCycle", 1, AdvancedCapabilityCycles, "/hcmnext.project.v1.ProjectService/CloseCycle", "POST", "/v1/projects/{project}/cycles/{cycle}:close", "hcmnext.project.manage_cycles", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
		{"AddDependency", 1, AdvancedCapabilityDependencies, "/hcmnext.project.v1.ProjectService/AddDependency", "POST", "/v1/projects/{project}/dependencies", "hcmnext.project.edit_task", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
		{"RemoveDependency", 1, AdvancedCapabilityDependencies, "/hcmnext.project.v1.ProjectService/RemoveDependency", "DELETE", "/v1/projects/{project}/dependencies/{dependency}", "hcmnext.project.edit_task", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
		{"ListDependencies", 1, AdvancedCapabilityDependencies, "/hcmnext.project.v1.ProjectService/ListDependencies", "GET", "/v1/projects/{project}/dependencies", "hcmnext.project.read_task", "CONFIDENTIAL_HR", false, false, "OPAQUE_CURSOR_BOUNDED"},
		{"CreateMilestone", 1, AdvancedCapabilityMilestones, "/hcmnext.project.v1.ProjectService/CreateMilestone", "POST", "/v1/projects/{project}/milestones", "hcmnext.project.manage_milestones", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
		{"CompleteMilestone", 1, AdvancedCapabilityMilestones, "/hcmnext.project.v1.ProjectService/CompleteMilestone", "POST", "/v1/projects/{project}/milestones/{milestone}:complete", "hcmnext.project.manage_milestones", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
		{"PreviewMigration", 1, AdvancedCapabilityMigrations, "/hcmnext.project.v1.ProjectService/PreviewMigration", "POST", "/v1/projects/{project}/migrations:preview", "hcmnext.project.configure_workflow", "CONFIDENTIAL_HR", true, false, "NOT_APPLICABLE"},
		{"PublishMigration", 1, AdvancedCapabilityMigrations, "/hcmnext.project.v1.ProjectService/PublishMigration", "POST", "/v1/projects/{project}/migrations:publish", "hcmnext.project.publish_configuration", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
		{"GetMigrationStatus", 1, AdvancedCapabilityMigrations, "/hcmnext.project.v1.ProjectService/GetMigrationStatus", "GET", "/v1/projects/{project}/migrations/{migration}", "hcmnext.project.read_project", "CONFIDENTIAL_HR", false, false, "NOT_APPLICABLE"},
		{"PreviewImport", 1, AdvancedCapabilityImports, "/hcmnext.project.v1.ProjectService/PreviewImport", "POST", "/v1/projects/{project}/imports:preview", "hcmnext.project.configure_workflow", "CONFIDENTIAL_HR", true, false, "NOT_APPLICABLE"},
		{"CommitImport", 1, AdvancedCapabilityImports, "/hcmnext.project.v1.ProjectService/CommitImport", "POST", "/v1/projects/{project}/imports:commit", "hcmnext.project.edit_task", "CONFIDENTIAL_HR", true, true, "NOT_APPLICABLE"},
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].GRPCProcedure < rows[j].GRPCProcedure })
	return AdvancedProjectManifest{Version: AdvancedProjectManifestVersion, Endpoints: rows}
}

// ValidateAdvancedProjectManifest catches a contract row that would expose a
// capability without the common project scope, classification, or revision
// and retry fences. It is intentionally pure so generated transport parity
// tests can call it after the owning RPCs are introduced.
func ValidateAdvancedProjectManifest(m AdvancedProjectManifest) error {
	if m.Version != AdvancedProjectManifestVersion || len(m.Endpoints) == 0 {
		return fmt.Errorf("advanced project manifest: invalid version or empty endpoint set")
	}
	seen := make(map[string]bool, len(m.Endpoints))
	for _, endpoint := range m.Endpoints {
		if endpoint.Version != m.Version || endpoint.Name == "" || seen[endpoint.GRPCProcedure] || !validAdvancedCapability(endpoint.Capability) || !strings.HasPrefix(endpoint.GRPCProcedure, "/hcmnext.project.v1.ProjectService/") || endpoint.HTTPMethod == "" || !strings.HasPrefix(endpoint.HTTPPath, "/v1/projects/") || !strings.HasPrefix(endpoint.CapabilityRef, "hcmnext.project.") || endpoint.ClassificationRef == "" {
			return fmt.Errorf("advanced project manifest: invalid endpoint %q", endpoint.Name)
		}
		if endpoint.ExpectedRevision && endpoint.IdempotencyRequired == false && endpoint.Capability != AdvancedCapabilityMigrations && endpoint.Capability != AdvancedCapabilityImports {
			return fmt.Errorf("advanced project manifest: mutation %q lacks idempotency", endpoint.Name)
		}
		seen[endpoint.GRPCProcedure] = true
	}
	return nil
}

func validAdvancedCapability(capability AdvancedBoardCapability) bool {
	switch capability {
	case AdvancedCapabilityWIP, AdvancedCapabilityCycles, AdvancedCapabilityDependencies, AdvancedCapabilityMilestones, AdvancedCapabilityMigrations, AdvancedCapabilityImports:
		return true
	default:
		return false
	}
}
