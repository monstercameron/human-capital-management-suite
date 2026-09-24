package workflow

import "sort"

type parameterResolverSnapshot map[string]ParameterDeclaration

func (r parameterResolverSnapshot) ResolveParameter(key string) (ParameterDeclaration, bool) {
	declaration, ok := r[key]
	return cloneParameterDeclaration(declaration), ok
}

// snapshotParameters reads every referenced key once so a stateful resolver
// cannot return one declaration during validation and another during plan
// normalization.
func snapshotParameters(def *Definition, resolver ParameterResolver) ParameterResolver {
	if resolver == nil {
		return nil
	}
	keys := map[string]struct{}{}
	for _, node := range def.Nodes {
		for _, mapping := range node.InputMappings {
			if mapping.Source.Kind == SourceParameter && mapping.Source.ParameterKey != "" {
				keys[mapping.Source.ParameterKey] = struct{}{}
			}
		}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	resolved := make(parameterResolverSnapshot, len(ordered))
	for _, key := range ordered {
		if declaration, ok := resolver.ResolveParameter(key); ok {
			resolved[key] = cloneParameterDeclaration(declaration)
		}
	}
	return resolved
}

func cloneParameterDeclaration(declaration ParameterDeclaration) ParameterDeclaration {
	declaration.Type = declaration.Type.clone()
	declaration.AllowedConsumers = append([]ParameterConsumer(nil), declaration.AllowedConsumers...)
	return declaration
}

// contextRequirement returns the node's declaration for a context kind.
func contextRequirement(n *Node, kind string) (ContextRequirement, bool) {
	for _, r := range n.RequiredContext {
		if r.Kind == kind {
			return r, true
		}
	}
	return ContextRequirement{}, false
}

func contextDeclaresPath(req ContextRequirement, path string) bool {
	for _, p := range req.FieldPaths {
		if p == path {
			return true
		}
	}
	return false
}

// isPinnedSource reports whether a source is a pinned snapshot rather than a
// live read. Everything except an unpinned context read is pinned: workflow
// input, a predecessor's recorded output and a literal constant cannot change
// under the node's feet.
func isPinnedSource(n *Node, src Source) bool {
	if src.Kind == SourceParameter {
		return src.ParameterMode == ParameterPinnedAtPublish || src.ParameterMode == ParameterPinnedAtStart
	}
	if src.Kind != SourceContext {
		return true
	}
	req, ok := contextRequirement(n, src.ContextKind)
	return ok && req.Pinned
}

// sourceType resolves the declared type of a mapping source.
func sourceType(def *Definition, g *graph, n *Node, src Source, parameters ParameterResolver) (ValueType, bool) {
	switch src.Kind {
	case SourceWorkflowInput:
		t, ok := fieldsByPath(def.Inputs)[src.Path]
		return t, ok
	case SourceNodeOutput:
		producer, ok := g.nodes[src.NodeID]
		if !ok {
			return ValueType{}, false
		}
		t, ok := fieldsByPath(producer.Outputs)[src.Path]
		return t, ok
	case SourceContext, SourceConstant:
		if err := src.Type.Validate(); err != nil {
			return ValueType{}, false
		}
		return src.Type, true
	case SourceParameter:
		if parameters == nil {
			return ValueType{}, false
		}
		parameter, ok := parameters.ResolveParameter(src.ParameterKey)
		if !ok || parameter.Key != src.ParameterKey || parameter.Type.Validate() != nil {
			return ValueType{}, false
		}
		return parameter.Type, true
	default:
		_ = n
		return ValueType{}, false
	}
}

// checkMappings proves every declared input field is bound exactly once, by a
// resolvable source, with an assignable type (WF-COMP-001).
func checkMappings(def *Definition, g *graph, parameters ParameterResolver, c *collector) {
	for _, id := range g.sortedNodeIDs() {
		n := g.nodes[id]
		inputs := fieldsByPath(n.Inputs)
		bound := map[string]bool{}

		for _, m := range n.InputMappings {
			loc := Location{NodeID: id, Field: m.Target}
			want, declared := inputs[m.Target]
			if !declared {
				c.add(CodeUnresolvedRef, loc, "mapping target is not a declared input field of this node")
				continue
			}
			if bound[m.Target] {
				c.add(CodeDuplicateMapping, loc, "input field is bound by more than one mapping")
				continue
			}
			bound[m.Target] = true

			if m.Source.Path == "*" {
				c.add(CodeUnrestrictedResultCopy, loc,
					"a mapping binds a declared typed field; it never copies an unrestricted result")
				continue
			}

			switch m.Source.Kind {
			case SourceWorkflowInput:
				if _, ok := fieldsByPath(def.Inputs)[m.Source.Path]; !ok {
					c.add(CodeUnresolvedRef, loc,
						"workflow input %q is not a declared workflow input field", m.Source.Path)
					continue
				}
			case SourceNodeOutput:
				producer, ok := g.nodes[m.Source.NodeID]
				if !ok {
					c.add(CodeUnresolvedRef, loc,
						"source node %q is not a declared node", m.Source.NodeID)
					continue
				}
				if !g.dominates(m.Source.NodeID, id) {
					c.add(CodeSourceNotPredecessor, loc,
						"source node %q does not run before this node on every path", m.Source.NodeID)
					continue
				}
				if _, ok := fieldsByPath(producer.Outputs)[m.Source.Path]; !ok {
					c.add(CodeUnresolvedRef, loc,
						"node %q declares no output field %q", m.Source.NodeID, m.Source.Path)
					continue
				}
			case SourceContext:
				req, ok := contextRequirement(n, m.Source.ContextKind)
				if !ok {
					c.add(CodeUnresolvedRef, loc,
						"context %q is read but not declared in required_context; an undeclared read fails",
						m.Source.ContextKind)
					continue
				}
				if !contextDeclaresPath(req, m.Source.Path) {
					c.add(CodeUnresolvedRef, loc,
						"context %q does not declare field path %q", m.Source.ContextKind, m.Source.Path)
					continue
				}
				if err := m.Source.Type.Validate(); err != nil {
					c.add(CodeUnresolvedRef, loc, "context source type: %v", err)
					continue
				}
			case SourceConstant:
				if err := m.Source.Type.Validate(); err != nil {
					c.add(CodeUnresolvedRef, loc, "constant source type: %v", err)
					continue
				}
				if m.Source.Type.Nullable {
					c.add(CodeTypeMismatch, loc, "a constant source is never nullable")
					continue
				}
			case SourceParameter:
				if m.Source.ParameterKey == "" || !m.Source.ParameterMode.valid() {
					c.add(CodeUnresolvedRef, loc, "parameter mapping requires a key and declared binding mode")
					continue
				}
				if parameters == nil {
					c.add(CodeReferenceResolverRequired, loc, "parameter mapping requires a parameter resolver")
					continue
				}
				parameter, ok := parameters.ResolveParameter(m.Source.ParameterKey)
				if !ok || parameter.Key != m.Source.ParameterKey {
					c.add(CodeUnresolvedRef, loc, "parameter %q is not declared", m.Source.ParameterKey)
					continue
				}
				if parameter.Type.Validate() != nil {
					c.add(CodeUnresolvedRef, loc, "parameter %q has an invalid declared type", m.Source.ParameterKey)
					continue
				}
				allowed := false
				for _, consumer := range parameter.AllowedConsumers {
					if consumer.Kind == "WORKFLOW" && consumer.ID == def.WorkflowID {
						allowed = true
						break
					}
				}
				if !allowed {
					c.add(CodeUnresolvedRef, loc, "workflow %q is not an allowed consumer of parameter %q", def.WorkflowID, m.Source.ParameterKey)
					continue
				}
				if parameter.Classification == "" || parameter.Classification != n.Governance.Classification {
					c.add(CodeTypeMismatch, loc, "parameter %q classification %q cannot flow to node classification %q", m.Source.ParameterKey, parameter.Classification, n.Governance.Classification)
					continue
				}
				if m.Source.ParameterMode == ParameterLive {
					if m.Source.MaxAgeSeconds == 0 {
						c.add(CodeUnresolvedRef, loc, "LIVE parameter binding requires a positive maximum age")
						continue
					}
				} else if m.Source.MaxAgeSeconds != 0 {
					c.add(CodeUnresolvedRef, loc, "maximum age is valid only for LIVE parameter bindings")
					continue
				}
				if m.Source.ParameterMode == ParameterPinnedAtPublish && (parameter.DefinitionName == "" || parameter.DefinitionVersion == "" || parameter.Revision == 0) {
					c.add(CodeUnresolvedRef, loc, "PINNED_AT_PUBLISH requires a trusted parameter value revision")
					continue
				}
				if parameter.Type.AssignableTo(want) != nil {
					c.add(CodeTypeMismatch, loc, "%s -> %s: parameter type is not assignable to target", parameter.Type, want)
					continue
				}
				if n.Type == StepDecision && m.Source.ParameterMode == ParameterLive {
					c.add(CodeMutableDecisionInput, loc, "a DECISION cannot bind a LIVE parameter")
					continue
				}
			default:
				c.add(CodeUnresolvedRef, loc,
					"mapping source kind %q is not a declared source kind", string(m.Source.Kind))
				continue
			}

			got, ok := sourceType(def, g, n, m.Source, parameters)
			if !ok {
				c.add(CodeUnresolvedRef, loc, "mapping source type does not resolve")
				continue
			}
			if err := got.AssignableTo(want); err != nil {
				c.add(CodeTypeMismatch, loc, "%s -> %s: %v", got, want, err)
			}
		}

		for _, f := range n.Inputs {
			if !bound[f.Path] {
				c.add(CodeUnresolvedRef, Location{NodeID: id, Field: f.Path},
					"declared input field has no mapping")
			}
		}

		if n.Type == StepEnd {
			checkWorkflowOutputs(def, n, c)
		}
	}
}

// checkWorkflowOutputs proves an END produces exactly the workflow's declared
// result. The END node's inputs are the workflow's output mapping: a declared
// workflow output no terminal produces is a promise the plan cannot keep.
func checkWorkflowOutputs(def *Definition, n *Node, c *collector) {
	inputs := fieldsByPath(n.Inputs)
	for _, out := range def.Outputs {
		loc := Location{NodeID: n.ID, Field: out.Path}
		got, ok := inputs[out.Path]
		if !ok {
			c.add(CodeUnresolvedRef, loc,
				"terminal does not produce declared workflow output %q", out.Path)
			continue
		}
		if err := got.AssignableTo(out.Type); err != nil {
			c.add(CodeTypeMismatch, loc,
				"workflow output %q: %s -> %s: %v", out.Path, got, out.Type, err)
		}
	}
}
