package agentpersonastore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_007_Integration_RolloutSchemaReportsMissingExactRefs(t *testing.T) {
	f := newFixture(t, values.TenantId("tenant-rollout"))
	store := f.store(t, values.TenantId("tenant-rollout"))

	report, err := store.CheckRolloutSchema(context.Background())
	if err != nil {
		t.Fatalf("check rollout schema: %v", err)
	}
	want := []string{}
	if !reflect.DeepEqual(report.MissingColumns, want) {
		t.Fatalf("missing columns = %v, want %v", report.MissingColumns, want)
	}
	if !report.Ready() {
		t.Fatalf("migration baseline reports missing ref columns: %v", report.MissingColumns)
	}
	if !strings.Contains(RequiredRolloutMigration, "ADD COLUMN rollout_preview_ref") || !strings.Contains(RequiredRolloutMigration, "ADD COLUMN rollout_approval_ref") {
		t.Fatalf("required migration does not name both missing columns: %s", RequiredRolloutMigration)
	}
}

func TestTodo_AGENTP_007_RolloutMutationUsesSchemaBaseline(t *testing.T) {
	f := newFixture(t, values.TenantId("tenant-rollout"))
	store := f.store(t, values.TenantId("tenant-rollout"))
	mutation := RolloutMutation{InstallationID: "install-1", ExpectedRevision: 1, PreviewRef: "preview-1", ApprovalRef: "approval-1", Reason: "reviewed"}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "pause", call: func() error { _, err := store.PauseInstallation(context.Background(), mutation); return err }},
		{name: "remove", call: func() error { _, err := store.RemoveInstallation(context.Background(), mutation); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrConflict) {
				t.Fatalf("error = %v, want ErrConflict after schema baseline", err)
			}
		})
	}
}

func TestTodo_AGENTP_007_RolloutMutationValidatesExactRefs(t *testing.T) {
	f := newFixture(t, values.TenantId("tenant-rollout"))
	store := f.store(t, values.TenantId("tenant-rollout"))
	_, err := store.PauseInstallation(context.Background(), RolloutMutation{InstallationID: "install-1", ExpectedRevision: 1, Reason: "reviewed"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}
