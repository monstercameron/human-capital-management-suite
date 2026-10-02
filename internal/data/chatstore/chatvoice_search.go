package chatstore

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type ChatvoiceSearchSource struct {
	Adapter   *Adapter
	Kind      chatsearch.Kind
	Authorize ChatSearchAuthority
}

func (s *Adapter) RegisterVoiceSearch(registry *chatsearch.Registry, authorize ChatSearchAuthority) error {
	if s == nil || registry == nil || authorize == nil {
		return chatsearch.ErrRegistry
	}
	for _, kind := range []chatsearch.Kind{chatsearch.Voice, chatsearch.VoiceCorrection} {
		if err := registry.RegisterSource(kind, &ChatvoiceSearchSource{Adapter: s, Kind: kind, Authorize: authorize}); err != nil {
			return err
		}
	}
	return nil
}

// Search checks current membership, history and the deployment's message/segment
// authority before loading transcript text or applying search filters.
func (s *ChatvoiceSearchSource) Search(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
	if s == nil || s.Adapter == nil || s.Authorize == nil || (s.Kind != chatsearch.Voice && s.Kind != chatsearch.VoiceCorrection) || q.Actor.TenantID == "" || q.Actor.HomeTenantID == "" || q.Actor.PersonID == "" {
		return nil, chatsearch.ErrInvalid
	}
	out := []chatsearch.Row{}
	err := s.Adapter.Store.RunTenantTx(ctx, q.Actor.TenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT v.conversation_id,v.post_id,v.artifact_id,p.author_id,p.created_at,p.sequence,p.parent_id FROM chat_voice_transcript v JOIN chat_post p ON p.tenant_id=v.tenant_id AND p.conversation_id=v.conversation_id AND p.id=v.post_id AND NOT p.tombstoned JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$2 AND m.member_id=$3 AND m.state='active' AND m.left_at IS NULL WHERE v.tenant_id=$1 AND v.state='ready' AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) AND ($4='' OR p.conversation_id=$4) AND ($5='' OR p.id=$5) ORDER BY p.created_at DESC,p.id,v.artifact_id`, q.Actor.TenantID, q.Actor.HomeTenantID, q.Actor.PersonID, q.Filters.Conversation, q.OpenMessageID)
		if err != nil {
			return err
		}
		type candidate struct {
			request chat.TranscriptionRequest
			row     chatsearch.Row
		}
		candidates := []candidate{}
		for rows.Next() {
			c := candidate{request: chat.TranscriptionRequest{TenantID: q.Actor.TenantID}, row: chatsearch.Row{Kind: s.Kind, TenantID: q.Actor.TenantID, HasVoice: true}}
			if err = rows.Scan(&c.request.ConversationID, &c.request.PostID, &c.request.ArtifactID, &c.row.AuthorID, &c.row.At, &c.row.Target.Sequence, &c.row.Target.ThreadID); err != nil {
				rows.Close()
				return err
			}
			c.row.ID = c.request.PostID + ":" + c.request.ArtifactID
			c.row.Target.ConversationID = c.request.ConversationID
			c.row.Target.MessageID = c.request.PostID
			c.row.Target.ItemID = c.request.ArtifactID
			c.row.InThread = c.row.Target.ThreadID != ""
			candidates = append(candidates, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, c := range candidates {
			allowed, err := s.Authorize(ctx, q.Actor, c.row)
			if err != nil {
				return err
			}
			if !allowed {
				continue
			}
			if err = lockVisibleReactionPost(ctx, tx, c.request.TenantID, c.request.ConversationID, c.request.PostID, q.Actor.HomeTenantID, q.Actor.PersonID); err != nil {
				if errors.Is(err, chat.ErrPermissionDenied) {
					continue
				}
				return err
			}
			record, err := voiceRead(ctx, tx, c.request)
			if errors.Is(err, chat.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if record.Transcript.State != chat.TranscriptReady {
				continue
			}
			c.row.Text = record.Transcript.Text()
			if s.Kind == chatsearch.VoiceCorrection {
				if record.Transcript.Correction == "" {
					continue
				}
				c.row.Text = record.Transcript.Correction
			}
			if chatsearch.Match(c.row, q) {
				out = append(out, c.row)
			}
		}
		return nil
	})
	return out, err
}

func (s *ChatvoiceSearchSource) CanOpen(ctx context.Context, actor chatsearch.Actor, row chatsearch.Row) (bool, error) {
	if row.TenantID != actor.TenantID || row.Kind != s.Kind {
		return false, nil
	}
	rows, err := s.Search(ctx, chatsearch.Request{Actor: actor, At: time.Now().UTC(), OpenMessageID: row.Target.MessageID, Filters: chatsearch.Filters{Conversation: row.Target.ConversationID}})
	if err != nil {
		return false, err
	}
	for _, current := range rows {
		if current.ID == row.ID && current.Text == row.Text && current.Target == row.Target {
			return true, nil
		}
	}
	return false, nil
}
