package connectivity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// MediaTopology is one evaluated way to move call media for the call kinds
// of CHATCALL-001. Bandwidth figures are planning estimates (Opus voice
// about 24 kbps, 720p video about 1 Mbps, 360p about 0.5 Mbps); the
// prototype of TopologyMeasurementContract confirms or corrects them.
type MediaTopology struct {
	ID                     string
	Name                   string
	ParticipantLimit       string
	UpstreamPerPerson      string
	DownstreamPerPerson    string
	ServerCPUPerCall       string
	ServerBandwidthPerCall string
	PoorNetwork            string
	Encryption             string
	Captions               string
	Recording              string
}

// MediaTopologies returns the comparison in increasing server involvement.
func MediaTopologies() []MediaTopology {
	return []MediaTopology{
		{ID: "P2P", Name: "Direct peer-to-peer",
			ParticipantLimit:       "Two people. A third peer has no place in the design.",
			UpstreamPerPerson:      "One outgoing media stream: about 24 kbps for voice, about 1 Mbps for 720p video.",
			DownstreamPerPerson:    "One incoming media stream, same rates.",
			ServerCPUPerCall:       "None for media. The server only exchanges signalling messages and, when a relay is needed, forwards packets without decoding them.",
			ServerBandwidthPerCall: "None for media, except relayed calls, which cost one upstream plus one downstream per person at the relay.",
			PoorNetwork:            "The sender lowers bitrate under loss; video stops and the call takes the audio-only fallback before it drops. One weak network degrades exactly its own call.",
			Encryption:             "DTLS-SRTP end to end between the two browsers. No server ever holds media keys or plaintext.",
			Captions:               "Not possible on the server: there is no server on the media path. Only the browsers could caption, from their own decrypted audio.",
			Recording:              "Not possible on the server for the same reason. Either browser could record locally, which policy cannot prevent (CHATCALL-005)."},
		{ID: "MESH", Name: "Mesh of peer connections",
			ParticipantLimit:       "About five or six people in practice. Every person encodes once per peer, so CPU and uplink grow with the square of the group.",
			UpstreamPerPerson:      "(N-1) outgoing streams: at five people, four times the one-to-one rate, about 4 Mbps for 720p video.",
			DownstreamPerPerson:    "(N-1) incoming streams, same multiple.",
			ServerCPUPerCall:       "None for media, as with direct peer-to-peer.",
			ServerBandwidthPerCall: "None for media, except relayed pairs at the relay.",
			PoorNetwork:            "Each pair adapts on its own; one weak uplink degrades that person's streams to everyone. Audio-only fallback applies per pair.",
			Encryption:             "Pairwise DTLS-SRTP, end to end on every leg. No server holds media.",
			Captions:               "Not possible on the server: there is no server on the media path.",
			Recording:              "Not possible on the server. A participant's local recording would capture only what their browser mixed."},
		{ID: "SFU", Name: "Selective forwarding unit",
			ParticipantLimit:       "Hundreds for audio, tens with video. The server forwards but never decodes, so cost grows with packets, not with mixing.",
			UpstreamPerPerson:      "One outgoing stream plus simulcast layers, roughly one and a half to twice the one-to-one rate.",
			DownstreamPerPerson:    "(N-1) forwarded streams, capped by what each receiver subscribes to: the drop-in shows at most nine video tiles and takes audio for the rest.",
			ServerCPUPerCall:       "Packet forwarding only: a fraction of a core for a team-sized call. Simulcast layer selection and audio-level forwarding add small multiples, not mixing cost.",
			ServerBandwidthPerCall: "About N upstreams plus N downstreams through the server: for twenty-five audio-first participants, on the order of tens of megabits per second.",
			PoorNetwork:            "Simulcast and scalable video let each receiver drop to a lower layer without affecting anyone else; receivers that cannot hold video keep audio-only while the rest stay on video.",
			Encryption:             "DTLS-SRTP hop by hop, terminating at the server. End-to-end encryption is optional (SFrame) at the cost of every server media feature below.",
			Captions:               "Possible on the server by decrypting and transcribing, unless end-to-end encryption is on, which forbids it.",
			Recording:              "Possible on the server by storing forwarded streams, unless end-to-end encryption is on, which forbids it."},
		{ID: "MCU", Name: "Mixing server",
			ParticipantLimit:       "Tens. Every participant needs a decode, and every receiver needs an encode, so the ceiling is a CPU budget, not a network one.",
			UpstreamPerPerson:      "One outgoing stream, as with direct peer-to-peer.",
			DownstreamPerPerson:    "One incoming mixed stream: cheap for the receiver, the mixer's whole point.",
			ServerCPUPerCall:       "High: decode every input, mix, encode every output. Cores per call, not calls per core.",
			ServerBandwidthPerCall: "About N upstreams plus N mixed downstreams, at the mixed bitrate.",
			PoorNetwork:            "The mixer adapts one outgoing stream per receiver, which flatters weak receivers and hides who is struggling from everyone else.",
			Encryption:             "The mixer must decrypt to mix, so end-to-end encryption is impossible. Media plaintext always exists on the server.",
			Captions:               "Trivially possible on the mixed stream — and unavoidable at the server, which hears everything mixed.",
			Recording:              "Trivially possible on the mixed stream, for the same reason."},
	}
}

