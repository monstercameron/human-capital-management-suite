package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// Additional stable refusal codes this file introduces, on top of the ones
// errors.go already declares for WF-RUN-001. They share that file's [Error]
// type and [refuse]/[wrap] helpers; there is nothing WF-RUN-023-specific
// about the mechanism, only the vocabulary.
const (
	// CodeMutableProposal reports a proposal revision with no minted material
	// digest: [intent.NewProposalRevision] always mints one, so a revision
	// without it never went through that constructor and cannot be trusted as
	// an immutable artifact.
	CodeMutableProposal = "MUTABLE_PROPOSAL"
	// CodeUnapprovedProposal reports a proposal revision presented with no
	// recorded approval.
	CodeUnapprovedProposal = "UNAPPROVED_PROPOSAL"
	// CodeSupersededProposal reports a proposal revision the caller's own
	// ledger no longer considers current.
	CodeSupersededProposal = "SUPERSEDED_PROPOSAL"
	// CodeWorkflowResolutionFailed reports a [WorkflowResolver] that returned
	// an error, or a selection with no workflow id or no compiled plan.
	CodeWorkflowResolutionFailed = "WORKFLOW_RESOLUTION_FAILED"
	// CodeVersionResolutionFailed reports [version.Resolve] refusing the pin.
	CodeVersionResolutionFailed = "VERSION_RESOLUTION_FAILED"
	// CodeVersionNotActive reports a resolved [version.CompiledVersion] whose
	// status is not ACTIVE.
	CodeVersionNotActive = "VERSION_NOT_ACTIVE"
	// CodeVersionPlanMismatch reports a [WorkflowResolver]-supplied compiled
	// plan whose digest disagrees with the governance record
	// [version.Resolve] returned for the same pin.
	CodeVersionPlanMismatch = "VERSION_PLAN_MISMATCH"
	// CodeTenantMismatch, CodeIntentMismatch and CodeSubjectMismatch report a
	// start request whose declared context disagrees with the material
	// content of the ProposalRevision it binds.
	CodeTenantMismatch  = "TENANT_MISMATCH"
	CodeIntentMismatch  = "INTENT_MISMATCH"
	CodeSubjectMismatch = "SUBJECT_MISMATCH"
	// CodeUnresolvedContext reports a start node whose declared required
	// context the caller did not present a resolution for.
	CodeUnresolvedContext = "UNRESOLVED_CONTEXT"
	// CodeStartConflict reports a start idempotency key already bound to a
	// request whose digests do not match this one.
	CodeStartConflict = "START_CONFLICT"
	// CodeApprovalBindingMismatch reports an approval decision the approval
	// store hands back for the started revision's own id, whose bound
	// material digest is not that revision's own digest (WF-RUN-027).
	CodeApprovalBindingMismatch = "APPROVAL_BINDING_MISMATCH"
)

// startInstanceNamespace is the fixed UUIDv5 namespace a start's instance
// identity is derived under -- see [derivedStartInstanceID].
var startInstanceNamespace = uuid.MustParse("2f7e9c3a-8b1d-4e6f-9a2c-5d8b1e4f7a3c")

// derivedStartInstanceID derives the identity [Start] will create or replay,
// from exactly the tuple that defines "the same start request": the tenant,
// the resolved workflow and the caller's own idempotency key. Deriving rather
// than allocating is what makes an identical retry collide on
// workflow_instance's primary key instead of silently minting a second
// instance for one logical start -- the same reasoning [NodeExecutionID]
// documents for node attempts.
//
// A different key derives a different, unrelated instance id on purpose: two
// different keys are two different logical starts as far as this package is
// concerned, never a conflict with each other.
func derivedStartInstanceID(tenantID uuid.UUID, workflowID, startIdempotencyKey string) uuid.UUID {
	name := tenantID.String() + "\x00" + workflowID + "\x00" + startIdempotencyKey
	return uuid.NewSHA1(startInstanceNamespace, []byte(name))
}

