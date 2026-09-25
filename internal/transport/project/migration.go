package project

import (
	"strings"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

func workflowMappingsFromProto(statuses []*projectv1.WorkflowStatusMigrationMapping, fields []*projectv1.WorkflowFieldMigrationMapping) (projectworkflow.MigrationMappings, error) {
	out := projectworkflow.MigrationMappings{Statuses: make(map[string]string, len(statuses)), Fields: make(map[string]string, len(fields))}
	for _, mapping := range statuses {
		if mapping == nil || strings.TrimSpace(mapping.GetSourceStatusId()) == "" || strings.TrimSpace(mapping.GetTargetStatusId()) == "" {
			return projectworkflow.MigrationMappings{}, envelope.New(envelope.CodeInvalidArgument, "project.workflow.mapping_invalid", "workflow migration mappings are invalid")
		}
		source, target := strings.TrimSpace(mapping.GetSourceStatusId()), strings.TrimSpace(mapping.GetTargetStatusId())
		if source != mapping.GetSourceStatusId() || target != mapping.GetTargetStatusId() {
			return projectworkflow.MigrationMappings{}, envelope.New(envelope.CodeInvalidArgument, "project.workflow.mapping_invalid", "workflow migration mappings are invalid")
		}
		if _, duplicate := out.Statuses[source]; duplicate {
			return projectworkflow.MigrationMappings{}, envelope.New(envelope.CodeInvalidArgument, "project.workflow.mapping_invalid", "workflow migration mappings are invalid")
		}
		out.Statuses[source] = target
	}
	for _, mapping := range fields {
		if mapping == nil || strings.TrimSpace(mapping.GetSourceFieldId()) == "" || strings.TrimSpace(mapping.GetTargetFieldId()) == "" {
			return projectworkflow.MigrationMappings{}, envelope.New(envelope.CodeInvalidArgument, "project.workflow.mapping_invalid", "workflow migration mappings are invalid")
		}
		source, target := strings.TrimSpace(mapping.GetSourceFieldId()), strings.TrimSpace(mapping.GetTargetFieldId())
		if source != mapping.GetSourceFieldId() || target != mapping.GetTargetFieldId() {
			return projectworkflow.MigrationMappings{}, envelope.New(envelope.CodeInvalidArgument, "project.workflow.mapping_invalid", "workflow migration mappings are invalid")
		}
		if _, duplicate := out.Fields[source]; duplicate {
			return projectworkflow.MigrationMappings{}, envelope.New(envelope.CodeInvalidArgument, "project.workflow.mapping_invalid", "workflow migration mappings are invalid")
		}
		out.Fields[source] = target
	}
	return out, nil
}
