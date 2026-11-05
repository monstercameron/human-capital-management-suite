// Package fragments publishes the WF-EXT-015 compile-time expansions: the
// notify fragment (a notice send, optionally held behind a release gate)
// and the document fragment (render, signature request, correlated
// completion signal). Fragments expand into nodes before Compile runs —
// nothing fragment-shaped reaches the compiler or the runtime — and the
// served capability handlers in this package adapt runner calls onto the
// messaging and documents planes.
package fragments

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/documents"
	"github.com/monstercameron/human-capital-management-suite/internal/messaging"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
)

// Derived node identities. Authors wire edges against these: expansion is
// deterministic, so the identities are predictable.
func NotifySendID(id string) string     { return id + ".send" }
func NotifyReleaseID(id string) string  { return id + ".release" }
func DocumentRenderID(id string) string { return id + ".render" }
func DocumentSignID(id string) string   { return id + ".sign" }
func DocumentSignedID(id string) string { return id + ".signed" }

// ReleaseGate holds a notice behind a correlated signal: the send node is
// unreachable until the release event arrives.
type ReleaseGate struct {
	EventType                string
	CorrelationKeyExpression string
	AcceptedSources          []string
	CloseAfterSeconds        uint64
}

// NoticeConfig is the constant notice the send node emits. Notices are
// fully declared: every input the send capability takes arrives as a
// constant mapping, so the compiled plan shows the exact notice.
type NoticeConfig struct {
	TenantID                 string
	OrganizationScope        string
	AudienceExpression       string
	AudienceResolutionPolicy string
	Purpose                  string
	Classification           string
	Urgency                  string
	DeliveryRequirement      string
	ReplyMode                string
	TemplateRef              string
	ContentRef               string
	ParametersRef            string
	CorrelationID            string
	ExpiresAt                int64
	NoticeKey                string
}

// Notify declares one notice send with an optional release gate. The send is
// an external mutation, so the author names its failure route and observes
// delivery downstream: the compiler refuses unobserved external effects, and
// delivery evidence is deployment-specific, so observation stays authored
// rather than implied.
type Notify struct {
	ID                  string
	CapabilityVersion   uint32
	AuthorityScopes     []string
	Governance          workflow.NodeGovernance
	WorkerInputPath     string
	ReleaseGate         *ReleaseGate
	Notice              NoticeConfig
	IdempotencyKeyField string
	FailureNode         string
}

// Document declares one letter flow: render a published template, request a
// signature on the artifact, and await the correlated completion signal.
type Document struct {
	ID                  string
	CapabilityVersion   uint32
	AuthorityScopes     []string
	Governance          workflow.NodeGovernance
	WorkerInputPath     string
	TemplateID          string
	TemplateVersion     string
	BindingsInputPath   string
	SignerInputPath     string
	Assurance           string
	ExpiresInSeconds    int64
	CeremonyKey         string
	IdempotencyKeyField string
}

var fragmentID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// signalEnvelope is the conventional schema pair for fragment signal nodes.
func signalEnvelope(stem string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID:         "hcmnext.fragments." + stem,
		Version:          1,
		ProtobufFullName: "hcmnext.fragments.v1.Envelope",
	}
}

func plainString() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }

func constMapping(target, value string) workflow.Mapping {
	return workflow.Mapping{
		Target: target,
		Source: workflow.Source{Kind: workflow.SourceConstant, Type: plainString(), Constant: value},
	}
}

func stringField(path string) workflow.Field { return workflow.Field{Path: path, Type: plainString()} }

