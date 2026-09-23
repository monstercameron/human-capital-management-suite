package app

// UXLIVE-011: the served propose form asked for the target position as free
// text, under help that said "Entering an unconfirmed position will block
// the request." That is a field predicting its own refusal. The refusal was
// real -- internal/domains/promotion.evaluateTargetPositionSelection has
// required a server-issued position.RevisionRef since PROMOUX-004, so a
// typed identifier like POS-ENG-MGR-101 was always rejected -- so the box
// could only ever produce a blocked proposal or an unnamed position.
//
// PROMOUX-004 also built everything needed to offer the real choice: the
// filter (internal/domains/promotion/positionpicker) and the accessible
// control (internal/humanwork/productui.PositionPicker). What was missing
// was the list itself, which only this cell can produce. This file produces
// it.

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/positionpicker"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PositionDirectorySource lists the positions a cell can see at a
// coordinate, with their occupancy. It is the seam
// internal/data/positionfacts.Reader satisfies; a cell composed without an
// execution database has none, and then there is no vacancy list to offer.
type PositionDirectorySource interface {
	Directory(ctx context.Context, tenant values.TenantId, asOf position.AsOf) ([]positionfacts.DirectoryRow, error)
}

// positionVacancies projects the authorized, still-open positions this
// principal may target.
//
// A nil directory or reader yields no options and no error. That is the
// honest answer for a cell that cannot read positions: it is not "there are
// no vacancies", and the form must not turn it back into a free-text box --
// it renders the same explicit empty state, which says the cell cannot
// offer a position rather than inventing one.
//
// The authorization hook is nil, deliberately and not by omission. It must
// be the identical hook the resubmission check uses for the same viewer
// (positionpicker.Request.Authorize documents why: a picker and a check
// that disagree either leak or wrongly refuse), and the production propose
// path passes no per-position hook today -- CorpusInputs builds its
// promotion.PreflightRequest with PositionReader and no Authorize. The
// boundary that is actually enforced is tenancy: the directory is read
// inside a tenant-scoped transaction, and every candidate's reference
// decodes only within that tenant. When a per-position hook is introduced,
// it belongs in both places in the same change.
func (e *journeyEngine) positionVacancies(ctx context.Context, principal *trust.Principal) ([]workspace.PositionVacancyOption, error) {
	if e == nil || e.positions == nil || e.positionReader == nil || principal == nil {
		return nil, nil
	}
	tenant := values.TenantId(principal.Tenant())
	if err := tenant.Validate(); err != nil {
		return nil, fmt.Errorf("app: journey: vacancy tenant: %w", err)
	}
	now := e.now()
	effective, err := values.NewLocalDate(now.Year(), now.Month(), now.Day())
	if err != nil {
		return nil, fmt.Errorf("app: journey: vacancy effective date: %w", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(now.UTC()))
	if err != nil {
		return nil, fmt.Errorf("app: journey: vacancy coordinate: %w", err)
	}
	asOf := position.AsOf{EffectiveOn: effective, KnownAt: knownAt}

	rows, err := e.positions.Directory(ctx, tenant, asOf)
	if err != nil {
		return nil, fmt.Errorf("app: journey: position directory: %w", err)
	}

	reader, err := preloadPositionRevisions(ctx, e.positionReader, tenant, asOf, rows)
	if err != nil {
		return nil, err
	}

	out := make([]workspace.PositionVacancyOption, 0, len(rows))
	for _, row := range rows {
		// One call per row, each carrying only that row's own occupancy.
		// position.Occupant names no position, so a pooled occupancy set
		// would make every position look as full as the busiest one --
		// which would hide real vacancies rather than disclose false ones,
		// but is wrong either way.
		candidates, err := positionpicker.ResolveCandidates(ctx, reader, positionpicker.Request{
			Tenant:    tenant,
			AsOf:      asOf,
			Directory: []positionpicker.DirectoryEntry{{Position: row.Position, Title: row.Title, Organization: row.Organization, Location: row.Location}},
			Occupants: row.Occupants,
		})
		if err != nil {
			return nil, fmt.Errorf("app: journey: resolve position candidates: %w", err)
		}
		for _, candidate := range candidates {
			out = append(out, workspace.PositionVacancyOption{
				Reference:        candidate.Reference.String(),
				Title:            candidate.Title,
				Organization:     candidate.Organization,
				Manager:          candidate.Manager,
				Location:         candidate.Location,
				JobCode:          row.JobCode,
				OrgUnit:          row.OrgUnit,
				VacancyEndISO:    vacancyEndISO(candidate),
				ReservationState: candidate.ReservationState,
			})
		}
	}
	return out, nil
}

// PositionRevisionPreloader is the additional capability a
// [position.PositionFacts] may offer: resolving a whole set of positions at
// one coordinate in one transaction, and answering from that set afterwards.
// internal/data/positionfacts.Reader implements it.
//
// It is declared here rather than widened onto position.PositionFacts because
// every other holder of that port asks about exactly one position -- a
// proposal's preflight, a resubmission check -- and only a list surface has a
// set to resolve.
type PositionRevisionPreloader interface {
	RevisionsAt(ctx context.Context, tenant values.TenantId, asOf position.AsOf, positions []values.EntityRef) (position.PositionFacts, error)
}

// preloadPositionRevisions resolves every directory row's revision up front
// when the cell's reader can do it, and otherwise hands back the reader
// unchanged.
//
// This is the whole of UXLIVE-011's cost: positionpicker.ResolveCandidates
// resolves each candidate's revision twice (position.CheckCompatibility and
// position.CalculateCapacity each ask), and a Reader answers each of those in
// its own transaction, so the demo tenant's hundred and sixty positions cost
// three hundred and twenty transactions on every read of the propose form.
// Preloading makes it one, and changes no verdict: the preloaded reader
// replays exactly what the per-position reader would have answered, refusals
// included, and defers anything outside the set to the reader itself.
//
// A reader that does not offer the capability -- every test double in this
// package -- keeps the per-position path, so the seam adds no behaviour of
// its own.
func preloadPositionRevisions(
	ctx context.Context, reader position.PositionFacts, tenant values.TenantId,
	asOf position.AsOf, rows []positionfacts.DirectoryRow,
) (position.PositionFacts, error) {
	preloader, capable := reader.(PositionRevisionPreloader)
	if !capable || len(rows) == 0 {
		return reader, nil
	}
	refs := make([]values.EntityRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, row.Position)
	}
	preloaded, err := preloader.RevisionsAt(ctx, tenant, asOf, refs)
	if err != nil {
		return nil, fmt.Errorf("app: journey: preload position revisions: %w", err)
	}
	return preloaded, nil
}

// vacancyEndISO renders the disclosed vacancy end as a plain date, or empty
// when the position domain disclosed none. It is deliberately the same
// yyyy-mm-dd shape a date input accepts, so no caller has to reparse a
// localized string to compare it against a proposed effective date.
func vacancyEndISO(candidate positionpicker.Candidate) string {
	if !candidate.HasVacancyEnd || !candidate.VacancyEnd.IsSet() {
		return ""
	}
	return time.Date(int(candidate.VacancyEnd.Year()), candidate.VacancyEnd.Month(), int(candidate.VacancyEnd.Day()),
		0, 0, 0, 0, time.UTC).Format("2006-01-02")
}
