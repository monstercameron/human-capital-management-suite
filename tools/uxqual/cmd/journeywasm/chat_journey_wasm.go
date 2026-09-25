//go:build js && wasm

package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Journey quick looks for shared in-progress workflows in chat. The cache is
// identity-scoped like the project previews: a sign-in change discards every
// entry, and entries revalidate after chatJourneyFreshFor so a revoked grant
// does not keep showing a promotion's details.
const (
	chatJourneyFreshFor       = 15 * time.Second
	chatJourneyPendingTimeout = 20 * time.Second
	chatJourneyLimit          = 30
)

type chatJourneyEntry struct {
	value   chatui.JourneyPreview
	at      time.Time
	pending bool
}

var chatJourneys = struct {
	sync.Mutex
	identity string
	entries  map[string]chatJourneyEntry
}{}

func chatJourneyRefs(model chatui.Model) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(body string) {
		for _, ref := range chatui.JourneyReferences(body, model.EmbedOrigin) {
			if !seen[ref.IntentID] && len(ids) < chatJourneyLimit {
				seen[ref.IntentID] = true
				ids = append(ids, ref.IntentID)
			}
		}
	}
	add(model.Draft)
	for i := len(model.ThreadMessages) - 1; i >= 0; i-- {
		add(model.ThreadMessages[i].Body)
	}
	for i := len(model.Messages) - 1; i >= 0; i-- {
		add(model.Messages[i].Body)
	}
	for i := range model.SearchMessages {
		add(model.SearchMessages[i].Message.Body)
	}
	return ids
}

// chatJourneyProjection is what the model shows now for the visible ids.
func chatJourneyProjection(identity string, ids []string) map[string]chatui.JourneyPreview {
	chatJourneys.Lock()
	defer chatJourneys.Unlock()
	if chatJourneys.identity != identity {
		return nil
	}
	out := map[string]chatui.JourneyPreview{}
	for _, id := range ids {
		if entry, ok := chatJourneys.entries[id]; ok {
			out[id] = entry.value
		}
	}
	return out
}

func chatJourneyFingerprint(model chatui.Model) string {
	return model.SelectedID + "|" + strings.Join(chatJourneyRefs(model), "|")
}

// resolveVisibleChatJourneys reads each visible journey once per freshness
// window through InspectJourney, as the current viewer.
func resolveVisibleChatJourneys(cfg journeyclient.Config) {
	active := chatBrowser.config(cfg)
	client := chatWorkers
	if active.Tenant == "" || active.Subject == "" || client == nil {
		return
	}
	identity := active.Tenant + "\x00" + active.Subject
	now := time.Now()
	var claims []string
	chatJourneys.Lock()
	if chatJourneys.identity != identity || chatJourneys.entries == nil {
		chatJourneys.identity, chatJourneys.entries = identity, map[string]chatJourneyEntry{}
	}
	for _, id := range chatJourneyRefs(chatBrowser.snapshot()) {
		entry, ok := chatJourneys.entries[id]
		if ok && entry.pending && now.Sub(entry.at) < chatJourneyPendingTimeout {
			continue
		}
		if ok && !entry.pending && now.Sub(entry.at) < chatJourneyFreshFor {
			continue
		}
		entry.pending, entry.at = true, now
		if !ok {
			entry.value = chatui.JourneyPreview{IntentID: id, State: "loading"}
		}
		chatJourneys.entries[id] = entry
		claims = append(claims, id)
	}
	chatJourneys.Unlock()
	if len(claims) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		answers := map[string]chatui.JourneyPreview{}
		for _, id := range claims {
			answers[id] = readChatJourney(ctx, active, client, id)
		}
		ui.PostAsync(func() {
			chatJourneys.Lock()
			if chatJourneys.identity != identity {
				chatJourneys.Unlock()
				return
			}
			for id, value := range answers {
				chatJourneys.entries[id] = chatJourneyEntry{value: value, at: time.Now()}
			}
			chatJourneys.Unlock()
			chatBrowser.mutate(func(model *chatui.Model) {
				model.JourneyPreviews = chatJourneyProjection(identity, chatJourneyRefs(*model))
			})
			refreshChatRoute()
		})
	}()
}

// readChatJourney maps one InspectJourney answer onto a card. A refused or
// failed read is the neutral restricted card.
func readChatJourney(ctx context.Context, active journeyclient.Config, client journeyv1.JourneyServiceClient, id string) chatui.JourneyPreview {
	restricted := chatui.JourneyPreview{IntentID: id, State: "restricted"}
	response, err := client.InspectJourney(chatRPCContext(ctx, active), &journeyv1.InspectJourneyRequest{IntentId: id})
	journey := response.GetDetail().GetJourney()
	if err != nil || journey == nil || journey.GetIntentId() != id || strings.TrimSpace(journey.GetWorkerName()) == "" {
		return restricted
	}
	placement := func(p *journeyv1.Placement) string {
		if p == nil {
			return ""
		}
		parts := []string{}
		for _, v := range []string{p.GetJobCode(), p.GetGrade()} {
			if strings.TrimSpace(v) != "" {
				parts = append(parts, strings.TrimSpace(v))
			}
		}
		return strings.Join(parts, " · ")
	}
	stage := journey.GetStage().String()
	preview := chatui.JourneyPreview{
		IntentID: id, Readable: true, State: "ready", Worker: strings.TrimSpace(journey.GetWorkerName()),
		From: placement(journey.GetCurrent()), To: placement(journey.GetTarget()),
		Stage: stage, StageTone: chatui.JourneyStageGroup(stage), Effective: journey.GetEffectiveDate(),
	}
	if approver := strings.TrimSpace(response.GetDetail().GetApprover()); approver != "" {
		names, _ := projectDirectory(ctx, active)
		preview.Approver = chatDirectoryDisplayName(approver, names)
	}
	return preview
}

// chatDirectoryDisplayName names a principal from the chat directory, or a
// readable form of its subject, never a raw identifier.
func chatDirectoryDisplayName(subject string, names map[string]string) string {
	if name := strings.TrimSpace(names[subject]); name != "" {
		return name
	}
	trimmed := subject
	if i := strings.LastIndex(trimmed, ":"); i >= 0 {
		trimmed = trimmed[i+1:]
	}
	parts := strings.Split(trimmed, "-")
	words := []string{}
	for _, part := range parts {
		if part == "" || part == "hc" || strings.Trim(part, "0123456789") == "" {
			continue
		}
		words = append(words, strings.ToUpper(part[:1])+part[1:])
	}
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, " ")
}