// TopologyCandidate is one implementation the product could run or buy, with
// the verdict. Licences are as published; re-check at build time.
type TopologyCandidate struct {
	ID          string
	Name        string
	Language    string
	Licence     string
	Maturity    string
	Maintenance string
	MonolithFit string
	// Verdict is ADOPT or REJECT, with the reason inside MonolithFit.
	Verdict string
}

// TopologyCandidates returns the evaluated implementations: a Go-native
// stack, the established forwarding servers, and the relay every topology
// needs on hostile networks.
func TopologyCandidates() []TopologyCandidate {
	return []TopologyCandidate{
		{ID: "PION", Name: "Pion", Language: "Go", Licence: "MIT",
			Maturity:    "Mature: a full WebRTC stack in Go, underneath several shipped products.",
			Maintenance: "Actively maintained, with a public security process.",
			MonolithFit: "Runs in the server process or as a Go sidecar beside the monolith: same toolchain, same deployment, no new runtime. The one-to-one prototype is built on it.",
			Verdict:     "ADOPT"},
		{ID: "LIVEKIT", Name: "LiveKit", Language: "Go", Licence: "Apache-2.0",
			Maturity:    "Mature as both a managed service and an open-source selective forwarding unit.",
			Maintenance: "Actively maintained, with commercial backing behind the open core.",
			MonolithFit: "A separate Go process beside the monolith, federated over the deployment's own network; needs a small Redis for multi-node rooms. The drop-in call runs on it.",
			Verdict:     "ADOPT"},
		{ID: "COTURN", Name: "coturn", Language: "C", Licence: "BSD",
			Maturity:    "Mature: the standard TURN relay, embedded in most self-hosted call products.",
			Maintenance: "Maintained, with a slow and stable surface.",
			MonolithFit: "A standalone relay process with no runtime coupling to the monolith; every topology uses it for the calls that cannot connect directly.",
			Verdict:     "ADOPT"},
		{ID: "MEDIASOUP", Name: "mediasoup", Language: "C++ with a Node.js control layer", Licence: "ISC",
			Maturity:    "Mature and widely deployed as a forwarding unit.",
			Maintenance: "Actively maintained.",
			MonolithFit: "Rejected: the control layer needs a Node.js runtime beside the Go monolith — a second runtime to deploy, monitor and patch for one feature.",
			Verdict:     "REJECT"},
		{ID: "JANUS", Name: "Janus", Language: "C", Licence: "GPLv3",
			Maturity:    "Mature and general-purpose, with a plugin for most call shapes.",
			Maintenance: "Actively maintained.",
			MonolithFit: "Rejected: the GPLv3 copyleft reaches the combined distribution, which a paid product cannot accept; and the general-purpose plugin model is more server than a drop-in call needs.",
			Verdict:     "REJECT"},
		{ID: "JITSI", Name: "Jitsi Videobridge", Language: "Java", Licence: "Apache-2.0",
			Maturity:    "Mature and proven at large scale.",
			Maintenance: "Actively maintained.",
			MonolithFit: "Rejected: a JVM beside the Go monolith plus its signalling companions is the heaviest operation of the set, for no capability LiveKit lacks.",
			Verdict:     "REJECT"},
	}
}

// TopologyRecommendation is the choice per call kind of CHATCALL-001, with
// the point at which the choice changes.
type TopologyRecommendation struct {
	CallKind    string
	Recommended string
	Crossover   string
}

// TopologyRecommendations returns the choice per call kind.
func TopologyRecommendations() []TopologyRecommendation {
	return []TopologyRecommendation{
		{CallKind: "One-to-one voice", Recommended: "Direct peer-to-peer with relay fallback.",
			Crossover: "None: two people never need a server for media. A server enters only if recording or captions are ever required, which CHATCALL-001 does not offer."},
		{CallKind: "One-to-one video", Recommended: "Direct peer-to-peer with relay fallback.",
			Crossover: "If server-side recording or captions are ever required, route the call through the forwarding unit instead; until then the media stays between the two browsers."},
		{CallKind: "Drop-in call in a channel", Recommended: "Selective forwarding unit.",
			Crossover: "Below five people a mesh would connect, but the choice does not change: one behaviour, one caption and recording path, and one operation at every group size. The ceiling stays twenty-five audio-first participants with at most nine video tiles."},
		{CallKind: "Screen sharing", Recommended: "Rides the call's own topology: peer-to-peer in a one-to-one call, forwarded in a drop-in.",
			Crossover: "None: a screen is one more video stream, and one sharer at a time keeps it the cheapest stream in the call."},
	}
}

