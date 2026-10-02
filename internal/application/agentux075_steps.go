package application

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// AGENTUX-075 part 2: the working message under a question says what the run is
// doing now. The run notes each stage and each tool call here, as a typed kind
// and, where one applies, the name of the thing (a document's title, the subject
// of a search); the progress read puts the latest note on the invocation row the
// asker's page already polls. Nothing here is an activity word or an internal
// identifier: the page turns the kind into a sentence in the person's language.

// The typed kinds a run reports.
const (
	personaStepReadingQuestion = "reading_question"
	personaStepSearching       = "searching"
	personaStepReadingDocument = "reading_document"
	personaStepReadingMany     = "reading_documents"
	personaStepWriting         = "writing"
	personaStepWorking         = "working"
)

const (
	// personaRunStepTTL is how long a note outlives its last update. A run that
	// stops noting is over or lost; the read then falls back to its checkpoints.
	personaRunStepTTL = 15 * time.Minute
	// personaRunStepCap bounds the board, so a flood of runs cannot grow it.
	personaRunStepCap = 4096
	// personaRunStepSubjectRunes bounds a subject shown on the page.
	personaRunStepSubjectRunes = 80
)

type personaRunStepKey struct{ tenant, invocation string }

type personaRunStep struct {
	kind, subject string
	at            time.Time
}

// personaRunStepBoard holds the latest step of each run in flight. It is a value
// the serving composition owns and hands to the run (through its context) and to
// the progress read; it holds nothing durable, so a restart falls back to the
// run's checkpoints.
type personaRunStepBoard struct {
	mu    sync.Mutex
	now   func() time.Time
	steps map[personaRunStepKey]personaRunStep
}

func newPersonaRunStepBoard(now func() time.Time) *personaRunStepBoard {
	if now == nil {
		now = time.Now
	}
	return &personaRunStepBoard{now: now, steps: make(map[personaRunStepKey]personaRunStep)}
}

// Note records what the run of invocation is doing now.
func (b *personaRunStepBoard) Note(tenant, invocation, kind, subject string) {
	if b == nil || tenant == "" || invocation == "" || kind == "" {
		return
	}
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.steps) >= personaRunStepCap {
		for key, step := range b.steps {
			if now.Sub(step.at) > personaRunStepTTL {
				delete(b.steps, key)
			}
		}
		if len(b.steps) >= personaRunStepCap {
			// Still full of live runs: drop the oldest note rather than refuse this one.
			var oldest personaRunStepKey
			var oldestAt time.Time
			for key, step := range b.steps {
				if oldestAt.IsZero() || step.at.Before(oldestAt) {
					oldest, oldestAt = key, step.at
				}
			}
			delete(b.steps, oldest)
		}
	}
	b.steps[personaRunStepKey{tenant, invocation}] = personaRunStep{kind: kind, subject: cleanPersonaStepSubject(subject), at: now}
}

// Current is the latest note of the run of invocation, when it is still fresh.
func (b *personaRunStepBoard) Current(tenant, invocation string) (kind, subject string, ok bool) {
	if b == nil {
		return "", "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	step, found := b.steps[personaRunStepKey{tenant, invocation}]
	if !found || b.now().Sub(step.at) > personaRunStepTTL {
		return "", "", false
	}
	return step.kind, step.subject, true
}

// cleanPersonaStepSubject keeps a subject to one short printable line.
func cleanPersonaStepSubject(subject string) string {
	subject = strings.Join(strings.FieldsFunc(subject, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
	if !utf8.ValidString(subject) {
		return ""
	}
	if runes := []rune(subject); len(runes) > personaRunStepSubjectRunes {
		subject = string(runes[:personaRunStepSubjectRunes-1]) + "…"
	}
	return subject
}

type personaRunStepBoardKey struct{}

// withPersonaRunSteps hands the board to the run that the context reaches.
func withPersonaRunSteps(ctx context.Context, board *personaRunStepBoard) context.Context {
	if board == nil {
		return ctx
	}
	return context.WithValue(ctx, personaRunStepBoardKey{}, board)
}

// personaRunStepNoter returns the function a run calls to report its step. With
// no board in the context it does nothing.
func personaRunStepNoter(ctx context.Context, admission agentrun.Record) func(kind, subject string) {
	board, _ := ctx.Value(personaRunStepBoardKey{}).(*personaRunStepBoard)
	tenant, invocation := admission.Request.Source.TenantID, admission.Request.Source.Key
	if board == nil || tenant == "" || invocation == "" {
		return func(string, string) {}
	}
	return func(kind, subject string) { board.Note(tenant, invocation, kind, subject) }
}

// personaStepForTool names the step a tool call is: a document search is
// "searching", with the subject of the search when its arguments carry one; any
// other tool is "working", and its name is never shown.
func personaStepForTool(name string, arguments json.RawMessage) (kind, subject string) {
	switch name {
	case personaDocumentSearchTool, personaWorkspaceSearchTool:
		var args struct {
			Query string `json:"query"`
		}
		if json.Unmarshal(arguments, &args) == nil {
			subject = args.Query
		}
		return personaStepSearching, subject
	}
	return personaStepWorking, ""
}

// personaStepForDocuments is the step after a search returned: one document is
// read by its title, several by their number.
func personaStepForDocuments(documents []personaQualitySearchedDocument) (kind, subject string) {
	seen := make(map[string]bool, len(documents))
	title := ""
	for _, document := range documents {
		key := document.DocumentID
		if key == "" {
			key = document.Title
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if title == "" {
			title = strings.TrimSpace(document.Title)
		}
	}
	switch {
	case len(seen) == 1 && title != "":
		return personaStepReadingDocument, title
	case len(seen) > 0:
		return personaStepReadingMany, ""
	}
	return personaStepWriting, ""
}

// personaStepFromRun is the step a run is at when no note is held for it (the
// server restarted, or another process holds the run): what its durable
// checkpoints say, and never a name.
func personaStepFromRun(run runstate.Run) string {
	step := personaStepReadingQuestion
	for _, checkpoint := range run.Checkpoints {
		switch checkpoint.Phase {
		case runstate.PhaseToolCall:
			step = personaStepReadingMany
		case runstate.PhaseModelCall:
			// The first model turn only proposes the search; the second writes.
			if checkpoint.Attempt >= 2 {
				step = personaStepWriting
			}
		case runstate.PhaseValidation, runstate.PhaseDelivery:
			step = personaStepWriting
		}
	}
	return step
}

// personaStatusInFlight reports whether a run in this state is still at work.
func personaStatusInFlight(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "claimed", "started", "ready", "running", "waiting", "reconciling":
		return true
	}
	return false
}

// projectPersonaStep puts the step of a run in flight on its invocation row: the
// run's own note when it holds a fresh one, else what its checkpoints say. A run
// that is over has no step.
func (s *PersonaChatSurface) projectPersonaStep(out *personachat.Invocation, invocation agentinvoke.Invocation, run runstate.Run, haveRun bool) {
	if out == nil || !personaStatusInFlight(out.Status) {
		return
	}
	if s != nil {
		if kind, subject, ok := s.Steps.Current(invocation.TenantID, invocation.ID); ok {
			out.StepKind, out.StepSubject = kind, subject
			return
		}
	}
	out.StepKind = personaStepReadingQuestion
	if haveRun {
		out.StepKind = personaStepFromRun(run)
	}
}