// Expand appends the fragment nodes to a copy of def: it never mutates its
// input. Capability versions resolve against reg, so generated nodes bind
// the exact published manifests. Edge routing stays authored: expansion
// contributes nodes only, and the author wires every outcome explicitly.
func Expand(def workflow.Definition, reg *capability.Registry, notifies []Notify, docs []Document) (workflow.Definition, error) {
	if reg == nil {
		return workflow.Definition{}, fmt.Errorf("%w: expansion requires a capability registry", ErrInvalidFragment)
	}
	seen := map[string]bool{}
	for _, n := range def.Nodes {
		seen[n.ID] = true
	}
	claim := func(id string) error {
		if !fragmentID.MatchString(id) {
			return fmt.Errorf("%w: fragment id %q", ErrInvalidFragment, id)
		}
		if seen[id] {
			return fmt.Errorf("%w: fragment id %q collides", ErrInvalidFragment, id)
		}
		seen[id] = true
		return nil
	}
	out := def
	out.Nodes = append([]workflow.Node(nil), def.Nodes...)
	for _, n := range notifies {
		if err := claim(n.ID); err != nil {
			return workflow.Definition{}, err
		}
		nodes, err := expandNotify(n, reg)
		if err != nil {
			return workflow.Definition{}, err
		}
		for _, node := range nodes {
			if seen[node.ID] {
				return workflow.Definition{}, fmt.Errorf("%w: generated node %q collides", ErrInvalidFragment, node.ID)
			}
			seen[node.ID] = true
		}
		out.Nodes = append(out.Nodes, nodes...)
	}
	for _, d := range docs {
		if err := claim(d.ID); err != nil {
			return workflow.Definition{}, err
		}
		nodes, err := expandDocument(d, reg)
		if err != nil {
			return workflow.Definition{}, err
		}
		for _, node := range nodes {
			if seen[node.ID] {
				return workflow.Definition{}, fmt.Errorf("%w: generated node %q collides", ErrInvalidFragment, node.ID)
			}
			seen[node.ID] = true
		}
		out.Nodes = append(out.Nodes, nodes...)
	}
	return out, nil
}

func capabilityVersion(v uint32) uint32 {
	if v == 0 {
		return 1
	}
	return v
}

func lookupCapability(reg *capability.Registry, id string, version uint32) (capability.Record, error) {
	rec, ok := reg.Lookup(capability.Key{ID: id, Version: capabilityVersion(version)})
	if !ok {
		return capability.Record{}, fmt.Errorf("%w: capability %s/v%d is not published",
			ErrInvalidFragment, id, capabilityVersion(version))
	}
	return rec, nil
}

func checkGovernance(g workflow.NodeGovernance) error {
	if g.Purpose == "" {
		return fmt.Errorf("%w: fragment governance carries a purpose", ErrInvalidFragment)
	}
	return nil
}

func workerPath(path string) string {
	if path == "" {
		return "worker_id"
	}
	return path
}

func expandNotify(n Notify, reg *capability.Registry) ([]workflow.Node, error) {
	if err := checkGovernance(n.Governance); err != nil {
		return nil, err
	}
	rec, err := lookupCapability(reg, messaging.CapSend, n.CapabilityVersion)
	if err != nil {
		return nil, err
	}
	if n.Notice.NoticeKey == "" {
		return nil, fmt.Errorf("%w: notify %q requires a notice key", ErrInvalidFragment, n.ID)
	}
	if n.FailureNode == "" {
		return nil, fmt.Errorf("%w: notify %q names its failure route", ErrInvalidFragment, n.ID)
	}
	keyField := n.IdempotencyKeyField
	if keyField == "" {
		keyField = "notice_key"
	}
	notice := n.Notice
	constants := map[string]string{
		"tenant_id": notice.TenantID, "organization_scope": notice.OrganizationScope,
		"audience_expression": notice.AudienceExpression, "audience_resolution_policy": notice.AudienceResolutionPolicy,
		"purpose": notice.Purpose, "classification": notice.Classification, "urgency": notice.Urgency,
		"delivery_requirement": notice.DeliveryRequirement, "reply_mode": notice.ReplyMode,
		"template_ref": notice.TemplateRef, "content_ref": notice.ContentRef, "parameters_ref": notice.ParametersRef,
		"correlation_id": notice.CorrelationID, "expires_at": strconv.FormatInt(notice.ExpiresAt, 10),
		"notice_key": notice.NoticeKey,
	}
	var nodes []workflow.Node
	if n.ReleaseGate != nil {
		gate := n.ReleaseGate
		if gate.EventType == "" || gate.CorrelationKeyExpression == "" {
			return nil, fmt.Errorf("%w: release gate names an event and a correlation", ErrInvalidFragment)
		}
		nodes = append(nodes, signalNode(
			NotifyReleaseID(n.ID), n.Governance, workerPath(n.WorkerInputPath),
			"release_ref", gate.EventType, gate.CorrelationKeyExpression,
			gate.AcceptedSources, gate.CloseAfterSeconds,
			map[string]string{"hcmnext.fragment": n.ID, "hcmnext.fragment.role": "release"},
		))
	}
	send := capabilityNode(
		NotifySendID(n.ID), rec, constants, nil,
		[]string{"message_id", "digest"},
		n.AuthorityScopes, keyField, "messaging.notice.dispatch",
		workflow.RoleDownstreamEffect, n.FailureNode, n.Governance,
		map[string]string{"hcmnext.fragment": n.ID, "hcmnext.fragment.role": "send"},
	)
	nodes = append(nodes, send)
	return nodes, nil
}

