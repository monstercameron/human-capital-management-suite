package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// Journey notes (migrations/00312_journey_note.sql): free-standing,
// append-only notes on one promotion journey. A note is written by anyone the
// journey detail admits and read by the same audience; it is not a decision
// and changes nothing about the journey.

var _ workspace.JourneyNoteEngine = (*journeyEngine)(nil)

// AddNote implements [workspace.JourneyNoteEngine].
//
// Admission is the detail read itself: the note is recorded only after
// [journeyEngine.Inspect] has admitted the caller to this journey, and the
// stage it records is the stage that read returned. A retry with the same
// idempotency key returns the first note; the same key reused for a
// different note, journey or author is refused rather than silently answered
// with somebody else's note.
func (e *journeyEngine) AddNote(ctx context.Context, intentID string, in workspace.JourneyNoteInput) (recorded workspace.JourneyNote, _ workspace.JourneyDetail, retErr error) {
	defer func() {
		e.journeyEvent(ctx, "journey.note_added", intentID, retErr, slog.String("note_id", recorded.NoteID), slog.String("stage", string(recorded.Stage)))
	}()
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return workspace.JourneyNote{}, workspace.JourneyDetail{}, err
	}
	normalized, err := workspace.NormalizeJourneyNote(in)
	if err != nil {
		return workspace.JourneyNote{}, workspace.JourneyDetail{}, err
	}
	intentUUID, parseErr := uuid.Parse(intentID)
	if parseErr != nil {
		return workspace.JourneyNote{}, workspace.JourneyDetail{}, fmt.Errorf("%w: %s", workspace.ErrJourneyUnknown, intentID)
	}
	detail, err := e.Inspect(ctx, intentID)
	if err != nil {
		return workspace.JourneyNote{}, workspace.JourneyDetail{}, err
	}

	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return workspace.JourneyNote{}, workspace.JourneyDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantID := e.svc.tenantUUID(principal.Tenant())
	author := principal.Subject()

	stored, found, err := readJourneyNoteByKey(ctx, tx, tenantID, normalized.IdempotencyKey)
	if err != nil {
		return workspace.JourneyNote{}, workspace.JourneyDetail{}, err
	}
	if !found {
		var count int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM journey_note WHERE tenant_id = $1 AND intent_id = $2`,
			tenantID, intentUUID).Scan(&count); err != nil {
			return workspace.JourneyNote{}, workspace.JourneyDetail{}, fmt.Errorf("app: journey: count notes: %w", err)
		}
		if count >= workspace.MaxJourneyNotes {
			return workspace.JourneyNote{}, workspace.JourneyDetail{}, &workspace.JourneyInputError{
				FieldPath: "body", ReasonRef: workspace.JourneyNoteReasonLimit,
				Detail: fmt.Sprintf("this journey already has %d notes", workspace.MaxJourneyNotes)}
		}
		noteID, idErr := uuid.NewV7()
		if idErr != nil {
			return workspace.JourneyNote{}, workspace.JourneyDetail{}, fmt.Errorf("app: journey: mint note id: %w", idErr)
		}
		createdAt := e.now().UTC()
		inserted, insErr := tx.Exec(ctx, `
			INSERT INTO journey_note (tenant_id, note_id, intent_id, author_ref, body, stage, idempotency_key, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
			tenantID, noteID, intentUUID, author, normalized.Body, string(detail.Summary.Stage), normalized.IdempotencyKey, createdAt)
		if insErr != nil {
			return workspace.JourneyNote{}, workspace.JourneyDetail{}, fmt.Errorf("app: journey: record note: %w", insErr)
		}
		if inserted == 0 {
			// A concurrent submission with the same key won the insert; its
			// row is the answer, subject to the same sameness check below.
			if stored, found, err = readJourneyNoteByKey(ctx, tx, tenantID, normalized.IdempotencyKey); err != nil {
				return workspace.JourneyNote{}, workspace.JourneyDetail{}, err
			} else if !found {
				return workspace.JourneyNote{}, workspace.JourneyDetail{}, errors.New("app: journey: note insert conflicted with no visible row")
			}
		} else {
			stored = storedJourneyNote{
				intentID: intentUUID, note: workspace.JourneyNote{
					NoteID: noteID.String(), AuthorRef: author, Body: normalized.Body,
					Stage: detail.Summary.Stage, CreatedAt: createdAt,
				},
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return workspace.JourneyNote{}, workspace.JourneyDetail{}, fmt.Errorf("app: journey: commit note: %w", err)
		}
	}
	if stored.intentID != intentUUID || stored.note.AuthorRef != author || stored.note.Body != normalized.Body {
		return workspace.JourneyNote{}, workspace.JourneyDetail{}, &workspace.JourneyInputError{
			FieldPath: "idempotency_key", ReasonRef: workspace.JourneyNoteReasonKeyReused,
			Detail: "the idempotency key already recorded a different note"}
	}

	note := stored.note
	note.AuthoredByViewer = true
	note.AuthorDisplay = e.assigneeNameResolver(ctx, principal.Tenant())(author)
	if !containsJourneyNote(detail.Notes, note.NoteID) {
		detail.Notes = append(detail.Notes, note)
	}
	return note, detail, nil
}

