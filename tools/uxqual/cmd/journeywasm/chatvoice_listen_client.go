package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Listen (CHATVOICE-006) asks the server to read one message aloud. The answer
// is audio, played from memory and kept nowhere; these are the refusals the page
// words differently.
var (
	errListenOutsideBarred = errors.New("listen: the conversation never uses an outside service")
	errListenTooLong       = errors.New("listen: the message is too long to read aloud")
)

// voiceSpeakInput names one message, or with ThreadID a whole thread, to read.
// Names maps each author's id to the name the page shows, for a thread.
type voiceSpeakInput struct {
	TenantID, ConversationID, PostID string
	ThreadID                         string            `json:",omitempty"`
	Names                            map[string]string `json:",omitempty"`
}

const listenMaxBytes = 16 << 20

// voiceSpeech is the audio of a reading and, when the engine reported them, the
// sentences with the spans of the audio they occupy.
type voiceSpeech struct {
	Audio       []byte
	ContentType string
	Timings     []chatui.ListenTiming
}

func voiceSpeak(ctx context.Context, client *http.Client, cfg journeyclient.Config, input voiceSpeakInput) ([]byte, string, error) {
	speech, err := voiceSpeakTimed(ctx, client, cfg, input)
	return speech.Audio, speech.ContentType, err
}

func voiceSpeakTimed(ctx context.Context, client *http.Client, cfg journeyclient.Config, input voiceSpeakInput) (voiceSpeech, error) {
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" || cfg.Bearer == "" {
		return voiceSpeech{}, chat.ErrVoiceUnavailable
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return voiceSpeech{}, chat.ErrInvalidArgument
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "/api/chat/voice/speak", "", "", ""
	raw, err := json.Marshal(input)
	if err != nil {
		return voiceSpeech{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return voiceSpeech{}, err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return voiceSpeech{}, err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusConflict:
		return voiceSpeech{}, errListenOutsideBarred
	case http.StatusRequestEntityTooLarge:
		return voiceSpeech{}, errListenTooLong
	case http.StatusForbidden, http.StatusNotFound:
		return voiceSpeech{}, chat.ErrPermissionDenied
	case http.StatusBadRequest:
		return voiceSpeech{}, chat.ErrInvalidArgument
	default:
		return voiceSpeech{}, chat.ErrVoiceUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, listenMaxBytes+1))
	if err != nil || len(data) > listenMaxBytes || len(data) == 0 {
		return voiceSpeech{}, chat.ErrVoiceUnavailable
	}
	// No header, or one that does not parse, means no timings: nothing is marked.
	return voiceSpeech{Audio: data, ContentType: response.Header.Get("Content-Type"), Timings: chatui.ParseListenTimings(response.Header.Get(chatui.ListenTimingsHeader))}, nil
}

// listenStatusKey names the copy key that words an error for the reader.
func listenStatusKey(err error) string {
	switch {
	case errors.Is(err, errListenOutsideBarred):
		return "barred"
	case errors.Is(err, errListenTooLong):
		return "toolong"
	case errors.Is(err, chat.ErrVoiceUnavailable):
		return "unavailable"
	}
	return "failed"
}

// listenAutoplays is whether Listen starts playing once the audio is ready. It
// does when the person pressed Listen with a pointer. When the press came from a
// keyboard, a switch or an assistive technology's own activation (a click that
// carries no pointer detail), a screen reader is likely in use: the audio would
// talk over it, so the player is left ready for the person to start (CHATVOICE-006).
func listenAutoplays(clickDetail int) bool { return clickDetail > 0 }
