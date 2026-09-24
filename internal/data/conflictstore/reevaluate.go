package conflictstore

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

const preflightPolicyVersion = "conflict-preflight/v1"

// ReevaluateAtCommit compares the exact approval-pinned intent with the
// current plan writes and every persisted active footprint that overlaps
// those writes. It only reads; callers run it inside the transaction that
// will subsequently validate the durable commit fence.
func (s *Store) ReevaluateAtCommit(ctx context.Context, tx dbport.Tx, req conflict.CommitRequest) (conflict.ExecutionPreflight, error) {
	if req.TenantID == "" || req.IntentID == "" || req.SnapshotDigest == "" || len(req.Writes) == 0 {
		return conflict.ExecutionPreflight{}, conflict.ErrInvalidIntent
	}
	var proposal, snapshot string
	if err := tx.QueryRow(ctx, `SELECT proposal_id,snapshot_digest FROM conflict_write_intent WHERE tenant_id=$1::uuid AND intent_id=$2`, req.TenantID, req.IntentID).Scan(&proposal, &snapshot); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return conflict.ExecutionPreflight{}, fmt.Errorf("%w: %s", ErrNotFound, req.IntentID)
		}
		return conflict.ExecutionPreflight{}, fmt.Errorf("load pinned conflict intent: %w", err)
	}
	if snapshot != req.SnapshotDigest {
		return conflict.ExecutionPreflight{}, &conflict.Error{Code: conflict.CodeConflictStaleBaseline, IntentID: req.IntentID, Err: conflict.ErrStaleBaseline}
	}
	pinnedFootprints, err := s.readFootprints(ctx, tx, req.TenantID, req.IntentID)
	if err != nil {
		return conflict.ExecutionPreflight{}, err
	}
	pinned := conflict.WriteIntent{TenantID: req.TenantID, ID: req.IntentID, ProposalID: proposal, SnapshotDigest: snapshot, Footprints: pinnedFootprints}
	currentFootprints := make([]conflict.WriteFootprint, 0, len(req.Writes))
	for _, baseline := range req.Writes {
		footprint, err := baseline.Footprint()
		if err != nil {
			return conflict.ExecutionPreflight{}, err
		}
		currentFootprints = append(currentFootprints, footprint)
	}
	current := []conflict.WriteIntent{{TenantID: req.TenantID, ID: req.IntentID, ProposalID: proposal, SnapshotDigest: snapshot, Footprints: currentFootprints}}
	peers, err := s.readOverlappingCurrentIntents(ctx, tx, req.TenantID, req.IntentID, currentFootprints)
	if err != nil {
		return conflict.ExecutionPreflight{}, err
	}
	current = append(current, peers...)
	return conflict.ReevaluatePreflight(conflict.ReevaluateRequest{
		PolicyVersion: preflightPolicyVersion,
		Pinned:        []conflict.WriteIntent{pinned},
		Current:       current,
	})
}

func (s *Store) readFootprints(ctx context.Context, tx dbport.Tx, tenantID, intentID string) ([]conflict.WriteFootprint, error) {
	rows, err := tx.Query(ctx, `SELECT stream_key,expected_sequence,resource_canonical,field_path,operation,authority_domain,authority_policy_ref,effective_interval_canonical FROM conflict_write_footprint WHERE tenant_id=$1::uuid AND intent_id=$2 ORDER BY ordinal`, tenantID, intentID)
	if err != nil {
		return nil, fmt.Errorf("read pinned conflict footprints: %w", err)
	}
	defer rows.Close()
	var footprints []conflict.WriteFootprint
	for rows.Next() {
		footprint, err := scanFootprint(rows)
		if err != nil {
			return nil, err
		}
		footprints = append(footprints, footprint)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pinned conflict footprints: %w", err)
	}
	if len(footprints) == 0 {
		return nil, conflict.ErrInvalidIntent
	}
	return footprints, nil
}

