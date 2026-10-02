package application

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATBUG-014: when Chat opens it asks for the status of every channel in the
// list (locked, archived, announcements only), and it asked one request per
// channel: 14 to 18 requests on the review data, one after the other. The same
// statuses are answered here in one request. Each conversation is read through
// the same service call as the single read, as the same principal, so it is
// authorized exactly as it is there; a conversation the reader may not see is
// answered with its own refusal and the others are not affected.

const (
	// channelStatusBatchParam carries the conversation ids, comma separated.
	channelStatusBatchParam = "conversations"
	// channelStatusBatchLimit bounds one request. A list longer than this is
	// asked for in more than one.
	channelStatusBatchLimit = 100
)

// ChannelStatusBatchItem is the answer for one conversation of a batch read:
// its status as the single read returns it, or the code of its refusal.
type ChannelStatusBatchItem struct {
	ConversationID string `json:"conversation_id"`
	Code           string `json:"code,omitempty"`
	Snapshot       any    `json:"snapshot,omitempty"`
}

// channelStatusBatchIDs reads the ids of a batch request: non-empty, each
// named once, in the order asked. It reports false for none, or too many.
func channelStatusBatchIDs(raw string) ([]string, bool) {
	var ids []string
	seen := map[string]bool{}
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, len(ids) > 0 && len(ids) <= channelStatusBatchLimit
}

func (h ChannelStatusHandler) serveBatch(w http.ResponseWriter, r *http.Request, principal chat.Principal) {
	ids, ok := channelStatusBatchIDs(r.URL.Query().Get(channelStatusBatchParam))
	if !ok {
		chatstateHTTPError(w, "invalid_argument", http.StatusBadRequest)
		return
	}
	items := make([]ChannelStatusBatchItem, 0, len(ids))
	for _, id := range ids {
		if r.Context().Err() != nil {
			chatstateHTTPError(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		request := chat.GetConversationRequest{Principal: principal, TenantID: principal.TenantID, ConversationID: id}
		var value any
		var err error
		if snapshots, ok := h.Service.(chat.ChannelStatusSnapshotService); ok {
			value, err = snapshots.GetChannelStatusSnapshot(r.Context(), request)
		} else {
			value, err = h.Service.GetChannelStatus(r.Context(), request)
		}
		item := ChannelStatusBatchItem{ConversationID: id}
		if err != nil {
			item.Code, _ = chatstateRefusal(err)
		} else {
			item.Snapshot = value
		}
		items = append(items, item)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(items)
}
