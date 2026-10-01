package latencygate

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"strings"
	"time"
)

// WorkflowPageBudget is the measurable contract for the workflow page
// surfaces. Designer-only assets are explicitly outside the initial bundle.
type WorkflowPageBudget struct {
	InitialGzipBytes        int
	WarmWorkflowInteractive time.Duration
	WarmDesignerOpen        time.Duration
	InitialSurfaces         []string
	DesignerRouteSurfaces   []string
}

const (
	WorkflowPageInitialGzipBudget = 150 * 1024
	WorkflowPageWarmInteractive   = 1500 * time.Millisecond
	WorkflowPageWarmDesigner      = 2500 * time.Millisecond
)

func DefaultWorkflowPageBudget() WorkflowPageBudget {
	return WorkflowPageBudget{
		InitialGzipBytes: WorkflowPageInitialGzipBudget, WarmWorkflowInteractive: WorkflowPageWarmInteractive, WarmDesignerOpen: WorkflowPageWarmDesigner,
		InitialSurfaces:       []string{"workflow-start", "workflow-history", "workflow-page-rules"},
		DesignerRouteSurfaces: []string{"workflow-designer", "widget-palette", "workflow-page-preview"},
	}
}

// WorkflowPageBundleMeasurement is emitted by the browser/WASM harness and
// is deliberately small enough to be checked in CI or in a golden fixture.
type WorkflowPageBundleMeasurement struct {
	InitialGzipBytes        int
	WorkflowInteractive     time.Duration
	DesignerOpen            time.Duration
	DesignerLoadedOnInitial bool
}

func GzipBytes(payload []byte) (int, error) {
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return 0, fmt.Errorf("workflow page budget: gzip writer: %w", err)
	}
	if _, err := writer.Write(payload); err != nil {
		return 0, fmt.Errorf("workflow page budget: gzip payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return 0, fmt.Errorf("workflow page budget: gzip close: %w", err)
	}
	return compressed.Len(), nil
}

func CheckWorkflowPageBudget(budget WorkflowPageBudget, measurement WorkflowPageBundleMeasurement) error {
	if budget.InitialGzipBytes < 1 || budget.WarmWorkflowInteractive <= 0 || budget.WarmDesignerOpen <= 0 {
		return fmt.Errorf("workflow page budget: positive byte and time budgets are required")
	}
	if measurement.InitialGzipBytes > budget.InitialGzipBytes {
		return fmt.Errorf("workflow page budget: initial bundle %d gzip bytes exceeds %d", measurement.InitialGzipBytes, budget.InitialGzipBytes)
	}
	if measurement.WorkflowInteractive > budget.WarmWorkflowInteractive {
		return fmt.Errorf("workflow page budget: workflow page interactive time %s exceeds %s", measurement.WorkflowInteractive, budget.WarmWorkflowInteractive)
	}
	if measurement.DesignerOpen > budget.WarmDesignerOpen {
		return fmt.Errorf("workflow page budget: designer open time %s exceeds %s", measurement.DesignerOpen, budget.WarmDesignerOpen)
	}
	if measurement.DesignerLoadedOnInitial {
		return fmt.Errorf("workflow page budget: designer palette/preview loaded in initial bundle")
	}
	return nil
}

func ValidateWorkflowPageBudgetDefinition(budget WorkflowPageBudget) error {
	if budget.InitialGzipBytes != WorkflowPageInitialGzipBudget || budget.WarmWorkflowInteractive != WorkflowPageWarmInteractive || budget.WarmDesignerOpen != WorkflowPageWarmDesigner {
		return fmt.Errorf("workflow page budget: governed limits changed")
	}
	seen := make(map[string]bool, len(budget.InitialSurfaces))
	for _, surface := range budget.InitialSurfaces {
		if surface = strings.TrimSpace(surface); surface == "" || seen[surface] {
			return fmt.Errorf("workflow page budget: invalid initial surface")
		}
		seen[surface] = true
	}
	for _, surface := range budget.DesignerRouteSurfaces {
		if strings.TrimSpace(surface) == "" || seen[strings.TrimSpace(surface)] {
			return fmt.Errorf("workflow page budget: designer surface is not lazy")
		}
	}
	return nil
}