func (s *Store) readOverlappingCurrentIntents(ctx context.Context, tx dbport.Tx, tenantID, excludedIntentID string, footprints []conflict.WriteFootprint) ([]conflict.WriteIntent, error) {
	byID := make(map[string]*conflict.WriteIntent)
	for _, footprint := range footprints {
		dim, err := footprintDimensions(footprint)
		if err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT i.intent_id,i.proposal_id,i.snapshot_digest,f.stream_key,f.expected_sequence,f.resource_canonical,f.field_path,f.operation,f.authority_domain,f.authority_policy_ref,f.effective_interval_canonical
			FROM conflict_write_intent i JOIN conflict_write_footprint f USING (tenant_id,intent_id)
			WHERE i.tenant_id=$1::uuid AND i.intent_id<>$2 AND i.status IN ('SUBMITTED','COMMITTED')
			AND f.resource_canonical=$3 AND f.interval_kind=$4
			AND f.interval_start < COALESCE($6,'9999-12-31T23:59:59.999999999Z')
			AND COALESCE(f.interval_end,'9999-12-31T23:59:59.999999999Z') > $5
			AND (f.field_path=$7 OR f.field_path LIKE $7 || '.%' OR $7 LIKE f.field_path || '.%')
			ORDER BY i.intent_id,f.ordinal`, tenantID, excludedIntentID, dim.resource, dim.kind, dim.start, dim.end, dim.field)
		if err != nil {
			return nil, fmt.Errorf("read current overlapping conflict footprints: %w", err)
		}
		for rows.Next() {
			var id, proposal, snapshot string
			var currentFootprint conflict.WriteFootprint
			if err := scanIntentFootprint(rows, &id, &proposal, &snapshot, &currentFootprint); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan current overlapping conflict intent: %w", err)
			}
			intent := byID[id]
			if intent == nil {
				intent = &conflict.WriteIntent{TenantID: tenantID, ID: id, ProposalID: proposal, SnapshotDigest: snapshot}
				byID[id] = intent
			}
			duplicate := false
			for _, existing := range intent.Footprints {
				if existing.Digest() == currentFootprint.Digest() {
					duplicate = true
					break
				}
			}
			if !duplicate {
				intent.Footprints = append(intent.Footprints, currentFootprint)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read current overlapping conflict footprints: %w", err)
		}
		rows.Close()
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	intents := make([]conflict.WriteIntent, 0, len(ids))
	for _, id := range ids {
		intents = append(intents, *byID[id])
	}
	return intents, nil
}

func scanFootprint(row dbport.Row) (conflict.WriteFootprint, error) {
	var stream, resourceText, field, operation, domain, policy string
	var sequence int64
	var intervalBytes []byte
	if err := row.Scan(&stream, &sequence, &resourceText, &field, &operation, &domain, &policy, &intervalBytes); err != nil {
		return conflict.WriteFootprint{}, fmt.Errorf("scan conflict footprint: %w", err)
	}
	if sequence < 0 {
		return conflict.WriteFootprint{}, conflict.ErrInvalidFootprint
	}
	baseline := conflict.WriteBaseline{
		ResourceCanonical: resourceText, FieldPath: conflict.FieldPath(field), StreamKey: stream,
		ExpectedSequence: uint64(sequence), AuthorityDomain: domain,
		SourceAuthorityDecision: policy, Operation: conflict.Operation(operation),
	}
	interval, err := decodeEffectiveInterval(intervalBytes)
	if err != nil {
		return conflict.WriteFootprint{}, fmt.Errorf("decode stored effective interval: %w", err)
	}
	baseline.EffectiveInterval = interval
	return baseline.Footprint()
}

func scanIntentFootprint(row dbport.Row, id, proposal, snapshot *string, footprint *conflict.WriteFootprint) error {
	var stream, resourceText, field, operation, domain, policy string
	var sequence int64
	var intervalBytes []byte
	if err := row.Scan(id, proposal, snapshot, &stream, &sequence, &resourceText, &field, &operation, &domain, &policy, &intervalBytes); err != nil {
		return err
	}
	if sequence < 0 {
		return conflict.ErrInvalidFootprint
	}
	baseline := conflict.WriteBaseline{
		ResourceCanonical: resourceText, FieldPath: conflict.FieldPath(field), StreamKey: stream,
		ExpectedSequence: uint64(sequence), AuthorityDomain: domain,
		SourceAuthorityDecision: policy, Operation: conflict.Operation(operation),
	}
	var err error
	baseline.EffectiveInterval, err = decodeEffectiveInterval(intervalBytes)
	if err != nil {
		return err
	}
	*footprint, err = baseline.Footprint()
	return err
}

// decodeEffectiveInterval reads the stable canonical interval bytes written
// by the values kernel and validates the result by requiring an exact
// canonical round trip.
func decodeEffectiveInterval(encoded []byte) (values.EffectiveInterval, error) {
	if len(encoded) < 3 || encoded[0] != 0x05 {
		return values.EffectiveInterval{}, values.ErrIntervalUnset
	}
	kind, endFlag := encoded[1], encoded[2]
	if endFlag > 1 {
		return values.EffectiveInterval{}, values.ErrIntervalUnset
	}
	position := 3
	var err error
	var startInstant, endInstant values.Instant
	var startDate, endDate values.LocalDate
	if kind == 2 {
		var next int
		var err error
		startInstant, next, err = decodeInstant(encoded, position)
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		position = next
		if endFlag == 1 {
			endInstant, next, err = decodeInstant(encoded, position)
			if err != nil {
				return values.EffectiveInterval{}, err
			}
			position = next
		}
	} else if kind == 1 {
		var next int
		var err error
		startDate, next, err = decodeLocalDate(encoded, position)
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		position = next
		if endFlag == 1 {
			endDate, position, err = decodeLocalDate(encoded, position)
			if err != nil {
				return values.EffectiveInterval{}, err
			}
		}
	} else {
		return values.EffectiveInterval{}, values.ErrIntervalUnset
	}
	calendarRef, next, err := decodeLengthPrefixed(encoded, position)
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	position = next
	calendarVersion, next, err := decodeLengthPrefixed(encoded, position)
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	position = next
	zoneID, next, err := decodeLengthPrefixed(encoded, position)
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	position = next
	zoneVersion, next, err := decodeLengthPrefixed(encoded, position)
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	position = next
	if position != len(encoded)-1 {
		return values.EffectiveInterval{}, values.ErrIntervalUnset
	}
	disambiguation := values.Disambiguation(encoded[position])
	var interval values.EffectiveInterval
	if kind == 2 {
		if calendarRef != "" || calendarVersion != "" || zoneID != "" || zoneVersion != "" || disambiguation != values.DisambiguationUnspecified {
			return values.EffectiveInterval{}, values.ErrIntervalUnset
		}
		if endFlag == 1 {
			interval, err = values.NewInstantInterval(startInstant, endInstant)
		} else {
			interval, err = values.NewOpenInstantInterval(startInstant)
		}
	} else {
		calendar := values.CalendarRef{Ref: calendarRef, Version: calendarVersion}
		if endFlag == 1 {
			interval, err = values.NewLocalDateInterval(startDate, endDate, calendar)
		} else {
			interval, err = values.NewOpenLocalDateInterval(startDate, calendar)
		}
		if err == nil && (zoneID != "" || zoneVersion != "" || disambiguation != values.DisambiguationUnspecified) {
			interval, err = interval.WithZone(values.ZoneRef{ID: zoneID, TzdbVersion: zoneVersion}, disambiguation)
		}
	}
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	position++
	if position != len(encoded) || string(interval.Canonical()) != string(encoded) {
		return values.EffectiveInterval{}, values.ErrIntervalUnset
	}
	return interval, nil
}

func decodeInstant(encoded []byte, position int) (values.Instant, int, error) {
	if position+13 > len(encoded) || encoded[position] != 0x01 {
		return values.Instant{}, position, values.ErrInstantUnset
	}
	seconds := int64(binary.BigEndian.Uint64(encoded[position+1 : position+9]))
	nanos := int32(binary.BigEndian.Uint32(encoded[position+9 : position+13]))
	instant, err := values.NewInstantFromUnix(seconds, nanos)
	return instant, position + 13, err
}

func decodeLocalDate(encoded []byte, position int) (values.LocalDate, int, error) {
	if position+7 > len(encoded) || encoded[position] != 0x02 {
		return values.LocalDate{}, position, values.ErrLocalDateUnset
	}
	year := int(binary.BigEndian.Uint32(encoded[position+1 : position+5]))
	date, err := values.NewLocalDate(year, time.Month(encoded[position+5]), int(encoded[position+6]))
	return date, position + 7, err
}

func decodeLengthPrefixed(encoded []byte, position int) (string, int, error) {
	if position+4 > len(encoded) {
		return "", position, values.ErrIntervalUnset
	}
	length := int(binary.BigEndian.Uint32(encoded[position : position+4]))
	position += 4
	if length < 0 || position+length > len(encoded) {
		return "", position, values.ErrIntervalUnset
	}
	return string(encoded[position : position+length]), position + length, nil
}
