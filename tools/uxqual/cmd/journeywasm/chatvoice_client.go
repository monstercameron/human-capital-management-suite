package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type voiceSendInput struct {
	TenantID, ConversationID, IdempotencyKey, ContentType, Note, Locale string
	Content                                                             []byte
	Waveform                                                            []float64
}
type voiceSendReply struct {
	Post  chat.Post
	Voice chat.VoiceRecord
}

func voiceRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, action string, input, output any) error {
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || cfg.Bearer == "" {
		return chat.ErrVoiceUnavailable
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return chat.ErrInvalidArgument
	}
	switch action {
	case "send", "read", "correct", "retry", "report":
	default:
		return chat.ErrInvalidArgument
	}
	endpoint.Path = "/api/chat/voice/" + action
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(raw) > 6<<20 {
		return chat.ErrInvalidArgument
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	r.Header.Set("Content-Type", "application/json")
	response, err := client.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return chat.ErrPermissionDenied
	case http.StatusBadRequest:
		return chat.ErrInvalidArgument
	default:
		return chat.ErrVoiceUnavailable
	}
	if output == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 2<<20))
		return err
	}
	return json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(output)
}
func voiceOriginSafe(raw string) bool {
	return chat.VoicePlaybackURL(raw)
}
