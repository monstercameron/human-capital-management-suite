package agentstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var ErrBackupInvalid = errors.New("agentstore: invalid backup")
var ErrRestoreOccupied = errors.New("agentstore: restore destination is occupied")

// BackupImage is a tenant-scoped, consistent image of every agent-owned
// relation, including retained inbox/outbox, checkpoints and dedupe receipts.
// It contains sensitive agent data and belongs in deployment-managed storage.
type BackupImage struct {
	TenantID   uuid.UUID     `json:"tenant_id"`
	CapturedAt time.Time     `json:"captured_at"`
	Migration  int64         `json:"migration"`
	Tables     []BackupTable `json:"tables"`
	Digest     string        `json:"digest"`
}
type BackupTable struct {
	Name    string            `json:"name"`
	Columns string            `json:"columns"`
	Rows    []json.RawMessage `json:"rows"`
}
type RestoreEvidence struct {
	TenantID       uuid.UUID
	Rows           int
	SnapshotDigest string
	RPO            time.Duration
	RTO            time.Duration
}

type RestoredInstallation struct {
	ID          string
	Revision    int64
	PrincipalID uuid.UUID
	Published   bool
}

// InactiveRestoredInstallationIDs repairs the crash window between an older
// installation suspension and its security-scope revocation.
func (s *Store) InactiveRestoredInstallationIDs(ctx context.Context, tenant uuid.UUID) ([]string, error) {
	var out []string
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, "SELECT installation_id FROM persona_installations WHERE tenant_id=$1 AND state<>'ACTIVE' ORDER BY installation_id", tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, err
}

