package schedulingstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PublishedScheduleSnapshot is the immutable state required to rehydrate a
// worker self-service decision after a process restart. Payload is a versioned
// JSON object encoded by the domain/application boundary; the store binds its
// exact bytes to the listed approved, publication, problem and rule digests.
type PublishedScheduleSnapshot struct {
	Revision          string
	ApprovedDigest    string
	PublicationDigest string
	ProblemDigest     string
	RuleRevision      string
	RuleDigest        string
	PayloadDigest     string
	Payload           json.RawMessage
}

// PublishedSchedulePayload is the sealed outer envelope for the persisted
// domain snapshot. Data carries the encoded approved schedule, publication,
// optimization problem and rule snapshot supplied by the trusted caller.
type PublishedSchedulePayload struct {
	Schema            string          `json:"schema"`
	Revision          string          `json:"revision"`
	ApprovedDigest    string          `json:"approved_digest"`
	PublicationDigest string          `json:"publication_digest"`
	ProblemDigest     string          `json:"problem_digest"`
	RuleRevision      string          `json:"rule_revision"`
	RuleDigest        string          `json:"rule_digest"`
	Data              json.RawMessage `json:"data"`
}

const publishedSchedulePayloadSchema = "schedopt-published-schedule/v1"

// BindPublishedSchedulePayload builds a versioned envelope whose metadata is
// checked against the snapshot row before persistence or after reload.
func BindPublishedSchedulePayload(snapshot *PublishedScheduleSnapshot, data json.RawMessage) error {
	if snapshot == nil || !validContentDigest(snapshot.ApprovedDigest) || !validContentDigest(snapshot.PublicationDigest) ||
		!validContentDigest(snapshot.ProblemDigest) || !validContentDigest(snapshot.RuleDigest) ||
		strings.TrimSpace(snapshot.Revision) == "" || strings.TrimSpace(snapshot.RuleRevision) == "" || !validJSONObject(data) {
		return coded(CodeInvalid, ErrInvalid, "published schedule envelope metadata or data is invalid")
	}
	snapshot.ApprovedDigest = domainDigest(snapshot.ApprovedDigest)
	snapshot.PublicationDigest = domainDigest(snapshot.PublicationDigest)
	snapshot.ProblemDigest = domainDigest(snapshot.ProblemDigest)
	snapshot.RuleDigest = domainDigest(snapshot.RuleDigest)
	envelope := PublishedSchedulePayload{
		Schema: publishedSchedulePayloadSchema, Revision: snapshot.Revision, ApprovedDigest: snapshot.ApprovedDigest,
		PublicationDigest: snapshot.PublicationDigest, ProblemDigest: snapshot.ProblemDigest,
		RuleRevision: snapshot.RuleRevision, RuleDigest: snapshot.RuleDigest, Data: append(json.RawMessage(nil), data...),
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return coded(CodeInvalid, ErrInvalid, "encode published schedule envelope")
	}
	snapshot.Payload = payload
	snapshot.PayloadDigest = payloadDigest(payload)
	return nil
}

// LoadPublishedSchedule returns the current immutable snapshot and its CAS
// fence. It verifies the stored payload digest before returning any bytes.
func (s *Store) LoadPublishedSchedule(ctx context.Context, tenant values.TenantId, scheduleID string) (PublishedScheduleSnapshot, uint64, error) {
	var snapshot PublishedScheduleSnapshot
	var fence int64
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var payload string
		var approvedDigest, publicationDigest, problemDigest, ruleDigest, payloadDigest string
		var pointerDigest, stateDigest string
		err := tx.QueryRow(ctx, `SELECT r.revision,r.approved_digest,r.publication_digest,r.problem_digest,r.rule_revision,r.rule_digest,r.payload_digest,r.payload,c.publication_digest,s.publication_digest,s.fence
			FROM schedopt_published_schedule_current c JOIN schedopt_published_schedule_revision r
			ON r.tenant_id=c.tenant_id AND r.schedule_id=c.schedule_id AND r.revision=c.current_revision
			JOIN schedopt_self_service_state s ON s.tenant_id=c.tenant_id AND s.schedule_id=c.schedule_id
			WHERE c.tenant_id=$1 AND c.schedule_id=$2`, tenantID, scheduleID).
			Scan(&snapshot.Revision, &approvedDigest, &publicationDigest, &problemDigest, &snapshot.RuleRevision, &ruleDigest, &payloadDigest, &payload, &pointerDigest, &stateDigest, &fence)
		if errors.Is(err, dbport.ErrNoRows) {
			return coded(CodeNotFound, ErrNotFound, "published schedule")
		}
		if err != nil {
			return coded(CodeDatabase, err, "load published schedule")
		}
		snapshot.ApprovedDigest = domainDigest(approvedDigest)
		snapshot.PublicationDigest = domainDigest(publicationDigest)
		snapshot.ProblemDigest = domainDigest(problemDigest)
		snapshot.RuleDigest = domainDigest(ruleDigest)
		snapshot.PayloadDigest = domainDigest(payloadDigest)
		snapshot.Payload = append(json.RawMessage(nil), payload...)
		if fence < 1 || domainDigest(pointerDigest) != snapshot.PublicationDigest || domainDigest(stateDigest) != snapshot.PublicationDigest || !validSnapshot(snapshot) {
			return coded(CodeDatabase, ErrInvalid, "stored published schedule snapshot is invalid")
		}
		return nil
	})
	return snapshot, uint64(fence), err
}

