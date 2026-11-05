package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// CallDecision is one recorded answer to the scope questions of CHATCALL-001:
// what kinds of calls the product would offer, to whom, what a call leaves
// behind, how ringing respects attention, whether anything is recorded, what
// is out of scope, and where calls end and voice messages begin. The record
// is code so a test can hold later work to it, and so that adopting,
// narrowing or rejecting any answer has to be seen. None of it is a decision
// to build.
type CallDecision struct {
	Number int
	Topic  string
	// Decision is the proposal, Rejected the alternatives turned down, Why
	// the reason.
	Decision string
	Rejected string
	Why      string
}

// CallDecisionsRecordedOn is when the record was written (owner request
// 2026-10-01: "research webrtc for chat based voice and video calls").
const CallDecisionsRecordedOn = "2026-10-22"

// CallDecisions returns the record in question order.
func CallDecisions() []CallDecision {
	return []CallDecision{
		{1, "call kinds in delivery order and their ceilings",
			"The product would offer four call kinds in this order. One-to-one voice, ceiling two people. One-to-one video, ceiling two people. A drop-in voice call in a group or channel, ceiling twenty-five people, audio-first: at most nine video tiles are shown and everyone else is heard, not seen. Screen sharing, one sharer at a time, inside any video-capable call rather than a kind of its own. Each step reuses the media path of the previous one, and the ceilings bound bandwidth and server cost until CHATCALL-002 measures them.",
			"Starting with group video for everyone, a webinar or broadcast kind, an unlimited drop-in, and screen sharing as a separate call kind.",
			"A drop-in that shows twenty-five cameras needs a forwarding server on day one; an audio-first drop-in can start on the one-to-one path. A webinar is a different product (presenters, audience, moderation) with no Chat membership to govern it. Ceilings stated now keep topology, cost and compliance honest later."},
		{2, "who may start a call where",
			"Any active member of a conversation may start a call there, following the conversation's existing membership and policy: the same active-membership check that admits a post (chat.Service over Membership, left members excluded) admits a caller, in the caller's tenant only. One-to-one voice and video may start in a direct or group conversation; a drop-in may start in a group or channel. A channel manager may end a drop-in for everyone; anyone may leave any call. A person removed from the conversation mid-call is disconnected, enforced by the per-message membership check of CHATCALL-003.",
			"Manager-only starting, starting without membership (link guests), and calls that survive removal from the conversation.",
			"Posting authority is already membership plus policy; a second, weaker authority for live media would be the bypass. Guests have no membership to check, so there is nothing to enforce consent or removal against."},
		{3, "what a call leaves behind",
			"A call leaves two system lines in the conversation and nothing else by default: \"Call started\" when media connects, and \"Call ended · 12 min · 3 people\" when it ends, naming duration and peak participants. No audio, no video and no transcript is stored. A one-to-one call that rings unanswered leaves \"Missed call\" instead. The lines follow the system-line rules (CHATUX-021): visible in the timeline, left out of agent context unless asked for.",
			"Storing media by default, and silent calls with no history at all.",
			"That a call happened is a fact worth one line in an employment record; everything beyond it needs the consent of decision 5. Silent calls cannot be audited, explained or billed."},
		{4, "presence, ringing, quiet hours and do-not-disturb",
			"Presence stays today's Online signal: no new \"in a call\" presence at launch, no offline labels for members without a signal. A one-to-one call rings the other person for thirty seconds, then gives up and leaves \"Missed call\"; a drop-in never rings and shows a join banner instead. Ringing honours QuietHours and NotificationPreferences: a muted conversation and a person inside quiet hours are never sounded, and the attempt becomes a missed-call line without sound. Do-not-disturb is exactly those two switches, not a third state. Ringing on phones and background tabs is decided by CHATCALL-004; the server rules here do not change for it.",
			"Ringing a whole channel, new presence states, and ringing through quiet hours or a mute.",
			"People already configured quiet hours and mutes; a call that ignores them teaches everyone that the switches are decorative. A ringing channel is a denial of service on attention, and a banner joins just as well."},
		{5, "recording, live captions and transcripts",
			"Recording, live captions and after-call transcripts are not offered at launch. If they are ever offered, the product follows the strictest rule everywhere: every participant is told before it starts, a continuous on-screen indicator shows while it runs, and any participant may decline by leaving — one decline stops the recording or transcript for all while the call itself continues. Stored recordings and transcripts would follow the conversation's retention and hold rules exactly as CHATVOICE-005 requires for voice messages, decided in CHATCALL-005. One party's consent is not enough in, at least, the US all-party states (California, Connecticut, Florida, Illinois, Maryland, Massachusetts, Montana, New Hampshire, Pennsylvania, Washington), the European Union and Germany in particular (employee monitoring needs the works council's agreement), the United Kingdom, the United Arab Emirates and Saudi Arabia; counsel confirms the list before anything is built.",
			"A one-party-consent default, recording without a continuous indicator, and per-participant recording where some are recorded and some are not.",
			"This is an HR product and a call may be a disciplinary conversation; a recording that is lawful for one participant and unlawful for another is a liability. One strict behaviour everywhere is cheaper than a jurisdiction matrix in the call path."},
		{6, "what is explicitly out of scope",
			"Out of scope: calls to phone numbers (no PSTN, no dial-in, no emergency calling), external guests (anyone without conversation membership), and webinars. Emergency calling is called out because a voice product that cannot reach emergency services must say so plainly wherever calls are offered.",
			"Treating phone dial-in or guest links as a later phase of this work.",
			"PSTN needs a carrier, emergency-call obligations and a different abuse model; guests need an identity the product does not have. Both would be separate products, not phases."},
		{7, "boundary with voice messages",
			"A call is live and leaves one history line; a voice message (CHATVOICE-001) is a self-recording of at most two minutes, never a recording of a call or of other people. A call never becomes a voice message, and recording a call — if decision 5 ever allows it — never uses the voice-message path. Agents treat call audio exactly like voice audio (never received) and, if transcripts ever exist, call transcripts exactly like voice transcripts (fenced, untrusted, transcript only, per MessageContentText), decided in CHATCALL-007.",
			"Recording a call as a voice message, and letting agents join call audio.",
			"The consent is different (self versus others) and so are the retention questions; sharing the path would smuggle other people's speech into a store built for one's own. CHATVOICE-001 decision 1 already forbids recording other people."},
	}
}

