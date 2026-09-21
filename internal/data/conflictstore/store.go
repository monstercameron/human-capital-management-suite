// Package conflictstore persists write intents and closes their commit fence
// inside a transaction owned by the caller.
package conflictstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

var ErrNotFound = errors.New("conflictstore: intent not found")

type Store struct{}

func New() *Store { return &Store{} }

type dimensions struct {
	resource, field, kind, start string
	end                          *string
}

func footprintDimensions(f conflict.WriteFootprint) (dimensions, error) {
	if err := f.Validate(); err != nil {
		return dimensions{}, err
	}
	d := dimensions{resource: f.Resource.String(), field: string(f.Field), kind: f.Interval.Kind().String()}
	if start, ok := f.Interval.StartDate(); ok {
		d.start = start.String()
		if end, exists := f.Interval.EndDate(); exists {
			v := end.String()
			d.end = &v
		}
		return d, nil
	}
	if start, ok := f.Interval.StartInstant(); ok {
		d.start = start.String()
		if end, exists := f.Interval.EndInstant(); exists {
			v := end.String()
			d.end = &v
		}
		return d, nil
	}
	return dimensions{}, conflict.ErrInvalidFootprint
}

func effectiveDimensions(interval values.EffectiveInterval) (dimensions, error) {
	if err := interval.Validate(); err != nil {
		return dimensions{}, err
	}
	d := dimensions{kind: interval.Kind().String()}
	if start, ok := interval.StartDate(); ok {
		d.start = start.String()
		if end, exists := interval.EndDate(); exists {
			v := end.String()
			d.end = &v
		}
		return d, nil
	}
	if start, ok := interval.StartInstant(); ok {
		d.start = start.String()
		if end, exists := interval.EndInstant(); exists {
			v := end.String()
			d.end = &v
		}
		return d, nil
	}
	return dimensions{}, conflict.ErrInvalidFootprint
}

