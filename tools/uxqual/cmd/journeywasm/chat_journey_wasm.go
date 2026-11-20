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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// chatJourneyRefs are the journeys the visible messages need read; the typed
// references on a post count as much as an address in its body (issue 3725).
func chatJourneyRefs(model chatui.Model) []string {
	return chatJourneyPreviewIDs(model)
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
		// Signed out: no stream may keep running under the old principal (#3727).
		stopChatJourneyWatches()
		return
	}
	identity := active.Tenant + "\x00" + active.Subject
	now := time.Now()
	var claims []string
	identityChanged := false
	chatJourneys.Lock()
	if chatJourneys.identity != identity || chatJourneys.entries == nil {
		identityChanged = chatJourneys.identity != identity
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
	if identityChanged {
		// A reason typed by one person is never shown to the next (#3724).
		chatBrowser.mutate(func(model *chatui.Model) { model.JourneyDecisions = nil })
	}
	syncChatJourneyWatches(active, identity)
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

// retryChatJourney reads one card again after a temporary failure (#3729): the
// cached failure is dropped so the card shows its loading state, then the
// normal resolver reads it as the current viewer.
func retryChatJourney(cfg journeyclient.Config, intentID string) {
	chatJourneys.Lock()
	entry, known := chatJourneys.entries[intentID]
	identity := chatJourneys.identity
	// A read already under way is the retry; a second press must not start
	// another one, and only a failed card can be retried.
	if !known || entry.pending || entry.value.State != "unavailable" {
		chatJourneys.Unlock()
		return
	}
	delete(chatJourneys.entries, intentID)
	chatJourneys.Unlock()
	resolveVisibleChatJourneys(cfg)
	chatJourneys.Lock()
	if claimed, ok := chatJourneys.entries[intentID]; ok && claimed.pending && chatJourneys.identity == identity {
		claimed.value.Retrying = true
		chatJourneys.entries[intentID] = claimed
	}
	chatJourneys.Unlock()
	chatBrowser.mutate(func(model *chatui.Model) {
		model.JourneyPreviews = chatJourneyProjection(identity, chatJourneyRefs(*model))
	})
	refreshChatRoute()
}

// chatJourneyFailureState names the card state for a failed InspectJourney
// call (#3729): a missing journey, a refused read and a temporary failure
// each get their own copy, so a viewer can tell them apart.
func chatJourneyFailureState(err error) string {
	switch status.Code(err) {
	case codes.NotFound:
		return "missing"
	case codes.PermissionDenied, codes.Unauthenticated:
		return "restricted"
	}
	return "unavailable"
}

// readChatJourney maps one InspectJourney answer onto a card.
func readChatJourney(ctx context.Context, active journeyclient.Config, client journeyv1.JourneyServiceClient, id string) chatui.JourneyPreview {
	response, err := client.InspectJourney(chatRPCContext(ctx, active), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil {
		return chatui.JourneyPreview{IntentID: id, State: chatJourneyFailureState(err)}
	}
	return chatJourneyPreviewFromDetail(ctx, active, id, response.GetDetail())
}

// chatJourneyPreviewFromDetail maps one authorized journey detail, from a read
// or from the live watch (#3727), onto a card.
func chatJourneyPreviewFromDetail(ctx context.Context, active journeyclient.Config, id string, detail *journeyv1.JourneyDetail) chatui.JourneyPreview {
	response := &journeyv1.InspectJourneyResponse{Detail: detail}
	restricted := chatui.JourneyPreview{IntentID: id, State: "restricted"}
	journey := response.GetDetail().GetJourney()
	if journey == nil || journey.GetIntentId() != id || strings.TrimSpace(journey.GetWorkerName()) == "" {
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
		// The claim rests on the stable subject, never on the display name (#3994).
		preview.WaitsOnViewer = chatui.JourneyWaitsOnSubject(stage, approver, strings.TrimSpace(active.Subject))
	}
	// Requester, waiting-since and decision detail come from the same
	// authorized answer (#3730). The service names the initiator only to the
	// initiator, so the card says "Requested by you" and nothing more.
	events := make([]chatui.JourneyStatusEvent, 0, len(response.GetDetail().GetTimeline()))
	for _, e := range response.GetDetail().GetTimeline() {
		var at time.Time // a missing instant stays zero; AsTime would say 1970
		if e.GetAt() != nil {
			at = e.GetAt().AsTime()
		}
		events = append(events, chatui.JourneyStatusEvent{At: at, Kind: e.GetKind(), Actor: e.GetActor(), Title: e.GetTitle(), Detail: e.GetDetail()})
	}
	initiator := false
	for _, r := range journey.GetViewer().GetRelationships() {
		if r == journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR {
			initiator = true
		}
	}
	facts := chatui.JourneyStatusFacts(stage, events, initiator)
	preview.RequestedByViewer, preview.WaitingSince = facts.RequestedByViewer, facts.WaitingSince
	preview.DecidedBy, preview.DecisionReason = facts.DecidedBy, facts.DecisionReason
	// When the service last updated this state (#3980); only the requester's
	// card reads it, and a missing instant stays empty rather than 1970.
	if initiator && journey.GetUpdatedAt() != nil && journey.GetUpdatedAt().IsValid() {
		preview.Confirmed = journey.GetUpdatedAt().AsTime().UTC().Format(time.RFC3339)
	}
	// Help me resolve this (#3978): the authoritative findings reduced to
	// severity and code (never the engine's prose, which can carry pay), with
	// the revision they were read for and the journey's next step.
	if initiator {
		resolve := make([]chatui.JourneyResolveFinding, 0, len(response.GetDetail().GetFindings()))
		for _, f := range response.GetDetail().GetFindings() {
			resolve = append(resolve, chatui.JourneyResolveFinding{Severity: f.GetSeverity(), Code: f.GetCode()})
		}
		preview.ResolveFindings = chatui.EncodeJourneyResolveFindings(resolve)
		preview.Revision = strings.TrimSpace(journey.GetProposalRevisionId())
		preview.NextStep = journey.GetViewer().GetNextStep().String()
	}
	// The routed approver binds a decision to the revision read here (#3724).
	if preview.Revision == "" {
		preview.Revision = journeyDecisionRevision(initiator, preview.WaitsOnViewer, journey.GetProposalRevisionId())
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
