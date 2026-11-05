package governance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// CallEndToEndRow is the encryption recommendation for one call kind of
// CHATCALL-001: what end-to-end encryption would protect and what it would
// prevent there. The Server flags name the server media features the kind
// keeps; a kind that claims end-to-end encryption keeps none.
type CallEndToEndRow struct {
	CallKind        string
	Recommendation  string
	EndToEnd        bool
	Protects        string
	Prevents        string
	ServerCaptions  bool
	ServerRecording bool
	ServerAgents    bool
}

// CallConsent is the recording and transcription consent rule.
type CallConsent struct {
	Rule                string
	StrictJurisdictions []string
	WorksCouncil        string
}

// CallThreat is one threat of the call threat model with exactly one
// answer: a Mitigation or an AcceptedRisk, never both, never neither.
type CallThreat struct {
	Threat       string
	Mitigation   string
	AcceptedRisk string
}

// CallPrivacy is the CHATCALL-005 record: encryption, consent, metadata,
// visibility, abuse handling, governance inheritance and the threat model.
type CallPrivacy struct {
	TransportEncryption   string
	EndToEndByKind        []CallEndToEndRow
	Consent               CallConsent
	MetadataFields        []string
	MetadataRetention     string
	MetadataHold          string
	MetadataExport        string
	AdminVisibility       string
	TenantVisibility      string
	AbuseHandling         string
	GovernanceInheritance string
	Threats               []CallThreat
}

// CallPrivacyRecord returns the record. Recording, live captions and
// transcripts are not offered at launch (CHATCALL-001); everything here is
// the behaviour they must satisfy if they ever are.
func CallPrivacyRecord() CallPrivacy {
	return CallPrivacy{
		TransportEncryption: "Transport encryption is always on: every call negotiates DTLS-SRTP, and unencrypted media is never offered, accepted or fallen back to, on any topology, in any region, for any tenant.",
		EndToEndByKind: []CallEndToEndRow{
			{CallKind: "One-to-one voice", Recommendation: "End-to-end encrypted between the two browsers: DTLS-SRTP with no server on the media path.", EndToEnd: true,
				Protects: "Call content from the relay, the deployment and any network observer.",
				Prevents: "Server-side captions, recording and agent features — nothing to prevent at launch, since none are offered."},
			{CallKind: "One-to-one video", Recommendation: "End-to-end encrypted between the two browsers, as with voice.", EndToEnd: true,
				Protects: "Call content from the relay, the deployment and any network observer.",
				Prevents: "Server-side captions, recording and agent features, none of which are offered."},
			{CallKind: "Drop-in call in a channel", Recommendation: "Transport encryption terminating at the forwarding unit; SFrame end-to-end encryption stays a later option, not the launch posture.", EndToEnd: false,
				Protects: "Call content on the wire and everywhere outside the deployment.",
				Prevents: "Nothing at launch; turning SFrame on later would prevent server captions, recording and agent features."},
			{CallKind: "Screen sharing", Recommendation: "Follows the call it rides: end-to-end inside a one-to-one call, transport-only inside a drop-in.", EndToEnd: false,
				Protects: "Whatever the carrying call protects; a screen is one more stream, not a weaker one.",
				Prevents: "Nothing beyond what the carrying call prevents."},
		},
		Consent: CallConsent{
			Rule: "Strictest wins everywhere: every participant is told before recording or transcription starts, an on-screen indicator is shown continuously while it runs, and any participant may decline by leaving — one decline stops the recording or transcript for all while the call itself continues.",
			StrictJurisdictions: []string{"California", "Connecticut", "Florida", "Illinois", "Maryland", "Massachusetts", "Montana", "New Hampshire", "Pennsylvania", "Washington",
				"European Union", "Germany", "United Kingdom", "United Arab Emirates", "Saudi Arabia"},
			WorksCouncil: "In Germany and wherever employee co-determination applies, recording or transcribing employee calls needs the works council's agreement before the feature ships. The product offers no per-tenant switch to bypass that, and no silent rollout.",
		},
		MetadataFields:        []string{"call identity", "conversation identity", "tenant identity", "participant identities", "start time", "end time", "duration", "peak participant count", "join and leave events", "capture indicator state"},
		MetadataRetention:     "Call metadata follows the conversation's retention policy (chatrecords RetentionPolicy): the same per-kind schedule and the same tenant ownership. The history lines are posts and retire with them.",
		MetadataHold:          "A legal hold preserves metadata and history lines exactly like posts: holds block deletion, and unknown copies block selection. The retention dry-run rules apply to calls unchanged.",
		MetadataExport:        "Export carries metadata and history lines with the conversation's export. Media, if any is ever stored, exports only under the same hold and audience rules as its conversation.",
		AdminVisibility:       "Administrators see call metadata — who, when, how long — for conversations they administer, never media content. A private conversation's call content stays invisible to non-members exactly as its posts do, following the moderation-queue rule that queue visibility must not open private conversations.",
		TenantVisibility:      "A tenant sees its own calls' metadata and nothing else. In a cross-company conversation each side sees only what its own membership admits; no tenant sees another tenant's calls.",
		AbuseHandling:         "Unwanted one-to-one calls are refused by mute, quiet hours and membership removal; ringing is rate-limited through the chat admission bounds; drop-ins never ring. A report path for abusive calls is required before launch under the company-chat abuse release gate, reusing the report, block and mute controls.",
		GovernanceInheritance: "A recording or transcript, if ever allowed, inherits the conversation's governance exactly as CHATVOICE-005 requires for voice messages: the same audience, retention, hold, export and deletion. Agents receive the transcript fenced and untrusted, and never the audio.",
		Threats: []CallThreat{
			{Threat: "eavesdropping at a relay",
				Mitigation: "DTLS-SRTP end to end on one-to-one calls and hop by hop through the forwarding unit on drop-ins: relays forward ciphertext they cannot read, and relay credentials are short-lived and bound to one call, person, tenant and membership revision (CHATCALL-003)."},
			{Threat: "a participant silently recording with their own tools",
				AcceptedRisk: "No product mechanism stops a participant from recording their own device's output. The announcement, the continuous indicator and the consent rule make silent recording a policy violation with evidence — the indicator state is in the call metadata — not a preventable action."},
			{Threat: "a compromised forwarding server",
				Mitigation: "Drop-in media terminates at the forwarding unit, so a compromise exposes live drop-ins: contain it with per-call isolation, no stored media, short-lived call-bound credentials, and SFrame end-to-end encryption as the later upgrade that removes the server from trust."},
			{Threat: "call injection",
				Mitigation: "Every signalling message passes the conversation's tenant and active-membership check (CHATCALL-003). An injected offer for a conversation the attacker cannot post in is refused exactly like a forged post."},
			{Threat: "address disclosure",
				Mitigation: "Relay-only by default: clients exchange no host candidates, so no signalling message carries a network address that could leak, and no caller learns whether the callee sits behind a VPN, at home or in the office (CHATCALL-003)."},
		},
	}
}

