// Package pgstore provides durable, one-time storage for OIDC PKCE state.
package pgstore

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type DB interface{ dbport.Beginner }

type Store struct {
	db       DB
	tenantID func(values.TenantId) values.TenantId
}

var tenantUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func New(db DB) *Store { return NewWithTenantMapper(db, nil) }

func NewWithTenantMapper(db DB, tenantID func(values.TenantId) values.TenantId) *Store {
	if tenantID == nil {
		tenantID = func(v values.TenantId) values.TenantId { return v }
	}
	return &Store{db: db, tenantID: tenantID}
}

func (s *Store) Put(ctx context.Context, p oidc.PendingAuthorization) error {
	if s == nil || s.db == nil || s.tenantID == nil || p.Tenant.Validate() != nil || p.State == "" || p.Nonce == "" || p.CodeChallengeDigest == "" || p.IssuerURL == "" || p.ClientID == "" || p.RedirectURI == "" || p.CreatedAt.IsZero() || !p.ExpiresAt.After(p.CreatedAt) {
		return errors.New("oidc pgstore: invalid pending authorization")
	}
	storageTenant := s.tenantID(p.Tenant)
	if storageTenant.Validate() != nil || !tenantUUID.MatchString(storageTenant.String()) {
		return errors.New("oidc pgstore: tenant mapper returned an invalid storage tenant")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("oidc pgstore: begin put: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := cleanupExpired(ctx, tx); err != nil {
		return fmt.Errorf("oidc pgstore: clean expired state: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, tenancy.SessionSetting, storageTenant.String()); err != nil {
		return fmt.Errorf("oidc pgstore: set tenant: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO oidc_pending_authorization (tenant_id,tenant_key,state,issuer_url,client_id,redirect_uri,nonce,code_challenge_digest,created_at,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, storageTenant.String(), p.Tenant.String(), p.State, p.IssuerURL, p.ClientID, p.RedirectURI, p.Nonce, p.CodeChallengeDigest, p.CreatedAt.UTC(), p.ExpiresAt.UTC())
	if err != nil {
		return fmt.Errorf("oidc pgstore: insert pending authorization: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oidc_pending_authorization_pointer (state,tenant_id,expires_at) VALUES ($1,$2,$3)`, p.State, storageTenant.String(), p.ExpiresAt.UTC()); err != nil {
		return fmt.Errorf("oidc pgstore: insert state pointer: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("oidc pgstore: commit put: %w", err)
	}
	return nil
}

func cleanupExpired(ctx context.Context, tx dbport.Tx) error {
	rows, err := tx.Query(ctx, `DELETE FROM oidc_pending_authorization_pointer WHERE state IN (SELECT state FROM oidc_pending_authorization_pointer WHERE expires_at <= now() ORDER BY expires_at LIMIT 100) RETURNING state,tenant_id::text`)
	if err != nil {
		return err
	}
	type expiredState struct{ state, tenant string }
	var expired []expiredState
	for rows.Next() {
		var row expiredState
		if err := rows.Scan(&row.state, &row.tenant); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, row := range expired {
		if _, err := tx.Exec(ctx, `SELECT set_config($1,$2,true)`, tenancy.SessionSetting, row.tenant); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM oidc_pending_authorization WHERE tenant_id=$1 AND state=$2`, row.tenant, row.state); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Take(ctx context.Context, state string) (oidc.PendingAuthorization, bool, error) {
	if s == nil || s.db == nil || state == "" {
		return oidc.PendingAuthorization{}, false, errors.New("oidc pgstore: invalid state lookup")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oidc.PendingAuthorization{}, false, fmt.Errorf("oidc pgstore: begin take: %w", err)
	}
	defer tx.Rollback(ctx)
	var tenant string
	err = tx.QueryRow(ctx, `DELETE FROM oidc_pending_authorization_pointer WHERE state=$1 RETURNING tenant_id::text`, state).Scan(&tenant)
	if errors.Is(err, dbport.ErrNoRows) {
		return oidc.PendingAuthorization{}, false, nil
	}
	if err != nil {
		return oidc.PendingAuthorization{}, false, fmt.Errorf("oidc pgstore: resolve state: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, tenancy.SessionSetting, tenant); err != nil {
		return oidc.PendingAuthorization{}, false, fmt.Errorf("oidc pgstore: set tenant: %w", err)
	}
	var p oidc.PendingAuthorization
	var tenantKey string
	err = tx.QueryRow(ctx, `DELETE FROM oidc_pending_authorization WHERE tenant_id=$1 AND state=$2 RETURNING tenant_key,issuer_url,client_id,redirect_uri,nonce,code_challenge_digest,created_at,expires_at`, tenant, state).Scan(&tenantKey, &p.IssuerURL, &p.ClientID, &p.RedirectURI, &p.Nonce, &p.CodeChallengeDigest, &p.CreatedAt, &p.ExpiresAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return oidc.PendingAuthorization{}, false, fmt.Errorf("oidc pgstore: state pointer has no pending authorization")
	}
	if err != nil {
		return oidc.PendingAuthorization{}, false, fmt.Errorf("oidc pgstore: consume authorization: %w", err)
	}
	p.Tenant = values.TenantId(tenantKey)
	p.State = state
	if err := tx.Commit(ctx); err != nil {
		return oidc.PendingAuthorization{}, false, fmt.Errorf("oidc pgstore: commit take: %w", err)
	}
	return p, true, nil
}

var _ oidc.StateStore = (*Store)(nil)
