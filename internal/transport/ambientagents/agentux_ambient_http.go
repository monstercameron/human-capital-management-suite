package ambientagents

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

const Path = "/api/chat/ambient"

type Command struct {
	Conversation     string `json:"conversation"`
	ID               string `json:"id"`
	Action           string `json:"action"`
	Title            string `json:"title,omitempty"`
	Date             string `json:"date,omitempty"`
	Clock            string `json:"clock,omitempty"`
	ExpectedRevision uint64 `json:"expected_revision"`
}
type GrantCommand struct {
	Conversation    string `json:"conversation"`
	Agent           string `json:"agent"`
	Enabled         bool   `json:"enabled"`
	AutomaticPublic bool   `json:"automatic_public"`
}
type OptOutCommand struct {
	Conversation string `json:"conversation"`
	OptOut       bool   `json:"opt_out"`
}
type Grant struct {
	Agent                            string
	Enabled, AutomaticPublic, Paused bool
}
type Task struct {
	ID, Title, Conversation, Source string
	Completed                       bool
	Due                             string
}
type Snapshot struct {
	Cards  []chatui.AgentUXAmbientCard `json:"cards"`
	Grants []Grant                     `json:"grants"`
	OptOut bool                        `json:"opt_out"`
	Tasks  []Task                      `json:"tasks"`
}

// Surface obtains tenant and person from verified request context, never JSON.
type Surface interface {
	Snapshot(context.Context, string) (Snapshot, error)
	Control(context.Context, Command) (Snapshot, error)
	Grant(context.Context, GrantCommand) (Snapshot, error)
	OptOut(context.Context, OptOutCommand) (Snapshot, error)
}

type Handler struct{ Surface Surface }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.Surface == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	var reply Snapshot
	var err error
	if r.Method == http.MethodGet && r.URL.Path == Path {
		reply, err = h.Surface.Snapshot(r.Context(), r.URL.Query().Get("conversation"))
	} else if r.Method == http.MethodPost {
		decode := func(value any) bool {
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
			decoder.DisallowUnknownFields()
			if decoder.Decode(value) != nil {
				return false
			}
			return decoder.Decode(&struct{}{}) == io.EOF
		}
		switch r.URL.Path {
		case Path + "/control":
			var cmd Command
			if !decode(&cmd) {
				http.Error(w, "invalid request", 400)
				return
			}
			reply, err = h.Surface.Control(r.Context(), cmd)
		case Path + "/grant":
			var cmd GrantCommand
			if !decode(&cmd) {
				http.Error(w, "invalid request", 400)
				return
			}
			reply, err = h.Surface.Grant(r.Context(), cmd)
		case Path + "/opt-out":
			var cmd OptOutCommand
			if !decode(&cmd) {
				http.Error(w, "invalid request", 400)
				return
			}
			reply, err = h.Surface.OptOut(r.Context(), cmd)
		default:
			http.NotFound(w, r)
			return
		}
	} else {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		http.Error(w, "request refused", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}
