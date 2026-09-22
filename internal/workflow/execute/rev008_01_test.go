package execute

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	steptask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// --- REV-008-01 fixtures ------------------------------------------------------
//
// The served TASK node must resume through internal/workflow/steps/task
// instead of the ad hoc drift check alone: the typed outcome a caller
// presents has to bind the stored completion digest, and a recorded
// submission has to satisfy the compiled node's schema and
// accommodation-acknowledgement contract before the advance commits.

var (
	errRev008SchemaMismatch = errors.New("rev008: output schema mismatch")
	errRev008MissingAck     = errors.New("rev008: accommodation acknowledgement missing")
	errRev008UnknownNode    = errors.New("rev008: unknown task node")
)

var rev008TaskNode = workflow.CompiledNode{
	ID:   "reapproval_task",
	Type: workflow.StepTask,
	OutputSchema: workflow.SchemaRef{
		SchemaID:         "hcmnext.workflows.promotion.execute.ReapprovalTaskResult/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.workflows.promotion.execute.ReapprovalTaskResult",
	},
}

func rev008TaskContract() steptask.CompiledTaskNode {
	return steptask.CompiledTaskNode{
		WorkflowID: "promotion.execute", WorkflowVersion: 1,
		NodeID: rev008TaskNode.ID, WorkType: "task.promotion.reapproval/v1",
		OutputSchema:        rev008TaskNode.OutputSchema,
		FormDefinition:      steptask.VersionedRef{Ref: "form.promotion.reapproval", Version: 1},
		AccessibilityPolicy: steptask.VersionedRef{Ref: "policy.accessibility.default", Version: 1},
		AccommodationPolicy: steptask.VersionedRef{Ref: "policy.accommodation.default", Version: 1},
	}
}

var rev008TaskValidator = steptask.ValidatorFunc(func(req steptask.ValidationRequest) error {
	if req.Submission.OutputSchema != req.Node.OutputSchema {
		return errRev008SchemaMismatch
	}
	if req.Submission.FormDefinition != req.Node.FormDefinition {
		return errRev008SchemaMismatch
	}
	if req.Submission.AccessibilityEvidenceRef == "" || req.Submission.AccommodationEvidenceRef == "" {
		return errRev008MissingAck
	}
	return nil
})

func rev008TaskPolicy(records map[uuid.UUID]workitem.DecisionRecord) *TaskResumePolicy {
	return &TaskResumePolicy{
		NodeFor: func(_ runtime.WorkflowSelection, node workflow.CompiledNode, _ workitem.WorkItem) (steptask.CompiledTaskNode, error) {
			if node.ID != rev008TaskNode.ID {
				return steptask.CompiledTaskNode{}, errRev008UnknownNode
			}
			return rev008TaskContract(), nil
		},
		Validator: rev008TaskValidator,
		LoadDecision: func(_ context.Context, _ workitem.Executor, _ uuid.UUID, workItemID uuid.UUID) (workitem.DecisionRecord, error) {
			rec, ok := records[workItemID]
			if !ok {
				return workitem.DecisionRecord{}, &workitem.Error{Code: workitem.CodeWorkItemNotFound, WorkItemID: workItemID.String(), Detail: "no decision recorded for this work item"}
			}
			return rec, nil
		},
	}
}

func rev008ResumeFixture() ResumeRequest {
	tenantID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	instanceID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	workItemID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	proposalDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	plan := &workflow.CompiledWorkflow{
		WorkflowID: "promotion.execute", Version: 1,
		Nodes: []workflow.CompiledNode{rev008TaskNode},
	}
	selection := runtime.WorkflowSelection{
		WorkflowID: "promotion.execute", Pin: version.Pin{SemanticVersion: "1.0.0"}, Plan: plan,
	}
	record := version.CompiledVersion{
		WorkflowID: "promotion.execute", SemanticVersion: "1.0.0",
		CompiledPlanDigest: plan.Digest(), Status: version.StatusActive,
	}
	req := ResumeRequest{
		Start: runtime.StartRequest{
			TenantID: tenantID, CellID: "cell-1", StartIdempotencyKey: "start-1",
			Resolver: staticResolver{selection}, Versions: staticVersions{record},
			Proposal: runtime.ProposalBinding{}, CorrelationID: "corr-1",
			BusinessSubjectRefs: []string{"employment:1"},
		},
		InstanceID: instanceID, ExpectedInstanceVersion: 3, RecordedAt: at,
		WorkItemID: workItemID, ExpectedWorkItemVersion: 7,
		Outcome: frontier.NodeOutcome{
			Outcome:      workflow.OutcomeSucceeded,
			OutputDigest: "sha256:" + strings.Repeat("d", 64),
		},
	}
	req.Start.Proposal.Revision.MaterialDigest.Digest = proposalDigest
	return req
}

