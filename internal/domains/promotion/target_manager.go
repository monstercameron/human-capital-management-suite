package promotion

// PROMOUX-005: "Include reporting-line and organization impact in
// management promotions."
//
// RED: a promotion into management can reach approval without resolving the
// target manager, organization, reporting-line edge, team impact and cycle
// check, or the review comparison omits those effects. Before this file,
// promotion.PreflightRequest had no field for a target manager at all --
// TargetPlacement only ever named a job, grade, organizational unit,
// position and pay zone -- so nothing about the reporting-line edge a
// management promotion implies was ever evaluated, let alone certified free
// of a cycle.
//
// GREEN's "position selection derives or explicitly collects authorized
// target organization and manager revisions": this file is the "explicitly
// collects authorized" half. TargetManagerSelection carries the candidate
// manager and the workers who would newly report to the subject (the
// "affected direct-report scope" GREEN requires the review to show); every
// one of those relationships is re-derived fresh from the real Organization
// capability rather than trusted from the caller's say-so, mirroring
// target_position.go's own PROMOUX-004 pattern exactly: a caller states an
// intention, this file proves it.
//
// REFACTOR: "organization semantics stay in the Organization capability;
// promotion composes its typed effect rather than copying graph logic." This
// file contains no graph traversal of its own. Existence, disclosure and
// cycle safety are all decided by internal/domains/org
// (org.ResolveManagerRelationships, org.DetectManagerCycle); this file only
// classifies the answer into a promotion.Finding a reviewer can read, the
// same division of labor target_position.go already established for the
// Position capability.
import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Finding codes for the target-manager selection.
const (
	// CodeTargetManagerNotFound is the existence/authorization ground: the
	// candidate does not exist, or exists but this caller is not authorized
	// to learn anything about their reporting relationships. Both collapse
	// to the identical finding -- see [evaluateTargetManagerSelection] --
	// so a guessed candidate and a real, unauthorized one are indistinguishable,
	// the same no-enumeration property PROMOUX-004 established for a target
	// position.
	CodeTargetManagerNotFound = "promotion.target_manager_not_found"
	// CodeManagerRelationshipCycle means at least one proposed reporting-line
	// edge -- the subject's own new manager, or one of the affected direct
	// reports' new manager becoming the subject -- would make a worker
	// report to themselves, directly or through any chain.
	CodeManagerRelationshipCycle = "promotion.manager_relationship_cycle"
	// CodeManagerChainUnresolved means at least one proposed edge could not
	// be certified either way: the chain above it could not be fully
	// resolved (withheld, stale, ambiguous or deeper than the declared
	// bound). It fails closed rather than defaulting to safe.
	CodeManagerChainUnresolved = "promotion.manager_chain_unresolved"
)

// TargetManagerSelection is the caller's stated management-promotion
// intention: who the subject would report to, and who would newly report to
// the subject as a result.
//
// There is deliberately no field here for a "current" value -- exactly
// TargetPlacement and PositionSelection's own rule: the caller states only
// what it wants to be true, and every "before" this file needs comes from
// re-reading the real Organization capability, never from the caller's own
// assertion.
type TargetManagerSelection struct {
	// Reference is the candidate worker the subject would report to.
	Reference values.EntityRef
	// AffectedDirectReports are the workers who would newly report to the
	// SUBJECT once the promotion takes effect -- the team the subject
	// gains. GREEN requires the review to show exactly this scope: who
	// else moves when this person moves. Each is checked for cycle safety
	// exactly like the subject's own new-manager edge.
	AffectedDirectReports []values.EntityRef
	// AsOf is the bitemporal coordinate the manager graph is walked at:
	// ordinarily the promotion's own effective date and known-at horizon.
	AsOf    values.Instant
	KnownAt values.KnownAt
	// ChainDepth bounds the cycle walk. See [org.CycleQuery.MaxDepth]: it is
	// a safety valve, never a business assumption about legitimate chain
	// depth.
	ChainDepth int
	// Authorize is the same per-hop disclosure hook
	// org.ResolveManagerRelationships and org.DetectManagerCycle take. It
	// must be the exact hook any manager picker used to decide which
	// candidates to disclose, or an unauthorized manager could pass here
	// while never having appeared as a choice.
	Authorize org.Authorizer
}

// ManagementImpact is GREEN's review requirement made a typed value: what a
// management promotion changes in the reporting graph, named explicitly
// rather than left for a reviewer to infer from job and grade fields alone.
// The zero value means no target-manager selection was evaluated.
type ManagementImpact struct {
	// TargetManager is the candidate manager the subject would report to,
	// once proven to exist and disclosed to this caller.
	TargetManager values.EntityRef
	// AffectedDirectReports is the exact scope [TargetManagerSelection]
	// declared: who else moves when this person moves.
	AffectedDirectReports []values.EntityRef
	// CycleSafe reports whether every proposed edge -- the subject's own
	// and every affected direct report's -- was certified free of a
	// reporting cycle. False whenever any edge is a confirmed cycle or
	// could not be certified either way.
	CycleSafe bool
}

