// Package webhookreceipts stores authenticated inbound callbacks as a durable
// inbox. The exact signed bytes and parsed receipt share one append-only row,
// so a worker can replay the verified event after a process restart.
package webhookreceipts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var (
	ErrInvalid  = errors.New("webhookreceipts: invalid receipt")
	ErrConflict = errors.New("webhookreceipts: event id has different signed bytes")
	ErrNotFound = errors.New("webhookreceipts: receipt not found")
)

// DB opens transactions for tenant scoped inbox operations.
type DB interface{ dbport.Beginner }

// Scope fixes the endpoint identity selected by application configuration.
// A provider never chooses its endpoint, tenant, or provider from the body.
type Scope struct {
	TenantID   uuid.UUID
	Provider   string
	EndpointID string
}

// Store persists one endpoint's authenticated inbound receipts.
type Store struct {
	db    DB
	scope Scope
}

func New(db DB, scope Scope) (*Store, error) {
	if db == nil || scope.TenantID == uuid.Nil || strings.TrimSpace(scope.EndpointID) == "" || strings.TrimSpace(scope.EndpointID) != scope.EndpointID ||
		(scope.Provider != "payroll" && scope.Provider != "iam") {
		return nil, fmt.Errorf("%w: database and complete endpoint scope are required", ErrInvalid)
	}
	return &Store{db: db, scope: scope}, nil
}

// Record atomically appends a parsed receipt and its exact signed payload.
// It reports duplicate for a byte-identical delivery and refuses changed
// bytes under the same provider event identity.
func (s *Store) Record(ctx context.Context, parsed providerreceipt.Parsed, receivedAt time.Time) (duplicate bool, err error) {
	if err := s.validate(parsed, receivedAt); err != nil {
		return false, err
	}
	storedParsed := parsed
	storedParsed.Payload = nil
	encoded, err := json.Marshal(storedParsed)
	if err != nil {
		return false, fmt.Errorf("webhookreceipts: encode parsed receipt: %w", err)
	}
	receiptID := receiptUUID(s.scope, parsed.EventID)
	outboxPayload, err := json.Marshal(struct {
		ReceiptID string `json:"receipt_id"`
		TenantID  string `json:"tenant_id"`
		Provider  string `json:"provider"`
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		Schema    string `json:"schema"`
	}{receiptID.String(), parsed.TenantID, parsed.Provider, parsed.EventID, parsed.EventType, parsed.Schema})
	if err != nil {
		return false, fmt.Errorf("webhookreceipts: encode outbox reference: %w", err)
	}
	err = s.withScope(ctx, func(tx dbport.Tx) error {
		requestDigest := envelopeDigest(s.scope, parsed)
		n, err := tx.Exec(ctx, `INSERT INTO integration_webhook_receipt
			(tenant_id, receipt_id, provider, endpoint_id, event_id, event_type, schema_ref, payload_digest, request_digest, payload_bytes, parsed_receipt, received_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)
			ON CONFLICT (tenant_id, provider, event_id) DO NOTHING`, s.scope.TenantID, receiptID, s.scope.Provider, s.scope.EndpointID,
			parsed.EventID, parsed.EventType, parsed.Schema, parsed.PayloadDigest, requestDigest, parsed.Payload, string(encoded), receivedAt.UTC())
		if err != nil {
			return fmt.Errorf("webhookreceipts: insert inbox receipt: %w", err)
		}
		if n == 1 {
			if _, err := tx.Exec(ctx, `INSERT INTO integration_webhook_outbox
				(tenant_id, outbox_id, receipt_id, effect_identity, ordering_key, schema_ref, payload, status, attempts, available_at, created_at, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,'PENDING',0,$8,$8,$8)`, s.scope.TenantID,
				receiptID, receiptID,
				"webhook.receipt:"+receiptID.String(), s.scope.EndpointID, parsed.Schema, string(outboxPayload), receivedAt.UTC()); err != nil {
				return fmt.Errorf("webhookreceipts: enqueue verified receipt: %w", err)
			}
			return nil
		}
		var storedDigest string
		if err := tx.QueryRow(ctx, `SELECT request_digest FROM integration_webhook_receipt
			WHERE tenant_id=$1 AND provider=$2 AND event_id=$3`, s.scope.TenantID, s.scope.Provider, parsed.EventID).Scan(&storedDigest); err != nil {
			return fmt.Errorf("webhookreceipts: read duplicate digest: %w", err)
		}
		if storedDigest != requestDigest {
			return ErrConflict
		}
		duplicate = true
		return nil
	})
	return duplicate, err
}