// RestoredRunIDs includes an accepted inbox whose execution insert was lost,
// as well as all unfinished executions. Terminal owner receipts stay retained.
func (s *Store) RestoredRunIDs(ctx context.Context, tenant uuid.UUID, limit int) ([]string, error) {
	if limit < 1 || limit > 1000 {
		return nil, ErrBackupInvalid
	}
	var out []string
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT a.request_id FROM agent_run_request a LEFT JOIN agent_run_execution e ON e.tenant_id=a.tenant_id AND e.admission_id=a.request_id WHERE a.tenant_id=$1 AND a.decision='ACCEPTED' AND (e.run_id IS NULL OR e.state IN ('READY','RUNNING','WAITING','RECONCILING')) ORDER BY a.admitted_at,a.request_id LIMIT $2`, tenant, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, err
}

// RestoredInstallations returns the exact active placement and version
// projection; current principal lifecycle remains with the core trust owner.
func (s *Store) RestoredInstallations(ctx context.Context, tenant uuid.UUID) ([]RestoredInstallation, error) {
	var out []RestoredInstallation
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT i.installation_id,i.revision,b.principal_id,
   COALESCE((SELECT e.to_state='PUBLISHED' AND e.profile_digest=v.content_digest AND e.review_digest<>'' AND e.evaluation_digest<>'' AND e.evaluation_profile_digest=v.content_digest FROM persona_lifecycle_events e WHERE e.tenant_id=i.tenant_id AND e.persona_id=i.persona_id AND e.persona_version=i.persona_version ORDER BY event_sequence DESC LIMIT 1),false)
   FROM persona_installations i LEFT JOIN persona_versions v ON v.tenant_id=i.tenant_id AND v.persona_id=i.persona_id AND v.version=i.persona_version
   LEFT JOIN persona_agent_principal_binding b ON b.tenant_id=i.tenant_id AND b.persona_id=i.persona_id AND b.persona_version=i.persona_version
   WHERE i.tenant_id=$1 AND i.state='ACTIVE' ORDER BY i.installation_id`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row RestoredInstallation
			var principal *uuid.UUID
			if err := rows.Scan(&row.ID, &row.Revision, &principal, &row.Published); err != nil {
				return err
			}
			if principal != nil {
				row.PrincipalID = *principal
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}

// SuspendRestoredInstallation fences recovered authority in the same
// transaction as the installation suspension. Replays do not bump twice.
func (s *Store) SuspendRestoredInstallation(ctx context.Context, tenant uuid.UUID, item RestoredInstallation, reason string) error {
	if item.ID == "" || item.Revision < 1 || strings.TrimSpace(reason) == "" {
		return ErrBackupInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		scope := PersonaSecurityScope{Kind: "INSTALLATION", Key: item.ID}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, personaScopeLockKey(tenant, scope)); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `UPDATE persona_installations SET state='SUSPENDED',suspension_reason=$4,revision=revision+1,revocation_epoch=revocation_epoch+1,updated_at=now() WHERE tenant_id=$1 AND installation_id=$2 AND revision=$3 AND state='ACTIVE'`, tenant, item.ID, item.Revision, reason)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		n, err = tx.Exec(ctx, `INSERT INTO persona_security_scope(tenant_id,scope_kind,scope_key,epoch,state,reason,revoked_at) VALUES($1,'INSTALLATION',$2,2,'REVOKED',$3,now()) ON CONFLICT(tenant_id,scope_kind,scope_key) DO UPDATE SET epoch=persona_security_scope.epoch+1,state='REVOKED',reason=EXCLUDED.reason,revoked_at=EXCLUDED.revoked_at WHERE persona_security_scope.state='ACTIVE'`, tenant, item.ID, reason)
		if err != nil || n == 0 {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO persona_security_scope_event(tenant_id,event_id,scope_kind,scope_key,epoch,state,reason,occurred_at) SELECT tenant_id,$3,scope_kind,scope_key,epoch,state,reason,revoked_at FROM persona_security_scope WHERE tenant_id=$1 AND scope_kind='INSTALLATION' AND scope_key=$2`, tenant, item.ID, uuid.NewString())
		return err
	})
}

// CaptureBackup uses the deployment backup identity, never the serving role.
// Repeatable read gives all relations the same committed snapshot.
func CaptureBackup(ctx context.Context, db *sql.DB, tenant uuid.UUID, now time.Time) (BackupImage, error) {
	if db == nil || tenant == uuid.Nil || now.IsZero() {
		return BackupImage{}, ErrBackupInvalid
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return BackupImage{}, err
	}
	defer tx.Rollback()
	tables, err := backupCatalog(ctx, tx)
	if err != nil {
		return BackupImage{}, err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tenant WHERE tenant_id=$1)", tenant).Scan(&exists); err != nil {
		return BackupImage{}, err
	}
	if !exists {
		return BackupImage{}, ErrBackupInvalid
	}
	image := BackupImage{TenantID: tenant, CapturedAt: now.UTC(), Tables: tables}
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(max(version_id),0) FROM goose_db_version WHERE is_applied").Scan(&image.Migration); err != nil {
		return BackupImage{}, err
	}
	for i := range image.Tables {
		table := &image.Tables[i]
		rows, err := tx.QueryContext(ctx, "SELECT to_jsonb(t) FROM "+pgx.Identifier{table.Name}.Sanitize()+" t WHERE tenant_id=$1 ORDER BY to_jsonb(t)::text", tenant)
		if err != nil {
			return BackupImage{}, err
		}
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return BackupImage{}, err
			}
			table.Rows = append(table.Rows, json.RawMessage(raw))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return BackupImage{}, err
		}
	}
	image.Digest, err = backupDigest(image)
	if err != nil {
		return BackupImage{}, err
	}
	if err = tx.Commit(); err != nil {
		return BackupImage{}, err
	}
	return image, nil
}

// RestoreBackup imports into a freshly migrated empty agent destination.
// It never deletes or overwrites existing data and never connects to core,
// chat or workflow. Current owner authority must be reconciled before serving.
func RestoreBackup(ctx context.Context, db *sql.DB, expectedTenant uuid.UUID, image BackupImage, now time.Time, maxRPO time.Duration) (RestoreEvidence, error) {
	started := time.Now()
	if db == nil || expectedTenant == uuid.Nil || image.TenantID != expectedTenant || now.IsZero() || image.CapturedAt.IsZero() || image.CapturedAt.After(now) || maxRPO < 0 || now.Sub(image.CapturedAt) > maxRPO {
		return RestoreEvidence{}, ErrBackupInvalid
	}
	digest, err := backupDigest(image)
	if err != nil || digest != image.Digest {
		return RestoreEvidence{}, ErrBackupInvalid
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RestoreEvidence{}, err
	}
	defer tx.Rollback()
	catalog, err := backupCatalog(ctx, tx)
	if err != nil {
		return RestoreEvidence{}, err
	}
	var version int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(max(version_id),0) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil {
		return RestoreEvidence{}, err
	}
	if version != image.Migration || len(catalog) != len(image.Tables) {
		return RestoreEvidence{}, ErrBackupInvalid
	}
	tables := map[string]BackupTable{}
	for i, table := range image.Tables {
		if table.Name != catalog[i].Name || !compatibleBackupColumns(table, catalog[i]) {
			return RestoreEvidence{}, ErrBackupInvalid
		}
		if _, err := tx.ExecContext(ctx, "LOCK TABLE "+pgx.Identifier{table.Name}.Sanitize()+" IN SHARE ROW EXCLUSIVE MODE"); err != nil {
			return RestoreEvidence{}, err
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+pgx.Identifier{table.Name}.Sanitize()+")").Scan(&exists); err != nil {
			return RestoreEvidence{}, err
		}
		if exists {
			return RestoreEvidence{}, ErrRestoreOccupied
		}
		for _, raw := range table.Rows {
			var row map[string]json.RawMessage
			if json.Unmarshal(raw, &row) != nil {
				return RestoreEvidence{}, ErrBackupInvalid
			}
			var scoped uuid.UUID
			if json.Unmarshal(row["tenant_id"], &scoped) != nil || scoped != expectedTenant {
				return RestoreEvidence{}, ErrBackupInvalid
			}
		}
		tables[table.Name] = table
	}
	order, err := backupDependencyOrder(ctx, tx, catalog)
	if err != nil {
		return RestoreEvidence{}, err
	}
	if len(tables["tenant"].Rows) != 1 {
		return RestoreEvidence{}, ErrBackupInvalid
	}
	evidence := RestoreEvidence{TenantID: expectedTenant, SnapshotDigest: digest, RPO: now.Sub(image.CapturedAt)}
	for _, name := range order {
		identifier := pgx.Identifier{name}.Sanitize()
		for _, row := range tables[name].Rows {
			if _, err = tx.ExecContext(ctx, "INSERT INTO "+identifier+" SELECT * FROM jsonb_populate_record(NULL::"+identifier+",$1::jsonb)", string(row)); err != nil {
				return RestoreEvidence{}, fmt.Errorf("restore agent relation %s: %w", name, err)
			}
			evidence.Rows++
		}
	}
	// Explicit serial values require the destination's owned sequences to move
	// past the restored maximum, otherwise the next lifecycle append collides.
	rows, err := tx.QueryContext(ctx, `SELECT table_name,column_name FROM information_schema.columns WHERE table_schema=current_schema() AND column_default LIKE 'nextval(%' ORDER BY table_name,column_name`)
	if err != nil {
		return RestoreEvidence{}, err
	}
	var sequences [][2]string
	for rows.Next() {
		var pair [2]string
		if err = rows.Scan(&pair[0], &pair[1]); err != nil {
			rows.Close()
			return RestoreEvidence{}, err
		}
		sequences = append(sequences, pair)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return RestoreEvidence{}, err
	}
	for _, pair := range sequences {
		if _, ok := tables[pair[0]]; !ok {
			continue
		}
		table, column := pgx.Identifier{pair[0]}.Sanitize(), pgx.Identifier{pair[1]}.Sanitize()
		if _, err = tx.ExecContext(ctx, "SELECT setval(pg_get_serial_sequence($1,$2),COALESCE((SELECT max("+column+") FROM "+table+"),1),(SELECT count(*)>0 FROM "+table+"))", pair[0], pair[1]); err != nil {
			return RestoreEvidence{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return RestoreEvidence{}, err
	}
	evidence.RTO = time.Since(started)
	return evidence, nil
}

func backupDigest(image BackupImage) (string, error) {
	image.Digest = ""
	raw, err := json.Marshal(image)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func compatibleBackupColumns(source, target BackupTable) bool {
	if source.Columns == target.Columns {
		return true
	}
	// The retained development schema predates this exact column. No policy
	// facts may be inferred when importing that historical shape.
	if source.Name != "persona_model_route_policy" || len(source.Rows) != 0 {
		return false
	}
	var old, current map[string]string
	if json.Unmarshal([]byte(source.Columns), &old) != nil || json.Unmarshal([]byte(target.Columns), &current) != nil || old == nil || current == nil || current["policy_schema_version"] != "integer" {
		return false
	}
	if _, exists := old["policy_schema_version"]; exists || len(current) != len(old)+1 {
		return false
	}
	for name, datatype := range old {
		if actual, exists := current[name]; !exists || actual != datatype {
			return false
		}
	}
	return true
}

func backupCatalog(ctx context.Context, tx *sql.Tx) ([]BackupTable, error) {
	var agent, foreign bool
	if err := tx.QueryRowContext(ctx, "SELECT to_regclass('agent_definition_version') IS NOT NULL,to_regclass('worker_state') IS NOT NULL OR to_regclass('chat_post') IS NOT NULL").Scan(&agent, &foreign); err != nil {
		return nil, err
	}
	if !agent || foreign {
		return nil, ErrSharedDatabase
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.relname,jsonb_object_agg(a.attname,format_type(a.atttypid,a.atttypmod))::text
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped
 WHERE n.nspname=current_schema() AND c.relkind='r' AND EXISTS(SELECT 1 FROM pg_attribute t WHERE t.attrelid=c.oid AND t.attname='tenant_id' AND NOT t.attisdropped)
 GROUP BY c.relname ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []BackupTable
	for rows.Next() {
		var table BackupTable
		if err = rows.Scan(&table.Name, &table.Columns); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, ErrBackupInvalid
	}
	return tables, nil
}

func backupDependencyOrder(ctx context.Context, tx *sql.Tx, tables []BackupTable) ([]string, error) {
	pending := map[string]bool{}
	parents := map[string][]string{}
	for _, t := range tables {
		pending[t.Name] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT child.relname,parent.relname FROM pg_constraint fk JOIN pg_class child ON child.oid=fk.conrelid JOIN pg_class parent ON parent.oid=fk.confrelid JOIN pg_namespace n ON n.oid=child.relnamespace WHERE fk.contype='f' AND n.nspname=current_schema() AND child.oid<>parent.oid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var child, parent string
		if err = rows.Scan(&child, &parent); err != nil {
			return nil, err
		}
		if pending[child] && pending[parent] {
			parents[child] = append(parents[child], parent)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var out []string
	for len(pending) > 0 {
		var ready []string
		for child := range pending {
			blocked := false
			for _, parent := range parents[child] {
				if pending[parent] {
					blocked = true
				}
			}
			if !blocked {
				ready = append(ready, child)
			}
		}
		sort.Strings(ready)
		if len(ready) == 0 {
			return nil, fmt.Errorf("%w: cyclic restore dependencies %s", ErrBackupInvalid, strings.Join(out, ","))
		}
		for _, name := range ready {
			out = append(out, name)
			delete(pending, name)
		}
	}
	return out, nil
}
