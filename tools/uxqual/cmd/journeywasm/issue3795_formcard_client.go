package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3795: the browser side of form cards. Two plain JSON calls: read what
// this reader may do with the cards on screen, and answer one. Nothing here
// decides who may answer or whether an answer is valid: the server does, and
// the client shows its word.

type formcardViewJSON struct {
	PostID      string            `json:"post_id"`
	CanSubmit   bool              `json:"can_submit"`
	Submitted   bool              `json:"submitted"`
	SubmittedAt string            `json:"submitted_at"`
	Mine        map[string]string `json:"mine"`
}

type formcardReply struct {
	Views map[string]formcardViewJSON `json:"views"`
	View  *formcardViewJSON           `json:"view"`
}

// formcardError is a refusal reduced to the server's short code. Fields names
// the refused fields of an invalid answer by their own codes.
type formcardError struct {
	Code   string
	Fields map[string]string
}

func (e *formcardError) Error() string { return e.Code }

func formcardView(raw formcardViewJSON) chatui.FormCardView {
	view := chatui.FormCardView{Loaded: true, CanSubmit: raw.CanSubmit && !raw.Submitted, Submitted: raw.Submitted, Mine: raw.Mine}
	if at, err := time.Parse(time.RFC3339, raw.SubmittedAt); err == nil {
		view.SubmittedAt = at
	}
	return view
}

func formcardCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, method, path, rawPath string, query url.Values, payload any) (formcardReply, error) {
	var reply formcardReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, &formcardError{Code: "unavailable"}
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = path, rawPath, query.Encode(), ""
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
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
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return reply, &formcardError{Code: "unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&refusal)
		if refusal.Code == "" {
			switch response.StatusCode {
			case http.StatusNotFound:
				refusal.Code = "not_found"
			case http.StatusForbidden:
				refusal.Code = "permission_denied"
			case http.StatusConflict:
				refusal.Code = "already_answered"
			default:
				refusal.Code = "unavailable"
			}
		}
		return formcardReply{}, &formcardError{Code: refusal.Code, Fields: refusal.Fields}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&reply); err != nil {
		return formcardReply{}, &formcardError{Code: "unavailable"}
	}
	return reply, nil
}

func formcardPaths(conversation string) (string, string) {
	return "/api/chat/conversations/" + conversation + "/forms", "/api/chat/conversations/" + url.PathEscape(conversation) + "/forms"
}

// formcardRead asks what the reader may do with each of the given cards. A card
// the server does not describe is not a card this reader can open: it comes
// back loaded with nothing offered.
func formcardRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string, posts []string) (map[string]chatui.FormCardView, error) {
	path, raw := formcardPaths(conversation)
	reply, err := formcardCall(ctx, client, cfg, http.MethodGet, path, raw, url.Values{"post": posts}, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]chatui.FormCardView{}
	for _, id := range posts {
		out[id] = chatui.FormCardView{Loaded: true}
	}
	for id, view := range reply.Views {
		if _, asked := out[id]; asked {
			out[id] = formcardView(view)
		}
	}
	return out, nil
}

// formcardSubmit sends one card's answers.
func formcardSubmit(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, post string, values map[string]string) (chatui.FormCardView, error) {
	path, raw := formcardPaths(conversation)
	reply, err := formcardCall(ctx, client, cfg, http.MethodPost, path+"/"+post+"/answers", raw+"/"+url.PathEscape(post)+"/answers", nil, map[string]any{"values": values})
	if err != nil {
		return chatui.FormCardView{}, err
	}
	if reply.View == nil {
		return chatui.FormCardView{}, &formcardError{Code: "unavailable"}
	}
	return formcardView(*reply.View), nil
}
