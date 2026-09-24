package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// WF-EXT-004: the two durable data-plane artifacts a mapping resolves
// against on the EXECUTE path -- the run's typed input and bound context
// documents, recorded once at start, and each node's typed output, recorded
// once per attempt.
// They mirror node_inputs.go's own NodeInputArtifact exactly: append-only,
// re-digested on read, an idempotent insert that refuses a conflicting
// second write instead of replacing it.

// CodeWorkflowInputConflict reports a second, different workflow input
// document for an instance that already recorded one. A replayed start under
// the same idempotency key with the identical document is a no-op; one with a
// different document is refused rather than silently rebinding the run's
// inputs.
const CodeWorkflowInputConflict = "WORKFLOW_INPUT_ARTIFACT_CONFLICT"

// CodeNodeOutputConflict reports a second, different output artifact for a
// node attempt that already recorded one, for the same reason
// [CodeNodeInputConflict] refuses a second input artifact: an attempt's
// recorded outputs are historical evidence, never replaced.
const CodeNodeOutputConflict = "NODE_OUTPUT_ARTIFACT_CONFLICT"

const (
	workflowInputArtifactDigestProfile = "hcmnext.workflow.runtime.WorkflowInputArtifact/v1"
	nodeOutputArtifactDigestProfile    = "hcmnext.workflow.runtime.NodeOutputArtifact/v1"
)

// TypedArtifactValue is one typed value recorded in a data-plane artifact,
// as its canonical text (a decimal at its declared scale, "true"/"false", a
// string verbatim) plus the declared type the consumer type-checks it
// against. Carrying the type here, rather than only in the compiled plan a
// reader must separately hold, is what lets [workflow.ResolveMappings] check
// TYPE_MISMATCH from the artifact alone.
type TypedArtifactValue struct {
	Path  string             `json:"path"`
	Type  workflow.ValueType `json:"type"`
	Value string             `json:"value"`
}

// TypedContextSnapshot binds a typed context document to the reference the
// caller resolved before starting the workflow. The snapshot's values and
// reference are covered by WorkflowInputArtifact.Digest.
type TypedContextSnapshot struct {
	Kind      string               `json:"kind"`
	Reference string               `json:"reference"`
	Values    []TypedArtifactValue `json:"values"`
}

func typedArtifactValuesOf(outs []workflow.TypedOutput) []TypedArtifactValue {
	out := make([]TypedArtifactValue, 0, len(outs))
	for _, o := range outs {
		out = append(out, TypedArtifactValue{Path: o.Path, Type: o.Value.Type, Value: o.Value.Text})
	}
	return out
}

func typedOutputsOf(vals []TypedArtifactValue) []workflow.TypedOutput {
	out := make([]workflow.TypedOutput, 0, len(vals))
	for _, v := range vals {
		out = append(out, workflow.TypedOutput{Path: v.Path, Value: workflow.TypedValue{Type: v.Type, Text: v.Value}})
	}
	return out
}

// WorkflowInputArtifact is the durable record of the typed input and context
// documents one instance was started with. It is written once, inside the
// same transaction as [Start], and is the only source [workflow.ResolveMappings]
// reads WORKFLOW_INPUT and CONTEXT mappings from on the durable path.
type WorkflowInputArtifact struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	InstanceID uuid.UUID `json:"instance_id"`
	PlanDigest string    `json:"plan_digest"`

	Inputs   []TypedArtifactValue   `json:"inputs"`
	Contexts []TypedContextSnapshot `json:"contexts,omitempty"`

	RecordedAt time.Time `json:"recorded_at"`
}