func expandDocument(d Document, reg *capability.Registry) ([]workflow.Node, error) {
	if err := checkGovernance(d.Governance); err != nil {
		return nil, err
	}
	if d.TemplateID == "" || d.TemplateVersion == "" {
		return nil, fmt.Errorf("%w: document %q names its template", ErrInvalidFragment, d.ID)
	}
	switch documents.AssuranceLevel(d.Assurance) {
	case documents.AssuranceBasic, documents.AssuranceEnhanced, documents.AssuranceQualified:
	default:
		return nil, fmt.Errorf("%w: assurance %q is not governed", ErrInvalidFragment, d.Assurance)
	}
	if d.BindingsInputPath == "" || d.SignerInputPath == "" {
		return nil, fmt.Errorf("%w: document %q names its bindings and signer inputs", ErrInvalidFragment, d.ID)
	}
	if d.CeremonyKey == "" {
		return nil, fmt.Errorf("%w: document %q requires a ceremony key", ErrInvalidFragment, d.ID)
	}
	renderRec, err := lookupCapability(reg, documents.CapRender, d.CapabilityVersion)
	if err != nil {
		return nil, err
	}
	signRec, err := lookupCapability(reg, documents.CapRequestSignature, d.CapabilityVersion)
	if err != nil {
		return nil, err
	}
	renderID := DocumentRenderID(d.ID)
	render := capabilityNode(
		renderID, renderRec,
		map[string]string{"template_id": d.TemplateID, "template_version": d.TemplateVersion},
		[]workflow.Mapping{{
			Target: "bindings_json",
			Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: d.BindingsInputPath, Type: plainString()},
		}},
		[]string{"artifact_digest", "content"},
		d.AuthorityScopes, "", "", "", "", d.Governance,
		map[string]string{"hcmnext.fragment": d.ID, "hcmnext.fragment.role": "render"},
	)
	keyField := d.IdempotencyKeyField
	if keyField == "" {
		keyField = "ceremony_key"
	}
	sign := capabilityNode(
		DocumentSignID(d.ID), signRec,
		map[string]string{
			"assurance":          d.Assurance,
			"expires_in_seconds": strconv.FormatInt(d.ExpiresInSeconds, 10),
			"ceremony_key":       d.CeremonyKey,
		},
		[]workflow.Mapping{
			{
				Target: "artifact_hash",
				Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: renderID, Path: "artifact_digest", Type: plainString()},
			},
			{
				Target: "signer_id",
				Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: d.SignerInputPath, Type: plainString()},
			},
		},
		[]string{"ceremony_id", "state", "digest"},
		d.AuthorityScopes, keyField, "documents.signature.request",
		workflow.RoleAuthoritativeCore, "", d.Governance,
		map[string]string{"hcmnext.fragment": d.ID, "hcmnext.fragment.role": "sign"},
	)
	signed := signalNode(
		DocumentSignedID(d.ID), d.Governance, workerPath(d.WorkerInputPath),
		"ceremony_id", documents.EventSignatureSigned, workerPath(d.WorkerInputPath),
		[]string{"documents"}, 0,
		map[string]string{"hcmnext.fragment": d.ID, "hcmnext.fragment.role": "signed"},
	)
	return []workflow.Node{render, sign, signed}, nil
}

