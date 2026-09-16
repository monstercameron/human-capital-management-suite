package workflow

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// EffectRole classifies one write-effect node into the set the workflow
// runtime spec names under "authoritative core versus downstream effects"
// (WF-RUN-037). It is not a second effect taxonomy: the node's
// [capability.EffectClass] still says what the node can mutate, and the role
// only says how that mutation relates to the business outcome. Which roles a
// class admits is derived from the class itself ([AdmittedEffectRoles]), so
// a PURE or READ_ONLY node carries no role at all.
type EffectRole string

// The three declared roles. There is no fourth.
const (
	// RoleAuthoritativeCore is a domain write inside the one declared
	// transaction authority and commit boundary. Its commit is the business
	// outcome; nothing downstream may roll it back.
	RoleAuthoritativeCore EffectRole = "AUTHORITATIVE_CORE"
	// RoleDownstreamEffect is independently committed work -- payroll, IAM,
	// messaging, a connector or an outbox leg -- observed and reconciled after
	// the core commit. Its failure routes to reconciliation, never back into
	// the core.
	RoleDownstreamEffect EffectRole = "DOWNSTREAM_EFFECT"
	// RoleDerivedUpdate is rebuildable projection, search, analytics or cache
	// work. Its failure is marked rebuildable from the core instead of failing
	// the workflow.
	RoleDerivedUpdate EffectRole = "DERIVED_UPDATE"
)

// Valid reports whether r is one of the three declared roles.
func (r EffectRole) Valid() bool {
	switch r {
	case RoleAuthoritativeCore, RoleDownstreamEffect, RoleDerivedUpdate:
		return true
	default:
		return false
	}
}

// AdmittedEffectRoles returns the roles a node of this effect class may
// declare, in canonical order. A read admits none: there is no mutation to
// classify. An internal mutation may be the core, an outbox leg downstream of
// the core, or a derived update. An external or irreversible mutation is
// only ever a downstream effect: an external provider never participates in
// the core's ACID commit, and an irreversible effect is not rebuildable.
func AdmittedEffectRoles(class capability.EffectClass) []EffectRole {
	switch class {
	case capability.EffectInternalMutation:
		return []EffectRole{RoleAuthoritativeCore, RoleDownstreamEffect, RoleDerivedUpdate}
	case capability.EffectExternalMutation, capability.EffectIrreversibleExternalMutation:
		return []EffectRole{RoleDownstreamEffect}
	default:
		return nil
	}
}

func roleAdmitted(class capability.EffectClass, role EffectRole) bool {
	for _, r := range AdmittedEffectRoles(class) {
		if r == role {
			return true
		}
	}
	return false
}

// NodesWithRole lists, in plan order, the ids of the compiled nodes carrying
// role.
func (p *CompiledWorkflow) NodesWithRole(role EffectRole) []string {
	var out []string
	for _, n := range p.Nodes {
		if n.EffectRole == role {
			out = append(out, n.ID)
		}
	}
	return out
}

// analyzeEffectRoles requires and validates the effect role of every node
// (WF-RUN-037) and returns node ids per role for the effect summary. It runs
// after [analyzeEffects] and reads the same effect class, so a manifest that
// changes a capability's class changes which roles its node may declare.
func analyzeEffectRoles(g *graph, records map[string]capability.Record, c *collector) map[string][]string {
	byRole := map[string][]string{}
	var cores, dependents []*Node
	for _, id := range g.sortedNodeIDs() {
		n := g.nodes[id]
		loc := Location{NodeID: id, Field: "effect_role"}
		class := effectClassOf(n, records)
		if !class.Valid() {
			continue // analyzeEffects already refused the class itself
		}
		switch {
		case !class.IsWrite() && n.EffectRole != "":
			c.add(CodeEffectRoleConflict, loc,
				"%s resolves to %s, which mutates nothing; only a write effect declares an effect role", n.Type, class)
			continue
		case !class.IsWrite():
			continue
		case n.EffectRole == "":
			c.add(CodeEffectRoleMissing, loc,
				"%s resolves to %s and must declare AUTHORITATIVE_CORE, DOWNSTREAM_EFFECT or DERIVED_UPDATE", n.Type, class)
			continue
		case !n.EffectRole.Valid():
			c.add(CodeEffectRoleConflict, loc, "effect role %q is not one of the three declared roles", string(n.EffectRole))
			continue
		case !roleAdmitted(class, n.EffectRole):
			c.add(CodeEffectRoleConflict, loc, "%s cannot be %s; it admits %v", class, n.EffectRole, AdmittedEffectRoles(class))
			continue
		}
		byRole[string(n.EffectRole)] = append(byRole[string(n.EffectRole)], id)
		if n.EffectRole == RoleAuthoritativeCore {
			cores = append(cores, n)
			continue
		}
		dependents = append(dependents, n)
		if n.FailureRoute == "" {
			c.add(CodeEffectRoleUnrouted, Location{NodeID: id, Field: "failure_route"},
				"a %s must declare the failure route its reconciliation or rebuild takes; its failure never fails the core", n.EffectRole)
		}
	}
	for _, n := range dependents {
		checkDependentOrder(g, n, records, cores, c)
	}
	for k := range byRole {
		sort.Strings(byRole[k])
	}
	if len(byRole) == 0 {
		return nil
	}
	return byRole
}

// checkDependentOrder proves a downstream effect or derived update never
// gates the core and, where it needs one, follows it. A core reachable from
// a dependent node would commit only after independently committed or
// rebuildable work succeeded -- exactly the coupling the classification
// exists to forbid. A derived update and an internal outbox leg are defined
// relative to a core, so each must be reachable from one; an external effect
// may stand alone.
func checkDependentOrder(g *graph, n *Node, records map[string]capability.Record, cores []*Node, c *collector) {
	loc := Location{NodeID: n.ID, Field: "effect_role"}
	follows := false
	for _, core := range cores {
		if g.reaches(n.ID, core.ID) {
			c.add(CodeEffectRoleOrder, loc,
				"%s reaches AUTHORITATIVE_CORE %s; the core must commit independently of it", n.EffectRole, core.ID)
		}
		if g.reaches(core.ID, n.ID) {
			follows = true
		}
	}
	needsCore := n.EffectRole == RoleDerivedUpdate || effectClassOf(n, records) == capability.EffectInternalMutation
	if needsCore && !follows {
		c.add(CodeEffectRoleOrder, loc,
			"%s on %s is not reachable from any AUTHORITATIVE_CORE, so there is no committed core to reconcile or rebuild it from",
			n.EffectRole, effectClassOf(n, records))
	}
}
