package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3793: the browser side of typed requests (/leave). Three plain JSON
// calls: which request commands this person may use in the conversation, check
// a request and read back what would be sent, and send it. Nothing here decides
// what is allowed or valid: the server does, and the client shows its word. The
// page never names the intent, the schema or the agent that takes it in.

type requestAvailableJSON struct {
	Command   string `json:"command"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

type requestPreviewJSON struct {
	Command string            `json:"command"`
	Values  map[string]string `json:"values"`
	Via     string            `json:"via"`
}

type requestReceiptJSON struct {
	Command  string `json:"command"`
	IntentID string `json:"intent_id"`
	Href     string `json:"href"`
}

type requestReply struct {
	Commands []requestAvailableJSON `json:"commands"`
	Preview  *requestPreviewJSON    `json:"preview"`
	Receipt  *requestReceiptJSON    `json:"receipt"`
}

// requestError is a refusal reduced to the server's short code. Fields names
// the refused fields of an invalid request by their own codes; Reason says why
// a command is not available.
type requestError struct {
	Code   string
	Reason string
	Fields map[string]string
}

func (e *requestError) Error() string { return e.Code }

func requestPaths(conversation string) (string, string) {
	return "/api/chat/conversations/" + conversation + "/requests", "/api/chat/conversations/" + url.PathEscape(conversation) + "/requests"
}

func requestCall(ctx context.Context, client *http.Client, cfg journeyclient.Config, method, path, rawPath string, payload any) (requestReply, error) {
	var reply requestReply
	endpoint, problem := tunnelHTTPBase(cfg.TunnelURL)
	if problem != tunnelOK || cfg.Bearer == "" || client == nil {
		return reply, &requestError{Code: "unavailable"}
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = path, rawPath, "", ""
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
		return reply, &requestError{Code: "unavailable"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal struct {
			Code   string            `json:"code"`
			Reason string            `json:"reason"`
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
				refusal.Code = "not_available"
			case http.StatusBadRequest:
				refusal.Code = "invalid_argument"
			default:
				refusal.Code = "unavailable"
			}
		}
		return requestReply{}, &requestError{Code: refusal.Code, Reason: refusal.Reason, Fields: refusal.Fields}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&reply); err != nil {
		return requestReply{}, &requestError{Code: "unavailable"}
	}
	return reply, nil
}

// requestRead asks which request commands this person may use here. A command
// the server did not describe is not offered.
func requestRead(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation string) (map[string]chatui.IntentRequestAvailable, error) {
	path, raw := requestPaths(conversation)
	reply, err := requestCall(ctx, client, cfg, http.MethodGet, path, raw, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]chatui.IntentRequestAvailable{}
	for _, c := range reply.Commands {
		if c.Command != "" {
			out[c.Command] = chatui.IntentRequestAvailable{Loaded: true, Available: c.Available, Reason: c.Reason}
		}
	}
	return out, nil
}

// requestPreview checks a request and returns the agent that would take it in.
func requestPreview(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, command string, values map[string]string) (string, error) {
	path, raw := requestPaths(conversation)
	reply, err := requestCall(ctx, client, cfg, http.MethodPost, path+"/"+command+"/preview", raw+"/"+url.PathEscape(command)+"/preview", map[string]any{"values": values})
	if err != nil {
		return "", err
	}
	if reply.Preview == nil {
		return "", &requestError{Code: "unavailable"}
	}
	return reply.Preview.Via, nil
}

// requestSend sends one reviewed request. submission names this one act: a
// retry of the same reviewed request reuses it, a different request does not.
func requestSend(ctx context.Context, client *http.Client, cfg journeyclient.Config, conversation, command, submission string, values map[string]string) (string, error) {
	path, raw := requestPaths(conversation)
	reply, err := requestCall(ctx, client, cfg, http.MethodPost, path+"/"+command, raw+"/"+url.PathEscape(command), map[string]any{"values": values, "submission": submission})
	if err != nil {
		return "", err
	}
	if reply.Receipt == nil || reply.Receipt.IntentID == "" {
		return "", &requestError{Code: "unavailable"}
	}
	return reply.Receipt.Href, nil
}
