package schedulingstore

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// LoadPersonaPublishedScheduleForWorker resolves a worker from the current
// authoritative publication projections. An absent or ambiguous publication
// never selects an arbitrary schedule. The application must validate the
// decoded publication and ownership again after this current-pointer read.
func (s *Store) LoadPersonaPublishedScheduleForWorker(ctx context.Context, tenant values.TenantId, worker values.EntityRef) (string, PublishedScheduleSnapshot, uint64, error) {
	if worker.Validate() != nil || worker.Tenant != tenant || worker.Kind != "candidate" {
		return "", PublishedScheduleSnapshot{}, 0, coded(CodeInvalid, ErrInvalid, "current schedule worker is invalid")
	}
	var scheduleID string
	var snapshot PublishedScheduleSnapshot
	var fence uint64
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		// Membership, immutable revision, current pointer and fence share one
		// statement snapshot, so a concurrent publication cannot join a worker
		// from one revision to a different revision's payload.
		rows, err := tx.Query(ctx, `SELECT c.schedule_id,r.revision,r.approved_digest,r.publication_digest,r.problem_digest,r.rule_revision,r.rule_digest,r.payload_digest,r.payload,c.publication_digest,s.publication_digest,s.fence
			FROM schedopt_published_schedule_current c JOIN schedopt_published_schedule_revision r
			ON r.tenant_id=c.tenant_id AND r.schedule_id=c.schedule_id AND r.revision=c.current_revision
			JOIN schedopt_self_service_state s ON s.tenant_id=c.tenant_id AND s.schedule_id=c.schedule_id
			WHERE c.tenant_id=$1 AND r.payload::jsonb->'data'->>'schema'='persona-work-schedule/v1'
			AND EXISTS (SELECT 1 FROM jsonb_array_elements(r.payload::jsonb->'data'->'workers') w WHERE w->>'worker'=$2)
			ORDER BY c.schedule_id LIMIT 2`, tenantID, worker.String())
		if err != nil {
			return coded(CodeDatabase, err, "resolve worker published schedule")
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var approvedDigest, publicationDigest, problemDigest, ruleDigest, digest, payload, pointerDigest, stateDigest string
			var storedFence int64
			if err := rows.Scan(&scheduleID, &snapshot.Revision, &approvedDigest, &publicationDigest, &problemDigest, &snapshot.RuleRevision, &ruleDigest, &digest, &payload, &pointerDigest, &stateDigest, &storedFence); err != nil {
				return coded(CodeDatabase, err, "read worker published schedule")
			}
			snapshot.ApprovedDigest = domainDigest(approvedDigest)
			snapshot.PublicationDigest = domainDigest(publicationDigest)
			snapshot.ProblemDigest = domainDigest(problemDigest)
			snapshot.RuleDigest = domainDigest(ruleDigest)
			snapshot.PayloadDigest = domainDigest(digest)
			snapshot.Payload = append(json.RawMessage(nil), payload...)
			if storedFence < 1 || domainDigest(pointerDigest) != snapshot.PublicationDigest || domainDigest(stateDigest) != snapshot.PublicationDigest || !validSnapshot(snapshot) {
				return coded(CodeDatabase, ErrInvalid, "stored worker schedule snapshot is invalid")
			}
			fence = uint64(storedFence)
			count++
		}
		if err := rows.Err(); err != nil {
			return coded(CodeDatabase, err, "read worker published schedule")
		}
		if count == 0 {
			return coded(CodeNotFound, ErrNotFound, "worker has no published schedule")
		}
		if count != 1 || strings.TrimSpace(scheduleID) == "" {
			return coded(CodeInvalid, ErrInvalid, "worker has ambiguous published schedules")
		}
		return nil
	})
	if err != nil {
		return "", PublishedScheduleSnapshot{}, 0, err
	}
	return scheduleID, snapshot, fence, nil
}
