package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ChatSearchAuthority consults current channel policy, segment visibility and
// artifact grants. A deployment must supply this authority; nil fails closed.
// The first call (with just a conversation target) precedes text matching.
type ChatSearchAuthority func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error)

// ChatSearchRendering returns the caller's current rendering after access is
// granted. It may change Text, never identity, audience or the opening target.
type ChatSearchRendering func(context.Context, chatsearch.Actor, chatsearch.Row) (string, error)
type ChatSearchSource struct {
	store     *Store
	kind      chatsearch.Kind
	authority ChatSearchAuthority
	rendering ChatSearchRendering
}

func (s *Store) RegisterChatSearch(registry *chatsearch.Registry, authority ChatSearchAuthority) error {
	return s.RegisterChatSearchRendering(registry, authority, nil)
}

func (s *Store) RegisterChatSearchRendering(registry *chatsearch.Registry, authority ChatSearchAuthority, rendering ChatSearchRendering) error {
	if s == nil || authority == nil || registry == nil {
		return chatsearch.ErrInvalid
	}
	for _, kind := range []chatsearch.Kind{chatsearch.Message, chatsearch.Thread, chatsearch.Conversation, chatsearch.File, chatsearch.Pin, chatsearch.Todo, chatsearch.Poll, chatsearch.AgentAnswer, chatsearch.Location} {
		d, _ := registry.Declaration(kind)
		if err := registry.Register(d, &ChatSearchSource{store: s, kind: kind, authority: authority, rendering: rendering}); err != nil {
			return err
		}
	}
	return nil
}

func (s *ChatSearchSource) Search(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
	return s.store.searchChatContent(ctx, q, s.kind, s.authority, s.rendering, "")
}
func (s *ChatSearchSource) CanOpen(ctx context.Context, actor chatsearch.Actor, row chatsearch.Row) (bool, error) {
	// Reload the row, not only its conversation: edits, removed pins and expired
	// private envelopes can disappear while another source is being read.
	q := chatsearch.Request{Actor: actor, At: time.Now().UTC(), OpenMessageID: row.Target.MessageID}
	rows, err := s.store.searchChatContent(ctx, q, s.kind, s.authority, s.rendering, row.ID)
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

func (s *ChatSearchSource) CanOpenRows(ctx context.Context, actor chatsearch.Actor, rows []chatsearch.Row) (map[string]bool, error) {
	q := chatsearch.Request{Actor: actor, At: time.Now().UTC(), Limit: len(rows), OpenIDs: []string{}, OpenMessageIDs: []string{}}
	for _, row := range rows {
		q.OpenIDs = append(q.OpenIDs, row.ID)
		q.OpenMessageIDs = append(q.OpenMessageIDs, row.Target.MessageID)
	}
	current, err := s.store.searchChatContent(ctx, q, s.kind, s.authority, s.rendering, "")
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, row := range rows {
		for _, fresh := range current {
			if fresh.ID == row.ID && fresh.Text == row.Text && fresh.Target == row.Target {
				allowed[row.ID] = true
				break
			}
		}
	}
	return allowed, nil
}

const chatsearchRooms = `SELECT c.id FROM chat_conversation c JOIN chat_membership m ON m.tenant_id=c.tenant_id AND m.conversation_id=c.id AND m.home_tenant_id=$2 AND m.member_id=$3 AND m.state='active' AND m.left_at IS NULL WHERE c.tenant_id=$1`
const chatsearchPosts = `WITH visible AS NOT MATERIALIZED (
 SELECT p.* FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$2 AND m.member_id=$3 AND m.state='active' AND m.left_at IS NULL
 JOIN chat_conversation c ON c.tenant_id=p.tenant_id AND c.id=p.conversation_id
 WHERE p.tenant_id=$1 AND p.conversation_id=ANY($4) AND ($5='' OR $6::bool OR p.id=$7) AND NOT p.tombstoned AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))
), catalog AS (`

// chatsearchRowSkipped names, for a developer cell that opts in with
// HCMNEXT_AGENT_DEBUG_CAUSES=1, why one row or room was withheld from a search.
func chatsearchRowSkipped(kind chatsearch.Kind, cause error) {
	if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
		slog.Warn("hcmnext.chat_search_row_withheld", "source", string(kind), "cause", cause.Error())
	}
}

