package program

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testCaller = Caller{ID: "ops-1", Tenants: []string{"tenant-acme"}}

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	c := NewCatalog()
	if err := c.RecordConformance(SignedConformance{
		TodoID: "PROGRAM-CONF-001",
		Digest: "sha256:" + strings.Repeat("a", 64),
		Signer: "conformance-board",
	}); err != nil {
		t.Fatalf("RecordConformance: %v", err)
	}
	return c
}

func mustDefine(t *testing.T, c *Catalog) Definition {
	t.Helper()
	def, err := c.Define(testCaller, Definition{
		ID:       "bonus-fy26",
		Name:     "FY26 Annual Bonus",
		Type:     ProgramBonus,
		Owner:    "total-rewards",
		Scope:    []string{"org:acme"},
		Funding:  FundingEmployer,
		Outcomes: []string{"payout"},
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}
	return def
}

func mustRevision(t *testing.T, c *Catalog, programID string) Revision {
	t.Helper()
	rev, err := c.AppendRevision(testCaller, Revision{
		ProgramID:    programID,
		Version:      1,
		Tenant:       "tenant-acme",
		Org:          "org:acme",
		Jurisdiction: "US-CA",
		From:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:           time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}
	return rev
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return strings.TrimSpace(string(raw))
}