// Validate refuses an artifact that could not be read back correctly.
func (a WorkflowInputArtifact) Validate() error {
	instance := a.InstanceID.String()
	switch {
	case a.TenantID == uuid.Nil || a.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, instance, "", "workflow input artifact needs a tenant and an instance")
	case a.PlanDigest == "":
		return refuse(CodeInvalidRecord, instance, "", "workflow input artifact pins no compiled plan")
	case len(a.Inputs) == 0 && len(a.Contexts) == 0:
		return refuse(CodeInvalidRecord, instance, "", "workflow input artifact records no inputs or contexts")
	case a.RecordedAt.IsZero():
		return refuse(CodeInvalidRecord, instance, "", "workflow input artifact carries no recorded instant")
	}
	seen := map[string]bool{}
	for _, in := range a.Inputs {
		if strings.TrimSpace(in.Path) == "" || seen[in.Path] {
			return refuse(CodeInvalidRecord, instance, "", "workflow input paths must be present and unique (%q)", in.Path)
		}
		seen[in.Path] = true
	}
	contextKinds := map[string]bool{}
	for _, snapshot := range a.Contexts {
		if strings.TrimSpace(snapshot.Kind) == "" || contextKinds[snapshot.Kind] || strings.TrimSpace(snapshot.Reference) == "" || len(snapshot.Values) == 0 {
			return refuse(CodeInvalidRecord, instance, "", "context snapshots need a unique kind, reference and values")
		}
		contextKinds[snapshot.Kind] = true
		paths := map[string]bool{}
		for _, value := range snapshot.Values {
			if strings.TrimSpace(value.Path) == "" || paths[value.Path] {
				return refuse(CodeInvalidRecord, instance, "", "context %s paths must be present and unique (%q)", snapshot.Kind, value.Path)
			}
			paths[value.Path] = true
		}
	}
	return nil
}

func (a WorkflowInputArtifact) canonical() WorkflowInputArtifact {
	a.Inputs = append([]TypedArtifactValue(nil), a.Inputs...)
	sort.Slice(a.Inputs, func(i, j int) bool { return a.Inputs[i].Path < a.Inputs[j].Path })
	a.Contexts = append([]TypedContextSnapshot(nil), a.Contexts...)
	for i := range a.Contexts {
		a.Contexts[i].Values = append([]TypedArtifactValue(nil), a.Contexts[i].Values...)
		sort.Slice(a.Contexts[i].Values, func(j, k int) bool { return a.Contexts[i].Values[j].Path < a.Contexts[i].Values[k].Path })
	}
	sort.Slice(a.Contexts, func(i, j int) bool { return a.Contexts[i].Kind < a.Contexts[j].Kind })
	a.RecordedAt = a.RecordedAt.UTC()
	return a
}

// Digest is the artifact's canonical content identity.
func (a WorkflowInputArtifact) Digest() string {
	return "sha256:" + canonicalDigest(workflowInputArtifactDigestProfile, a.canonical())
}

// Clone returns a deep copy.
func (a WorkflowInputArtifact) Clone() WorkflowInputArtifact {
	a.Inputs = append([]TypedArtifactValue(nil), a.Inputs...)
	a.Contexts = append([]TypedContextSnapshot(nil), a.Contexts...)
	for i := range a.Contexts {
		a.Contexts[i].Values = append([]TypedArtifactValue(nil), a.Contexts[i].Values...)
	}
	return a
}

// Input returns the recorded value at path.
func (a WorkflowInputArtifact) Input(path string) (TypedArtifactValue, bool) {
	for _, in := range a.Inputs {
		if in.Path == path {
			return in, true
		}
	}
	return TypedArtifactValue{}, false
}

// Context returns one value from the context snapshot of the requested kind.
func (a WorkflowInputArtifact) Context(kind, path string) (TypedArtifactValue, bool) {
	for _, snapshot := range a.Contexts {
		if snapshot.Kind != kind {
			continue
		}
		for _, value := range snapshot.Values {
			if value.Path == path {
				return value, true
			}
		}
	}
	return TypedArtifactValue{}, false
}

