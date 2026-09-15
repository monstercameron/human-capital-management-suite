package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

const advanceRequestDigestProfile = "hcmnext.workflow.runtime.AdvanceRequest/v1"

type advanceRequestIdentity struct {
	TenantID                string
	InstanceID              string
	ExpectedInstanceVersion int64
	Attempt                 int
	PlanDigest              string
	Outcome                 frontier.NodeOutcome
	Refs                    GovernanceRefs
	TraceID                 string
	RecordedAt              string
}

func computeAdvanceRequestDigest(req AdvanceRequest) string {
	return canonicalDigest(advanceRequestDigestProfile, advanceRequestIdentity{
		TenantID:                req.TenantID.String(),
		InstanceID:              req.InstanceID.String(),
		ExpectedInstanceVersion: req.ExpectedInstanceVersion,
		Attempt:                 req.Attempt,
		PlanDigest:              req.Plan.Digest(),
		Outcome:                 req.Outcome,
		Refs:                    req.Refs,
		TraceID:                 req.TraceID,
		RecordedAt:              req.RecordedAt.UTC().Format(time.RFC3339Nano),
	})
}

// durableAdvanceReceipt is the JSON payload stored for a committed Advance.
// It deliberately omits Continuations (which are independently durable) and
// Replay (which describes this read, not the committed result). ReceiptDigest
// detects malformed or manually altered payloads on the replay path.
type durableAdvanceReceipt struct {
	TenantID           string   `json:"tenant_id"`
	InstanceID         string   `json:"instance_id"`
	NodeID             string   `json:"node_id"`
	Attempt            int      `json:"attempt"`
	CompletedState     string   `json:"completed_state"`
	RouteKey           string   `json:"route_key,omitempty"`
	OutputDigest       string   `json:"output_digest,omitempty"`
	NewInstanceVersion int64    `json:"new_instance_version"`
	Frontier           []string `json:"frontier"`
	Complete           bool     `json:"complete"`
	TerminalCode       string   `json:"terminal_code,omitempty"`
	ReceiptDigest      string   `json:"receipt_digest"`
}

type storedAdvancementReceipt struct {
	RequestDigest            string
	ResultingInstanceVersion int64
	Receipt                  AdvanceReceipt
}

