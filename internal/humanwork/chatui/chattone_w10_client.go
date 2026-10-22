package chatui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// SendAsWritten tells the server the writer wants others to read this message
// as it was written. The server accepts it only from the message's writer and
// only where members may read messages as written.
func (c ReadingSettingsClient) SendAsWritten(ctx context.Context, room, post string) error {
	body, err := json.Marshal(struct {
		Message string `json:"message"`
	}{post})
	if err != nil {
		return err
	}
	response, err := c.requestEndpoint(ctx, http.MethodPost, "send-as-written", room, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return readingStatusError(response.StatusCode)
	}
	return nil
}

// ReportTranslation sends a reader's "this translation is wrong" with their
// optional better wording.
func (c ReadingSettingsClient) ReportTranslation(ctx context.Context, room string, rendering chatrender.Rendering, reason string) error {
	body, err := json.Marshal(struct {
		Rendering chatrender.Rendering `json:"rendering"`
		Reason    string               `json:"reason"`
	}{rendering, reason})
	if err != nil {
		return err
	}
	response, err := c.requestEndpoint(ctx, http.MethodPost, "report", room, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return readingStatusError(response.StatusCode)
	}
	return nil
}
