// Package performancestore persists the seven performance metadata tables from
// migrations/00108_performance.sql. The adapter stores only the table-shaped
// durable records exposed by internal/domains/performance; rich kernel values
// remain immutable and are validated before they cross this boundary.
package performancestore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the minimal database capability required by Store.
type DB interface{ dbport.Beginner }

// Store is the PostgreSQL implementation of performance.Store.
type Store struct{ db DB }

var _ performance.Store = (*Store)(nil)

// New returns a store over db. The caller normally supplies a connection that
// can SET ROLE hcmnext_app, matching the other data-plane stores.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) withTenant(ctx context.Context, tenant performance.TenantID, fn func(dbport.Tx, uuid.UUID) error) error {
	if ctx == nil {
		return refuse("PERFORMANCE_INVALID_CONTEXT", "context", "context is required", performance.ErrStoreRefused)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return refuse("PERFORMANCE_INVALID_STORE", "database", "database is required", performance.ErrStoreRefused)
	}
	if tenant == nil || strings.TrimSpace(tenant.String()) == "" {
		return refuse("PERFORMANCE_INVALID_TENANT", "tenant_id", "tenant id is required", performance.ErrStoreRefused)
	}
	tenantID, err := uuid.Parse(tenant.String())
	if err != nil || tenantID == uuid.Nil {
		return refuse("PERFORMANCE_INVALID_TENANT", "tenant_id", "tenant id must be a non-nil UUID", performance.ErrStoreRefused)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("performancestore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("performancestore: commit transaction: %w", err)
	}
	return nil
}

func refuse(code, field, reason string, cause error) error {
	return &performance.RefusalError{Code: code, Field: field, Reason: reason, Cause: cause}
}

func notFound(field, reason string) error {
	return refuse(performance.ErrNotFound.Error(), field, reason, performance.ErrNotFound)
}

func digest(value string) (string, error) {
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != 64 {
		return "", refuse("PERFORMANCE_INVALID_DIGEST", "canonical_digest", "digest must be a SHA-256 hex digest", performance.ErrStoreRefused)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", refuse("PERFORMANCE_INVALID_DIGEST", "canonical_digest", "digest must be a SHA-256 hex digest", performance.ErrStoreRefused)
	}
	return value, nil
}

func domainDigest(value string) string {
	if value == "" || strings.Contains(value, ":") {
		return value
	}
	return "sha256:" + value
}

func validCycle(c performance.CycleRevision) error {
	if strings.TrimSpace(c.CycleID) == "" || c.Revision == 0 || !c.State.Valid() || c.SupersedesRevision >= c.Revision {
		return refuse("PERFORMANCE_INVALID_CYCLE", "cycle", "cycle revision is incomplete", performance.ErrStoreRefused)
	}
	_, err := digest(c.CanonicalDigest)
	return err
}

func validCase(c performance.RatingCaseRecord) error {
	if strings.TrimSpace(c.CaseID) == "" || strings.TrimSpace(c.ParticipantID) == "" {
		return refuse("PERFORMANCE_INVALID_CASE", "case", "rating case is incomplete", performance.ErrStoreRefused)
	}
	_, err := digest(c.CanonicalDigest)
	return err
}

func validEvent(e performance.RatingEventRecord, sequence uint64) error {
	if sequence == 0 || (e.Kind != performance.RatingEventContestRaised && e.Kind != performance.RatingEventCorrectionDecided && e.Kind != performance.RatingEventFinalized) || strings.TrimSpace(e.ActorID) == "" || !e.At.IsSet() {
		return refuse("PERFORMANCE_INVALID_EVENT", "event", "rating event is incomplete", performance.ErrStoreRefused)
	}
	_, err := digest(e.Digest)
	return err
}

func validSession(s performance.CalibrationSessionRecord) error {
	if strings.TrimSpace(s.SessionID) == "" || strings.TrimSpace(s.CycleID) == "" || s.CycleRevision == 0 || !json.Valid(s.Graph) || !json.Valid(s.Adjustments) {
		return refuse("PERFORMANCE_INVALID_SESSION", "session", "calibration session is incomplete or invalid JSON", performance.ErrStoreRefused)
	}
	_, err := digest(s.CanonicalDigest)
	return err
}

