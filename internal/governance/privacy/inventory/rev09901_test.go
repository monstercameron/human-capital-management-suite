package inventory

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func nonAdequateTransferInventory() Inventory {
	i := validInventory()
	i.Activities[0].Subprocessors = []string{i.Flows[0].Recipient}
	i.Flows[0].SourceRegion = "EU"
	i.Flows[0].TransferRegions = []string{"US"}
	i.Activities[0].Regions = []string{"EU", "US"}
	i.Flows[0].Safeguards = []string{"eu.scc.2021-914.module-2"}
	i.Flows[0].Exporter = i.Flows[0].Controller
	i.Flows[0].ExporterRole = TransferPartyController
	i.Flows[0].Importer = i.Flows[0].Recipient
	i.Flows[0].ImporterRole = TransferPartyProcessor
	i.Flows[0].ContractRefs = []string{"dpa/1", "scc/payroll/2021-914"}
	i.Flows[0].MechanismEvidence = []TransferMechanismEvidence{{MechanismID: "eu.scc.2021-914.module-2", DocumentRef: "scc/payroll/2021-914", DocumentVersion: "2021/914"}}
	i.Activities[0].TransferAssessmentRefs = []string{"tia/payroll-provider/us/1"}
	i.Activities[0].TransferPartyRoles = []TransferPartyRoleBinding{
		{Party: i.Flows[0].Controller, Role: TransferPartyController},
		{Party: i.Flows[0].Processor, Role: TransferPartyProcessor},
		{Party: i.Flows[0].Importer, Role: TransferPartyProcessor},
	}
	i.TransferImpactAssessments = []TransferImpactAssessment{{
		ID: "tia/payroll-provider/us/1", Version: "1", RulePackVersion: "privacy-transfer-rules-2026.09.1",
		Regime: TransferRegimeEU, AssessmentKind: TransferAssessmentEUTIA, Processor: i.Flows[0].Importer, Region: "US",
		Status: TransferAssessmentApproved, Outcome: TransferOutcomePermitted,
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	i.Occurrences[0].Region = "US"
	return i
}

func currentTransferPackForTest(t *testing.T) TransferRulePack {
	t.Helper()
	pack, err := CurrentTransferRulePack()
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func TestTodo_REV_099_01(t *testing.T) {
	pack := currentTransferPackForTest(t)
	i := nonAdequateTransferInventory()
	if err := i.ValidateTransfers(pack, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "counsel approval") {
		t.Fatalf("non-adequate transfer escaped absent trusted legal approval: %v", err)
	}
	if _, err := ValidateExecutableWithTransferPolicy(i, pack, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "counsel approval") {
		t.Fatalf("executable inventory escaped absent trusted legal approval: %v", err)
	}
	if err := i.Flows[0].ValidateTransfer(i.Activities[0], i.TransferImpactAssessments, pack, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "counsel approval") {
		t.Fatalf("flow-level validation escaped absent trusted legal approval: %v", err)
	}
	if _, err := ValidateExecutable(i); err == nil || !strings.Contains(err.Error(), "counsel approval") {
		t.Fatalf("default unapproved legal-rule snapshot allowed non-adequate transfer: %v", err)
	}
	uk := nonAdequateTransferInventory()
	uk.Flows[0].SourceRegion = "UK"
	uk.Flows[0].Safeguards = []string{"uk.idta.current.v1"}
	uk.Flows[0].MechanismEvidence[0] = TransferMechanismEvidence{MechanismID: "uk.idta.current.v1", DocumentRef: "scc/payroll/2021-914", DocumentVersion: "uk-idta/v1"}
	uk.TransferImpactAssessments[0].Regime = TransferRegimeUK
	uk.TransferImpactAssessments[0].AssessmentKind = TransferAssessmentUKTest
	if err := uk.ValidateWithTransferPolicy(pack, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "counsel approval") {
		t.Fatalf("UK transfer escaped absent trusted legal approval: %v", err)
	}
	de := nonAdequateTransferInventory()
	de.Flows[0].SourceRegion = "DE"
	if err := de.ValidateWithTransferPolicy(pack, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "counsel approval") {
		t.Fatalf("country-code exporter bypassed EU regime derivation: %v", err)
	}
}

func TestTodo_REV_099_01_Golden(t *testing.T) {
	canonicalPath := filepath.Join("..", "..", "..", "..", "definitions", "legal", "packs", "privacy", "transfer-rules-2026.09.json")
	canonical, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(canonical), bytes.TrimSpace(transferPolicyJSON)) {
		t.Fatal("embedded transfer policy differs from the governed legal pack")
	}
	pack, err := CurrentTransferRulePack()
	if err != nil {
		t.Fatal(err)
	}
	mechanisms := make([]string, 0, len(pack.Mechanisms))
	for _, mechanism := range pack.Mechanisms {
		mechanisms = append(mechanisms, mechanism.ID)
	}
	sort.Strings(mechanisms)
	regionGroups := append([]TransferRegionGroup(nil), pack.RegionGroups...)
	sort.Slice(regionGroups, func(i, j int) bool { return regionGroups[i].ID < regionGroups[j].ID })
	type regionSummary struct {
		ID      string `json:"id"`
		Members int    `json:"members"`
	}
	regions := make([]regionSummary, 0, len(regionGroups))
	for _, group := range regionGroups {
		regions = append(regions, regionSummary{ID: group.ID, Members: len(group.Regions)})
	}
	got, err := json.MarshalIndent(struct {
		Version          string          `json:"version"`
		ReviewState      string          `json:"review_state"`
		AdequacyComplete bool            `json:"adequacy_registry_complete"`
		AdequacyEntries  int             `json:"adequacy_entries"`
		RegionGroups     []regionSummary `json:"region_groups"`
		Mechanisms       []string        `json:"mechanisms"`
	}{pack.Version, pack.ReviewState, pack.AdequacyRegistryComplete, len(pack.Adequacy), regions, mechanisms}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "rev09901.golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transfer registry golden changed\n got: %s\nwant: %s", got, want)
	}
}

func TestTodo_REV_099_01_Security(t *testing.T) {
	pack := currentTransferPackForTest(t)
	cases := []struct {
		name   string
		mutate func(*Inventory)
		want   string
	}{
		{"free-form safeguard", func(i *Inventory) { i.Flows[0].Safeguards = []string{"trust me"} }, "not in transfer mechanism registry"},
		{"wrong processor assessment", func(i *Inventory) { i.TransferImpactAssessments[0].Processor = "other-processor" }, "counsel approval"},
		{"wrong region assessment", func(i *Inventory) { i.TransferImpactAssessments[0].Region = "CA" }, "counsel approval"},
		{"wrong regime assessment type", func(i *Inventory) { i.TransferImpactAssessments[0].AssessmentKind = TransferAssessmentUKTest }, "incomplete identity or scope"},
		{"unbound flow processor", func(i *Inventory) { i.Flows[0].Processor = "attacker-processor" }, "authority differs from activity"},
		{"mismatched SCC module roles", func(i *Inventory) { i.Flows[0].ImporterRole = TransferPartyController }, "counsel approval"},
		{"missing mechanism document", func(i *Inventory) { i.Flows[0].MechanismEvidence = nil }, "counsel approval"},
		{"mechanism document outside contract scope", func(i *Inventory) { i.Flows[0].ContractRefs = []string{"dpa/1"} }, "counsel approval"},
		{"stale assessment", func(i *Inventory) { i.TransferImpactAssessments[0].EffectiveTo = time.Now().UTC().Add(-time.Hour) }, "counsel approval"},
		{"expired policy version", func(i *Inventory) { i.Flows[0].TransferPolicyVersion = "privacy-transfer-rules-2025.01.1" }, "does not match current"},
		{"missing assessment reference", func(i *Inventory) { i.Activities[0].TransferAssessmentRefs = nil }, "counsel approval"},
		{"unresolved assessment reference", func(i *Inventory) { i.Activities[0].TransferAssessmentRefs = []string{"unregistered-tia"} }, "does not resolve"},
		{"unknown mechanism version", func(i *Inventory) { i.Flows[0].Safeguards = []string{"eu.scc.module-2"} }, "not in transfer mechanism registry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := nonAdequateTransferInventory()
			tc.mutate(&i)
			err := i.ValidateTransfers(pack, time.Now().UTC())
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateTransfers() = %v, want ErrInvalid containing %q", err, tc.want)
			}
		})
	}
	badPack := pack
	badPack.Mechanisms = append([]TransferMechanism(nil), pack.Mechanisms...)
	badPack.Mechanisms[3].Module = "5"
	if err := badPack.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mechanism registry accepted SCC module 5: %v", err)
	}
	adequate := validInventory()
	adequate.Flows[0].SourceRegion = "DE"
	adequate.Flows[0].TransferRegions = []string{"UK"}
	adequate.Activities[0].Regions = []string{"EU", "UK"}
	adequate.Occurrences[0].Region = "UK"
	if err := adequate.Validate(); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "unapproved adequacy") {
		t.Fatalf("default legal pack allowed reliance on pending adequacy entry: %v", err)
	}

	forged := pack
	forged.ReviewState = "APPROVED"
	forged.ApprovalRef = "self-signed"
	forged.ApprovedBy = "caller"
	forged.ApprovedAt = time.Now().UTC().Add(-time.Hour)
	if err := nonAdequateTransferInventory().ValidateTransfers(forged, time.Now().UTC()); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "caller-supplied approval metadata is not trusted") {
		t.Fatalf("caller-forged approval was trusted: %v", err)
	}
	if err := validateTransferParties(nonAdequateTransferInventory().Activities[0], nonAdequateTransferInventory().Flows[0]); err != nil {
		t.Fatalf("authoritative activity party identifiers were rejected: %v", err)
	}
	badParties := nonAdequateTransferInventory()
	badParties.Flows[0].Exporter = "attacker"
	if err := validateTransferParties(badParties.Activities[0], badParties.Flows[0]); !errors.Is(err, ErrInvalid) {
		t.Fatalf("self-asserted exporter was accepted: %v", err)
	}
	if hasMechanismEvidence(pack, nonAdequateTransferInventory().Flows[0], pack.Mechanisms[3], "US") {
		t.Fatal("flow-supplied document metadata counted as independent governed contract evidence")
	}
	governedDocumentPack := pack
	governedDocumentPack.MechanismDocuments = []TransferMechanismDocument{{
		MechanismID: "eu.scc.2021-914.module-2", DocumentRef: "scc/payroll/2021-914", DocumentVersion: "2021/914",
		Exporter: "acme", ExporterRole: TransferPartyController, Importer: "payroll-provider", ImporterRole: TransferPartyProcessor,
		Regions: []string{"US"},
	}}
	flow := nonAdequateTransferInventory().Flows[0]
	sccModule2 := TransferMechanism{ID: "eu.scc.2021-914.module-2", Regime: TransferRegimeEU, Kind: "SCC", Version: "2021/914", Module: "2"}
	if !hasMechanismEvidence(governedDocumentPack, flow, sccModule2, "US") {
		t.Fatal("matching governed document binding was not recognized")
	}
	wrongVersion := flow
	wrongVersion.MechanismEvidence = []TransferMechanismEvidence{{MechanismID: "eu.scc.2021-914.module-2", DocumentRef: "scc/payroll/2021-914", DocumentVersion: "forged"}}
	if hasMechanismEvidence(governedDocumentPack, wrongVersion, sccModule2, "US") {
		t.Fatal("flow-supplied document version overrode governed document registry")
	}
}

func TestTodo_REV_099_01_Mutation(t *testing.T) {
	base, _ := json.Marshal(canonicalInventory(nonAdequateTransferInventory()))
	mutated := nonAdequateTransferInventory()
	mutated.TransferImpactAssessments[0].Version = "2"
	changed, _ := json.Marshal(canonicalInventory(mutated))
	if bytes.Equal(base, changed) {
		t.Fatal("TIA version mutation did not change the executable inventory digest")
	}
}
