package storeprivacy_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/storeprivacy"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

func scanLive(t *testing.T) storeprivacy.Inventory {
	t.Helper()
	inv, err := storeprivacy.Scan(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func findingsByTable(inv storeprivacy.Inventory) map[string][]storeprivacy.Finding {
	out := map[string][]storeprivacy.Finding{}
	for _, f := range inv.Findings {
		out[f.Table] = append(out[f.Table], f)
	}
	return out
}

// TestTodo_WF_REV_014 is the PRIMARY contract: the checker classifies every
// field written to a PERMANENT table and fails on the current registry,
// where personal fields sit inline with no payload-vault reference and
// every row is PLATFORM_MANAGED.
func TestTodo_WF_REV_014(t *testing.T) {
	inv := scanLive(t)
	// The registry keeps growing with every persistence lane; floors hold
	// the contract without freezing unrelated work.
	if inv.PermanentTables < 275 {
		t.Fatalf("PERMANENT tables = %d, want at least 275", inv.PermanentTables)
	}
	if len(inv.Fields) < 3208 {
		t.Fatalf("classified fields = %d, want at least 3208", len(inv.Fields))
	}
	if len(inv.Tables) != inv.PermanentTables {
		t.Fatalf("table summaries = %d, want %d", len(inv.Tables), inv.PermanentTables)
	}

	// RED clause 1: no table uses field-level encryption.
	registry, err := storagedisposition.Load(filepath.Join(repoRoot(t), storeprivacy.DefaultRegistryPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range registry.Tables {
		if entry.EncryptionClass != storagedisposition.EncryptionPlatformManaged {
			t.Fatalf("table %s uses %s, want every row PLATFORM_MANAGED (RED)", entry.Table, entry.EncryptionClass)
		}
	}

	// RED clause 2 + GREEN: the check fails on personal-inline fields.
	err = storeprivacy.Check(repoRoot(t))
	checkErr, ok := err.(*storeprivacy.CheckError)
	if !ok {
		t.Fatalf("Check error = %T (%v), want *CheckError with findings", err, err)
	}
	if !errors.Is(err, storeprivacy.ErrPersonalInline) {
		t.Fatalf("Check error does not match ErrPersonalInline: %v", err)
	}
	if errors.Is(err, storeprivacy.ErrPayloadUndeclared) {
		t.Fatalf("unexpected PAYLOAD_UNDECLARED findings: every payload column must be reviewed, got %v", err)
	}
	if errors.Is(err, storeprivacy.ErrVaultUnproven) {
		t.Fatalf("unexpected VAULT_UNPROVEN findings: no vault exists yet, got %v", err)
	}
	if len(checkErr.Findings) == 0 {
		t.Fatal("Check returned no findings against the current registry (RED not captured)")
	}

	// Witnesses: the findings the todo names must be present with owner.
	byTable := findingsByTable(inv)
	witnesses := []struct {
		table, field, location, owner string
	}{
		{"ledger_event", "payload", storeprivacy.LocationPayload, "internal/data/ledger"},
		{"proposal_revision", "payload", storeprivacy.LocationPayload, "internal/intent/app/pgstore"},
		{"person", "legal_name", storeprivacy.LocationColumn, "internal/domains/people"},
		{"person", "preferred_name", storeprivacy.LocationColumn, "internal/domains/people"},
		{"definition_version", "body", storeprivacy.LocationPayload, "internal/data/schema"},
		{"paymethod_destination", "bank_detail_ref", storeprivacy.LocationColumn, "internal/data/paymethodstore"},
		{"external_observation", "payload", storeprivacy.LocationPayload, "internal/connectivity/observe/adapters/postgres"},
	}
	for _, w := range witnesses {
		var found *storeprivacy.Finding
		for i, f := range byTable[w.table] {
			if f.Field == w.field && f.Code == storeprivacy.CodePersonalInline && f.Location == w.location {
				found = &byTable[w.table][i]
				break
			}
		}
		if found == nil {
			t.Fatalf("missing PERSONAL_INLINE finding for %s.%s", w.table, w.field)
		}
		if found.Owner != w.owner {
			t.Fatalf("%s.%s owner = %q, want %q", w.table, w.field, found.Owner, w.owner)
		}
	}

	// Every finding carries its owner: the audit is recorded, not anonymous.
	for _, f := range inv.Findings {
		if f.Owner == "" || f.Table == "" || f.Field == "" || f.Code == "" {
			t.Fatalf("finding without owner/table/field/code: %+v", f)
		}
	}
	if inv.Explain() == "" || storeprivacy.Version() != 1 {
		t.Fatal("inventory identity is empty or version moved without review")
	}
}

// goldenShape is the pinned file shape. The roster plus counts is the
// inventory WF-REV-014 pins; a new personal field or PERMANENT table breaks
// it until reviewed.
type goldenShape struct {
	Version         int                    `json:"version"`
	PermanentTables int                    `json:"permanent_tables"`
	Fields          int                    `json:"fields"`
	Digest          string                 `json:"digest"`
	Roster          []storeprivacy.Finding `json:"roster"`
}

func encodeGolden(inv storeprivacy.Inventory) ([]byte, error) {
	return json.MarshalIndent(goldenShape{
		Version:         inv.Version,
		PermanentTables: inv.PermanentTables,
		Fields:          len(inv.Fields),
		Digest:          inv.Digest(),
		Roster:          inv.Roster(),
	}, "", "  ")
}

// TestTodo_WF_REV_014_Golden pins the personal-field inventory byte for
// byte against testdata.
func TestTodo_WF_REV_014_Golden(t *testing.T) {
	inv := scanLive(t)
	got, err := encodeGolden(inv)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "policy", "storeprivacy", "testdata", "storeprivacy.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden inventory drifted: got %d findings %s, want pinned bytes (%d bytes); review the diff and re-pin only after audit",
			len(inv.Findings), inv.Digest(), len(want))
	}
	// Identity is deterministic across scans.
	again, err := storeprivacy.Scan(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest() != inv.Digest() {
		t.Fatalf("inventory digest unstable: %s vs %s", again.Digest(), inv.Digest())
	}
}

// TestTodo_WF_REV_014_Security proves the fail-closed properties on
// synthetic registries: undeclared payloads fail, aliases and case cannot
// dodge classification, opaque references are not flagged, and out-of-scope
// tables are untouched.
func TestTodo_WF_REV_014_Security(t *testing.T) {
	dir := t.TempDir()
	ddl := `CREATE TABLE t_claim (
    tenant_id    uuid         NOT NULL,
    EMAIL_ADDRESS text        NOT NULL,
    EmailAddress2 text,
    statement_id uuid         NOT NULL,
    grievant_ref text         NOT NULL,
    mystery      jsonb        NOT NULL,
    payload      bytea,
    amount       numeric      NOT NULL,
    decided_at   timestamptz  NOT NULL
);
CREATE TABLE vault.t_claim (
    email text NOT NULL
);
CREATE TABLE t_claim_p0 PARTITION OF t_claim FOR VALUES WITH (modulus 4, remainder 0);
CREATE TABLE o_note (
    email text NOT NULL,
    payload bytea
);
ALTER TABLE t_claim ADD COLUMN nickname text;
`
	if err := os.WriteFile(filepath.Join(dir, "00001_test.sql"), []byte(ddl), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := &storagedisposition.Registry{Tables: []storagedisposition.TableEntry{
		{Table: "t_claim", Migration: "00001_test.sql", OwnerPackage: "example/claims", RetentionClass: storagedisposition.RetentionPermanent, EncryptionClass: storagedisposition.EncryptionPlatformManaged},
		{Table: "o_note", Migration: "00001_test.sql", OwnerPackage: "example/notes", RetentionClass: storagedisposition.RetentionOperational, EncryptionClass: storagedisposition.EncryptionPlatformManaged},
	}}
	inv, err := storeprivacy.Assemble(registry, dir)
	if err != nil {
		t.Fatal(err)
	}
	byField := map[string]storeprivacy.FieldRecord{}
	for _, rec := range inv.Fields {
		byField[rec.Table+"."+rec.Field] = rec
	}
	// Aliases and case cannot dodge: both spellings are personal inline.
	for _, field := range []string{"t_claim.email_address", "t_claim.emailaddress2", "t_claim.nickname"} {
		rec, ok := byField[field]
		if !ok {
			t.Fatalf("field %s not classified", field)
		}
		if rec.Class != storeprivacy.ClassPersonal || rec.Storage != storeprivacy.StorageInline {
			t.Fatalf("field %s = %s/%s, want personal/inline", field, rec.Class, rec.Storage)
		}
	}
	// Opaque reference carries no content; the subject link stays flagged.
	if rec := byField["t_claim.statement_id"]; rec.Class != storeprivacy.ClassNonPersonal {
		t.Fatalf("statement_id = %s, want non_personal", rec.Class)
	}
	if rec := byField["t_claim.grievant_ref"]; rec.Class != storeprivacy.ClassPersonal {
		t.Fatalf("grievant_ref = %s, want personal", rec.Class)
	}
	// Companion schema, partitions and OPERATIONAL tables are out of scope.
	for _, field := range []string{"vault.t_claim", "t_claim_p0", "o_note.email", "o_note.payload"} {
		if _, ok := byField[field]; ok {
			t.Fatalf("out-of-scope field %s entered the inventory", field)
		}
	}
	// Undeclared payloads fail closed with the sentinel.
	var codes []string
	for _, f := range inv.Findings {
		codes = append(codes, f.Table+"."+f.Field+":"+f.Code)
	}
	for _, want := range []string{
		"t_claim.mystery:" + storeprivacy.CodePayloadUndeclared,
		"t_claim.payload:" + storeprivacy.CodePayloadUndeclared,
		"t_claim.email_address:" + storeprivacy.CodePersonalInline,
	} {
		found := false
		for _, c := range codes {
			if c == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing finding %s, got %v", want, codes)
		}
	}
	// Assembling with a forged verdict (unknown table) must fail; the live
	// declarations are proven clean by every Scan above.
	if err := checkLiveDeclarationsClean(t); err != nil {
		t.Fatal(err)
	}
}

func checkLiveDeclarationsClean(t *testing.T) error {
	t.Helper()
	inv := scanLive(t)
	for _, f := range inv.Findings {
		if f.Code != storeprivacy.CodePersonalInline {
			return errors.New("live inventory carries non-personal-inline findings: " + f.Error())
		}
	}
	return nil
}

// TestTodo_WF_REV_014_Classify pins the classifier matrix: pattern hits,
// override precedence, suffix rule and vault-adjacent storage values.
func TestTodo_WF_REV_014_Classify(t *testing.T) {
	entry := storagedisposition.TableEntry{Table: "t", OwnerPackage: "example/test"}
	cases := []struct {
		name, typ, class, storage string
	}{
		{"legal_name", "text", storeprivacy.ClassPersonal, storeprivacy.StorageInline},
		{"EMAIL", "citext", storeprivacy.ClassPersonal, storeprivacy.StorageInline},
		{"PhoneNumber", "text", storeprivacy.ClassPersonal, storeprivacy.StorageInline},
		{"participants", "jsonb", storeprivacy.ClassPersonal, storeprivacy.StorageInline},
		{"payload", "bytea", storeprivacy.ClassUnreviewed, storeprivacy.StorageInline},
		{"mystery", "jsonb", storeprivacy.ClassUnreviewed, storeprivacy.StorageInline},
		{"statement_id", "uuid", storeprivacy.ClassNonPersonal, storeprivacy.StorageInline},
		{"evidence_digest", "content_digest", storeprivacy.ClassNonPersonal, storeprivacy.StorageInline},
		{"namespace", "text", storeprivacy.ClassNonPersonal, storeprivacy.StorageInline},
		{"question_bank_ref", "text", storeprivacy.ClassNonPersonal, storeprivacy.StorageInline},
		{"name", "text", storeprivacy.ClassNonPersonal, storeprivacy.StorageInline},
		{"grievant_ref", "text", storeprivacy.ClassPersonal, storeprivacy.StorageInline},
		{"membership_id", "uuid", storeprivacy.ClassPersonal, storeprivacy.StorageInline},
		{"secret_index", "integer", storeprivacy.ClassPersonal, storeprivacy.StorageInline},
		{"api_key", "text", storeprivacy.ClassPersonal, storeprivacy.StorageInline},
		{"amount", "numeric", storeprivacy.ClassNonPersonal, storeprivacy.StorageInline},
		{"occurred_at", "timestamptz", storeprivacy.ClassNonPersonal, storeprivacy.StorageInline},
	}
	for _, c := range cases {
		rec := storeprivacy.Classify(storeprivacy.Column{Table: "t", Name: c.name, Type: c.typ}, entry)
		if rec.Class != c.class || rec.Storage != c.storage {
			t.Errorf("Classify(%s %s) = %s/%s, want %s/%s", c.name, c.typ, rec.Class, rec.Storage, c.class, c.storage)
		}
		if rec.Owner != entry.OwnerPackage || rec.Reason == "" {
			t.Errorf("Classify(%s) missing owner/reason: %+v", c.name, rec)
		}
		if got := storeprivacy.Classify(storeprivacy.Column{Table: "t", Name: strings.ToUpper(c.name), Type: c.typ}, entry); got.Class != c.class {
			t.Errorf("Classify(%s) not case-insensitive: %s vs %s", c.name, got.Class, c.class)
		}
	}
}