func validReview(r performance.ReviewRecord) error {
	if strings.TrimSpace(r.ReviewID) == "" || strings.TrimSpace(r.ReviewerID) == "" || strings.TrimSpace(r.ParticipantID) == "" || strings.TrimSpace(r.CycleID) == "" || r.CycleRevision == 0 || r.ReviewRevision == 0 || !r.SubmittedAt.IsSet() {
		return refuse("PERFORMANCE_INVALID_REVIEW", "review", "review is incomplete", performance.ErrStoreRefused)
	}
	return nil
}

func validFinal(r performance.FinalRatingRecord) (string, error) {
	if _, err := uuid.Parse(r.RatingRef); err != nil {
		return "", refuse("PERFORMANCE_INVALID_FINAL_RATING", "rating_ref", "rating reference must be a UUID", performance.ErrStoreRefused)
	}
	if strings.TrimSpace(r.ParticipantID) == "" || strings.TrimSpace(r.CycleID) == "" || !r.FinalizedAt.IsSet() {
		return "", refuse("PERFORMANCE_INVALID_FINAL_RATING", "final_rating", "final rating is incomplete", performance.ErrStoreRefused)
	}
	if err := r.FinalRating.Validate(); err != nil || r.FinalRating.Scale() > 2 {
		return "", refuse("PERFORMANCE_INVALID_FINAL_RATING", "final_rating", "final rating must be a valid numeric value at scale two or less", performance.ErrStoreRefused)
	}
	return digest(r.CanonicalDigest)
}

func validOutcome(r performance.OutcomeLinkRecord) (string, error) {
	if _, err := uuid.Parse(r.RatingRef); err != nil {
		return "", refuse("PERFORMANCE_INVALID_OUTCOME_LINK", "rating_ref", "rating reference must be a UUID", performance.ErrStoreRefused)
	}
	if r.RatingRevision == 0 || (r.Action != performance.OutcomeLinkActionLink && r.Action != performance.OutcomeLinkActionUnlink) || !r.EffectiveAt.IsSet() {
		return "", refuse("PERFORMANCE_INVALID_OUTCOME_LINK", "outcome_link", "outcome link is incomplete", performance.ErrStoreRefused)
	}
	ratingDigest, err := digest(r.RatingDigest)
	if err != nil {
		return "", err
	}
	if _, err := digest(r.Digest); err != nil {
		return "", err
	}
	return ratingDigest, nil
}

func (s *Store) SaveCycle(ctx context.Context, tenant performance.TenantID, c performance.CycleRevision) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		if err := validCycle(c); err != nil {
			return err
		}
		storedDigest, err := digest(c.CanonicalDigest)
		if err != nil {
			return err
		}
		var latest int64
		err = tx.QueryRow(ctx, `SELECT revision FROM performance_cycle WHERE tenant_id=$1 AND cycle_id=$2 ORDER BY revision DESC LIMIT 1`, tenantID, c.CycleID).Scan(&latest)
		if err == nil {
			if uint64(latest) >= c.Revision {
				if uint64(latest) == c.Revision {
					return refuse(performance.ErrDuplicateRevision.Error(), "revision", "cycle revision is already stored", performance.ErrDuplicateRevision)
				}
				return refuse(performance.ErrStaleRevision.Error(), "revision", "cycle revision is behind the current chain", performance.ErrStaleRevision)
			}
			if c.Revision != uint64(latest)+1 || c.SupersedesRevision != uint64(latest) {
				return refuse(performance.ErrStaleRevision.Error(), "supersedes_revision", "cycle revision does not extend the current chain", performance.ErrStaleRevision)
			}
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("performancestore: inspect cycle chain: %w", err)
		} else if c.Revision != 1 || c.SupersedesRevision != 0 {
			return refuse(performance.ErrStaleRevision.Error(), "revision", "first cycle revision must be one", performance.ErrStaleRevision)
		}
		_, err = tx.Exec(ctx, `INSERT INTO performance_cycle (tenant_id,row_id,cycle_id,revision,state,supersedes_revision,canonical_digest) VALUES ($1,$2,$3,$4,$5,$6,$7)`, tenantID, uuid.New(), c.CycleID, int64(c.Revision), string(c.State), nullableRevision(c.SupersedesRevision), storedDigest)
		return mapWriteError("save cycle", c.CycleID, err)
	})
}

