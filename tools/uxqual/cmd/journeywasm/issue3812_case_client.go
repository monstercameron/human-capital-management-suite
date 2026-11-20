package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3812: the browser side of a conversation's governed case. Three plain
// JSON calls (read, bind, lift) to the case routes. Nothing here decides who may
// do what: the server does, and the client shows its word.

const caseRoute = "/api/chat/case/v1/"

type caseViewJSON struct {
	Bound      bool   `json:"bound"`
	CaseRef    string `json:"case_ref"`
	OwnerID    string `json:"owner_id"`
	OwnerIsMe  bool   `json:"owner_is_me"`
	Restricted bool   `json:"restricted"`
	CanManage  bool   `json:"can_manage"`
}

type caseRequestJSON struct {
	ConversationID string `json:"conversation_id"`
	CaseRef        string `json:"case_ref,omitempty"`
	OwnerID        string `json:"owner_id,omitempty"`
	Restricted     bool   `json:"restricted,omitempty"`
}

// caseError is a refusal reduced to the server's short code.
type caseError struct{ Code string }

func (e *caseError) Error() string { return e.Code }

func caseState(conversation string, raw caseViewJSON) chatui.CaseState {
	return chatui.CaseState{ConversationID: conversation, Loaded: true, Bound: raw.Bound, CaseRef: raw.CaseRef, OwnerID: raw.OwnerID,
		OwnerIsMe: raw.OwnerIsMe, Restricted: raw.Bound && raw.Restricted, CanManage: raw.CanManage}
}

func caseCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, body caseRequestJSON) (caseViewJSON, error) {
	var view caseViewJSON
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return view, &caseError{Code: "unavailable"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return view, &caseError{Code: "unavailable"}
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = caseRoute+action, "", "", ""
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
			default:
				refusal.Code = "unavailable"
			}
		}
		return caseViewJSON{}, &caseError{Code: refusal.Code}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&view); err != nil {
		return caseViewJSON{}, &caseError{Code: "unavailable"}
	}
	return view, nil
}

// caseRead asks what the reader may know of the conversation's case.
func caseRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string) (chatui.CaseState, error) {
	view, err := caseCall(ctx, client, cfg, "get", caseRequestJSON{ConversationID: conversation})
	if err != nil {
		return chatui.CaseState{}, err
	}
	return caseState(conversation, view), nil
}

// caseBind binds the conversation, or changes its binding.
func caseBind(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, caseRef, owner string, restricted bool) (chatui.CaseState, error) {
	view, err := caseCall(ctx, client, cfg, "bind", caseRequestJSON{ConversationID: conversation, CaseRef: caseRef, OwnerID: owner, Restricted: restricted})
	if err != nil {
		return chatui.CaseState{}, err
	}
	return caseState(conversation, view), nil
}

// caseLift lifts the binding.
func caseLift(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string) (chatui.CaseState, error) {
	view, err := caseCall(ctx, client, cfg, "unbind", caseRequestJSON{ConversationID: conversation})
	if err != nil {
		return chatui.CaseState{}, err
	}
	return caseState(conversation, view), nil
}
