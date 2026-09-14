package app

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// PROMOUX-015: the manager-chain relationship facts a governed read presents
// to authz.
//
// authz.ResolveAuthorizationScope grants the manager role only over a subject
// a MANAGER_CHAIN relationship fact covers, and it never invents one. Before
// this file no call site in this package supplied any, so a principal whose
// only authority over a worker was managing them was refused
// relationship_not_established for every worker, and the only principals who
// could propose, simulate or review a promotion were administrative ones.
// The facts come from the one manager relationship the created population
// records, journey_worker.manager_relationship_ref, read through the cell's
// own worker locator.

// maxManagerChainDepth bounds how many reporting hops one read walks. It is
// far deeper than any seeded hierarchy and exists so a malformed cycle can
// never make a read unbounded.
const maxManagerChainDepth = 16

// managerChainSource attributes every fact this file builds.
const managerChainSource = "journey_worker.manager_relationship_ref"

// managerChainFacts returns the MANAGER_CHAIN fact that principal manages
// subject directly or indirectly, or nil. It walks the subject's reporting
// line upward through locate and stops at the first worker whose key or id
// is the principal's subject, at a reference that names no created worker (a
// sentinel such as "board:harborcare"), at a repeated worker, or at
// [maxManagerChainDepth]. It reads nothing for a principal that does not hold
// the manager role, the only role authz grants a MANAGER_CHAIN fact to. A
// locator failure yields no fact: a relationship that could not be read is
// not established, and authz then refuses rather than guessing.
func managerChainFacts(ctx context.Context, locate WorkerLocator, principal *trust.Principal, subject values.EntityRef) []authz.RelationshipFact {
	if locate == nil || principal == nil || !principal.HasRole(string(authz.RoleManager)) || principal.Subject() == "" {
		return nil
	}
	location, found, err := locate(ctx, subject.Tenant, subject.Id)
	if err != nil || !found || location.Created == nil {
		return nil
	}
	row := *location.Created
	seen := map[string]bool{row.WorkerID.String(): true}
	ref := row.ManagerRelationshipRef
	for depth := 0; depth < maxManagerChainDepth && ref != ""; depth++ {
		manager, ok, lookupErr := locate(ctx, subject.Tenant, ref)
		if lookupErr != nil || !ok || manager.Created == nil {
			return nil
		}
		if manager.Created.WorkerKey == principal.Subject() || manager.Created.WorkerID.String() == principal.Subject() {
			fact, factErr := managerChainFact(subject, row)
			if factErr != nil {
				return nil
			}
			return []authz.RelationshipFact{fact}
		}
		id := manager.Created.WorkerID.String()
		if seen[id] {
			return nil
		}
		seen[id] = true
		ref = manager.Created.ManagerRelationshipRef
	}
	return nil
}

// managerChainFact builds the fact from the subject's own durable row: it is
// effective from the row's effective date and carries the row's own knowledge
// coordinates.
func managerChainFact(subject values.EntityRef, row workforce.WorkerRow) (authz.RelationshipFact, error) {
	effectiveFrom, err := values.ParseLocalDate(row.EffectiveFrom)
	if err != nil {
		return authz.RelationshipFact{}, err
	}
	start := time.Date(int(effectiveFrom.Year()), effectiveFrom.Month(), int(effectiveFrom.Day()), 0, 0, 0, 0, time.UTC)
	effective, err := values.NewOpenInstantInterval(values.NewInstant(start))
	if err != nil {
		return authz.RelationshipFact{}, err
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(row.KnownAt))
	if err != nil {
		return authz.RelationshipFact{}, err
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(row.RecordedAt))
	if err != nil {
		return authz.RelationshipFact{}, err
	}
	fact := authz.RelationshipFact{
		Kind: authz.RelationshipManagerChain, Subject: subject, Source: managerChainSource,
		Effective: effective, RecordedAt: recordedAt, KnownAt: knownAt,
	}
	return fact, fact.Validate()
}
