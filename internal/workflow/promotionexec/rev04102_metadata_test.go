package promotionexec

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_REV_041_02_CompiledMetadataPreservesDigestContract(t *testing.T) {
	baseline, err := Compile()
	if err != nil {
		t.Fatal(err)
	}

	presentation := Definition()
	for i := range presentation.Nodes {
		if presentation.Nodes[i].ID == NodeExecutePromotion {
			presentation.Nodes[i].Metadata = make(map[string]string)
			presentation.Nodes[i].Metadata["hcmnext.designer.display_name"] = "Release promotion"
		}
	}
	presentationPlan, err := Compile(presentation)
	if err != nil {
		t.Fatal(err)
	}
	if presentationPlan.Digest() != baseline.Digest() {
		t.Fatalf("presentation metadata changed plan digest: %s != %s", presentationPlan.Digest(), baseline.Digest())
	}
	compiledNode, ok := presentationPlan.Node(NodeExecutePromotion)
	if !ok || compiledNode.Metadata["hcmnext.designer.display_name"] != "Release promotion" {
		t.Fatalf("in-process compiled node lost authoring metadata: %+v", compiledNode.Metadata)
	}

	attested := Definition()
	for i := range attested.Nodes {
		if attested.Nodes[i].ID == NodeExecutePromotion {
			attested.Nodes[i].Metadata = make(map[string]string)
			attested.Nodes[i].Metadata[workflow.AttestationExecutionMetadataPrefix+"response_id"] = "response:time/1"
		}
	}
	attestedPlan, err := Compile(attested)
	if err != nil {
		t.Fatal(err)
	}
	if attestedPlan.Digest() == baseline.Digest() {
		t.Fatal("execution attestation metadata did not change the plan digest")
	}
	encoded, err := json.Marshal(attestedPlan)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := workflow.DecodeCanonicalPlan(encoded)
	if err != nil {
		t.Fatalf("DecodeCanonicalPlan: %v", err)
	}
	if decoded.Digest() != attestedPlan.Digest() || decoded.Verify() != nil {
		t.Fatalf("round-trip plan digest=%s verify=%v; want %s", decoded.Digest(), decoded.Verify(), attestedPlan.Digest())
	}
	decodedNode, ok := decoded.Node(NodeExecutePromotion)
	if !ok || decodedNode.Metadata[workflow.AttestationExecutionMetadataPrefix+"response_id"] != "response:time/1" {
		t.Fatalf("canonical round-trip lost execution metadata: %+v", decodedNode.Metadata)
	}
	decodedNode.Metadata[workflow.AttestationExecutionMetadataPrefix+"response_id"] = "response:forged/1"
	decoded.Nodes[indexNode(decoded.Nodes, NodeExecutePromotion)] = decodedNode
	if err := decoded.Verify(); err == nil {
		t.Fatal("mutated attestation execution metadata still verifies")
	}
}

func indexNode(nodes []workflow.CompiledNode, id string) int {
	for i, node := range nodes {
		if node.ID == id {
			return i
		}
	}
	return -1
}
