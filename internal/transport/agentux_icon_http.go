package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

const AgentIconPath = "/api/agent-controls/icons"

// AgentIconImageURL is the same-origin address of an agent's uploaded image
// at a revision; the revision makes the URL cache-stable.
func AgentIconImageURL(personaID string, revision int64) string {
	return AgentIconPath + "/image?persona_id=" + url.QueryEscape(personaID) + "&revision=" + strconv.FormatInt(revision, 10)
}

var (
	ErrAgentIconUnauthenticated = errors.New("agent icon: authentication required")
	ErrAgentIconDenied          = errors.New("agent icon: access denied")
	ErrAgentIconInvalid         = errors.New("agent icon: invalid request")
	ErrAgentIconConflict        = errors.New("agent icon: revision conflict")
	ErrAgentIconUnavailable     = errors.New("agent icon: unavailable")
)

type AgentIconCommand struct {
	PersonaID        string `json:"persona_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Action           string `json:"action,omitempty"`
	// Image carries an owner's own image for the upload action, base64 in
	// JSON; every other action must leave it empty.
	Image          []byte `json:"image,omitempty"`
	ImageMediaType string `json:"image_media_type,omitempty"`
}

type AgentIconReply struct {
	Icon     agenticon.Value `json:"icon"`
	Revision int64           `json:"revision"`
	// ImageURL serves the owner's uploaded image from this origin when the
	// agent has one; empty for a generated icon.
	ImageURL string `json:"image_url,omitempty"`
}

// AgentIconImage is one stored upload served from this origin.
type AgentIconImage struct {
	Content   []byte
	MediaType string
	Revision  int64
}

// AgentIconSurface derives actor and tenant from the verified request context.
type AgentIconSurface interface {
	ChangeAgentIcon(context.Context, AgentIconCommand) (AgentIconReply, error)
	PreviewAgentIcon(context.Context, AgentIconCommand) (AgentIconReply, error)
	AgentIconImage(context.Context, string) (AgentIconImage, error)
}

type AgentIconHandler struct{ Surface AgentIconSurface }

func (h AgentIconHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == AgentIconPath+"/image" {
		h.serveImage(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, AgentIconPath+"/")
	if r.URL.Path != AgentIconPath+"/"+action || (action != "regenerate" && action != "shuffle" && action != "reset" && action != "undo" && action != "preview" && action != "upload") {
		http.NotFound(w, r)
		return
	}
	if h.Surface == nil {
		writeAgentIconReply(w, AgentIconReply{}, ErrAgentIconUnavailable)
		return
	}
	limit := int64(4096)
	if action == "upload" {
		limit = 4 << 20
	}
	var command AgentIconCommand
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
		writeAgentIconReply(w, AgentIconReply{}, ErrAgentIconInvalid)
		return
	}
	var reply AgentIconReply
	var err error
	if action == "preview" {
		reply, err = h.Surface.PreviewAgentIcon(r.Context(), command)
	} else {
		if command.Action != "" && command.Action != action {
			writeAgentIconReply(w, AgentIconReply{}, ErrAgentIconInvalid)
			return
		}
		command.Action = action
		reply, err = h.Surface.ChangeAgentIcon(r.Context(), command)
	}
	writeAgentIconReply(w, reply, err)
}

// serveImage serves a stored upload from this origin. A missing agent and a
// missing upload both answer 403, so the endpoint never tells which agents
// exist or which have an image.
func (h AgentIconHandler) serveImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.Surface == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	image, err := h.Surface.AgentIconImage(r.Context(), r.URL.Query().Get("persona_id"))
	switch {
	case err == nil:
	case errors.Is(err, ErrAgentIconUnauthenticated):
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	case errors.Is(err, ErrAgentIconDenied):
		http.Error(w, "denied", http.StatusForbidden)
		return
	default:
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", image.MediaType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("ETag", strconv.FormatInt(image.Revision, 10))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'self'")
	w.Write(image.Content)
}

func writeAgentIconReply(w http.ResponseWriter, reply AgentIconReply, err error) {
	if err == nil {
		_ = json.NewEncoder(w).Encode(reply)
		return
	}
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, ErrAgentIconUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, ErrAgentIconDenied):
		status, code = http.StatusForbidden, "denied"
	case errors.Is(err, ErrAgentIconInvalid):
		status, code = http.StatusBadRequest, "invalid"
	case errors.Is(err, ErrAgentIconConflict):
		status, code = http.StatusConflict, "conflict"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
