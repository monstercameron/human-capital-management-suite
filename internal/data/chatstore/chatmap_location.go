package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func (s *Store) AttachLocation(ctx context.Context, v chat.LocationShare) (chat.LocationShare, error) {
	if v.Version != chat.LocationVersion || v.ID == "" || v.PostRevision == 0 {
		return chat.LocationShare{}, chat.ErrInvalidArgument
	}
	if v.SharedAt.IsZero() {
		v.SharedAt = time.Now().UTC()
	}
	v.Classification = chat.LocationClassification
	if v.SharerKind == "" {
		v.SharerKind = "human"
	}
	place, err := chat.PrepareLocation(v.Place, time.Now().UTC())
	if err != nil {
		return chat.LocationShare{}, err
	}
	// Service has already coarsened; approximate input is safe to coarsen again.
	if place.Source == chat.LocationJobSite {
		place.Position = nil
		place.Label = ""
		place.Address = ""
	}
	payload, err := json.Marshal(place)
	if err != nil {
		return chat.LocationShare{}, err
	}
	err = s.RunTenantTx(ctx, v.TenantID, func(tx dbport.Tx) error {
		if err := fenceContextWrite(ctx, tx, v.TenantID, v.ConversationID); err != nil {
			return err
		}
		var rev uint64
		var author, home string
		var removed bool
		if err := tx.QueryRow(ctx, `SELECT revision,author_id,author_home_tenant_id,tombstoned FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 FOR UPDATE`, v.TenantID, v.ConversationID, v.PostID).Scan(&rev, &author, &home, &removed); err != nil {
			return chat.ErrNotFound
		}
		if removed || author != v.SharerID || (home != "" && home != v.SharerTenantID) {
			return chat.ErrPermissionDenied
		}
		if rev != v.PostRevision {
			return chat.ErrConflict
		}
		var positionAt *time.Time
		if v.Live {
			positionAt = &v.SharedAt
		}
		inserted, err := tx.Exec(ctx, `INSERT INTO chat_location_share(tenant_id,conversation_id,post_id,id,post_revision,sharer_id,sharer_tenant_id,place,expires_at,shared_at,sharer_kind,live,live_interval_seconds,position_updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, v.TenantID, v.ConversationID, v.PostID, v.ID, v.PostRevision, v.SharerID, v.SharerTenantID, payload, v.ExpiresAt, v.SharedAt, v.SharerKind, v.Live, int(v.LiveInterval/time.Second), positionAt)
		if err != nil {
			return err
		}
		if inserted == 0 {
			var matches bool
			if err := tx.QueryRow(ctx, `SELECT place=$5::jsonb AND expires_at IS NOT DISTINCT FROM $6::timestamptz AND NOT ended FROM chat_location_share WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND id=$4`, v.TenantID, v.ConversationID, v.PostID, v.ID, payload, v.ExpiresAt).Scan(&matches); err != nil {
				return chat.ErrConflict
			}
			if !matches {
				return chat.ErrConflict
			}
			return nil
		}
		ref, _ := json.Marshal([]chat.Reference{{Kind: chat.LocationReference, TenantID: v.TenantID, ConversationID: v.ConversationID, ID: v.ID}})
		var x Post
		err = tx.QueryRow(ctx, `UPDATE chat_post SET references_json=(CASE WHEN jsonb_typeof(references_json)='array' THEN references_json ELSE '[]'::jsonb END) || $4::jsonb,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 RETURNING id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,revision,tombstoned,created_at,parent_id,references_json,source_attribution`, v.TenantID, v.ConversationID, v.PostID, ref).Scan(&x.ID, &x.TenantID, &x.ConversationID, &x.AuthorID, &x.AuthorHomeTenantID, &x.Sequence, &x.Body, &x.Revision, &x.Tombstoned, &x.CreatedAt, &x.ParentID, &x.References, &x.SourceAttribution)
		if err != nil {
			return err
		}
		post, err := chatPost(x)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body,parent_id,references_json,source_attribution,tombstoned) VALUES($1,$2,$3,$4,$5,$6,$7,$8,false)`, v.TenantID, v.PostID, x.Revision, x.AuthorID, x.Body, x.ParentID, x.References, x.SourceAttribution); err != nil {
			return err
		}
		if err = RecordRenderingRevisionTx(ctx, tx, v.TenantID, v.PostID, uint64(x.Revision), nil); err != nil {
			return err
		}
		var policyRevision, eventSequence int64
		if err = tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING settings_revision,event_sequence`, v.TenantID, v.ConversationID).Scan(&policyRevision, &eventSequence); err != nil {
			return err
		}
		return writeOutbox(ctx, tx, outboxWrite{TenantID: v.TenantID, ConversationID: v.ConversationID, AggregateID: v.PostID, EventType: "post.edited", ActorHomeTenantID: v.SharerTenantID, ActorID: v.SharerID, TargetID: v.PostID, RecordID: "post:" + v.PostID, RecordKind: "EDIT", SourceID: v.PostID, Revision: post.Revision, PolicyRevision: policyRevision, EventSequence: eventSequence, Value: post})
	})
	v.Place = place
	return v, err
}
func (s *Store) ReadLocation(ctx context.Context, k chat.LocationKey, now time.Time) (chat.LocationShare, error) {
	var v chat.LocationShare
	err := s.RunTenantTx(ctx, k.TenantID, func(tx dbport.Tx) error {
		// Deletion is committed with the read, including the first read after expiry.
		if _, err := tx.Exec(ctx, `UPDATE chat_location_share SET place=NULL,ended=true,ended_at=expires_at,ended_reason='expired',revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND id=$4 AND NOT ended AND expires_at<=$5`, k.TenantID, k.ConversationID, k.PostID, k.ID, now); err != nil {
			return err
		}
		var b []byte
		var interval int
		err := tx.QueryRow(ctx, `SELECT id,tenant_id,conversation_id,post_id,post_revision,sharer_id,sharer_tenant_id,place,expires_at,ended,shared_at,sharer_kind,live,live_interval_seconds,position_updated_at,ended_at,ended_reason FROM chat_location_share WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND id=$4`, k.TenantID, k.ConversationID, k.PostID, k.ID).Scan(&v.ID, &v.TenantID, &v.ConversationID, &v.PostID, &v.PostRevision, &v.SharerID, &v.SharerTenantID, &b, &v.ExpiresAt, &v.Ended, &v.SharedAt, &v.SharerKind, &v.Live, &interval, &v.PositionAt, &v.EndedAt, &v.EndedReason)
		v.LiveInterval = time.Duration(interval) * time.Second
		if err == dbport.ErrNoRows {
			return chat.ErrNotFound
		}
		if err != nil {
			return err
		}
		v.Version = chat.LocationVersion
		v.Classification = chat.LocationClassification
		if v.SharerKind == "" {
			v.SharerKind = "human"
		}
		if len(b) > 0 {
			return json.Unmarshal(b, &v.Place)
		}
		return nil
	})
	return v, err
}
func (s *Store) EndLocation(ctx context.Context, k chat.LocationKey) error {
	return s.RunTenantTx(ctx, k.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chat_location_share SET place=NULL,ended=true,revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND id=$4`, k.TenantID, k.ConversationID, k.PostID, k.ID)
		if err == nil && n == 0 {
			return chat.ErrNotFound
		}
		return err
	})
}
func (s *Store) SweepLocations(ctx context.Context, tenantID string, now time.Time) (int64, error) {
	var count int64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chat_location_share l SET place=NULL,ended=true,ended_at=CASE WHEN expires_at<=$2 THEN expires_at ELSE now() END,ended_reason=CASE WHEN expires_at<=$2 THEN 'expired' ELSE 'removed' END,revision=revision+1 WHERE tenant_id=$1 AND NOT ended AND (expires_at<=$2 OR (EXISTS(SELECT 1 FROM chat_post p WHERE p.tenant_id=l.tenant_id AND p.conversation_id=l.conversation_id AND p.id=l.post_id AND (p.tombstoned OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p.references_json)='array' THEN p.references_json ELSE '[]'::jsonb END) r WHERE r->>'Kind'='LOCATION' AND r->>'ID'=l.id))) AND NOT EXISTS(SELECT 1 FROM chat_record_inventory i JOIN chat_record_hold h ON h.tenant_id=i.tenant_id AND h.hold_id IN (SELECT jsonb_array_elements_text(i.hold_ids)) WHERE i.tenant_id=l.tenant_id AND i.record_id='post:'||l.post_id AND h.released_at IS NULL)))`, tenantID, now)
		if err == nil {
			count = n
		}
		return err
	})
	return count, err
}

// SearchLocations requires current message authority before evaluating labels.
// There is deliberately no search by person or position across conversations.
func (s *Store) SearchLocations(ctx context.Context, p chat.Principal, locations *chat.LocationService, tenantID, conversationID, query string, now time.Time) ([]chat.LocationShare, error) {
	if len(query) > 1000 {
		return nil, chat.ErrInvalidArgument
	}
	if locations == nil || locations.Chat == nil {
		return nil, chat.ErrPermissionDenied
	}
	if _, _, err := locations.Chat.ReadAuthorizedReference(ctx, p, tenantID, conversationID, ""); err != nil {
		return nil, err
	}
	keys := []chat.LocationKey{}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT post_id,id FROM chat_location_share WHERE tenant_id=$1 AND conversation_id=$2 AND NOT ended AND (expires_at IS NULL OR expires_at>$3) ORDER BY id`, tenantID, conversationID, now)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			k := chat.LocationKey{TenantID: tenantID, ConversationID: conversationID}
			if err = rows.Scan(&k.PostID, &k.ID); err != nil {
				return err
			}
			keys = append(keys, k)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	out := []chat.LocationShare{}
	for _, k := range keys {
		v, err := locations.Read(ctx, p, k)
		if err != nil {
			if !errors.Is(err, chat.ErrPermissionDenied) && !errors.Is(err, chat.ErrNotFound) {
				return nil, err
			}
			continue
		}
		if !v.Ended && strings.Contains(strings.ToLower(v.Place.Label+" "+v.Place.Address), strings.ToLower(query)) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *Adapter) LocationReferenceExists(ctx context.Context, p chat.Principal, tenantID, conversationID, id string) error {
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var found bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_location_share WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 AND sharer_id=$4 AND sharer_tenant_id=$5 AND NOT ended AND (expires_at IS NULL OR expires_at>now()))`, tenantID, conversationID, id, p.SubjectID, p.TenantID).Scan(&found)
		if err != nil {
			return err
		}
		if !found {
			return chat.ErrPermissionDenied
		}
		return nil
	})
}
