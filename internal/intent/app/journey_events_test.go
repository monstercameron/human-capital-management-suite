package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

func TestJourneyEventLevelsOutcomesAndRequestIDs(t *testing.T) {
	var buf bytes.Buffer
	engine := &journeyEngine{events: slog.New(logging.NewHandler(&buf, logging.WithService("hcmnext")))}
	ctx := logging.WithRequestID(context.Background(), "req:abc")

	engine.journeyEvent(ctx, "journey.decision_recorded", "intent-1", nil, slog.Bool("approve", true))
	engine.journeyEvent(ctx, "journey.decision_recorded", "intent-1", fmt.Errorf("%w: closed", workspace.ErrJourneyStage))
	engine.journeyEvent(ctx, "journey.decision_recorded", "intent-1", errors.New("database exploded: secret detail"))

	var lines []map[string]any
	for _, raw := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("not a JSON envelope: %s", raw)
		}
		lines = append(lines, line)
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	for i, want := range []struct{ level, outcome string }{{"INFO", "ok"}, {"WARN", "wrong_stage"}, {"ERROR", "failed"}} {
		attrs, _ := lines[i]["attrs"].(map[string]any)
		if lines[i]["level"] != want.level || attrs["outcome"] != want.outcome || attrs["intent_id"] != "intent-1" {
			t.Errorf("line %d = %v", i, lines[i])
		}
		if lines[i]["request_id"] != "req:abc" {
			t.Errorf("line %d lost the request id: %v", i, lines[i])
		}
	}
	if bytes.Contains(buf.Bytes(), []byte("secret detail")) {
		t.Fatal("an error's text reached the business event log")
	}
}

func TestJourneyEventWithoutLoggerIsSilent(t *testing.T) {
	var engine *journeyEngine
	engine.journeyEvent(context.Background(), "journey.proposed", "x", nil) // must not panic
	(&journeyEngine{}).journeyEvent(context.Background(), "journey.proposed", "x", nil)
}

func TestJourneyDecisionFailureLogsOwnedReasonWithoutPayload(t *testing.T) {
	var buf bytes.Buffer
	engine := &journeyEngine{events: slog.New(logging.NewHandler(&buf))}
	err := envelope.New(envelope.CodeUnavailable, "intent.simulation.failed", "private proposal contents")
	engine.journeyEvent(context.Background(), "journey.decision_recorded", "intent-1", err, slog.String("decision_phase", "revalidate"))
	var line struct {
		Attrs map[string]any `json:"attrs"`
	}
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line.Attrs["reason_ref"] != "intent.simulation.failed" || line.Attrs["error_code"] != envelope.CodeUnavailable.String() || line.Attrs["decision_phase"] != "revalidate" {
		t.Fatalf("missing safe diagnostic: %v", line.Attrs)
	}
	if bytes.Contains(buf.Bytes(), []byte("private proposal contents")) {
		t.Fatal("private message logged")
	}
}

func TestJourneyErrorClassNamesEverySentinel(t *testing.T) {
	for err, want := range map[error]string{
		workspace.ErrJourneyInput:          "invalid_input",
		workspace.ErrDenied:                "denied",
		workspace.ErrJourneyStage:          "wrong_stage",
		workspace.ErrJourneyUnknown:        "not_found",
		workspace.ErrJourneyActiveConflict: "active_conflict",
		workspace.ErrJourneyUnavailable:    "unavailable",
		errors.New("other"):                "failed",
	} {
		if got := journeyErrorClass(fmt.Errorf("wrapped: %w", err)); got != want {
			t.Errorf("class(%v) = %q, want %q", err, got, want)
		}
	}
}
