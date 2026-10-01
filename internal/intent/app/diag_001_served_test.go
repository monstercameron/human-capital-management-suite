package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_DIAG_001_Integration(t *testing.T) {
	cell, err := NewCell(CellConfig{
		Store:    newMemLifecycleStore(),
		Audience: "hcm-next-api",
		Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) {
			return nil, errors.New("verification bypassed")
		}),
	})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	if cell.Service == nil || cell.Service.Diagnostics() == nil {
		t.Fatal("served cell did not compose the diagnostic admission adapter")
	}
	manifest := cell.Service.Diagnostics().Manifest()
	if manifest.Enabled || len(manifest.Surfaces) != 0 {
		t.Fatalf("served diagnostics must remain default-off: %+v", manifest)
	}
}
