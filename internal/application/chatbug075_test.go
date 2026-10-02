package application

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatbug075Log collects what the process logs, by message and level.
type chatbug075Log struct {
	mu      sync.Mutex
	records []slog.Record
}

func (l *chatbug075Log) Enabled(context.Context, slog.Level) bool { return true }
func (l *chatbug075Log) Handle(_ context.Context, record slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, record)
	return nil
}
func (l *chatbug075Log) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *chatbug075Log) WithGroup(string) slog.Handler      { return l }

func (l *chatbug075Log) count(message string, level slog.Level) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, record := range l.records {
		if record.Message == message && record.Level == level {
			n++
		}
	}
	return n
}

func chatbug075Capture(t *testing.T) *chatbug075Log {
	t.Helper()
	captured := &chatbug075Log{}
	previous := slog.Default()
	slog.SetDefault(slog.New(captured))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return captured
}

// chatbug075Personas counts how often the agent directory is built.
type chatbug075Personas struct {
	profiles []agentpersona.PersonaVersion
	mu       sync.Mutex
	reads    int
}

func (p *chatbug075Personas) ListAvailable(context.Context, *trust.Principal) ([]agentpersona.PersonaVersion, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	return p.profiles, nil
}

func (p *chatbug075Personas) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads
}

// chatbug075Invocations counts the reads the activity stream makes.
type chatbug075Invocations struct {
	*personaSurfaceInvocationsFixture
	mu    sync.Mutex
	reads int
}

func (s *chatbug075Invocations) ListPersonaInvocations(ctx context.Context, tenant, owner, room string) ([]agentinvoke.Invocation, error) {
	s.mu.Lock()
	s.reads++
	s.mu.Unlock()
	return s.personaSurfaceInvocationsFixture.ListPersonaInvocations(ctx, tenant, owner, room)
}

func (s *chatbug075Invocations) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

const chatbug075Empty = "hcmnext.persona_projection_empty"

// An idle open conversation with no agent: the activity is read once a second
// for thirty seconds, the page reads the directory when it opens and a few
// times more, and the server builds the directory only for the page and writes
// one line about the empty directory, at DEBUG, for the life of the process.
func TestTodo_CHATBUG_075(t *testing.T) {
	logged := chatbug075Capture(t)
	surface, ctx, _, invocations, _ := personaSurfaceFixture(t)
	personas := &chatbug075Personas{}
	surface.Personas, surface.memory = personas, &personaSurfaceMemory{}
	invocations.rows = nil
	surface.References.(*personaSurfaceReferencesFixture).candidates = nil

	for second := 0; second < 30; second++ {
		progress, err := surface.Progress(ctx, "channel-a")
		if err != nil || len(progress.Invocations) != 0 {
			t.Fatalf("second %d: %+v %v", second, progress, err)
		}
	}
	if personas.count() != 0 {
		t.Fatalf("thirty idle seconds built the agent directory %d times, want 0", personas.count())
	}
	if n := logged.count(chatbug075Empty, slog.LevelInfo) + logged.count(chatbug075Empty, slog.LevelDebug); n != 0 {
		t.Fatalf("thirty idle seconds wrote %d lines about an empty directory, want 0", n)
	}

	// The page's own reads: on open, and again whenever membership changes.
	for read := 0; read < 5; read++ {
		directory, err := surface.Directory(ctx, "channel-a")
		if err != nil || len(directory.Personas) != 0 {
			t.Fatalf("directory read %d: %+v %v", read, directory, err)
		}
	}
	if personas.count() != 5 {
		t.Fatalf("the page's five directory reads reached the store %d times", personas.count())
	}
	if info := logged.count(chatbug075Empty, slog.LevelInfo); info != 0 {
		t.Fatalf("an empty directory is logged at INFO %d times, want never", info)
	}
	if debug := logged.count(chatbug075Empty, slog.LevelDebug); debug != 1 {
		t.Fatalf("an empty directory is logged at DEBUG %d times, want once per conversation per process", debug)
	}
	// Another conversation is its own finding; the first stays quiet.
	room := surface.Chat.(*personaSurfaceChatFixture)
	room.room.ID = "channel-b"
	room.members[0].ConversationID = "channel-b"
	if _, err := surface.Directory(ctx, "channel-b"); err != nil {
		t.Fatal(err)
	}
	if debug := logged.count(chatbug075Empty, slog.LevelDebug); debug != 2 {
		t.Fatalf("a second conversation with no agent wrote %d lines in all, want 2", debug)
	}
}