// manifestSchema carries a published capability schema onto a node. The two
// schema structs share a shape but not a type, so the copy is explicit.
func manifestSchema(s capability.SchemaRef) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: s.SchemaID, Version: s.Version, ProtobufFullName: s.ProtobufFullName}
}

// capabilityNode builds one compiler-shaped capability invocation: schemas
// pinned to the published manifest, every input bound, the declared effect
// from the manifest, and a SIMULATE overlay suppressing writes to reads.
func capabilityNode(id string, rec capability.Record, constants map[string]string, extra []workflow.Mapping, outputs, scopes []string, idempotencyKey, effectBinding string, role workflow.EffectRole, failureRoute string, governance workflow.NodeGovernance, metadata map[string]string) workflow.Node {
	ordered := make([]string, 0, len(constants))
	for target := range constants {
		ordered = append(ordered, target)
	}
	sort.Strings(ordered)
	inputs := make([]workflow.Field, 0, len(ordered)+len(extra))
	mappings := make([]workflow.Mapping, 0, len(ordered)+len(extra))
	for _, target := range ordered {
		inputs = append(inputs, stringField(target))
		mappings = append(mappings, constMapping(target, constants[target]))
	}
	for _, m := range extra {
		inputs = append(inputs, stringField(m.Target))
		mappings = append(mappings, m)
	}
	outs := make([]workflow.Field, 0, len(outputs))
	for _, path := range outputs {
		outs = append(outs, stringField(path))
	}
	node := workflow.Node{
		ID:            id,
		Type:          workflow.StepCapability,
		InputSchema:   manifestSchema(rec.Definition.RequestSchema),
		OutputSchema:  manifestSchema(rec.Definition.ResponseSchema),
		Inputs:        inputs,
		Outputs:       outs,
		InputMappings: mappings,
		Capability: &workflow.CapabilityRef{
			ID:                    rec.Definition.ID,
			Version:               rec.Definition.Version,
			OperationMode:         workflow.ModeExecute,
			AuthorityScopes:       scopes,
			IdempotencyKeyMapping: idempotencyKey,
			EffectBinding:         effectBinding,
		},
		DeclaredEffect: rec.Definition.EffectClass,
		EffectRole:     role,
		FailureRoute:   failureRoute,
		Governance:     governance,
		Metadata:       metadata,
	}
	if rec.Definition.EffectClass.IsWrite() {
		node.ModeOverlay = &workflow.ModeOverlay{SimulateEffect: capability.EffectReadOnly}
	}
	return node
}

// signalNode builds one compiler-shaped correlated suspension.
func signalNode(id string, governance workflow.NodeGovernance, workerInputPath, output, eventType, correlation string, sources []string, closeAfter uint64, metadata map[string]string) workflow.Node {
	branded := workflow.ValueType{Kind: workflow.KindString, Brand: "WorkerID"}
	return workflow.Node{
		ID:             id,
		Type:           workflow.StepSignal,
		DeclaredEffect: capability.EffectPure,
		InputSchema:    signalEnvelope("signal.input"),
		OutputSchema:   signalEnvelope("signal.result"),
		Inputs:         []workflow.Field{{Path: "worker_id", Type: branded}},
		Outputs:        []workflow.Field{{Path: output, Type: plainString()}},
		InputMappings: []workflow.Mapping{{
			Target: "worker_id",
			Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: workerInputPath, Type: branded},
		}},
		Signal: &workflow.SignalSpec{
			EventType:                eventType,
			CorrelationKeyExpression: correlation,
			ExpectedSchemaRef:        signalEnvelope("signal." + eventType),
			AcceptedSources:          sources,
			Ordering:                 workflow.SignalOrderingNone,
			CloseAfterSeconds:        closeAfter,
		},
		Governance: governance,
		Metadata:   metadata,
	}
}

