package application

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// CallAgentFeature is one idea for what an agent may do with a call, with
// its verdict under CHATCALL-005. Permitted features name the consent they
// need and what a decline does; the rest say whether they are blocked or
// ruled out, and why.
type CallAgentFeature struct {
	ID        string
	Name      string
	Permitted bool
	Consent   string
	OnDecline string
	Mechanism string
}

// CallAgentFeatures returns the verdicts in contract order: the three
// ideas the contract asks about, then the four prohibitions.
func CallAgentFeatures() []CallAgentFeature {
	return []CallAgentFeature{
		{ID: "LIVE_CAPTIONS", Name: "Live captions for accessibility", Permitted: false,
			Consent:   "Not permitted and blocked: as specified — an in-deployment speech model with nothing stored — there is no model to consent to. The only speech path is the outside-service gateway, which would need every participant's consent plus the channel's outside-service switch.",
			OnDecline: "Blocked features have no decline path yet; if captions are ever offered, one decline disables them for all.",
			Mechanism: "No in-deployment speech model exists: CHATVOICE-001 decision 2 routes transcription through the agent model gateway to OpenAI. Live captions need a model built inside the deployment, or a consented outside-service path that sends live audio out."},
		{ID: "AFTER_CALL_SUMMARY", Name: "After-call summary, tasks and reminders", Permitted: true,
			Consent:   "The consent of every participant to the transcript, given before the call under the CHATCALL-005 strictest-wins rule. No transcript, no summary.",
			OnDecline: "One decline means no transcript is made, so no summary, tasks or reminders follow; the call itself is unaffected.",
			Mechanism: "Task Catcher and Reminder read the consented transcript under the AGENTUX-066 installation grants, with the shared AgentUXAmbientAudienceFor scope deciding what is offered publicly and what stays private."},
		{ID: "PRIVATE_NOTES", Name: "Private notes for me", Permitted: true,
			Consent:   "The consent of every participant, because other people's speech is processed, plus the requester's own request.",
			OnDecline: "One decline disables the feature for that call, and the requester is told plainly that notes are off because not everyone agreed.",
			Mechanism: "Delivered only to the requester in their conversation with the agent, never posted to the call's conversation, and never visible to anyone else."},
		{ID: "SILENT_ATTENDANCE", Name: "Silent attendance", Permitted: false,
			Consent:   "No consent can permit it — ruled out: an agent that attends without every participant knowing is secret surveillance.",
			OnDecline: "Ruled out: there is nothing to decline because the feature cannot exist.",
			Mechanism: "AGENTUX-066 makes agent reads visible (the header names the readers, members can opt out); a silent attendee breaks that visibility by design."},
		{ID: "SCORING_ASSESSMENT", Name: "Scoring or assessing a participant", Permitted: false,
			Consent:   "No consent can permit it — ruled out: no score or assessment of a participant is ever derived from call speech.",
			OnDecline: "Ruled out: there is nothing to decline because the feature cannot exist.",
			Mechanism: "Call speech is conversation, not evidence of performance; scoring it would make every call a monitored examination."},
		{ID: "SENTIMENT_EMOTION", Name: "Sentiment or emotion inference", Permitted: false,
			Consent:   "No consent can permit it — ruled out: CHATVOICE-001 decision 5 already forbids emotion inference from voice, and calls inherit the ban.",
			OnDecline: "Ruled out: there is nothing to decline because the feature cannot exist.",
			Mechanism: "Emotion inference from workplace speech is employee monitoring with a model; the product does not do it for voice messages and does not do it for calls."},
		{ID: "EMPLOYMENT_DECISIONS", Name: "Any use in an employment decision", Permitted: false,
			Consent:   "No consent can permit it — ruled out: no call-derived content — transcript, summary, notes — is ever used in an employment decision.",
			OnDecline: "Ruled out: there is nothing to decline because the use is forbidden.",
			Mechanism: "A disciplinary conversation that feeds its own verdict through a call summary destroys trust in both calls and summaries; the boundary is absolute."},
	}
}

// CallSpeechFitnessReport is the fitness finding for the in-deployment speech
// model CHATCALL-007 asks about: whether one exists, and whether it was
// measured on recorded test audio.
type CallSpeechFitnessReport struct {
	Model             string
	Available         bool
	Latency           string
	SpeakerSeparation string
	Languages         string
	Procedure         string
	Status            string
}

// CallSpeechFitness returns the finding. There is no in-deployment speech
// model — CHATVOICE-003 transcribes through the agent model gateway — so
// fitness is unevaluated and the procedure below is the contract a future
// model must satisfy.
func CallSpeechFitness() CallSpeechFitnessReport {
	return CallSpeechFitnessReport{
		Model:             "in-deployment speech model",
		Available:         false,
		Latency:           "Unevaluated: no model exists to measure. Live use needs partial results within about a second of speech; batch transcription latency says nothing about that.",
		SpeakerSeparation: "Unevaluated: no model exists to measure. A caption stream that cannot tell speakers apart is unusable past two people.",
		Languages:         "The product's three languages — en-US, de-DE, ar — with the CHATVOICE-003 golden ceilings (0.10, 0.15, 0.25 word error) as the accuracy reference for any future evaluation.",
		Procedure:         "Recorded test audio in en-US, de-DE and ar through the candidate model, reporting latency to first partial transcript, speaker-separation quality on overlapping speech, and word error per language against the golden ceilings.",
		Status:            "NOT_RUN",
	}
}

// RenderCallAgents is the stable text form used by the golden test.
func RenderCallAgents() string {
	var b strings.Builder
	b.WriteString("CHATCALL-007 call agent permissions recorded 2026-10-22\n")
	b.WriteString("against CHATCALL-005, CHATVOICE-003 and AGENTUX-066\n")
	for _, f := range CallAgentFeatures() {
		fmt.Fprintf(&b, "\n%s %s (permitted=%v)\nconsent: %s\non decline: %s\nmechanism: %s\n",
			f.ID, f.Name, f.Permitted, f.Consent, f.OnDecline, f.Mechanism)
	}
	s := CallSpeechFitness()
	fmt.Fprintf(&b, "\nspeech model: %s (available=%v)\nlatency: %s\nspeakers: %s\nlanguages: %s\nprocedure: %s\nstatus: %s\n",
		s.Model, s.Available, s.Latency, s.SpeakerSeparation, s.Languages, s.Procedure, s.Status)
	return b.String()
}

// CallAgentsDigest is the digest of the rendered record.
func CallAgentsDigest() string {
	sum := sha256.Sum256([]byte(RenderCallAgents()))
	return hex.EncodeToString(sum[:])
}