func (s *Store) LoadCycle(ctx context.Context, tenant performance.TenantID, cycleID string, revision uint64) (performance.CycleRevision, error) {
	var out performance.CycleRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var rev int64
		var supersedes *int64
		var state, storedDigest string
		err := tx.QueryRow(ctx, `SELECT cycle_id,revision,state,supersedes_revision,canonical_digest FROM performance_cycle WHERE tenant_id=$1 AND cycle_id=$2 AND revision=$3`, tenantID, cycleID, int64(revision)).Scan(&out.CycleID, &rev, &state, &supersedes, &storedDigest)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("cycle_id", "cycle revision was not found")
		}
		if err != nil {
			return fmt.Errorf("performancestore: load cycle: %w", err)
		}
		out.Revision = uint64(rev)
		out.State = performance.PerformanceCycleState(state)
		if supersedes != nil {
			out.SupersedesRevision = uint64(*supersedes)
		}
		out.CanonicalDigest = domainDigest(storedDigest)
		return nil
	})
	return out, err
}

func (s *Store) SaveParticipantReviewerGraph(ctx context.Context, tenant performance.TenantID, graph performance.FrozenParticipantReviewerGraph) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		if err := graph.Validate(); err != nil {
			return refuse("PERFORMANCE_INVALID_GRAPH", "graph", "frozen participant/reviewer graph is invalid", err)
		}
		if graph.GraphRevision == 0 || graph.SupersedesGraphRevision+1 != graph.GraphRevision {
			return refuse(performance.ErrStaleRevision.Error(), "graph_revision", "graph revision must extend its frozen predecessor", performance.ErrStaleRevision)
		}
		var latestCycleRevision int64
		var latestCycleState string
		err := tx.QueryRow(ctx, `SELECT revision,state FROM performance_cycle WHERE tenant_id=$1 AND cycle_id=$2 ORDER BY revision DESC LIMIT 1`, tenantID, graph.CycleID).Scan(&latestCycleRevision, &latestCycleState)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("cycle_id", "frozen graph cycle was not found")
		}
		if err != nil {
			return fmt.Errorf("performancestore: inspect graph cycle: %w", err)
		}
		if uint64(latestCycleRevision) != graph.CycleRevision || performance.PerformanceCycleState(latestCycleState) != performance.PerformanceCycleOpen {
			return refuse(performance.ErrStaleRevision.Error(), "cycle_revision", "graph must bind the current open cycle revision", performance.ErrStaleRevision)
		}
		var latestGraphRevision int64
		err = tx.QueryRow(ctx, `SELECT graph_revision FROM performance_participant_reviewer_graph WHERE tenant_id=$1 AND cycle_id=$2 AND cycle_revision=$3 ORDER BY graph_revision DESC LIMIT 1`, tenantID, graph.CycleID, int64(graph.CycleRevision)).Scan(&latestGraphRevision)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("performancestore: inspect frozen graph revisions: %w", err)
		}
		if uint64(latestGraphRevision) >= graph.GraphRevision {
			if uint64(latestGraphRevision) == graph.GraphRevision {
				return refuse(performance.ErrDuplicateRevision.Error(), "graph_revision", "participant/reviewer graph revision is already stored", performance.ErrDuplicateRevision)
			}
			return refuse(performance.ErrStaleRevision.Error(), "graph_revision", "participant/reviewer graph revision is behind the current chain", performance.ErrStaleRevision)
		}
		if graph.GraphRevision != uint64(latestGraphRevision)+1 || graph.SupersedesGraphRevision != uint64(latestGraphRevision) {
			return refuse(performance.ErrStaleRevision.Error(), "supersedes_graph_revision", "graph revision does not extend the current chain", performance.ErrStaleRevision)
		}
		encoded, err := json.Marshal(graph)
		if err != nil {
			return fmt.Errorf("performancestore: encode frozen graph: %w", err)
		}
		storedDigest, err := digest(graph.Digest)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO performance_participant_reviewer_graph (tenant_id,row_id,cycle_id,cycle_revision,graph_revision,supersedes_graph_revision,graph_digest,graph) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, tenantID, uuid.New(), graph.CycleID, int64(graph.CycleRevision), int64(graph.GraphRevision), int64(graph.SupersedesGraphRevision), storedDigest, string(encoded))
		return mapWriteError("save participant/reviewer graph", graph.CycleID, err)
	})
}