// Register writes the immutable footprint rows. The caller owns tx; no
// connection or transaction is opened here.
func (s *Store) Register(ctx context.Context, tx dbport.Tx, in conflict.WriteIntent) error {
	if err := in.Validate(); err != nil {
		return err
	}
	inserted, err := tx.Exec(ctx, `INSERT INTO conflict_write_intent (tenant_id,intent_id,proposal_id,snapshot_digest,status) VALUES ($1::uuid,$2,$3,$4,'SUBMITTED') ON CONFLICT (tenant_id,intent_id) DO NOTHING`, in.TenantID, in.ID, in.ProposalID, in.SnapshotDigest)
	if err != nil {
		return fmt.Errorf("register intent: %w", err)
	}
	if inserted == 0 {
		var proposal, snapshot string
		if err := tx.QueryRow(ctx, `SELECT proposal_id,snapshot_digest FROM conflict_write_intent WHERE tenant_id=$1::uuid AND intent_id=$2`, in.TenantID, in.ID).Scan(&proposal, &snapshot); err != nil {
			return fmt.Errorf("read registered intent: %w", err)
		}
		if proposal != in.ProposalID || snapshot != in.SnapshotDigest {
			return conflict.ErrIntentConflict
		}
	}
	for n, f := range in.Footprints {
		seq, ok := f.ExpectedRevision.Sequence()
		if !ok {
			return conflict.ErrInvalidFootprint
		}
		d, err := footprintDimensions(f)
		if err != nil {
			return err
		}
		count, err := tx.Exec(ctx, `INSERT INTO conflict_write_footprint (tenant_id,intent_id,ordinal,stream_key,expected_sequence,scope_digest,resource_canonical,field_path,operation,authority_domain,authority_policy_ref,effective_interval_canonical,interval_kind,interval_start,interval_end) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT DO NOTHING`, in.TenantID, in.ID, n, f.ExpectedRevision.Stream(), seq, f.ScopeDigest(), d.resource, d.field, f.Operation, f.Authority.Domain, f.Authority.PolicyRef, f.Interval.Canonical(), d.kind, d.start, d.end)
		if err != nil {
			return fmt.Errorf("register footprint: %w", err)
		}
		if count == 0 {
			var stream, scope, resource, field, operation, authorityDomain, authorityPolicy, kind, start string
			var end *string
			var intervalCanonical []byte
			var storedSequence int64
			if err := tx.QueryRow(ctx, `SELECT stream_key,expected_sequence,scope_digest,resource_canonical,field_path,operation,authority_domain,authority_policy_ref,effective_interval_canonical,interval_kind,interval_start,interval_end FROM conflict_write_footprint WHERE tenant_id=$1::uuid AND intent_id=$2 AND ordinal=$3`, in.TenantID, in.ID, n).Scan(&stream, &storedSequence, &scope, &resource, &field, &operation, &authorityDomain, &authorityPolicy, &intervalCanonical, &kind, &start, &end); err != nil {
				return fmt.Errorf("read registered footprint: %w", err)
			}
			if stream != f.ExpectedRevision.Stream() || storedSequence != int64(seq) || scope != f.ScopeDigest() || resource != d.resource || field != d.field || operation != string(f.Operation) || authorityDomain != f.Authority.Domain || authorityPolicy != f.Authority.PolicyRef || !bytes.Equal(intervalCanonical, f.Interval.Canonical()) || kind != d.kind || start != d.start || !sameOptional(end, d.end) {
				return conflict.ErrIntentConflict
			}
		}
	}
	var storedCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM conflict_write_footprint WHERE tenant_id=$1::uuid AND intent_id=$2`, in.TenantID, in.ID).Scan(&storedCount); err != nil {
		return fmt.Errorf("count registered footprints: %w", err)
	}
	if storedCount != len(in.Footprints) {
		return conflict.ErrIntentConflict
	}
	return nil
}

func sameOptional(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ValidateAtCommit locks the intent and checks every authoritative stream
// head in the same caller-owned transaction. A unique active scope index
// provides the cross-connection winner fence; domain appends must occur in
// this same transaction.
func (s *Store) ValidateAtCommit(ctx context.Context, tx dbport.Tx, req conflict.CommitRequest) (conflict.CommitResult, error) {
	if req.TenantID == "" || req.IntentID == "" || req.SnapshotDigest == "" {
		return conflict.CommitResult{}, conflict.ErrInvalidIntent
	}
	var tenant, proposal, snapshot, status string
	var fence int64
	err := tx.QueryRow(ctx, `SELECT tenant_id::text,proposal_id,snapshot_digest,status,fence FROM conflict_write_intent WHERE tenant_id=$1::uuid AND intent_id=$2 FOR UPDATE`, req.TenantID, req.IntentID).Scan(&tenant, &proposal, &snapshot, &status, &fence)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return conflict.CommitResult{}, fmt.Errorf("%w: %s", ErrNotFound, req.IntentID)
		}
		return conflict.CommitResult{}, fmt.Errorf("lock intent %s: %w", req.IntentID, err)
	}
	if status != "SUBMITTED" {
		return conflict.CommitResult{}, conflict.ErrAlreadyTerminal
	}
	if snapshot != req.SnapshotDigest {
		return conflict.CommitResult{}, &conflict.Error{Code: conflict.CodeConflictStaleBaseline, IntentID: req.IntentID, Err: conflict.ErrStaleBaseline}
	}
	rows, err := tx.Query(ctx, `SELECT stream_key,expected_sequence,scope_digest,resource_canonical,field_path,operation,authority_domain,authority_policy_ref,effective_interval_canonical,interval_kind,interval_start,interval_end FROM conflict_write_footprint WHERE tenant_id=$1::uuid AND intent_id=$2 ORDER BY ordinal`, req.TenantID, req.IntentID)
	if err != nil {
		return conflict.CommitResult{}, err
	}
	defer rows.Close()
	type footprint struct {
		stream, resource, field, operation, authorityDomain, authorityPolicy, kind, start string
		expected                                                                          int64
		scope                                                                             string
		end                                                                               *string
		intervalCanonical                                                                 []byte
	}
	var footprints []footprint
	for rows.Next() {
		var stream, scope, resource, field, operation, authorityDomain, authorityPolicy, kind, start string
		var end *string
		var intervalCanonical []byte
		var expected int64
		if err := rows.Scan(&stream, &expected, &scope, &resource, &field, &operation, &authorityDomain, &authorityPolicy, &intervalCanonical, &kind, &start, &end); err != nil {
			return conflict.CommitResult{}, err
		}
		footprints = append(footprints, footprint{stream: stream, expected: expected, scope: scope, resource: resource, field: field, operation: operation, authorityDomain: authorityDomain, authorityPolicy: authorityPolicy, intervalCanonical: intervalCanonical, kind: kind, start: start, end: end})
	}
	if err := rows.Err(); err != nil {
		return conflict.CommitResult{}, err
	}
	rows.Close()
	if len(footprints) == 0 {
		return conflict.CommitResult{}, conflict.ErrInvalidIntent
	}
	if len(req.FootprintDigests) != len(footprints) {
		return conflict.CommitResult{}, conflict.ErrIntentConflict
	}
	registeredDigests := make(map[string]int, len(footprints))
	for _, footprint := range footprints {
		registeredDigests[footprint.scope]++
	}
	for _, digest := range req.FootprintDigests {
		if registeredDigests[digest] == 0 {
			return conflict.CommitResult{}, conflict.ErrIntentConflict
		}
		registeredDigests[digest]--
	}
	requested := make(map[string]uint64, len(req.Streams))
	for _, baseline := range req.Streams {
		if baseline.StreamKey == "" {
			return conflict.CommitResult{}, conflict.ErrInvalidIntent
		}
		if _, duplicate := requested[baseline.StreamKey]; duplicate {
			return conflict.CommitResult{}, conflict.ErrIntentConflict
		}
		requested[baseline.StreamKey] = baseline.ExpectedSequence
	}
	registered := make(map[string]uint64, len(footprints))
	for _, footprint := range footprints {
		if old, duplicate := registered[footprint.stream]; duplicate && old != uint64(footprint.expected) {
			return conflict.CommitResult{}, conflict.ErrIntentConflict
		}
		registered[footprint.stream] = uint64(footprint.expected)
		sequence, exists := requested[footprint.stream]
		if !exists || sequence != uint64(footprint.expected) {
			return conflict.CommitResult{}, conflict.ErrIntentConflict
		}
	}
	if len(requested) != len(registered) {
		return conflict.CommitResult{}, conflict.ErrIntentConflict
	}
	requestedWrites := make(map[string]bool, len(req.Writes))
	for _, write := range req.Writes {
		if write.ResourceCanonical == "" || write.FieldPath == "" || write.StreamKey == "" || write.AuthorityDomain == "" || write.SourceAuthorityDecision == "" || !write.Operation.Valid() {
			return conflict.CommitResult{}, conflict.ErrInvalidIntent
		}
		d, err := effectiveDimensions(write.EffectiveInterval)
		if err != nil {
			return conflict.CommitResult{}, conflict.ErrInvalidIntent
		}
		requestedWrites[write.ResourceCanonical+"\x00"+string(write.FieldPath)+"\x00"+write.StreamKey+"\x00"+write.AuthorityDomain+"\x00"+write.SourceAuthorityDecision+"\x00"+string(write.Operation)+"\x00"+d.kind+"\x00"+d.start+"\x00"+optionalString(d.end)+"\x00"+hex.EncodeToString(write.EffectiveInterval.Canonical())] = true
	}
	registeredWrites := make(map[string]bool, len(footprints))
	for _, footprint := range footprints {
		key := footprint.resource + "\x00" + footprint.field + "\x00" + footprint.stream + "\x00" + footprint.authorityDomain + "\x00" + footprint.authorityPolicy + "\x00" + footprint.operation + "\x00" + footprint.kind + "\x00" + footprint.start + "\x00" + optionalString(footprint.end) + "\x00" + hex.EncodeToString(footprint.intervalCanonical)
		registeredWrites[key] = true
		if !requestedWrites[key] {
			return conflict.CommitResult{}, conflict.ErrIntentConflict
		}
	}
	if len(requestedWrites) != len(registeredWrites) {
		return conflict.CommitResult{}, conflict.ErrIntentConflict
	}
	sort.Slice(footprints, func(i, j int) bool {
		if footprints[i].stream != footprints[j].stream {
			return footprints[i].stream < footprints[j].stream
		}
		return footprints[i].scope < footprints[j].scope
	})
	var resources []string
	seenResources := map[string]bool{}
	for _, footprint := range footprints {
		if !seenResources[footprint.resource] {
			resources = append(resources, footprint.resource)
			seenResources[footprint.resource] = true
		}
	}
	newFence := fence + 1
	sort.Strings(resources)
	for _, resource := range resources {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2,0))`, tenant, resource); err != nil {
			return conflict.CommitResult{}, fmt.Errorf("lock conflict resource: %w", err)
		}
	}
	// Stream rows are locked only after every conflict bucket is held, and in
	// canonical stream order. Competing multi-resource plans therefore cannot
	// acquire the same lock set in opposite orders and deadlock.
	for _, footprint := range footprints {
		var head int64
		if err := tx.QueryRow(ctx, `SELECT head_sequence FROM stream_head WHERE tenant_id=$1::uuid AND stream_key=$2 FOR UPDATE`, tenant, footprint.stream).Scan(&head); err != nil {
			return conflict.CommitResult{}, fmt.Errorf("lock stream head %s: %w", footprint.stream, err)
		}
		if head != footprint.expected {
			return conflict.CommitResult{}, &conflict.Error{Code: conflict.CodeConflictStaleBaseline, IntentID: req.IntentID, Err: conflict.ErrStaleBaseline}
		}
	}
	for _, f := range footprints {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM conflict_scope_fence WHERE tenant_id=$1::uuid AND intent_id<>$2 AND resource_canonical=$3 AND interval_kind=$5 AND interval_start < COALESCE($7, '9999-12-31T23:59:59.999999999Z') AND COALESCE(interval_end, '9999-12-31T23:59:59.999999999Z') > $6 AND (field_path=$4 OR field_path LIKE $4 || '.%' OR $4 LIKE field_path || '.%'))`, tenant, req.IntentID, f.resource, f.field, f.kind, f.start, f.end).Scan(&exists); err != nil {
			return conflict.CommitResult{}, fmt.Errorf("read conflict scope: %w", err)
		}
		if exists {
			return conflict.CommitResult{}, &conflict.Error{Code: conflict.CodeConflictStaleBaseline, IntentID: req.IntentID, Err: conflict.ErrStaleBaseline}
		}
	}
	for _, f := range footprints {
		if _, err := tx.Exec(ctx, `INSERT INTO conflict_scope_fence (tenant_id,scope_digest,intent_id,fence,resource_canonical,field_path,interval_kind,interval_start,interval_end) SELECT $1::uuid,$2,$3,$4,$5,$6,$7,$8,$9 WHERE NOT EXISTS (SELECT 1 FROM conflict_scope_fence WHERE tenant_id=$1::uuid AND intent_id=$3 AND scope_digest=$2)`, tenant, f.scope, req.IntentID, newFence, f.resource, f.field, f.kind, f.start, f.end); err != nil {
			return conflict.CommitResult{}, fmt.Errorf("insert conflict scope: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE conflict_write_intent SET status='COMMITTED',fence=$1 WHERE tenant_id=$2::uuid AND intent_id=$3`, newFence, tenant, req.IntentID); err != nil {
		return conflict.CommitResult{}, err
	}
	return conflict.CommitResult{Intent: conflict.WriteIntent{TenantID: tenant, ID: req.IntentID, ProposalID: proposal, SnapshotDigest: snapshot, Status: conflict.IntentCommitted, Fence: uint64(newFence)}, Fence: uint64(newFence), Decision: conflict.DecisionHardConflict}, nil
}

func (s *Store) Release(ctx context.Context, tx dbport.Tx, tenant, id string, fence uint64) error {
	var storedFence int64
	var status string
	if err := tx.QueryRow(ctx, `SELECT fence,status FROM conflict_write_intent WHERE tenant_id=$1::uuid AND intent_id=$2 FOR UPDATE`, tenant, id).Scan(&storedFence, &status); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return fmt.Errorf("lock intent %s for release: %w", id, err)
	}
	if fence == 0 || uint64(storedFence) != fence {
		return conflict.ErrFence
	}
	if status == "RELEASED" {
		return nil
	}
	if status != "COMMITTED" {
		return conflict.ErrAlreadyTerminal
	}
	if _, err := tx.Exec(ctx, `UPDATE conflict_write_intent SET status='RELEASED' WHERE tenant_id=$1::uuid AND intent_id=$2`, tenant, id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT hcmnext_release_conflict_scope_fence($1::uuid,$2,$3)`, tenant, id, fence)
	return err
}
