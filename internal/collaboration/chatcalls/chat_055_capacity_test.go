package chatcalls

import (
	"errors"
	"testing"
)

func TestTodo_CHAT_055_Conformance_CapacityGuard(t *testing.T) {
	base := ServerGroupCallRequest{
		CallID: "group-capacity", ConversationID: "conversation-1", At: callAt,
		Participants: []Participant{
			participant("group-capacity", "alice", MediaAudio),
			participant("group-capacity", "bob", MediaAudio),
			participant("group-capacity", "cara", MediaAudio),
		},
		Media: media(true, false), Routing: ServerRouting{Region: "us-east", RelayID: "relay-1", RouteEpoch: 1},
		Capacity: ServerCapacity{MaxParticipants: 10, ReservedParticipants: 2, AudioSlots: 10, ReservedAudioSlots: 2, VideoSlots: 10, ReservedVideoSlots: 2},
		Cost:     CostBudget{Currency: "USD", MaximumCents: 100, EstimatedCents: 80},
	}
	tests := []struct {
		name   string
		mutate func(*ServerCapacity)
	}{
		{"negative participant reservation cannot create capacity", func(c *ServerCapacity) { c.ReservedParticipants = -1 }},
		{"participant reservation cannot exceed capacity", func(c *ServerCapacity) { c.ReservedParticipants = c.MaxParticipants + 1 }},
		{"negative audio reservation cannot create capacity", func(c *ServerCapacity) { c.ReservedAudioSlots = -1 }},
		{"audio reservation cannot exceed slots", func(c *ServerCapacity) { c.ReservedAudioSlots = c.AudioSlots + 1 }},
		{"negative video reservation is invalid", func(c *ServerCapacity) { c.ReservedVideoSlots = -1 }},
		{"video reservation cannot exceed slots", func(c *ServerCapacity) { c.ReservedVideoSlots = c.VideoSlots + 1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req.Capacity)
			if _, err := AdmitServerGroup(req); !errors.Is(err, ErrCapacity) {
				t.Fatalf("AdmitServerGroup error = %v, want %v", err, ErrCapacity)
			}
		})
	}
}

func TestTodo_CHAT_055_Conformance_Recording(t *testing.T) {
	req := ServerGroupCallRequest{
		CallID: "group-recording", ConversationID: "conversation-1", At: callAt,
		Participants: []Participant{
			participant("group-recording", "alice", MediaAudio),
			participant("group-recording", "bob", MediaAudio),
			participant("group-recording", "cara", MediaAudio),
		},
		Media:    MediaPolicy{Audio: true, Recording: RecordingPolicy{Enabled: true, ConsentRequired: true, Notice: "This call is recorded."}},
		Routing:  ServerRouting{Region: "us-east", RelayID: "relay-1", RouteEpoch: 1},
		Capacity: ServerCapacity{MaxParticipants: 10, ReservedParticipants: 1, AudioSlots: 10, ReservedAudioSlots: 1},
		Cost:     CostBudget{Currency: "USD", MaximumCents: 100, EstimatedCents: 80},
	}
	for i := range req.Participants {
		req.Participants[i].Consent.RecordingGranted = true
	}
	got, err := AdmitServerGroup(req)
	if err != nil {
		t.Fatalf("AdmitServerGroup with explicit recording consent: %v", err)
	}
	if !got.Media.Recording.Enabled || !got.Media.Recording.ConsentRequired || got.Media.Recording.Notice != "This call is recorded." {
		t.Fatalf("recording policy = %+v", got.Media.Recording)
	}
}
