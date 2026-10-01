package application

import (
	"context"
	"testing"
)

// TestServeAgentComposition_DisablesWithoutConfiguredDatabase proves the
// optional isolated pool does not silently fall back to the core database.
func TestServeAgentComposition_DisablesWithoutConfiguredDatabase(t *testing.T) {
	composed, err := composeAgentDatabase(context.Background(), ServeConfig{})
	if err != nil {
		t.Fatalf("composeAgentDatabase() error = %v", err)
	}
	if composed.store != nil || composed.personas != nil || composed.close != nil {
		t.Fatalf("unconfigured agent database composed resources: %+v", composed)
	}
}

// TestServeAgentComposition_UsesDedicatedLifecycleNames keeps the graph and
// shutdown names tied to the isolated pool composition.
func TestServeAgentComposition_UsesDedicatedLifecycleNames(t *testing.T) {
	if ComponentAgentDatabasePool == ComponentDatabasePool {
		t.Fatal("agent database pool must have an independent graph component")
	}
	if ComponentShutdownAgentDatabase == ComponentShutdownChat {
		t.Fatal("agent database shutdown must have an independent lifecycle step")
	}
}
