package agentmodel

import (
	"context"
	"io"
	"math"
	"net/http"
	"testing"
)

// A rate near the int64 maximum must saturate the speech cost at the maximum,
// never wrap it negative into the ledger.
func TestAudioSpeakHugePriceSaturates(t *testing.T) {
	guard := &audioGuardFake{}
	port := audioFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3 speech"))
	}, guard)
	port.price.SpeechMicrosPerThousandChars = math.MaxInt64
	out, err := port.Speak(context.Background(), SpeakRequest{Scope: AudioScope{TenantID: "t", ConversationID: "c", Actor: "a"}, Text: "Hello there"})
	if err != nil {
		t.Fatal(err)
	}
	if out.CostMicros != math.MaxInt64 || len(guard.settled) != 1 || guard.settled[0] != math.MaxInt64 {
		t.Fatalf("speech cost wrapped: cost=%d settled=%v", out.CostMicros, guard.settled)
	}
}

// A rate near the int64 maximum must saturate the transcription cost at the
// maximum, never convert it into a negative estimate.
func TestAudioTranscribeHugePriceSaturates(t *testing.T) {
	guard := &audioGuardFake{}
	port := audioFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, audioTranscriptionJSON)
	}, guard)
	port.price.TranscribeMicrosPerSecond = math.MaxInt64
	out, err := port.Transcribe(context.Background(), TranscribeRequest{Scope: AudioScope{TenantID: "t", ConversationID: "c", Actor: "w"}, Audio: []byte("opus bytes"), ContentType: "audio/webm", DurationMS: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if out.CostMicros != math.MaxInt64 || len(guard.settled) != 1 || guard.settled[0] != math.MaxInt64 {
		t.Fatalf("transcription cost wrapped: cost=%d settled=%v", out.CostMicros, guard.settled)
	}
}
