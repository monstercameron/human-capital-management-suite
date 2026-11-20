package chatstore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3929: durable, person-scoped saved searches. Every statement runs in
// the workspace tenant transaction and names the owner, so a row is never
// read, changed or removed through another person request. The table holds
// only a name and the query text; no result is stored.

var _ chatsearch.SavedStore = (*Store)(nil)

func issue3929Actor(a chatsearch.Actor) error {
	if a.TenantID == "" || a.HomeTenantID == "" || a.PersonID == "" {
		return chatsearch.ErrInvalid
	}
	return nil
}

func (s *Store) ListSaved(ctx context.Context, a chatsearch.Actor) ([]chatsearch.SavedSearch, error) {
	if err := issue3929Actor(a); err != nil {
		return nil, err
	}
	out := []chatsearch.SavedSearch{}
	err := s.RunTenantTx(ctx, a.TenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,name,query,updated_at FROM chat_saved_search WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 ORDER BY lower(name),id LIMIT 200`, a.TenantID, a.HomeTenantID, a.PersonID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v chatsearch.SavedSearch
			if err := rows.Scan(&v.ID, &v.Name, &v.Query, &v.UpdatedAt); err != nil {
				return err
			}
			v.UpdatedAt = v.UpdatedAt.UTC()
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// CreateSaved keeps a new shortcut unless its owner already has limit of them
// or already uses the name (case aside). The checks and the insert are one
// transaction; the unique index backs the name check against a race.
func (s *Store) CreateSaved(ctx context.Context, a chatsearch.Actor, v chatsearch.SavedSearch, limit int) (chatsearch.SavedSearch, error) {
	if err := issue3929Actor(a); err != nil {
		return chatsearch.SavedSearch{}, err
	}
	if v.Name == "" || v.Query == "" {
		return chatsearch.SavedSearch{}, chatsearch.ErrInvalid
	}
	v.ID = uuid.NewString()
	err := s.RunTenantTx(ctx, a.TenantID, func(tx dbport.Tx) error {
		var held, same int
		if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE lower(name)=lower($4)) FROM chat_saved_search WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3`, a.TenantID, a.HomeTenantID, a.PersonID, v.Name).Scan(&held, &same); err != nil {
			return err
		}
		if same > 0 {
			return chatsearch.ErrSavedNameUsed
		}
		if limit > 0 && held >= limit {
			return chatsearch.ErrSavedLimit
		}
		return tx.QueryRow(ctx, `INSERT INTO chat_saved_search(tenant_id,id,home_tenant_id,person_id,name,query) VALUES($1,$2,$3,$4,$5,$6) RETURNING updated_at`,
			a.TenantID, v.ID, a.HomeTenantID, a.PersonID, v.Name, v.Query).Scan(&v.UpdatedAt)
	})
	v.UpdatedAt = v.UpdatedAt.UTC()
	if err != nil {
		return chatsearch.SavedSearch{}, err
	}
	return v, nil
}

// UpdateSaved renames or edits one shortcut of the owner. Another person row
// and a missing row are the same answer: not found.
func (s *Store) UpdateSaved(ctx context.Context, a chatsearch.Actor, v chatsearch.SavedSearch) (chatsearch.SavedSearch, error) {
	if err := issue3929Actor(a); err != nil {
		return chatsearch.SavedSearch{}, err
	}
	if v.ID == "" || v.Name == "" || v.Query == "" {
		return chatsearch.SavedSearch{}, chatsearch.ErrInvalid
	}
	err := s.RunTenantTx(ctx, a.TenantID, func(tx dbport.Tx) error {
		var taken int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_saved_search WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND lower(name)=lower($4) AND id<>$5`, a.TenantID, a.HomeTenantID, a.PersonID, v.Name, v.ID).Scan(&taken); err != nil {
			return err
		}
		if taken > 0 {
			return chatsearch.ErrSavedNameUsed
		}
		err := tx.QueryRow(ctx, `UPDATE chat_saved_search SET name=$5,query=$6,updated_at=now() WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND id=$4 RETURNING updated_at`,
			a.TenantID, a.HomeTenantID, a.PersonID, v.ID, v.Name, v.Query).Scan(&v.UpdatedAt)
		if errors.Is(err, dbport.ErrNoRows) {
			return chatsearch.ErrSavedNotFound
		}
		return err
	})
	v.UpdatedAt = v.UpdatedAt.UTC()
	if err != nil {
		return chatsearch.SavedSearch{}, err
	}
	return v, nil
}

func (s *Store) DeleteSaved(ctx context.Context, a chatsearch.Actor, id string) error {
	if err := issue3929Actor(a); err != nil {
		return err
	}
	if id == "" {
		return chatsearch.ErrInvalid
	}
	return s.RunTenantTx(ctx, a.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `DELETE FROM chat_saved_search WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND id=$4`, a.TenantID, a.HomeTenantID, a.PersonID, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return chatsearch.ErrSavedNotFound
		}
		return nil
	})
}
