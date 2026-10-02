package chatui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// CHATBUG-087: a Chat tab that stays open outlives the credential it was opened
// with (the server was restarted, the session ended, the browser token was
// replaced). Every save then answered "could not be saved" with nothing to say
// why. The clients tell the reasons apart, get a fresh credential from the
// server once and repeat the request, and only then say plainly what is wrong.

// ErrReadingSignedOut means the server no longer accepts the session behind
// the page. A fresh credential could not be had either, so the person has to
// sign in again.
var ErrReadingSignedOut = errors.New("chatui: the session behind this page has ended")

// readingStatusError names what an answer other than 200 means.
func readingStatusError(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return ErrReadingSignedOut
	case http.StatusForbidden:
		return chatrender.ErrDenied
	case http.StatusBadRequest:
		return chatrender.ErrInvalid
	}
	return chatrender.ErrUnavailable
}

// journeyConfigMarker is how the page shell opens its JSON island.
const journeyConfigMarker = `id="journey-config">`

// ParseJourneyBearer reads the bearer out of the journey page shell's JSON
// island. It is the credential the server would hand a freshly loaded page.
func ParseJourneyBearer(page string) (string, bool) {
	start := strings.Index(page, journeyConfigMarker)
	if start < 0 {
		return "", false
	}
	rest := page[start+len(journeyConfigMarker):]
	end := strings.Index(rest, "</script>")
	if end < 0 {
		return "", false
	}
	var island struct {
		Bearer string `json:"bearer"`
	}
	if json.Unmarshal([]byte(rest[:end]), &island) != nil || strings.TrimSpace(island.Bearer) == "" {
		return "", false
	}
	return island.Bearer, true
}

// withIslandBearer returns the island's JSON with its bearer replaced and every
// other field kept as it was.
func withIslandBearer(island, bearer string) (string, error) {
	fields := map[string]json.RawMessage{}
	if strings.TrimSpace(island) != "" {
		if err := json.Unmarshal([]byte(island), &fields); err != nil {
			return "", err
		}
	}
	encoded, err := json.Marshal(bearer)
	if err != nil {
		return "", err
	}
	fields["bearer"] = encoded
	out, err := json.Marshal(fields)
	return string(out), err
}

// readingFailureKey is the copy key (chatrender_copy.go) that says, in plain
// words, why a settings request failed.
func readingFailureKey(err error) string {
	switch {
	case errors.Is(err, ErrReadingSignedOut):
		return "error_signed_out"
	case errors.Is(err, chatrender.ErrDenied):
		return "error_denied"
	case errors.Is(err, chatrender.ErrInvalid):
		return "error"
	}
	return "error_unreachable"
}
