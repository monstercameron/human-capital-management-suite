package performancestore

// REV-096-01: the production CalibratedRatingLookup.
//
// internal/intent/app declares CalibratedRatingLookup and routes a top-band
// subject's promotion to the high-performer variant, but nothing in the
// repository implemented that interface outside a test stub, so a served
// promotion always took the "no lookup" branch. The obstacle is shape, not
// storage: performance_final_rating records the participant, the cycle and
// the final decimal, while performance.FinalCalibratedRating.Validate also
// requires the proposed rating it came from, the adjustment history that led
// to it, and the proposed-rating and session digests those hash to.
//
// The calibration session is where those facts already live. This file fixes
// the document the adjustments column carries -- the finalized calibrated
// ratings of that session's cohort -- and reads a subject's rating back out
// of it, cross-checked against the final-rating row. The rating is then
// validated by the domain before it is returned, so a document somebody
// edited by hand cannot route anybody anywhere: a tampered value fails the
// digest chain and the lookup reports a miss.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CalibratedRatingsSchema versions the document [EncodeCalibratedRatings]
// writes into performance_calibration_session.adjustments.
const CalibratedRatingsSchema = "hcmnext.data.performancestore.CalibratedRatings/v1"

// CalibrationGraphSchema versions the document [EncodeCalibrationGraph]
// writes into performance_calibration_session.graph. The column holds the
// reference to the frozen participant/reviewer graph, never a copy of the
// population it names.
const CalibrationGraphSchema = "hcmnext.data.performancestore.CalibrationGraph/v1"

// calibratedRatingEntry is one cohort member's finalized calibrated rating.
// The participant id is repeated outside the rating so the document can be
// searched with a jsonb containment predicate rather than read whole.
type calibratedRatingEntry struct {
	ParticipantID string                            `json:"participant_id"`
	Rating        performance.FinalCalibratedRating `json:"rating"`
}

type calibratedRatingsDocument struct {
	Schema  string                  `json:"schema"`
	Ratings []calibratedRatingEntry `json:"ratings"`
}

type calibrationGraphDocument struct {
	Schema        string `json:"schema"`
	CycleID       string `json:"cycle_id"`
	GraphRevision uint64 `json:"graph_revision"`
	GraphDigest   string `json:"graph_digest"`
	OrgUnit       string `json:"org_unit"`
}

// EncodeCalibratedRatings renders a calibration session's finalized ratings
// as the adjustments document. Every rating is validated first: an invalid
// rating is refused here rather than stored and refused on every read.
// Entries are sorted by participant so the same cohort always produces the
// same bytes.
func EncodeCalibratedRatings(ratings []performance.FinalCalibratedRating) ([]byte, error) {
	document := calibratedRatingsDocument{Schema: CalibratedRatingsSchema, Ratings: make([]calibratedRatingEntry, 0, len(ratings))}
	seen := make(map[string]struct{}, len(ratings))
	for _, rating := range ratings {
		if err := rating.Validate(); err != nil {
			return nil, fmt.Errorf("performancestore: encode calibrated rating %s: %w", rating.ParticipantID, err)
		}
		if _, exists := seen[rating.ParticipantID]; exists {
			return nil, fmt.Errorf("performancestore: encode calibrated ratings: duplicate participant %s", rating.ParticipantID)
		}
		seen[rating.ParticipantID] = struct{}{}
		document.Ratings = append(document.Ratings, calibratedRatingEntry{ParticipantID: rating.ParticipantID, Rating: rating})
	}
	sort.Slice(document.Ratings, func(i, j int) bool {
		return document.Ratings[i].ParticipantID < document.Ratings[j].ParticipantID
	})
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("performancestore: encode calibrated ratings: %w", err)
	}
	return raw, nil
}

