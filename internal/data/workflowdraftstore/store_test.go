package workflowdraftstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowdraftstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(parseMain *testing.M) { pgtest.RunMain(parseMain) }

func buildDraftStore(parseT *testing.T) (*workflowdraftstore.Store, *pgtest.DB, uuid.UUID, values.TenantId) {
	parseT.Helper()
	parseDB := pgtest.New(parseT)
	parseTenantUUID := uuid.New()
	if _, parseErr := parseDB.Conn.Exec(context.Background(), `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, parseTenantUUID, "wf-ui-003-"+parseTenantUUID.String(), "Designer tenant"); parseErr != nil {
		parseT.Fatalf("insert tenant: %v", parseErr)
	}
	parseTenant := values.TenantId("tenant:wf-ui-003:" + parseTenantUUID.String())
	return workflowdraftstore.New(parseDB.Conn, func(values.TenantId) uuid.UUID { return parseTenantUUID }), parseDB, parseTenantUUID, parseTenant
}

func TestTodo_WF_UI_003(parseT *testing.T) {
	parseStore, parseDB, _, parseTenant := buildDraftStore(parseT)
	parseAt := time.Date(2026, 9, 19, 16, 0, 0, 0, time.UTC)
	parseDraftID := uuid.New()
	parseCreated, parseErr := parseStore.Save(context.Background(), parseTenant, workflowdraftstore.SaveRequest{
		DraftID: parseDraftID, WorkflowID: "promotion", AuthorRef: "user:alice", SemanticVersion: "1.1.0", BaseVersionDigest: "sha256:base",
		Document: json.RawMessage(`{"nodes":[{"id":"start"}]}`), ExpiresAt: parseAt.Add(24 * time.Hour), At: parseAt,
	})
	if parseErr != nil {
		parseT.Fatalf("Save(create): %v", parseErr)
	}
	if parseCreated.Revision != 1 || parseCreated.SemanticVersion != "1.1.0" || parseCreated.BaseVersionDigest != "sha256:base" || parseCreated.AuthorRef != "user:alice" {
		parseT.Fatalf("created draft = %+v", parseCreated)
	}
	parseSaved, parseErr := parseStore.Save(context.Background(), parseTenant, workflowdraftstore.SaveRequest{
		DraftID: parseDraftID, WorkflowID: "promotion", AuthorRef: "user:alice", SemanticVersion: "1.1.0", BaseVersionDigest: "sha256:base",
		ExpectedRevision: 1, Document: json.RawMessage(`{"nodes":[{"id":"start"},{"id":"review"}]}`),
		ExpiresAt: parseAt.Add(48 * time.Hour), At: parseAt.Add(time.Minute),
	})
	if parseErr != nil || parseSaved.Revision != 2 || !json.Valid(parseSaved.Document) {
		parseT.Fatalf("Save(autosave) = %+v, %v", parseSaved, parseErr)
	}
	var parsePublishedCount int
	if parseErr = parseDB.Conn.QueryRow(context.Background(), `SELECT count(*) FROM workflow_compiled_version`).Scan(&parsePublishedCount); parseErr != nil {
		parseT.Fatalf("count compiled versions: %v", parseErr)
	}
	if parsePublishedCount != 0 {
		parseT.Fatalf("autosave wrote %d immutable compiled versions", parsePublishedCount)
	}
}

func TestTodo_WF_UI_003_Recovery(parseT *testing.T) {
	parseStore, _, _, parseTenant := buildDraftStore(parseT)
	parseAt := time.Date(2026, 9, 19, 17, 0, 0, 0, time.UTC)
	parseDraftID := uuid.New()
	parseSaved, parseErr := parseStore.Save(context.Background(), parseTenant, workflowdraftstore.SaveRequest{
		DraftID: parseDraftID, WorkflowID: "promotion", AuthorRef: "user:recovery", SemanticVersion: "1.1.0", Document: json.RawMessage(`{"selection":["review"],"viewport":{"zoom":1.2}}`),
		ExpiresAt: parseAt.Add(7 * 24 * time.Hour), At: parseAt,
	})
	if parseErr != nil {
		parseT.Fatalf("Save: %v", parseErr)
	}
	parseRecovered, parseErr := parseStore.Load(context.Background(), parseTenant, parseDraftID)
	if parseErr != nil {
		parseT.Fatalf("Load: %v", parseErr)
	}
	if parseRecovered.Revision != parseSaved.Revision || string(parseRecovered.Document) != string(parseSaved.Document) || !parseRecovered.UpdatedAt.Equal(parseSaved.UpdatedAt) {
		parseT.Fatalf("recovered = %+v, want %+v", parseRecovered, parseSaved)
	}
}

func TestTodo_WF_UI_003_Fault(parseT *testing.T) {
	parseStore, _, _, parseTenant := buildDraftStore(parseT)
	parseAt := time.Date(2026, 9, 19, 18, 0, 0, 0, time.UTC)
	parseDraftID := uuid.New()
	_, parseErr := parseStore.Save(context.Background(), parseTenant, workflowdraftstore.SaveRequest{
		DraftID: parseDraftID, WorkflowID: "promotion", AuthorRef: "user:alice", SemanticVersion: "1.1.0", Document: json.RawMessage(`{"nodes":[]}`),
		ExpiresAt: parseAt.Add(time.Hour), At: parseAt,
	})
	if parseErr != nil {
		parseT.Fatalf("Save: %v", parseErr)
	}
	_, parseErr = parseStore.Save(context.Background(), parseTenant, workflowdraftstore.SaveRequest{
		DraftID: parseDraftID, WorkflowID: "promotion", AuthorRef: "user:alice", SemanticVersion: "1.1.0", ExpectedRevision: 8,
		Document: json.RawMessage(`{"nodes":[]}`), ExpiresAt: parseAt.Add(time.Hour), At: parseAt.Add(time.Minute),
	})
	if !errors.Is(parseErr, workflowdraftstore.ErrConflict) {
		parseT.Fatalf("stale Save error = %v, want ErrConflict", parseErr)
	}
	parseRemoved, parseErr := parseStore.PurgeExpired(context.Background(), parseTenant, parseAt.Add(2*time.Hour))
	if parseErr != nil || parseRemoved != 1 {
		parseT.Fatalf("PurgeExpired = %d, %v", parseRemoved, parseErr)
	}
	if _, parseErr = parseStore.Load(context.Background(), parseTenant, parseDraftID); !errors.Is(parseErr, workflowdraftstore.ErrNotFound) {
		parseT.Fatalf("Load after purge = %v, want ErrNotFound", parseErr)
	}
	_, parseErr = parseStore.Save(context.Background(), parseTenant, workflowdraftstore.SaveRequest{
		DraftID: uuid.New(), WorkflowID: "promotion", AuthorRef: "user:alice", SemanticVersion: "1.1.0", Document: json.RawMessage(`[]`),
		ExpiresAt: parseAt.Add(time.Hour), At: parseAt,
	})
	if !errors.Is(parseErr, workflowdraftstore.ErrInvalid) {
		parseT.Fatalf("array document Save error = %v, want ErrInvalid", parseErr)
	}
	_, parseErr = parseStore.Save(context.Background(), parseTenant, workflowdraftstore.SaveRequest{
		DraftID: uuid.New(), WorkflowID: "promotion", AuthorRef: "user:alice", SemanticVersion: "v2",
		Document: json.RawMessage(`{"nodes":[]}`), ExpiresAt: parseAt.Add(time.Hour), At: parseAt,
	})
	if !errors.Is(parseErr, workflowdraftstore.ErrInvalid) {
		parseT.Fatalf("invalid semantic version Save error = %v, want ErrInvalid", parseErr)
	}
}

func TestTodo_WF_UI_003_TenantIsolation(parseT *testing.T) {
	parseDB := pgtest.New(parseT)
	parseTenantA, parseTenantB := uuid.New(), uuid.New()
	for parseIndex, parseTenantID := range []uuid.UUID{parseTenantA, parseTenantB} {
		parseKey := "wf-ui-003-isolation-" + parseTenantID.String()
		if _, parseErr := parseDB.Conn.Exec(context.Background(), `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, parseTenantID, parseKey, "Designer tenant"); parseErr != nil {
			parseT.Fatalf("insert tenant %d: %v", parseIndex, parseErr)
		}
	}
	parseTenantRef := values.TenantId("tenant:wf-ui-003:isolation")
	parseStoreA := workflowdraftstore.New(parseDB.Conn, func(values.TenantId) uuid.UUID { return parseTenantA })
	parseStoreB := workflowdraftstore.New(parseDB.Conn, func(values.TenantId) uuid.UUID { return parseTenantB })
	parseAt := time.Date(2026, 9, 19, 19, 0, 0, 0, time.UTC)
	parseDraftID := uuid.New()
	for _, parseFixture := range []struct {
		store  *workflowdraftstore.Store
		author string
		body   string
	}{{parseStoreA, "user:a", `{"tenant":"a"}`}, {parseStoreB, "user:b", `{"tenant":"b"}`}} {
		if _, parseErr := parseFixture.store.Save(context.Background(), parseTenantRef, workflowdraftstore.SaveRequest{
			DraftID: parseDraftID, WorkflowID: "promotion", AuthorRef: parseFixture.author, SemanticVersion: "1.1.0",
			Document: json.RawMessage(parseFixture.body), ExpiresAt: parseAt.Add(time.Hour), At: parseAt,
		}); parseErr != nil {
			parseT.Fatalf("Save tenant draft: %v", parseErr)
		}
	}
	parseLoadedA, parseErr := parseStoreA.Load(context.Background(), parseTenantRef, parseDraftID)
	var parseDocumentA map[string]string
	if parseDecodeErr := json.Unmarshal(parseLoadedA.Document, &parseDocumentA); parseDecodeErr != nil {
		parseT.Fatalf("decode tenant A document: %v", parseDecodeErr)
	}
	if parseErr != nil || parseDocumentA["tenant"] != "a" {
		parseT.Fatalf("tenant A Load = %+v, %v", parseLoadedA, parseErr)
	}
	parseLoadedB, parseErr := parseStoreB.Load(context.Background(), parseTenantRef, parseDraftID)
	var parseDocumentB map[string]string
	if parseDecodeErr := json.Unmarshal(parseLoadedB.Document, &parseDocumentB); parseDecodeErr != nil {
		parseT.Fatalf("decode tenant B document: %v", parseDecodeErr)
	}
	if parseErr != nil || parseDocumentB["tenant"] != "b" {
		parseT.Fatalf("tenant B Load = %+v, %v", parseLoadedB, parseErr)
	}
}

