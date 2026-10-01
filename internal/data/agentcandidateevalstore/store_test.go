package agentcandidateevalstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type candidateJournalDB struct {
	rows   map[string][]byte
	tenant string
	digest string
}

func (db *candidateJournalDB) Begin(context.Context) (dbport.Tx, error) { return db, nil }
func (db *candidateJournalDB) Exec(_ context.Context, query string, args ...any) (int64, error) {
	if strings.Contains(query, "INSERT INTO persona_candidate_case_journal") {
		key := args[0].(uuid.UUID).String() + ":" + args[1].(string)
		if db.rows[key] == nil {
			db.rows[key] = append([]byte(nil), args[9].([]byte)...)
			db.digest = args[8].(string)
			return 1, nil
		}
		return 0, nil
	}
	return 1, nil
}
func (db *candidateJournalDB) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, ErrEvidence
}
func (db *candidateJournalDB) QueryRow(_ context.Context, query string, args ...any) dbport.Row {
	if strings.Contains(query, "set_config") {
		return candidateJournalRow{value: ""}
	}
	if len(args) < 2 {
		return candidateJournalRow{err: ErrEvidence}
	}
	key := args[0].(uuid.UUID).String() + ":" + args[1].(string)
	row := db.rows[key]
	if row == nil {
		return candidateJournalRow{err: dbport.ErrNoRows}
	}
	if strings.Contains(query, "SELECT evidence_digest") {
		var record Record
		_ = json.Unmarshal(row, &record)
		return candidateJournalRow{value: record.EvidenceDigest}
	}
	return candidateJournalRow{value: row}
}
func (db *candidateJournalDB) Commit(context.Context) error   { return nil }
func (db *candidateJournalDB) Rollback(context.Context) error { return nil }

type candidateJournalRow struct {
	value any
	err   error
}

func (row candidateJournalRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	switch value := dest[0].(type) {
	case *string:
		*value = row.value.(string)
	case *[]byte:
		*value = append([]byte(nil), row.value.([]byte)...)
	default:
		return ErrEvidence
	}
	return nil
}

func TestTodo_AGENTP_021_CandidateJournal(t *testing.T) {
	db := &candidateJournalDB{rows: map[string][]byte{}}
	store, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	digestValue := "sha256:" + strings.Repeat("a", 64)
	record := Record{Target: agenteval.PersonaEvaluationTarget{TenantID: uuid.NewString(), SyntheticTenantID: uuid.NewString(), PersonaID: "policy", PersonaVersion: 1, ProfileDigest: digestValue, ModelDigest: digestValue, InvokerID: "invoker"},
		CaseDigest: digestValue, RequestDigest: digestValue, InvocationID: "invocation", TaskID: "refused-admission", AdmissionDigest: digestValue,
		Outcome: "REFUSED", RefusalCode: "OUT_OF_SCOPE", RefusalPointer: "/agents/policy", CompletedAt: time.Now().UTC()}
	if err := store.Append(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("same observation replay: %v", err)
	}
	got, err := store.Read(context.Background(), record.Target, record.InvocationID)
	if err != nil || got.EvidenceDigest == "" || got.RefusalCode != "OUT_OF_SCOPE" {
		t.Fatalf("retained evidence=%+v err=%v", got, err)
	}
	changed := record
	changed.RefusalCode = "AUTHORITY_DENIED"
	if err := store.Append(context.Background(), changed); !errors.Is(err, ErrEvidence) {
		t.Fatalf("changed observation accepted: %v", err)
	}
	foreign := record.Target
	foreign.SyntheticTenantID = uuid.NewString()
	if _, err := store.Read(context.Background(), foreign, record.InvocationID); !errors.Is(err, ErrEvidence) {
		t.Fatalf("foreign scope: %v", err)
	}
	wrong := record.Target
	wrong.ModelDigest = "sha256:" + strings.Repeat("b", 64)
	if _, err := store.Read(context.Background(), wrong, record.InvocationID); !errors.Is(err, ErrEvidence) {
		t.Fatalf("changed model: %v", err)
	}
	for key := range db.rows {
		db.rows[key] = []byte(`{"Outcome":"COMPLETED"}`)
	}
	if _, err := store.Read(context.Background(), record.Target, record.InvocationID); !errors.Is(err, ErrEvidence) {
		t.Fatalf("tampered row: %v", err)
	}
}

func TestTodo_AGENTP_021_CandidateJournalRejectsIncompleteProof(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrEvidence) {
		t.Fatal("nil journal database accepted")
	}
	store, _ := New(&candidateJournalDB{rows: map[string][]byte{}})
	if err := store.Append(context.Background(), Record{}); !errors.Is(err, ErrEvidence) {
		t.Fatal("incomplete record accepted")
	}
	if _, err := store.Read(context.Background(), agenteval.PersonaEvaluationTarget{}, "invocation"); !errors.Is(err, ErrEvidence) {
		t.Fatal("incomplete scope accepted")
	}
}

func TestTodo_AGENTP_021_CandidateJournalBindsLogicalTenantsToDistinctStorage(t *testing.T) {
	production, synthetic := uuid.New(), uuid.New()
	mapper := func(tenant string) uuid.UUID {
		switch tenant {
		case "demo":
			return production
		case "demo-eval":
			return synthetic
		default:
			return uuid.Nil
		}
	}
	store, err := NewWithTenantUUID(&candidateJournalDB{rows: map[string][]byte{}}, mapper)
	if err != nil {
		t.Fatal(err)
	}
	digestValue := "sha256:" + strings.Repeat("a", 64)
	record := Record{Target: agenteval.PersonaEvaluationTarget{TenantID: "demo", SyntheticTenantID: "demo-eval", PersonaID: "policy", PersonaVersion: 1, ProfileDigest: digestValue, ModelDigest: digestValue, InvokerID: "invoker"}, CaseDigest: digestValue, RequestDigest: digestValue, InvocationID: "i", TaskID: "a", AdmissionDigest: digestValue, Outcome: "REFUSED", RefusalCode: "AUTHORITY_DENIED", CompletedAt: time.Now().UTC()}
	if err := store.Append(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	got, err := store.Read(context.Background(), record.Target, "i")
	if err != nil || got.Target != record.Target {
		t.Fatalf("logical identity lost: %+v %v", got, err)
	}
	aliased, err := NewWithTenantUUID(&candidateJournalDB{rows: map[string][]byte{}}, func(string) uuid.UUID { return production })
	if err != nil {
		t.Fatal(err)
	}
	if err := aliased.Append(context.Background(), record); !errors.Is(err, ErrEvidence) {
		t.Fatalf("shared physical tenant accepted: %v", err)
	}
}