// ProposalBinding is what a caller presents as proof that one
// [intent.ProposalRevision] may be bound to a new workflow instance.
//
// WF-RUN-023 let this type carry the caller's own asserted Approved and
// Superseded flags. WF-RUN-027 retires that trust: [Start] now resolves both
// facts itself, through [StartRequest.ProposalFacts] and
// [StartRequest.ApprovalFacts], reading only Revision from this type. [Start]
// still checks the revision's own immutability (a minted material digest)
// and its material alignment with the rest of the [StartRequest]: an
// assertion -- caller- or store-sourced -- is not proof of the content it
// claims to describe.
type ProposalBinding struct {
	Revision intent.ProposalRevision

	// ApprovalRef and Superseded are retained only as deprecated
	// source-compatibility fields for callers migrating from WF-RUN-023. They
	// are never consulted by Start; authorization and currency come only from
	// ProposalFacts and ApprovalFacts (TestTodo_WF_RUN_027 pins this boundary).
	// The Approved flag completed the same migration and was deleted.
	//
	// internal/intent/app.ExecuteIntent supplies both ports when composed with
	// an execution database, backed by
	// migration 00024's intent_decision and intent_relationship
	// (internal/intent/app.DurableProposalFacts). The fields remain solely so
	// those packages can migrate their fixtures independently; a request with
	// no facts ports is rejected before they can influence a start.
	ApprovalRef string
	Superseded  bool
}

// validate checks only the revision's own structural immutability: a
// [ProposalBinding] naming no revision, or a revision with no minted material
// digest, is never bindable regardless of which approval path a
// [StartRequest] uses.
func (b ProposalBinding) validate() error {
	if b.Revision.ProposalRevisionID == "" {
		return refuse(CodeInvalidRecord, "", "", "proposal binding names no revision")
	}
	if b.Revision.MaterialDigest.Digest == "" {
		return refuse(CodeMutableProposal, "", "",
			"proposal revision %s carries no minted material digest; only an immutable, digested revision may be bound",
			b.Revision.ProposalRevisionID)
	}
	return nil
}

// StartRequest is one caller's request to start a workflow instance from an
// immutable [ProposalBinding].
type StartRequest struct {
	TenantID uuid.UUID
	CellID   string

	// StartIdempotencyKey names one logical start attempt; see
	// [derivedStartInstanceID].
	StartIdempotencyKey string

	Resolver WorkflowResolver
	Versions version.Store

	Proposal ProposalBinding

	// ProposalFacts and ApprovalFacts resolve Proposal.Revision's supersession
	// and approval decisions from the caller-owned proposal and approval
	// stores (WF-RUN-027). Both are mandatory: Start has no caller-asserted
	// fallback and never treats ProposalBinding's deprecated fields as facts.
	ProposalFacts ProposalFacts
	ApprovalFacts ApprovalFacts

	// ConflictFacts and ConflictCandidate enable an optional pre-write
	// conflict boundary. When supplied, Start classifies the candidate through
	// the current conflict authority before resolving or writing the instance.
	ConflictFacts     ConflictFacts
	ConflictCandidate *conflict.Candidate

	// ExpectedIntentID, ExpectedTenant and BusinessSubjectRefs are the
	// caller's own declared context. A non-empty ExpectedIntentID or
	// ExpectedTenant that disagrees with the bound revision's own material
	// content refuses the start (WF-RUN-023's "mismatched tenant/intent"
	// RED case), and every subject in BusinessSubjectRefs must be one the
	// revision itself names.
	ExpectedIntentID      string
	ExpectedTenant        values.TenantId
	BusinessSubjectRefs   []string
	BusinessTransactionID *uuid.UUID

	ExecutionMode workflow.ExecutionMode
	CorrelationID string

	// ResolvedContext supplies, keyed by [workflow.ContextRequirement.Kind],
	// the reference proving one required-context read the start node
	// declares was actually resolved before this call. A declared kind with
	// no entry here is [CodeUnresolvedContext].
	ResolvedContext map[string]string

	// JoinDeclarations is passed through to [frontier.Seed] unchanged. It is
	// empty for every plan this phase compiles, none of which declare a JOIN.
	JoinDeclarations []frontier.JoinDeclaration

	// Workload, when non-nil, is WF-RUN-021's admission gate: the start is
	// refused OVERLOADED or ADMISSION_DEFERRED before any row is written when
	// its demand exceeds the resolved limits.
	Workload *WorkloadGate

	CreatedAt time.Time
}