// RecordWorkflowInputs appends the pinned input and context documents of one instance and
// returns its digest. It is meant to run inside the same transaction as
// [Start], only when that call created the instance -- see startOnce in
// internal/workflow/execute/driver.go, which also calls this on a replay so
// an identical resupplied document stays a no-op and a different one is
// refused rather than silently rebinding the run.
//
// The artifact must pin the plan the instance runs on. Recording the
// identical artifact twice is a no-op; a different artifact for the same
// instance is [CodeWorkflowInputConflict].
func RecordWorkflowInputs(ctx context.Context, ex Executor, a WorkflowInputArtifact) (ret0 string, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.record_workflow_inputs", a.TenantID, a.InstanceID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := a.Validate(); err != nil {
		return "", err
	}
	inst, err := (Store{}).LoadInstance(ctx, ex, a.TenantID, a.InstanceID)
	if err != nil {
		return "", err
	}
	if inst.CompiledPlanHash != a.PlanDigest {
		return "", refuse(CodeAdvancePlanMismatch, a.InstanceID.String(), "",
			"instance pins compiled plan %s; the workflow inputs pin %s", inst.CompiledPlanHash, a.PlanDigest)
	}
	a = a.canonical()
	digest := a.Digest()
	body, err := json.Marshal(a)
	if err != nil {
		return "", wrap(CodeInvalidRecord, a.InstanceID.String(), "", err, "encode workflow input artifact")
	}
	if _, err := ex.Exec(ctx, `INSERT INTO workflow_input_artifact
		(tenant_id, instance_id, plan_digest, input_digest, artifact, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, instance_id) DO NOTHING`,
		a.TenantID, a.InstanceID, a.PlanDigest, digest, body, a.RecordedAt); err != nil {
		return "", wrap(CodeStorageFailed, a.InstanceID.String(), "", err, "record workflow input artifact")
	}
	var stored string
	if err := ex.QueryRow(ctx, `SELECT input_digest FROM workflow_input_artifact
		WHERE tenant_id = $1 AND instance_id = $2`,
		a.TenantID, a.InstanceID).Scan(&stored); err != nil {
		return "", wrap(CodeStorageFailed, a.InstanceID.String(), "", err, "read back workflow input artifact")
	}
	if stored != digest {
		return "", refuse(CodeWorkflowInputConflict, a.InstanceID.String(), "",
			"instance already recorded workflow inputs %s; refusing to replace them with %s", stored, digest)
	}
	return digest, nil
}

