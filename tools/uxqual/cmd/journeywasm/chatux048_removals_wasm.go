//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatux048Removals reads what the moderation record says about removed
// messages, for a person who may review them: the same closed-items read the
// Moderation page's Resolved tab makes, so the server limits it to what that
// person may see. A person who may not review gets nothing and asks nothing, and
// a read that fails leaves the notice with the words every reader gets.
func chatux048Removals(ctx context.Context, client *http.Client, cfg journeyclient.Config, state chatui.ModerationState) map[string]chatui.ModerationRemoval {
	if !state.ReviewEverywhere && len(state.Reviewable) == 0 {
		return nil
	}
	reply, err := chatremoveRequest(ctx, client, cfg, "", "state:closed", nil)
	if err != nil {
		return nil
	}
	var queue struct{ Items []chat.ModerationItem }
	if json.Unmarshal(reply, &queue) != nil {
		return nil
	}
	return chatui.ModerationRemovalsFromItems(queue.Items)
}
