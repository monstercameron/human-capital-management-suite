package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// REV-090-02: the list read path.
//
// ListJourneys used to resolve each journey on its own: one proposal lookup,
// one work-item locate and the four workflow-record reads per journey, plus a
// full governed re-simulation (and a second record read) for every proposal
// that had not executed. A page of N journeys therefore cost O(N) round trips
// and N simulations. readJourneyListRecords answers the same questions for
// the whole page in a fixed number of statements, independent of N:
//
//  1. the latest stored proposal revision per intent (proposal_revision),
//  2. the stored simulation answer bound to it (intent_simulation_result),
//  3. the workflow instance each stored proposal started (work_item),
//  4. those instances, 5. their node executions, 6. their work items,
//  7. those work items' transitions, and 8. their ledger facts.
//
// An unexecuted proposal is described by what Propose durably recorded -- its
// materialized revision and the READY simulation result bound to it -- rather
// than by re-running today's rules. The detail read (Inspect) still
// re-simulates one journey when its reader opens it; the list does not.

// journeyListEntry is one intent's durable list facts. Every field is empty
// for an intent that never stored a proposal revision.
type journeyListEntry struct {
	materialDigest     string
	proposalRevisionID string
	record             journeyRecord
}

// readJourneyListRecords resolves every listed intent's durable record inside
// the caller's tenant-scoped transaction. intentIDs that are not UUIDs have
// no durable rows by construction and are simply absent from the answer.
func (e *journeyEngine) readJourneyListRecords(
	ctx context.Context, tx dbport.Tx, principal *trust.Principal, intentIDs []string,
) (map[string]journeyListEntry, error) {
	out := make(map[string]journeyListEntry, len(intentIDs))
	ids := make([]string, 0, len(intentIDs))
	for _, raw := range intentIDs {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		ids = append(ids, parsed.String())
	}
	if len(ids) == 0 {
		return out, nil
	}
	tenantID := e.svc.tenantUUID(principal.Tenant())

	// 1. Latest stored proposal revision per intent.
	digestRows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (intent_id) intent_id::text, material_digest
		FROM proposal_revision
		WHERE tenant_id = $1 AND intent_id = ANY($2::text[]::uuid[])
		ORDER BY intent_id, revision DESC`, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("app: journey: list stored proposals: %w", err)
	}
	digests := make(map[string]string, len(ids))
	if err := scanPairs(digestRows, func(intentID, digest string) { digests[intentID] = digest }); err != nil {
		return nil, fmt.Errorf("app: journey: list stored proposals: %w", err)
	}
	if len(digests) == 0 {
		return out, nil
	}

	// 2. The stored simulation answer that names the revision id. Only a
	// READY result bound to the same material digest counts.
	simRows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (intent_id) intent_id::text,
		       CASE WHEN simulation_status = $3 THEN proposal_digest || '|' || coalesce(result_body->>'proposal_revision_id', '') ELSE '' END
		FROM intent_simulation_result
		WHERE tenant_id = $1 AND intent_id = ANY($2::text[]::uuid[])
		ORDER BY intent_id, revision DESC, simulation_sequence DESC`, tenantID, ids, intentcontrol.SimulationReady)
	if err != nil {
		return nil, fmt.Errorf("app: journey: list stored simulations: %w", err)
	}
	revisionIDs := make(map[string]string, len(digests))
	if err := scanPairs(simRows, func(intentID, bound string) {
		at := strings.LastIndex(bound, "|")
		if at > 0 && at < len(bound)-1 && bound[:at] == digests[intentID] {
			revisionIDs[intentID] = bound[at+1:]
		}
	}); err != nil {
		return nil, fmt.Errorf("app: journey: list stored simulations: %w", err)
	}

	// 3. The workflow instance each stored proposal started. The earliest
	// work item names it, exactly as locateInstance picks it.
	refs := make([]string, 0, len(digests))
	for _, digest := range digests {
		refs = append(refs, digest)
	}
	sort.Strings(refs)
	locateRows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (proposal_ref) proposal_ref, workflow_instance_id::text
		FROM work_item
		WHERE tenant_id = $1 AND proposal_ref = ANY($2::text[])
		ORDER BY proposal_ref, created_at`, tenantID, refs)
	if err != nil {
		return nil, fmt.Errorf("app: journey: locate workflow instances: %w", err)
	}
	instanceByDigest := make(map[string]uuid.UUID, len(refs))
	var instanceIDs []uuid.UUID
	var parseErr error
	if err := scanPairs(locateRows, func(digest, rawInstance string) {
		id, idErr := uuid.Parse(rawInstance)
		if idErr != nil {
			parseErr = idErr
			return
		}
		instanceByDigest[digest] = id
		instanceIDs = append(instanceIDs, id)
	}); err != nil {
		return nil, fmt.Errorf("app: journey: locate workflow instances: %w", err)
	}
	if parseErr != nil {
		return nil, fmt.Errorf("app: journey: locate workflow instances: %w", parseErr)
	}

	records, err := e.loadJourneyRecords(ctx, tx, tenantID, instanceIDs)
	if err != nil {
		return nil, err
	}
	for intentID, digest := range digests {
		entry := journeyListEntry{materialDigest: digest, proposalRevisionID: revisionIDs[intentID]}
		if instanceID, ok := instanceByDigest[digest]; ok {
			entry.record = records[instanceID]
		}
		out[intentID] = entry
	}
	return out, nil
}

// loadJourneyRecords is readRecord for a set of instances: statements 4-8.
// An instance the batch cannot read (absent, or hidden by row-level security)
// yields the zero record, which is what readRecord's caller sees for a
// journey that has not executed.
func (e *journeyEngine) loadJourneyRecords(
	ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, instanceIDs []uuid.UUID,
) (map[uuid.UUID]journeyRecord, error) {
	out := make(map[uuid.UUID]journeyRecord, len(instanceIDs))
	if len(instanceIDs) == 0 {
		return out, nil
	}
	runtimeStore, itemStore := runtime.Store{}, workitem.Store{}
	instances, err := runtimeStore.LoadInstances(ctx, tx, tenantID, instanceIDs)
	if err != nil {
		return nil, fmt.Errorf("app: journey: load the workflow instances: %w", err)
	}
	if len(instances) == 0 {
		return out, nil
	}
	present := make([]uuid.UUID, 0, len(instances))
	for _, id := range instanceIDs {
		if _, ok := instances[id]; ok {
			present = append(present, id)
		}
	}
	nodes, err := runtimeStore.LoadNodeExecutionsForInstances(ctx, tx, tenantID, present)
	if err != nil {
		return nil, fmt.Errorf("app: journey: load the node executions: %w", err)
	}
	items, err := itemStore.ListForInstances(ctx, tx, tenantID, present)
	if err != nil {
		return nil, fmt.Errorf("app: journey: list the work items: %w", err)
	}
	var itemIDs []uuid.UUID
	for _, id := range present {
		for _, item := range items[id] {
			itemIDs = append(itemIDs, item.WorkItemID)
		}
	}
	transitions, err := itemStore.LoadTransitionsForItems(ctx, tx, tenantID, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("app: journey: load work item transitions: %w", err)
	}
	ledger, err := e.readLedgers(ctx, tx, tenantID, instances)
	if err != nil {
		return nil, err
	}
	for _, id := range present {
		instance := instances[id]
		record := journeyRecord{instance: &instance, nodes: nodes[id], items: items[id]}
		if record.nodes == nil {
			record.nodes = []runtime.NodeExecution{}
		}
		if record.items == nil {
			record.items = []workitem.WorkItem{}
		}
		for _, item := range record.items {
			for _, t := range transitions[item.WorkItemID] {
				record.transitions = append(record.transitions, workspace.JourneyTransition{
					WorkItemID: t.WorkItemID.String(),
					From:       string(t.FromStatus),
					To:         string(t.ToStatus),
					Actor:      t.ActorPrincipalID,
					Reason:     t.Reason,
					At:         t.At.UTC(),
				})
			}
		}
		sort.SliceStable(record.transitions, func(i, j int) bool {
			return record.transitions[i].At.Before(record.transitions[j].At)
		})
		record.ledger = ledger[effects.StreamKeyFor(instance.WorkflowID, instance.InstanceID.String())]
		out[id] = record
	}
	return out, nil
}

// readLedgers is readLedger for a set of instances: the first governed fact
// on each instance's own stream, in one statement.
func (e *journeyEngine) readLedgers(
	ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, instances map[uuid.UUID]runtime.Instance,
) (map[string]*workspace.JourneyLedgerEvent, error) {
	out := make(map[string]*workspace.JourneyLedgerEvent, len(instances))
	keys := make([]string, 0, len(instances))
	for _, instance := range instances {
		keys = append(keys, effects.StreamKeyFor(instance.WorkflowID, instance.InstanceID.String()))
	}
	sort.Strings(keys)
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (stream_key) stream_key, sequence, schema_ref, digest, idempotency_key,
		       occurred_at, effective_at, recorded_at
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = ANY($2::text[])
		ORDER BY stream_key, sequence`, tenantID, keys)
	if err != nil {
		return nil, fmt.Errorf("app: journey: read the ledger facts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event workspace.JourneyLedgerEvent
		if err := rows.Scan(&event.StreamKey, &event.Sequence, &event.SchemaRef, &event.Digest,
			&event.IdempotencyKey, &event.OccurredAt, &event.EffectiveAt, &event.RecordedAt); err != nil {
			return nil, fmt.Errorf("app: journey: read the ledger facts: %w", err)
		}
		event.OccurredAt = event.OccurredAt.UTC()
		event.EffectiveAt = event.EffectiveAt.UTC()
		event.RecordedAt = event.RecordedAt.UTC()
		out[event.StreamKey] = &event
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("app: journey: read the ledger facts: %w", err)
	}
	return out, nil
}

// scanPairs drains a two-text-column result.
func scanPairs(rows dbport.Rows, each func(first, second string)) error {
	defer rows.Close()
	for rows.Next() {
		var first, second string
		if err := rows.Scan(&first, &second); err != nil {
			return err
		}
		each(first, second)
	}
	return rows.Err()
}

// normalizedIntentID is the canonical text form readJourneyListRecords keys
// its answer by; a value that is not a UUID is returned unchanged and matches
// nothing.
func normalizedIntentID(raw string) string {
	if parsed, err := uuid.Parse(raw); err == nil {
		return parsed.String()
	}
	return raw
}