// LoadWorkflowInputs returns the instance's recorded workflow input and context document.
// found is false for an instance that recorded none -- a run that supplies no
// [ExecuteRequest.Inputs] (Promotion, as of WF-EXT-004, and every plan before
// it) keeps working exactly as before. A stored row is re-digested and
// refused with [CodeInvalidRecord] unless it still matches the digest
// recorded with it.
func LoadWorkflowInputs(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (ret0 WorkflowInputArtifact, found bool, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.load_workflow_inputs", tenantID, instanceID)
	defer func() { observe.DoneWith(obsOp, retErr, found) }()
	var (
		stored string
		body   []byte
	)
	err := ex.QueryRow(ctx, `SELECT input_digest, artifact FROM workflow_input_artifact
		WHERE tenant_id = $1 AND instance_id = $2`, tenantID, instanceID).Scan(&stored, &body)
	if errors.Is(err, dbport.ErrNoRows) {
		return WorkflowInputArtifact{}, false, nil
	}
	if err != nil {
		return WorkflowInputArtifact{}, false, wrap(CodeStorageFailed, instanceID.String(), "", err, "load workflow input artifact")
	}
	var a WorkflowInputArtifact
	if err := json.Unmarshal(body, &a); err != nil {
		return WorkflowInputArtifact{}, false, wrap(CodeInvalidRecord, instanceID.String(), "", err, "decode workflow input artifact")
	}
	if a.TenantID != tenantID || a.InstanceID != instanceID || a.Digest() != stored {
		return WorkflowInputArtifact{}, false, refuse(CodeInvalidRecord, instanceID.String(), "",
			"workflow input artifact no longer matches its key or recorded digest %s", stored)
	}
	return a, true, nil
}

// NodeOutputArtifact is the durable record of the typed output one node
// attempt produced. It is written once, in the advancement transaction, and
// is the source [workflow.ResolveMappings] reads a NODE_OUTPUT mapping from
// on the durable path.
type NodeOutputArtifact struct {
	TenantID   uuid.UUID         `json:"tenant_id"`
	InstanceID uuid.UUID         `json:"instance_id"`
	NodeID     string            `json:"node_id"`
	Attempt    int               `json:"attempt"`
	StepType   workflow.StepType `json:"step_type"`
	PlanDigest string            `json:"plan_digest"`

	Outputs []TypedArtifactValue `json:"outputs"`

	RecordedAt time.Time `json:"recorded_at"`
}

// Validate refuses an artifact that could not be read back correctly.
func (a NodeOutputArtifact) Validate() error {
	instance := a.InstanceID.String()
	switch {
	case a.TenantID == uuid.Nil || a.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node output artifact needs a tenant and an instance")
	case strings.TrimSpace(a.NodeID) == "":
		return refuse(CodeInvalidRecord, instance, "", "node output artifact names no node")
	case a.Attempt < 1:
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node output artifact attempt must be at least 1")
	case a.StepType == "":
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node output artifact names no step type")
	case a.PlanDigest == "":
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node output artifact pins no compiled plan")
	case len(a.Outputs) == 0:
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node output artifact records no outputs")
	case a.RecordedAt.IsZero():
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node output artifact carries no recorded instant")
	}
	seen := map[string]bool{}
	for _, out := range a.Outputs {
		if strings.TrimSpace(out.Path) == "" || seen[out.Path] {
			return refuse(CodeInvalidRecord, instance, a.NodeID, "node output paths must be present and unique (%q)", out.Path)
		}
		seen[out.Path] = true
	}
	return nil
}

// validateAgainstNode refuses an output the compiled node does not declare,
// or whose type is not assignable to the node's own declared output type.
// It is checked only when a caller of [RecordNodeOutputs] passes the node;
// every production caller does.
func (a NodeOutputArtifact) validateAgainstNode(node workflow.CompiledNode) error {
	if node.ID != "" && node.ID != a.NodeID {
		return refuse(CodeInvalidRecord, a.InstanceID.String(), a.NodeID,
			"node output artifact names node %q but the compiled node passed to record it is %q", a.NodeID, node.ID)
	}
	declared := make(map[string]workflow.ValueType, len(node.Outputs))
	for _, f := range node.Outputs {
		declared[f.Path] = f.Type
	}
	for _, out := range a.Outputs {
		want, ok := declared[out.Path]
		if !ok {
			return refuse(CodeInvalidRecord, a.InstanceID.String(), a.NodeID,
				"node output artifact names path %q, which %s does not declare as an output", out.Path, node.ID)
		}
		if err := out.Type.AssignableTo(want); err != nil {
			return refuse(CodeInvalidRecord, a.InstanceID.String(), a.NodeID,
				"node output %q: %s is not assignable to declared type %s: %v", out.Path, out.Type, want, err)
		}
	}
	return nil
}

func (a NodeOutputArtifact) canonical() NodeOutputArtifact {
	a.Outputs = append([]TypedArtifactValue(nil), a.Outputs...)
	sort.Slice(a.Outputs, func(i, j int) bool { return a.Outputs[i].Path < a.Outputs[j].Path })
	a.RecordedAt = a.RecordedAt.UTC()
	return a
}

// Digest is the artifact's canonical content identity.
func (a NodeOutputArtifact) Digest() string {
	return "sha256:" + canonicalDigest(nodeOutputArtifactDigestProfile, a.canonical())
}

// Clone returns a deep copy.
func (a NodeOutputArtifact) Clone() NodeOutputArtifact {
	a.Outputs = append([]TypedArtifactValue(nil), a.Outputs...)
	return a
}

// Output returns the recorded value at path.
func (a NodeOutputArtifact) Output(path string) (TypedArtifactValue, bool) {
	for _, out := range a.Outputs {
		if out.Path == path {
			return out, true
		}
	}
	return TypedArtifactValue{}, false
}

// RecordNodeOutputs appends the pinned output artifact of one node attempt
// and returns its digest. It is meant to run through the advancement
// transaction, so the outputs and the outcome they were produced with commit
// or roll back together.
//
// node, when non-nil, is the compiled node the attempt belongs to: its
// declared output fields gate every path and type this call will accept
// ([NodeOutputArtifact.validateAgainstNode]). The artifact must pin the plan
// the instance runs on. Recording the identical artifact twice is a no-op; a
// different artifact for the same attempt is [CodeNodeOutputConflict].
func RecordNodeOutputs(ctx context.Context, ex Executor, a NodeOutputArtifact, node *workflow.CompiledNode) (ret0 string, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.record_node_outputs", a.TenantID, a.InstanceID, a.NodeID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := a.Validate(); err != nil {
		return "", err
	}
	if node != nil {
		if err := a.validateAgainstNode(*node); err != nil {
			return "", err
		}
	}
	inst, err := (Store{}).LoadInstance(ctx, ex, a.TenantID, a.InstanceID)
	if err != nil {
		return "", err
	}
	if inst.CompiledPlanHash != a.PlanDigest {
		return "", refuse(CodeAdvancePlanMismatch, a.InstanceID.String(), a.NodeID,
			"instance pins compiled plan %s; the node outputs pin %s", inst.CompiledPlanHash, a.PlanDigest)
	}
	a = a.canonical()
	digest := a.Digest()
	body, err := json.Marshal(a)
	if err != nil {
		return "", wrap(CodeInvalidRecord, a.InstanceID.String(), a.NodeID, err, "encode node output artifact")
	}
	if _, err := ex.Exec(ctx, `INSERT INTO workflow_node_output_artifact
		(tenant_id, instance_id, node_id, attempt, plan_digest, output_digest, artifact, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, instance_id, node_id, attempt) DO NOTHING`,
		a.TenantID, a.InstanceID, a.NodeID, a.Attempt, a.PlanDigest, digest, body, a.RecordedAt); err != nil {
		return "", wrap(CodeStorageFailed, a.InstanceID.String(), a.NodeID, err, "record node output artifact")
	}
	var stored string
	if err := ex.QueryRow(ctx, `SELECT output_digest FROM workflow_node_output_artifact
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND attempt = $4`,
		a.TenantID, a.InstanceID, a.NodeID, a.Attempt).Scan(&stored); err != nil {
		return "", wrap(CodeStorageFailed, a.InstanceID.String(), a.NodeID, err, "read back node output artifact")
	}
	if stored != digest {
		return "", refuse(CodeNodeOutputConflict, a.InstanceID.String(), a.NodeID,
			"attempt %d already recorded outputs %s; refusing to replace them with %s", a.Attempt, stored, digest)
	}
	return digest, nil
}

