package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3804: the browser side of what the agents of a channel remember. The
// calls are plain JSON over the four /api/chat/memory/v1 routes; nothing here
// decides who may remember, approve or forget a note, the server does.

const memoryHTTPPath = "/api/chat/memory/v1"

type memoryJSON struct {
	ID                   string     `json:"id"`
	ConversationID       string     `json:"conversation_id"`
	Text                 string     `json:"text"`
	SourcePostID         string     `json:"source_post_id"`
	SourceSequence       uint64     `json:"source_sequence"`
	Source               string     `json:"source"`
	ProposerHomeTenantID string     `json:"proposer_home_tenant_id"`
	ProposerID           string     `json:"proposer_id"`
	Status               string     `json:"status"`
	TTLDays              int        `json:"ttl_days"`
	CreatedAt            time.Time  `json:"created_at"`
	ExpiresAt            *time.Time `json:"expires_at"`
	CanApprove           bool       `json:"can_approve"`
	CanForget            bool       `json:"can_forget"`
}

type memoryReply struct {
	Memory      *memoryJSON  `json:"memory"`
	Items       []memoryJSON `json:"items"`
	CanRemember bool         `json:"can_remember"`
	IsManager   bool         `json:"is_manager"`
}

// memoryCommand mirrors application.ChatMemoryCommand.
type memoryCommand struct {
	HostTenantID   string `json:"host_tenant_id,omitempty"`
	ConversationID string `json:"conversation_id"`
	SourcePostID   string `json:"source_post_id,omitempty"`
	ID             string `json:"id,omitempty"`
}

type memoryError string

func (e memoryError) Error() string { return string(e) }

// memoryErrorCode reduces any failure to the short code the copy table knows.
func memoryErrorCode(err error) string {
	var code memoryError
	if errors.As(err, &code) {
		return string(code)
	}
	return "unavailable"
}

// memoryCall makes one request. A non-200 answer is reduced to the server's
// short code; the body of a refusal is never shown.
func memoryCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, command memoryCommand) (memoryReply, error) {
	var reply memoryReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	switch action {
	case "propose", "list", "approve", "forget":
	default:
		return reply, memoryError("unavailable")
	}
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, memoryError("unavailable")
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = memoryHTTPPath+"/"+action, "", ""
	encoded, err := json.Marshal(command)
	if err != nil {
		return reply, memoryError("invalid_argument")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return reply, memoryError("unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil || response == nil {
		return reply, memoryError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&refusal)
		switch refusal.Code {
		case "conflict", "permission_denied", "not_found", "invalid_argument":
			return memoryReply{}, memoryError(refusal.Code)
		}
		return memoryReply{}, memoryError("unavailable")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&reply); err != nil {
		return memoryReply{}, memoryError("unavailable")
	}
	return reply, nil
}

// memoryItems turns the server's notes into what the details section draws,
// in the order the server sent them (newest first). Who added a note is said
// as "you", the member's name, or the former-member label.
func memoryItems(model chatui.Model, list []memoryJSON, you, unknown string) []chatui.MemoryItem {
	items := make([]chatui.MemoryItem, 0, len(list))
	for _, n := range list {
		item := chatui.MemoryItem{ID: n.ID, Text: n.Text, Proposer: decisionsOwnerName(model, n.ProposerHomeTenantID, n.ProposerID, you, unknown), Status: strings.ToLower(n.Status), Source: strings.ToLower(n.Source),
			SourcePostID: n.SourcePostID, SourceSequence: n.SourceSequence, TTLDays: n.TTLDays, CanApprove: n.CanApprove, CanForget: n.CanForget}
		if n.ExpiresAt != nil {
			item.ExpiresAt = *n.ExpiresAt
		}
		if item.Source == "" {
			item.Source = "live"
		}
		items = append(items, item)
	}
	return items
}