func TestTodo_WF_UI_010(parseT *testing.T) {
	parseStore, _, _, parseTenant := buildDraftStore(parseT)
	parseAt := time.Date(2026, 9, 19, 20, 0, 0, 0, time.UTC)
	parseDraftID := uuid.New()
	parseSave := func(parseExpected uint64, parseLabel, parseDocument string, parseOffset time.Duration) workflowdraftstore.Draft {
		parseT.Helper()
		parseSaved, parseErr := parseStore.Save(context.Background(), parseTenant, workflowdraftstore.SaveRequest{
			DraftID: parseDraftID, WorkflowID: "promotion", AuthorRef: "user:alice", SemanticVersion: "1.1.0",
			ExpectedRevision: parseExpected, CommandLabel: parseLabel, Document: json.RawMessage(parseDocument),
			ExpiresAt: parseAt.Add(24 * time.Hour), At: parseAt.Add(parseOffset),
		})
		if parseErr != nil {
			parseT.Fatalf("Save(%s): %v", parseLabel, parseErr)
		}
		return parseSaved
	}
	parseSave(0, "Create workflow", `{"nodes":[{"id":"start"}]}`, 0)
	parseSave(1, "Add review", `{"nodes":[{"id":"start"},{"id":"review"}]}`, time.Minute)
	parseThird := parseSave(2, "Connect review", `{"nodes":[{"id":"start"},{"id":"review"}],"edges":[{"from":"start","to":"review"}]}`, 2*time.Minute)
	parseHistory, parseErr := parseStore.LoadHistory(context.Background(), parseTenant, parseDraftID)
	if parseErr != nil || parseHistory.Position != 3 || parseHistory.Length != 3 || parseHistory.CurrentLabel != "Connect review" || !strings.Contains(string(parseHistory.Previous), `"review"`) {
		parseT.Fatalf("LoadHistory = %+v, %v", parseHistory, parseErr)
	}
	parseUndone, parseErr := parseStore.Navigate(context.Background(), parseTenant, workflowdraftstore.NavigateRequest{
		DraftID: parseDraftID, AuthorRef: "user:alice", ExpectedRevision: parseThird.Revision, Direction: "UNDO", At: parseAt.Add(3 * time.Minute),
	})
	if parseErr != nil || parseUndone.Revision != 4 || parseUndone.HistoryPosition != 2 || parseUndone.HistoryLength != 3 || strings.Contains(string(parseUndone.Document), `"edges"`) {
		parseT.Fatalf("Navigate(UNDO) = %+v, %v", parseUndone, parseErr)
	}
	parseRedone, parseErr := parseStore.Navigate(context.Background(), parseTenant, workflowdraftstore.NavigateRequest{
		DraftID: parseDraftID, AuthorRef: "user:alice", ExpectedRevision: parseUndone.Revision, Direction: "REDO", At: parseAt.Add(4 * time.Minute),
	})
	if parseErr != nil || parseRedone.Revision != 5 || parseRedone.HistoryPosition != 3 || !strings.Contains(string(parseRedone.Document), `"edges"`) {
		parseT.Fatalf("Navigate(REDO) = %+v, %v", parseRedone, parseErr)
	}
	parseUndone, parseErr = parseStore.Navigate(context.Background(), parseTenant, workflowdraftstore.NavigateRequest{
		DraftID: parseDraftID, AuthorRef: "user:alice", ExpectedRevision: parseRedone.Revision, Direction: "UNDO", At: parseAt.Add(5 * time.Minute),
	})
	if parseErr != nil {
		parseT.Fatalf("Navigate(second UNDO): %v", parseErr)
	}
	parseBranched := parseSave(parseUndone.Revision, "Rename review", `{"nodes":[{"id":"start"},{"id":"approval"}]}`, 6*time.Minute)
	parseHistory, parseErr = parseStore.LoadHistory(context.Background(), parseTenant, parseDraftID)
	if parseErr != nil || parseHistory.Position != 3 || parseHistory.Length != 3 || parseHistory.CurrentLabel != "Rename review" || strings.Contains(string(parseHistory.Current), `"edges"`) {
		parseT.Fatalf("branched history = %+v, %v", parseHistory, parseErr)
	}
	_, parseErr = parseStore.Navigate(context.Background(), parseTenant, workflowdraftstore.NavigateRequest{
		DraftID: parseDraftID, AuthorRef: "user:alice", ExpectedRevision: parseBranched.Revision, Direction: "REDO", At: parseAt.Add(7 * time.Minute),
	})
	if !errors.Is(parseErr, workflowdraftstore.ErrConflict) {
		parseT.Fatalf("redo after branch error = %v, want ErrConflict", parseErr)
	}
}