// rev008StoredItem is the durable COMPLETED TASK row the fake reader hands
// back: its completion digest binds the minted submission below.
func rev008StoredItem(req ResumeRequest) workitem.WorkItem {
	at := req.RecordedAt
	completedAt := at
	completedBy := "principal:hr-partner"
	return workitem.WorkItem{
		TenantID: req.Start.TenantID, WorkItemID: req.WorkItemID, ItemVersion: req.ExpectedWorkItemVersion,
		Kind: workitem.KindTask, WorkType: "task.promotion.reapproval/v1", Status: workitem.StatusCompleted,
		CorrelationID: req.Start.CorrelationID, WorkflowInstanceID: req.InstanceID, NodeID: rev008TaskNode.ID,
		ProposalRef: req.Start.Proposal.Revision.MaterialDigest.Digest,
		SubjectRefs: []string{"employment:1"}, OwnerKind: workitem.OwnerPrincipal,
		OwnerRef: completedBy, PolicyRouteRef: "route.promotion.hr_business_partner/v1",
		Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org-1",
		DeadlineAt: at.Add(time.Hour), CompletedBy: completedBy, CompletedAt: &completedAt,
		CompletedOutputDigest: req.Outcome.OutputDigest,
		CreatedAt:             at.Add(-time.Hour), RecordedAt: at,
		Assignment: workitem.Assignment{
			Resolution: humanwork.Resolution{
				RequirementID: "task.promotion.reapproval/v1", Outcome: humanwork.OutcomeResolved,
				Candidates: []humanwork.Candidate{{PrincipalID: completedBy, Via: humanwork.SourceDirect, TermRef: "term:execution-authority-approver"}},
				ResolvedAt: values.NewInstant(at), EffectiveAt: values.NewInstant(at),
				DirectoryVersion:  "directory.execution-authority/1",
				ExpressionDigest:  "sha256:" + strings.Repeat("1", 64),
				RequirementDigest: "sha256:" + strings.Repeat("2", 64),
				QuorumRequired:    1,
			},
			Trigger: workitem.TriggerInitialRouting, ChosenOwner: completedBy,
		},
	}
}

// rev008MintSubmission mints the immutable typed submission the stored
// completion digest binds, exactly as task.Submit would before completing.
func rev008MintSubmission(t *testing.T, req ResumeRequest, item workitem.WorkItem) steptask.Submission {
	t.Helper()
	contract := rev008TaskContract()
	sub, err := steptask.NewSubmission(steptask.SubmissionSpec{
		WorkflowInstanceID: req.InstanceID, NodeID: contract.NodeID,
		WorkItemID: req.WorkItemID, ItemVersion: item.ItemVersion,
		CompletedBy: item.CompletedBy, CandidateVia: humanwork.SourceDirect,
		ClaimID:                  uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"),
		ClaimExpiresAt:           values.NewInstant(req.RecordedAt.Add(time.Hour)),
		SubmittedAt:              values.NewInstant(req.RecordedAt),
		OutputSchema:             contract.OutputSchema,
		CanonicalPayloadDigest:   "sha256:" + strings.Repeat("9", 64),
		FormDefinition:           contract.FormDefinition,
		RenderContextDigest:      "sha256:" + strings.Repeat("8", 64),
		ValidationEvidenceRef:    "validation:reapproval:reviewed",
		AccessibilityEvidenceRef: "ack:accessibility:reviewed",
		AccommodationEvidenceRef: "ack:accommodation:reviewed",
	})
	if err != nil {
		t.Fatalf("NewSubmission: %v", err)
	}
	return sub
}

func rev008DecisionRecord(item workitem.WorkItem, body json.RawMessage, digest string) workitem.DecisionRecord {
	return workitem.DecisionRecord{
		TenantID: item.TenantID, WorkItemID: item.WorkItemID,
		WorkflowInstanceID: item.WorkflowInstanceID, ItemVersion: item.ItemVersion,
		Kind:       workitem.DecisionKindTask,
		Body:       body,
		BodyDigest: digest,
		DecidedBy:  item.CompletedBy,
	}
}

