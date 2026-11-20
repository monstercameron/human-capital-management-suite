package application

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// ISSUE-3929: the routes behind Saved searches. They manage a person's own
// shortcuts and nothing else: a shortcut is a name and query text, so these
// handlers never run a search, never read results and never log the query. The
// owner is always the signed-in person; no body field can name another.

const chatSearchSavedPath = ChatSearchPath + "/saved"

type issue3929Body struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

func issue3929IsSavedPath(path string) bool { return path == chatSearchSavedPath }

func (h ChatSearchHTTP) serveSaved(w http.ResponseWriter, r *http.Request, actor chatsearch.Actor) {
	if h.Saved == nil {
		chatsearchHTTPError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	switch r.Method {
	case http.MethodGet:
		rows, err := h.Saved.List(r.Context(), actor)
		if err != nil {
			issue3929Fail(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(rows)
	case http.MethodPost, http.MethodPut:
		body, ok := issue3929Read(w, r)
		if !ok {
			return
		}
		if r.Method == http.MethodPost {
			saved, err := h.Saved.Create(r.Context(), actor, body.Name, body.Query)
			if err != nil {
				issue3929Fail(w, err)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(saved)
			return
		}
		saved, err := h.Saved.Update(r.Context(), actor, id, body.Name, body.Query)
		if err != nil {
			issue3929Fail(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(saved)
	case http.MethodDelete:
		if err := h.Saved.Delete(r.Context(), actor, id); err != nil {
			issue3929Fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		chatsearchHTTPError(w, http.StatusMethodNotAllowed, "method")
	}
}

func issue3929Read(w http.ResponseWriter, r *http.Request) (issue3929Body, bool) {
	var body issue3929Body
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		chatsearchHTTPError(w, http.StatusUnsupportedMediaType, "json_required")
		return body, false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		chatsearchHTTPError(w, http.StatusBadRequest, "invalid")
		return body, false
	}
	return body, true
}

// issue3929Fail answers with a short code and never the cause.
func issue3929Fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, chatsearch.ErrInvalid):
		chatsearchHTTPError(w, http.StatusBadRequest, "invalid")
	case errors.Is(err, chatsearch.ErrSavedNotFound):
		chatsearchHTTPError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, chatsearch.ErrSavedNameUsed):
		chatsearchHTTPError(w, http.StatusConflict, "name_taken")
	case errors.Is(err, chatsearch.ErrSavedLimit):
		chatsearchHTTPError(w, http.StatusConflict, "limit")
	default:
		chatsearchLogCause("hcmnext.chat_saved_search_unavailable", err)
		chatsearchHTTPError(w, http.StatusServiceUnavailable, "unavailable")
	}
}