// PublishScheduleSnapshot appends a sealed revision and advances the current
// pointer and self-service fence atomically. An empty expected digest and zero
// fence are accepted only when the schedule has no current snapshot.
func (s *Store) PublishScheduleSnapshot(ctx context.Context, tenant values.TenantId, scheduleID string, expectedDigest string, expectedFence uint64, snapshot PublishedScheduleSnapshot) (uint64, error) {
	if strings.TrimSpace(scheduleID) == "" || !validSnapshot(snapshot) || expectedFence > uint64(^uint64(0)>>1) {
		return 0, coded(CodeInvalid, ErrInvalid, "published schedule snapshot or expected fence is invalid")
	}
	var nextFence uint64
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var currentDigest string
		var currentFence int64
		err := tx.QueryRow(ctx, `SELECT publication_digest,fence FROM schedopt_self_service_state WHERE tenant_id=$1 AND schedule_id=$2 FOR UPDATE`, tenantID, scheduleID).Scan(&currentDigest, &currentFence)
		if errors.Is(err, dbport.ErrNoRows) {
			if expectedDigest != "" || expectedFence != 0 {
				return coded(CodeVersionConflict, ErrVersionConflict, "published schedule has no current revision")
			}
			inserted, err := tx.Exec(ctx, `INSERT INTO schedopt_self_service_state (tenant_id,schedule_id,publication_digest,fence) VALUES ($1,$2,$3,1) ON CONFLICT (tenant_id,schedule_id) DO NOTHING`, tenantID, scheduleID, storageDigest(snapshot.PublicationDigest))
			if err != nil {
				return coded(CodeDatabase, err, "initialize published schedule fence")
			}
			if inserted != 1 {
				return coded(CodeVersionConflict, ErrVersionConflict, "another publisher initialized the schedule")
			}
			currentFence = 1
			nextFence = 1
		} else if err != nil {
			return coded(CodeDatabase, err, "lock published schedule fence")
		} else {
			var currentRevision, pointerDigest string
			if err := tx.QueryRow(ctx, `SELECT current_revision,publication_digest FROM schedopt_published_schedule_current WHERE tenant_id=$1 AND schedule_id=$2 FOR UPDATE`, tenantID, scheduleID).Scan(&currentRevision, &pointerDigest); err != nil {
				return coded(CodeDatabase, err, "lock published schedule pointer")
			}
			if currentRevision == "" || domainDigest(currentDigest) != expectedDigest || domainDigest(pointerDigest) != domainDigest(currentDigest) || uint64(currentFence) != expectedFence {
				return coded(CodeVersionConflict, ErrVersionConflict, "published schedule pointer or fence changed")
			}
			if currentFence < 1 || currentFence >= int64(^uint64(0)>>1) {
				return coded(CodeInvalid, ErrInvalid, "published schedule fence overflow")
			}
			nextFence = uint64(currentFence + 1)
			if _, err := tx.Exec(ctx, `UPDATE schedopt_self_service_state SET publication_digest=$3,fence=$4,updated_at=now() WHERE tenant_id=$1 AND schedule_id=$2 AND publication_digest=$5 AND fence=$6`, tenantID, scheduleID, storageDigest(snapshot.PublicationDigest), int64(nextFence), currentDigest, currentFence); err != nil {
				return coded(CodeDatabase, err, "advance published schedule fence")
			}
		}
		if err := insertPublishedRevision(ctx, tx, tenantID, scheduleID, snapshot); err != nil {
			return err
		}
		if nextFence == 1 && expectedFence == 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO schedopt_published_schedule_current (tenant_id,schedule_id,current_revision,publication_digest,fence) VALUES ($1,$2,$3,$4,$5)`, tenantID, scheduleID, snapshot.Revision, storageDigest(snapshot.PublicationDigest), int64(nextFence)); err != nil {
				return mapWriteError(CodeDuplicate, "schedopt_published_schedule_current", err)
			}
		} else if _, err := tx.Exec(ctx, `UPDATE schedopt_published_schedule_current SET current_revision=$3,publication_digest=$4,fence=$5,updated_at=now() WHERE tenant_id=$1 AND schedule_id=$2`, tenantID, scheduleID, snapshot.Revision, storageDigest(snapshot.PublicationDigest), int64(nextFence)); err != nil {
			return coded(CodeDatabase, err, "advance published schedule pointer")
		}
		return nil
	})
	return nextFence, err
}

// ClaimShiftOfferWithSnapshot consumes an offer and stores its successor
// approved schedule/publication in the same transaction as the fence advance.
func (s *Store) ClaimShiftOfferWithSnapshot(ctx context.Context, tenant values.TenantId, scheduleID, offerID string, expectedOfferFence uint64, snapshot PublishedScheduleSnapshot) (uint64, error) {
	if expectedOfferFence == 0 || expectedOfferFence > uint64(^uint64(0)>>1) || strings.TrimSpace(scheduleID) == "" || strings.TrimSpace(offerID) == "" || !validSnapshot(snapshot) {
		return 0, coded(CodeInvalid, ErrInvalid, "claim or successor schedule snapshot is invalid")
	}
	var nextFence uint64
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var currentDigest string
		var currentFence int64
		if err := tx.QueryRow(ctx, `SELECT publication_digest,fence FROM schedopt_self_service_state WHERE tenant_id=$1 AND schedule_id=$2 FOR UPDATE`, tenantID, scheduleID).Scan(&currentDigest, &currentFence); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return coded(CodeVersionConflict, ErrVersionConflict, "schedule state is missing")
			}
			return coded(CodeDatabase, err, "lock self-service schedule state")
		}
		var currentRevision, pointerDigest string
		if err := tx.QueryRow(ctx, `SELECT current_revision,publication_digest FROM schedopt_published_schedule_current WHERE tenant_id=$1 AND schedule_id=$2 FOR UPDATE`, tenantID, scheduleID).Scan(&currentRevision, &pointerDigest); err != nil {
			return coded(CodeVersionConflict, ErrVersionConflict, "published schedule pointer is missing")
		}
		var state, offerDigest string
		var offerFence int64
		var offerPayload []byte
		if err := tx.QueryRow(ctx, `SELECT offer_state,offer_fence,publication_digest,payload FROM schedopt_self_service_offer WHERE tenant_id=$1 AND schedule_id=$2 AND offer_id=$3 FOR UPDATE`, tenantID, scheduleID, offerID).Scan(&state, &offerFence, &offerDigest, &offerPayload); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return coded(CodeVersionConflict, ErrVersionConflict, "shift offer is missing")
			}
			return coded(CodeDatabase, err, "lock shift offer")
		}
		if state != "OPEN" || uint64(offerFence) != expectedOfferFence || offerDigest != currentDigest ||
			domainDigest(pointerDigest) != domainDigest(currentDigest) || snapshot.PublicationDigest == domainDigest(currentDigest) || snapshot.Revision == currentRevision {
			return coded(CodeVersionConflict, ErrVersionConflict, "offer or successor schedule revision is stale")
		}
		var offer schedopt.ShiftOffer
		if err := json.Unmarshal(offerPayload, &offer); err != nil {
			return coded(CodeDatabase, err, "decode shift offer")
		}
		if offer.ID != offerID || offer.FencingToken != expectedOfferFence || offer.PublicationDigest != domainDigest(offerDigest) ||
			(offer.Kind != schedopt.ShiftOfferOpenClaim && offer.Kind != schedopt.ShiftOfferTrade) {
			return coded(CodeDatabase, ErrInvalid, "stored shift offer does not match its fence row")
		}
		nextOfferState := "CLAIMED"
		if offer.Kind == schedopt.ShiftOfferTrade {
			nextOfferState = "TRADED"
		}
		if currentFence < 1 || currentFence >= int64(^uint64(0)>>1) {
			return coded(CodeInvalid, ErrInvalid, "self-service schedule fence overflow")
		}
		nextFence = uint64(currentFence + 1)
		if err := insertPublishedRevision(ctx, tx, tenantID, scheduleID, snapshot); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE schedopt_self_service_offer SET offer_state=$4,updated_at=now() WHERE tenant_id=$1 AND schedule_id=$2 AND offer_id=$3 AND offer_state='OPEN' AND offer_fence=$5`, tenantID, scheduleID, offerID, nextOfferState, int64(expectedOfferFence)); err != nil {
			return coded(CodeDatabase, err, "consume shift offer")
		}
		if _, err := tx.Exec(ctx, `UPDATE schedopt_self_service_state SET publication_digest=$3,fence=$4,updated_at=now() WHERE tenant_id=$1 AND schedule_id=$2 AND publication_digest=$5 AND fence=$6`, tenantID, scheduleID, storageDigest(snapshot.PublicationDigest), int64(nextFence), currentDigest, currentFence); err != nil {
			return coded(CodeDatabase, err, "advance self-service schedule state")
		}
		if _, err := tx.Exec(ctx, `UPDATE schedopt_published_schedule_current SET current_revision=$3,publication_digest=$4,fence=$5,updated_at=now() WHERE tenant_id=$1 AND schedule_id=$2 AND current_revision=$6 AND publication_digest=$7`, tenantID, scheduleID, snapshot.Revision, storageDigest(snapshot.PublicationDigest), int64(nextFence), currentRevision, currentDigest); err != nil {
			return coded(CodeDatabase, err, "advance published schedule pointer")
		}
		return nil
	})
	return nextFence, err
}

