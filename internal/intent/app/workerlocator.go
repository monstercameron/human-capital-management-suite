package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Resolving "which worker did somebody mean" is a separate question from
// "what may this caller see about them", and it has to be answered the same
// way everywhere: the domain-input resolver, the workspace's read surface and
// the journey engine all accept a worker reference typed by a person, and if
// they disagreed about what one means the same string would name different
// people on different surfaces.
//
// So there is exactly one locator per composed cell. [NewCell] builds it, and
// every surface that accepts a worker reference is handed that value. The
// corpus is asked first and its answer is final; only when it does not know
// the reference is the tenant's created population asked. That is the same
// order internal/data/workforce.NewLayeredWorkerFacts reads facts in, and it
// is what makes a created worker unable to shadow a corpus one.

// WorkerLocation is one resolved worker reference.
type WorkerLocation struct {
	// Ref is the entity reference every governed read names the worker by.
	Ref values.EntityRef
	// Key is the stable reference a surface lists and a request payload
	// carries: the corpus key, or the created worker's own key. It is never
	// a raw entity id for a worker that has a key.
	Key string
	// Created is the durable row when this is a created worker, and nil for a
	// corpus one. It is what the journey's baseline reads a created worker's
	// declared compensation from instead of the ported legacy scenario's,
	// which describes exactly one worker and is not this one.
	Created *workforce.WorkerRow
}

// WorkerLocator resolves a worker reference within one tenant, reporting
// whether it names anybody at all.
//
// Absence is a false rather than an error, because "no such worker" is an
// answer every caller has to render; an error means the lookup itself failed.
type WorkerLocator func(ctx context.Context, tenant values.TenantId, ref string) (WorkerLocation, bool, error)

// corpusWorkerLocator resolves against the release's fixed corpus and nothing
// else. It is the locator a cell composed with no execution database gets, and
// it behaves exactly as this repository's worker resolution behaved before a
// created population existed.
func corpusWorkerLocator(_ context.Context, tenant values.TenantId, ref string) (WorkerLocation, bool, error) {
	return locateCorpusWorker(tenant, ref)
}

// locateCorpusWorker resolves a reference against the corpus by key or by
// entity id, falling back to treating a well-formed identifier as an entity id
// this cell has simply not been told about yet.
//
// The trailing permissive case is deliberate and pre-existing: a caller
// holding a raw id must still resolve, and it is the governed read -- not this
// function -- that then reports the worker is not disclosed. It accepts only
// what internal/kernel/values would accept as an identifier, so a reference
// that is neither a corpus key nor a well-formed id is refused here.
func locateCorpusWorker(tenant values.TenantId, ref string) (WorkerLocation, bool, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return WorkerLocation{}, false, nil
	}
	if location, ok, err := locateCorpusWorkerExact(tenant, ref); err != nil || ok {
		return location, ok, err
	}
	return locateIdentifier(tenant, ref)
}

