package agentdemo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const BirthdayPreferencePath = "/api/agent-controls/profile/birthday-preference"

// A profile request names no worker. The application binds it to the subject.
type BirthdayPreference struct {
	ShareBirthday bool   `json:"share_birthday"`
	Revision      uint64 `json:"revision"`
}
type BirthdayPreferenceSurface interface {
	ReadBirthdayPreference(context.Context) (BirthdayPreference, error)
	SaveBirthdayPreference(context.Context, BirthdayPreference) (BirthdayPreference, error)
}
type BirthdayPreferenceHandler struct{ Surface BirthdayPreferenceSurface }

func (h BirthdayPreferenceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != BirthdayPreferencePath {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.Surface == nil {
		agentuxDemoWriteError(w, ErrUnavailable)
		return
	}
	var result BirthdayPreference
	var err error
	if r.Method == http.MethodGet {
		result, err = h.Surface.ReadBirthdayPreference(r.Context())
	} else {
		// Pointers distinguish an explicit false/zero from missing form fields.
		var input struct {
			Share    *bool   `json:"share_birthday"`
			Revision *uint64 `json:"revision"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		if err = d.Decode(&input); err != nil || input.Share == nil || input.Revision == nil {
			agentuxDemoWriteError(w, ErrInvalid)
			return
		}
		var extra any
		if err = d.Decode(&extra); !errors.Is(err, io.EOF) {
			agentuxDemoWriteError(w, ErrInvalid)
			return
		}
		result, err = h.Surface.SaveBirthdayPreference(r.Context(), BirthdayPreference{ShareBirthday: *input.Share, Revision: *input.Revision})
	}
	if err != nil {
		agentuxDemoWriteError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