func (r StartRequest) validate() error {
	switch {
	case r.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, "", "", "tenant id must not be the nil UUID")
	case r.CellID == "":
		return refuse(CodeInvalidRecord, "", "", "cell id is required")
	case r.StartIdempotencyKey == "":
		return refuse(CodeInvalidRecord, "", "", "start idempotency key is required")
	case r.Resolver == nil:
		return refuse(CodeInvalidRecord, "", "", "no workflow resolver supplied")
	case r.Versions == nil:
		return refuse(CodeInvalidRecord, "", "", "no version store supplied")
	case r.CorrelationID == "":
		return refuse(CodeInvalidRecord, "", "", "correlation id is required")
	case !modeValid(r.ExecutionMode):
		return refuse(CodeInvalidRecord, "", "", "execution mode %q is not declared", string(r.ExecutionMode))
	case r.CreatedAt.IsZero():
		return refuse(CodeInvalidRecord, "", "", "created_at must be supplied; this package never reads a wall clock")
	case len(r.BusinessSubjectRefs) == 0:
		return refuse(CodeInvalidRecord, "", "", "start names no business subject")
	case r.ProposalFacts == nil && r.ApprovalFacts == nil:
		return refuse(CodeInvalidRecord, "", "",
			"start requires ProposalFacts and ApprovalFacts; caller-asserted proposal flags are not authorization facts")
	case (r.ProposalFacts == nil) != (r.ApprovalFacts == nil):
		return refuse(CodeInvalidRecord, "", "",
			"start supplies one of ProposalFacts/ApprovalFacts without the other")
	case (r.ConflictFacts == nil) != (r.ConflictCandidate == nil):
		return refuse(CodeInvalidRecord, "", "",
			"start supplies one of ConflictFacts/ConflictCandidate without the other")
	}
	return r.Proposal.validate()
}

// resolveProposalFacts refuses [CodeSupersededProposal], [CodeUnapprovedProposal]
// or [CodeApprovalBindingMismatch] from stored facts, and returns the sorted
// ids of every APPROVED decision it relied on -- WF-RUN-027's replacement for
// [ProposalBinding]'s caller-asserted Approved/ApprovalRef/Superseded flags.
func resolveProposalFacts(ctx context.Context, ex Executor, req StartRequest) ([]string, error) {
	rev := req.Proposal.Revision
	supersession, err := req.ProposalFacts.Supersession(ctx, ex, req.TenantID, rev)
	if err != nil {
		return nil, wrap(CodeStorageFailed, "", "", err, "resolve proposal supersession for %s", rev.ProposalRevisionID)
	}
	if supersession.Superseded {
		return nil, refuse(CodeSupersededProposal, "", "",
			"proposal revision %s is superseded by %s", rev.ProposalRevisionID, supersession.SupersededByRevisionID)
	}

	decisions, err := req.ApprovalFacts.Decisions(ctx, ex, req.TenantID, rev)
	if err != nil {
		return nil, wrap(CodeStorageFailed, "", "", err, "resolve approval decisions for %s", rev.ProposalRevisionID)
	}
	var approved []string
	for _, d := range decisions {
		if d.ProposalDigest != rev.MaterialDigest.Digest {
			return nil, refuse(CodeApprovalBindingMismatch, "", "",
				"approval decision %s is bound to proposal digest %s, not %s naming revision %s",
				d.DecisionID, d.ProposalDigest, rev.MaterialDigest.Digest, rev.ProposalRevisionID)
		}
		if d.Invalidated {
			continue
		}
		if d.Outcome == ApprovalOutcomeApproved {
			approved = append(approved, d.DecisionID)
		}
	}
	if len(approved) == 0 {
		return nil, refuse(CodeUnapprovedProposal, "", "",
			"proposal revision %s carries no recorded approval", rev.ProposalRevisionID)
	}
	sort.Strings(approved)
	return approved, nil
}