// Evaluated reports whether a target-manager selection was actually run.
func (m ManagementImpact) Evaluated() bool { return m.TargetManager.Validate() == nil }

// Canonical returns the canonical byte encoding, so a material change to the
// target manager or the affected direct-report scope moves a promotion's
// input digest -- which is what makes GREEN's "material changes invalidate
// approval" clause bind to this file's own facts rather than only to job,
// grade and compensation.
func (m ManagementImpact) Canonical() []byte {
	evaluated := m.Evaluated()
	w := canonicalbytes.New("hcmnext.domains.promotion.ManagementImpact", promotionSchemaVer).
		Bool("evaluated?", evaluated)
	if evaluated {
		w.Value("target_manager", m.TargetManager).
			Count("affected_direct_reports", len(m.AffectedDirectReports))
		for _, report := range m.AffectedDirectReports {
			w.Field("affected_direct_report", report.Canonical())
		}
		w.Bool("cycle_safe", m.CycleSafe)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func targetManagerNotFoundFinding() Finding {
	return Finding{
		Code: CodeTargetManagerNotFound, Severity: SeverityBlocking, Field: "target.manager",
		Message: "the selected manager could not be found",
	}
}

func managerRelationshipCycleFinding() Finding {
	return Finding{
		Code: CodeManagerRelationshipCycle, Severity: SeverityBlocking, Field: "target.manager",
		Message: "this management change would make a worker report to themselves, directly or through the reporting chain",
	}
}

func managerChainUnresolvedFinding() Finding {
	return Finding{
		Code: CodeManagerChainUnresolved, Severity: SeverityNeedsData, Field: "target.manager",
		Message: "the reporting chain above the selected manager could not be fully resolved; cycle safety cannot be certified",
	}
}

// managerEdge is one proposed direct-manager relationship this file checks:
// node's manager would become proposedManager.
type managerEdge struct {
	node            values.EntityRef
	proposedManager values.EntityRef
}

// edgesFor lists every reporting-line edge a management promotion proposes:
// the subject's own new manager, and each affected direct report's manager
// becoming the subject. Both families are checked identically -- neither is
// a special case -- because a cycle can close through either one.
func edgesFor(subject values.EntityRef, sel TargetManagerSelection) []managerEdge {
	edges := make([]managerEdge, 0, 1+len(sel.AffectedDirectReports))
	edges = append(edges, managerEdge{node: subject, proposedManager: sel.Reference})
	for _, report := range sel.AffectedDirectReports {
		edges = append(edges, managerEdge{node: report, proposedManager: subject})
	}
	return edges
}

// evaluateTargetManagerSelection is this todo's RED-closing composition. It
// runs only when req.TargetManagerSelection is set -- a caller who names no
// target manager gets exactly the job/grade/org-only preflight this package
// always ran, unchanged -- but the moment one is set, every ground below is
// re-derived from internal/domains/org's own ports rather than trusted from
// the caller's assertion.
//
// Only a genuine contract failure (a nil manager-facts reader, a malformed
// bitemporal coordinate, or the reader itself failing) is returned as an
// error. Every business refusal is a Finding.
func evaluateTargetManagerSelection(ctx context.Context, req PreflightRequest) ([]Finding, ManagementImpact, error) {
	sel := req.TargetManagerSelection
	if sel == nil {
		return nil, ManagementImpact{}, nil
	}
	if req.ManagerFacts == nil {
		return nil, ManagementImpact{}, fmt.Errorf("%w: no manager facts reader is configured for a selected target manager", ErrRequestInvalid)
	}
	if err := sel.AsOf.Validate(); err != nil {
		return nil, ManagementImpact{}, fmt.Errorf("%w: target manager as-of: %w", ErrRequestInvalid, err)
	}
	if sel.ChainDepth < 1 {
		return nil, ManagementImpact{}, fmt.Errorf("%w: target manager chain depth must be positive", ErrRequestInvalid)
	}

	if err := sel.Reference.Validate(); err != nil || sel.Reference.Tenant != req.Tenant || sel.Reference.Kind != people.KindWorker {
		// A malformed or cross-tenant reference -- including every guessed
		// identifier no picker ever issued -- fails closed at the existence
		// ground without ever reaching the org domain, exactly like a
		// well-formed reference naming nobody.
		return []Finding{targetManagerNotFoundFinding()}, ManagementImpact{}, nil
	}

	set, err := req.ManagerFacts.WorkerFactsAt(ctx, org.WorkerFactsQuery{
		Tenant: req.Tenant, Worker: sel.Reference, AsOf: sel.AsOf, KnownAt: sel.KnownAt,
	})
	if err != nil {
		return nil, ManagementImpact{}, fmt.Errorf("promotion: read target manager facts: %w", err)
	}
	if err := set.Validate(); err != nil {
		return nil, ManagementImpact{}, fmt.Errorf("promotion: target manager facts: %w", err)
	}
	if !set.Exists {
		return []Finding{targetManagerNotFoundFinding()}, ManagementImpact{}, nil
	}

	// Fold "exists, but this caller may not learn anything about their own
	// reporting relationship" into the identical not-found refusal. An
	// authorization denial must never be distinguishable from a nonexistent
	// candidate -- the same enumeration-closing rule PROMOUX-004 required for
	// a guessed unauthorized position.
	//
	// resolution.Direct, not the aggregate resolution.Disclosure, is the
	// right signal: [ResolveManagerRelationships] sets the aggregate
	// disclosure to FULL the instant a worker is found to exist, before any
	// hop is even evaluated, so a single denied hop only ever downgrades it
	// to PARTIAL -- it can never surface as WITHHELD at the aggregate level
	// once the subject itself exists. The candidate's own direct-manager hop
	// is where a denial actually shows up: WITHHELD means the whole hop was
	// refused, and a manager reference specifically denied (the hop
	// disclosed but its Manager field ruled DENY) is functionally the same
	// blindness for this purpose -- either way this caller cannot verify
	// anything about the candidate's own reporting line.
	//
	// The walk is bounded by the selection's own ChainDepth, not by one hop:
	// org.ResolveManagerRelationships refuses a chain that continues past
	// its declared depth, so a one-hop bound failed for every candidate who
	// has a manager of their own (REV-091-02 found this once the review page
	// evaluated real reporting lines). A chain too deep or already looping
	// above the candidate cannot be certified, which is the unresolved
	// finding, never a contract failure and never "safe".
	visibility, err := org.ResolveManagerRelationships(ctx, req.ManagerFacts, org.ManagerResolutionRequest{
		Tenant: req.Tenant, Worker: sel.Reference, AsOf: sel.AsOf, KnownAt: sel.KnownAt,
		MaxDepth: sel.ChainDepth, Authorize: sel.Authorize,
	})
	switch {
	case errors.Is(err, org.ErrDepthExceeded), errors.Is(err, org.ErrRelationshipCycle):
		return []Finding{managerChainUnresolvedFinding()}, ManagementImpact{}, nil
	case err != nil:
		return nil, ManagementImpact{}, fmt.Errorf("promotion: resolve target manager visibility: %w", err)
	}
	if visibility.Direct != nil &&
		(visibility.Direct.Disclosure == people.DisclosureWithheld || visibility.Direct.Manager.Access != people.AccessAuthorized) {
		return []Finding{targetManagerNotFoundFinding()}, ManagementImpact{}, nil
	}

	// Real reachability, over every proposed edge -- never a single-hop
	// "is the new manager the subject themselves" check, and never a fixed
	// depth assumption. org.DetectManagerCycle owns the walk; this file only
	// asks it, once per edge, and classifies what comes back.
	var anyCycle, anyUndetermined bool
	for _, edge := range edgesFor(req.Subject, *sel) {
		finding, err := org.DetectManagerCycle(ctx, req.ManagerFacts, org.CycleQuery{
			Tenant: req.Tenant, Node: edge.node, ProposedManager: edge.proposedManager,
			AsOf: sel.AsOf, KnownAt: sel.KnownAt, MaxDepth: sel.ChainDepth, Authorize: sel.Authorize,
		})
		if err != nil {
			return nil, ManagementImpact{}, fmt.Errorf("promotion: detect manager cycle: %w", err)
		}
		switch finding.Status {
		case org.CycleStatusCycle:
			anyCycle = true
		case org.CycleStatusUndetermined:
			anyUndetermined = true
		}
	}

	affected := append([]values.EntityRef(nil), sel.AffectedDirectReports...)
	sort.Slice(affected, func(i, j int) bool { return affected[i].Id < affected[j].Id })
	impact := ManagementImpact{
		TargetManager:         sel.Reference,
		AffectedDirectReports: affected,
		CycleSafe:             !anyCycle && !anyUndetermined,
	}

	switch {
	case anyCycle:
		return []Finding{managerRelationshipCycleFinding()}, impact, nil
	case anyUndetermined:
		// No zero value meaning permissive: an unresolved chain fails closed
		// with its own finding rather than being reported safe by omission.
		return []Finding{managerChainUnresolvedFinding()}, impact, nil
	default:
		return nil, impact, nil
	}
}
