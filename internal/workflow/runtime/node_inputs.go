package runtime

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// CodeNodeInputConflict reports a second, different input artifact for a node
// attempt that already recorded one. Recorded inputs are historical evidence
// (WF-RUN-013's REFACTOR clause): an attempt is evaluated against one set of
// inputs, and a later write can never replace them.
const CodeNodeInputConflict = "NODE_INPUT_ARTIFACT_CONFLICT"

// PinnedVersionRuleTable is the [PinnedArtifactVersion.Kind] of a decision
// table (internal/engines/rules) a DECISION node was evaluated against.
const PinnedVersionRuleTable = "RULE_TABLE"

const nodeInputArtifactDigestProfile = "hcmnext.workflow.runtime.NodeInputArtifact/v1"

// NodeInputValue is one typed input a pure node consumed, rendered as its
// canonical text (a decimal at its declared scale, "true"/"false", a string
// verbatim). The consumer parses it against its own declared column kind, so
// the artifact never needs a kind of its own to be read correctly.
type NodeInputValue struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

// PinnedArtifactVersion is one versioned artifact a node evaluation was bound
// to: a rule table, a transform, a capability manifest.
type PinnedArtifactVersion struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// NodeInputArtifact is the durable record of what one pure node attempt was
// evaluated against. It is written once, in the advancement transaction, and
// is the only input a deterministic replay recomputes that node from.
type NodeInputArtifact struct {
	TenantID   uuid.UUID         `json:"tenant_id"`
	InstanceID uuid.UUID         `json:"instance_id"`
	NodeID     string            `json:"node_id"`
	Attempt    int               `json:"attempt"`
	StepType   workflow.StepType `json:"step_type"`
	// PlanDigest and ExecutionContextDigest bind the inputs to the plan and
	// the execution context the instance pinned at start.
	PlanDigest             string `json:"plan_digest"`
	ExecutionContextDigest string `json:"execution_context_digest,omitempty"`

	Inputs   []NodeInputValue        `json:"inputs"`
	Versions []PinnedArtifactVersion `json:"versions,omitempty"`

	RecordedAt time.Time `json:"recorded_at"`
}

// Validate refuses an artifact that could not be replayed from.
func (a NodeInputArtifact) Validate() error {
	instance := a.InstanceID.String()
	switch {
	case a.TenantID == uuid.Nil || a.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node input artifact needs a tenant and an instance")
	case strings.TrimSpace(a.NodeID) == "":
		return refuse(CodeInvalidRecord, instance, "", "node input artifact names no node")
	case a.Attempt < 1:
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node input artifact attempt must be at least 1")
	case a.StepType == "":
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node input artifact names no step type")
	case a.PlanDigest == "":
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node input artifact pins no compiled plan")
	case len(a.Inputs) == 0:
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node input artifact records no inputs")
	case a.RecordedAt.IsZero():
		return refuse(CodeInvalidRecord, instance, a.NodeID, "node input artifact carries no recorded instant")
	}
	seen := map[string]bool{}
	for _, in := range a.Inputs {
		if strings.TrimSpace(in.Path) == "" || seen[in.Path] {
			return refuse(CodeInvalidRecord, instance, a.NodeID, "node input paths must be present and unique (%q)", in.Path)
		}
		seen[in.Path] = true
	}
	for _, v := range a.Versions {
		if v.Kind == "" || v.Ref == "" || v.Version == "" || v.Digest == "" {
			return refuse(CodeInvalidRecord, instance, a.NodeID, "a pinned version needs a kind, ref, version and digest")
		}
	}
	return nil
}

// canonical returns a copy with inputs and versions sorted and the instant in
// UTC, so the digest is independent of the order a recorder appended them.
func (a NodeInputArtifact) canonical() NodeInputArtifact {
	a.Inputs = append([]NodeInputValue(nil), a.Inputs...)
	sort.Slice(a.Inputs, func(i, j int) bool { return a.Inputs[i].Path < a.Inputs[j].Path })
	a.Versions = append([]PinnedArtifactVersion(nil), a.Versions...)
	sort.Slice(a.Versions, func(i, j int) bool {
		if a.Versions[i].Kind != a.Versions[j].Kind {
			return a.Versions[i].Kind < a.Versions[j].Kind
		}
		return a.Versions[i].Ref < a.Versions[j].Ref
	})
	a.RecordedAt = a.RecordedAt.UTC()
	return a
}

// Digest is the artifact's canonical content identity.
func (a NodeInputArtifact) Digest() string {
	return "sha256:" + canonicalDigest(nodeInputArtifactDigestProfile, a.canonical())
}

// Input returns the recorded value at path.
func (a NodeInputArtifact) Input(path string) (string, bool) {
	for _, in := range a.Inputs {
		if in.Path == path {
			return in.Value, true
		}
	}
	return "", false
}

// Version returns the pinned version of the given kind.
func (a NodeInputArtifact) Version(kind string) (PinnedArtifactVersion, bool) {
	for _, v := range a.Versions {
		if v.Kind == kind {
			return v, true
		}
	}
	return PinnedArtifactVersion{}, false
}