// checkProposalAlignment refuses a start whose declared context disagrees
// with the material content of the bound revision.
func (r StartRequest) checkProposalAlignment() error {
	rev := r.Proposal.Revision
	if r.ExpectedIntentID != "" && rev.IntentID != r.ExpectedIntentID {
		return refuse(CodeIntentMismatch, "", "",
			"proposal revision binds intent %q, start expected %q", rev.IntentID, r.ExpectedIntentID)
	}
	if r.ExpectedTenant != "" && rev.Tenant != r.ExpectedTenant {
		return refuse(CodeTenantMismatch, "", "",
			"proposal revision binds tenant %q, start expected %q", string(rev.Tenant), string(r.ExpectedTenant))
	}
	bySubject := make(map[string]bool, len(rev.Subjects))
	for _, s := range rev.Subjects {
		bySubject[s.SubjectID] = true
	}
	for _, want := range r.BusinessSubjectRefs {
		if !bySubject[want] {
			return refuse(CodeSubjectMismatch, "", "",
				"start names subject %q, which proposal revision %s does not carry", want, rev.ProposalRevisionID)
		}
	}
	return nil
}

// checkResolvedContext refuses a start node whose declared required context
// the caller presents no resolution for.
func checkResolvedContext(node workflow.CompiledNode, resolved map[string]string) error {
	for _, req := range node.RequiredContext {
		if resolved[req.Kind] == "" {
			return refuse(CodeUnresolvedContext, node.ID, "",
				"start node declares required context %q with no resolution presented", req.Kind)
		}
	}
	return nil
}

// resolveActiveVersion resolves the pin, cross-checks the resolver's own
// compiled plan against the governance record's digest, and requires the
// record to be ACTIVE. All three are WF-RUN-023 RED cases: an unresolvable
// pin, a plan that does not match what governance approved, and a version
// that is not (or no longer) authorized to start.
func resolveActiveVersion(store version.Store, workflowID string, pin version.Pin, plan *workflow.CompiledWorkflow) (version.CompiledVersion, error) {
	cv, err := version.Resolve(store, workflowID, pin)
	if err != nil {
		return version.CompiledVersion{}, wrap(CodeVersionResolutionFailed, "", "", err,
			"resolve compiled version of %s", workflowID)
	}
	if plan == nil {
		return version.CompiledVersion{}, refuse(CodeWorkflowResolutionFailed, "", "",
			"workflow resolver returned no compiled plan for %s", workflowID)
	}
	if cv.CompiledPlanDigest != plan.Digest() {
		return version.CompiledVersion{}, refuse(CodeVersionPlanMismatch, "", "",
			"resolved version of %s carries digest %s; the resolver's own compiled plan digests to %s",
			workflowID, cv.CompiledPlanDigest, plan.Digest())
	}
	if cv.Status != version.StatusActive {
		return version.CompiledVersion{}, refuse(CodeVersionNotActive, "", "",
			"resolved version %s of %s is %s, not ACTIVE", cv.CompiledPlanDigest, workflowID, string(cv.Status))
	}
	return cv, nil
}

// startFingerprint is the canonicalized content one start binds: exactly the
// fields WF-RUN-023's GREEN clause names (proposal revision/digest, exact
// compiled version/digest, execution mode, control snapshot, subjects and
// correlation), plus the resolved-context proof. Its digest is stored as the
// instance's own InputRef, which is what lets a retry under the same start
// idempotency key be compared against "the same bound digests" without a
// dedicated idempotency table: [Instance.InputRef] already exists for
// exactly this purpose (WF-RUN-001: "the digest of the declared workflow
// input snapshot").
type startFingerprint struct {
	WorkflowID            string            `json:"workflow_id"`
	CompiledPlanDigest    string            `json:"compiled_plan_digest"`
	ProposalRevisionID    string            `json:"proposal_revision_id"`
	ProposalDigest        string            `json:"proposal_digest"`
	ExecutionMode         string            `json:"execution_mode"`
	ControlSnapshotDigest string            `json:"control_snapshot_digest"`
	Subjects              []string          `json:"subjects"`
	CorrelationID         string            `json:"correlation_id"`
	ResolvedContext       map[string]string `json:"resolved_context,omitempty"`
}

