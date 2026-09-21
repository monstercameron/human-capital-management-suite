// Package workflowdraftstore persists mutable, expiring workflow-designer
// autosaves separately from the immutable compiled-version registry.
package workflowdraftstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var (
	// ErrInvalid reports a malformed draft or unavailable dependency.
	ErrInvalid = errors.New("workflowdraftstore: invalid input")
	// ErrConflict reports an optimistic-revision or author-ownership conflict.
	ErrConflict = errors.New("workflowdraftstore: revision conflict")
	// ErrNotFound reports a draft that does not exist in the tenant scope.
	ErrNotFound = errors.New("workflowdraftstore: draft not found")
)

// Draft is one recoverable workflow editing session.
type Draft struct {
	TenantID          uuid.UUID
	DraftID           uuid.UUID
	WorkflowID        string
	AuthorRef         string
	SemanticVersion   string
	BaseVersionDigest string
	Revision          uint64
	HistoryPosition   uint64
	HistoryLength     uint64
	Document          json.RawMessage
	ExpiresAt         time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// SaveRequest creates or replaces one autosave with optimistic revisioning.
type SaveRequest struct {
	DraftID           uuid.UUID
	WorkflowID        string
	AuthorRef         string
	SemanticVersion   string
	BaseVersionDigest string
	ExpectedRevision  uint64
	CommandLabel      string
	Document          json.RawMessage
	ExpiresAt         time.Time
	At                time.Time
}

// History is the durable undo/redo cursor and the two documents needed to
// explain the current semantic change. Documents never leave the application
// boundary; transport receives only the derived change projection.
type History struct {
	Position     uint64
	Length       uint64
	CurrentLabel string
	Current      json.RawMessage
	Previous     json.RawMessage
}

// NavigateRequest moves the history cursor while advancing the optimistic
// draft revision. Direction is deliberately closed at this package boundary.
type NavigateRequest struct {
	DraftID          uuid.UUID
	AuthorRef        string
	ExpectedRevision uint64
	Direction        string
	At               time.Time
}

// Store is the PostgreSQL-backed mutable draft store.
type Store struct {
	db     dbport.Beginner
	tenant func(values.TenantId) uuid.UUID
}

// New constructs a tenant-scoped workflow draft store.
func New(parseDB dbport.Beginner, parseTenant func(values.TenantId) uuid.UUID) *Store {
	return &Store{db: parseDB, tenant: parseTenant}
}

// Save creates a draft at revision one or atomically advances the expected
// revision. The author recorded at creation remains the owner of later saves.
func (parseStore *Store) Save(parseContext context.Context, parseTenant values.TenantId, parseRequest SaveRequest) (Draft, error) {
	parseRequest.WorkflowID = strings.TrimSpace(parseRequest.WorkflowID)
	parseRequest.AuthorRef = strings.TrimSpace(parseRequest.AuthorRef)
	parseRequest.SemanticVersion = strings.TrimSpace(parseRequest.SemanticVersion)
	parseRequest.BaseVersionDigest = strings.TrimSpace(parseRequest.BaseVersionDigest)
	parseRequest.CommandLabel = strings.TrimSpace(parseRequest.CommandLabel)
	if parseRequest.CommandLabel == "" {
		parseRequest.CommandLabel = "Edit workflow"
	}
	if parseRequest.At.IsZero() {
		parseRequest.At = time.Now().UTC()
	} else {
		parseRequest.At = parseRequest.At.UTC()
	}
	parseRequest.ExpiresAt = parseRequest.ExpiresAt.UTC()
	if parseRequest.DraftID == uuid.Nil || parseRequest.WorkflowID == "" || parseRequest.AuthorRef == "" || workflowversion.ValidateSemanticVersion(parseRequest.SemanticVersion) != nil ||
		parseRequest.ExpiresAt.IsZero() || !parseRequest.ExpiresAt.After(parseRequest.At) || !isDraftDocument(parseRequest.Document) {
		return Draft{}, ErrInvalid
	}

	var parseSaved Draft
	parseErr := parseStore.withTenant(parseContext, parseTenant, func(parseTx dbport.Tx, parseTenantID uuid.UUID) error {
		if parseRequest.ExpectedRevision == 0 {
			parseAffected, parseInsertErr := parseTx.Exec(parseContext, `INSERT INTO workflow_designer_draft
				(tenant_id,draft_id,workflow_id,author_ref,semantic_version,base_version_digest,revision,history_position,history_length,document,expires_at,created_at,updated_at)
				VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),1,1,1,$7,$8,$9,$9) ON CONFLICT DO NOTHING`,
				parseTenantID, parseRequest.DraftID, parseRequest.WorkflowID, parseRequest.AuthorRef,
				parseRequest.SemanticVersion, parseRequest.BaseVersionDigest, []byte(parseRequest.Document), parseRequest.ExpiresAt, parseRequest.At)
			if parseInsertErr != nil {
				return fmt.Errorf("workflowdraftstore: create draft: %w", parseInsertErr)
			}
			if parseAffected != 1 {
				return ErrConflict
			}
			if _, parseInsertErr = parseTx.Exec(parseContext, `INSERT INTO workflow_designer_draft_history
				(tenant_id,draft_id,history_position,command_label,document,created_at) VALUES ($1,$2,1,$3,$4,$5)`,
				parseTenantID, parseRequest.DraftID, parseRequest.CommandLabel, []byte(parseRequest.Document), parseRequest.At); parseInsertErr != nil {
				return fmt.Errorf("workflowdraftstore: create history: %w", parseInsertErr)
			}
		} else {
			var parsePosition, parseLength uint64
			parseLockErr := parseTx.QueryRow(parseContext, `SELECT history_position,history_length FROM workflow_designer_draft
				WHERE tenant_id=$1 AND draft_id=$2 AND author_ref=$3 AND revision=$4 FOR UPDATE`,
				parseTenantID, parseRequest.DraftID, parseRequest.AuthorRef, parseRequest.ExpectedRevision).Scan(&parsePosition, &parseLength)
			if errors.Is(parseLockErr, dbport.ErrNoRows) {
				return ErrConflict
			}
			if parseLockErr != nil {
				return fmt.Errorf("workflowdraftstore: lock draft history: %w", parseLockErr)
			}
			if parsePosition < parseLength {
				if _, parseDeleteErr := parseTx.Exec(parseContext,
					`SELECT hcmnext_discard_workflow_draft_redo($1,$2,$3)`,
					parseTenantID, parseRequest.DraftID, parsePosition); parseDeleteErr != nil {
					return fmt.Errorf("workflowdraftstore: discard redo history: %w", parseDeleteErr)
				}
			}
			parseNextPosition := parsePosition + 1
			if _, parseInsertErr := parseTx.Exec(parseContext, `INSERT INTO workflow_designer_draft_history
				(tenant_id,draft_id,history_position,command_label,document,created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
				parseTenantID, parseRequest.DraftID, parseNextPosition, parseRequest.CommandLabel, []byte(parseRequest.Document), parseRequest.At); parseInsertErr != nil {
				return fmt.Errorf("workflowdraftstore: append history: %w", parseInsertErr)
			}
			parseAffected, parseUpdateErr := parseTx.Exec(parseContext, `UPDATE workflow_designer_draft
				SET workflow_id=$4, semantic_version=$5, base_version_digest=NULLIF($6,''), revision=revision+1,
					history_position=$11, history_length=$11, document=$7, expires_at=$8, updated_at=$9
				WHERE tenant_id=$1 AND draft_id=$2 AND author_ref=$3 AND revision=$10`,
				parseTenantID, parseRequest.DraftID, parseRequest.AuthorRef, parseRequest.WorkflowID,
				parseRequest.SemanticVersion, parseRequest.BaseVersionDigest, []byte(parseRequest.Document), parseRequest.ExpiresAt, parseRequest.At, parseRequest.ExpectedRevision, parseNextPosition)
			if parseUpdateErr != nil {
				return fmt.Errorf("workflowdraftstore: update draft: %w", parseUpdateErr)
			}
			if parseAffected != 1 {
				return ErrConflict
			}
		}
		var parseLoadErr error
		parseSaved, parseLoadErr = loadDraft(parseContext, parseTx, parseTenantID, parseRequest.DraftID)
		return parseLoadErr
	})
	return parseSaved, parseErr
}

// LoadHistory returns the current cursor plus the preceding snapshot used for
// semantic diff. A draft at its first snapshot has an empty Previous value.
func (parseStore *Store) LoadHistory(parseContext context.Context, parseTenant values.TenantId, parseDraftID uuid.UUID) (History, error) {
	if parseDraftID == uuid.Nil {
		return History{}, ErrInvalid
	}
	var parseHistory History
	parseErr := parseStore.withTenant(parseContext, parseTenant, func(parseTx dbport.Tx, parseTenantID uuid.UUID) error {
		parseErr := parseTx.QueryRow(parseContext, `SELECT d.history_position,d.history_length,h.command_label,h.document
			FROM workflow_designer_draft d
			JOIN workflow_designer_draft_history h ON h.tenant_id=d.tenant_id AND h.draft_id=d.draft_id AND h.history_position=d.history_position
			WHERE d.tenant_id=$1 AND d.draft_id=$2`, parseTenantID, parseDraftID).Scan(
			&parseHistory.Position, &parseHistory.Length, &parseHistory.CurrentLabel, &parseHistory.Current)
		if errors.Is(parseErr, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if parseErr != nil {
			return fmt.Errorf("workflowdraftstore: load history: %w", parseErr)
		}
		if parseHistory.Position > 1 {
			if parseErr = parseTx.QueryRow(parseContext, `SELECT document FROM workflow_designer_draft_history
				WHERE tenant_id=$1 AND draft_id=$2 AND history_position=$3`, parseTenantID, parseDraftID, parseHistory.Position-1).Scan(&parseHistory.Previous); parseErr != nil {
				return fmt.Errorf("workflowdraftstore: load previous history: %w", parseErr)
			}
		}
		parseHistory.Current = append(json.RawMessage(nil), parseHistory.Current...)
		parseHistory.Previous = append(json.RawMessage(nil), parseHistory.Previous...)
		return nil
	})
	return parseHistory, parseErr
}

// Navigate restores the adjacent immutable snapshot while preserving a
// strictly increasing revision. New edits after an undo discard only the
// abandoned redo branch; retained history documents are never rewritten.
func (parseStore *Store) Navigate(parseContext context.Context, parseTenant values.TenantId, parseRequest NavigateRequest) (Draft, error) {
	parseRequest.AuthorRef = strings.TrimSpace(parseRequest.AuthorRef)
	parseRequest.Direction = strings.ToUpper(strings.TrimSpace(parseRequest.Direction))
	if parseRequest.At.IsZero() {
		parseRequest.At = time.Now().UTC()
	} else {
		parseRequest.At = parseRequest.At.UTC()
	}
	if parseRequest.DraftID == uuid.Nil || parseRequest.AuthorRef == "" || parseRequest.ExpectedRevision == 0 ||
		(parseRequest.Direction != "UNDO" && parseRequest.Direction != "REDO") {
		return Draft{}, ErrInvalid
	}
	var parseSaved Draft
	parseErr := parseStore.withTenant(parseContext, parseTenant, func(parseTx dbport.Tx, parseTenantID uuid.UUID) error {
		var parsePosition, parseLength uint64
		parseLockErr := parseTx.QueryRow(parseContext, `SELECT history_position,history_length FROM workflow_designer_draft
			WHERE tenant_id=$1 AND draft_id=$2 AND author_ref=$3 AND revision=$4 FOR UPDATE`,
			parseTenantID, parseRequest.DraftID, parseRequest.AuthorRef, parseRequest.ExpectedRevision).Scan(&parsePosition, &parseLength)
		if errors.Is(parseLockErr, dbport.ErrNoRows) {
			return ErrConflict
		}
		if parseLockErr != nil {
			return fmt.Errorf("workflowdraftstore: lock history navigation: %w", parseLockErr)
		}
		parseTarget := parsePosition
		if parseRequest.Direction == "UNDO" && parsePosition > 1 {
			parseTarget--
		} else if parseRequest.Direction == "REDO" && parsePosition < parseLength {
			parseTarget++
		} else {
			return ErrConflict
		}
		var parseDocument json.RawMessage
		if parseLoadErr := parseTx.QueryRow(parseContext, `SELECT document FROM workflow_designer_draft_history
			WHERE tenant_id=$1 AND draft_id=$2 AND history_position=$3`, parseTenantID, parseRequest.DraftID, parseTarget).Scan(&parseDocument); parseLoadErr != nil {
			return fmt.Errorf("workflowdraftstore: load navigation target: %w", parseLoadErr)
		}
		parseAffected, parseUpdateErr := parseTx.Exec(parseContext, `UPDATE workflow_designer_draft
			SET revision=revision+1,history_position=$5,document=$6,updated_at=$7
			WHERE tenant_id=$1 AND draft_id=$2 AND author_ref=$3 AND revision=$4`,
			parseTenantID, parseRequest.DraftID, parseRequest.AuthorRef, parseRequest.ExpectedRevision, parseTarget, []byte(parseDocument), parseRequest.At)
		if parseUpdateErr != nil {
			return fmt.Errorf("workflowdraftstore: navigate history: %w", parseUpdateErr)
		}
		if parseAffected != 1 {
			return ErrConflict
		}
		var parseLoadErr error
		parseSaved, parseLoadErr = loadDraft(parseContext, parseTx, parseTenantID, parseRequest.DraftID)
		return parseLoadErr
	})
	return parseSaved, parseErr
}

// Load restores the latest durable autosave for an interrupted edit session.
func (parseStore *Store) Load(parseContext context.Context, parseTenant values.TenantId, parseDraftID uuid.UUID) (Draft, error) {
	if parseDraftID == uuid.Nil {
		return Draft{}, ErrInvalid
	}
	var parseDraft Draft
	parseErr := parseStore.withTenant(parseContext, parseTenant, func(parseTx dbport.Tx, parseTenantID uuid.UUID) error {
		var parseLoadErr error
		parseDraft, parseLoadErr = loadDraft(parseContext, parseTx, parseTenantID, parseDraftID)
		return parseLoadErr
	})
	return parseDraft, parseErr
}

// PurgeExpired removes abandoned drafts whose explicit retention window has
// elapsed and returns the number removed.
func (parseStore *Store) PurgeExpired(parseContext context.Context, parseTenant values.TenantId, parseAt time.Time) (int64, error) {
	if parseAt.IsZero() {
		return 0, ErrInvalid
	}
	var parseRemoved int64
	parseErr := parseStore.withTenant(parseContext, parseTenant, func(parseTx dbport.Tx, parseTenantID uuid.UUID) error {
		var parseDeleteErr error
		parseDeleteErr = parseTx.QueryRow(parseContext,
			`SELECT hcmnext_purge_expired_workflow_drafts($1,$2)`,
			parseTenantID, parseAt.UTC()).Scan(&parseRemoved)
		if parseDeleteErr != nil {
			return fmt.Errorf("workflowdraftstore: purge expired drafts: %w", parseDeleteErr)
		}
		return nil
	})
	return parseRemoved, parseErr
}

func loadDraft(parseContext context.Context, parseDB dbport.Querier, parseTenantID uuid.UUID, parseDraftID uuid.UUID) (Draft, error) {
	var parseDraft Draft
	parseErr := parseDB.QueryRow(parseContext, `SELECT tenant_id,draft_id,workflow_id,author_ref,semantic_version,COALESCE(base_version_digest,''),revision,history_position,history_length,document,expires_at,created_at,updated_at
		FROM workflow_designer_draft WHERE tenant_id=$1 AND draft_id=$2`, parseTenantID, parseDraftID).Scan(
		&parseDraft.TenantID, &parseDraft.DraftID, &parseDraft.WorkflowID, &parseDraft.AuthorRef,
		&parseDraft.SemanticVersion, &parseDraft.BaseVersionDigest, &parseDraft.Revision, &parseDraft.HistoryPosition, &parseDraft.HistoryLength, &parseDraft.Document, &parseDraft.ExpiresAt,
		&parseDraft.CreatedAt, &parseDraft.UpdatedAt)
	if errors.Is(parseErr, dbport.ErrNoRows) {
		return Draft{}, ErrNotFound
	}
	if parseErr != nil {
		return Draft{}, fmt.Errorf("workflowdraftstore: load draft: %w", parseErr)
	}
	parseDraft.Document = append(json.RawMessage(nil), parseDraft.Document...)
	return parseDraft, nil
}

func isDraftDocument(parseDocument json.RawMessage) bool {
	if !json.Valid(parseDocument) {
		return false
	}
	var parseValue map[string]json.RawMessage
	return json.Unmarshal(parseDocument, &parseValue) == nil && parseValue != nil
}

func (parseStore *Store) withTenant(parseContext context.Context, parseTenant values.TenantId, parseRun func(dbport.Tx, uuid.UUID) error) error {
	if parseStore == nil || parseStore.db == nil || parseStore.tenant == nil || parseRun == nil {
		return ErrInvalid
	}
	parseTenantID := parseStore.tenant(parseTenant)
	if parseTenantID == uuid.Nil {
		return ErrInvalid
	}
	parseTx, parseErr := parseStore.db.Begin(parseContext)
	if parseErr != nil {
		return fmt.Errorf("workflowdraftstore: begin: %w", parseErr)
	}
	defer func() { _ = parseTx.Rollback(parseContext) }()
	if parseErr = tenancy.WithTenant(parseContext, parseTx, parseTenantID); parseErr != nil {
		return parseErr
	}
	if parseErr = parseRun(parseTx, parseTenantID); parseErr != nil {
		return parseErr
	}
	if parseErr = parseTx.Commit(parseContext); parseErr != nil {
		return fmt.Errorf("workflowdraftstore: commit: %w", parseErr)
	}
	return nil
}
