package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The client half of polls and to-do lists posted as messages: the three calls
// of /api/chat/message-card/v1, and the rules for which cards on the page need
// their view read. Nothing here touches the browser, so it runs in native tests.

const chatcmd002HTTPPath = "/api/chat/message-card/v1"

// chatcmd002Command is the request body; it mirrors application.Chatcmd002CardCommand.
type chatcmd002Command struct {
	HostTenantID     string                   `json:"host_tenant_id,omitempty"`
	ConversationID   string                   `json:"conversation_id"`
	ParentID         string                   `json:"parent_id,omitempty"`
	IdempotencyKey   string                   `json:"idempotency_key,omitempty"`
	Card             *chat.Chatcmd002Card     `json:"card,omitempty"`
	PostID           string                   `json:"post_id,omitempty"`
	PostIDs          []string                 `json:"post_ids,omitempty"`
	ExpectedRevision uint64                   `json:"expected_revision,omitempty"`
	Mutation         *chat.Chatcmd002Mutation `json:"mutation,omitempty"`
}

type chatcmd002Reply struct {
	PostID   string                         `json:"post_id,omitempty"`
	Revision uint64                         `json:"revision,omitempty"`
	View     *chat.Chatcmd002View           `json:"view,omitempty"`
	Views    map[string]chat.Chatcmd002View `json:"views,omitempty"`
}

// chatcmd002Error is the service's refusal code ("conflict", "permission_denied",
// "not_found", "invalid_argument") or "unavailable" when it did not answer.
type chatcmd002Error string

func (e chatcmd002Error) Error() string { return "message card: " + string(e) }

// Code is the refusal as the preview reads it (chatui's post error line), so
// a refused post says why beside the preview.
func (e chatcmd002Error) Code() string { return string(e) }

// chatcmd002Request makes one card call. action is post, read, mutate or tidy;
// out receives the decoded answer.
func chatcmd002Request(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, in, out any) error {
	endpoint, err := personaChatURL(cfg, chatcmd002HTTPPath+"/"+action, "")
	if err != nil || cfg.Bearer == "" || client == nil {
		return chatcmd002Error("unavailable")
	}
	body, err := json.Marshal(in)
	if err != nil {
		return chatcmd002Error("invalid_argument")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return chatcmd002Error("unavailable")
	}
	request.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil || response == nil {
		return chatcmd002Error("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&refusal)
		switch refusal.Code {
		case "conflict", "permission_denied", "not_found", "invalid_argument":
			return chatcmd002Error(refusal.Code)
		}
		return chatcmd002Error("unavailable")
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(out) != nil {
		return chatcmd002Error("unavailable")
	}
	return nil
}

// chatcmd002Notice is the line a card shows when the reader's press was not
// accepted. A conflict on an anonymous vote means the ballot was already cast,
// which is final; any other conflict means the card moved on.
func chatcmd002Notice(err error, mutation chat.Chatcmd002Mutation, anonymous bool) string {
	code, _ := err.(chatcmd002Error)
	switch code {
	case "conflict":
		if mutation.Operation == "ADD_OPTION" {
			// The option is already there: nothing moved on, so it is not "changed
			// while you were looking".
			return "failed"
		}
		if mutation.Operation == "VOTE" && anonymous {
			return "final"
		}
		return "conflict"
	case "permission_denied":
		return "denied"
	}
	return "failed"
}

// chatcmd002IsCard reports whether a message body carries a card. The marker
// is checked first so an ordinary message costs no decoding.
func chatcmd002IsCard(body string) bool {
	if !strings.Contains(body, chat.Chatcmd002BodyMarker) {
		return false
	}
	_, ok := chat.Chatcmd002Decode(body)
	return ok
}

// chatcmd002Cards lists the card messages on the page with the revision each
// is shown at: the timeline, the open thread, its root and the pinned messages.
func chatcmd002Cards(m chatui.Model) map[string]uint64 {
	cards := map[string]uint64{}
	for _, message := range integrate2ReaderMessages(m) {
		if message.ID != "" && chatcmd002IsCard(message.Body) && message.Revision >= cards[message.ID] {
			cards[message.ID] = message.Revision
		}
	}
	return cards
}

// chatcmd002Fingerprint changes when a card appears on the page or one of them
// changes revision, which is when their views must be read again.
func chatcmd002Fingerprint(m chatui.Model) string {
	var key strings.Builder
	key.WriteString(m.CurrentTenantID + "\x00" + m.CurrentUser + "\x00" + m.SelectedID)
	for _, message := range integrate2ReaderMessages(m) {
		if message.ID != "" && strings.Contains(message.Body, chat.Chatcmd002BodyMarker) {
			key.WriteString("\x00" + message.ID + ":")
			key.WriteString(strconv.FormatUint(message.Revision, 10))
		}
	}
	return key.String()
}

// chatcmd002Stale lists the cards whose view has not been read at the revision
// on the page. asked holds the revision each card was last asked for, so one
// revision is read once however many renders pass.
func chatcmd002Stale(cards, asked map[string]uint64) []string {
	var ids []string
	for id, revision := range cards {
		if at, ok := asked[id]; !ok || at < revision {
			ids = append(ids, id)
		}
	}
	return ids
}

// chatcmd002PlainMessages returns the messages with the edited mark cleared on
// cards: a card's revision moves with every vote and tick, and none of those
// is an edit of what its author wrote. The input is not changed.
func chatcmd002PlainMessages(messages []chatui.Message) []chatui.Message {
	var out []chatui.Message
	for i, message := range messages {
		if !message.Edited || !chatcmd002IsCard(message.Body) {
			continue
		}
		if out == nil {
			out = append([]chatui.Message(nil), messages...)
		}
		out[i].Edited = false
	}
	if out == nil {
		return messages
	}
	return out
}