// DecodeCalibratedRating reads one participant's finalized rating out of an
// adjustments document. A rating that does not validate is reported as a
// miss, never returned.
func DecodeCalibratedRating(raw []byte, participantID string) (performance.FinalCalibratedRating, error) {
	var document calibratedRatingsDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return performance.FinalCalibratedRating{}, fmt.Errorf("performancestore: decode calibrated ratings: %w", err)
	}
	if document.Schema != CalibratedRatingsSchema {
		return performance.FinalCalibratedRating{}, refuse("PERFORMANCE_INVALID_SESSION", "adjustments",
			"calibration adjustments document is not a calibrated-rating document", performance.ErrStoreRefused)
	}
	for _, entry := range document.Ratings {
		if entry.ParticipantID != participantID {
			continue
		}
		if entry.Rating.ParticipantID != participantID {
			return performance.FinalCalibratedRating{}, refuse("PERFORMANCE_INVALID_FINAL_RATING", "participant_id",
				"stored calibrated rating names a different participant", performance.ErrStoreRefused)
		}
		if err := entry.Rating.Validate(); err != nil {
			return performance.FinalCalibratedRating{}, fmt.Errorf("performancestore: stored calibrated rating for %s: %w", participantID, err)
		}
		return entry.Rating, nil
	}
	return performance.FinalCalibratedRating{}, notFound("participant_id", "the calibration session records no rating for the subject")
}

// EncodeCalibrationGraph renders the graph reference a stored calibration
// session carries.
func EncodeCalibrationGraph(cycleID string, graphRevision uint64, graphDigest, orgUnit string) ([]byte, error) {
	if strings.TrimSpace(cycleID) == "" || graphRevision == 0 || strings.TrimSpace(graphDigest) == "" {
		return nil, fmt.Errorf("performancestore: encode calibration graph: cycle, revision and digest are required")
	}
	raw, err := json.Marshal(calibrationGraphDocument{
		Schema: CalibrationGraphSchema, CycleID: cycleID, GraphRevision: graphRevision,
		GraphDigest: graphDigest, OrgUnit: orgUnit,
	})
	if err != nil {
		return nil, fmt.Errorf("performancestore: encode calibration graph: %w", err)
	}
	return raw, nil
}

// CalibratedRatings resolves one subject's most recently finalized cycle and
// its calibrated rating. It implements the CalibratedRatingLookup the intent
// service's high-performer routing reads through; the interface is satisfied
// structurally, so this package does not depend on the intent layer.
type CalibratedRatings struct {
	db     DB
	tenant func(values.TenantId) uuid.UUID
}

// NewCalibratedRatings returns a reader over db. tenant maps the cell's
// tenant key onto the tenant row identity, exactly as the other composed
// stores' mappers do; a nil db or mapper yields a reader that reports every
// lookup as unavailable rather than silently routing nobody.
func NewCalibratedRatings(db DB, tenant func(values.TenantId) uuid.UUID) *CalibratedRatings {
	return &CalibratedRatings{db: db, tenant: tenant}
}

