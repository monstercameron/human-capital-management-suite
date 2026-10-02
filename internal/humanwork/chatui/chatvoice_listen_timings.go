package chatui

import (
	"encoding/base64"
	"encoding/json"
)

// Listen timings (CHATVOICE-006). When the speech engine reports when each
// sentence is spoken, the server sends them beside the audio in the response
// header named below, and the player row marks the sentence being read. When the
// engine reports nothing, no header is sent and nothing is marked: the page
// never estimates a position from the text.

// ListenTimingsHeader carries the timings: a base64 JSON list.
const ListenTimingsHeader = "X-Speech-Timings"

const (
	listenTimingsMaxCount = 200
	listenTimingsMaxText  = 1200
	listenTimingsMaxBytes = 32 << 10
)

// ListenTiming is one spoken sentence and the span of the audio it occupies.
type ListenTiming struct {
	Text    string `json:"text"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
}

// EncodeListenTimings is the header value for a list of timings, or "" when the
// list is not one the page would accept (empty, out of order, or too large), so
// a bad list is dropped whole rather than half shown.
func EncodeListenTimings(timings []ListenTiming) string {
	if !validListenTimings(timings) {
		return ""
	}
	raw, err := json.Marshal(timings)
	if err != nil || base64.StdEncoding.EncodedLen(len(raw)) > listenTimingsMaxBytes {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// ParseListenTimings reads a header value. Anything that is not a well-formed,
// ordered list gives nil: no timings, no mark.
func ParseListenTimings(header string) []ListenTiming {
	if header == "" || len(header) > listenTimingsMaxBytes {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		return nil
	}
	var timings []ListenTiming
	if json.Unmarshal(raw, &timings) != nil || !validListenTimings(timings) {
		return nil
	}
	return timings
}

func validListenTimings(timings []ListenTiming) bool {
	if len(timings) == 0 || len(timings) > listenTimingsMaxCount {
		return false
	}
	var previous int64
	for _, t := range timings {
		if t.Text == "" || len([]rune(t.Text)) > listenTimingsMaxText || t.StartMS < previous || t.EndMS <= t.StartMS {
			return false
		}
		previous = t.EndMS
	}
	return true
}

// ListenSentenceAt is the index of the sentence being spoken at a position in
// the audio, or -1 when the position is before the first sentence, between two
// or after the last (no sentence is marked then) or there are no timings.
func ListenSentenceAt(timings []ListenTiming, positionMS int64) int {
	for i, t := range timings {
		if positionMS >= t.StartMS && positionMS < t.EndMS {
			return i
		}
	}
	return -1
}
