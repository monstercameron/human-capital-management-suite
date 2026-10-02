package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATUX-022: "Mark unread from here". The read position used to move one way
// only, so the page could not put a conversation back to unread: a mark made
// there was lost on reload and never reached the person's other devices.
//
// The two statements differ in one word. An advance takes the greater of the
// stored and the offered sequence, so a late or repeated advance cannot
// un-read anything; a rewind takes the lesser, so it cannot mark anything
// read. Both bump the cursor's revision, which is what tells the cached
// sidebar counts (chatscale_read_state) to rebuild.
const (
	chatReadAdvanceSQL = `UPDATE chat_cursor SET last_sequence=GREATEST(last_sequence,$1),revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND conversation_id=$3 AND home_tenant_id=$4 AND member_id=$5 AND revision=$6 RETURNING last_sequence,revision`
	chatReadRewindSQL  = `UPDATE chat_cursor SET last_sequence=LEAST(last_sequence,$1),revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND conversation_id=$3 AND home_tenant_id=$4 AND member_id=$5 AND revision=$6 RETURNING last_sequence,revision`
)

// RewindReadState moves a member's read position back to x.LastReadSequence.
// It has PutReadState's checks: an active membership, a sequence the member's
// history allows, and the expected revision.
func (s *Adapter) RewindReadState(ctx context.Context, x chat.ReadState, expected uint64) (chat.ReadState, error) {
	return s.putReadState(ctx, x, expected, true)
}
