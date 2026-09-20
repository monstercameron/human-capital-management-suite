package storeprivacy

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

func TestTodo_WF_REV_014_Scan(t *testing.T) {
	dir := t.TempDir()
	ddl := `-- leading comment (with parens)
CREATE TABLE IF NOT EXISTS t (
    id          uuid                    NOT NULL,
    payload     bytea,
    note        text                    NOT NULL DEFAULT '',
    amount      numeric(10, 2)          NOT NULL,
    big         character varying(96)   NOT NULL,
    at          timestamp with time zone NOT NULL,
    precise     double precision        NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT t_note_present CHECK (note <> ''),
    CONSTRAINT t_frag CHECK (
        amount >= 0
        OR note <> ''
        AND note IS NOT NULL
    ),
    FOREIGN KEY (id) REFERENCES other (id)
);
CREATE TABLE companion.t (
    email text NOT NULL
);
CREATE TABLE t_p0 PARTITION OF t FOR VALUES WITH (modulus 2, remainder 0);
ALTER TABLE t ADD COLUMN added_email text;
ALTER TABLE missing.t ADD COLUMN x text;
`
	if err := os.WriteFile(filepath.Join(dir, "00001_a.sql"), []byte(ddl), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not sql"), 0o600); err != nil {
		t.Fatal(err)
	}
	cols, err := scanColumns(dir, map[string]bool{"t": true})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Column{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	// Default-schema table parsed; companion schema, partitions and the
	// missing table contribute nothing.
	for _, want := range []string{"id", "payload", "note", "amount", "big", "at", "precise", "added_email"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("column %s not scanned", want)
		}
	}
	if len(byName) != 8 {
		t.Errorf("scanned %d columns, want 8: %v", len(byName), byName)
	}
	if byName["big"].Type != "varchar" || byName["at"].Type != "timestamptz" || byName["precise"].Type != "float8" {
		t.Errorf("multi-word types not normalized: %v", byName)
	}
	if _, err := scanColumns(filepath.Join(dir, "absent"), map[string]bool{}); err == nil {
		t.Error("scanColumns with a missing dir must fail")
	}
	if _, _, ok := parseColumnLine("    CONSTRAINT x CHECK (a <> '')"); ok {
		t.Error("constraint line parsed as a column")
	}
	if _, _, ok := parseColumnLine(""); ok {
		t.Error("empty line parsed as a column")
	}
	if _, _, ok := parseColumnLine(");"); ok {
		t.Error("table close parsed as a column")
	}
}

func TestTodo_WF_REV_014_Declarations(t *testing.T) {
	saved := payloadVerdicts
	payloadVerdicts = map[string]struct {
		class  string
		reason string
	}{"t.payload": {ClassPersonal, "test verdict"}}
	defer func() { payloadVerdicts = saved }()
	ok := []Column{{Table: "t", Name: "payload", Type: "bytea"}}
	scope := map[string]storagedisposition.TableEntry{"t": {Table: "t"}}
	if err := checkDeclarations(scope, ok); err != nil {
		t.Fatal(err)
	}
	payloadVerdicts["t.ghost"] = struct {
		class  string
		reason string
	}{ClassPersonal, "forged"}
	if err := checkDeclarations(scope, ok); err == nil {
		t.Fatal("forged payload verdict did not fail the check")
	} else if !strings.Contains(err.Error(), "t.ghost") {
		t.Fatalf("forged verdict error names nothing useful: %v", err)
	}
}