// Replay returns the stored parsed receipt and the exact signed bytes only
// after an approved internal actor states the replay purpose.
func (s *Store) Replay(ctx context.Context, eventID string, approval webhook.ReplayApproval) (providerreceipt.Parsed, error) {
	if s == nil || s.db == nil {
		return providerreceipt.Parsed{}, ErrInvalid
	}
	if !approval.Approved || strings.TrimSpace(approval.Actor) == "" || strings.TrimSpace(approval.Purpose) == "" {
		return providerreceipt.Parsed{}, webhook.ErrReplayNotAuthorized
	}
	if strings.TrimSpace(eventID) == "" {
		return providerreceipt.Parsed{}, ErrInvalid
	}
	var parsed providerreceipt.Parsed
	var rawParsed string
	var exact []byte
	err := s.withScope(ctx, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload_bytes, parsed_receipt::text FROM integration_webhook_receipt
			WHERE tenant_id=$1 AND provider=$2 AND endpoint_id=$3 AND event_id=$4`,
			s.scope.TenantID, s.scope.Provider, s.scope.EndpointID, eventID).Scan(&exact, &rawParsed)
	})
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return providerreceipt.Parsed{}, ErrNotFound
		}
		return providerreceipt.Parsed{}, fmt.Errorf("webhookreceipts: read replay: %w", err)
	}
	if err := json.Unmarshal([]byte(rawParsed), &parsed); err != nil {
		return providerreceipt.Parsed{}, fmt.Errorf("webhookreceipts: decode replay receipt: %w", err)
	}
	// parsed_receipt includes a payload copy; the bytea column is authoritative
	// because it is the exact byte sequence covered by the provider signature.
	if digest(exact) != parsed.PayloadDigest {
		return providerreceipt.Parsed{}, fmt.Errorf("webhookreceipts: stored byte digest mismatch")
	}
	parsed.Payload = append([]byte(nil), exact...)
	return parsed, nil
}

func (s *Store) validate(parsed providerreceipt.Parsed, at time.Time) error {
	if s == nil || s.db == nil || s.scope.TenantID == uuid.Nil || parsed.TenantID != s.scope.TenantID.String() ||
		parsed.Provider != s.scope.Provider || strings.TrimSpace(parsed.EventID) == "" || strings.TrimSpace(parsed.EventType) == "" ||
		strings.TrimSpace(parsed.Schema) == "" || len(parsed.Payload) == 0 || len(parsed.Payload) > 65536 || at.IsZero() || digest(parsed.Payload) != parsed.PayloadDigest {
		return ErrInvalid
	}
	return nil
}

func (s *Store) withScope(ctx context.Context, fn func(dbport.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("webhookreceipts: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, s.scope.TenantID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("webhookreceipts: commit: %w", err)
	}
	return nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func envelopeDigest(scope Scope, parsed providerreceipt.Parsed) string {
	return digest([]byte(parsed.TenantID + "\n" + parsed.Provider + "\n" + scope.EndpointID + "\n" + parsed.EventID + "\n" + parsed.EventType + "\n" + parsed.Schema + "\n" + parsed.PayloadDigest))
}

func receiptUUID(scope Scope, eventID string) uuid.UUID {
	identity := scope.TenantID.String() + "\n" + scope.Provider + "\n" + eventID
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(identity))
}
