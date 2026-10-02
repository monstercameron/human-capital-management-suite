package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATBUG-014: the status of every channel in the list (locked, archived,
// announcements only) was read one request per channel when Chat opened, 14 to
// 18 requests on the review data. LoadMany reads them in one.

// chatstateBatchLimit mirrors the server's bound on one request.
const chatstateBatchLimit = 100

// chatstateBatchItem is the server's answer for one conversation: its snapshot
// or the code of its refusal.
type chatstateBatchItem struct {
	ConversationID string                      `json:"conversation_id"`
	Code           string                      `json:"code"`
	Snapshot       *chat.ChannelStatusSnapshot `json:"snapshot"`
}

// chatperf2StatusRooms lists the channels whose status is still to be read:
// every channel of rooms that has no view yet and is not the open conversation
// (which has its own watch), each once.
func chatperf2StatusRooms(rooms []chatui.Conversation, views map[string]chatui.ChannelStatusView, selected string) []string {
	var wanted []string
	seen := map[string]bool{}
	for _, room := range rooms {
		if room.Kind != chatui.PublicChannel && room.Kind != chatui.PrivateChannel {
			continue
		}
		if _, read := views[room.ID]; read || room.ID == "" || room.ID == selected || seen[room.ID] {
			continue
		}
		seen[room.ID] = true
		wanted = append(wanted, room.ID)
	}
	return wanted
}

// LoadMany reads the status of several conversations in one request per
// chatstateBatchLimit of them. views holds every conversation that was
// answered with a status; refused holds the refusal of every conversation the
// server would not answer for (which a single read would have got too). A
// conversation in neither was not answered, because the request failed or the
// server did not name it: the caller reads those one at a time, as before.
func (c chatstateClient) LoadMany(ctx context.Context, conversations []string, previous map[string]chatui.ChannelStatusView) (views map[string]chatui.ChannelStatusView, refused map[string]error) {
	views, refused = map[string]chatui.ChannelStatusView{}, map[string]error{}
	if c.HTTP == nil || c.Tenant == "" {
		return views, refused
	}
	for start := 0; start < len(conversations); start += chatstateBatchLimit {
		ids := conversations[start:min(len(conversations), start+chatstateBatchLimit)]
		asked := make(map[string]bool, len(ids))
		for _, id := range ids {
			asked[id] = true
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/api/chat/v1/channel-status/?conversations="+url.QueryEscape(strings.Join(ids, ",")), nil)
		if err != nil {
			continue
		}
		request.Header.Set("Authorization", "Bearer "+c.Bearer)
		request.Header.Set("Cache-Control", "no-store")
		response, err := c.HTTP.Do(request)
		if err != nil {
			continue
		}
		payload, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		response.Body.Close()
		var items []chatstateBatchItem
		if err != nil || len(payload) > 2<<20 || response.StatusCode != http.StatusOK || json.Unmarshal(payload, &items) != nil {
			continue
		}
		for _, item := range items {
			// Only what was asked for is taken, and each conversation once.
			if !asked[item.ConversationID] {
				continue
			}
			asked[item.ConversationID] = false
			switch {
			case item.Code != "":
				refused[item.ConversationID] = chatstateRefusal{Code: item.Code}
			case item.Snapshot != nil:
				if view, err := c.view(item.ConversationID, *item.Snapshot, previous[item.ConversationID]); err == nil {
					views[item.ConversationID] = view
				}
			}
		}
	}
	return views, refused
}
