package chatstore

import (
	"context"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// CHATGATE-010 and CHATGATE-011 in the chat database: a post addressed to
// segments, and the tally of a poll addressed to them.
//
// One rule decides who reads a restricted post, written once here as SQL for the
// queries that list posts (history, threads, search, the thread snapshot an agent
// reads) and once as PostReadableBy for the paths that fetch one post. A post is
// restricted when chat_post_audience says so; its readers are its author and the
// member_ids frozen when it was sent. An agent reading as an installed app sees no
// restricted post at all.

// chatgateAudienceFilter2, 3 and 4 differ only in the number of the parameter that
// holds the reader's subject: the queries they extend number theirs differently.
const chatgateAudienceFilter2 = ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=p.tenant_id AND au.post_id=p.id AND au.restricted AND au.author_id<>$2 AND NOT ($2=ANY(au.member_ids)))`
const chatgateAudienceFilter3 = ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=p.tenant_id AND au.post_id=p.id AND au.restricted AND au.author_id<>$3 AND NOT ($3=ANY(au.member_ids)))`
const chatgateAudienceFilter4 = ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=p.tenant_id AND au.post_id=p.id AND au.restricted AND au.author_id<>$4 AND NOT ($4=ANY(au.member_ids)))`

// chatgateAudienceNone is for readers that are not people: nothing restricted.
const chatgateAudienceNone = ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=p.tenant_id AND au.post_id=p.id AND au.restricted)`

// writePostAudience stores the audience a context carries, in the transaction
// that writes the post.
func writePostAudience(ctx context.Context, tx dbport.Tx, tenant, conversation, post, author string) error {
	a, ok := chat.PostAudienceFrom(ctx)
	if !ok {
		return nil
	}
	segments := append([]string(nil), a.Segments...)
	members := append([]string(nil), a.Members...)
	if !a.Restricted {
		members = []string{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO chat_post_audience(tenant_id,conversation_id,post_id,author_id,segments,restricted,member_ids) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, conversation, post, author, segments, a.Restricted, members)
	return err
}

// PostReadableBy is the rule for one post. A post nobody addressed, or addressed
// without restriction, is readable by anybody the history rules allow.
func (s *Adapter) PostReadableBy(ctx context.Context, tenantID, conversationID, postID, person string) (bool, error) {
	if tenantID == "" || postID == "" || person == "" {
		return false, chat.ErrInvalidArgument
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, tenantID); err != nil {
		return false, err
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=$1 AND au.post_id=$2 AND au.restricted AND au.author_id<>$3 AND NOT ($3=ANY(au.member_ids)))`, tenantID, postID, person).Scan(&allowed); err != nil {
		return false, err
	}
	return allowed, tx.Commit(ctx)
}

// RestrictedCount is how many restricted messages a channel holds. Administrators
// are told that they exist and how many; reading them is the audience's.
func (s *Store) RestrictedCount(ctx context.Context, tenantID, conversationID string) (int, error) {
	if s == nil || tenantID == "" || conversationID == "" {
		return 0, chat.ErrInvalidArgument
	}
	n := 0
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_post_audience a JOIN chat_post p ON p.tenant_id=a.tenant_id AND p.id=a.post_id WHERE a.tenant_id=$1 AND a.conversation_id=$2 AND a.restricted AND NOT p.tombstoned`, tenantID, conversationID).Scan(&n)
	})
	return n, err
}

// chatcmd002VoterSegments asks the gate which of the card's segments the voter
// is in. No way to ask, or no segment, is a refusal.
func chatcmd002VoterSegments(ctx context.Context, r chat.Chatcmd002Request, card chat.Chatcmd002Card) ([]string, error) {
	if r.Segments == nil {
		return nil, chat.ErrPermissionDenied
	}
	signature, err := r.Segments(ctx, card.Audience)
	if err != nil {
		return nil, chat.ErrUnavailable
	}
	if len(signature) == 0 {
		return nil, chat.ErrPermissionDenied
	}
	return signature, nil
}

func chatcmd002Signature(segments []string) string {
	sorted := append([]string(nil), segments...)
	sort.Strings(sorted)
	return strings.Join(sorted, "|")
}

// chatcmd002SegmentTally adds one anonymous ballot to the poll's tally by cell.
func chatcmd002SegmentTally(ctx context.Context, tx dbport.Tx, r chat.Chatcmd002Request, options []string) error {
	if len(r.VoterSegments) == 0 {
		return nil
	}
	signature := chatcmd002Signature(r.VoterSegments)
	for _, option := range append([]string{""}, options...) {
		if _, err := tx.Exec(ctx, `INSERT INTO chat_post_card_segment_tally(tenant_id,conversation_id,post_id,signature,option_id,votes) VALUES($1,$2,$3,$4,$5,1) ON CONFLICT(tenant_id,post_id,signature,option_id) DO UPDATE SET votes=chat_post_card_segment_tally.votes+1`, r.TenantID, r.ConversationID, r.PostID, signature, option); err != nil {
			return err
		}
	}
	return nil
}

// chatcmd002SegmentBreakdown reports a poll by segment under the standard group
// size. It reads the tally by cell and nothing that names a voter.
func chatcmd002SegmentBreakdown(ctx context.Context, tx dbport.Tx, r chat.Chatcmd002Request) (*chatgate.Breakdown, error) {
	rows, err := tx.Query(ctx, `SELECT signature,option_id,votes FROM chat_post_card_segment_tally WHERE tenant_id=$1 AND post_id=$2`, r.TenantID, r.PostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cells := map[string]*chatgate.Cell{}
	for rows.Next() {
		var signature, option string
		var votes int
		if err := rows.Scan(&signature, &option, &votes); err != nil {
			return nil, err
		}
		cell := cells[signature]
		if cell == nil {
			cell = &chatgate.Cell{Signature: strings.Split(signature, "|"), Options: map[string]int{}}
			cells[signature] = cell
		}
		if option == "" {
			cell.Voters = votes
		} else {
			cell.Options[option] = votes
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	list := make([]chatgate.Cell, 0, len(cells))
	for _, cell := range cells {
		list = append(list, *cell)
	}
	breakdown := chatgate.BreakDown(list, chatgate.MinGroup)
	return &breakdown, nil
}

// The same rule for the events of a conversation: the outbox rows of a restricted
// post (created, edited, deleted, reacted to) are delivered to the people the post
// is restricted to, and to nobody else's stream. The reader's subject is the
// parameter named in the constant; for a machine reader there is none and no
// restricted post's event is delivered.
const chatgateOutboxAudience5 = ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=chat_outbox.tenant_id AND au.post_id=(chat_outbox.payload->>'` + OutboxKeyTargetID + `') AND au.restricted AND au.author_id<>$5 AND NOT ($5=ANY(au.member_ids)))`
const chatgateOutboxAudience7 = ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=chat_outbox.tenant_id AND au.post_id=(chat_outbox.payload->>'` + OutboxKeyTargetID + `') AND au.restricted AND au.author_id<>$7 AND NOT ($7=ANY(au.member_ids)))`
const chatgateOutboxAudienceNone = ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=o.tenant_id AND au.post_id=(o.payload->>'` + OutboxKeyTargetID + `') AND au.restricted)`
