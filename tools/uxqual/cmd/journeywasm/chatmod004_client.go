package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// errChatmod005NoteRequired is raised before any request: Message the author
// sends the moderator's note, so there is nothing to send without one.
var errChatmod005NoteRequired = errors.New("chatmod: a note is required")

// errChatmod004NothingSelected is raised before any request: the form that
// removes the ticked messages was sent with none ticked.
var errChatmod004NothingSelected = errors.New("chatmod: no message is selected")

// chatmod004QuickWith removes (or restores) one message in one step for the
// person: it asks the server for the count, which must be exactly one, and then
// applies with the confirmation the server gave. Both calls are the ordinary
// preview and apply commands, so the server checks the permission twice.
func chatmod004QuickWith(ctx context.Context, client *http.Client, cfg journeyclient.Config, input chatremoveClientInput) ([]byte, error) {
	reply, err := chatremoveRequest(ctx, client, cfg, "preview", "", input)
	if err != nil {
		return nil, err
	}
	var preview chat.RemovalPreview
	if err = json.Unmarshal(reply, &preview); err != nil {
		return nil, chat.ErrUnavailable
	}
	if preview.Count != 1 || preview.Confirmation == "" {
		return nil, chat.ErrConflict
	}
	input.Removal.Confirmation, input.Removal.ConfirmedCount = preview.Confirmation, preview.Count
	return chatremoveRequest(ctx, client, cfg, "apply", "", input)
}

func chatmod004Quick(ctx context.Context, cfg journeyclient.Config, input chatremoveClientInput) ([]byte, error) {
	return chatmod004QuickWith(ctx, http.DefaultClient, cfg, input)
}

// chatmod005SummaryWith reads the viewer's moderation summary.
func chatmod005SummaryWith(ctx context.Context, client *http.Client, cfg journeyclient.Config) (chat.ModerationSummary, error) {
	var summary chat.ModerationSummary
	reply, err := chatremoveRequest(ctx, client, cfg, "summary", "", nil)
	if err != nil {
		return summary, err
	}
	if err = json.Unmarshal(reply, &summary); err != nil {
		return chat.ModerationSummary{}, chat.ErrUnavailable
	}
	return summary, nil
}
