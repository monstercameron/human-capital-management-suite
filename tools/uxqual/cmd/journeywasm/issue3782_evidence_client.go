package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3782: the browser side of the evidence attached to a conversation's
// case. Two plain JSON calls (list, attach) to the evidence routes. The page
// sends message ids, never text: the server reads each message itself. Nothing
// here decides who may do what or what a reader may see: the server does.

const evidenceRoute = "/api/chat/case-evidence/v1/"

type evidenceItemJSON struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	AuthorID    string `json:"author_id"`
	AuthoredAt  string `json:"authored_at"`
	Body        string `json:"body"`
	Fingerprint string `json:"fingerprint"`
}

type evidenceExcerptJSON struct {
	ID           string             `json:"id"`
	CaseRevision string             `json:"case_revision"`
	Note         string             `json:"note"`
	AttachedBy   string             `json:"attached_by"`
	AttachedAt   string             `json:"attached_at"`
	Items        []evidenceItemJSON `json:"items"`
}

type evidenceViewJSON struct {
	Bound     bool                  `json:"bound"`
	CaseRef   string                `json:"case_ref"`
	CanAttach bool                  `json:"can_attach"`
	Excerpts  []evidenceExcerptJSON `json:"excerpts"`
}

type evidenceRequestJSON struct {
	ConversationID string   `json:"conversation_id"`
	CaseRef        string   `json:"case_ref,omitempty"`
	CaseRevision   string   `json:"case_revision,omitempty"`
	Note           string   `json:"note,omitempty"`
	MessageIDs     []string `json:"message_ids,omitempty"`
}

func evidenceTime(raw string) time.Time {
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return at.UTC()
}

func evidenceState(conversation string, raw evidenceViewJSON) chatui.EvidenceState {
	state := chatui.EvidenceState{ConversationID: conversation, Loaded: true, Bound: raw.Bound, CaseRef: raw.CaseRef, CanAttach: raw.Bound && raw.CanAttach}
	for _, e := range raw.Excerpts {
		excerpt := chatui.EvidenceExcerpt{ID: e.ID, CaseRevision: e.CaseRevision, Note: e.Note, AttachedBy: e.AttachedBy, AttachedAt: evidenceTime(e.AttachedAt)}
		for _, item := range e.Items {
			excerpt.Items = append(excerpt.Items, chatui.EvidenceItem{ID: item.ID, Status: item.Status, AuthorID: item.AuthorID, AuthoredAt: evidenceTime(item.AuthoredAt), Body: item.Body, Fingerprint: item.Fingerprint})
		}
		state.Excerpts = append(state.Excerpts, excerpt)
	}
	return state
}

func evidenceCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, body evidenceRequestJSON) (evidenceViewJSON, error) {
	var view evidenceViewJSON
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return view, &caseError{Code: "unavailable"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return view, &caseError{Code: "unavailable"}
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = evidenceRoute+action, "", "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return view, &caseError{Code: "unavailable"}
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return view, &caseError{Code: "unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&refusal)
		if refusal.Code == "" {
			switch response.StatusCode {
			case http.StatusNotFound:
				refusal.Code = "not_found"
			case http.StatusForbidden:
				refusal.Code = "permission_denied"
			case http.StatusBadRequest:
				refusal.Code = "invalid_request"
			case http.StatusConflict:
				refusal.Code = "case_changed"
			default:
				refusal.Code = "unavailable"
			}
		}
		return evidenceViewJSON{}, &caseError{Code: refusal.Code}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&view); err != nil {
		return evidenceViewJSON{}, &caseError{Code: "unavailable"}
	}
	return view, nil
}

// evidenceRead asks what the reader may see of the conversation's evidence.
func evidenceRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string) (chatui.EvidenceState, error) {
	view, err := evidenceCall(ctx, client, cfg, "list", evidenceRequestJSON{ConversationID: conversation})
	if err != nil {
		return chatui.EvidenceState{}, err
	}
	return evidenceState(conversation, view), nil
}

// evidenceAttach attaches the reviewed messages, named by id, against the case
// the person reviewed them against.
func evidenceAttach(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, caseRef, revision, note string, messageIDs []string) (chatui.EvidenceState, error) {
	view, err := evidenceCall(ctx, client, cfg, "attach", evidenceRequestJSON{ConversationID: conversation, CaseRef: caseRef, CaseRevision: revision, Note: note, MessageIDs: messageIDs})
	if err != nil {
		return chatui.EvidenceState{}, err
	}
	return evidenceState(conversation, view), nil
}
