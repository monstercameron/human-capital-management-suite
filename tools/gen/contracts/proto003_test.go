package contracts

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
)

// TestTodo_PROTO_003 proves the generated workflow, human-task and operator
// service contracts (schema/proto/hcmnext/workflow/v1,
// schema/proto/hcmnext/humanwork/v1) satisfy the todo's GREEN criteria:
// every declared method carries a total P1A disposition comment, every new
// enum declares an UNSPECIFIED zero value, presence rules are explicit where
// the convention requires them, generated messages round-trip, and clean
// `buf generate` runs are byte-identical.
func TestTodo_PROTO_003(t *testing.T) {
	repoRoot := findRepoRoot(t)
	dir := t.TempDir()
	fds := buildDescriptorSet(t, repoRoot, dir, "descriptor.binpb")

	t.Run("WorkflowServiceHasTotalDispositionComments", func(t *testing.T) {
		assertServiceHasTotalDispositionComments(t, fds,
			"hcmnext/workflow/v1/workflow_service.proto", "WorkflowService",
			[]string{
				"GetWorkflow", "ListNodeExecutions",
				"PauseWorkflow", "ResumeWorkflow", "CancelWorkflow", "RetryNode",
			})
	})

	t.Run("WorkServiceHasTotalDispositionComments", func(t *testing.T) {
		assertServiceHasTotalDispositionComments(t, fds,
			"hcmnext/humanwork/v1/humanwork_service.proto", "WorkService",
			[]string{
				"ListWorkItems", "GetWorkItem", "ClaimWorkItem", "ReleaseWorkItem",
				"CompleteWorkItem", "DecideApproval", "GetThresholdTable",
			})
	})

	t.Run("WorkflowEnumsHaveUnspecifiedZero", func(t *testing.T) {
		// Force registration: importing the generated package already runs
		// its file-level registration init(), and referencing a type here
		// keeps that import from reading as unused-for-linting purposes.
		_ = (&workflowv1.WorkflowInstance{}).ProtoReflect().Descriptor()
		assertEnumsHaveUnspecifiedZero(t, "hcmnext.workflow.v1", 5)
	})

	t.Run("HumanworkEnumsHaveUnspecifiedZero", func(t *testing.T) {
		_ = (&humanworkv1.WorkItem{}).ProtoReflect().Descriptor()
		assertEnumsHaveUnspecifiedZero(t, "hcmnext.humanwork.v1", 2)
	})

	t.Run("PresenceRulesAreExplicit", func(t *testing.T) {
		nodeExec := findMessage(t, "hcmnext.workflow.v1", "hcmnext.workflow.v1.NodeExecution")
		assertFieldPresence(t, nodeExec, "output_artifact_ref", true)
		assertFieldPresence(t, nodeExec, "retry_at", true)
		// node_id is always populated by the runtime and carries no distinct
		// "unset" meaning, so it must not declare explicit presence.
		assertFieldPresence(t, nodeExec, "node_id", false)

		instance := findMessage(t, "hcmnext.workflow.v1", "hcmnext.workflow.v1.WorkflowInstance")
		assertFieldPresence(t, instance, "business_transaction_id", true)
		assertFieldPresence(t, instance, "completed_at", true)
		assertFieldPresence(t, instance, "instance_id", false)

		workItem := findMessage(t, "hcmnext.humanwork.v1", "hcmnext.humanwork.v1.WorkItem")
		assertFieldPresence(t, workItem, "due_at", true)
		assertFieldPresence(t, workItem, "assigned_principal_id", true)
		assertFieldPresence(t, workItem, "work_item_id", false)
	})

	t.Run("GoldenRoundTrip", func(t *testing.T) {
		createdAt := timestamppb.New(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))

		instance := &workflowv1.WorkflowInstance{
			InstanceId: "instance-1",
			TenantId:   "tenant-1",
			Definition: &workflowv1.WorkflowDefinitionReference{
				WorkflowId: "promotion.preflight-simulate-observe",
				Version:    1,
			},
			CompiledPlanDigest: "sha256:deadbeef",
			BusinessSubjectRefs: []*commonv1.EntityRef{
				{TenantId: "tenant-1", Kind: "worker", Id: "w-1"},
			},
			ExecutionMode:   intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
			RuntimeStatus:   workflowv1.RuntimeStatus_RUNTIME_STATUS_RUNNING,
			Lifecycle:       &intentsv1.LifecycleDimensions{Request: intentsv1.RequestState_REQUEST_STATE_SUBMITTED},
			InputRef:        "artifact://input/1",
			CurrentNodeIds:  []string{"node-1"},
			InstanceVersion: 1,
			CorrelationId:   "corr-1",
			CreatedAt:       createdAt,
		}

		nodeExec := &workflowv1.NodeExecution{
			NodeExecutionId:    "ne-1",
			WorkflowInstanceId: "instance-1",
			NodeId:             "node-1",
			Attempt:            1,
			Status:             workflowv1.NodeExecutionStatus_NODE_EXECUTION_STATUS_SUCCEEDED,
			InputSnapshotRef:   "artifact://snapshot/1",
			OutputArtifactRef:  proto.String("artifact://output/1"),
			TraceId:            "trace-1",
		}

		workItem := &humanworkv1.WorkItem{
			WorkItemId:              "wi-1",
			TenantId:                "tenant-1",
			WorkType:                "approval.raise",
			Status:                  humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_AVAILABLE,
			Priority:                "P2",
			RiskClass:               "medium",
			RequiredOutputSchemaRef: "hcmnext.humanwork.v1.ApprovalDecisionResult",
			CorrelationId:           "corr-1",
			ItemVersion:             1,
			CreatedAt:               createdAt,
			AssignedPrincipalId:     proto.String("user-1"),
		}

		threshold := &humanworkv1.ThresholdTable{
			TableId:    "raise-approval-table",
			Version:    1,
			TenantId:   "tenant-1",
			InputNames: []string{"raise_percent", "compa_ratio", "country"},
			Rows: []*humanworkv1.ThresholdRow{
				{
					Conditions: []*humanworkv1.ThresholdCondition{
						{InputName: "country", Comparison: "US"},
						{InputName: "raise_percent", Comparison: ">10"},
					},
					Outcome: "FINANCE_REQUIRED",
				},
				{Outcome: "MANAGER_ONLY"},
			},
			HitPolicy: humanworkv1.ThresholdHitPolicy_THRESHOLD_HIT_POLICY_FIRST,
		}

		assertGoldenRoundTrip(t, []proto.Message{instance, nodeExec, workItem, threshold})
	})

	t.Run("BufGenerateIdempotent", func(t *testing.T) {
		assertBufGenerateIdempotent(t)
	})
}
