// Package brandassetstore persists tenant scoped brand asset bytes and immutable revisions.
package brandassetstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"path"
	"strings"

	_ "golang.org/x/image/webp"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/brandasset"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalid  = errors.New("brandassetstore: invalid asset")
	ErrConflict = brandasset.ErrConflict
	ErrNotFound = errors.New("brandassetstore: asset not found")
)

type DB interface{ dbport.Beginner }

type Store struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
}

func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: database and canonical tenant mapper are required", ErrInvalid)
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

func (s *Store) Save(ctx context.Context, tenantText string, expected int, actor string, asset brandasset.Asset) (brandasset.Asset, error) {
	if err := validate(s, tenantText, actor); err != nil {
		return brandasset.Asset{}, err
	}
	if err := validateAsset(asset); err != nil {
		return brandasset.Asset{}, err
	}
	tenant := s.tenantUUID(values.TenantId(tenantText))
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return brandasset.Asset{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockTenant(ctx, tx, tenant); err != nil {
		return brandasset.Asset{}, err
	}
	current, found, err := currentTx(ctx, tx, tenant)
	if err != nil {
		return brandasset.Asset{}, err
	}
	version := 0
	if found {
		version = current.Revision
	}
	if expected != version {
		return brandasset.Asset{}, ErrConflict
	}
	asset.TenantID, asset.Revision = tenantText, version+1
	if err := insertRevision(ctx, tx, tenant, actor, asset); err != nil {
		return brandasset.Asset{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO brand_asset_head (tenant_id, revision) VALUES ($1,$2)
		ON CONFLICT (tenant_id) DO UPDATE SET revision=EXCLUDED.revision`, tenant, asset.Revision); err != nil {
		return brandasset.Asset{}, fmt.Errorf("brandassetstore: update head: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return brandasset.Asset{}, fmt.Errorf("brandassetstore: commit revision: %w", err)
	}
	return asset, nil
}

func (s *Store) Current(ctx context.Context, tenantText string) (brandasset.Asset, bool, error) {
	if err := validate(s, tenantText, "reader"); err != nil {
		return brandasset.Asset{}, false, err
	}
	tenant := s.tenantUUID(values.TenantId(tenantText))
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return brandasset.Asset{}, false, err
	}
	defer tx.Rollback(ctx)
	asset, found, err := currentTx(ctx, tx, tenant)
	if err != nil {
		return brandasset.Asset{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return brandasset.Asset{}, false, err
	}
	return asset, found, nil
}

func (s *Store) Read(ctx context.Context, tenantText, digest string) (brandasset.Asset, bool, error) {
	if err := validate(s, tenantText, "reader"); err != nil {
		return brandasset.Asset{}, false, err
	}
	if len(digest) != 64 {
		return brandasset.Asset{}, false, ErrInvalid
	}
	tenant := s.tenantUUID(values.TenantId(tenantText))
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return brandasset.Asset{}, false, err
	}
	defer tx.Rollback(ctx)
	asset, err := scanAsset(tx.QueryRow(ctx, `SELECT tenant_id,revision,filename,media_type,width,height,asset_digest,original_bytes,proxy_bytes,proxy_type,removed
		FROM brand_asset_revision WHERE tenant_id=$1 AND asset_digest=$2 ORDER BY revision DESC LIMIT 1`, tenant, digest))
	if errors.Is(err, dbport.ErrNoRows) {
		return brandasset.Asset{}, false, nil
	}
	if err != nil {
		return brandasset.Asset{}, false, err
	}
	if asset.Removed {
		return brandasset.Asset{}, false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return brandasset.Asset{}, false, err
	}
	return asset, true, nil
}

// History returns metadata for immutable tenant revisions newest first. Bytes
// remain available only through the tenant-scoped Read operation.
func (s *Store) History(ctx context.Context, tenantText string, before int) ([]brandasset.Asset, bool, error) {
	if err := validate(s, tenantText, "reader"); err != nil {
		return nil, false, err
	}
	if before < 0 {
		return nil, false, ErrInvalid
	}
	tenant := s.tenantUUID(values.TenantId(tenantText))
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT r.tenant_id,r.revision,r.filename,r.media_type,r.width,r.height,r.asset_digest,r.removed,
		(SELECT NOT latest.removed FROM brand_asset_revision latest WHERE latest.tenant_id=r.tenant_id AND latest.asset_digest=r.asset_digest ORDER BY latest.revision DESC LIMIT 1)
		FROM brand_asset_revision r WHERE r.tenant_id=$1 AND ($2::bigint=0 OR r.revision<$2) ORDER BY r.revision DESC LIMIT $3`, tenant, before, brandasset.HistoryPageSize+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	history := make([]brandasset.Asset, 0, 16)
	for rows.Next() {
		var asset brandasset.Asset
		var tenantID uuid.UUID
		if scanErr := rows.Scan(&tenantID, &asset.Revision, &asset.Name, &asset.MediaType, &asset.Width, &asset.Height, &asset.Digest, &asset.Removed, &asset.Available); scanErr != nil {
			return nil, false, scanErr
		}
		asset.TenantID = tenantID.String()
		history = append(history, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(history) > brandasset.HistoryPageSize
	if hasMore {
		history = history[:brandasset.HistoryPageSize]
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return history, hasMore, nil
}

func (s *Store) Rollback(ctx context.Context, tenantText string, expected, target int, actor string) (brandasset.Asset, error) {
	if err := validate(s, tenantText, actor); err != nil {
		return brandasset.Asset{}, err
	}
	tenant := s.tenantUUID(values.TenantId(tenantText))
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return brandasset.Asset{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockTenant(ctx, tx, tenant); err != nil {
		return brandasset.Asset{}, err
	}
	current, found, err := currentTx(ctx, tx, tenant)
	if err != nil {
		return brandasset.Asset{}, err
	}
	if !found || current.Revision != expected || target < 1 || target >= expected {
		return brandasset.Asset{}, ErrConflict
	}
	asset, err := scanAsset(tx.QueryRow(ctx, `SELECT tenant_id,revision,filename,media_type,width,height,asset_digest,original_bytes,proxy_bytes,proxy_type,removed
		FROM brand_asset_revision WHERE tenant_id=$1 AND revision=$2`, tenant, target))
	if err != nil || asset.Removed {
		return brandasset.Asset{}, ErrNotFound
	}
	asset.Revision = expected + 1
	if err := insertRevision(ctx, tx, tenant, actor, asset); err != nil {
		return brandasset.Asset{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE brand_asset_head SET revision=$2 WHERE tenant_id=$1`, tenant, asset.Revision); err != nil {
		return brandasset.Asset{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return brandasset.Asset{}, err
	}
	return asset, nil
}

func (s *Store) Remove(ctx context.Context, tenantText string, expected int, actor string) (brandasset.Asset, error) {
	if err := validate(s, tenantText, actor); err != nil {
		return brandasset.Asset{}, err
	}
	tenant := s.tenantUUID(values.TenantId(tenantText))
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return brandasset.Asset{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockTenant(ctx, tx, tenant); err != nil {
		return brandasset.Asset{}, err
	}
	current, found, err := currentTx(ctx, tx, tenant)
	if err != nil {
		return brandasset.Asset{}, err
	}
	if !found || current.Revision != expected {
		return brandasset.Asset{}, ErrConflict
	}
	current.Revision, current.Removed = expected+1, true
	if err := insertRevision(ctx, tx, tenant, actor, current); err != nil {
		return brandasset.Asset{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE brand_asset_head SET revision=$2 WHERE tenant_id=$1`, tenant, current.Revision); err != nil {
		return brandasset.Asset{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return brandasset.Asset{}, err
	}
	return current, nil
}

func (s *Store) begin(ctx context.Context, tenant uuid.UUID) (dbport.Tx, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("brandassetstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func validate(s *Store, tenantText, actor string) error {
	if s == nil || s.db == nil || s.tenantUUID == nil || strings.TrimSpace(actor) == "" || strings.TrimSpace(actor) != actor {
		return ErrInvalid
	}
	if strings.TrimSpace(tenantText) == "" || strings.TrimSpace(tenantText) != tenantText || s.tenantUUID(values.TenantId(tenantText)) == uuid.Nil {
		return ErrInvalid
	}
	return nil
}

func lockTenant(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) error {
	var locked uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT tenant_id FROM tenant WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&locked); err != nil {
		return fmt.Errorf("brandassetstore: lock tenant: %w", err)
	}
	return nil
}

func currentTx(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) (brandasset.Asset, bool, error) {
	asset, err := scanAsset(tx.QueryRow(ctx, `SELECT r.tenant_id,r.revision,r.filename,r.media_type,r.width,r.height,r.asset_digest,r.original_bytes,r.proxy_bytes,r.proxy_type,r.removed
		FROM brand_asset_head h JOIN brand_asset_revision r USING (tenant_id,revision) WHERE h.tenant_id=$1`, tenant))
	if errors.Is(err, dbport.ErrNoRows) {
		return brandasset.Asset{}, false, nil
	}
	return asset, err == nil, err
}

func insertRevision(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, actor string, asset brandasset.Asset) error {
	_, err := tx.Exec(ctx, `INSERT INTO brand_asset_revision
		(tenant_id,revision,asset_digest,filename,media_type,width,height,original_bytes,proxy_bytes,proxy_type,removed,actor_id,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,now())`, tenant, asset.Revision, asset.Digest, asset.Name, asset.MediaType,
		asset.Width, asset.Height, asset.Original, asset.Proxy, asset.ProxyType, asset.Removed, actor)
	if err != nil {
		return fmt.Errorf("brandassetstore: insert revision: %w", err)
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanAsset(row rowScanner) (brandasset.Asset, error) {
	var asset brandasset.Asset
	var tenant uuid.UUID
	err := row.Scan(&tenant, &asset.Revision, &asset.Name, &asset.MediaType, &asset.Width, &asset.Height, &asset.Digest, &asset.Original, &asset.Proxy, &asset.ProxyType, &asset.Removed)
	asset.TenantID = tenant.String()
	if err == nil && !asset.Removed {
		err = validateAsset(asset)
	}
	return asset, err
}

func validateAsset(asset brandasset.Asset) error {
	if asset.Name == "" || asset.Width < 16 || asset.Height < 16 || asset.Width > 1024 || asset.Height > 1024 ||
		len(asset.Original) == 0 || len(asset.Original) > 2<<20 || len(asset.Proxy) == 0 || len(asset.Proxy) > 2<<20 ||
		(asset.MediaType != "image/png" && asset.MediaType != "image/jpeg" && asset.MediaType != "image/webp") ||
		asset.ProxyType != "image/jpeg" {
		return ErrInvalid
	}
	digest := sha256.Sum256(asset.Original)
	if hex.EncodeToString(digest[:]) != asset.Digest {
		return fmt.Errorf("%w: original digest mismatch", ErrInvalid)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(asset.Original))
	wantFormat := strings.TrimPrefix(strings.ToLower(path.Ext(asset.Name)), ".")
	if wantFormat == "jpg" {
		wantFormat = "jpeg"
	}
	wantMedia := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	if err != nil || format != wantFormat || asset.MediaType != wantMedia || config.Width != asset.Width || config.Height != asset.Height {
		return fmt.Errorf("%w: original media metadata mismatch", ErrInvalid)
	}
	proxyConfig, proxyFormat, err := image.DecodeConfig(bytes.NewReader(asset.Proxy))
	if err != nil || proxyFormat != "jpeg" || proxyConfig.Width < 1 || proxyConfig.Height < 1 || proxyConfig.Width > 512 || proxyConfig.Height > 512 {
		return fmt.Errorf("%w: proxy is not a bounded JPEG", ErrInvalid)
	}
	return nil
}