// RenderCallPrivacy is the stable text form used by the golden test.
func RenderCallPrivacy() string {
	r := CallPrivacyRecord()
	var b strings.Builder
	b.WriteString("CHATCALL-005 call security, privacy and compliance recorded 2026-10-22\n")
	fmt.Fprintf(&b, "\ntransport: %s\n", r.TransportEncryption)
	b.WriteString("\nend-to-end by kind:\n")
	for _, row := range r.EndToEndByKind {
		fmt.Fprintf(&b, "%s (e2e=%v server=captions:%v recording:%v agents:%v): %s\n  protects: %s\n  prevents: %s\n",
			row.CallKind, row.EndToEnd, row.ServerCaptions, row.ServerRecording, row.ServerAgents, row.Recommendation, row.Protects, row.Prevents)
	}
	fmt.Fprintf(&b, "\nconsent: %s\nstrict: %s\nworks council: %s\n",
		r.Consent.Rule, strings.Join(r.Consent.StrictJurisdictions, ", "), r.Consent.WorksCouncil)
	fmt.Fprintf(&b, "\nmetadata: %s\nretention: %s\nhold: %s\nexport: %s\n",
		strings.Join(r.MetadataFields, "; "), r.MetadataRetention, r.MetadataHold, r.MetadataExport)
	fmt.Fprintf(&b, "\nadministrators see: %s\ntenants see: %s\nabuse: %s\ninheritance: %s\n",
		r.AdminVisibility, r.TenantVisibility, r.AbuseHandling, r.GovernanceInheritance)
	b.WriteString("\nthreats:\n")
	for _, t := range r.Threats {
		fmt.Fprintf(&b, "%s\n  mitigation: %s\n  accepted risk: %s\n", t.Threat, t.Mitigation, t.AcceptedRisk)
	}
	return b.String()
}

// CallPrivacyDigest is the digest of the rendered record.
func CallPrivacyDigest() string {
	sum := sha256.Sum256([]byte(RenderCallPrivacy()))
	return hex.EncodeToString(sum[:])
}