func recordAdvancementReceipt(
	ctx context.Context,
	ex Executor,
	req AdvanceRequest,
	requestDigest string,
	receipt AdvanceReceipt,
) error {
	payload, err := json.Marshal(durableAdvanceReceipt{
		TenantID:           receipt.TenantID.String(),
		InstanceID:         receipt.InstanceID.String(),
		NodeID:             receipt.NodeID,
		Attempt:            receipt.Attempt,
		CompletedState:     receipt.CompletedState,
		RouteKey:           receipt.RouteKey,
		OutputDigest:       receipt.OutputDigest,
		NewInstanceVersion: receipt.NewInstanceVersion,
		Frontier:           append([]string(nil), receipt.Frontier...),
		Complete:           receipt.Complete,
		TerminalCode:       receipt.TerminalCode,
		ReceiptDigest:      receipt.Digest(),
	})
	if err != nil {
		return wrap(CodeStorageFailed, req.InstanceID.String(), req.Outcome.NodeID, err,
			"encode advancement receipt")
	}
	_, err = ex.Exec(ctx, `
		INSERT INTO workflow_advancement_receipt (
			tenant_id, instance_id, node_id, attempt, expected_instance_version,
			request_digest, resulting_instance_version, receipt, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		req.TenantID, req.InstanceID, req.Outcome.NodeID, req.Attempt,
		req.ExpectedInstanceVersion, requestDigest, receipt.NewInstanceVersion,
		payload, req.RecordedAt.UTC())
	if err != nil {
		return wrap(CodeStorageFailed, req.InstanceID.String(), req.Outcome.NodeID, err,
			"record durable advancement receipt")
	}
	return nil
}

func loadAdvancementReceipt(
	ctx context.Context,
	ex Executor,
	req AdvanceRequest,
) (storedAdvancementReceipt, bool, error) {
	var (
		stored  storedAdvancementReceipt
		payload []byte
	)
	err := ex.QueryRow(ctx, `
		SELECT request_digest, resulting_instance_version, receipt
		FROM workflow_advancement_receipt
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3
		  AND attempt = $4 AND expected_instance_version = $5`,
		req.TenantID, req.InstanceID, req.Outcome.NodeID, req.Attempt,
		req.ExpectedInstanceVersion).Scan(
		&stored.RequestDigest, &stored.ResultingInstanceVersion, &payload)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return storedAdvancementReceipt{}, false, nil
		}
		return storedAdvancementReceipt{}, false, wrap(
			CodeStorageFailed, req.InstanceID.String(), req.Outcome.NodeID, err,
			"load durable advancement receipt")
	}

	var durable durableAdvanceReceipt
	if err := json.Unmarshal(payload, &durable); err != nil {
		return storedAdvancementReceipt{}, false, wrap(
			CodeStorageFailed, req.InstanceID.String(), req.Outcome.NodeID, err,
			"decode durable advancement receipt")
	}
	tenantID, err := uuid.Parse(durable.TenantID)
	if err != nil {
		return storedAdvancementReceipt{}, false, wrap(
			CodeStorageFailed, req.InstanceID.String(), req.Outcome.NodeID, err,
			"decode durable advancement receipt tenant")
	}
	instanceID, err := uuid.Parse(durable.InstanceID)
	if err != nil {
		return storedAdvancementReceipt{}, false, wrap(
			CodeStorageFailed, req.InstanceID.String(), req.Outcome.NodeID, err,
			"decode durable advancement receipt instance")
	}
	stored.Receipt = AdvanceReceipt{
		TenantID:           tenantID,
		InstanceID:         instanceID,
		NodeID:             durable.NodeID,
		Attempt:            durable.Attempt,
		CompletedState:     durable.CompletedState,
		RouteKey:           durable.RouteKey,
		OutputDigest:       durable.OutputDigest,
		NewInstanceVersion: durable.NewInstanceVersion,
		Frontier:           append([]string(nil), durable.Frontier...),
		Complete:           durable.Complete,
		TerminalCode:       durable.TerminalCode,
		Replay:             true,
	}
	stored.Receipt.digest = computeAdvanceReceiptDigest(stored.Receipt)
	if stored.ResultingInstanceVersion != stored.Receipt.NewInstanceVersion ||
		durable.ReceiptDigest != stored.Receipt.Digest() ||
		stored.Receipt.TenantID != req.TenantID ||
		stored.Receipt.InstanceID != req.InstanceID ||
		stored.Receipt.NodeID != req.Outcome.NodeID ||
		stored.Receipt.Attempt != req.Attempt {
		return storedAdvancementReceipt{}, false, refuse(
			CodeStorageFailed, req.InstanceID.String(), req.Outcome.NodeID,
			"durable advancement receipt does not match its storage key or digest")
	}
	return stored, true, nil
}

// AdvancementReceiptRecord is one durable advancement receipt as the
// execution inspector (WF-RUN-019) reads it back: the storage key, the
// request digest the replay authority compares, and the receipt itself.
//
// DigestVerified is false when the stored payload no longer digests to the
// receipt digest it carries, or no longer matches its own storage key. Such a
// row is still returned -- an inspector that silently dropped tampered
// evidence would hide exactly the row an operator needs to see -- but Receipt
// then carries only what the payload claims and must not be trusted.
type AdvancementReceiptRecord struct {
	NodeID                   string
	Attempt                  int
	ExpectedInstanceVersion  int64
	ResultingInstanceVersion int64
	RequestDigest            string
	// StoredReceiptDigest is the digest the payload recorded at commit.
	StoredReceiptDigest string
	DigestVerified      bool
	Receipt             AdvanceReceipt
	RecordedAt          time.Time
}

// LoadAdvancementReceipts reads every durable advancement receipt one
// instance committed, ordered by the instance version each produced, and
// re-verifies each payload against its recorded digest and storage key.
//
// A missing instance and another tenant's instance both read as an empty
// list: this function never discloses that an instance it may not see
// exists. Only a storage or decode failure is an error.
func LoadAdvancementReceipts(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (ret0 []AdvancementReceiptRecord, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.load_advancement_receipts", tenantID, instanceID)
	defer func() { observe.DoneWith(obsOp, retErr, len(ret0)) }()
	rows, err := ex.Query(ctx, `
		SELECT node_id, attempt, expected_instance_version, resulting_instance_version,
		       request_digest, receipt, recorded_at
		FROM workflow_advancement_receipt
		WHERE tenant_id = $1 AND instance_id = $2
		ORDER BY resulting_instance_version, node_id, attempt`,
		tenantID, instanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "read advancement receipts")
	}
	defer rows.Close()

	out := []AdvancementReceiptRecord{}
	for rows.Next() {
		var (
			rec     AdvancementReceiptRecord
			attempt int32
			payload []byte
		)
		if err := rows.Scan(&rec.NodeID, &attempt, &rec.ExpectedInstanceVersion,
			&rec.ResultingInstanceVersion, &rec.RequestDigest, &payload, &rec.RecordedAt); err != nil {
			return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "scan advancement receipt")
		}
		rec.Attempt, rec.RecordedAt = int(attempt), rec.RecordedAt.UTC()
		var durable durableAdvanceReceipt
		if err := json.Unmarshal(payload, &durable); err != nil {
			return nil, wrap(CodeStorageFailed, instanceID.String(), rec.NodeID, err,
				"decode advancement receipt")
		}
		receiptTenant, tenantErr := uuid.Parse(durable.TenantID)
		receiptInstance, instanceErr := uuid.Parse(durable.InstanceID)
		rec.StoredReceiptDigest = durable.ReceiptDigest
		rec.Receipt = AdvanceReceipt{
			TenantID: receiptTenant, InstanceID: receiptInstance,
			NodeID: durable.NodeID, Attempt: durable.Attempt,
			CompletedState: durable.CompletedState, RouteKey: durable.RouteKey,
			OutputDigest: durable.OutputDigest, NewInstanceVersion: durable.NewInstanceVersion,
			Frontier: append([]string{}, durable.Frontier...),
			Complete: durable.Complete, TerminalCode: durable.TerminalCode, Replay: true,
		}
		rec.Receipt.digest = computeAdvanceReceiptDigest(rec.Receipt)
		rec.DigestVerified = tenantErr == nil && instanceErr == nil &&
			durable.ReceiptDigest == rec.Receipt.Digest() &&
			receiptTenant == tenantID && receiptInstance == instanceID &&
			durable.NodeID == rec.NodeID && durable.Attempt == rec.Attempt &&
			durable.NewInstanceVersion == rec.ResultingInstanceVersion
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "iterate advancement receipts")
	}
	return out, nil
}
