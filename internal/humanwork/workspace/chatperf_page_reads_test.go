package workspace

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// chatperfMeet makes the reads of one request wait for each other: each read
// that arrives waits until all of them have, or gives up. Reads made one after
// the other can never meet.
type chatperfMeet struct {
	want    int32
	arrived atomic.Int32
	all     chan struct{}
	once    sync.Once
	missed  atomic.Int32
}

func newChatperfMeet(want int32) *chatperfMeet {
	return &chatperfMeet{want: want, all: make(chan struct{})}
}

func (m *chatperfMeet) arrive() {
	if m == nil {
		return
	}
	if m.arrived.Add(1) >= m.want {
		m.once.Do(func() { close(m.all) })
	}
	select {
	case <-m.all:
	case <-time.After(2 * time.Second):
		m.missed.Add(1)
	}
}

type chatperfPreferences struct {
	preferences.Store
	loads atomic.Int32
	meet  *chatperfMeet
	user  preferences.User
	err   error
}

func (s *chatperfPreferences) Load(context.Context, values.TenantId, string, string) (preferences.Snapshot, error) {
	s.loads.Add(1)
	s.meet.arrive()
	return preferences.Snapshot{User: s.user}, s.err
}

type chatperfAgentSettings struct{ meet *chatperfMeet }

func (s chatperfAgentSettings) AgentsEnabled(context.Context, values.TenantId) (bool, error) {
	s.meet.arrive()
	return false, nil
}
func (chatperfAgentSettings) SetAgentsEnabled(context.Context, values.TenantId, bool, string) error {
	return nil
}

func chatperfServe(t *testing.T, h *Handler, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

// TestTodo_CHATBUG_014_PageReads: the workspace document reads the viewer's
// preferences once, makes its independent reads together, and says on the
// response what each step cost.
func TestTodo_CHATBUG_014_PageReads(t *testing.T) {
	h, token := newShellHandler(t, false)
	meet := newChatperfMeet(3)
	prefs := &chatperfPreferences{meet: meet, user: preferences.User{
		FavoritePages: []string{workflowStartFavoritePrefix + "promotion"},
		WorkflowUses:  map[string]int64{"promotion": 7},
	}}
	h.preferences = prefs
	h.agentSettings = chatperfAgentSettings{meet: meet}
	var catalogReads atomic.Int32
	h.workflowStarts = func(context.Context, values.TenantId, bool) ([]WorkflowStartConfig, error) {
		catalogReads.Add(1)
		meet.arrive()
		return []WorkflowStartConfig{{WorkflowID: "promotion", Name: "Promotion", Availability: "available"}, {WorkflowID: "transfer", Name: "Transfer", Availability: "available"}}, nil
	}

	response := chatperfServe(t, h, token, PathProductHome)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", PathProductHome, response.Code)
	}
	if got := prefs.loads.Load(); got != 1 {
		t.Fatalf("the document read the viewer's preferences %d times, want once", got)
	}
	if got := catalogReads.Load(); got != 1 {
		t.Fatalf("the document read the workflow catalog %d times, want once", got)
	}
	if missed := meet.missed.Load(); missed != 0 {
		t.Fatalf("%d of the page's reads never overlapped the others: they are being made one after the other again", missed)
	}

	// The one preference read still does both of its jobs: the catalog carries
	// the viewer's favourite and recent use.
	body := response.Body.String()
	island := body[strings.Index(body, `id="`+JourneyConfigElementID+`"`):]
	island = island[:strings.Index(island, "</script>")]
	if !strings.Contains(island, `"workflow_id":"promotion"`) || !strings.Contains(island, `"favorite":true`) || !strings.Contains(island, `"recent_rank":7`) {
		t.Fatalf("the workflow catalog lost the viewer's favourite or recent use: %s", island)
	}

	// Every step is named on the response, with a duration.
	timing := response.Header().Get("Server-Timing")
	for _, step := range []string{"access", "starts", "agents", "rollout", "preferences", "render"} {
		if !strings.Contains(timing, step+";dur=") {
			t.Errorf("Server-Timing %q does not report %s", timing, step)
		}
	}

	// A preference store that fails still refuses the page, as before.
	failing, token := newShellHandler(t, false)
	failing.preferences = &chatperfPreferences{err: errors.New("store unavailable")}
	if refused := chatperfServe(t, failing, token, PathProductHome); refused.Code != http.StatusServiceUnavailable {
		t.Fatalf("a failed preference read served %d, want 503", refused.Code)
	}

	// The timing header is a list a browser can read.
	var steps pageTiming
	steps.measure("first", func() {})
	steps.measure("second", func() { time.Sleep(2 * time.Millisecond) })
	parts := strings.Split(steps.header(), ", ")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "first;dur=") || !strings.HasPrefix(parts[1], "second;dur=") || parts[1] == "second;dur=0.0" {
		t.Fatalf("header = %q", steps.header())
	}
}
