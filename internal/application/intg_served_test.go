package application

import (
	"testing"

	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
)

func TestTodo_INTG_004_Served(t *testing.T) {
	svc, _, ctx, connection := integrationServiceFixture(t)
	result, err := svc.TestConnectorConnection(ctx, &integrationv1.TestConnectorConnectionRequest{ConnectionId: connection.ID()})
	if err != nil {
		t.Fatalf("TestConnectorConnection: %v", err)
	}
	checks := make(map[string]integrationv1.DiagnosticResult, len(result.GetDiagnostic().GetChecks()))
	for _, check := range result.GetDiagnostic().GetChecks() {
		checks[check.GetName()] = check.GetResult()
	}
	if got := checks["surface:integrations.schemas.discover"]; got != integrationv1.DiagnosticResult_DIAGNOSTIC_RESULT_PASS {
		t.Fatalf("schema discovery check = %s, want PASS", got)
	}
	if got := checks["surface:integrations.schemas.diff"]; got != integrationv1.DiagnosticResult_DIAGNOSTIC_RESULT_SKIPPED {
		t.Fatalf("schema diff check = %s, want SKIPPED without two admitted bodies", got)
	}
	if got := checks["surface:integrations.mappings.validate"]; got != integrationv1.DiagnosticResult_DIAGNOSTIC_RESULT_SKIPPED {
		t.Fatalf("mapping check = %s, want SKIPPED without an explicit profile", got)
	}
}