type storedJourneyNote struct {
	intentID uuid.UUID
	note     workspace.JourneyNote
}

func readJourneyNoteByKey(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, key string) (storedJourneyNote, bool, error) {
	var (
		stored    storedJourneyNote
		noteID    uuid.UUID
		stage     string
		createdAt time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT note_id, intent_id, author_ref, body, stage, created_at
		FROM journey_note WHERE tenant_id = $1 AND idempotency_key = $2`,
		tenantID, key).Scan(&noteID, &stored.intentID, &stored.note.AuthorRef, &stored.note.Body, &stage, &createdAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return storedJourneyNote{}, false, nil
	}
	if err != nil {
		return storedJourneyNote{}, false, fmt.Errorf("app: journey: read note by key: %w", err)
	}
	stored.note.NoteID = noteID.String()
	stored.note.Stage = workspace.JourneyStage(stage)
	stored.note.CreatedAt = createdAt.UTC()
	return stored, true, nil
}

// readJourneyNotes returns one journey's notes oldest first, bounded by
// [workspace.MaxJourneyNotes]. Authors are disclosed by display name; the
// principal stays in AuthorRef, which transport never projects.
func readJourneyNotes(
	ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, intentID, viewer string, displayName func(string) string,
) ([]workspace.JourneyNote, error) {
	intentUUID, err := uuid.Parse(intentID)
	if err != nil {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT note_id, author_ref, body, stage, created_at
		FROM journey_note WHERE tenant_id = $1 AND intent_id = $2
		ORDER BY created_at, note_id LIMIT $3`,
		tenantID, intentUUID, workspace.MaxJourneyNotes)
	if err != nil {
		return nil, fmt.Errorf("app: journey: read notes: %w", err)
	}
	defer rows.Close()
	var notes []workspace.JourneyNote
	for rows.Next() {
		var (
			note      workspace.JourneyNote
			noteID    uuid.UUID
			stage     string
			createdAt time.Time
		)
		if err := rows.Scan(&noteID, &note.AuthorRef, &note.Body, &stage, &createdAt); err != nil {
			return nil, fmt.Errorf("app: journey: scan note: %w", err)
		}
		note.NoteID = noteID.String()
		note.Stage = workspace.JourneyStage(stage)
		note.CreatedAt = createdAt.UTC()
		note.AuthoredByViewer = note.AuthorRef == viewer
		if displayName != nil {
			note.AuthorDisplay = displayName(note.AuthorRef)
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("app: journey: read notes: %w", err)
	}
	return notes, nil
}

func containsJourneyNote(notes []workspace.JourneyNote, noteID string) bool {
	for _, note := range notes {
		if note.NoteID == noteID {
			return true
		}
	}
	return false
}
