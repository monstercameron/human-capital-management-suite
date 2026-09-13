package productui

// UXAUDIT-004 GREEN: reporting lines must nest by the authorized manager
// relationship org.ResolveManagerRelationships actually reports, never by
// matching Person.Manager display names -- the defect this todo closes (see
// planning/todos.md "## 69. Live product UX audit remediation", UXAUDIT-004).
// PROMOUX-005 added org.ResolveManagerRelationships and orgfacts.Reader
// specifically so callers stop inventing a second hierarchy source; this file
// is the one seam productui uses to consume that projection, mirroring the
// pattern compensation_guardrail.go already established for
// promotion.CompensationGuardrail (a domain function decides; this file only
// adapts and renders).
//
// organizationHopFacts is an org.WorkerFacts adapter scoped to exactly ONE
// worker's own direct-manager hop. Any worker other than the one being
// resolved is answered as having no further relationships, so
// org.ResolveManagerRelationships always stops after that single hop. This
// page does not need the resolver's full upward walk to the ultimate root --
// the tree is assembled by connecting one hop per visible worker -- and
// walking further would let a cycle or a long chain far above the worker
// being rendered fail that worker's own, otherwise perfectly resolvable,
// immediate relationship. It also keeps each worker's resolution O(1) instead
// of O(chain length), since a page can resolve every visible worker this way.
//
// Boundary: EntityRef.Tenant here is a constant, valid slug used only to
// satisfy values.EntityRef's typed contract for this presentation-local
// computation. The adapter never reads a store and never checks the tenant
// against anything, so no real tenant claim is made; the real tenant-scoped
// production reader is internal/data/orgfacts.Reader, used elsewhere for the
// promotion domain's own manager-chain reads.

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const organizationRelationshipTenant = values.TenantId("organization-projection")

const (
	organizationRelationshipPolicy  = "productui.organization_relationship_projection/v1"
	organizationRelationshipStream  = "productui.organization_relationship"
	organizationRelationshipPurpose = "organization.reporting_line"
)

// organizationRelationshipAsOf is a fixed instant rather than time.Now(): the
// resolution never needs wall-clock time (the synthetic fact's effective
// interval is open-started from the same fixed instant), and a fixed instant
// keeps this page's rendered output -- including the whole-shell SHA-256
// goldens internal/humanwork/productui pins -- reproducible byte for byte.
var organizationRelationshipAsOf = values.NewInstant(organizationRelationshipEpoch())

func organizationRelationshipEpoch() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

// organizationRelationshipOutcome is the one authorized relationship
// projection every organization-node renderer consumes -- the flat
// organization list, the organization tree, and the Myself subtree alike.
// Resolved is false only when the identity or resolution machinery itself
// could not run at all (a non-canonical worker id, or a genuine self-report
// cycle); that is a distinct, honestly labeled state and is never rendered as
// a plain, unexplained root.
type organizationRelationshipOutcome struct {
	resolution org.ManagerResolution
	resolved   bool
}

// organizationHopFacts implements org.WorkerFacts for one worker's own hop.
type organizationHopFacts struct {
	subject   values.EntityRef
	fact      *org.ManagerRelationshipFact
	watermark values.RevisionToken
}

func (f organizationHopFacts) WorkerFactsAt(_ context.Context, q org.WorkerFactsQuery) (org.WorkerFactSet, error) {
	if q.Worker != f.subject {
		// Any worker other than the one this hop answers for is reported as
		// having no further relationships -- see this file's header comment.
		return org.WorkerFactSet{Worker: q.Worker, Exists: true, Watermark: f.watermark, PolicyVersion: organizationRelationshipPolicy}, nil
	}
	var relationships []org.ManagerRelationshipFact
	if f.fact != nil {
		relationships = []org.ManagerRelationshipFact{*f.fact}
	}
	return org.WorkerFactSet{Worker: q.Worker, Exists: true, Relationships: relationships, Watermark: f.watermark, PolicyVersion: organizationRelationshipPolicy}, nil
}

// organizationRelationshipAuthorizer disclose-allows the one relationship
// this adapter ever asks about, unless withheld is set -- in which case it
// reports the subject itself as non-disclosable, which is what makes
// org.ResolveManagerRelationships return DisclosureWithheld rather than a
// value. There is no third state: this adapter answers about exactly one
// worker's own relationship, whose visibility is already the caller's to
// decide (Person.ManagerRelationshipWithheld).
func organizationRelationshipAuthorizer(withheld bool) org.Authorizer {
	return func(org.ManagerRelationshipFact) people.AuthorizationDecision {
		if withheld {
			return people.AuthorizationDecision{
				PolicyVersion: organizationRelationshipPolicy, Purpose: organizationRelationshipPurpose,
				SubjectDisclosable: false, SubjectDenialReason: "organization.manager_relationship_withheld",
			}
		}
		return people.AuthorizationDecision{
			PolicyVersion: organizationRelationshipPolicy, Purpose: organizationRelationshipPurpose,
			SubjectDisclosable: true,
			Fields:             map[people.FieldID]people.FieldRuling{people.FieldManagerRelation: {Effect: people.EffectAllow}},
		}
	}
}

// organizationEntityRef builds the typed identity org.ResolveManagerRelationships
// requires, or reports ok=false for an id this page cannot validate (a test
// fixture's short id, or a genuinely malformed value). A false result is
// never treated as permissive: callers map it to an explained, undetermined
// placement, never to a plain root.
func organizationEntityRef(id string) (values.EntityRef, bool) {
	ref := values.EntityRef{Tenant: organizationRelationshipTenant, Kind: people.KindWorker, Id: id}
	if ref.Validate() != nil {
		return values.EntityRef{}, false
	}
	return ref, true
}

