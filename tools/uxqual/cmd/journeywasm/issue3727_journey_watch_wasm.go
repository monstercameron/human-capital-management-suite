//go:build js && wasm

package main

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatJourneyWatches is the page's live feed for the journey cards in view
// (#3727). The periodic re-read in chat_journey_wasm.go stays as the fallback.
var chatJourneyWatches = &chatJourneyWatchHub{
	Max: chatJourneyWatchMax, Retry: chatJourneyWatchRetry,
	Open: openChatJourneyWatch, Deliver: deliverChatJourneyChange, Fail: failChatJourneyWatch,
}

// openChatJourneyWatch opens one stream as the signed-in viewer; the hub closes
// every stream when the identity changes, and a late answer for another identity
// is dropped when it is delivered.
func openChatJourneyWatch(ctx context.Context, id, since string) (func() (*journeyv1.JourneyDetail, error), error) {
	client := chatWorkers
	if client == nil {
		return nil, status.Error(codes.Unavailable, "journey service is not ready")
	}
	active := chatBrowser.config(journeyclient.Config{})
	stream, err := client.WatchJourney(chatRPCContext(ctx, active), &journeyv1.WatchJourneyRequest{IntentId: id, SinceDigest: since})
	if err != nil {
		return nil, err
	}
	return func() (*journeyv1.JourneyDetail, error) {
		msg, err := stream.Recv()
		if err != nil {
			return nil, err
		}
		return msg.GetDetail(), nil
	}, nil
}

// syncChatJourneyWatches opens a stream for each visible journey card as the
// current viewer and closes the rest.
func syncChatJourneyWatches(_ journeyclient.Config, identity string) {
	if chatWorkers == nil {
		return
	}
	chatJourneyWatches.Sync(identity, chatJourneyRefs(chatBrowser.snapshot()))
}

func stopChatJourneyWatches() { chatJourneyWatches.Stop() }

// deliverChatJourneyChange puts a pushed detail on its card. The card is only
// repainted when something the viewer can see changed.
func deliverChatJourneyChange(identity, id string, detail *journeyv1.JourneyDetail) {
	active := chatBrowser.config(journeyclient.Config{})
	ctx := context.Background()
	preview := chatJourneyPreviewFromDetail(ctx, active, id, detail)
	setChatJourneyPreview(identity, id, preview)
}

// failChatJourneyWatch shows a refused watch the way a refused read shows.
func failChatJourneyWatch(identity, id string, err error) {
	setChatJourneyPreview(identity, id, chatui.JourneyPreview{IntentID: id, State: chatJourneyFailureState(err)})
}

func setChatJourneyPreview(identity, id string, preview chatui.JourneyPreview) {
	ui.PostAsync(func() {
		chatJourneys.Lock()
		if chatJourneys.identity != identity || chatJourneys.entries == nil {
			chatJourneys.Unlock()
			return
		}
		entry, known := chatJourneys.entries[id]
		if known && !entry.pending && entry.value == preview {
			// Same card as already shown: keep it fresh, do not repaint.
			entry.at = time.Now()
			chatJourneys.entries[id] = entry
			chatJourneys.Unlock()
			return
		}
		chatJourneys.entries[id] = chatJourneyEntry{value: preview, at: time.Now()}
		chatJourneys.Unlock()
		chatBrowser.mutate(func(model *chatui.Model) {
			model.JourneyPreviews = chatJourneyProjection(identity, chatJourneyRefs(*model))
		})
		refreshChatRoute()
	})
}
