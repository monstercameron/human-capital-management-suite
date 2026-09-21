package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// CodeFingerprintDrift reports a stored execution fingerprint whose content no
// longer digests to the digest recorded with it (WF-RUN-038).
const CodeFingerprintDrift = "EXECUTION_FINGERPRINT_DRIFT"

// FingerprintKind names one class of versioned artifact an execution
// fingerprint records (specs/workflow-runtime.md "Execution Fingerprint").
type FingerprintKind string

// The component kinds a node fingerprint carries. Every one is read from the
// compiled plan the instance pinned, or from the runtime version its execution
// context pinned; none is looked up at run time.
const (
	// FingerprintWorkflow is "<workflow_id>@v<version>".
	FingerprintWorkflow FingerprintKind = "WORKFLOW"
	// FingerprintPlan is the compiled-plan digest.
	FingerprintPlan FingerprintKind = "COMPILED_PLAN"
	// FingerprintCompiler is the compiler version that produced the plan.
	FingerprintCompiler FingerprintKind = "COMPILER"
	// FingerprintRuntime is the runtime version the instance pinned.
	FingerprintRuntime FingerprintKind = "RUNTIME"
	// FingerprintCapability is a bound capability (connector) "<id>@v<version>".
	FingerprintCapability FingerprintKind = "CAPABILITY"
	// FingerprintCapabilityManifest is a bound capability's manifest digest.
	FingerprintCapabilityManifest FingerprintKind = "CAPABILITY_MANIFEST"
	// FingerprintPolicy is a policy reference: authorization scope,
	// idempotency, retry backoff, obligation, approval, data-access, output
	// validation, and the plan's failure/cancellation/migration/retention.
	FingerprintPolicy FingerprintKind = "POLICY"
	// FingerprintEvaluator is a decision evaluator "<ref>@v<version>".
	FingerprintEvaluator FingerprintKind = "EVALUATOR"
	// FingerprintRule is a decision rule reference.
	FingerprintRule FingerprintKind = "RULE"
	// FingerprintMapping is a transform (mapping) "<ref>@v<version>".
	FingerprintMapping FingerprintKind = "MAPPING"
	// FingerprintResolver is a bound resolver "<id>@<version>" (WF-COMP-007).
	FingerprintResolver FingerprintKind = "RESOLVER"
	// FingerprintTimeoutPolicy is a bound timeout policy "<id>@<version>"
	// (WF-COMP-007).
	FingerprintTimeoutPolicy FingerprintKind = "TIMEOUT_POLICY"
	// FingerprintCompensation is a bound compensation "<id>@<version>"
	// (WF-COMP-007).
	FingerprintCompensation FingerprintKind = "COMPENSATION"
	// FingerprintSchema is a data or event schema "<schema_id>@v<version>".
	FingerprintSchema FingerprintKind = "SCHEMA"
	// FingerprintReferenceData is reference data a node binds: a calendar
	// "<ref>@<version>" or a time-zone database "tzdb@<version>".
	FingerprintReferenceData FingerprintKind = "REFERENCE_DATA"
)

var fingerprintKinds = map[FingerprintKind]bool{
	FingerprintWorkflow: true, FingerprintPlan: true, FingerprintCompiler: true, FingerprintRuntime: true,
	FingerprintCapability: true, FingerprintCapabilityManifest: true, FingerprintPolicy: true,
	FingerprintEvaluator: true, FingerprintRule: true, FingerprintMapping: true, FingerprintSchema: true,
	FingerprintReferenceData: true,
	FingerprintResolver:      true, FingerprintTimeoutPolicy: true, FingerprintCompensation: true,
}

// Valid reports whether k is a declared component kind.
func (k FingerprintKind) Valid() bool { return fingerprintKinds[k] }

// FingerprintComponent is one versioned artifact a node execution used.
type FingerprintComponent struct {
	Kind FingerprintKind `json:"kind"`
	Ref  string          `json:"ref"`
}

// token is the stored and indexed spelling. A kind never contains '=', so the
// first '=' splits a token unambiguously even when the ref contains one.
func (c FingerprintComponent) token() string { return string(c.Kind) + "=" + c.Ref }

func componentFromToken(token string) (FingerprintComponent, bool) {
	kind, ref, ok := strings.Cut(token, "=")
	if !ok || !FingerprintKind(kind).Valid() || ref == "" {
		return FingerprintComponent{}, false
	}
	return FingerprintComponent{Kind: FingerprintKind(kind), Ref: ref}, true
}

