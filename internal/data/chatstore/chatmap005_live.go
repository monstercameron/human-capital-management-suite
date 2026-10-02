package chatstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// UpdateLivePosition replaces the one stored position of a live share. The
// statement itself refuses a share that has ended or run out, so the end time is
// the database row's and the device cannot extend it.
func (s *Store) UpdateLivePosition(ctx context.Context, k chat.LocationKey, place chat.LocationPlace, now time.Time) error {
	payload, err := json.Marshal(place)
	if err != nil {
		return err
	}
	return s.RunTenantTx(ctx, k.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chat_location_share SET place=$5,position_updated_at=$6,revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND id=$4 AND live AND NOT ended AND (expires_at IS NULL OR expires_at>$6)`, k.TenantID, k.ConversationID, k.PostID, k.ID, payload, now)
		if err != nil {
			return err
		}
		if n == 0 {
			return chat.ErrNotFound
		}
		return nil
	})
}

// EndLocationReason ends a share and records why. The position is deleted.
func (s *Store) EndLocationReason(ctx context.Context, k chat.LocationKey, reason string) error {
	return s.RunTenantTx(ctx, k.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chat_location_share SET place=NULL,ended=true,ended_reason=$5,revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND id=$4 AND NOT ended`, k.TenantID, k.ConversationID, k.PostID, k.ID, reason)
		if err == nil && n == 0 {
			// Already ended: ending twice is not an error, a missing row is.
			var found bool
			if e := tx.QueryRow(ctx, `SELECT true FROM chat_location_share WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND id=$4`, k.TenantID, k.ConversationID, k.PostID, k.ID).Scan(&found); e != nil {
				return chat.ErrNotFound
			}
		}
		return err
	})
}

// EndLiveBySharer ends one person's live shares in a conversation, or in all of
// them when conversation is empty. The person is always the caller's own.
func (s *Store) EndLiveBySharer(ctx context.Context, tenant, conversation, subject, subjectTenant, reason string) (int64, error) {
	var count int64
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chat_location_share SET place=NULL,ended=true,ended_reason=$5,revision=revision+1 WHERE tenant_id=$1 AND sharer_id=$2 AND sharer_tenant_id=$3 AND ($4='' OR conversation_id=$4) AND live AND NOT ended`, tenant, subject, subjectTenant, conversation, reason)
		count = n
		return err
	})
	return count, err
}

// SharerLocationKeys lists the shares one person has made that still exist.
func (s *Store) SharerLocationKeys(ctx context.Context, tenant, subject, subjectTenant string) ([]chat.LocationKey, error) {
	keys := []chat.LocationKey{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT conversation_id,post_id,id FROM chat_location_share WHERE tenant_id=$1 AND sharer_id=$2 AND sharer_tenant_id=$3 AND NOT ended ORDER BY shared_at DESC LIMIT 200`, tenant, subject, subjectTenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			k := chat.LocationKey{TenantID: tenant}
			if err = rows.Scan(&k.ConversationID, &k.PostID, &k.ID); err != nil {
				return err
			}
			keys = append(keys, k)
		}
		return rows.Err()
	})
	return keys, err
}

func policyFromRow(sharing, live, exact bool, maxLive, maxRetention int) chat.LocationPolicy {
	return chat.LocationPolicy{SharingEnabled: sharing, LiveEnabled: live, ExactAllowed: exact, MaxLive: time.Duration(maxLive) * time.Second, MaxRetention: time.Duration(maxRetention) * time.Second}
}

// ReadLocationPolicy returns the workspace settings (defaults when none were
// stored) and the channel's own settings when it has any.
func (s *Store) ReadLocationPolicy(ctx context.Context, tenant, conversation string) (chat.LocationPolicy, *chat.LocationPolicy, error) {
	workspace := chat.DefaultLocationPolicy()
	var channel *chat.LocationPolicy
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var sharing, live, exact bool
		var maxLive, maxRetention int
		err := tx.QueryRow(ctx, `SELECT sharing_enabled,live_enabled,exact_allowed,max_live_seconds,max_retention_seconds FROM chat_location_policy WHERE tenant_id=$1 AND conversation_id=''`, tenant).Scan(&sharing, &live, &exact, &maxLive, &maxRetention)
		if err == nil {
			workspace = policyFromRow(sharing, live, exact, maxLive, maxRetention)
		} else if err != dbport.ErrNoRows {
			return err
		}
		if conversation == "" {
			return nil
		}
		err = tx.QueryRow(ctx, `SELECT sharing_enabled,live_enabled,exact_allowed,max_live_seconds,max_retention_seconds FROM chat_location_policy WHERE tenant_id=$1 AND conversation_id=$2`, tenant, conversation).Scan(&sharing, &live, &exact, &maxLive, &maxRetention)
		if err == nil {
			p := policyFromRow(sharing, live, exact, maxLive, maxRetention)
			channel = &p
		} else if err != dbport.ErrNoRows {
			return err
		}
		return nil
	})
	return workspace, channel, err
}

func (s *Store) WriteLocationPolicy(ctx context.Context, tenant, conversation string, p chat.LocationPolicy, by string) error {
	if !p.Valid() {
		return chat.ErrInvalidArgument
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_location_policy(tenant_id,conversation_id,sharing_enabled,live_enabled,exact_allowed,max_live_seconds,max_retention_seconds,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()) ON CONFLICT (tenant_id,conversation_id) DO UPDATE SET sharing_enabled=$3,live_enabled=$4,exact_allowed=$5,max_live_seconds=$6,max_retention_seconds=$7,updated_by=$8,updated_at=now()`, tenant, conversation, p.SharingEnabled, p.LiveEnabled, p.ExactAllowed, int(p.MaxLive/time.Second), int(p.MaxRetention/time.Second), by)
		return err
	})
}

func (s *Store) ReadLocationJurisdiction(ctx context.Context, tenant, country string) (chat.LocationJurisdiction, bool, error) {
	j := chat.LocationJurisdiction{Country: country}
	found := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT enabled,basis FROM chat_location_jurisdiction WHERE tenant_id=$1 AND country=$2`, tenant, country).Scan(&j.Enabled, &j.Basis)
		if err == dbport.ErrNoRows {
			return nil
		}
		found = err == nil
		return err
	})
	return j, found, err
}

func (s *Store) WriteLocationJurisdiction(ctx context.Context, tenant string, j chat.LocationJurisdiction, by string) error {
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_location_jurisdiction(tenant_id,country,enabled,basis,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,now()) ON CONFLICT (tenant_id,country) DO UPDATE SET enabled=$3,basis=$4,updated_by=$5,updated_at=now()`, tenant, j.Country, j.Enabled, j.Basis, by)
		return err
	})
}

// IsLocationAdmin answers for a channel: its owner or a manager. The workspace
// scope (empty conversation) is not known to Chat's own tables, so it fails
// closed until the deployment supplies an administrator port.
func (s *Store) IsLocationAdmin(ctx context.Context, p chat.Principal, tenant, conversation string) bool {
	if conversation == "" || p.TenantID != tenant {
		return false
	}
	admin := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_conversation c WHERE c.tenant_id=$1 AND c.id=$2 AND c.owner_id=$3) OR EXISTS(SELECT 1 FROM chat_membership m WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.member_id=$3 AND m.home_tenant_id=$4 AND m.state='active' AND m.left_at IS NULL AND upper(m.role)='MANAGER')`, tenant, conversation, p.SubjectID, p.TenantID).Scan(&admin)
	})
	return err == nil && admin
}
