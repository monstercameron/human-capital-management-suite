package timeclock

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/contractortime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_WTIME_011(t *testing.T) {
	def := ContractorInvoiceDefinition(DefaultParams())
	if def.WorkflowID != ContractorWorkflowID || def.StartNodeID != NodeContractorOpenPeriod {
		t.Fatalf("contractor definition identity = %s/%s", def.WorkflowID, def.StartNodeID)
	}
	if len(def.Nodes) != 14 {
		t.Fatalf("contractor node count = %d, want 14", len(def.Nodes))
	}
	if _, err := CompileDefinition(def); err != nil {
		t.Fatalf("contractor definition does not compile: %v", err)
	}

	byID := make(map[string]workflow.Node, len(def.Nodes))
	for _, node := range def.Nodes {
		byID[node.ID] = node
	}
	for _, id := range []string{NodeContractorOpenPeriod, NodeContractorRecordEntry, NodeContractorSubmitToAP} {
		if byID[id].Capability == nil {
			t.Fatalf("%s has no capability binding", id)
		}
	}
	if byID[NodeContractorObserveAP].Observe == nil {
		t.Fatal("AP acceptance is not an observation")
	}
	if byID[NodeContractorAwaitReopen].Signal == nil {
		t.Fatal("reopen exception is not a typed signal")
	}

	for _, node := range def.Nodes {
		if node.Capability == nil {
			continue
		}
		for _, forbidden := range []string{"geofence", "photo", "lockout", "schedule", "clock", "break"} {
			if strings.Contains(strings.ToLower(node.Capability.ID), forbidden) {
				t.Fatalf("employee control %q appears in contractor capability %q", forbidden, node.Capability.ID)
			}
		}
	}
}

func TestTodo_WTIME_011_Security(t *testing.T) {
	valid := ContractorPolicy{
		Pricing: contractPricingHourly(), SOWReference: "sow-1", POReference: "po-1",
		RateCardRef: "rates-1", Currency: "USD", TaxTreatment: taxNone(), SelfBilling: true,
	}
	if !valid.Valid() {
		t.Fatal("valid contractor policy rejected")
	}
	invalid := valid
	invalid.SOWReference = ""
	if invalid.Valid() {
		t.Fatal("policy without SOW accepted")
	}
	invalid = valid
	invalid.Pricing = "EMPLOYEE_CLOCK"
	if invalid.Valid() {
		t.Fatal("employee control pricing accepted")
	}
}

func TestTodo_WTIME_011_Golden(t *testing.T) {
	def := ContractorInvoiceDefinition(DefaultParams())
	for _, edge := range def.Edges {
		if edge.From == NodeContractorObserveAP && edge.RouteKey == string(workflow.OutcomePass) && edge.To != NodeContractorAwaitReopen {
			t.Fatalf("AP pass route = %s, want reopen window", edge.To)
		}
		if edge.From == NodeContractorAwaitReopen && edge.RouteKey == "TIMED_OUT" && edge.To != NodeContractorEndSubmitted {
			t.Fatalf("reopen timeout route = %s, want submitted terminal", edge.To)
		}
	}
}

func contractPricingHourly() contractortime.PricingModel { return contractortime.PricingHourly }
func taxNone() contractortime.TaxKind                    { return contractortime.TaxNone }
