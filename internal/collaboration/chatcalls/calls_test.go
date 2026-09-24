package chatcalls

import (
	"errors"
	"testing"
	"time"
)

var callAt = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func participant(callID, id string, media ...MediaKind) Participant {
	grants := make(map[MediaKind]bool, len(media))
	for _, kind := range media {
		grants[kind] = true
	}
	return Participant{Authority: Authority{PrincipalID: id, TenantID: "tenant-a", Revision: 2, Active: true}, Consent: Consent{CallID: callID, ParticipantID: id, Revision: 3, Granted: true, Media: grants, GrantedAt: callAt.Add(-time.Minute)}}
}

func media(audio, video bool) MediaPolicy { return MediaPolicy{Audio: audio, Video: video} }

func TestTodo_CHAT_054(t *testing.T) {
	req := P2PCallRequest{CallID: "call-1", ConversationID: "conversation-1", At: callAt, Participants: [2]Participant{participant("call-1", "alice", MediaAudio), participant("call-1", "bob", MediaAudio)}, Media: media(true, false), Network: P2PNetworkPolicy{ICEIdentity: "ice", DTLSPeerIdentity: "dtls", AllowedEgressZone: "us-east"}}
	got, err := AdmitP2P(req)
	if err != nil {
		t.Fatalf("AdmitP2P: %v", err)
	}
	if got.CallID != req.CallID || got.ParticipantIDs[0] != "alice" || got.ConsentRevisions != [2]uint64{3, 3} {
		t.Fatalf("admission = %+v", got)
	}
}