// ListOpenParticipantReviewerGraphsForMember reads only the latest graph of
// each current OPEN cycle to which memberID belongs. The database predicate
// is tenant-scoped and narrows by graph membership before any snapshot leaves
// the store.
func (s *Store) ListOpenParticipantReviewerGraphsForMember(ctx context.Context, tenant performance.TenantID, memberID string) ([]performance.FrozenParticipantReviewerGraph, error) {
	if strings.TrimSpace(memberID) == "" {
		return nil, refuse("PERFORMANCE_INVALID_MEMBER", "member_id", "member reference is required", performance.ErrStoreRefused)
	}
	participantFilter, err := json.Marshal(struct {
		Participants []performance.ParticipantRef `json:"participants"`
	}{Participants: []performance.ParticipantRef{{ID: memberID}}})
	if err != nil {
		return nil, fmt.Errorf("performancestore: encode graph member filter: %w", err)
	}
	reviewerFilter, err := json.Marshal(map[string]any{
		"reviewers": []map[string]string{{"reviewer_id": memberID}},
	})
	if err != nil {
		return nil, fmt.Errorf("performancestore: encode graph reviewer filter: %w", err)
	}
	result := make([]performance.FrozenParticipantReviewerGraph, 0)
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `
			SELECT g.cycle_id,g.cycle_revision,g.graph_revision,g.supersedes_graph_revision,g.graph_digest,g.graph
			FROM performance_participant_reviewer_graph g
			JOIN performance_cycle c ON c.tenant_id=g.tenant_id AND c.cycle_id=g.cycle_id AND c.revision=g.cycle_revision
			WHERE g.tenant_id=$1
			  AND c.state='OPEN'
			  AND c.revision=(SELECT max(current_cycle.revision) FROM performance_cycle current_cycle WHERE current_cycle.tenant_id=g.tenant_id AND current_cycle.cycle_id=g.cycle_id)
			  AND g.graph_revision=(SELECT max(current_graph.graph_revision) FROM performance_participant_reviewer_graph current_graph WHERE current_graph.tenant_id=g.tenant_id AND current_graph.cycle_id=g.cycle_id AND current_graph.cycle_revision=g.cycle_revision)
			  AND (g.graph @> $2::jsonb OR g.graph @> $3::jsonb)
			ORDER BY g.cycle_id`, tenantID, string(participantFilter), string(reviewerFilter))
		if err != nil {
			return fmt.Errorf("performancestore: list member graph snapshots: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var cycleID, storedDigest string
			var cycleRevision, graphRevision, supersedes int64
			var raw []byte
			if err := rows.Scan(&cycleID, &cycleRevision, &graphRevision, &supersedes, &storedDigest, &raw); err != nil {
				return fmt.Errorf("performancestore: scan member graph snapshot: %w", err)
			}
			var graph performance.FrozenParticipantReviewerGraph
			if err := json.Unmarshal(raw, &graph); err != nil {
				return fmt.Errorf("performancestore: decode member graph snapshot: %w", err)
			}
			if graph.CycleID != cycleID || graph.CycleRevision != uint64(cycleRevision) || graph.GraphRevision != uint64(graphRevision) || graph.SupersedesGraphRevision != uint64(supersedes) || graph.Digest != domainDigest(storedDigest) {
				return refuse("PERFORMANCE_INVALID_GRAPH", "graph", "stored graph metadata does not match its snapshot", performance.ErrStoreRefused)
			}
			if err := graph.Validate(); err != nil {
				return refuse("PERFORMANCE_INVALID_GRAPH", "graph", "stored participant/reviewer graph is invalid", err)
			}
			if !graphMember(graph, memberID) {
				return refuse("PERFORMANCE_INVALID_GRAPH", "graph", "stored graph membership filter returned an unrelated snapshot", performance.ErrStoreRefused)
			}
			result = append(result, graph)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("performancestore: read member graph snapshots: %w", err)
		}
		return nil
	})
	return result, err
}

func graphMember(graph performance.FrozenParticipantReviewerGraph, memberID string) bool {
	for _, participant := range graph.Participants {
		if participant.ID == memberID {
			return true
		}
	}
	for _, reviewer := range graph.Reviewers {
		if reviewer.ReviewerID == memberID {
			return true
		}
	}
	return false
}