// LookupCalibratedRating returns the subject's calibrated rating from the
// most recently finalized cycle that has one. A subject with no finalized
// rating is a typed miss: the caller resolves the ordinary plan, never the
// variant.
func (r *CalibratedRatings) LookupCalibratedRating(ctx context.Context, tenant values.TenantId, subjectRef string) (performance.FinalCalibratedRating, error) {
	if r == nil || r.db == nil || r.tenant == nil {
		return performance.FinalCalibratedRating{}, refuse("PERFORMANCE_INVALID_STORE", "database", "database is required", performance.ErrStoreRefused)
	}
	subjectRef = strings.TrimSpace(subjectRef)
	if subjectRef == "" {
		return performance.FinalCalibratedRating{}, refuse("PERFORMANCE_INVALID_SUBJECT", "subject_ref", "subject reference is required", performance.ErrStoreRefused)
	}
	tenantID := r.tenant(tenant)
	if tenantID == uuid.Nil {
		return performance.FinalCalibratedRating{}, refuse("PERFORMANCE_INVALID_TENANT", "tenant_id", "tenant id must be a non-nil UUID", performance.ErrStoreRefused)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return performance.FinalCalibratedRating{}, fmt.Errorf("performancestore: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return performance.FinalCalibratedRating{}, err
	}
	rating, err := LookupCalibratedRatingTx(ctx, tx, tenantID, subjectRef)
	if err != nil {
		return performance.FinalCalibratedRating{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return performance.FinalCalibratedRating{}, fmt.Errorf("performancestore: commit transaction: %w", err)
	}
	return rating, nil
}

// LookupCalibratedRatingTx is [CalibratedRatings.LookupCalibratedRating] in
// the caller's own already-scoped transaction.
func LookupCalibratedRatingTx(ctx context.Context, ex dbport.Querier, tenantID uuid.UUID, subjectRef string) (performance.FinalCalibratedRating, error) {
	predicate, err := json.Marshal([]map[string]string{{"participant_id": subjectRef}})
	if err != nil {
		return performance.FinalCalibratedRating{}, fmt.Errorf("performancestore: build calibrated rating predicate: %w", err)
	}
	var (
		cycleID     string
		finalRating string
		storedHash  string
		adjustments []byte
	)
	// The cycle must actually be finalized: a rating recorded against a cycle
	// still open or already reopened is not the subject's standing.
	err = ex.QueryRow(ctx, `
		SELECT r.cycle_id, r.final_rating::text, r.canonical_digest, s.adjustments::text
		FROM performance_final_rating r
		JOIN performance_calibration_session s
		  ON s.tenant_id = r.tenant_id AND s.cycle_id = r.cycle_id
		WHERE r.tenant_id = $1 AND r.participant_id = $2
		  AND s.adjustments -> 'ratings' @> $3::jsonb
		  AND EXISTS (
			SELECT 1 FROM performance_cycle c
			WHERE c.tenant_id = r.tenant_id AND c.cycle_id = r.cycle_id
			  AND c.state IN ('CLOSED', 'LOCKED'))
		ORDER BY r.finalized_at DESC, r.cycle_id DESC
		LIMIT 1`, tenantID, subjectRef, string(predicate)).Scan(&cycleID, &finalRating, &storedHash, &adjustments)
	if errors.Is(err, dbport.ErrNoRows) {
		return performance.FinalCalibratedRating{}, notFound("participant_id", "the subject has no finalized calibrated rating")
	}
	if err != nil {
		return performance.FinalCalibratedRating{}, fmt.Errorf("performancestore: read calibrated rating for %s: %w", subjectRef, err)
	}
	rating, err := DecodeCalibratedRating(adjustments, subjectRef)
	if err != nil {
		return performance.FinalCalibratedRating{}, err
	}
	if storedHash != storageDigestOf(rating.CanonicalDigest) {
		return performance.FinalCalibratedRating{}, refuse("PERFORMANCE_INVALID_FINAL_RATING", "canonical_digest",
			"the calibration document disagrees with the recorded final rating digest", performance.ErrStoreRefused)
	}
	recorded, err := parseDecimal(finalRating)
	if err != nil {
		return performance.FinalCalibratedRating{}, err
	}
	if !recorded.Equal(rating.FinalRating) {
		return performance.FinalCalibratedRating{}, refuse("PERFORMANCE_INVALID_FINAL_RATING", "final_rating",
			"the calibration document disagrees with the recorded final rating", performance.ErrStoreRefused)
	}
	if rating.ProposedRating.CycleID != cycleID {
		return performance.FinalCalibratedRating{}, refuse("PERFORMANCE_INVALID_FINAL_RATING", "cycle_id",
			"the calibrated rating belongs to a different cycle", performance.ErrStoreRefused)
	}
	return rating, nil
}

// storageDigestOf is the column form of a domain digest: bare hex, no
// algorithm prefix.
func storageDigestOf(digest string) string { return strings.TrimPrefix(digest, "sha256:") }