// Clone returns a deep copy.
func (a NodeInputArtifact) Clone() NodeInputArtifact {
	a.Inputs = append([]NodeInputValue(nil), a.Inputs...)
	a.Versions = append([]PinnedArtifactVersion(nil), a.Versions...)
	return a
}

// RecordNodeInputs appends the pinned input artifact of one node attempt and
// returns its digest. It is meant to run through the advancement transaction
// (a TransactionalStepRunner's RunInTx), so the inputs and the outcome they
// produced commit or roll back together.
//
// The artifact must pin the plan the instance runs on; an empty execution
// context digest adopts the one the instance pinned, and a different one is
// [CodeContextDrift]. Recording the identical artifact twice is a no-op; a
// different artifact for the same attempt is [CodeNodeInputConflict].
func RecordNodeInputs(ctx context.Context, ex Executor, a NodeInputArtifact) (ret0 string, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.record_node_inputs", a.TenantID, a.InstanceID, a.NodeID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := a.Validate(); err != nil {
		return "", err
	}
	inst, err := (Store{}).LoadInstance(ctx, ex, a.TenantID, a.InstanceID)
	if err != nil {
		return "", err
	}
	if inst.CompiledPlanHash != a.PlanDigest {
		return "", refuse(CodeAdvancePlanMismatch, a.InstanceID.String(), a.NodeID,
			"instance pins compiled plan %s; the inputs pin %s", inst.CompiledPlanHash, a.PlanDigest)
	}
	switch {
	case a.ExecutionContextDigest == "":
		a.ExecutionContextDigest = inst.EffectiveContextRef
	case inst.EffectiveContextRef != "" && a.ExecutionContextDigest != inst.EffectiveContextRef:
		return "", refuse(CodeContextDrift, a.InstanceID.String(), a.NodeID,
			"inputs were evaluated under context %s; the instance pinned %s", a.ExecutionContextDigest, inst.EffectiveContextRef)
	}
	a = a.canonical()
	digest := a.Digest()
	body, err := json.Marshal(a)
	if err != nil {
		return "", wrap(CodeInvalidRecord, a.InstanceID.String(), a.NodeID, err, "encode node input artifact")
	}
	if _, err := ex.Exec(ctx, `INSERT INTO workflow_node_input_artifact
		(tenant_id, instance_id, node_id, attempt, plan_digest, context_digest, input_digest, artifact, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, instance_id, node_id, attempt) DO NOTHING`,
		a.TenantID, a.InstanceID, a.NodeID, a.Attempt, a.PlanDigest, a.ExecutionContextDigest, digest, body, a.RecordedAt); err != nil {
		return "", wrap(CodeStorageFailed, a.InstanceID.String(), a.NodeID, err, "record node input artifact")
	}
	var stored string
	if err := ex.QueryRow(ctx, `SELECT input_digest FROM workflow_node_input_artifact
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND attempt = $4`,
		a.TenantID, a.InstanceID, a.NodeID, a.Attempt).Scan(&stored); err != nil {
		return "", wrap(CodeStorageFailed, a.InstanceID.String(), a.NodeID, err, "read back node input artifact")
	}
	if stored != digest {
		return "", refuse(CodeNodeInputConflict, a.InstanceID.String(), a.NodeID,
			"attempt %d already recorded inputs %s; refusing to replace them with %s", a.Attempt, stored, digest)
	}
	return digest, nil
}

// LoadNodeInputs returns every pinned input artifact of an instance, ordered
// by node and attempt. Each row's content is re-digested and refused with
// [CodeInvalidRecord] unless it still matches the digest recorded with it.
func LoadNodeInputs(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (ret0 []NodeInputArtifact, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.load_node_inputs", tenantID, instanceID)
	defer func() { observe.DoneWith(obsOp, retErr, len(ret0)) }()
	rows, err := ex.Query(ctx, `SELECT node_id, attempt, input_digest, artifact FROM workflow_node_input_artifact
		WHERE tenant_id = $1 AND instance_id = $2 ORDER BY node_id, attempt`, tenantID, instanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "read node input artifacts")
	}
	defer rows.Close()
	var out []NodeInputArtifact
	for rows.Next() {
		var (
			nodeID, stored string
			attempt        int
			body           []byte
		)
		if err := rows.Scan(&nodeID, &attempt, &stored, &body); err != nil {
			return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "scan node input artifact")
		}
		var a NodeInputArtifact
		if err := json.Unmarshal(body, &a); err != nil {
			return nil, wrap(CodeInvalidRecord, instanceID.String(), nodeID, err, "decode node input artifact")
		}
		if a.TenantID != tenantID || a.InstanceID != instanceID || a.NodeID != nodeID || a.Attempt != attempt || a.Digest() != stored {
			return nil, refuse(CodeInvalidRecord, instanceID.String(), nodeID,
				"node input artifact for attempt %d no longer matches its key or recorded digest %s", attempt, stored)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "read node input artifacts")
	}
	return out, nil
}