func computeStartFingerprint(workflowID string, plan *workflow.CompiledWorkflow, req StartRequest) string {
	subjects := append([]string(nil), req.BusinessSubjectRefs...)
	sort.Strings(subjects)
	fp := startFingerprint{
		WorkflowID:            workflowID,
		CompiledPlanDigest:    plan.Digest(),
		ProposalRevisionID:    req.Proposal.Revision.ProposalRevisionID,
		ProposalDigest:        req.Proposal.Revision.MaterialDigest.Digest,
		ExecutionMode:         string(req.ExecutionMode),
		ControlSnapshotDigest: canonicalDigest(controlSnapshotDigestProfile, req.Proposal.Revision.ControlSnapshots),
		Subjects:              subjects,
		CorrelationID:         req.CorrelationID,
		ResolvedContext:       req.ResolvedContext,
	}
	return canonicalDigest(startFingerprintDigestProfile, fp)
}

// StartReceipt is everything [Start] produced: the instance it bound (or
// replayed) and the version it pinned.
type StartReceipt struct {
	TenantID           uuid.UUID
	InstanceID         uuid.UUID
	WorkflowID         string
	WorkflowVersion    uint32
	CompiledPlanDigest string
	SemanticVersion    string
	ExecutionMode      workflow.ExecutionMode
	InstanceVersion    int64
	Frontier           []string
	CorrelationID      string
	// Replay reports that this receipt was reconstructed from an
	// already-existing instance under an identical start idempotency key and
	// identical bound digests, rather than freshly created.
	Replay    bool
	CreatedAt time.Time

	// ApprovalDecisionIDs names, sorted, every approval decision id [Start]
	// relied on to admit the bound proposal revision (WF-RUN-027). It is the
	// approval store's own decision ids. There is no caller-asserted fallback.
	ApprovalDecisionIDs []string

	digest string
}

// Digest is the receipt's content identity.
func (r StartReceipt) Digest() string { return r.digest }

func newStartReceipt(inst Instance, cv version.CompiledVersion, replay bool, approvalDecisionIDs []string) StartReceipt {
	rec := StartReceipt{
		TenantID:            inst.TenantID,
		InstanceID:          inst.InstanceID,
		WorkflowID:          inst.WorkflowID,
		WorkflowVersion:     inst.WorkflowVersion,
		CompiledPlanDigest:  inst.CompiledPlanHash,
		SemanticVersion:     cv.SemanticVersion,
		ExecutionMode:       inst.ExecutionMode,
		InstanceVersion:     inst.InstanceVersion,
		Frontier:            append([]string(nil), inst.CurrentNodeIDs...),
		CorrelationID:       inst.CorrelationID,
		Replay:              replay,
		CreatedAt:           inst.CreatedAt,
		ApprovalDecisionIDs: append([]string(nil), approvalDecisionIDs...),
	}
	rec.digest = computeStartReceiptDigest(rec)
	return rec
}