// newWorkerLocator layers the tenant's created population behind the corpus.
//
// A created worker's display reference is a readable slug, while its
// durable key is the opaque worker id. This seam resolves both the display
// slug and the durable id.
//
// A nil database or a nil tenant mapping yields the corpus locator unchanged.
func newWorkerLocator(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) WorkerLocator {
	if db == nil || tenantUUID == nil {
		return corpusWorkerLocator
	}
	created := workforce.NewFacts(db, tenantUUID)
	return func(ctx context.Context, tenant values.TenantId, ref string) (WorkerLocation, bool, error) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return WorkerLocation{}, false, nil
		}
		// The corpus is authoritative for every worker it knows, so it is
		// asked first -- but only for an exact key or id match. Its permissive
		// fallback is deferred to the end, because a created worker's key
		// would otherwise never get the chance to resolve.
		//
		// PROMOUX-015: and only for a tenant that has no population of its
		// own. ListWorkers serves the corpus as a fallback rather than as an
		// addition, and a reference that resolved to somebody the directory
		// does not offer would put the two halves of this cell back out of
		// step -- a person nobody can pick but every form accepts. The
		// population is read only when the reference really is a corpus one,
		// which on a tenant with its own people it never is, so this costs
		// nothing on the paths that matter.
		corpusLocation, isCorpus, err := locateCorpusWorkerExact(tenant, ref)
		if err != nil {
			return WorkerLocation{}, false, err
		}
		if isCorpus {
			populated, popErr := created.Populated(ctx, tenant)
			if popErr != nil {
				return WorkerLocation{}, false, fmt.Errorf("app: read whether %s has a population of its own: %w", tenant, popErr)
			}
			if !populated {
				return corpusLocation, true, nil
			}
			// A populated tenant serves only its own people. The reference
			// still falls through to the durable lookup and then to the
			// permissive identifier fallback, so a raw entity id resolves
			// exactly as it always did -- but this cell no longer names
			// somebody its own directory does not offer.
		}
		row, found, err := created.Lookup(ctx, tenant, ref)
		if err != nil {
			return WorkerLocation{}, false, fmt.Errorf("app: resolve the created worker %q: %w", ref, err)
		}
		if !found {
			row, found, err = lookupCreatedDisplaySlug(ctx, db, tenantUUID, tenant, ref)
			if err != nil {
				return WorkerLocation{}, false, fmt.Errorf("app: resolve created worker display reference: %w", err)
			}
		}
		if found {
			return WorkerLocation{
				Ref: values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: row.WorkerID.String()},
				Key: journeyWorkerKey(row.PreferredName, row.LegalName, shortID(row.WorkerID)),
				// The row travels with the location because the caller that
				// needs it -- the journey's compensation baseline -- would
				// otherwise read the same row a second time.
				Created: &row,
			}, true, nil
		}
		// The corpus has already been ruled out for this reference -- either
		// it is not a corpus one, or this tenant has its own people -- so
		// only the permissive identifier case is left.
		return locateIdentifier(tenant, ref)
	}
}

// lookupCreatedDisplaySlug resolves the read-time name slug after the opaque
// durable key lookup has had its chance. It scans only the tenant's own rows;
// the slug is a presentation convenience, never a persisted coordinate.
func lookupCreatedDisplaySlug(ctx context.Context, db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, tenant values.TenantId, ref string) (workforce.WorkerRow, bool, error) {
	tenantID := tenantUUID(tenant)
	tx, err := db.Begin(ctx)
	if err != nil {
		return workforce.WorkerRow{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return workforce.WorkerRow{}, false, err
	}
	rows, err := (workforce.Store{}).List(ctx, tx, tenantID)
	if err != nil {
		return workforce.WorkerRow{}, false, err
	}
	for _, row := range rows {
		if journeyWorkerKey(row.PreferredName, row.LegalName, shortID(row.WorkerID)) == ref {
			return row, true, nil
		}
	}
	return workforce.WorkerRow{}, false, nil
}

// tenantHasPopulation reports whether this cell can see a durable population
// for the tenant. A cell that cannot read one at all answers false, which is
// the same answer an empty tenant gives: in both cases the release's fixed
// corpus is what this cell has to serve.
func (e *journeyEngine) tenantHasPopulation(ctx context.Context, tenant values.TenantId) (bool, error) {
	if e == nil || e.db == nil || e.svc == nil || e.svc.tenantUUID == nil {
		return false, nil
	}
	return workforce.NewFacts(e.db, e.svc.tenantUUID).Populated(ctx, tenant)
}

// locateIdentifier is [locateCorpusWorker]'s trailing permissive case on its
// own: a reference that is a well-formed worker identifier resolves to it,
// and anything else resolves to nothing.
//
// It is reached when the corpus has already been ruled out for this tenant,
// so re-running the corpus match would be the one thing that must not happen.
func locateIdentifier(tenant values.TenantId, ref string) (WorkerLocation, bool, error) {
	candidate := values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: ref}
	if candidate.Validate() != nil {
		return WorkerLocation{}, false, nil
	}
	return WorkerLocation{Ref: candidate, Key: ref}, true, nil
}

// locateCorpusWorkerExact is [locateCorpusWorker] without its permissive
// fallback: an exact corpus key or id, or nothing.
func locateCorpusWorkerExact(tenant values.TenantId, ref string) (WorkerLocation, bool, error) {
	profiles, err := fixtures.Workers()
	if err != nil {
		return WorkerLocation{}, false, fmt.Errorf("app: read the worker corpus: %w", err)
	}
	for _, p := range profiles {
		if p.Key == ref || p.ID == ref {
			return WorkerLocation{
				Ref: values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: p.ID},
				Key: p.Key,
			}, true, nil
		}
	}
	return WorkerLocation{}, false, nil
}