// The live declaration set must resolve fully: every verdict and vault
// table is a PERMANENT registry row, so a typo cannot silently disarm the
// check. This runs against the real registry, not a synthetic one.
func TestTodo_WF_REV_014_DeclarationsLive(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
	registry, err := storagedisposition.Load(filepath.Join(root, "definitions", "storage", "storage-disposition.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	permanent := map[string]bool{}
	for _, entry := range registry.Tables {
		if entry.RetentionClass == storagedisposition.RetentionPermanent {
			permanent[strings.ToLower(entry.Table)] = true
		}
	}
	for key := range payloadVerdicts {
		if !permanent[key[:strings.Index(key, ".")]] {
			t.Errorf("verdict %s names a table outside the PERMANENT registry", key)
		}
	}
	for table := range vaultRefs {
		if !permanent[strings.ToLower(table)] {
			t.Errorf("vault reference %s names a table outside the PERMANENT registry", table)
		}
	}
}

func TestTodo_WF_REV_014_Vault(t *testing.T) {
	savedVerdicts := payloadVerdicts
	payloadVerdicts = map[string]struct {
		class  string
		reason string
	}{}
	defer func() { payloadVerdicts = savedVerdicts }()
	dir := t.TempDir()
	ddl := `CREATE TABLE v_claim (
    ssn         text  NOT NULL,
    case_note   text  NOT NULL
);
`
	if err := os.WriteFile(filepath.Join(dir, "00001_v.sql"), []byte(ddl), 0o600); err != nil {
		t.Fatal(err)
	}
	vaultRefs["v_claim"] = map[string]bool{"ssn": true}
	defer delete(vaultRefs, "v_claim")

	managed := &storagedisposition.Registry{Tables: []storagedisposition.TableEntry{
		{Table: "v_claim", Migration: "00001_v.sql", OwnerPackage: "example/v", RetentionClass: storagedisposition.RetentionPermanent, EncryptionClass: storagedisposition.EncryptionPlatformManaged},
	}}
	inv, err := Assemble(managed, dir)
	if err != nil {
		t.Fatal(err)
	}
	var unproven, inline bool
	for _, f := range inv.Findings {
		switch {
		case f.Table == "v_claim" && f.Field == "ssn" && f.Code == CodeVaultUnproven:
			unproven = true
		case f.Table == "v_claim" && f.Field == "case_note" && f.Code == CodePersonalInline:
			inline = true
		}
	}
	if !unproven {
		t.Fatalf("vault claim without FIELD_LEVEL must be VAULT_UNPROVEN, got %+v", inv.Findings)
	}
	if !inline {
		t.Fatalf("plain personal column must stay PERSONAL_INLINE, got %+v", inv.Findings)
	}
	if err := Check(dir); err == nil {
		t.Fatal("Check on a raw dir (no registry) must fail")
	}

	// The same vault claim with FIELD_LEVEL encryption is honored: the
	// vaulted field drops out of the findings while the inline note stays.
	fielded := &storagedisposition.Registry{Tables: []storagedisposition.TableEntry{
		{Table: "v_claim", Migration: "00001_v.sql", OwnerPackage: "example/v", RetentionClass: storagedisposition.RetentionPermanent, EncryptionClass: storagedisposition.EncryptionFieldLevel},
	}}
	inv, err = Assemble(fielded, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range inv.Findings {
		if f.Field == "ssn" {
			t.Fatalf("proven vault reference still flagged: %+v", f)
		}
	}
	checkErr := &CheckError{Findings: inv.Findings}
	if !errors.Is(checkErr, ErrPersonalInline) {
		t.Fatalf("remaining inline finding must match ErrPersonalInline: %+v", inv.Findings)
	}
	if errors.Is(checkErr, ErrVaultUnproven) || errors.Is(checkErr, ErrPayloadUndeclared) {
		t.Fatalf("wrong sentinels matched: %+v", inv.Findings)
	}
	empty := &CheckError{}
	if empty.Error() != "" || errors.Is(empty, ErrPersonalInline) {
		t.Fatal("empty CheckError must be quiet and match nothing")
	}
	unknown := &CheckError{Findings: []Finding{{Table: "t", Field: "f", Code: "NOPE"}}}
	if errors.Is(unknown, ErrPersonalInline) {
		t.Fatal("unknown finding code must match no sentinel")
	}
	if s := unknown.Error(); !strings.Contains(s, "NOPE") {
		t.Fatalf("CheckError text drops the finding: %q", s)
	}
}