// NodeFingerprint is the version fingerprint of one node execution attempt.
type NodeFingerprint struct {
	NodeID             string                 `json:"node_id"`
	StepType           workflow.StepType      `json:"step_type"`
	WorkflowID         string                 `json:"workflow_id"`
	WorkflowVersion    uint32                 `json:"workflow_version"`
	CompiledPlanDigest string                 `json:"compiled_plan_digest"`
	CompilerVersion    string                 `json:"compiler_version"`
	RuntimeVersion     string                 `json:"runtime_version"`
	Components         []FingerprintComponent `json:"components"`
}

// Digest is the fingerprint's canonical content digest.
func (f NodeFingerprint) Digest() string {
	body, _ := json.Marshal(f)
	sum := sha256.Sum256(append([]byte("hcmnext.workflow.NodeFingerprint/v1\n"), body...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Touches reports whether the fingerprint records the component (kind, ref).
func (f NodeFingerprint) Touches(kind FingerprintKind, ref string) bool {
	for _, c := range f.Components {
		if c.Kind == kind && c.Ref == ref {
			return true
		}
	}
	return false
}

// Validate refuses a fingerprint missing a version the index relies on.
func (f NodeFingerprint) Validate() error {
	switch {
	case f.NodeID == "" || f.StepType == "":
		return refuse(CodeInvalidRecord, "", f.NodeID, "execution fingerprint names no node or step type")
	case f.WorkflowID == "" || f.CompiledPlanDigest == "" || f.CompilerVersion == "" || f.RuntimeVersion == "":
		return refuse(CodeInvalidRecord, "", f.NodeID,
			"execution fingerprint names no workflow, compiled plan digest, compiler or runtime version")
	case len(f.Components) == 0:
		return refuse(CodeInvalidRecord, "", f.NodeID, "execution fingerprint records no components")
	}
	return nil
}

// DeriveNodeFingerprint derives the fingerprint of nodeID from the compiled
// plan the instance pinned and the runtime version its execution context
// pinned. It is pure and reads only compiled facts, so re-deriving it for the
// same plan digest, node and runtime version always yields the same digest.
func DeriveNodeFingerprint(plan *workflow.CompiledWorkflow, nodeID, runtimeVersion string) (NodeFingerprint, error) {
	if plan == nil {
		return NodeFingerprint{}, refuse(CodeInvalidRecord, "", nodeID, "no compiled plan supplied")
	}
	node, ok := plan.Node(nodeID)
	if !ok {
		return NodeFingerprint{}, refuse(CodeInvalidRecord, "", nodeID, "compiled plan %s does not declare this node", plan.WorkflowID)
	}
	f := NodeFingerprint{
		NodeID: node.ID, StepType: node.Type,
		WorkflowID: plan.WorkflowID, WorkflowVersion: plan.Version, CompiledPlanDigest: plan.Digest(),
		CompilerVersion: plan.CompilerVersion, RuntimeVersion: runtimeVersion,
	}
	set := map[FingerprintComponent]bool{}
	add := func(kind FingerprintKind, ref string) {
		if ref = strings.TrimSpace(ref); ref != "" {
			set[FingerprintComponent{Kind: kind, Ref: ref}] = true
		}
	}
	addPolicies := func(refs ...string) {
		for _, r := range refs {
			add(FingerprintPolicy, r)
		}
	}
	addSchema := func(s workflow.SchemaRef) {
		if s.SchemaID != "" {
			add(FingerprintSchema, versioned(s.SchemaID, s.Version))
		}
	}
	// addReference records one WF-COMP-007 published binding under its own
	// kind, so a bad resolver, timeout-policy or compensation release is a
	// blast-radius query away. The compiler resolves these against published
	// registries; a nil binding means the plan binds none.
	addReference := func(kind FingerprintKind, r *workflow.ResolvedReference) {
		if r == nil {
			return
		}
		id, version := strings.TrimSpace(r.ID), strings.TrimSpace(r.Version)
		if id != "" && version != "" {
			add(kind, id+"@"+version)
		}
	}

	add(FingerprintWorkflow, versioned(plan.WorkflowID, plan.Version))
	add(FingerprintPlan, f.CompiledPlanDigest)
	add(FingerprintCompiler, f.CompilerVersion)
	add(FingerprintRuntime, runtimeVersion)
	addPolicies(plan.FailurePolicyRef, plan.CancellationPolicyRef, plan.MigrationPolicyRef, plan.RetentionPolicyRef)
	addReference(FingerprintResolver, node.ResolverRef)
	addReference(FingerprintTimeoutPolicy, node.TimeoutPolicy)
	addReference(FingerprintCompensation, node.CompensationRef)

	addSchema(node.InputSchema)
	addSchema(node.OutputSchema)
	addPolicies(node.Governance.DataAccessManifestRef, node.Governance.OutputValidatorRef)
	addPolicies(node.Governance.ObligationRefs...)
	addPolicies(node.Governance.ApprovalRequirements...)
	if node.Retry != nil {
		addPolicies(node.Retry.BackoffRef)
	}
	if c := node.Capability; c != nil {
		add(FingerprintCapability, versioned(c.ID, c.Version))
		add(FingerprintCapabilityManifest, c.Digest)
		addSchema(c.RequestSchema)
		addSchema(c.ResponseSchema)
		addPolicies(c.AuthZScopeRef, c.IdempotencyPolicyRef)
	}
	if d := node.Decision; d != nil {
		add(FingerprintEvaluator, versioned(d.EvaluatorRef, d.EvaluatorVersion))
		add(FingerprintRule, d.RuleRef)
		// The resolved rule target names the exact published table the bare
		// RuleRef spelling resolved to; both spellings are indexed so an
		// operator holding either one finds the executions.
		addReference(FingerprintRule, d.Rule)
	}
	if t := node.Transform; t != nil {
		add(FingerprintMapping, versioned(t.TransformRef, t.Version))
		// Every compiled lookup is pinned (the compiler refuses an unpinned
		// one), so the snapshot digest identifies the reference data read.
		for _, l := range t.Lookups {
			ref, snap := strings.TrimSpace(l.Ref), strings.TrimSpace(l.SnapshotDigest)
			if ref != "" && snap != "" {
				add(FingerprintReferenceData, "lookup:"+ref+"@"+snap)
			}
		}
	}
	if w := node.Wait; w != nil {
		if w.CalendarRef != "" {
			add(FingerprintReferenceData, w.CalendarRef+"@"+w.CalendarVersion)
		}
		if w.ZoneTzdbVersion != "" {
			add(FingerprintReferenceData, "tzdb@"+w.ZoneTzdbVersion)
		}
	}
	if s := node.Signal; s != nil {
		addSchema(s.ExpectedSchemaRef)
	}

	f.Components = make([]FingerprintComponent, 0, len(set))
	for c := range set {
		f.Components = append(f.Components, c)
	}
	sort.Slice(f.Components, func(i, j int) bool { return f.Components[i].token() < f.Components[j].token() })
	return f, nil
}

func versioned(ref string, version uint32) string {
	if ref == "" {
		return ""
	}
	return ref + "@v" + strconv.FormatUint(uint64(version), 10)
}

// nodeFingerprinter records the fingerprint of every node execution one Start
// or Advance inserts, against the plan and runtime version the instance
// pinned.
type nodeFingerprinter struct {
	plan           *workflow.CompiledWorkflow
	runtimeVersion string
	recordedAt     time.Time
}

// pinnedFingerprinter builds the recorder for an advancement: the runtime
// version comes from the execution context the instance pinned at start. An
// instance started before execution contexts were recorded falls back to
// this runtime's own [RuntimeVersion], the only version that can be running it.
func pinnedFingerprinter(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID,
	plan *workflow.CompiledWorkflow, recordedAt time.Time,
) (nodeFingerprinter, error) {
	var pinned *string
	err := ex.QueryRow(ctx, `SELECT context->>'runtime_version' FROM workflow_execution_context WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instanceID).Scan(&pinned)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return nodeFingerprinter{}, wrap(CodeStorageFailed, instanceID.String(), "", err, "read pinned runtime version")
	}
	version := RuntimeVersion
	if pinned != nil && *pinned != "" {
		version = *pinned
	}
	return nodeFingerprinter{plan: plan, runtimeVersion: version, recordedAt: recordedAt}, nil
}

// record derives and inserts the fingerprint of ne in the caller's
// transaction.
func (r nodeFingerprinter) record(ctx context.Context, ex Executor, ne NodeExecution) error {
	f, err := DeriveNodeFingerprint(r.plan, ne.NodeID, r.runtimeVersion)
	if err != nil {
		return err
	}
	if err := f.Validate(); err != nil {
		return err
	}
	tokens := make([]string, len(f.Components))
	for i, c := range f.Components {
		tokens[i] = c.token()
	}
	if _, err := ex.Exec(ctx, `INSERT INTO workflow_execution_fingerprint (tenant_id, instance_id, node_id, attempt, step_type,
			workflow_id, workflow_version, compiled_plan_digest, compiler_version, runtime_version,
			components, fingerprint_digest, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		ne.TenantID, ne.InstanceID, ne.NodeID, int32(ne.Attempt), string(f.StepType),
		f.WorkflowID, int32(f.WorkflowVersion), f.CompiledPlanDigest, f.CompilerVersion, f.RuntimeVersion,
		tokens, f.Digest(), r.recordedAt.UTC()); err != nil {
		return wrap(CodeStorageFailed, ne.InstanceID.String(), ne.NodeID, err, "record execution fingerprint for attempt %d", ne.Attempt)
	}
	return nil
}

// LoadNodeFingerprint returns the fingerprint recorded for one node execution
// attempt and refuses it with [CodeFingerprintDrift] unless its stored content
// still digests to the digest recorded with it. found is false for an attempt
// recorded before fingerprints were.
func LoadNodeFingerprint(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, nodeID string, attempt int) (ret0 NodeFingerprint, found bool, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.load_node_fingerprint", tenantID, instanceID, nodeID, attempt)
	defer func() { observe.DoneWith(obsOp, retErr, ret0.Digest()) }()
	var (
		f               NodeFingerprint
		stepType        string
		workflowVersion int32
		tokens          []string
		stored          string
	)
	err := ex.QueryRow(ctx, `SELECT node_id, step_type, workflow_id, workflow_version, compiled_plan_digest, compiler_version,
			runtime_version, components, fingerprint_digest
		FROM workflow_execution_fingerprint WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND attempt = $4`,
		tenantID, instanceID, nodeID, int32(attempt)).Scan(&f.NodeID, &stepType, &f.WorkflowID, &workflowVersion,
		&f.CompiledPlanDigest, &f.CompilerVersion, &f.RuntimeVersion, &tokens, &stored)
	if errors.Is(err, dbport.ErrNoRows) {
		return NodeFingerprint{}, false, nil
	}
	if err != nil {
		return NodeFingerprint{}, false, wrap(CodeStorageFailed, instanceID.String(), nodeID, err, "load execution fingerprint")
	}
	f.StepType, f.WorkflowVersion = workflow.StepType(stepType), uint32(workflowVersion)
	f.Components = make([]FingerprintComponent, 0, len(tokens))
	for _, token := range tokens {
		c, ok := componentFromToken(token)
		if !ok {
			return NodeFingerprint{}, false, refuse(CodeFingerprintDrift, instanceID.String(), nodeID,
				"stored fingerprint component %q is not a declared KIND=ref token", token)
		}
		f.Components = append(f.Components, c)
	}
	if f.Digest() != stored {
		return NodeFingerprint{}, false, refuse(CodeFingerprintDrift, instanceID.String(), nodeID,
			"stored fingerprint for attempt %d no longer digests to %s", attempt, stored)
	}
	return f, true, nil
}

// InstancesTouching is the blast-radius index: the ids of every instance of
// tenantID with a node execution whose fingerprint records the component
// (kind, ref), in ascending order. Attempts that were SKIPPED never ran, so
// they touched nothing and are not counted.
func InstancesTouching(ctx context.Context, ex Executor, tenantID uuid.UUID, kind FingerprintKind, ref string) (ret0 []uuid.UUID, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.instances_touching", tenantID, string(kind), ref)
	defer func() { observe.DoneWith(obsOp, retErr, len(ret0)) }()
	if tenantID == uuid.Nil {
		return nil, refuse(CodeInvalidRecord, "", "", "tenant id must not be the nil UUID")
	}
	if !kind.Valid() || strings.TrimSpace(ref) == "" {
		return nil, refuse(CodeInvalidRecord, "", "", "fingerprint component %q=%q is not a declared kind with a reference", string(kind), ref)
	}
	rows, err := ex.Query(ctx, `SELECT DISTINCT f.instance_id
		FROM workflow_execution_fingerprint f
		JOIN workflow_node_execution n
		  ON n.tenant_id = f.tenant_id AND n.instance_id = f.instance_id AND n.node_id = f.node_id AND n.attempt = f.attempt
		WHERE f.tenant_id = $1 AND f.components @> ARRAY[$2]::text[] AND n.status <> 'SKIPPED'
		ORDER BY f.instance_id`,
		tenantID, FingerprintComponent{Kind: kind, Ref: ref}.token())
	if err != nil {
		return nil, wrap(CodeStorageFailed, "", "", err, "query instances touching %s", string(kind))
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, wrap(CodeStorageFailed, "", "", err, "scan touched instance")
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, "", "", err, "iterate touched instances")
	}
	return out, nil
}