// chatsearchLikePatterns turns the typed words into ILIKE patterns for the
// database prefilter. A word with non-ASCII letters is left to Match alone,
// because the database's case folding there can differ from Go's; leaving a
// word out only widens the prefilter. nil means no prefilter.
func chatsearchLikePatterns(query string) []string {
	var out []string
	escape := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	for _, word := range strings.Fields(query) {
		ascii := true
		for i := 0; i < len(word); i++ {
			ascii = ascii && word[i] < 0x80
		}
		if word == "*" || !ascii {
			continue
		}
		out = append(out, "%"+escape.Replace(word)+"%")
	}
	return out
}

// SearchChatContent is the store's extension search port. No index contains a
// historic audience snapshot. All kinds read only current authoritative rows.
func (s *Store) SearchChatContent(ctx context.Context, q chatsearch.Request, kind chatsearch.Kind, authority ChatSearchAuthority, id string) ([]chatsearch.Row, error) {
	return s.searchChatContent(ctx, q, kind, authority, nil, id)
}

func (s *Store) searchChatContent(ctx context.Context, q chatsearch.Request, kind chatsearch.Kind, authority ChatSearchAuthority, rendering ChatSearchRendering, id string) ([]chatsearch.Row, error) {
	if s == nil || authority == nil || q.Actor.TenantID == "" || q.Actor.HomeTenantID == "" || q.Actor.PersonID == "" {
		return nil, chatsearch.ErrInvalid
	}
	var out []chatsearch.Row
	err := s.RunTenantTx(ctx, q.Actor.TenantID, func(tx dbport.Tx) error {
		rooms, err := tx.Query(ctx, chatsearchRooms, q.Actor.TenantID, q.Actor.HomeTenantID, q.Actor.PersonID)
		if err != nil {
			return err
		}
		var candidates []string
		for rooms.Next() {
			var room string
			if err := rooms.Scan(&room); err != nil {
				rooms.Close()
				return err
			}
			candidates = append(candidates, room)
		}
		err = rooms.Err()
		rooms.Close()
		if err != nil {
			return err
		}
		allowed := []string{}
		for _, room := range candidates {
			if q.Filters.Conversation != "" && q.Filters.Conversation != room {
				continue
			}
			ok, e := authority(ctx, q.Actor, chatsearch.Row{Kind: kind, TenantID: q.Actor.TenantID, Target: chatsearch.Target{ConversationID: room}})
			if e != nil {
				if ctx.Err() != nil {
					return e
				}
				chatsearchRowSkipped(kind, e)
				continue
			}
			if ok {
				allowed = append(allowed, room)
			}
		}
		if len(allowed) == 0 {
			return nil
		}
		body, err := chatsearchCatalog(kind)
		if err != nil {
			return err
		}
		// Candidate windows are chronological, never text-ranked or counted.
		// Refill after authorization so denied rows cannot hide later matches.
		query := chatsearchPosts + body + `) SELECT payload FROM catalog WHERE ($5='' OR payload->>'ID'=$5)`
		skipPostID := kind != chatsearch.Message && kind != chatsearch.Thread && kind != chatsearch.Pin && kind != chatsearch.File
		postID := q.OpenMessageID
		if postID == "" && kind != chatsearch.File {
			postID = id
		}
		if postID == "" {
			skipPostID = true
		}
		if id != "" && !skipPostID {
			query = strings.Replace(query, "($5='' OR $6::bool OR p.id=$7)", "($6::bool IS NOT NULL AND p.id=$7)", 1)
		}
		if q.OpenIDs != nil {
			query += ` AND payload->>'ID'=ANY($14::text[]) AND $15::text[] IS NOT NULL AND $7::text IS NOT NULL`
			if kind == chatsearch.Message || kind == chatsearch.Thread || kind == chatsearch.Pin || kind == chatsearch.File {
				query = strings.Replace(query, "($5='' OR $6::bool OR p.id=$7)", "($6::bool IS NOT NULL AND p.id=ANY($15::text[]))", 1)
			}
		}
		var scanAt *time.Time
		scanKey := ""
		if q.BeforeKey != "" {
			scanAt = &q.BeforeAt
			scanKey = strings.ReplaceAll(q.BeforeKey, "\x00", "\x01")
		}
		// The words are matched in the database first, so the policy and rendering
		// work below is done only for rows that can match, never for every post the
		// reader may read. The match stays a superset of Match (ASCII words only),
		// which still decides every row.
		if kind != chatsearch.Message && kind != chatsearch.Thread && kind != chatsearch.Pin && kind != chatsearch.AgentAnswer {
			query += ` AND ($12::text[] IS NULL OR payload->>'Text' ILIKE ALL($12::text[]))`
		} else if kind == chatsearch.Pin || kind == chatsearch.AgentAnswer {
			// Rendered kinds are matched after rendering; the argument is still
			// referenced so every kind takes the same arguments.
			query += ` AND ($12::text[] IS NULL OR true)`
		}
		query += ` AND ($13::text[] IS NULL OR true) AND ($8::timestamptz IS NULL OR (order_at,($10::text||chr(1)||order_id) COLLATE "C")<($8,$9::text)) ORDER BY order_at DESC,order_id COLLATE "C" DESC LIMIT $11`
		limit := q.Limit
		if limit == 0 {
			limit = 20
		}
		const window = 100
		var words []string
		if id == "" && q.OpenIDs == nil {
			words = chatsearchLikePatterns(q.Query)
		}
		// Posts that have a stored rendering (a translation) carrying the words
		// are found by id, so the planner never sees a subquery in the post filter.
		var renderIDs []string
		if words != nil && (kind == chatsearch.Message || kind == chatsearch.Thread) {
			renderIDs = []string{}
			found, e := tx.Query(ctx, `SELECT post_id FROM chatrender_rendering WHERE tenant_id=$1 AND rendering->>'text' ILIKE ALL($2::text[]) LIMIT 1000`, q.Actor.TenantID, words)
			if e != nil {
				return e
			}
			for found.Next() {
				var post string
				if e := found.Scan(&post); e != nil {
					found.Close()
					return e
				}
				renderIDs = append(renderIDs, post)
			}
			e = found.Err()
			found.Close()
			if e != nil {
				return e
			}
		}
		for {
			args := []any{q.Actor.TenantID, q.Actor.HomeTenantID, q.Actor.PersonID, allowed, id, skipPostID, postID, scanAt, scanKey, string(kind), window, words, renderIDs}
			if q.OpenIDs != nil {
				args = append(args, q.OpenIDs, q.OpenMessageIDs)
			}
			rows, e := tx.Query(ctx, query, args...)
			if e != nil {
				return e
			}
			read := 0
			for rows.Next() {
				read++
				var payload []byte
				if e := rows.Scan(&payload); e != nil {
					rows.Close()
					return e
				}
				var row chatsearch.Row
				if e := json.Unmarshal(payload, &row); e != nil {
					rows.Close()
					return e
				}
				at := row.At
				scanAt = &at
				scanKey = string(row.Kind) + "\x01" + row.ID
				if row.TenantID != q.Actor.TenantID || row.Deleted || row.Removed || !row.ExpiresAt.IsZero() && !q.At.Before(row.ExpiresAt) {
					continue
				}
				ok, e := authority(ctx, q.Actor, row)
				if e != nil {
					// A row whose access cannot be settled is withheld; it
					// must not take every other row of this kind down with it.
					if ctx.Err() != nil {
						rows.Close()
						return e
					}
					chatsearchRowSkipped(kind, e)
					continue
				}
				if !ok {
					continue
				}
				if rendering != nil {
					row.Text, e = rendering(ctx, q.Actor, row)
					if e != nil {
						// No rendering means no safe text to match or show (a
						// masked message must never fall back to its original),
						// so this one row is withheld and the rest answer.
						if ctx.Err() != nil {
							rows.Close()
							return e
						}
						chatsearchRowSkipped(kind, e)
						continue
					}
				}
				if chatsearch.Match(row, q) {
					out = append(out, row)
					if id == "" && len(out) > limit {
						rows.Close()
						return nil
					}
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if read < window || id != "" {
				return nil
			}
		}

	})
	return out, err
}

const chatsearchPostPayload = `jsonb_build_object('Kind',%s,'ID',p.id,'TenantID',p.tenant_id,'Text',p.body,'AuthorID',p.author_id,'At',p.created_at,
 'Target',jsonb_build_object('ConversationID',p.conversation_id,'MessageID',p.id,'Sequence',p.sequence,'ThreadID',p.parent_id,'ThreadSequence',COALESCE((SELECT root.sequence FROM chat_post root JOIN chat_membership rm ON rm.tenant_id=root.tenant_id AND rm.conversation_id=root.conversation_id AND rm.home_tenant_id=$2 AND rm.member_id=$3 AND rm.state='active' AND rm.left_at IS NULL WHERE root.tenant_id=p.tenant_id AND root.conversation_id=p.conversation_id AND root.id=p.parent_id AND NOT root.tombstoned AND (rm.history_visibility='FULL_HISTORY' OR (rm.history_visibility='FROM_JOIN' AND root.created_at>=rm.joined_at))),0)),
 'InThread',p.parent_id<>'','HasLink',p.body~'https?://','HasFile',EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p.references_json)='array' THEN p.references_json ELSE '[]'::jsonb END) r WHERE r->>'Kind'='MEDIA'),
 'HasReactions',EXISTS(SELECT 1 FROM chat_reaction r WHERE r.tenant_id=p.tenant_id AND r.post_id=p.id),
 'MentionsMe',EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p.references_json)='array' THEN p.references_json ELSE '[]'::jsonb END) r WHERE r->>'Kind'='PERSON_MENTION' AND r->>'TenantID'=$2 AND r->>'ID'=$3),
 'ByAgent',EXISTS(SELECT 1 FROM chat_app_installation i WHERE i.tenant_id=p.tenant_id AND i.conversation_id=p.conversation_id AND i.app_id=p.author_id))`

func chatsearchCatalog(kind chatsearch.Kind) (string, error) {
	post := func(label string) string { return strings.Replace(chatsearchPostPayload, "%s", "'"+label+"'", 1) }
	switch kind {
	case chatsearch.Message:
		return `SELECT ` + post("message") + ` payload,p.created_at order_at,p.id order_id FROM visible p WHERE p.parent_id='' AND ` + chatsearchBodyMatch, nil
	case chatsearch.Thread:
		return `SELECT ` + post("thread") + ` payload,p.created_at order_at,p.id order_id FROM visible p WHERE p.parent_id<>'' AND ` + chatsearchBodyMatch, nil
	case chatsearch.Pin:
		return `SELECT ` + post("pin") + ` payload,p.created_at order_at,p.id order_id FROM visible p WHERE EXISTS(SELECT 1 FROM chat_pin x WHERE x.tenant_id=p.tenant_id AND x.conversation_id=p.conversation_id AND x.post_id=p.id)`, nil
	case chatsearch.File:
		return `SELECT ` + post("file") + ` || jsonb_build_object('ID',p.id||':'||(r->>'ID'),'Text',r->>'Display','HasFile',true,'Target',jsonb_build_object('ConversationID',p.conversation_id,'MessageID',p.id,'Sequence',p.sequence,'ItemID',r->>'ID','ThreadID',p.parent_id,'ThreadSequence',COALESCE((SELECT root.sequence FROM chat_post root JOIN chat_membership rm ON rm.tenant_id=root.tenant_id AND rm.conversation_id=root.conversation_id AND rm.home_tenant_id=$2 AND rm.member_id=$3 AND rm.state='active' AND rm.left_at IS NULL WHERE root.tenant_id=p.tenant_id AND root.conversation_id=p.conversation_id AND root.id=p.parent_id AND NOT root.tombstoned AND (rm.history_visibility='FULL_HISTORY' OR (rm.history_visibility='FROM_JOIN' AND root.created_at>=rm.joined_at))),0))) payload,p.created_at order_at,p.id||':'||(r->>'ID') order_id FROM visible p CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(p.references_json)='array' THEN p.references_json ELSE '[]'::jsonb END) r WHERE r->>'Kind'='MEDIA'`, nil

	case chatsearch.Location:
		return `SELECT jsonb_build_object('Kind','location','ID',l.id,'TenantID',p.tenant_id,'Text',concat_ws(' ',l.place->>'Label',l.place->>'Address'),'AuthorID',l.sharer_id,'At',l.shared_at,'ExpiresAt',l.expires_at,'InThread',p.parent_id<>'','Target',jsonb_build_object('ConversationID',p.conversation_id,'MessageID',p.id,'Sequence',p.sequence,'ThreadID',p.parent_id,'ItemID',l.id)) payload,l.shared_at order_at,l.id order_id FROM visible p JOIN chat_location_share l ON l.tenant_id=p.tenant_id AND l.conversation_id=p.conversation_id AND l.post_id=p.id WHERE NOT l.ended AND l.place IS NOT NULL AND (l.expires_at IS NULL OR l.expires_at>now()) AND EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p.references_json)='array' THEN p.references_json ELSE '[]'::jsonb END) r WHERE r->>'Kind'='LOCATION' AND r->>'ID'=l.id)`, nil
	case chatsearch.Conversation:
		return `SELECT jsonb_build_object('Kind','conversation','ID',c.id,'TenantID',c.tenant_id,'Text',concat_ws(' ',c.name,c.description),'At',c.created_at,'Target',jsonb_build_object('ConversationID',c.id)) payload,c.created_at order_at,c.id order_id FROM chat_conversation c WHERE c.tenant_id=$1 AND c.id=ANY($4)`, nil
	case chatsearch.Todo:
		return `SELECT jsonb_build_object('Kind','todo','ID',c.conversation_id||':'||(item->>'id'),'TenantID',c.tenant_id,'Text',item->>'text','AuthorID',item->>'created_by','At',to_timestamp((item->>'created_at_unix')::bigint),'Target',jsonb_build_object('ConversationID',c.conversation_id,'ItemID',item->>'id','MessageID',item->>'source_post_id')) payload,to_timestamp((item->>'created_at_unix')::bigint) order_at,c.conversation_id||':'||(item->>'id') order_id FROM chat_channel_todo c CROSS JOIN LATERAL jsonb_array_elements(c.items_json) item WHERE c.tenant_id=$1 AND c.conversation_id=ANY($4) AND (COALESCE(item->>'source_post_id','')='' OR EXISTS(SELECT 1 FROM visible p WHERE p.id=item->>'source_post_id' AND p.conversation_id=c.conversation_id))`, nil
	case chatsearch.Poll:
		return `SELECT jsonb_build_object('Kind','poll','ID',c.conversation_id,'TenantID',c.tenant_id,'Text',concat_ws(' ',c.question,(SELECT string_agg(o->>'text',' ') FROM jsonb_array_elements(c.options_json) o)),'At',c.updated_at,'Target',jsonb_build_object('ConversationID',c.conversation_id,'ItemID','poll')) payload,c.updated_at order_at,c.conversation_id order_id FROM chat_channel_poll c WHERE c.tenant_id=$1 AND c.conversation_id=ANY($4) AND c.question<>''`, nil
	case chatsearch.AgentAnswer:
		return `SELECT ` + post("agent_answer") + ` payload,p.created_at order_at,p.id order_id FROM visible p WHERE EXISTS(SELECT 1 FROM chat_app_installation i WHERE i.tenant_id=p.tenant_id AND i.conversation_id=p.conversation_id AND i.app_id=p.author_id)
 UNION ALL SELECT jsonb_build_object('Kind','agent_answer','ID',e.id,'TenantID',e.tenant_id,'Text',e.body,'At',e.created_at,'OwnerID',e.recipient_subject_id,'Private',true,'ByAgent',true,'ExpiresAt',e.expires_at,'Target',jsonb_build_object('ConversationID',e.durable_copy_conversation_id,'MessageID',e.durable_copy_post_id,'Sequence',copy.sequence,'ThreadID',copy.parent_id)) payload,e.created_at order_at,e.id order_id FROM chat_ephemeral_post e JOIN visible copy ON copy.id=e.durable_copy_post_id AND copy.conversation_id=e.durable_copy_conversation_id WHERE e.tenant_id=$1 AND e.conversation_id=ANY($4) AND e.recipient_home_tenant_id=$2 AND e.recipient_subject_id=$3 AND e.expires_at>now() AND EXISTS(SELECT 1 FROM chat_membership m WHERE m.tenant_id=e.tenant_id AND m.conversation_id=e.conversation_id AND m.home_tenant_id=$2 AND m.member_id=$3 AND m.state='active' AND m.left_at IS NULL AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND e.created_at>=m.joined_at)))`, nil
	default:
		return "", errors.Join(chatsearch.ErrRegistry, chatsearch.ErrUnavailable)
	}
}

// chatsearchBodyMatch is the database prefilter for a post: its authored words,
// or the words of a stored rendering (a translation) the reader may be shown.
// A mask only removes words from the authored text, so the authored words are a
// superset for masked renderings. Match still decides every row afterwards.
const chatsearchBodyMatch = `($12::text[] IS NULL OR p.body ILIKE ALL($12::text[]) OR p.id = ANY($13::text[]))`
