package main

import (
	"encoding/json"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATGATE-014: the labels of the open conversation and who holds each (for
// Conversation details), what the writer may address a message to (for the
// composer), and who a message is for (for its tag).

// chatgateLabelsAction and chatgateAudienceAction are the client's names for the
// reads; the server takes them as GET ?labels=1 and ?audience=1 with
// &conversation=....
const (
	chatgateLabelsAction   = "labels"
	chatgateAudienceAction = "audience"
)

// chatgateLabels reads the server's answer into what the page keeps for the
// conversation. ok is false for an answer that cannot be read; the page then
// keeps what it had.
func chatgateLabels(raw json.RawMessage) ([]chatui.GateLabel, bool) {
	var rows []struct {
		Name   string   `json:"name"`
		People []string `json:"people"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &rows) != nil {
		return nil, false
	}
	out := make([]chatui.GateLabel, 0, len(rows))
	for _, row := range rows {
		if row.Name == "" || len(row.People) == 0 {
			continue
		}
		out = append(out, chatui.GateLabel{Name: row.Name, People: append([]string(nil), row.People...)})
	}
	return out, true
}

// chatgateAudienceOptions reads what the writer may address. A segment with no
// name is dropped; ok is false for an answer that cannot be read.
func chatgateAudienceOptions(raw json.RawMessage) (chatui.GateAudienceOptions, bool) {
	var reply struct {
		Segments []struct {
			Name   string `json:"name"`
			People int    `json:"people"`
		} `json:"segments"`
		Restricted bool `json:"restricted"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &reply) != nil {
		return chatui.GateAudienceOptions{}, false
	}
	out := chatui.GateAudienceOptions{Restricted: reply.Restricted}
	for _, s := range reply.Segments {
		if s.Name != "" {
			out.Segments = append(out.Segments, chatui.GateAudienceChoice{Name: s.Name, People: s.People})
		}
	}
	return out, true
}

// chatgateMessageAudience is who a post is for, nil for a post for everyone.
func chatgateMessageAudience(post *chatv1.Post) *chatui.MessageAudience {
	if len(post.GetAudienceSegments()) == 0 {
		return nil
	}
	return &chatui.MessageAudience{Segments: append([]string(nil), post.GetAudienceSegments()...), Restricted: post.GetAudienceRestricted(), People: int(post.GetAudiencePeople())}
}