func TestTodo_CHAT_054_Conformance(t *testing.T) {
	base := P2PCallRequest{CallID: "call-1", ConversationID: "conversation-1", At: callAt, Participants: [2]Participant{participant("call-1", "alice", MediaAudio), participant("call-1", "bob", MediaAudio)}, Media: media(true, false), Network: P2PNetworkPolicy{ICEIdentity: "ice", DTLSPeerIdentity: "dtls", AllowedEgressZone: "us-east"}}
	tests := []struct {
		name   string
		mutate func(*P2PCallRequest)
		want   error
	}{
		{"explicit-call-consent-required", func(r *P2PCallRequest) { r.Participants[1].Consent.Granted = false }, ErrConsent},
		{"padded-call-identity-refused", func(r *P2PCallRequest) { r.CallID = " call-1" }, ErrInvalid},
		{"padded-conversation-identity-refused", func(r *P2PCallRequest) { r.ConversationID = "conversation-1 " }, ErrInvalid},
		{"padded-participant-identity-refused", func(r *P2PCallRequest) { r.Participants[0].Authority.PrincipalID = " alice" }, ErrAuthority},
		{"padded-tenant-identity-refused", func(r *P2PCallRequest) { r.Participants[0].Authority.TenantID = "tenant-a " }, ErrAuthority},
		{"duplicate-participants-refused", func(r *P2PCallRequest) { r.Participants[1].Authority.PrincipalID = "alice" }, ErrInvalid},
		{"consent-bound-to-call", func(r *P2PCallRequest) { r.Participants[0].Consent.CallID = "other-call" }, ErrConsent},
		{"consent-bound-to-participant", func(r *P2PCallRequest) { r.Participants[0].Consent.ParticipantID = "bob" }, ErrConsent},
		{"video-consent-required-when-requested", func(r *P2PCallRequest) {
			r.Media.Video = true
		}, ErrConsent},
		{"stale-authority-refused", func(r *P2PCallRequest) { r.Participants[0].Authority.Active = false }, ErrAuthority},
		{"expired-authority-refused-at-boundary", func(r *P2PCallRequest) { r.Participants[0].Authority.ExpiresAt = callAt }, ErrAuthority},
		{"recording-needs-notice-and-consent", func(r *P2PCallRequest) { r.Media.Recording = RecordingPolicy{Enabled: true} }, ErrRecordingPolicy},
		{"recording-needs-each-participant-consent", func(r *P2PCallRequest) {
			r.Media.Recording = RecordingPolicy{Enabled: true, ConsentRequired: true, Notice: "recording notice"}
		}, ErrConsent},
		{"caption-needs-provider", func(r *P2PCallRequest) { r.Media.Captions = CaptionPolicy{Enabled: true, Language: "en-US"} }, ErrCaptionPolicy},
		{"network-identity-required", func(r *P2PCallRequest) { r.Network.DTLSPeerIdentity = "" }, ErrNetworkPolicy},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req)
			_, err := AdmitP2P(req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestP2PCallAdmissionAcceptsSeparatelyConsentedAudioVideoRecording(t *testing.T) {
	participants := [2]Participant{participant("call-1", "alice", MediaAudio, MediaVideo), participant("call-1", "bob", MediaAudio, MediaVideo)}
	for i := range participants {
		participants[i].Consent.RecordingGranted = true
	}
	req := P2PCallRequest{
		CallID: "call-1", ConversationID: "conversation-1", At: callAt,
		Participants: participants,
		Media: MediaPolicy{
			Audio: true, Video: true,
			Recording: RecordingPolicy{Enabled: true, ConsentRequired: true, Notice: "This call is recorded."},
		},
		Network: P2PNetworkPolicy{ICEIdentity: "ice", DTLSPeerIdentity: "dtls", AllowedEgressZone: "us-east"},
	}
	got, err := AdmitP2P(req)
	if err != nil {
		t.Fatalf("AdmitP2P: %v", err)
	}
	if got.Media != req.Media || got.ParticipantIDs != [2]string{"alice", "bob"} {
		t.Fatalf("admission did not preserve admitted call policy and pair: %+v", got)
	}
}

func TestTodo_CHAT_055(t *testing.T) {
	req := ServerGroupCallRequest{CallID: "group-1", ConversationID: "conversation-1", At: callAt, Participants: []Participant{participant("group-1", "alice", MediaAudio, MediaVideo), participant("group-1", "bob", MediaAudio, MediaVideo), participant("group-1", "cara", MediaAudio, MediaVideo)}, Media: MediaPolicy{Audio: true, Video: true, Captions: CaptionPolicy{Enabled: true, Language: "en-US", Provider: "captions-v1"}}, Routing: ServerRouting{Region: "us-east", RelayID: "relay-1", RouteEpoch: 9}, Capacity: ServerCapacity{MaxParticipants: 10, ReservedParticipants: 2, AudioSlots: 10, ReservedAudioSlots: 2, VideoSlots: 10, ReservedVideoSlots: 2}, Cost: CostBudget{Currency: "USD", MaximumCents: 100, EstimatedCents: 80}}
	got, err := AdmitServerGroup(req)
	if err != nil {
		t.Fatalf("AdmitServerGroup: %v", err)
	}
	if len(got.ParticipantIDs) != 3 || got.Routing.RelayID != "relay-1" || got.Cost.EstimatedCents != 80 {
		t.Fatalf("admission = %+v", got)
	}
}

func TestTodo_CHAT_055_Conformance(t *testing.T) {
	base := ServerGroupCallRequest{CallID: "group-1", ConversationID: "conversation-1", At: callAt, Participants: []Participant{participant("group-1", "alice", MediaAudio), participant("group-1", "bob", MediaAudio), participant("group-1", "cara", MediaAudio)}, Media: media(true, false), Routing: ServerRouting{Region: "us-east", RelayID: "relay-1", RouteEpoch: 9}, Capacity: ServerCapacity{MaxParticipants: 4, ReservedParticipants: 1, AudioSlots: 4, ReservedAudioSlots: 1}, Cost: CostBudget{Currency: "USD", MaximumCents: 100, EstimatedCents: 80}}
	tests := []struct {
		name   string
		mutate func(*ServerGroupCallRequest)
		want   error
	}{
		{"server-capacity-is-required", func(r *ServerGroupCallRequest) { r.Capacity.MaxParticipants = 2 }, ErrCapacity},
		{"server-routing-is-required", func(r *ServerGroupCallRequest) { r.Routing.RelayID = "" }, ErrRouting},
		{"cost-budget-is-enforced", func(r *ServerGroupCallRequest) { r.Cost.EstimatedCents = 101 }, ErrCostBudget},
		{"group-participant-consent-is-explicit", func(r *ServerGroupCallRequest) { r.Participants[2].Consent.Granted = false }, ErrConsent},
		{"recording-policy-is-explicit", func(r *ServerGroupCallRequest) {
			r.Media.Recording = RecordingPolicy{Enabled: true, ConsentRequired: true}
		}, ErrRecordingPolicy},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req)
			_, err := AdmitServerGroup(req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
