// Package authoredwire is the protojson codec for the stored authoring form of
// a workflow definition (WF-EXT-010). It is the only workflow package that
// imports the protobuf JSON encoder, so the protobuf dependency root in
// definitions/architecture/dependency-roles.yaml can name this subpackage
// instead of the whole internal/workflow tree.
//
// The package deliberately knows nothing about workflow.Definition. Mapping
// between the wire message and the domain shape stays in internal/workflow
// (ToAuthoredProto and FromAuthoredProto), which keeps this package free of an
// import cycle back into its parent.
package authoredwire

import (
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Unmarshal decodes the protojson rendering of an AuthoredWorkflowDefinition.
// It is strict: protojson's default rejects unknown fields and mistyped
// values, so neither is silently omitted.
func Unmarshal(data []byte) (*workflowv1.AuthoredWorkflowDefinition, error) {
	var pb workflowv1.AuthoredWorkflowDefinition
	if err := protojson.Unmarshal(data, &pb); err != nil {
		return nil, err
	}
	return &pb, nil
}

// Marshal renders an AuthoredWorkflowDefinition as multiline protojson with
// two-space indentation. The output is the artifact that Unmarshal accepts.
func Marshal(pb *workflowv1.AuthoredWorkflowDefinition) ([]byte, error) {
	return protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(pb)
}