func (s *Store) SaveRatingCase(ctx context.Context, tenant performance.TenantID, c performance.RatingCaseRecord) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		if err := validCase(c); err != nil {
			return err
		}
		storedDigest, err := digest(c.CanonicalDigest)
		if err != nil {
			return err
		}
		var oldFinalized bool
		var oldDigest string
		err = tx.QueryRow(ctx, `SELECT finalized,canonical_digest FROM performance_rating_case WHERE tenant_id=$1 AND case_id=$2 FOR UPDATE`, tenantID, c.CaseID).Scan(&oldFinalized, &oldDigest)
		if errors.Is(err, dbport.ErrNoRows) {
			_, err = tx.Exec(ctx, `INSERT INTO performance_rating_case (tenant_id,row_id,case_id,participant_id,finalized,canonical_digest) VALUES ($1,$2,$3,$4,$5,$6)`, tenantID, uuid.New(), c.CaseID, c.ParticipantID, c.Finalized, storedDigest)
			return mapWriteError("save rating case", c.CaseID, err)
		}
		if err != nil {
			return fmt.Errorf("performancestore: inspect rating case: %w", err)
		}
		if oldFinalized && !c.Finalized {
			return refuse(performance.ErrStaleRevision.Error(), "finalized", "a finalized case cannot be reopened", performance.ErrStaleRevision)
		}
		if oldFinalized == c.Finalized && oldDigest == storedDigest {
			return refuse(performance.ErrDuplicateRevision.Error(), "case_id", "rating case is already stored", performance.ErrDuplicateRevision)
		}
		if !c.Finalized || oldFinalized {
			return refuse(performance.ErrStaleRevision.Error(), "finalized", "rating case update is not a finalization CAS", performance.ErrStaleRevision)
		}
		result, err := tx.Exec(ctx, `UPDATE performance_rating_case SET finalized=true,canonical_digest=$4 WHERE tenant_id=$1 AND case_id=$2 AND finalized=false`, tenantID, c.CaseID, c.ParticipantID, storedDigest)
		if err != nil {
			return fmt.Errorf("performancestore: finalize rating case: %w", err)
		}
		if result != 1 {
			return refuse(performance.ErrStaleRevision.Error(), "finalized", "rating case finalization lost its CAS", performance.ErrStaleRevision)
		}
		return nil
	})
}

func (s *Store) LoadRatingCase(ctx context.Context, tenant performance.TenantID, id string) (performance.RatingCaseRecord, error) {
	var out performance.RatingCaseRecord
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		var d string
		err := tx.QueryRow(ctx, `SELECT case_id,participant_id,finalized,canonical_digest FROM performance_rating_case WHERE tenant_id=$1 AND case_id=$2`, tid, id).Scan(&out.CaseID, &out.ParticipantID, &out.Finalized, &d)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("case_id", "rating case was not found")
		}
		if err != nil {
			return err
		}
		out.CanonicalDigest = domainDigest(d)
		return nil
	})
	return out, err
}

func (s *Store) FinalizeRatingCase(ctx context.Context, tenant performance.TenantID, id, canonicalDigest string) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		d, err := digest(canonicalDigest)
		if err != nil {
			return err
		}
		var finalized bool
		if err := tx.QueryRow(ctx, `SELECT finalized FROM performance_rating_case WHERE tenant_id=$1 AND case_id=$2 FOR UPDATE`, tid, id).Scan(&finalized); errors.Is(err, dbport.ErrNoRows) {
			return notFound("case_id", "rating case was not found")
		} else if err != nil {
			return err
		}
		if finalized {
			return refuse(performance.ErrStaleRevision.Error(), "finalized", "rating case was already finalized", performance.ErrStaleRevision)
		}
		n, err := tx.Exec(ctx, `UPDATE performance_rating_case SET finalized=true,canonical_digest=$3 WHERE tenant_id=$1 AND case_id=$2 AND finalized=false`, tid, id, d)
		if err != nil {
			return mapWriteError("finalize rating case", id, err)
		}
		if n != 1 {
			return refuse(performance.ErrStaleRevision.Error(), "finalized", "rating case finalization lost its CAS", performance.ErrStaleRevision)
		}
		return nil
	})
}

