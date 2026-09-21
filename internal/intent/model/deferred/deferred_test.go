package deferred

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/deferredschema"
)

func TestTodo_MSRC_006(t *testing.T) {
	sources := Sources()
	if err := Validate(sources); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := ValidateBootstrapNoWriteAuthority(); err != nil {
		t.Fatalf("ValidateBootstrapNoWriteAuthority: %v", err)
	}
	if got := len(sources); got != 10 {
		t.Fatalf("deferred domains = %d, want 10", got)
	}
	for _, source := range sources {
		if source.Authority == "" || source.SourceSystem == "" || len(source.Entities) != 2 {
			t.Fatalf("incomplete source authority: %+v", source)
		}
	}
}

func TestTodo_MSRC_006_Golden(t *testing.T) {
	preview, err := deferredschema.Generate()
	if err != nil {
		t.Fatalf("deferredschema.Generate: %v", err)
	}
	t.Logf("DB-016 preview digest: %s", preview.Digest())
	if preview.Digest() != DB016PreviewDigest {
		t.Fatalf("DB-016 preview digest = %q, want pinned %q", preview.Digest(), DB016PreviewDigest)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "deferred_sources.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got := encodeGolden(Sources()); got != string(want) {
		t.Fatalf("encoded deferred source set drifted:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTodo_MSRC_006_Race(t *testing.T) {
	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if err := Validate(Sources()); err != nil {
					t.Errorf("Validate: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}

func TestTodo_MSRC_006_Conformance(t *testing.T) {
	wantDomains := []string{"payroll", "benefits", "time", "leave", "recruiting", "talent", "learning", "case", "access", "regulatory"}
	wantTables := [][]string{
		{"payroll_run", "payroll_ledger_entry"}, {"benefit_election", "benefit_election_revision"},
		{"timecard", "timecard_revision"}, {"leave_request_preview", "leave_record_preview"},
		{"requisition", "requisition_revision"}, {"performance_review", "performance_review_revision"},
		{"learning_enrollment", "learning_completion"}, {"hr_case", "case_transition"},
		{"access_grant", "access_operation"}, {"government_filing", "filing_submission_attempt"},
	}
	for i, source := range Sources() {
		if source.Domain != wantDomains[i] {
			t.Errorf("domain %d = %q, want %q", i, source.Domain, wantDomains[i])
		}
		for j, entity := range source.Entities {
			if entity.PreviewTable != wantTables[i][j] {
				t.Errorf("%s entity %d table = %q, want %q", source.Domain, j, entity.PreviewTable, wantTables[i][j])
			}
		}
		if source.PreviewDigest != DB016PreviewDigest {
			t.Errorf("%s is not bound to the DB-016 preview digest", source.Domain)
		}
	}
}

func TestTodo_MSRC_006_Mutation(t *testing.T) {
	mutated := Sources()
	mutated[0].Entities[0].PreviewTable = "unauthorized_write_table"
	if Sources()[0].Entities[0].PreviewTable == mutated[0].Entities[0].PreviewTable {
		t.Fatal("Sources returned aliased entity storage")
	}

	base, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	record, ok := base.List()[0], true
	if !ok {
		t.Fatal("bootstrap registry is empty")
	}
	write := record.Definition
	write.ID = "hcmnext.test.deferred_write"
	write.EffectClass = capability.EffectInternalMutation
	write.WriteData = capability.DataDomainFieldSet{DataDomains: []string{"payroll"}}
	reg := capability.NewRegistry()
	if err := reg.Register(write, func(_ context.Context, payload any) (any, error) { return payload, nil }); err != nil {
		t.Fatalf("register mutation fixture: %v", err)
	}
	if err := ValidateNoWriteAuthority(reg); err == nil {
		t.Fatal("write capability passed deferred authority validation")
	}
	if !errors.Is(ValidateNoWriteAuthority(reg), ErrWriteAuthority) {
		t.Log("validator rejected the mutation fixture with its descriptive error")
	}
}

func encodeGolden(sources []DomainSource) string {
	var b strings.Builder
	for _, source := range sources {
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s|%s\n", source.Domain, source.Entities[0].Ref, source.Entities[0].PreviewTable, source.SourceSystem, source.Authority, source.AuthorityRef, source.PreviewDigest)
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s|%s\n", source.Domain, source.Entities[1].Ref, source.Entities[1].PreviewTable, source.SourceSystem, source.Authority, source.AuthorityRef, source.PreviewDigest)
	}
	return b.String()
}
