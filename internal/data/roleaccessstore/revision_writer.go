package roleaccessstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"google.golang.org/protobuf/proto"
)

const (
	roleAccessRevisionSchemaRef  = "journey.RoleAccessRevisionEvent.v1"
	roleAccessRevisionStreamKind = "TENANT"
)

func validRevisionReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return reason != "" && len(reason) <= 500 && !strings.ContainsAny(reason, "\r\n\x00")
}

func appendRevision(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, actor, reason string, kind ChangeKind, roleID, workerRef, organizationScopeID, pageID, featureID string, before, after any) error {
	if tx == nil || tenantID == uuid.Nil || strings.TrimSpace(actor) == "" || !validRevisionReason(reason) {
		return ErrInvalidRevision
	}
	var priorID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT revision_id
		FROM access_role_revision
		WHERE tenant_id=$1 AND change_kind=$2 AND role_id=$3 AND worker_ref=$4
		  AND organization_scope_id=$5 AND page_id=$6 AND feature_id=$7
		ORDER BY recorded_at DESC, revision_id DESC
		LIMIT 1`, tenantID, string(kind), roleID, workerRef, organizationScopeID, pageID, featureID).Scan(&priorID)
	var prior *uuid.UUID
	if err == nil {
		prior = &priorID
	} else if !errors.Is(err, dbport.ErrNoRows) {
		return fmt.Errorf("roleaccessstore: read prior permission revision: %w", err)
	}
	entry, err := newScopedRevision(uuid.New(), actor, kind, roleID, workerRef, organizationScopeID, pageID, featureID, before, after, prior, reason)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO access_role_revision (
			tenant_id, revision_id, recorded_at, actor_ref, change_reason, change_kind, role_id,
			worker_ref, organization_scope_id, page_id, feature_id, before_row, after_row, prior_revision
		) VALUES ($1,$2,transaction_timestamp(),$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::jsonb,$13)`,
		tenantID, entry.RevisionID, entry.ActorRef, entry.Reason, string(entry.Kind), entry.RoleID,
		entry.WorkerRef, entry.OrganizationScopeID, entry.PageID, entry.FeatureID,
		jsonImage(entry.Before), jsonImage(entry.After), entry.PriorRevision); err != nil {
		return fmt.Errorf("roleaccessstore: append permission revision: %w", err)
	}
	return appendLedgerEvent(ctx, tx, tenantID, entry)
}

func appendLedgerEvent(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, entry RevisionEntry) error {
	const messageName = "hcmnext.journey.v1.RoleAccessRevisionEvent"
	if _, err := tx.Exec(ctx, `INSERT INTO payload_schema (tenant_id,schema_ref,schema_id,schema_version,message_full_name,wire_format,canonicalization_profile)
		VALUES ($1,$2,$3,1,$4,'PROTOBUF','LEDGER_EVENT') ON CONFLICT (tenant_id,schema_ref) DO NOTHING`, tenantID, roleAccessRevisionSchemaRef, roleAccessRevisionSchemaRef, messageName); err != nil {
		return fmt.Errorf("roleaccessstore: register revision event schema: %w", err)
	}
	var schemaID, fullName, wireFormat, profile string
	var version int64
	if err := tx.QueryRow(ctx, `SELECT schema_id,schema_version,message_full_name,wire_format,canonicalization_profile FROM payload_schema WHERE tenant_id=$1 AND schema_ref=$2`, tenantID, roleAccessRevisionSchemaRef).Scan(&schemaID, &version, &fullName, &wireFormat, &profile); err != nil {
		return fmt.Errorf("roleaccessstore: verify revision event schema: %w", err)
	}
	if schemaID != roleAccessRevisionSchemaRef || version != 1 || fullName != messageName || wireFormat != "PROTOBUF" || profile != "LEDGER_EVENT" {
		return fmt.Errorf("roleaccessstore: incompatible registered revision event schema")
	}
	streamKey := "tenant:" + tenantID.String() + ":role-access"
	if err := ledger.EnsureStream(ctx, tx, tenantID, streamKey, roleAccessRevisionStreamKind, tenantID.String()); err != nil {
		return fmt.Errorf("roleaccessstore: ensure role access event stream: %w", err)
	}
	var head int64
	if err := tx.QueryRow(ctx, `SELECT head_sequence FROM stream_head WHERE tenant_id=$1 AND stream_key=$2 FOR UPDATE`, tenantID, streamKey).Scan(&head); err != nil {
		return fmt.Errorf("roleaccessstore: read role access event stream head: %w", err)
	}
	message := &journeyv1.RoleAccessRevisionEvent{
		RevisionId: entry.RevisionID.String(), ActorRef: entry.ActorRef, Reason: entry.Reason,
		ChangeKind: string(entry.Kind), RoleId: entry.RoleID, WorkerRef: entry.WorkerRef,
		OrganizationScopeId: entry.OrganizationScopeID, PageId: entry.PageID, FeatureId: entry.FeatureID,
		BeforeRow: append([]byte(nil), entry.Before...), AfterRow: append([]byte(nil), entry.After...),
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil {
		return fmt.Errorf("roleaccessstore: encode revision event: %w", err)
	}
	eventAt := time.Now().UTC()
	if _, err := ledger.Append(ctx, tx, ledger.AppendRequest{
		Tenant: tenantID, StreamKey: streamKey, ExpectedHead: head,
		AssertionClass: ledger.TransactionFact, SourceRef: "hcmnext:data:roleaccessstore",
		SchemaRef: roleAccessRevisionSchemaRef, Payload: payload, OccurredAt: eventAt,
		EffectiveAt: eventAt, CorrelationID: entry.RevisionID, IdempotencyKey: entry.RevisionID.String(),
	}); err != nil {
		return fmt.Errorf("roleaccessstore: append permission ledger event: %w", err)
	}
	return nil
}

func jsonImage(raw []byte) any {
	if raw == nil {
		return nil
	}
	return string(raw)
}
