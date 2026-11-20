package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3792: the browser side of /decide. The calls are plain JSON over the two
// /api/chat/decisions/v1 routes; nothing here decides who may record or read a
// decision, the server does.

const decisionsHTTPPath = "/api/chat/decisions/v1"

type decisionsJSON struct {
	ID                string    `json:"id"`
	ConversationID    string    `json:"conversation_id"`
	Text              string    `json:"text"`
	Rationale         string    `json:"rationale"`
	OwnerHomeTenantID string    `json:"owner_home_tenant_id"`
	OwnerID           string    `json:"owner_id"`
	DecidedAt         time.Time `json:"decided_at"`
	SourcePostID      string    `json:"source_post_id"`
	SourceSequence    uint64    `json:"source_sequence"`
	SupersedesID      string    `json:"supersedes_id"`
	SupersededByID    string    `json:"superseded_by_id"`
	CanReplace        bool      `json:"can_replace"`
}

type decisionsReply struct {
	Decision  *decisionsJSON  `json:"decision"`
	Decisions []decisionsJSON `json:"decisions"`
	CanRecord bool            `json:"can_record"`
}

// decisionsCommand mirrors application.ChatDecisionCommand.
type decisionsCommand struct {
	HostTenantID   string `json:"host_tenant_id,omitempty"`
	ConversationID string `json:"conversation_id"`
	Text           string `json:"text,omitempty"`
	Rationale      string `json:"rationale,omitempty"`
	SourcePostID   string `json:"source_post_id,omitempty"`
	SupersedesID   string `json:"supersedes_id,omitempty"`
	ParentID       string `json:"parent_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Label          string `json:"label,omitempty"`
}

type decisionsError string

func (e decisionsError) Error() string { return string(e) }

// decisionsErrorCode reduces any failure to the short code the copy table knows.
func decisionsErrorCode(err error) string {
	var code decisionsError
	if errors.As(err, &code) {
		return string(code)
	}
	return "unavailable"
}

// decisionsNewKey names one attempt to record a decision. Pressing the button
// again after a lost answer sends the same key, so it is never recorded twice.
func decisionsNewKey() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "decision-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return "decision-" + hex.EncodeToString(raw[:])
}

// decisionsCall makes one request. A non-200 answer is reduced to the server's
// short code; the body of a refusal is never shown.
func decisionsCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, command decisionsCommand) (decisionsReply, error) {
	var reply decisionsReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil || (action != "record" && action != "list") {
		return reply, decisionsError("unavailable")
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = decisionsHTTPPath+"/"+action, "", ""
	encoded, err := json.Marshal(command)
	if err != nil {
		return reply, decisionsError("invalid_argument")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return reply, decisionsError("unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil || response == nil {
		return reply, decisionsError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&refusal)
		switch refusal.Code {
		case "conflict", "permission_denied", "not_found", "invalid_argument":
			return decisionsReply{}, decisionsError(refusal.Code)
		}
		return decisionsReply{}, decisionsError("unavailable")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&reply); err != nil {
		return decisionsReply{}, decisionsError("unavailable")
	}
	return reply, nil
}

// decisionsOwnerName is who recorded a decision, in words: "you" for the
// viewer, the member's name, or the unknown label for someone who has left.
func decisionsOwnerName(model chatui.Model, homeTenant, id, you, unknown string) string {
	if id == model.CurrentUser && (homeTenant == "" || homeTenant == model.CurrentTenantID) {
		return you
	}
	for _, member := range model.Members {
		if member.ID == id && (member.HomeTenantID == "" || member.HomeTenantID == homeTenant) && strings.TrimSpace(member.Name) != "" {
			return member.Name
		}
	}
	for _, person := range model.SearchDirectory {
		if person.ID == id && strings.TrimSpace(person.Name) != "" {
			return person.Name
		}
	}
	return unknown
}

// decisionsItems turns the server's decisions into what the dialog draws, in
// the order the server sent them (newest first).
func decisionsItems(model chatui.Model, list []decisionsJSON, you, unknown string) []chatui.DecisionItem {
	items := make([]chatui.DecisionItem, 0, len(list))
	for _, d := range list {
		items = append(items, chatui.DecisionItem{ID: d.ID, Text: d.Text, Rationale: d.Rationale, Owner: decisionsOwnerName(model, d.OwnerHomeTenantID, d.OwnerID, you, unknown),
			DecidedAt: d.DecidedAt, SourcePostID: d.SourcePostID, SourceSequence: d.SourceSequence, SupersedesID: d.SupersedesID, SupersededByID: d.SupersededByID, CanReplace: d.CanReplace})
	}
	return items
}

// decisionsInForce counts the decisions nothing has replaced.
func decisionsInForce(list []decisionsJSON) int {
	n := 0
	for _, d := range list {
		if d.SupersededByID == "" {
			n++
		}
	}
	return n
}