// Handlers adapts runner calls onto the plane operations. The handlers
// decode the call inputs, run the governed operation, and answer with the
// declared outputs; a non-call payload or a failed operation fails the
// invocation, never the run.
func Handlers(sender messaging.Sender, store documents.TemplateStore, ceremonies *documents.CeremonyRegistry) map[capability.Key]capability.Handler {
	return map[capability.Key]capability.Handler{
		{ID: messaging.CapSend, Version: 1}: func(ctx context.Context, payload any) (any, error) {
			call, ok := payload.(capabilityrunner.CapabilityCall)
			if !ok {
				return nil, fmt.Errorf("%w: send takes a runner call", ErrInvalidInvocation)
			}
			out, err := messaging.Send(ctx, sender, call.InputTexts())
			if err != nil {
				return nil, err
			}
			return capabilityrunner.CapabilityAnswer{
				Outcome: workflow.OutcomeSucceeded,
				Outputs: map[string]capabilityrunner.ResolvedValue{
					"message_id": {Text: out.MessageID},
					"digest":     {Text: out.Digest},
				},
				Detail: "notice " + out.MessageID + " sent",
			}, nil
		},
		{ID: documents.CapRender, Version: 1}: func(_ context.Context, payload any) (any, error) {
			call, ok := payload.(capabilityrunner.CapabilityCall)
			if !ok {
				return nil, fmt.Errorf("%w: render takes a runner call", ErrInvalidInvocation)
			}
			out, err := documents.Render(store, call.InputTexts())
			if err != nil {
				return nil, err
			}
			return capabilityrunner.CapabilityAnswer{
				Outcome: workflow.OutcomeSucceeded,
				Outputs: map[string]capabilityrunner.ResolvedValue{
					"artifact_digest": {Text: out.ArtifactDigest},
					"content":         {Text: out.Content},
				},
				Detail: "artifact " + out.ArtifactDigest + " rendered",
			}, nil
		},
		{ID: documents.CapRequestSignature, Version: 1}: func(_ context.Context, payload any) (any, error) {
			call, ok := payload.(capabilityrunner.CapabilityCall)
			if !ok {
				return nil, fmt.Errorf("%w: request_signature takes a runner call", ErrInvalidInvocation)
			}
			out, err := documents.RequestSignature(ceremonies, call.InputTexts(), call.At)
			if err != nil {
				return nil, err
			}
			return capabilityrunner.CapabilityAnswer{
				Outcome: workflow.OutcomeSucceeded,
				Outputs: map[string]capabilityrunner.ResolvedValue{
					"ceremony_id": {Text: out.CeremonyID},
					"state":       {Text: out.State},
					"digest":      {Text: out.Digest},
				},
				Detail: "ceremony " + out.CeremonyID + " requested",
			}, nil
		},
	}
}

// RegisterInto publishes the three fragment capabilities bound to served
// handlers over the given planes.
func RegisterInto(reg *capability.Registry, sender messaging.Sender, store documents.TemplateStore, ceremonies *documents.CeremonyRegistry) error {
	if reg == nil {
		return fmt.Errorf("%w: registration requires a registry", ErrInvalidFragment)
	}
	handlers := Handlers(sender, store, ceremonies)
	defs := append(messaging.Definitions(), documents.Definitions()...)
	for _, d := range defs {
		handler, ok := handlers[capability.Key{ID: d.ID, Version: d.Version}]
		if !ok {
			return fmt.Errorf("%w: capability %s has no served handler", ErrInvalidFragment, d.ID)
		}
		if err := reg.Register(d, handler); err != nil {
			return err
		}
	}
	return nil
}
