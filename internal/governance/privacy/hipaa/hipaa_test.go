package hipaa

// REV-099-02: an expired BAA refuses flow approval; a current one admits
// with the program version pinned.
//
// RED: no HIPAA business-associate program existed anywhere in Go code.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rev09902Program() HIPAABusinessAssociateProgram {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return HIPAABusinessAssociateProgram{
		Version:          ProgramVersion,
		Applicability:    ApplicabilityAssumed,
		MinimumNecessary: MinimumNecessaryScope{Version: "medical-benefits-v1", Fields: []string{"leave_status", "restriction_code"}},
		Agreements: []BusinessAssociateAgreement{
			{Subprocessor: "claims-clearinghouse", Service: "claims processing", Reference: "BAA-2026-0007", EffectiveFrom: from, EffectiveTo: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), CopyReference: "contracts/baa-claims.pdf", CopyDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000001", ReviewedAt: from, MedicalFields: []string{"leave_status", "restriction_code"}},
			{Subprocessor: "records-vault", Service: "encrypted record storage", Reference: "BAA-2026-0008", EffectiveFrom: from, CopyReference: "contracts/baa-vault.pdf", CopyDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000002", ReviewedAt: from, MedicalFields: []string{"leave_status"}},
		},
	}
}

func TestTodo_REV_099_02(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	program := rev09902Program()
	inventory := fixtureCopyInventory(at)
	approval, err := approveMedicalFlow(program, inventory, "claims-clearinghouse", []string{"leave_status"}, at, 24*time.Hour)
	if err != nil {
		t.Fatalf("current BAA approval: %v", err)
	}
	if approval.Reference != "BAA-2026-0007" || approval.ProgramVersion != ProgramVersion {
		t.Fatalf("approval = %+v, want BAA-2026-0007 under program version %d", approval, ProgramVersion)
	}
	lapsed := time.Date(2027, 2, 1, 12, 0, 0, 0, time.UTC)
	if _, err := approveMedicalFlow(program, inventory, "claims-clearinghouse", []string{"leave_status"}, lapsed, 365*24*time.Hour); !errors.Is(err, ErrBAAExpired) {
		t.Fatalf("lapsed BAA approval = %v, want ErrBAAExpired", err)
	}
	if _, err := approveMedicalFlow(program, inventory, "unknown-vendor", []string{"leave_status"}, at, 24*time.Hour); !errors.Is(err, ErrBAARequired) {
		t.Fatalf("unnamed subprocessor approval = %v, want ErrBAARequired", err)
	}
	bad := rev09902Program()
	bad.Agreements[0].Reference = ""
	if _, err := approveMedicalFlow(bad, inventory, "claims-clearinghouse", []string{"leave_status"}, at, 24*time.Hour); !errors.Is(err, ErrProgramInvalid) {
		t.Fatalf("referenceless program approval = %v, want ErrProgramInvalid", err)
	}
}

func TestTodo_REV_099_02_Golden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "hipaa_program.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got, want := CanonicalProgram(rev09902Program()), strings.TrimSpace(string(raw)); got != want {
		t.Fatalf("program drifted:\n got: %q\nwant: %q", got, want)
	}
	breachRaw, err := os.ReadFile(filepath.Join("testdata", "hipaa_breach.golden"))
	if err != nil {
		t.Fatalf("read HIPAA breach golden: %v", err)
	}
	if got, want := StandardBreachMatrixExtension("breach-matrix-v3").Canonical(), strings.TrimSpace(string(breachRaw)); got != want {
		t.Fatalf("HIPAA breach matrix drifted:\n got: %q\nwant: %q", got, want)
	}
}
