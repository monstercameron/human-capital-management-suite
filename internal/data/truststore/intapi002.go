package truststore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CheckRevocation implements trust.RevocationSource over the durable
// registry: the client's lifecycle is re-read on every use, and the token
// identifier is recorded write-once, so a revoked client fails closed and
// a replayed identifier is refused even across replicas and restarts.
//
// Machine sessions live and die with the client registration and the
// token's own expiry: they are not rows in the session store, so there is
// no session-store lookup here. The advisory lock serializes concurrent
// first presentations of one identifier inside the transaction, which is
// what makes exactly-once hold under concurrency.
func (r *MachineRegistry) CheckRevocation(ctx context.Context, q trust.RevocationQuery) error {
	if r.store == nil {
		return failure(CodeInvalid, "machine_token_use", q.TokenID, errors.New("no store configured"))
	}
	id, err := r.tenantIDByKey(q.Tenant)
	if err != nil {
		return err
	}
	if q.ClientID == "" || q.TokenID == "" || q.Session == "" {
		return failure(CodeInvalid, "machine_token_use", q.TokenID, errors.New("client, token identifier and session are required"))
	}
	if q.ExpiresAt.IsZero() {
		return failure(CodeInvalid, "machine_token_use", q.TokenID, errors.New("token expiry is required"))
	}
	now := time.Now().UTC()
	return r.store.withTenant(ctx, id, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			id.String()+":trust-machine-token:"+q.TokenID); err != nil {
			return failure(CodeDatabase, "machine_token_use", q.TokenID, err)
		}
		var status string
		var revoked bool
		var expiresAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT status, revoked, expires_at FROM machine_client
			WHERE tenant_id=$1 AND client_id=$2`, id, q.ClientID).Scan(&status, &revoked, &expiresAt)
		if errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("%w: %s", trust.ErrClientRevoked, q.ClientID)
		}
		if err != nil {
			return failure(CodeDatabase, "machine_client", q.ClientID, err)
		}
		if status != MachineClientActive || revoked {
			return fmt.Errorf("%w: %s", trust.ErrClientRevoked, q.ClientID)
		}
		if expiresAt != nil && !now.Before(expiresAt.UTC()) {
			return fmt.Errorf("%w: %s", trust.ErrClientRevoked, q.ClientID)
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO machine_token_use (tenant_id,token_jti,client_id,session_ref,expires_at)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			id, q.TokenID, q.ClientID, q.Session, q.ExpiresAt.UTC())
		if err != nil {
			return failure(CodeDatabase, "machine_token_use", q.TokenID, err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: %s", trust.ErrTokenReplayed, q.TokenID)
		}
		return nil
	})
}

var _ trust.RevocationSource = (*MachineRegistry)(nil)