// Start binds an immutable, approved, non-superseded ProposalRevision to a
// new workflow instance under an ACTIVE compiled version resolved by exact
// pin, and persists the frontier exactly as [frontier.Seed] produces it.
//
// It is one atomic unit of work: every write happens through tx, which the
// caller began and will commit or roll back. An identical retry (same start
// idempotency key, same bound digests) returns the original instance and its
// initial READY frontier without writing anything a second time; a retry
// under the same key with any bound digest changed is [CodeStartConflict].
func Start(ctx context.Context, tx Executor, req StartRequest) (ret0 StartReceipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.start", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return StartReceipt{}, err
	}
	if err := req.checkProposalAlignment(); err != nil {
		return StartReceipt{}, err
	}
	approvalDecisionIDs, err := resolveProposalFacts(ctx, tx, req)
	if err != nil {
		return StartReceipt{}, err
	}
	if req.ConflictFacts != nil {
		if _, err := CheckConflict(ctx, tx, ConflictCheckRequest{
			TenantID: req.TenantID, SubjectRefs: req.BusinessSubjectRefs,
			Candidate: *req.ConflictCandidate,
		}, req.ConflictFacts); err != nil {
			return StartReceipt{}, err
		}
	}

	sel, err := req.Resolver.ResolveWorkflow(ctx, req)
	if err != nil {
		return StartReceipt{}, wrap(CodeWorkflowResolutionFailed, "", "", err, "resolve workflow selection")
	}
	if sel.WorkflowID == "" || sel.Plan == nil {
		return StartReceipt{}, refuse(CodeWorkflowResolutionFailed, "", "",
			"workflow resolver returned no workflow id or compiled plan")
	}

	cv, err := resolveActiveVersion(req.Versions, sel.WorkflowID, sel.Pin, sel.Plan)
	if err != nil {
		return StartReceipt{}, err
	}

	startNode, ok := sel.Plan.Node(sel.Plan.StartNodeID)
	if !ok {
		return StartReceipt{}, refuse(CodeInvalidRecord, "", sel.Plan.StartNodeID,
			"compiled plan declares a start node it does not carry")
	}
	if err := checkResolvedContext(startNode, req.ResolvedContext); err != nil {
		return StartReceipt{}, err
	}

	seeded, err := frontier.Seed(sel.Plan, "", req.JoinDeclarations...)
	if err != nil {
		return StartReceipt{}, wrap(CodeInvalidRecord, "", "", err, "seed initial frontier")
	}

	instanceID := derivedStartInstanceID(req.TenantID, sel.WorkflowID, req.StartIdempotencyKey)
	fingerprint := computeStartFingerprint(sel.WorkflowID, sel.Plan, req)

	inst, err := NewInstance(req.TenantID, instanceID, req.CellID, sel.Plan, req.ExecutionMode,
		fingerprint, req.CorrelationID, req.CreatedAt)
	if err != nil {
		return StartReceipt{}, err
	}
	if !equalStringSets(inst.CurrentNodeIDs, seeded.Frontier) {
		return StartReceipt{}, refuse(CodeInvalidRecord, instanceID.String(), "",
			"seeded frontier %v does not match the new instance's own start frontier %v",
			seeded.Frontier, inst.CurrentNodeIDs)
	}
	inst.BusinessSubjectRefs = append([]string(nil), req.BusinessSubjectRefs...)
	inst.BusinessTransactionID = req.BusinessTransactionID

	if err := admitWorkload(ctx, tx, req, sel.Plan, sel.WorkflowID, instanceID); err != nil {
		return StartReceipt{}, err
	}

	store := Store{}
	stored, created, err := insertInstanceIfAbsent(ctx, tx, inst)
	if err != nil {
		return StartReceipt{}, err
	}
	if !created {
		// Another (or this same) caller already holds the row this start
		// idempotency key derives: WF-RUN-023's retry path, not a conflict at
		// the SQL level -- see [insertInstanceIfAbsent].
		return replayStart(ctx, tx, req, instanceID, fingerprint, approvalDecisionIDs)
	}

	nextVersion := stored.InstanceVersion
	for _, ns := range seeded.Nodes {
		node, ok := sel.Plan.Node(ns.NodeID)
		if !ok {
			return StartReceipt{}, refuse(CodeInvalidRecord, instanceID.String(), ns.NodeID,
				"seeded frontier names a node the plan does not declare")
		}
		ne := NewNodeExecution(req.TenantID, instanceID, ns.NodeID, 1, node.Type, NodeStatus(ns.State))
		_, bumped, err := store.RecordNodeExecution(ctx, tx, ne, nextVersion)
		if err != nil {
			return StartReceipt{}, err
		}
		nextVersion = bumped
	}

	final, err := store.LoadInstance(ctx, tx, req.TenantID, instanceID)
	if err != nil {
		return StartReceipt{}, err
	}
	return newStartReceipt(final, cv, false, approvalDecisionIDs), nil
}