// LoadNodeOutputs returns every recorded output artifact of an instance,
// ordered by node and attempt. Each row's content is re-digested and refused
// with [CodeInvalidRecord] unless it still matches the digest recorded with
// it.
func LoadNodeOutputs(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (ret0 []NodeOutputArtifact, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.load_node_outputs", tenantID, instanceID)
	defer func() { observe.DoneWith(obsOp, retErr, len(ret0)) }()
	rows, err := ex.Query(ctx, `SELECT node_id, attempt, output_digest, artifact FROM workflow_node_output_artifact
		WHERE tenant_id = $1 AND instance_id = $2 ORDER BY node_id, attempt`, tenantID, instanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "read node output artifacts")
	}
	defer rows.Close()
	var out []NodeOutputArtifact
	for rows.Next() {
		var (
			nodeID, stored string
			attempt        int
			body           []byte
		)
		if err := rows.Scan(&nodeID, &attempt, &stored, &body); err != nil {
			return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "scan node output artifact")
		}
		var a NodeOutputArtifact
		if err := json.Unmarshal(body, &a); err != nil {
			return nil, wrap(CodeInvalidRecord, instanceID.String(), nodeID, err, "decode node output artifact")
		}
		if a.TenantID != tenantID || a.InstanceID != instanceID || a.NodeID != nodeID || a.Attempt != attempt || a.Digest() != stored {
			return nil, refuse(CodeInvalidRecord, instanceID.String(), nodeID,
				"node output artifact for attempt %d no longer matches its key or recorded digest %s", attempt, stored)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "read node output artifacts")
	}
	return out, nil
}
