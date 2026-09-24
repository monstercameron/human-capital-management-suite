package application

import (
	"context"
	"testing"
	"time"
)

func TestTodo_REV_028_02_ExporterFailsClosedWithoutDatabase(t *testing.T) {
	export := NewLedgerEvidenceExport(nil)
	_, err := export(context.Background(), "tenant-a", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("ledger evidence export succeeded without a database")
	}
}