func (s *Store) AppendRatingEvent(ctx context.Context, tenant performance.TenantID, caseID string, sequence uint64, e performance.RatingEventRecord) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		if err := validEvent(e, sequence); err != nil {
			return err
		}
		d, err := digest(e.Digest)
		if err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `INSERT INTO performance_rating_event (tenant_id,row_id,case_id,kind,actor_id,at,contest_digest,correction_digest,digest,event_sequence) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tid, uuid.New(), caseID, string(e.Kind), e.ActorID, e.At.Time(), nullableDigest(e.ContestDigest), nullableDigest(e.CorrectionDigest), d, int64(sequence))
		if err != nil {
			return mapWriteError("append rating event", caseID, err)
		}
		if n != 1 {
			return fmt.Errorf("performancestore: append rating event affected %d rows", n)
		}
		return nil
	})
}

func (s *Store) ListRatingEvents(ctx context.Context, tenant performance.TenantID, caseID string) ([]performance.RatingEventRecord, error) {
	var out []performance.RatingEventRecord
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT kind,actor_id,at,contest_digest,correction_digest,digest FROM performance_rating_event WHERE tenant_id=$1 AND case_id=$2 ORDER BY event_sequence`, tid, caseID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var kind string
			var actor *string
			var atTime timeValue
			var contest, correction, d *string
			if err := rows.Scan(&kind, &actor, &atTime, &contest, &correction, &d); err != nil {
				return err
			}
			at := values.NewInstant(atTime)
			e := performance.RatingEventRecord{Kind: performance.RatingEventKind(kind), ActorID: deref(actor), At: at, ContestDigest: domainDigest(deref(contest)), CorrectionDigest: domainDigest(deref(correction)), Digest: domainDigest(deref(d))}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) SaveFinalRating(ctx context.Context, tenant performance.TenantID, caseID string, r performance.FinalRatingRecord) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		d, err := validFinal(r)
		if err != nil {
			return err
		}
		var finalized bool
		if err := tx.QueryRow(ctx, `SELECT finalized FROM performance_rating_case WHERE tenant_id=$1 AND case_id=$2 FOR UPDATE`, tid, caseID).Scan(&finalized); errors.Is(err, dbport.ErrNoRows) {
			return notFound("case_id", "rating case was not found")
		} else if err != nil {
			return err
		}
		if !finalized {
			return refuse(performance.ErrFinalRatingNotFinal.Error(), "finalized", "final rating requires a finalized case", performance.ErrFinalRatingNotFinal)
		}
		ratingRef, _ := uuid.Parse(r.RatingRef)
		n, err := tx.Exec(ctx, `INSERT INTO performance_final_rating (tenant_id,row_id,rating_ref,participant_id,cycle_id,final_rating,finalized_at,canonical_digest) VALUES ($1,$2,$3,$4,$5,$6::numeric,$7,$8)`, tid, uuid.New(), ratingRef, r.ParticipantID, r.CycleID, r.FinalRating.String(), r.FinalizedAt.Time(), d)
		if err != nil {
			return mapWriteError("save final rating", r.ParticipantID, err)
		}
		if n != 1 {
			return fmt.Errorf("performancestore: save final rating affected %d rows", n)
		}
		return nil
	})
}

func (s *Store) LoadFinalRating(ctx context.Context, tenant performance.TenantID, participantID, cycleID string) (performance.FinalRatingRecord, error) {
	var out performance.FinalRatingRecord
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		var ref uuid.UUID
		var number string
		var atTime timeValue
		var d string
		var err error
		err = tx.QueryRow(ctx, `SELECT rating_ref,participant_id,cycle_id,final_rating::text,finalized_at,canonical_digest FROM performance_final_rating WHERE tenant_id=$1 AND participant_id=$2 AND cycle_id=$3`, tid, participantID, cycleID).Scan(&ref, &out.ParticipantID, &out.CycleID, &number, &atTime, &d)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("participant_id", "final rating was not found")
		}
		if err != nil {
			return err
		}
		out.RatingRef = ref.String()
		out.FinalRating, err = parseDecimal(number)
		if err != nil {
			return err
		}
		out.FinalizedAt = values.NewInstant(atTime)
		out.CanonicalDigest = domainDigest(d)
		return nil
	})
	return out, err
}