// A conversation with a run: the agents are named from the directory once a
// minute, not once a second, and a refused agent is logged once.
func TestTodo_CHATBUG_075_Integration(t *testing.T) {
	logged := chatbug075Capture(t)
	surface, ctx, _, _, _ := personaSurfaceFixture(t)
	profiles, _ := surface.Personas.ListAvailable(ctx, nil)
	personas := &chatbug075Personas{profiles: profiles}
	now := surface.Now()
	surface.Personas, surface.memory, surface.Now = personas, &personaSurfaceMemory{}, func() time.Time { return now }

	for second := 0; second < 30; second++ {
		progress, err := surface.Progress(ctx, "channel-a")
		if err != nil || len(progress.Invocations) != 1 || progress.Invocations[0].AgentName == "" {
			t.Fatalf("second %d: the run lost its agent's name: %+v %v", second, progress, err)
		}
		now = now.Add(time.Second)
	}
	if personas.count() != 1 {
		t.Fatalf("thirty seconds of one run built the agent directory %d times, want once", personas.count())
	}
	now = now.Add(personaSurfaceNamesTTL)
	if _, err := surface.Progress(ctx, "channel-a"); err != nil || personas.count() != 2 {
		t.Fatalf("after a minute the names were not read again: reads=%d err=%v", personas.count(), err)
	}

	// An agent the directory leaves out is logged once, not on every read.
	surface.References.(*personaSurfaceReferencesFixture).candidates[0].Eligible = false
	for read := 0; read < 10; read++ {
		if _, err := surface.Directory(ctx, "channel-a"); err != nil {
			t.Fatal(err)
		}
	}
	if omitted := logged.count("hcmnext.persona_projection_item_omitted", slog.LevelInfo); omitted != 1 {
		t.Fatalf("one omitted agent was logged %d times over ten reads, want once", omitted)
	}
	// A surface composed without a memory still works; it just remembers nothing.
	surface.memory = nil
	for read := 0; read < 2; read++ {
		if _, err := surface.Directory(ctx, "channel-a"); err != nil {
			t.Fatal(err)
		}
	}
	if omitted := logged.count("hcmnext.persona_projection_item_omitted", slog.LevelInfo); omitted != 3 {
		t.Fatalf("without a memory two reads logged %d lines in all, want 3", omitted)
	}
}

// The same count through the served boundary: the activity stream of an idle
// conversation runs for thirty ticks and the log holds nothing about it.
func TestTodo_CHATBUG_075_Browser(t *testing.T) {
	logged := chatbug075Capture(t)
	surface, ctx, _, invocations, _ := personaSurfaceFixture(t)
	personas := &chatbug075Personas{}
	counted := &chatbug075Invocations{personaSurfaceInvocationsFixture: invocations}
	surface.Personas, surface.memory, surface.Invocations = personas, &personaSurfaceMemory{}, counted
	invocations.rows = nil
	surface.References.(*personaSurfaceReferencesFixture).candidates = nil

	watch, cancel := context.WithCancel(ctx)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, personachat.Path+"/invocations?conversation_id=channel-a&watch=1", nil).WithContext(watch)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		personachat.Handler{Surface: surface, WatchInterval: time.Millisecond}.ServeHTTP(response, request)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for counted.count() < 31 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if counted.count() < 31 {
		t.Fatalf("the stream read the activity %d times, want the first read and thirty ticks", counted.count())
	}
	if response.Code != http.StatusOK {
		t.Fatalf("stream status %d", response.Code)
	}
	if personas.count() != 0 {
		t.Fatalf("an idle stream built the agent directory %d times, want 0", personas.count())
	}
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if n := logged.count(chatbug075Empty, level); n != 0 {
			t.Fatalf("an idle stream wrote %d lines about an empty directory at %s", n, level)
		}
	}
}
