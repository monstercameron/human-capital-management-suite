package promotionexec

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	steptask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
)

func rev008ContractNode() workflow.CompiledNode {
	return workflow.CompiledNode{
		ID: NodeReapproval, Type: workflow.StepTask,
		OutputSchema: ReapprovalOutputSchema(),
	}
}

func rev008ContractSelection(node workflow.CompiledNode) runtime.WorkflowSelection {
	plan := &workflow.CompiledWorkflow{WorkflowID: "promotion.execute", Version: 2, Nodes: []workflow.CompiledNode{node}}
	return runtime.WorkflowSelection{WorkflowID: "promotion.execute", Plan: plan}
}

func rev008ContractItem() workitem.WorkItem {
	return workitem.WorkItem{
		TenantID: uuid.New(), WorkItemID: uuid.New(), ItemVersion: 3,
		Kind: workitem.KindTask, WorkType: ReapprovalWorkType, NodeID: NodeReapproval,
	}
}

// TestReapprovalTaskContract pins the served TASK binding REV-008-01
// verifies at resume: the pinned plan node, the stored item and the output
// schema must all name the served reapproval, and the minted contract
// carries the canonical form and policy pins.
func TestReapprovalTaskContract(t *testing.T) {
	node := rev008ContractNode()
	selection := rev008ContractSelection(node)
	contract, err := ReapprovalTaskContract(selection, node, rev008ContractItem())
	if err != nil {
		t.Fatalf("ReapprovalTaskContract: %v", err)
	}
	if contract.NodeID != NodeReapproval || contract.WorkType != ReapprovalWorkType {
		t.Fatalf("contract = %+v, want the served reapproval binding", contract)
	}
	if contract.OutputSchema != ReapprovalOutputSchema() {
		t.Fatalf("contract schema = %s, want %s", contract.OutputSchema, ReapprovalOutputSchema())
	}
	if contract.FormDefinition != ReapprovalFormDefinition ||
		contract.AccessibilityPolicy != ReapprovalAccessibilityPolicy ||
		contract.AccommodationPolicy != ReapprovalAccommodationPolicy {
		t.Fatalf("contract pins = %+v, want the canonical reapproval pins", contract)
	}
	if contract.WorkflowID != "promotion.execute" || contract.WorkflowVersion != 2 {
		t.Fatalf("contract identity = %s v%d, want the pinned selection", contract.WorkflowID, contract.WorkflowVersion)
	}

	other := node
	other.ID = "some_other_task"
	if _, err := ReapprovalTaskContract(selection, other, rev008ContractItem()); err == nil {
		t.Fatal("contract accepted another node, want refusal")
	}
	wrongKind := node
	wrongKind.Type = workflow.StepApproval
	if _, err := ReapprovalTaskContract(selection, wrongKind, rev008ContractItem()); err == nil {
		t.Fatal("contract accepted a non-TASK node, want refusal")
	}
	wrongWork := rev008ContractItem()
	wrongWork.WorkType = "task.promotion.something_else/v1"
	if _, err := ReapprovalTaskContract(selection, node, wrongWork); err == nil {
		t.Fatal("contract accepted another work type, want refusal")
	}
	drifted := node
	drifted.OutputSchema = workflow.SchemaRef{SchemaID: "moved", Version: 9, ProtobufFullName: "moved"}
	if _, err := ReapprovalTaskContract(selection, drifted, rev008ContractItem()); err == nil {
		t.Fatal("contract accepted a moved output schema, want refusal")
	}
}

// TestReapprovalTaskValidator pins the served submission checks: schema and
// form equality plus present accessibility and accommodation evidence.
func TestReapprovalTaskValidator(t *testing.T) {
	node := rev008ContractNode()
	contract, err := ReapprovalTaskContract(rev008ContractSelection(node), node, rev008ContractItem())
	if err != nil {
		t.Fatalf("ReapprovalTaskContract: %v", err)
	}
	valid := steptask.Submission{
		OutputSchema: contract.OutputSchema, FormDefinition: contract.FormDefinition,
		ValidationEvidenceRef:    "validation:reapproval:reviewed",
		AccessibilityEvidenceRef: "ack:accessibility:reviewed", AccommodationEvidenceRef: "ack:accommodation:reviewed",
	}
	if err := ReapprovalTaskValidator.Validate(steptask.ValidationRequest{Node: contract, Submission: valid}); err != nil {
		t.Fatalf("validator refused a valid submission: %v", err)
	}
	moved := valid
	moved.OutputSchema = workflow.SchemaRef{SchemaID: "other", Version: 1, ProtobufFullName: "other"}
	if err := ReapprovalTaskValidator.Validate(steptask.ValidationRequest{Node: contract, Submission: moved}); err == nil {
		t.Fatal("validator accepted a moved output schema, want refusal")
	}
	noAck := valid
	noAck.AccommodationEvidenceRef = ""
	if err := ReapprovalTaskValidator.Validate(steptask.ValidationRequest{Node: contract, Submission: noAck}); err == nil {
		t.Fatal("validator accepted a missing accommodation acknowledgement, want refusal")
	}
	if got := ReapprovalOutputSchema().SchemaID; !strings.HasSuffix(got, "ReapprovalTaskResult/v1") {
		t.Fatalf("reapproval output schema = %q, want the plan's ReapprovalTaskResult", got)
	}
}