// PrototypeContract is the executable contract for the measured prototype:
// the path, the injected impairment, the reported metrics and their budgets,
// the measured result per metric, and the run provenance. Status is NOT_RUN
// until a review-cell run reports back, RUN once Results holds all three.
type PrototypeContract struct {
	Path           string
	LossProfile    string
	LatencyProfile string
	Metrics        []string
	Budgets        map[string]string
	Results        map[string]string
	Run            string
	Status         string
}

// TopologyMeasurementContract returns the contract for the recommended
// one-to-one path. Budgets are median figures over a five-minute call.
// Results holds the 2026-10-22 review-cell run: the audio budget is MISSED
// on the conservative G.711 fallback the throwaway prototype speaks, while
// setup and recovery meet their budgets with wide margins.
func TopologyMeasurementContract() PrototypeContract {
	return PrototypeContract{
		Path:           "One-to-one voice over direct peer-to-peer on the review cell, with the relay fallback armed but not used.",
		LossProfile:    "Five percent uniform packet loss, plus a severe ten percent pass.",
		LatencyProfile: "150 ms round trip, plus a severe 300 ms pass.",
		Metrics:        []string{"setup time", "audio quality score", "recovery time"},
		Budgets: map[string]string{
			"setup time":          "at most two seconds from acceptance to first audible audio, median",
			"audio quality score": "at least 3.5 MOS on a POLQA-class score under the standard profile",
			"recovery time":       "at most five seconds from the impairment clearing to full quality",
		},
		Results: map[string]string{
			"setup time":          "84 ms median over 20 fresh calls (min 71 ms, max 275 ms): MET",
			"audio quality score": "3.01 MOS-LQO median over ten 30 s utterances (min 2.87, max 3.19) under the standard profile with G.711 and repeat-last concealment; clean control 4.27, severe pass 2.39: MISSED on the conservative fallback, the recommended Opus path is expected to exceed it",
			"recovery time":       "10 ms median (max 160 ms) over 5 trials to gapless delivery, qualifying clean second completes about 1 s later: MET",
		},
		Run:    "2026-10-22 review cell windows-arm64 Go 1.26.3: Pion webrtc v4.2.25 with turn v4.1.4 relay armed (host/host selected on all 30 calls), G.711 mu-law voice, userspace 5 percent / 75 ms and 10 percent / 150 ms profiles, PESQ P.862 narrowband via pesq 0.0.4",
		Status: "RUN",
	}
}

// RenderMediaTopologies is the stable text form used by the golden test.
func RenderMediaTopologies() string {
	var b strings.Builder
	b.WriteString("CHATCALL-002 media topology comparison recorded 2026-10-22\n")
	for _, t := range MediaTopologies() {
		fmt.Fprintf(&b, "\n%s: %s\nlimit: %s\nup: %s\ndown: %s\nserver cpu: %s\nserver bandwidth: %s\npoor network: %s\nencryption: %s\ncaptions: %s\nrecording: %s\n",
			t.ID, t.Name, t.ParticipantLimit, t.UpstreamPerPerson, t.DownstreamPerPerson, t.ServerCPUPerCall, t.ServerBandwidthPerCall, t.PoorNetwork, t.Encryption, t.Captions, t.Recording)
	}
	b.WriteString("\ncandidates:\n")
	for _, c := range TopologyCandidates() {
		fmt.Fprintf(&b, "%s %s (%s, %s): %s\n  maturity: %s\n  maintenance: %s\n  fit: %s\n",
			c.ID, c.Name, c.Language, c.Licence, c.Verdict, c.Maturity, c.Maintenance, c.MonolithFit)
	}
	b.WriteString("\nrecommendations:\n")
	for _, r := range TopologyRecommendations() {
		fmt.Fprintf(&b, "%s -> %s\n  crossover: %s\n", r.CallKind, r.Recommended, r.Crossover)
	}
	c := TopologyMeasurementContract()
	fmt.Fprintf(&b, "\nprototype: %s\nloss: %s\nlatency: %s\nstatus: %s\nrun: %s\n", c.Path, c.LossProfile, c.LatencyProfile, c.Status, c.Run)
	for _, m := range c.Metrics {
		fmt.Fprintf(&b, "budget %s: %s\n", m, c.Budgets[m])
		fmt.Fprintf(&b, "result %s: %s\n", m, c.Results[m])
	}
	return b.String()
}

// MediaTopologiesDigest is the digest of the rendered comparison.
func MediaTopologiesDigest() string {
	sum := sha256.Sum256([]byte(RenderMediaTopologies()))
	return hex.EncodeToString(sum[:])
}