// buildOrganizationRelationship resolves one person's authorized direct
// manager hop. It never guesses: an identity that cannot be validated, or a
// resolution the domain itself refuses (a self-report cycle), comes back with
// resolved=false rather than a fabricated placement.
func buildOrganizationRelationship(person Person) organizationRelationshipOutcome {
	subject, ok := organizationEntityRef(person.WorkerID)
	if !ok {
		return organizationRelationshipOutcome{}
	}
	watermark, err := values.NewSequenceRevision(organizationRelationshipStream, 1)
	if err != nil {
		return organizationRelationshipOutcome{}
	}
	adapter := organizationHopFacts{subject: subject, watermark: watermark}
	if person.ManagerID != "" {
		if managerRef, ok := organizationEntityRef(person.ManagerID); ok {
			fact, err := organizationManagerFact(subject, managerRef, person)
			if err != nil {
				return organizationRelationshipOutcome{}
			}
			adapter.fact = &fact
		}
		// A manager id that fails to validate as a canonical identity cannot be
		// asserted as a fact; the worker resolves as VACANT below, the same
		// honest answer internal/data/orgfacts.Reader gives an unresolvable
		// manager_relationship_ref, rather than silently reporting to no one
		// while claiming an explanation was owed.
	}
	resolution, err := org.ResolveManagerRelationships(context.Background(), adapter, org.ManagerResolutionRequest{
		Tenant: organizationRelationshipTenant, Worker: subject, AsOf: organizationRelationshipAsOf, MaxDepth: 2,
		Authorize: organizationRelationshipAuthorizer(person.ManagerRelationshipWithheld),
	})
	if err != nil {
		return organizationRelationshipOutcome{}
	}
	return organizationRelationshipOutcome{resolution: resolution, resolved: true}
}

func organizationManagerFact(worker, manager values.EntityRef, person Person) (org.ManagerRelationshipFact, error) {
	effective, err := values.NewOpenInstantInterval(organizationRelationshipAsOf)
	if err != nil {
		return org.ManagerRelationshipFact{}, err
	}
	knownAt, err := values.NewKnownAt(organizationRelationshipAsOf)
	if err != nil {
		return org.ManagerRelationshipFact{}, err
	}
	recordedAt, err := values.NewRecordedAt(organizationRelationshipAsOf)
	if err != nil {
		return org.ManagerRelationshipFact{}, err
	}
	revision, err := values.NewSequenceRevision(organizationRelationshipStream, 1)
	if err != nil {
		return org.ManagerRelationshipFact{}, err
	}
	return org.ManagerRelationshipFact{
		RelationshipID: "organization-relationship:" + worker.Id + ">" + manager.Id,
		Type:           org.RelationshipDirectManager,
		Worker:         worker,
		Manager:        manager,
		AssignmentID:   "organization-relationship:" + worker.Id,
		Effective:      effective,
		KnownAt:        knownAt,
		Revision:       revision,
		Authority:      evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "productui.organization", PolicyRef: organizationRelationshipPolicy},
		Provenance:     evidence.Provenance{Source: "productui.organization", EvidenceRef: "person:" + person.ID, RecordedAt: recordedAt},
	}, nil
}

// organizationPlacement is the presentation verdict for one worker: either
// nested under a specific, visible manager, or a root -- genuine (no
// explanation needed, the honest top of a chain) or explained (an honest
// account of why this worker could not be nested: not visible, withheld,
// ambiguous, stale, disagreeing, cyclical, or otherwise undetermined).
type organizationPlacement struct {
	nested      bool
	managerID   string
	explanation string // an i18n key; empty means no explanation is owed.
}

// organizationPlacementFor maps one resolution onto a placement. Every
// org.ResolutionStatus and people.Disclosure branch is named explicitly; the
// default cases are unreachable today but still resolve to an explained,
// undetermined root rather than a plain one, so a future status or
// disclosure value can never silently become "invisible hierarchy is fine."
func organizationPlacementFor(outcome organizationRelationshipOutcome, visibleWorkerID map[string]bool) organizationPlacement {
	if !outcome.resolved {
		return organizationPlacement{explanation: "organization.relationship_undetermined"}
	}
	r := outcome.resolution
	if r.Direct == nil {
		switch r.Status {
		case org.StatusVacant:
			return organizationPlacement{}
		case org.StatusAmbiguous:
			return organizationPlacement{explanation: "organization.relationship_ambiguous"}
		case org.StatusStale:
			return organizationPlacement{explanation: "organization.relationship_stale"}
		case org.StatusDisagreeing:
			return organizationPlacement{explanation: "organization.relationship_disagreeing"}
		case org.StatusResolved:
			return organizationPlacement{explanation: "organization.relationship_undetermined"}
		default:
			return organizationPlacement{explanation: "organization.relationship_undetermined"}
		}
	}
	hop := *r.Direct
	switch hop.Disclosure {
	case people.DisclosureWithheld:
		return organizationPlacement{explanation: "organization.manager_withheld"}
	case people.DisclosureFull, people.DisclosurePartial:
		if hop.Manager.Access != people.AccessAuthorized {
			return organizationPlacement{explanation: "organization.manager_withheld"}
		}
		if !visibleWorkerID[hop.Manager.Value.Id] {
			return organizationPlacement{explanation: "organization.manager_not_visible"}
		}
		return organizationPlacement{nested: true, managerID: hop.Manager.Value.Id}
	default:
		return organizationPlacement{explanation: "organization.relationship_undetermined"}
	}
}