func (s *Store) SaveCalibrationSession(ctx context.Context, tenant performance.TenantID, r performance.CalibrationSessionRecord) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		if err := validSession(r); err != nil {
			return err
		}
		d, err := digest(r.CanonicalDigest)
		if err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `INSERT INTO performance_calibration_session (tenant_id,row_id,session_id,cycle_id,cycle_revision,graph,adjustments,canonical_digest) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8)`, tid, uuid.New(), r.SessionID, r.CycleID, int64(r.CycleRevision), string(r.Graph), string(r.Adjustments), d)
		if err != nil {
			return mapWriteError("save calibration session", r.SessionID, err)
		}
		if n != 1 {
			return fmt.Errorf("performancestore: save calibration session affected %d rows", n)
		}
		return nil
	})
}
func (s *Store) LoadCalibrationSession(ctx context.Context, tenant performance.TenantID, id string) (performance.CalibrationSessionRecord, error) {
	var out performance.CalibrationSessionRecord
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		var rev int64
		var graph, adjustments []byte
		var d string
		err := tx.QueryRow(ctx, `SELECT session_id,cycle_id,cycle_revision,graph::text,adjustments::text,canonical_digest FROM performance_calibration_session WHERE tenant_id=$1 AND session_id=$2`, tid, id).Scan(&out.SessionID, &out.CycleID, &rev, &graph, &adjustments, &d)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("session_id", "calibration session was not found")
		}
		if err != nil {
			return err
		}
		out.CycleRevision = uint64(rev)
		out.Graph = graph
		out.Adjustments = adjustments
		out.CanonicalDigest = domainDigest(d)
		return nil
	})
	return out, err
}

