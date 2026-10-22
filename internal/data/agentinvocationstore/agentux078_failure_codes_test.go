package agentinvocationstore

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// constraintCodes is the set of quoted codes in a check constraint's text, as
// the database prints it back.
func constraintCodes(t *testing.T, definition string) []string {
	t.Helper()
	var codes []string
	for _, match := range regexp.MustCompile(`'([A-Z_]+)'`).FindAllStringSubmatch(definition, -1) {
		codes = append(codes, match[1])
	}
	sort.Strings(codes)
	return codes
}

func sortedCopy(codes ...[]string) []string {
	var out []string
	for _, list := range codes {
		out = append(out, list...)
	}
	sort.Strings(out)
	return out
}

// TestTodo_AGENTUX_078_FailureCodesMatchConstraint compares the failure codes
// this package accepts with the text of the table's check constraints after
// every agent-store migration has run. A code written by the run path but
// missing from the constraint is refused by the database and the card then says
// nothing (SERVER_HAS_NO_MODEL did exactly that); the other way round, a
// constraint wider than the Go list means a code nothing can write.
func TestTodo_AGENTUX_078_FailureCodesMatchConstraint(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	var codeCheck, retryCheck string
	if err := db.SQL.QueryRowContext(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'persona_invocation_failure'::regclass AND conname = 'persona_invocation_failure_failure_code_check'`).Scan(&codeCheck); err != nil {
		t.Fatalf("read the failure-code constraint: %v", err)
	}
	if err := db.SQL.QueryRowContext(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'persona_invocation_failure'::regclass AND contype = 'c' AND pg_get_constraintdef(oid) ILIKE '%retryable%'`).Scan(&retryCheck); err != nil {
		t.Fatalf("read the retryable constraint: %v", err)
	}

	all := sortedCopy(retryableFailureCodes, finalFailureCodes)
	if got := constraintCodes(t, codeCheck); strings.Join(got, ",") != strings.Join(all, ",") {
		t.Errorf("the failure-code constraint lists\n  %v\nthe Go list is\n  %v\n(constraint text: %s)", got, all, codeCheck)
	}
	retryable := sortedCopy(retryableFailureCodes)
	if got := constraintCodes(t, retryCheck); strings.Join(got, ",") != strings.Join(retryable, ",") {
		t.Errorf("the retryable constraint lists\n  %v\nthe Go list is\n  %v\n(constraint text: %s)", got, retryable, retryCheck)
	}

	// The no-model outcome is recordable and is never offered a retry.
	if !validFailureCode(NoModelCode, false) || validFailureCode(NoModelCode, true) {
		t.Errorf("%s must be accepted as final and refused as retryable", NoModelCode)
	}
	tenant := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenant); err != nil {
		t.Fatal(err)
	}
	for _, code := range all {
		_, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_invocation_failure (tenant_id,invoker_id,post_id,conversation_id,thread_id,failure_code,retryable,retry_at) VALUES ($1,'alice',$2,'room',$2,$3,false,$4)`, tenant, "post-"+code, code, retryAtFor(code))
		if err != nil {
			t.Errorf("the table refuses %s: %v", code, err)
		}
	}
}

// retryAtFor is the retry time the table requires for a code: only the
// mention-limit outcome has one.
func retryAtFor(code string) any {
	if code == MentionLimitCode {
		return "2026-10-02T10:00:00Z"
	}
	return nil
}