// CallKind is one offerable call kind: its participant ceiling, its screen
// sharer ceiling, where it may start, and what media it carries. Screen
// sharing is not a call of its own, so its participant ceiling is zero.
type CallKind struct {
	ID              string
	Name            string
	MaxParticipants int
	MaxSharers      int
	Where           []ConversationKind
	Media           string
}

// CallKinds returns the kinds of decision 1 in delivery order.
func CallKinds() []CallKind {
	return []CallKind{
		{ID: "ONE_TO_ONE_VOICE", Name: "One-to-one voice", MaxParticipants: 2, Where: []ConversationKind{Direct, Group}, Media: "audio"},
		{ID: "ONE_TO_ONE_VIDEO", Name: "One-to-one video", MaxParticipants: 2, Where: []ConversationKind{Direct, Group}, Media: "audio and video"},
		{ID: "CHANNEL_DROP_IN", Name: "Drop-in call in a channel", MaxParticipants: 25, Where: []ConversationKind{Group, PublicChannel, PrivateChannel}, Media: "audio-first, at most nine video tiles"},
		{ID: "SCREEN_SHARE", Name: "Screen sharing", MaxSharers: 1, Where: []ConversationKind{Direct, Group, PublicChannel, PrivateChannel}, Media: "one screen inside a video-capable call"},
	}
}

// CallStartedLine is the history line posted when call media connects.
const CallStartedLine = "Call started"

// FormatCallEndedLine renders the history line posted when a call ends. A
// call with known duration and participants names both; anything else stays
// a plain end line rather than inventing numbers.
func FormatCallEndedLine(minutes, people int) string {
	if minutes < 1 || people < 2 {
		return "Call ended"
	}
	return fmt.Sprintf("Call ended · %d min · %d people", minutes, people)
}

// RenderCallDecisions is the stable text form used by the golden test.
func RenderCallDecisions() string {
	var b strings.Builder
	fmt.Fprintf(&b, "CHATCALL-001 decisions recorded %s\n", CallDecisionsRecordedOn)
	for _, d := range CallDecisions() {
		fmt.Fprintf(&b, "\n%d. %s\n%s\n", d.Number, d.Topic, d.Decision)
		fmt.Fprintf(&b, "  rejected: %s\n", d.Rejected)
		fmt.Fprintf(&b, "  why: %s\n", d.Why)
	}
	b.WriteString("\nkinds:\n")
	for _, k := range CallKinds() {
		fmt.Fprintf(&b, "%s ceiling=%d sharers=%d media=%s\n", k.ID, k.MaxParticipants, k.MaxSharers, k.Media)
	}
	fmt.Fprintf(&b, "\nlines: %q, %q\n", CallStartedLine, FormatCallEndedLine(12, 3))
	return b.String()
}

// CallDecisionsDigest is the digest of the rendered record.
func CallDecisionsDigest() string {
	sum := sha256.Sum256([]byte(RenderCallDecisions()))
	return hex.EncodeToString(sum[:])
}
