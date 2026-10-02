package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

const chattoneHTTPPath = "/api/chat-writing-style"

type chattoneDraftRequest struct {
	ConversationID string `json:"conversation_id"`
	Draft          string `json:"draft"`
	StyleID        string `json:"style_id"`
}
type chattoneHTTPReply struct {
	Draft      string                  `json:"draft,omitempty"`
	Suggestion *chatrewrite.Suggestion `json:"suggestion,omitempty"`
	Styles     []chatrewrite.Style     `json:"styles,omitempty"`
	Enabled    bool                    `json:"enabled"`
	// Available is false when the server did not compose the service; it is
	// absent on a normal answer.
	Available *bool `json:"available,omitempty"`
}
type chattoneRequestError struct{ Code string }

// chattoneNotAvailable is the code for the server's typed "this service is not
// composed" answer (a 200 body of {"available":false}).
const chattoneNotAvailable = "not_available"

// chattoneSession is the page session's memory of that answer: once the server
// says the writing style service is not available, no room asks again.
type chattoneSession struct{ off bool }

func (s *chattoneSession) Allow() bool { return !s.off }
func (s *chattoneSession) Record(err error) {
	if err != nil && chattoneErrorCode(err) == chattoneNotAvailable {
		s.off = true
	}
}

func (e chattoneRequestError) Error() string { return "writing style: " + e.Code }
func chattoneErrorCode(err error) string {
	var typed chattoneRequestError
	if errors.As(err, &typed) {
		return typed.Code
	}
	return "unavailable"
}
func chattoneRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, draft chattoneDraftRequest) (chattoneHTTPReply, error) {
	var result chattoneHTTPReply
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || cfg.Bearer == "" || client == nil {
		return result, chattoneRequestError{"unavailable"}
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return result, chattoneRequestError{"unavailable"}
	}
	endpoint.Path = chattoneHTTPPath + "/" + action
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	var body io.Reader
	method := http.MethodGet
	switch action {
	case "suggestion":
		q := url.Values{}
		q.Set("conversation_id", draft.ConversationID)
		endpoint.RawQuery = q.Encode()
	case "rewrite":
		if len(strings.Fields(draft.Draft)) < 3 || len(draft.Draft) > chatrewrite.MaxDraftBytes || draft.StyleID == "" {
			return result, chattoneRequestError{"invalid"}
		}
		data, _ := json.Marshal(draft)
		body = bytes.NewReader(data)
		method = http.MethodPost
	default:
		return result, chattoneRequestError{"invalid"}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, 48<<10))
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = decoder.Decode(&failure)
		switch failure.Error {
		case "limit", "preservation", "policy", "invalid", "denied", "disabled":
		default:
			failure.Error = "unavailable"
		}
		return result, chattoneRequestError{failure.Error}
	}
	if decoder.Decode(&result) != nil {
		return result, chattoneRequestError{"unavailable"}
	}
	if result.Available != nil && !*result.Available {
		return chattoneHTTPReply{}, chattoneRequestError{chattoneNotAvailable}
	}
	if action == "rewrite" && (!result.Enabled || result.Draft == "" || len(result.Draft) > chatrewrite.MaxDraftBytes) {
		return chattoneHTTPReply{}, chattoneRequestError{"unavailable"}
	}
	return result, nil
}

type chattonePreview struct {
	Original, Rewritten string
	Busy, ShowChanges   bool
	Serial              uint64
}

func (p *chattonePreview) Begin(draft string) (uint64, bool) {
	if p.Busy || len(strings.Fields(draft)) < 3 {
		return 0, false
	}
	p.Busy = true
	p.Serial++
	return p.Serial, true
}
func (p *chattonePreview) Complete(serial uint64, typed, current, rewrite string) bool {
	if serial != p.Serial || !p.Busy {
		return false
	}
	p.Busy = false
	if current != typed || rewrite == "" {
		return false
	}
	p.Original = typed
	p.Rewritten = rewrite
	p.ShowChanges = false
	return true
}
func (p *chattonePreview) Undo() (string, bool) {
	if p.Busy || p.Original == "" {
		return "", false
	}
	original := p.Original
	p.Original = ""
	p.Rewritten = ""
	p.ShowChanges = false
	p.Serial++
	return original, true
}
func (p *chattonePreview) Clear() {
	p.Original = ""
	p.Rewritten = ""
	p.ShowChanges = false
	p.Busy = false
	p.Serial++
}

func (p *chattonePreview) Edit(text string) {
	if text == "" {
		p.Clear()
		return
	}
	if !p.Busy && p.Original != "" {
		p.Rewritten = text
	}
}