func (s *Store) SaveReview(ctx context.Context, tenant performance.TenantID, r performance.ReviewRecord) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		if err := validReview(r); err != nil {
			return err
		}
		var latest int64
		err := tx.QueryRow(ctx, `SELECT review_revision FROM performance_review WHERE tenant_id=$1 AND review_id=$2 ORDER BY review_revision DESC LIMIT 1`, tid, r.ReviewID).Scan(&latest)
		if errors.Is(err, dbport.ErrNoRows) {
			if r.SupersedesReviewRevision != 0 {
				return refuse(performance.ErrStaleRevision.Error(), "supersedes_review_revision", "first review revision has no predecessor", performance.ErrStaleRevision)
			}
		} else if err != nil {
			return err
		} else if uint64(latest) >= r.ReviewRevision {
			return refuse(performance.ErrDuplicateRevision.Error(), "review_revision", "review revision is already stored", performance.ErrDuplicateRevision)
		} else if r.ReviewRevision != uint64(latest)+1 || r.SupersedesReviewRevision != uint64(latest) {
			return refuse(performance.ErrStaleRevision.Error(), "supersedes_review_revision", "review revision does not extend the current chain", performance.ErrStaleRevision)
		}
		n, err := tx.Exec(ctx, `INSERT INTO performance_review (tenant_id,row_id,review_id,reviewer_id,participant_id,cycle_id,cycle_revision,review_revision,supersedes_review_revision,submitted_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tid, uuid.New(), r.ReviewID, r.ReviewerID, r.ParticipantID, r.CycleID, int64(r.CycleRevision), int64(r.ReviewRevision), nullableRevision(r.SupersedesReviewRevision), r.SubmittedAt.Time())
		if err != nil {
			return mapWriteError("save review", r.ReviewID, err)
		}
		if n != 1 {
			return fmt.Errorf("performancestore: save review affected %d rows", n)
		}
		return nil
	})
}
func (s *Store) LoadReview(ctx context.Context, tenant performance.TenantID, id string, revision uint64) (performance.ReviewRecord, error) {
	var out performance.ReviewRecord
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		var cycleRev, reviewRev int64
		var supersedes *int64
		var atTime timeValue
		err := tx.QueryRow(ctx, `SELECT review_id,reviewer_id,participant_id,cycle_id,cycle_revision,review_revision,supersedes_review_revision,submitted_at FROM performance_review WHERE tenant_id=$1 AND review_id=$2 AND review_revision=$3`, tid, id, int64(revision)).Scan(&out.ReviewID, &out.ReviewerID, &out.ParticipantID, &out.CycleID, &cycleRev, &reviewRev, &supersedes, &atTime)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("review_id", "review revision was not found")
		}
		if err != nil {
			return err
		}
		out.CycleRevision = uint64(cycleRev)
		out.ReviewRevision = uint64(reviewRev)
		if supersedes != nil {
			out.SupersedesReviewRevision = uint64(*supersedes)
		}
		out.SubmittedAt = values.NewInstant(atTime)
		return nil
	})
	return out, err
}

func (s *Store) AppendOutcomeLink(ctx context.Context, tenant performance.TenantID, r performance.OutcomeLinkRecord) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		ratingDigest, err := validOutcome(r)
		if err != nil {
			return err
		}
		d, _ := digest(r.Digest)
		ref, _ := uuid.Parse(r.RatingRef)
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM performance_outcome_link WHERE tenant_id=$1 AND rating_ref=$2 AND digest=$3)`, tid, ref, d).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return refuse(performance.ErrDuplicateRevision.Error(), "digest", "outcome link is already stored", performance.ErrDuplicateRevision)
		}
		n, err := tx.Exec(ctx, `INSERT INTO performance_outcome_link (tenant_id,row_id,rating_ref,rating_digest,rating_revision,action,effective_at,digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, tid, uuid.New(), ref, ratingDigest, int64(r.RatingRevision), string(r.Action), r.EffectiveAt.Time(), d)
		if err != nil {
			return mapWriteError("append outcome link", r.RatingRef, err)
		}
		if n != 1 {
			return fmt.Errorf("performancestore: append outcome link affected %d rows", n)
		}
		return nil
	})
}
func (s *Store) ListOutcomeLinks(ctx context.Context, tenant performance.TenantID, ratingRef string) ([]performance.OutcomeLinkRecord, error) {
	var out []performance.OutcomeLinkRecord
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		ref, err := uuid.Parse(ratingRef)
		if err != nil {
			return refuse("PERFORMANCE_INVALID_OUTCOME_LINK", "rating_ref", "rating reference must be a UUID", performance.ErrStoreRefused)
		}
		rows, err := tx.Query(ctx, `SELECT rating_ref,rating_digest,rating_revision,action,effective_at,digest FROM performance_outcome_link WHERE tenant_id=$1 AND rating_ref=$2 ORDER BY effective_at,row_id`, tid, ref)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r uuid.UUID
			var ratingDigest, digestText *string
			var rev *int64
			var action string
			var atTime timeValue
			if err := rows.Scan(&r, &ratingDigest, &rev, &action, &atTime, &digestText); err != nil {
				return err
			}
			out = append(out, performance.OutcomeLinkRecord{RatingRef: r.String(), RatingDigest: domainDigest(deref(ratingDigest)), RatingRevision: uint64(derefInt64(rev)), Action: performance.OutcomeLinkAction(action), EffectiveAt: values.NewInstant(atTime), Digest: domainDigest(deref(digestText))})
		}
		return rows.Err()
	})
	return out, err
}

// timeValue is a tiny Scan target for timestamptz. pgx/database adapters scan
// into time.Time, and this alias keeps the SQL mapper's declarations compact.
type timeValue = time.Time

func parseDecimal(raw string) (values.Decimal, error) {
	scale := int32(0)
	if _, fraction, ok := strings.Cut(raw, "."); ok {
		scale = int32(len(fraction))
	}
	return values.NewDecimal(raw, scale, values.RoundingHalfEven)
}
func nullableRevision(n uint64) any {
	if n == 0 {
		return nil
	}
	return int64(n)
}
func nullableDigest(s string) any {
	if s == "" {
		return nil
	}
	return strings.TrimPrefix(s, "sha256:")
}
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func derefInt64(n *int64) int64 {
	if n == nil {
		return 0
	}
	return *n
}

func mapWriteError(operation, id string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if strings.Contains(pgErr.ConstraintName, "event_sequence") {
				return refuse(performance.ErrDuplicateEvent.Error(), "event_sequence", "rating event sequence is already stored", performance.ErrDuplicateEvent)
			}
			return refuse(performance.ErrDuplicateRevision.Error(), "revision", "performance identity is already stored", performance.ErrDuplicateRevision)
		case "23503":
			return notFound("reference", "referenced performance row was not found")
		}
	}
	return fmt.Errorf("performancestore: %s %s: %w", operation, id, err)
}
