package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3794: the browser side of handing off one linked work item. The calls
// are plain JSON over the /api/chat/work-handoffs routes; nothing here decides
// who may offer, see or answer an offer, the server does.

type handoffJSON struct {
	ID         string `json:"id"`
	PostID     string `json:"post_id"`
	ProjectID  string `json:"project_id"`
	TaskID     string `json:"task_id"`
	Mode       string `json:"mode"`
	Role       string `json:"role"`
	FromID     string `json:"from_id"`
	ToID       string `json:"to_id"`
	NextAction string `json:"next_action"`
	Note       string `json:"note"`
	Status     string `json:"status"`
	OwnerID    string `json:"owner_id"`
	OfferedOwn string `json:"offered_owner_id"`
	OfferedDue string `json:"offered_due_date"`
	Restricted bool   `json:"restricted"`
	Changed    bool   `json:"changed"`
}

type handoffReply struct {
	Handoffs []handoffJSON `json:"handoffs"`
	Handoff  *handoffJSON  `json:"handoff"`
}

type handoffOffer struct {
	ConversationID string `json:"conversation_id"`
	ProjectID      string `json:"project_id"`
	TaskID         string `json:"task_id"`
	Mode           string `json:"mode"`
	RecipientID    string `json:"recipient_id"`
	NextAction     string `json:"next_action"`
	Note           string `json:"note,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

// handoffCall makes one request. A non-200 answer is reduced to the server's
// short code; the body of a refusal is never shown.
func handoffCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, method, path string, query url.Values, command *handoffOffer) (handoffReply, error) {
	var reply handoffReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, chatworkError("unavailable")
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = path, query.Encode(), ""
	var body io.Reader
	if command != nil {
		encoded, err := json.Marshal(command)
		if err != nil {
			return reply, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return reply, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return reply, chatworkError("unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&refusal)
		if refusal.Code == "" {
			refusal.Code = "unavailable"
		}
		return handoffReply{}, chatworkError(refusal.Code)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&reply); err != nil {
		return handoffReply{}, chatworkError("unavailable")
	}
	return reply, nil
}

func handoffList(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string) ([]handoffJSON, error) {
	reply, err := handoffCall(ctx, client, cfg, http.MethodGet, "/api/chat/work-handoffs", url.Values{"conversation_id": {conversation}}, nil)
	return reply.Handoffs, err
}

func handoffSend(ctx context.Context, client *http.Client, cfg journeyclient.Config, command handoffOffer) (handoffJSON, error) {
	reply, err := handoffCall(ctx, client, cfg, http.MethodPost, "/api/chat/work-handoffs", nil, &command)
	if err != nil {
		return handoffJSON{}, err
	}
	if reply.Handoff == nil {
		return handoffJSON{}, chatworkError("unavailable")
	}
	return *reply.Handoff, nil
}

// handoffAnswer accepts, declines or withdraws one offer.
func handoffAnswer(ctx context.Context, client *http.Client, cfg journeyclient.Config, id, verb string) (handoffJSON, error) {
	if strings.TrimSpace(id) == "" || strings.Contains(id, "/") || (verb != "accept" && verb != "decline" && verb != "withdraw") {
		return handoffJSON{}, chatworkError("invalid_argument")
	}
	reply, err := handoffCall(ctx, client, cfg, http.MethodPost, "/api/chat/work-handoffs/"+url.PathEscape(id)+"/"+verb, nil, nil)
	if err != nil {
		return handoffJSON{}, err
	}
	if reply.Handoff == nil {
		return handoffJSON{}, chatworkError("unavailable")
	}
	return *reply.Handoff, nil
}

// handoffNewKey names one reviewed offer. The form keeps it until the offer is
// made or the person changes what they asked for, so a retry after an uncertain
// answer recovers the same offer instead of making a second.
func handoffNewKey() string { return "handoff-" + uuid.NewString() }

// handoffByTask groups the viewer's offers by the task they are about. An offer
// about work the viewer cannot open carries no task and is left out.
func handoffByTask(offers []handoffJSON) map[string][]chatui.WorkHandoffView {
	grouped := make(map[string][]chatui.WorkHandoffView)
	for _, offer := range offers {
		if offer.ID == "" || offer.TaskID == "" || offer.Restricted {
			continue
		}
		grouped[offer.TaskID] = append(grouped[offer.TaskID], chatui.WorkHandoffView{
			ID: offer.ID, PostID: offer.PostID, ProjectID: offer.ProjectID, TaskID: offer.TaskID, Mode: offer.Mode, Role: offer.Role,
			FromID: offer.FromID, ToID: offer.ToID, NextAction: offer.NextAction, Note: offer.Note, Status: offer.Status,
			OfferedOwner: offer.OfferedOwn, OfferedDue: offer.OfferedDue, Changed: offer.Changed,
		})
	}
	return grouped
}

// handoffMerge replaces one offer in the grouped state with its fresh answer.
func handoffMerge(grouped map[string][]chatui.WorkHandoffView, offer handoffJSON) map[string][]chatui.WorkHandoffView {
	next := make(map[string][]chatui.WorkHandoffView, len(grouped)+1)
	for task, list := range grouped {
		next[task] = append([]chatui.WorkHandoffView(nil), list...)
	}
	fresh := handoffByTask([]handoffJSON{offer})[offer.TaskID]
	if len(fresh) == 0 {
		return next
	}
	for i, existing := range next[offer.TaskID] {
		if existing.ID == offer.ID {
			next[offer.TaskID][i] = fresh[0]
			return next
		}
	}
	next[offer.TaskID] = append(next[offer.TaskID], fresh[0])
	return next
}