func rev008ResumeDriver(t *testing.T, item workitem.WorkItem, policy *TaskResumePolicy) (*Driver, *bool) {
	t.Helper()
	called := new(bool)
	driver, err := New(Options{
		DB: oneBeginner{&memoryTx{}}, Steps: noStepRunner{}, Items: fakeWorkItemReader{item: item},
		Tasks: policy,
		Advance: func(context.Context, runtime.Executor, runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			*called = true
			return runtime.AdvanceReceipt{NewInstanceVersion: 7, Complete: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return driver, called
}

// TestTodo_REV_008_01 is the PRIMARY case: the served TASK resume path binds
// the typed outcome to the stored completion digest and, when a submission is
// recorded, verifies it through the certified task contract before advancing.
func TestTodo_REV_008_01(t *testing.T) {
	t.Run("forged outcome digest is refused before advance", func(t *testing.T) {
		req := rev008ResumeFixture()
		item := rev008StoredItem(req)
		req.Outcome.OutputDigest = "sha256:" + strings.Repeat("f", 64)
		driver, called := rev008ResumeDriver(t, item, rev008TaskPolicy(nil))
		if _, err := driver.Resume(context.Background(), req); err == nil {
			t.Fatal("Resume with a forged outcome digest advanced, want refusal")
		}
		if *called {
			t.Fatal("Advance ran for a forged outcome digest")
		}
	})

	t.Run("non-task item on a task node is refused before advance", func(t *testing.T) {
		req := rev008ResumeFixture()
		item := rev008StoredItem(req)
		item.Kind = workitem.KindApproval
		driver, called := rev008ResumeDriver(t, item, rev008TaskPolicy(nil))
		if _, err := driver.Resume(context.Background(), req); err == nil {
			t.Fatal("Resume of an approval item on a TASK node advanced, want refusal")
		}
		if *called {
			t.Fatal("Advance ran for a non-task item on a TASK node")
		}
	})

	t.Run("legacy completion without a recorded submission still advances", func(t *testing.T) {
		req := rev008ResumeFixture()
		item := rev008StoredItem(req)
		driver, called := rev008ResumeDriver(t, item, rev008TaskPolicy(nil))
		if _, err := driver.Resume(context.Background(), req); err != nil {
			t.Fatalf("Resume of a legacy TASK completion: %v", err)
		}
		if !*called {
			t.Fatal("Advance did not run for a bound legacy TASK completion")
		}
	})

	t.Run("recorded submission omitting the accommodation acknowledgement is refused", func(t *testing.T) {
		req := rev008ResumeFixture()
		item := rev008StoredItem(req)
		sub := rev008MintSubmission(t, req, item)
		body, err := steptask.EncodeSubmission(sub)
		if err != nil {
			t.Fatalf("EncodeSubmission: %v", err)
		}
		var wire map[string]any
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatalf("unmarshal submission body: %v", err)
		}
		wire["accommodation_evidence_ref"] = ""
		tampered, err := json.Marshal(wire)
		if err != nil {
			t.Fatalf("marshal tampered body: %v", err)
		}
		// Bind the stored completion to the untampered digest so the refusal
		// proves the missing acknowledgement, not the digest binding.
		item.CompletedOutputDigest = sub.Digest()
		req.Outcome.OutputDigest = sub.Digest()
		records := map[uuid.UUID]workitem.DecisionRecord{
			item.WorkItemID: rev008DecisionRecord(item, tampered, sub.Digest()),
		}
		driver, called := rev008ResumeDriver(t, item, rev008TaskPolicy(records))
		if _, err := driver.Resume(context.Background(), req); err == nil {
			t.Fatal("Resume with a submission omitting the accommodation acknowledgement advanced, want refusal")
		}
		if *called {
			t.Fatal("Advance ran for a submission omitting the accommodation acknowledgement")
		}
	})

	t.Run("recorded submission failing schema validation is refused", func(t *testing.T) {
		req := rev008ResumeFixture()
		item := rev008StoredItem(req)
		sub := rev008MintSubmission(t, req, item)
		body, err := steptask.EncodeSubmission(sub)
		if err != nil {
			t.Fatalf("EncodeSubmission: %v", err)
		}
		var wire map[string]any
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatalf("unmarshal submission body: %v", err)
		}
		schema, _ := wire["output_schema"].(map[string]any)
		schema["schema_id"] = "hcmnext.workflows.promotion.execute.SomeOtherResult/v1"
		wire["output_schema"] = schema
		tampered, err := json.Marshal(wire)
		if err != nil {
			t.Fatalf("marshal tampered body: %v", err)
		}
		records := map[uuid.UUID]workitem.DecisionRecord{
			item.WorkItemID: rev008DecisionRecord(item, tampered, sub.Digest()),
		}
		// Rebind the stored completion to the tampered submission's own
		// digest so the refusal proves schema validation, not digest
		// binding: the tampered content must still fail.
		resub, err := steptask.DecodeSubmission(tampered)
		if err != nil {
			t.Fatalf("DecodeSubmission of schema-tampered body: %v", err)
		}
		item.CompletedOutputDigest = resub.Digest()
		req.Outcome.OutputDigest = resub.Digest()
		records[item.WorkItemID] = rev008DecisionRecord(item, tampered, resub.Digest())
		driver, called := rev008ResumeDriver(t, item, rev008TaskPolicy(records))
		if _, err := driver.Resume(context.Background(), req); err == nil {
			t.Fatal("Resume with a schema-violating submission advanced, want refusal")
		}
		if *called {
			t.Fatal("Advance ran for a schema-violating submission")
		}
	})

	t.Run("recorded valid submission advances through the task contract", func(t *testing.T) {
		req := rev008ResumeFixture()
		item := rev008StoredItem(req)
		sub := rev008MintSubmission(t, req, item)
		body, err := steptask.EncodeSubmission(sub)
		if err != nil {
			t.Fatalf("EncodeSubmission: %v", err)
		}
		// Bind the stored completion to the minted submission digest, exactly
		// as a Submit-backed completion would.
		item.CompletedOutputDigest = sub.Digest()
		req.Outcome.OutputDigest = sub.Digest()
		records := map[uuid.UUID]workitem.DecisionRecord{
			item.WorkItemID: rev008DecisionRecord(item, body, sub.Digest()),
		}
		driver, called := rev008ResumeDriver(t, item, rev008TaskPolicy(records))
		if _, err := driver.Resume(context.Background(), req); err != nil {
			t.Fatalf("Resume with a valid recorded submission: %v", err)
		}
		if !*called {
			t.Fatal("Advance did not run for a valid recorded submission")
		}
	})
}

// TestTodo_REV_008_01_Security proves the TASK resume gate refuses forged,
// cross-tenant and validator-rejected evidence without advancing.
func TestTodo_REV_008_01_Security(t *testing.T) {
	t.Run("cross-tenant stored row is refused before advance", func(t *testing.T) {
		req := rev008ResumeFixture()
		item := rev008StoredItem(req)
		item.TenantID = uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
		driver, called := rev008ResumeDriver(t, item, rev008TaskPolicy(nil))
		if _, err := driver.Resume(context.Background(), req); err == nil {
			t.Fatal("Resume bound to another tenant advanced, want refusal")
		}
		if *called {
			t.Fatal("Advance ran for a cross-tenant stored row")
		}
	})

	t.Run("validator rejection is refused before advance", func(t *testing.T) {
		req := rev008ResumeFixture()
		item := rev008StoredItem(req)
		sub := rev008MintSubmission(t, req, item)
		body, err := steptask.EncodeSubmission(sub)
		if err != nil {
			t.Fatalf("EncodeSubmission: %v", err)
		}
		item.CompletedOutputDigest = sub.Digest()
		req.Outcome.OutputDigest = sub.Digest()
		policy := rev008TaskPolicy(map[uuid.UUID]workitem.DecisionRecord{
			item.WorkItemID: rev008DecisionRecord(item, body, sub.Digest()),
		})
		deny := errors.New("rev008: deny")
		policy.Validator = steptask.ValidatorFunc(func(steptask.ValidationRequest) error { return deny })
		driver, called := rev008ResumeDriver(t, item, policy)
		err = nil
		if _, err = driver.Resume(context.Background(), req); err == nil {
			t.Fatal("Resume with a validator-rejected submission advanced, want refusal")
		}
		// Resolve reports the validator verdict through
		// task.ErrValidationFailed (it formats the validator's own
		// error with %v), so the refusal must unwrap to the
		// validation-failed sentinel and name the verdict.
		if !errors.Is(err, steptask.ErrValidationFailed) {
			t.Fatalf("Resume error = %v, want the validation-failed refusal", err)
		}
		if !strings.Contains(err.Error(), "rev008: deny") {
			t.Fatalf("Resume error = %v, want it to name the validator verdict", err)
		}
		if *called {
			t.Fatal("Advance ran for a validator-rejected submission")
		}
	})

	t.Run("tampered body that no longer matches its digest is refused", func(t *testing.T) {
		req := rev008ResumeFixture()
		item := rev008StoredItem(req)
		sub := rev008MintSubmission(t, req, item)
		body, err := steptask.EncodeSubmission(sub)
		if err != nil {
			t.Fatalf("EncodeSubmission: %v", err)
		}
		var wire map[string]any
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatalf("unmarshal submission body: %v", err)
		}
		wire["canonical_payload_digest"] = "sha256:" + strings.Repeat("7", 64)
		tampered, err := json.Marshal(wire)
		if err != nil {
			t.Fatalf("marshal tampered body: %v", err)
		}
		// Bind the stored completion to the untampered digest so the refusal
		// proves the body's own tamper evidence, not the digest binding.
		item.CompletedOutputDigest = sub.Digest()
		req.Outcome.OutputDigest = sub.Digest()
		records := map[uuid.UUID]workitem.DecisionRecord{
			item.WorkItemID: rev008DecisionRecord(item, tampered, sub.Digest()),
		}
		driver, called := rev008ResumeDriver(t, item, rev008TaskPolicy(records))
		if _, err := driver.Resume(context.Background(), req); err == nil {
			t.Fatal("Resume with a digest-mismatched body advanced, want refusal")
		}
		if *called {
			t.Fatal("Advance ran for a digest-mismatched body")
		}
	})
}

// TestTodo_REV_008_01_Integration drives the real Submit-backed completion
// chain against embedded PostgreSQL: Open, Route, Claim and task.Submit
// through workitem.Store, then the exact verification chain the resume gate
// uses (LoadDecision, DecodeSubmission, NewContinuation, Resolve) over the
// persisted rows.
func TestTodo_REV_008_01_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := uuid.New()
	instance := uuid.New()
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', $4)`,
		tenant, "rev008-01-"+tenant.String(), "REV-008-01 tenant", at.Add(-time.Hour))
	db.Exec(t, `
		INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'promotion.execute', 1,
			'0000000000000000000000000000000000000000000000000000000000000000', 'EXECUTE', 'WAITING', 'sha256:input',
			ARRAY['reapproval_task'], $3, $4)`,
		tenant, instance, "corr-rev008-01", at.Add(-time.Hour))
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	store := workitem.Store{}
	contract := rev008TaskContract()
	actor := "principal:hr-partner"

	runTx := func(fn func(tx dbport.Tx) error) {
		t.Helper()
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("assume tenant: %v", err)
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("transaction: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	var opened workitem.WorkItem
	runTx(func(tx dbport.Tx) error {
		var err error
		opened, err = steptask.Open(ctx, tx, store, steptask.OpenInput{
			TenantID: tenant, WorkflowInstanceID: instance, CorrelationID: "corr-rev008-01",
			SubjectRefs: []string{"employment:1"}, Node: contract,
			PolicyRouteRef: "route.promotion.hr_business_partner/v1",
			Visibility:     workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org-1",
			DeadlineAt: at.Add(48 * time.Hour), Now: at,
			Meta: workitem.TransitionMeta{ActorPrincipalID: "system:promotion-execution", Reason: "execution_authority.work_item.created", At: at},
		})
		return err
	})
	var routed workitem.WorkItem
	runTx(func(tx dbport.Tx) error {
		var err error
		routed, err = store.Route(ctx, tx, tenant, opened.WorkItemID, opened.ItemVersion,
			workitem.Assignment{
				Resolution: humanwork.Resolution{
					RequirementID: "task.promotion.reapproval/v1", Outcome: humanwork.OutcomeResolved,
					Candidates: []humanwork.Candidate{{PrincipalID: actor, Via: humanwork.SourceDirect, TermRef: "term:execution-authority-approver"}},
					ResolvedAt: values.NewInstant(at), EffectiveAt: values.NewInstant(at),
					DirectoryVersion:  "directory.execution-authority/1",
					ExpressionDigest:  "sha256:" + strings.Repeat("1", 64),
					RequirementDigest: "sha256:" + strings.Repeat("2", 64),
					QuorumRequired:    1,
				},
				GovernancePolicyRef: "policy.promotion.reapproval/v1",
				Trigger:             workitem.TriggerInitialRouting, ChosenOwner: actor,
			},
			workitem.TransitionMeta{ActorPrincipalID: "system:promotion-execution", Reason: "execution_authority.work_item.routed", At: at})
		return err
	})
	var claimed workitem.WorkItem
	runTx(func(tx dbport.Tx) error {
		var err error
		claimed, err = store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: routed.WorkItemID, ExpectedVersion: routed.ItemVersion,
			ClaimantPrincipalID: actor, ClaimExpiresAt: at.Add(time.Hour), Now: at,
			Meta: workitem.TransitionMeta{ActorPrincipalID: actor, Reason: "journey.approval.claimed", At: at},
		})
		return err
	})
	var started workitem.WorkItem
	runTx(func(tx dbport.Tx) error {
		var err error
		started, err = store.Start(ctx, tx, tenant, claimed.WorkItemID, claimed.ItemVersion, at,
			workitem.TransitionMeta{ActorPrincipalID: actor, Reason: "journey.approval.started", At: at})
		return err
	})
	claimed = started

	submitAt := at.Add(time.Minute)
	var completed workitem.WorkItem
	var sub steptask.Submission
	runTx(func(tx dbport.Tx) error {
		var err error
		completed, sub, err = steptask.Submit(ctx, tx, store, claimed, steptask.SubmitInput{
			Node: contract,
			Spec: steptask.SubmissionSpec{
				CompletedBy: actor, CandidateVia: humanwork.SourceDirect,
				ClaimID: *claimed.ClaimID, ClaimExpiresAt: values.NewInstant(*claimed.ClaimExpiresAt),
				SubmittedAt:              values.NewInstant(submitAt),
				OutputSchema:             contract.OutputSchema,
				CanonicalPayloadDigest:   "sha256:" + strings.Repeat("9", 64),
				FormDefinition:           contract.FormDefinition,
				RenderContextDigest:      "sha256:" + strings.Repeat("8", 64),
				ValidationEvidenceRef:    "validation:reapproval:reviewed",
				AccessibilityEvidenceRef: "ack:accessibility:reviewed",
				AccommodationEvidenceRef: "ack:accommodation:reviewed",
			},
			Validator: rev008TaskValidator, Now: submitAt,
			Meta: workitem.TransitionMeta{ActorPrincipalID: actor, Reason: "journey.approval.decided", At: submitAt},
		})
		return err
	})
	if completed.CompletedOutputDigest != sub.Digest() {
		t.Fatalf("Submit digest = %q, completed digest = %q", sub.Digest(), completed.CompletedOutputDigest)
	}

	var rec workitem.DecisionRecord
	runTx(func(tx dbport.Tx) error {
		var err error
		rec, err = workitem.LoadDecision(ctx, tx, tenant, completed.WorkItemID)
		return err
	})
	if rec.BodyDigest != sub.Digest() {
		t.Fatalf("recorded body digest = %q, want submission digest %q", rec.BodyDigest, sub.Digest())
	}
	decoded, err := steptask.DecodeSubmission(rec.Body)
	if err != nil {
		t.Fatalf("DecodeSubmission of the recorded row: %v", err)
	}
	if decoded.Digest() != sub.Digest() {
		t.Fatalf("decoded digest = %q, want %q", decoded.Digest(), sub.Digest())
	}
	cont, err := steptask.NewContinuation(instance, contract, completed)
	if err != nil {
		t.Fatalf("NewContinuation: %v", err)
	}
	res, err := steptask.Resolve(cont, completed, &decoded, rev008TaskValidator, values.NewInstant(submitAt), steptask.Event{})
	if err != nil {
		t.Fatalf("Resolve of the recorded submission: %v", err)
	}
	if res.Outcome != steptask.OutcomeSucceeded {
		t.Fatalf("Resolve outcome = %q, want SUCCEEDED", res.Outcome)
	}
	out := res.ToNodeOutcome(contract.NodeID)
	if out.Outcome != workflow.OutcomeSucceeded || out.OutputDigest != res.Digest {
		t.Fatalf("ToNodeOutcome = %+v, want SUCCEEDED with digest %s", out, res.Digest)
	}

	deny := steptask.ValidatorFunc(func(steptask.ValidationRequest) error { return errRev008SchemaMismatch })
	if _, err := steptask.Resolve(cont, completed, &decoded, deny, values.NewInstant(submitAt), steptask.Event{}); err == nil {
		t.Fatal("Resolve with a rejecting validator succeeded against the recorded row, want refusal")
	}
}
