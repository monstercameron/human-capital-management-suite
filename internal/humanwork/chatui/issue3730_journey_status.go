package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// JourneyStatusEvent is one entry of the journey's timeline as the journey
// service already authorized it for the viewer (#3730).
type JourneyStatusEvent struct {
	At                 time.Time
	Kind, Actor, Title string
	Detail             string
}

// JourneyStatusFactsResult is what the card adds to the stage: whether the
// viewer started the request, since when an approval has been waiting, and
// the decision with its decider and reason.
type JourneyStatusFactsResult struct {
	RequestedByViewer         bool
	WaitingSince              string // YYYY-MM-DD
	DecidedBy, DecisionReason string
}

const journeyReasonLimit = 160

// journeyDecisionReasonMarker is the journey service's prefix for a decision
// reason in a timeline detail.
const journeyDecisionReasonMarker = "decision_reason:"

// JourneyStatusFacts derives the card's status detail from the timeline. The
// service names the requester only to the requester, so the card says
// "Requested by you" and never guesses at anyone else. A decider the service
// could not name (or the workflow itself) is left out rather than invented.
func JourneyStatusFacts(stage string, events []JourneyStatusEvent, viewerIsInitiator bool) JourneyStatusFactsResult {
	out := JourneyStatusFactsResult{RequestedByViewer: viewerIsInitiator}
	last := func(suffixes ...string) (JourneyStatusEvent, bool) {
		var found JourneyStatusEvent
		ok := false
		for _, e := range events {
			if e.Kind != "WORK_ITEM" || e.At.IsZero() {
				continue
			}
			title := strings.ToLower(strings.TrimSpace(e.Title))
			for _, s := range suffixes {
				if strings.HasSuffix(title, s) && (!ok || !e.At.Before(found.At)) {
					found, ok = e, true
				}
			}
		}
		return found, ok
	}
	if journeyStageAwaitsApprover(stage) {
		if e, ok := last(" assigned"); ok {
			out.WaitingSince = e.At.UTC().Format("2006-01-02")
		}
	}
	// The journey service titles a decided approval "<step> approved" or
	// "<step> rejected" and carries its reason as "decision_reason:<text>".
	// Only a viewer the service lets see actors and details receives them;
	// everyone else gets "reviewer" and an empty detail, and the card then
	// shows no decider and no reason rather than guessing.
	if stage == "JOURNEY_STAGE_REJECTED" || stage == "JOURNEY_STAGE_COMPLETED" {
		suffix := " approved"
		if stage == "JOURNEY_STAGE_REJECTED" {
			suffix = " rejected"
		}
		if e, ok := last(suffix); ok {
			if who := strings.TrimSpace(e.Actor); who != "" && who != "system" && who != "reviewer" {
				out.DecidedBy = who
			}
			if detail := strings.TrimSpace(e.Detail); strings.HasPrefix(detail, journeyDecisionReasonMarker) {
				reason := strings.Join(strings.Fields(strings.TrimPrefix(detail, journeyDecisionReasonMarker)), " ")
				if r := []rune(reason); len(r) > journeyReasonLimit {
					reason = string(r[:journeyReasonLimit-1]) + "…"
				}
				out.DecisionReason = reason
			}
		}
	}
	return out
}

var journeyStatusWords = map[string][3]string{
	"requested": {"Requested by you", "Von Ihnen beantragt", "قدّمتَ هذا الطلب"},
	"waiting":   {"Waiting since {date}", "Wartet seit {date}", "بالانتظار منذ {date}"},
	"decided":   {"Decided by {name}", "Entschieden von {name}", "قرّر ذلك {name}"},
	"reason":    {"Reason: {reason}", "Grund: {reason}", "السبب: {reason}"},
}

func journeyStatusText(m Model, key string, vars map[string]string) string {
	words := journeyStatusWords[key]
	text := chatbug039Text(key, words[chatbug039LocaleIndex(m.Locale)], words[0])
	for k, v := range vars {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}

// journeyStatusMeta is the extra meta row of the journey card.
func journeyStatusMeta(m Model, p JourneyPreview) []ui.Node {
	var out []ui.Node
	add := func(class, text string) {
		out = append(out, html.Span(html.Props{Class: class, Dir: "auto", Text: text}))
	}
	if p.RequestedByViewer {
		add("chat-journey-embed-fact", journeyStatusText(m, "requested", nil))
	}
	if p.StageTone != "done" && p.WaitingSince != "" && journeyStageAwaitsApprover(p.Stage) {
		if since, err := time.Parse("2006-01-02", p.WaitingSince); err == nil {
			add("chat-journey-embed-fact", journeyStatusText(m, "waiting", map[string]string{"date": formatShortDate(m.Locale, since)}))
		}
	}
	if p.StageTone == "done" {
		if p.DecidedBy != "" {
			add("chat-journey-embed-fact", journeyStatusText(m, "decided", map[string]string{"name": p.DecidedBy}))
		}
		if p.DecisionReason != "" {
			add("chat-journey-embed-fact chat-journey-embed-reason", journeyStatusText(m, "reason", map[string]string{"reason": p.DecisionReason}))
		}
	}
	return out
}