func insertPublishedRevision(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, scheduleID string, snapshot PublishedScheduleSnapshot) error {
	if _, err := tx.Exec(ctx, `INSERT INTO schedopt_published_schedule_revision (tenant_id,schedule_id,revision,approved_digest,publication_digest,problem_digest,rule_revision,rule_digest,payload_digest,payload) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenantID, scheduleID, snapshot.Revision, storageDigest(snapshot.ApprovedDigest), storageDigest(snapshot.PublicationDigest), storageDigest(snapshot.ProblemDigest), snapshot.RuleRevision, storageDigest(snapshot.RuleDigest), storageDigest(snapshot.PayloadDigest), string(snapshot.Payload)); err != nil {
		return mapWriteError(CodeDuplicate, "schedopt_published_schedule_revision", err)
	}
	return nil
}

func validSnapshot(snapshot PublishedScheduleSnapshot) bool {
	if strings.TrimSpace(snapshot.Revision) == "" || strings.TrimSpace(snapshot.RuleRevision) == "" ||
		!validContentDigest(snapshot.ApprovedDigest) || !validContentDigest(snapshot.PublicationDigest) ||
		!validContentDigest(snapshot.ProblemDigest) || !validContentDigest(snapshot.RuleDigest) || !validContentDigest(snapshot.PayloadDigest) {
		return false
	}
	var envelope PublishedSchedulePayload
	if json.Unmarshal(snapshot.Payload, &envelope) != nil || envelope.Schema != publishedSchedulePayloadSchema ||
		envelope.Revision != snapshot.Revision || domainDigest(envelope.ApprovedDigest) != domainDigest(snapshot.ApprovedDigest) ||
		domainDigest(envelope.PublicationDigest) != domainDigest(snapshot.PublicationDigest) || domainDigest(envelope.ProblemDigest) != domainDigest(snapshot.ProblemDigest) ||
		envelope.RuleRevision != snapshot.RuleRevision || domainDigest(envelope.RuleDigest) != domainDigest(snapshot.RuleDigest) || !validJSONObject(envelope.Data) {
		return false
	}
	return domainDigest(snapshot.PayloadDigest) == payloadDigest(snapshot.Payload)
}

func validJSONObject(payload []byte) bool {
	var object map[string]json.RawMessage
	return len(payload) != 0 && json.Unmarshal(payload, &object) == nil && object != nil
}

func validContentDigest(digest string) bool {
	if strings.HasPrefix(digest, "sha256:") {
		digest = strings.TrimPrefix(digest, "sha256:")
	}
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

// PayloadDigest returns the exact SHA-256 digest expected by snapshot writes.
func PayloadDigest(payload []byte) string { return payloadDigest(payload) }

func payloadDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var _ schedopt.ShiftSelfServiceRepository = (*Store)(nil)
