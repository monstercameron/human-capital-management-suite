// Package draftcompile validates mutable workflow-designer documents through
// the production workflow compiler without granting the designer any runtime
// or publication authority.
package draftcompile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const maxDraftDocumentBytes = 4 << 20

// CapabilityPolicy is the tenant-owned allow list consulted before the
// compiler may resolve a capability manifest. A nil policy denies every
// capability reference rather than treating the global registry as a grant.
type CapabilityPolicy interface {
	AllowsCapability(context.Context, values.TenantId, capability.Key) bool
}

// CapabilityPolicyFunc adapts a function to CapabilityPolicy.
type CapabilityPolicyFunc func(context.Context, values.TenantId, capability.Key) bool

func (f CapabilityPolicyFunc) AllowsCapability(ctx context.Context, tenant values.TenantId, key capability.Key) bool {
	return f != nil && f(ctx, tenant, key)
}

// Diagnostic is the designer-safe projection of one compiler diagnostic.
// NodeID and EdgeID are stable graph identities; Field and Ref let the typed
// inspector focus the exact control that needs correction.
type Diagnostic struct {
	Code     string `json:"code"`
	NodeID   string `json:"node_id,omitempty"`
	EdgeID   string `json:"edge_id,omitempty"`
	EdgeFrom string `json:"edge_from,omitempty"`
	EdgeTo   string `json:"edge_to,omitempty"`
	RouteKey string `json:"route_key,omitempty"`
	Field    string `json:"field,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Detail   string `json:"detail"`
}

// UnwindBehavior is the conservative cancellation behavior derivable from
// the currently compiled plan. WF-REV-007 will enrich this into safe-point
// specific plans; the designer never labels an unresolved effect reversible.
type UnwindBehavior string

const (
	UnwindCompensate        UnwindBehavior = "COMPENSATE"
	UnwindForwardCorrection UnwindBehavior = "FORWARD_CORRECTION"
	UnwindUnresolved        UnwindBehavior = "UNRESOLVED"
)

// UnwindStep is one write-effect node's derived reversal behavior.
type UnwindStep struct {
	NodeID          string         `json:"node_id"`
	EffectClass     string         `json:"effect_class"`
	Behavior        UnwindBehavior `json:"behavior"`
	CompensationRef string         `json:"compensation_ref,omitempty"`
}

// UnwindSummary is deliberately conservative. Complete is false whenever a
// write effect has neither an immutable compensation reference nor the
// explicit forward-correction classification required for an irreversible
// external mutation.
type UnwindSummary struct {
	Complete bool         `json:"complete"`
	Steps    []UnwindStep `json:"steps,omitempty"`
}

// Result is the complete, deterministic answer returned to the designer.
// Invalid drafts carry every compiler diagnostic and no partial plan.
type Result struct {
	Valid       bool                   `json:"valid"`
	PlanDigest  string                 `json:"plan_digest,omitempty"`
	Diagnostics []Diagnostic           `json:"diagnostics,omitempty"`
	Effects     workflow.EffectSummary `json:"effects"`
	Unwind      UnwindSummary          `json:"unwind"`
}

// Compiler uses the same workflow.Compile entry point used by publication.
// Options may provide the global immutable registries, but capability lookup
// is always intersected with Policy for the request tenant.
type Compiler struct {
	Options workflow.Options
	Policy  CapabilityPolicy
}

// CompileDocument decodes one bounded authoring document, invokes the
// production compiler and maps all diagnostics back to graph identities.
func (c Compiler) CompileDocument(ctx context.Context, tenant values.TenantId, document json.RawMessage) Result {
	definition, ok := decodeDefinition(document)
	if !ok {
		return invalidDocumentResult()
	}
	options := c.Options
	options.Capabilities = tenantResolver{
		ctx: ctx, tenant: tenant, base: c.Options.Capabilities, policy: c.Policy,
	}
	plan, err := workflow.Compile(definition, options)
	if err != nil {
		var diagnostics *workflow.Diagnostics
		if errors.As(err, &diagnostics) {
			return Result{Diagnostics: projectDiagnostics(diagnostics), Unwind: UnwindSummary{Complete: false}}
		}
		return Result{Diagnostics: []Diagnostic{{Code: workflow.CodeInvalidDefinition, Detail: "workflow compilation failed"}}, Unwind: UnwindSummary{Complete: false}}
	}
	return Result{
		Valid:      true,
		PlanDigest: plan.Digest(),
		Effects:    plan.Effects,
		Unwind:     deriveUnwind(plan),
	}
}

func decodeDefinition(document json.RawMessage) (workflow.Definition, bool) {
	if len(document) == 0 || len(document) > maxDraftDocumentBytes {
		return workflow.Definition{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	var definition workflow.Definition
	if err := decoder.Decode(&definition); err != nil {
		return workflow.Definition{}, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return workflow.Definition{}, false
	}
	return definition, true
}

func invalidDocumentResult() Result {
	return Result{
		Diagnostics: []Diagnostic{{
			Code: workflow.CodeInvalidDefinition, Field: "document",
			Detail: "draft document is not valid workflow definition JSON",
		}},
		Unwind: UnwindSummary{Complete: false},
	}
}

func projectDiagnostics(diagnostics *workflow.Diagnostics) []Diagnostic {
	if diagnostics == nil {
		return nil
	}
	result := make([]Diagnostic, 0, len(diagnostics.Errors))
	for _, item := range diagnostics.Errors {
		location := item.Location
		result = append(result, Diagnostic{
			Code: item.Code, NodeID: location.NodeID,
			EdgeID:   edgeID(location.EdgeFrom, location.RouteKey, location.EdgeTo),
			EdgeFrom: location.EdgeFrom, EdgeTo: location.EdgeTo,
			RouteKey: location.RouteKey, Field: location.Field, Ref: location.Ref,
			Detail: item.Detail,
		})
	}
	return result
}

func edgeID(from, route, to string) string {
	if strings.TrimSpace(from) == "" && strings.TrimSpace(to) == "" {
		return ""
	}
	return fmt.Sprintf("%s--%s--%s", from, route, to)
}

func deriveUnwind(plan *workflow.CompiledWorkflow) UnwindSummary {
	if plan == nil {
		return UnwindSummary{}
	}
	summary := UnwindSummary{Complete: true}
	for _, node := range plan.Nodes {
		if node.Capability == nil || !node.Capability.EffectClass.IsWrite() {
			continue
		}
		step := UnwindStep{NodeID: node.ID, EffectClass: string(node.Capability.EffectClass)}
		switch {
		case node.Capability.EffectClass == capability.EffectIrreversibleExternalMutation:
			step.Behavior = UnwindForwardCorrection
		case node.CompensationRef != nil:
			step.Behavior = UnwindCompensate
			step.CompensationRef = node.CompensationRef.ID + "@" + node.CompensationRef.Version
		default:
			step.Behavior = UnwindUnresolved
			summary.Complete = false
		}
		summary.Steps = append(summary.Steps, step)
	}
	return summary
}

type tenantResolver struct {
	ctx    context.Context
	tenant values.TenantId
	base   workflow.CapabilityResolver
	policy CapabilityPolicy
}

func (r tenantResolver) Lookup(key capability.Key) (capability.Record, bool) {
	if r.base == nil || r.policy == nil || !r.policy.AllowsCapability(r.ctx, r.tenant, key) {
		return capability.Record{}, false
	}
	return r.base.Lookup(key)
}
