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
		Version: ProgramVersion,
		Agreements: []BusinessAssociateAgreement{
			{Subprocessor: "claims-clearinghouse", Service: "claims processing", Reference: "BAA-2026-0007", EffectiveFrom: from, EffectiveTo: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Subprocessor: "records-vault", Service: "encrypted record storage", Reference: "BAA-2026-0008", EffectiveFrom: from},
		},
	}
}

func TestTodo_REV_099_02(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	approval, err := ApproveFlow(rev09902Program(), "claims-clearinghouse", at)
	if err != nil {
		t.Fatalf("current BAA approval: %v", err)
	}
	if approval.Reference != "BAA-2026-0007" || approval.ProgramVersion != ProgramVersion {
		t.Fatalf("approval = %+v, want BAA-2026-0007 under program version %d", approval, ProgramVersion)
	}
	lapsed := time.Date(2027, 2, 1, 12, 0, 0, 0, time.UTC)
	if _, err := ApproveFlow(rev09902Program(), "claims-clearinghouse", lapsed); !errors.Is(err, ErrBAAExpired) {
		t.Fatalf("lapsed BAA approval = %v, want ErrBAAExpired", err)
	}
	if _, err := ApproveFlow(rev09902Program(), "unknown-vendor", at); !errors.Is(err, ErrBAARequired) {
		t.Fatalf("unnamed subprocessor approval = %v, want ErrBAARequired", err)
	}
	bad := rev09902Program()
	bad.Agreements[0].Reference = ""
	if _, err := ApproveFlow(bad, "claims-clearinghouse", at); !errors.Is(err, ErrProgramInvalid) {
		t.Fatalf("referenceless program approval = %v, want ErrProgramInvalid", err)
	}
}

func TestTodo_REV_099_02_Conformance(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "hipaa_program.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got, want := CanonicalProgram(rev09902Program()), strings.TrimSpace(string(raw)); got != want {
		t.Fatalf("program drifted:\n got: %q\nwant: %q", got, want)
	}
}