// replayStart handles a derived instance id that already exists: the
// idempotent-retry half of [Start]'s contract.
func replayStart(
	ctx context.Context, tx Executor, req StartRequest, instanceID uuid.UUID, fingerprint string, approvalDecisionIDs []string,
) (StartReceipt, error) {
	store := Store{}
	existing, err := store.LoadInstance(ctx, tx, req.TenantID, instanceID)
	if err != nil {
		return StartReceipt{}, err
	}
	if existing.InputRef != fingerprint {
		return StartReceipt{}, refuse(CodeStartConflict, instanceID.String(), "",
			"start idempotency key %q is already bound to a request with different digests", req.StartIdempotencyKey)
	}
	cv, err := version.Resolve(req.Versions, existing.WorkflowID, version.Pin{CompiledPlanDigest: existing.CompiledPlanHash})
	if err != nil {
		return StartReceipt{}, wrap(CodeVersionResolutionFailed, instanceID.String(), "", err,
			"resolve compiled version for replay")
	}
	return newStartReceipt(existing, cv, true, approvalDecisionIDs), nil
}

// insertInstanceIfAbsent inserts inst unless its (tenant_id, instance_id)
// already exists, in which case it changes nothing and reports created=false.
//
// [Store.CreateInstance] cannot be reused here: it issues a bare INSERT ...
// RETURNING, and on Postgres a statement that violates a constraint aborts
// the rest of the transaction, so a caller could not follow a failed insert
// with a read of the row that was already there. This mirrors
// internal/transaction/idempotency's own PostgresStore.Reserve instead:
// INSERT ... ON CONFLICT (the table's own primary key) DO NOTHING RETURNING
// turns "already exists" into an ordinary zero-row result rather than an
// error, so the transaction stays healthy and this package never inspects a
// driver-specific error code (tools/policy/libfirewall restricts
// github.com/jackc/pgx/v5 to the data-plane packages; this stays port-only).
// It reuses [instanceColumns] and [scanInstance] from store.go, in the same
// package, rather than duplicating that column list a second time.
func insertInstanceIfAbsent(ctx context.Context, ex Executor, inst Instance) (Instance, bool, error) {
	if err := inst.Validate(); err != nil {
		return Instance{}, false, err
	}
	dims, err := json.Marshal(inst.CompletionDimensions)
	if err != nil {
		return Instance{}, false, wrap(CodeInvalidRecord, inst.InstanceID.String(), "", err,
			"encode completion dimensions")
	}
	row := ex.QueryRow(ctx, `
		INSERT INTO workflow_instance (`+instanceColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (tenant_id, instance_id) DO NOTHING
		RETURNING `+instanceColumns,
		inst.TenantID, inst.InstanceID, inst.CellID, inst.WorkflowID, int32(inst.WorkflowVersion),
		inst.CompiledPlanHash, textArray(inst.BusinessSubjectRefs), inst.BusinessTransactionID,
		string(inst.ExecutionMode), string(inst.RuntimeStatus), dims, inst.InputRef,
		inst.VariableRevisionHead, textArray(inst.CurrentNodeIDs),
		nullableText(inst.EffectiveContextRef), nullableText(inst.LastCheckpointRef),
		inst.InstanceVersion, inst.CorrelationID,
		inst.CreatedAt.UTC(), utcOrNil(inst.StartedAt), utcOrNil(inst.CompletedAt))

	stored, err := scanInstance(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Instance{}, false, nil
		}
		return Instance{}, false, wrap(CodeStorageFailed, inst.InstanceID.String(), "", err,
			"insert workflow instance")
	}
	return stored, true, nil
}

// equalStringSets reports whether a and b carry the same elements, ignoring
// order -- both are already-sorted small slices in every caller, so a direct
// sorted comparison is enough.
func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}
